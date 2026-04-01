//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegration_MySQLCDC(t *testing.T) {
	mysqlDSN := os.Getenv("MYSQL_DSN")
	pgDSN := os.Getenv("POSTGRES_DSN")
	if mysqlDSN == "" || pgDSN == "" {
		t.Skip("MYSQL_DSN and POSTGRES_DSN env vars required")
	}

	ctx := context.Background()

	// --- Seed source ---
	sourceDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer sourceDB.Close()

	seedMySQLNoOrphans(t, sourceDB)

	// Ensure binlog_format is ROW.
	var binlogFormat string
	if err := sourceDB.QueryRow("SELECT @@binlog_format").Scan(&binlogFormat); err != nil {
		t.Fatalf("check binlog_format: %v", err)
	}
	if binlogFormat != "ROW" {
		t.Skipf("skipping CDC test: binlog_format=%s (need ROW)", binlogFormat)
	}

	sourceDB.Close()

	// --- Run migrate with CDC mode ---
	src := &mysqlSourceDB{}
	srcDB2, err := src.OpenDB(mysqlDSN)
	if err != nil {
		t.Fatalf("open mysql for introspection: %v", err)
	}
	dbName, err := src.ExtractDBName(mysqlDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}
	schema, err := src.IntrospectSchema(srcDB2, dbName)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	srcDB2.Close()

	pgPool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	defer pgPool.Close()

	const pgSchema = "inttest_cdc"
	_, _ = pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		pgPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	})

	typeMap := defaultTypeMappingConfig()

	// Create tables.
	if err := createTables(ctx, pgPool, schema, pgSchema, false, true, typeMap, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	// Capture binlog position.
	capDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true")
	if err != nil {
		t.Fatalf("open mysql for binlog capture: %v", err)
	}
	cdcPos, err := captureBinlogPosition(ctx, capDB)
	capDB.Close()
	if err != nil {
		t.Skipf("skipping CDC test: binary logging not available: %v", err)
	}
	t.Logf("captured binlog position: %s:%d", cdcPos.File, cdcPos.Pos)

	// Create and seed CDC checkpoint.
	if err := createCDCCheckpointTable(ctx, pgPool, pgSchema); err != nil {
		t.Fatalf("create cdc checkpoint table: %v", err)
	}
	if err := seedCDCCheckpoint(ctx, pgPool, pgSchema, cdcPos); err != nil {
		t.Fatalf("seed cdc checkpoint: %v", err)
	}

	// Migrate data (initial load).
	if err := migrateData(ctx, migrateDataConfig{
		Src: src, SrcDSN: mysqlDSN, Pool: pgPool, Schema: schema, PGSchema: pgSchema,
		Workers: 2, TypeMap: typeMap, SourceSnapshotMode: "single_tx",
		ChunkSize: 100000, ConfigDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("migrateData: %v", err)
	}

	// Post-migrate: add PKs so upserts work.
	cfg := &MigrationConfig{
		Schema:         pgSchema,
		OnSchemaExists: "error",
		TypeMapping:    typeMap,
	}
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	// Verify initial counts.
	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)

	// --- Make changes in MySQL ---
	changeDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql for changes: %v", err)
	}
	defer changeDB.Close()

	// INSERT a new user.
	if _, err := changeDB.Exec("INSERT INTO users (name, email) VALUES ('Frank', 'frank@example.com')"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// UPDATE an existing user.
	if _, err := changeDB.Exec("UPDATE users SET email = 'alice_updated@example.com' WHERE name = 'Alice'"); err != nil {
		t.Fatalf("update user: %v", err)
	}
	// DELETE a user (Eve, id=5 — delete comments on her posts, her posts, then her).
	// Eve's post (id=5) has comments from other users, so delete by post_id first.
	if _, err := changeDB.Exec("DELETE FROM comments WHERE post_id IN (SELECT id FROM posts WHERE user_id = 5)"); err != nil {
		t.Fatalf("delete comments on eve posts: %v", err)
	}
	// Also delete Eve's own comments on other posts.
	if _, err := changeDB.Exec("DELETE FROM comments WHERE user_id = 5"); err != nil {
		t.Fatalf("delete eve comments: %v", err)
	}
	if _, err := changeDB.Exec("DELETE FROM posts WHERE user_id = 5"); err != nil {
		t.Fatalf("delete posts: %v", err)
	}
	if _, err := changeDB.Exec("DELETE FROM users WHERE name = 'Eve'"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	// --- Run replicate ---
	tables := make(map[string]Table)
	for _, tbl := range schema.Tables {
		if tbl.PrimaryKey != nil {
			tables[tbl.SourceName] = tbl
		}
	}

	readerCtx, readerCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readerCancel()

	reader, err := NewBinlogReader(BinlogReaderConfig{
		DSN:      mysqlDSN,
		ServerID: 99999,
		StartPos: cdcPos,
		Tables:   tables,
		Src:      src,
		TypeMap:  typeMap,
		DBName:   dbName,
	})
	if err != nil {
		t.Fatalf("create binlog reader: %v", err)
	}
	defer reader.Close()

	applier := NewCDCApplier(pgPool, pgSchema, tables, src, typeMap)
	batcher := newCDCBatcher(100)

	// Read until we catch up (context timeout is our safety net).
	for {
		ev, readErr := reader.ReadEvent(readerCtx)
		if readErr != nil {
			break // Timeout means we've likely caught up.
		}
		if ev == nil {
			continue
		}
		if batch := batcher.Add(ev); batch != nil {
			if applyErr := applier.ApplyBatch(ctx, batch, batcher.Position()); applyErr != nil {
				t.Fatalf("apply batch: %v", applyErr)
			}
		}
	}
	// Flush remaining.
	if batch := batcher.Flush(); batch != nil {
		if err := applier.ApplyBatch(ctx, batch, batcher.Position()); err != nil {
			t.Fatalf("apply final batch: %v", err)
		}
	}

	// --- Assert CDC changes applied ---

	// Users: 5 original - 1 deleted (Eve) + 1 inserted (Frank) = 5
	assertRowCount(t, pgPool, pgSchema, "users", 5)

	// Check Frank was inserted.
	var frankEmail string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT email FROM %s.users WHERE name = 'Frank'", pgIdent(pgSchema)),
	).Scan(&frankEmail)
	if err != nil {
		t.Fatalf("query Frank: %v", err)
	}
	if frankEmail != "frank@example.com" {
		t.Errorf("expected Frank's email 'frank@example.com', got %q", frankEmail)
	}

	// Check Alice was updated.
	var aliceEmail string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT email FROM %s.users WHERE name = 'Alice'", pgIdent(pgSchema)),
	).Scan(&aliceEmail)
	if err != nil {
		t.Fatalf("query Alice: %v", err)
	}
	if aliceEmail != "alice_updated@example.com" {
		t.Errorf("expected Alice's email 'alice_updated@example.com', got %q", aliceEmail)
	}

	// Check Eve was deleted.
	var eveCount int
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s.users WHERE name = 'Eve'", pgIdent(pgSchema)),
	).Scan(&eveCount)
	if err != nil {
		t.Fatalf("query Eve count: %v", err)
	}
	if eveCount != 0 {
		t.Errorf("expected Eve to be deleted, got count=%d", eveCount)
	}

	// Check CDC checkpoint was advanced.
	checkpoint, err := readCDCCheckpoint(ctx, pgPool, pgSchema)
	if err != nil {
		t.Fatalf("read cdc checkpoint: %v", err)
	}
	if checkpoint.EventsApplied == 0 {
		t.Error("expected events_applied > 0")
	}
	t.Logf("CDC checkpoint: file=%s pos=%d applied=%d skipped=%d",
		checkpoint.BinlogFile, checkpoint.BinlogPos, checkpoint.EventsApplied, checkpoint.EventsSkipped)
}
