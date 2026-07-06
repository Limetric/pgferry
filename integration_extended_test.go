//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func requireMariaDBAndPostgresDSNs(t *testing.T) (string, string) {
	t.Helper()
	maria := os.Getenv("MARIADB_DSN")
	pg := os.Getenv("POSTGRES_DSN")
	if maria == "" || pg == "" {
		t.Skip("MARIADB_DSN and POSTGRES_DSN env vars required")
	}
	return maria, pg
}

func introspectMSSQLSchemaForTest(t *testing.T, mssqlDSN string) (*mssqlSourceDB, *Schema) {
	t.Helper()

	dsn := normalizeIntegrationMSSQLDSN(mssqlDSN)
	src := &mssqlSourceDB{}
	src.SetIdentifierCase("snake")
	src.SetSourceSchema("sales")

	db, err := src.OpenDB(dsn)
	if err != nil {
		t.Fatalf("open mssql for introspection: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	dbName, err := src.ExtractDBName(dsn)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}

	schema, err := src.IntrospectSchema(db, dbName)
	if err != nil {
		t.Fatalf("introspect schema: %v", err)
	}
	return src, schema
}

func runValidateFromConfig(t *testing.T, cfgPath string) {
	t.Helper()
	prev, prevV := configPath, validateConfigPath
	t.Cleanup(func() { configPath, validateConfigPath = prev, prevV })
	configPath = ""
	validateConfigPath = ""
	if err := runValidate(&cobra.Command{}, []string{cfgPath}); err != nil {
		t.Fatalf("runValidate(%s): %v", cfgPath, err)
	}
}

// seedSQLiteResumeFixture creates an events table with one row that cannot be loaded into timestamptz.
func seedSQLiteResumeFixture(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		"DROP TABLE IF EXISTS events",
		`CREATE TABLE events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			happened_at DATETIME NOT NULL,
			note TEXT NOT NULL
		)`,
		"INSERT INTO events (happened_at, note) VALUES ('2024-01-01 01:02:03', 'one')",
		"INSERT INTO events (happened_at, note) VALUES ('2024-01-02 02:03:04', 'two')",
		"INSERT INTO events (happened_at, note) VALUES ('2024-01-03 03:04:05', 'three')",
		"INSERT INTO events (happened_at, note) VALUES ('not-a-valid-timestamp', 'bad-four')",
		"INSERT INTO events (happened_at, note) VALUES ('2024-01-05 05:06:07', 'five')",
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed sqlite resume %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}
}

// --- MariaDB parity with selected MySQL-only scenarios ---

func TestIntegration_MariaDB_SingleTx(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)
	ctx := context.Background()

	db, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_maria_single_tx")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
workers = 4
chunk_size = 2
source_snapshot_mode = "single_tx"

[source]
type = "mariadb"
dsn = %q

[target]
dsn = %q
`, pgSchema, mariaDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)
	assertFKExists(t, pgPool, pgSchema, "posts", "users")

	var name string
	err = pgPool.QueryRow(ctx, fmt.Sprintf("SELECT name FROM %s.users WHERE id = 1", pgIdent(pgSchema))).Scan(&name)
	if err != nil {
		t.Fatalf("query migrated user: %v", err)
	}
	if name != "Alice" {
		t.Fatalf("name = %q, want Alice", name)
	}
}

func TestIntegration_MariaDB_ValidationRowCount(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)

	db, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_maria_validate_rows")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "mariadb"
dsn = %q

[target]
dsn = %q
`, pgSchema, mariaDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)
}

func TestIntegration_MariaDB_ValidationRowCountMismatchAfterHook(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)

	db, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_maria_validate_rows_fail")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "after_data.sql"), []byte(`DELETE FROM {{schema}}.users WHERE id = 1;`), 0644); err != nil {
		t.Fatalf("write after_data hook: %v", err)
	}
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "mariadb"
dsn = %q

[target]
dsn = %q

[hooks]
after_data = ["after_data.sql"]
`, pgSchema, mariaDSN, pgDSN))

	err = runMigrationFromConfigExpectError(t, cfgPath)
	if !strings.Contains(err.Error(), "row count mismatch") || !strings.Contains(err.Error(), "users") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestIntegration_MariaDB_ValidationSampledHash(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)

	db, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_maria_validate_hash")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "sampled_hash"

[source]
type = "mariadb"
dsn = %q

[target]
dsn = %q
`, pgSchema, mariaDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)
}

func TestIntegration_MariaDB_ValidateStandaloneRowCount(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)

	db, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_validate_standalone_maria")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "mariadb"
dsn = %q

[target]
dsn = %q
`, pgSchema, mariaDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)
	runValidateFromConfig(t, cfgPath)
}

func TestIntegration_MariaDB_ValidationSampledHashMismatchAfterHook(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)

	db, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_maria_validate_hash_fail")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "after_data.sql"), []byte(`UPDATE {{schema}}.users SET name = 'Mallory' WHERE id = 1;`), 0644); err != nil {
		t.Fatalf("write after_data hook: %v", err)
	}
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "sampled_hash"

[source]
type = "mariadb"
dsn = %q

[target]
dsn = %q

[hooks]
after_data = ["after_data.sql"]
`, pgSchema, mariaDSN, pgDSN))

	err = runMigrationFromConfigExpectError(t, cfgPath)
	if !strings.Contains(err.Error(), "sampled_hash mismatch") || !strings.Contains(err.Error(), "users") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestIntegration_MariaDB_ResumeAfterChunkFailure(t *testing.T) {
	mariaDSN, pgDSN := requireMariaDBAndPostgresDSNs(t)
	ctx := context.Background()

	mariaDB, err := sql.Open("mysql", mariaDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mariadb: %v", err)
	}
	defer mariaDB.Close()
	seedMySQLResumeFixture(t, mariaDB)

	src := &mariadbSourceDB{}
	src.SetIdentifierCase("snake")

	sourceDB, err := src.OpenDB(mariaDSN)
	if err != nil {
		t.Fatalf("open mariadb for introspection: %v", err)
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)

	dbName, err := src.ExtractDBName(mariaDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		t.Fatalf("introspect schema: %v", err)
	}

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_maria_resume")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	tmpDir := t.TempDir()
	cfg := &MigrationConfig{
		Source:             SourceConfig{Type: "mariadb", DSN: mariaDSN},
		Target:             TargetConfig{DSN: pgDSN},
		Schema:             pgSchema,
		IncludeTables:      []string{"events"},
		Workers:            1,
		ChunkSize:          2,
		Resume:             true,
		UnloggedTables:     false,
		PreserveDefaults:   true,
		OnSchemaExists:     "error",
		SourceSnapshotMode: "none",
		IdentifierCase:     "snake",
		Validation:         "none",
		TypeMapping:        defaultTypeMappingConfig(),
		configDir:          tmpDir,
	}
	schema, _, err = filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filter schema: %v", err)
	}
	cfg.TypeMapping.ZeroDateMode = "error"
	typeMap := effectiveTypeMapping(cfg)
	compat, err := buildCheckpointCompatibility(cfg, schema, src, dbName, typeMap)
	if err != nil {
		t.Fatalf("buildCheckpointCompatibility: %v", err)
	}

	if err := createTables(ctx, pgPool, schema, pgSchema, false, cfg.PreserveDefaults, typeMap, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	dataCfg := migrateDataConfig{
		Src:                 src,
		SrcDSN:              mariaDSN,
		Pool:                pgPool,
		Schema:              schema,
		PGSchema:            pgSchema,
		Workers:             1,
		TypeMap:             typeMap,
		SourceSnapshotMode:  "none",
		ChunkSize:           2,
		Resume:              true,
		ConfigDir:           tmpDir,
		ResumeCompatibility: compat,
	}

	err = migrateData(ctx, dataCfg)
	if err == nil {
		t.Fatal("expected migrateData to fail on zero datetime chunk")
	}
	if !strings.Contains(err.Error(), "zero date/time value") {
		t.Fatalf("unexpected migrateData error: %v", err)
	}

	assertRowCount(t, pgPool, pgSchema, "events", 2)

	cpPath := checkpointPath(tmpDir)
	state, err := loadCheckpoint(cpPath)
	if err != nil {
		t.Fatalf("loadCheckpoint: %v", err)
	}
	if state == nil || state.Tables["events"] == nil {
		t.Fatalf("checkpoint state = %+v, want events progress", state)
	}
	if !state.isChunkCompleted("events", 0) {
		t.Fatalf("checkpoint should mark first chunk completed: %+v", state.Tables["events"])
	}
	if state.isChunkCompleted("events", 1) {
		t.Fatalf("checkpoint should not mark failed chunk completed: %+v", state.Tables["events"])
	}

	if _, err := mariaDB.Exec(`UPDATE events SET happened_at = '2024-01-04 04:05:06' WHERE id = 4`); err != nil {
		t.Fatalf("fix source row: %v", err)
	}

	if err := migrateData(ctx, dataCfg); err != nil {
		t.Fatalf("resume migrateData: %v", err)
	}
	if _, err := os.Stat(cpPath); !os.IsNotExist(err) {
		t.Fatalf("checkpoint file should be removed after successful resume, stat err=%v", err)
	}

	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	assertRowCount(t, pgPool, pgSchema, "events", 5)

	seqName := generatedSequenceName(Table{PGName: "events"}, Column{PGName: "id"})
	var (
		lastValue int64
		isCalled  bool
	)
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT last_value, is_called FROM %s", pgQualifiedIdent(pgSchema, seqName)),
	).Scan(&lastValue, &isCalled)
	if err != nil {
		t.Fatalf("query resumed sequence state: %v", err)
	}
	if lastValue != 6 || isCalled {
		t.Fatalf("sequence state after resume = last_value:%d is_called:%t, want last_value:6 is_called:false", lastValue, isCalled)
	}

	var nextID int64
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s.events (happened_at, note) VALUES ('2024-01-06 06:07:08', 'after-resume') RETURNING id", pgIdent(pgSchema)),
	).Scan(&nextID)
	if err != nil {
		t.Fatalf("insert after resume: %v", err)
	}
	if nextID != 6 {
		t.Fatalf("next inserted id after resume = %d, want 6", nextID)
	}
}

// --- SQLite: schema/data split, validation, resume, standalone validate ---

func TestIntegration_SQLite_SchemaOnly(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_schema_only")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
schema_only = true

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 0)
	assertRowCount(t, pgPool, pgSchema, "posts", 0)
	assertRowCount(t, pgPool, pgSchema, "comments", 0)

	for _, tbl := range []string{"users", "posts", "comments"} {
		assertPKExists(t, pgPool, pgSchema, tbl)
		assertTablePersistence(t, pgPool, pgSchema, tbl, "p")
	}
	assertFKExists(t, pgPool, pgSchema, "posts", "users")
	assertFKExists(t, pgPool, pgSchema, "comments", "posts")
	assertFKExists(t, pgPool, pgSchema, "comments", "users")
}

func TestIntegration_SQLite_DataOnly_PrecreatedSchema(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	src := &sqliteSourceDB{}
	src.SetIdentifierCase("snake")
	db, err := src.OpenDB(sqliteFile)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	dbName, err := src.ExtractDBName(sqliteFile)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}
	schema, err := src.IntrospectSchema(db, dbName)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_data_only")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	preCfg := &MigrationConfig{
		Schema:           pgSchema,
		SchemaOnly:       true,
		PreserveDefaults: true,
		CleanOrphans:     true,
		TypeMapping:      defaultTypeMappingConfig(),
	}
	if err := createTables(ctx, pgPool, schema, pgSchema, false, preCfg.PreserveDefaults, preCfg.TypeMapping, src); err != nil {
		t.Fatalf("precreate tables: %v", err)
	}
	if err := postMigrate(ctx, pgPool, schema, preCfg); err != nil {
		t.Fatalf("precreate post-migrate: %v", err)
	}

	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE TABLE %s.manual_marker (note text)", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create marker table: %v", err)
	}
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("INSERT INTO %s.manual_marker (note) VALUES ('keep-me')", pgIdent(pgSchema))); err != nil {
		t.Fatalf("insert marker row: %v", err)
	}

	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
data_only = true

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)

	var insertedID int64
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s.users (name, email) VALUES ('SQLitePrecreatedProbe', NULL) RETURNING id", pgIdent(pgSchema)),
	).Scan(&insertedID)
	if err != nil {
		t.Fatalf("insert SQLite precreated-schema user after data_only: %v", err)
	}
	if insertedID != 6 {
		t.Errorf("inserted user id after SQLite precreated-schema data_only reset: got %d, want 6", insertedID)
	}

	var markerCount int
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s.manual_marker", pgIdent(pgSchema)),
	).Scan(&markerCount)
	if err != nil {
		t.Fatalf("count marker rows: %v", err)
	}
	if markerCount != 1 {
		t.Errorf("manual_marker row count: got %d, want 1", markerCount)
	}
}

// TestIntegration_SQLite_DataOnly_ExternalSchemaSequenceNames covers data_only
// against a target schema created by an external migration tool whose sequences
// do not follow pgferry's {table}_{col}_seq convention (issue #248): a renamed
// identity sequence, a custom-named OWNED BY sequence, and — for the fallback
// path — a free-standing sequence under the convention name.
func TestIntegration_SQLite_DataOnly_ExternalSchemaSequenceNames(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_ext_seq")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	qs := pgIdent(pgSchema)
	for _, stmt := range []string{
		fmt.Sprintf("CREATE SCHEMA %s", qs),
		// users: identity column whose backing sequence was renamed by the
		// external tool — resolvable only via pg_get_serial_sequence.
		fmt.Sprintf(`CREATE TABLE %s.users (
			id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			name text NOT NULL,
			email text
		)`, qs),
		fmt.Sprintf("ALTER SEQUENCE %s.users_id_seq RENAME TO ef_users_identity", qs),
		// posts: serial-style OWNED BY sequence under a custom name.
		fmt.Sprintf("CREATE SEQUENCE %s.ef_posts_pk", qs),
		fmt.Sprintf(`CREATE TABLE %s.posts (
			id bigint PRIMARY KEY DEFAULT nextval('%s.ef_posts_pk'),
			user_id bigint NOT NULL REFERENCES %s.users(id),
			title text NOT NULL,
			body text
		)`, qs, qs, qs),
		fmt.Sprintf("ALTER SEQUENCE %s.ef_posts_pk OWNED BY %s.posts.id", qs, qs),
		// comments: free-standing sequence under pgferry's convention name,
		// like a schema created by an older pgferry schema_only run.
		fmt.Sprintf("CREATE SEQUENCE %s.comments_id_seq", qs),
		fmt.Sprintf(`CREATE TABLE %s.comments (
			id bigint PRIMARY KEY DEFAULT nextval('%s.comments_id_seq'),
			post_id bigint NOT NULL REFERENCES %s.posts(id),
			user_id bigint NOT NULL REFERENCES %s.users(id),
			content text
		)`, qs, qs, qs, qs),
	} {
		if _, err := pgPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("create external schema %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}

	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
data_only = true

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)

	inserts := []struct {
		table  string
		sql    string
		wantID int64
	}{
		{"users", fmt.Sprintf("INSERT INTO %s.users (name, email) VALUES ('ExternalSeqProbe', NULL) RETURNING id", qs), 6},
		{"posts", fmt.Sprintf("INSERT INTO %s.posts (user_id, title, body) VALUES (1, 'Probe', NULL) RETURNING id", qs), 6},
		{"comments", fmt.Sprintf("INSERT INTO %s.comments (post_id, user_id, content) VALUES (1, 1, 'Probe') RETURNING id", qs), 11},
	}
	for _, ins := range inserts {
		var insertedID int64
		if err := pgPool.QueryRow(ctx, ins.sql).Scan(&insertedID); err != nil {
			t.Fatalf("insert into %s after external-schema data_only: %v", ins.table, err)
		}
		if insertedID != ins.wantID {
			t.Errorf("inserted %s id after external-schema data_only reset: got %d, want %d", ins.table, insertedID, ins.wantID)
		}
	}
}

// TestIntegration_SQLite_DataOnly_DefaultNextvalSequence covers data_only against
// a target schema whose sequence is wired to the column only through DEFAULT
// nextval(...) — custom name, no OWNED BY, no identity (issue #250). Entity
// Framework creates sequences this way; pg_get_serial_sequence returns NULL for
// them, so the lookup must resolve the sequence from the column's DEFAULT
// expression via pg_depend.
func TestIntegration_SQLite_DataOnly_DefaultNextvalSequence(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", sqliteFile)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			label TEXT NOT NULL
		)`,
		"INSERT INTO orders (label) VALUES ('one')",
		"INSERT INTO orders (label) VALUES ('two')",
		"INSERT INTO orders (label) VALUES ('three')",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed sqlite %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}
	db.Close()

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_nextval_seq")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	qs := pgIdent(pgSchema)
	for _, stmt := range []string{
		fmt.Sprintf("CREATE SCHEMA %s", qs),
		fmt.Sprintf(`CREATE SEQUENCE %s."OrderIdSequence"`, qs),
		fmt.Sprintf(`CREATE TABLE %s.orders (
			id bigint PRIMARY KEY DEFAULT nextval('%s."OrderIdSequence"'),
			label text NOT NULL
		)`, qs, qs),
	} {
		if _, err := pgPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("create external schema %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}

	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
data_only = true

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "orders", 3)

	var insertedID int64
	insert := fmt.Sprintf("INSERT INTO %s.orders (label) VALUES ('probe') RETURNING id", qs)
	if err := pgPool.QueryRow(ctx, insert).Scan(&insertedID); err != nil {
		t.Fatalf("insert into orders after DEFAULT-nextval data_only: %v", err)
	}
	if insertedID != 4 {
		t.Errorf("inserted orders id after DEFAULT-nextval sequence reset: got %d, want 4", insertedID)
	}
}

func TestIntegration_SQLite_SchemaOnlyThenDataOnly(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_split")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	schemaOnlyCfg := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
schema_only = true

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, schemaOnlyCfg)

	assertRowCount(t, pgPool, pgSchema, "users", 0)

	dataOnlyCfg := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
data_only = true

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, dataOnlyCfg)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)

	var body string
	err := pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT body FROM %s.posts WHERE id = 1", pgIdent(pgSchema)),
	).Scan(&body)
	if err != nil {
		t.Fatalf("spot-check split-mode post body: %v", err)
	}
	if body != "Hello world" {
		t.Errorf("posts.id=1 body: got %q, want %q", body, "Hello world")
	}

	var insertedID int64
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s.users (name, email) VALUES ('SQLiteSplitProbe', NULL) RETURNING id", pgIdent(pgSchema)),
	).Scan(&insertedID)
	if err != nil {
		t.Fatalf("insert SQLite split-mode user after data_only: %v", err)
	}
	if insertedID != 6 {
		t.Errorf("inserted user id after SQLite split-mode data_only reset: got %d, want 6", insertedID)
	}
}

func TestIntegration_SQLite_ValidationRowCount(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_validate")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)
}

func TestIntegration_SQLite_ResumeAfterChunkFailure(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	ctx := context.Background()
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "resume.db")
	seedSQLiteResumeFixture(t, sqliteFile)

	src := &sqliteSourceDB{}
	src.SetIdentifierCase("snake")

	sourceDB, err := src.OpenDB(sqliteFile)
	if err != nil {
		t.Fatalf("open sqlite for introspection: %v", err)
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)

	dbName, err := src.ExtractDBName(sqliteFile)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		t.Fatalf("introspect schema: %v", err)
	}

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_sqlite_resume")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	cfg := &MigrationConfig{
		Source:             SourceConfig{Type: "sqlite", DSN: sqliteFile},
		Target:             TargetConfig{DSN: pgDSN},
		Schema:             pgSchema,
		IncludeTables:      []string{"events"},
		Workers:            1,
		ChunkSize:          2,
		Resume:             true,
		UnloggedTables:     false,
		PreserveDefaults:   true,
		OnSchemaExists:     "error",
		SourceSnapshotMode: "none",
		IdentifierCase:     "snake",
		Validation:         "none",
		TypeMapping:        defaultTypeMappingConfig(),
		configDir:          tmpDir,
	}
	schema, _, err = filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filter schema: %v", err)
	}
	typeMap := effectiveTypeMapping(cfg)
	compat, err := buildCheckpointCompatibility(cfg, schema, src, dbName, typeMap)
	if err != nil {
		t.Fatalf("buildCheckpointCompatibility: %v", err)
	}

	if err := createTables(ctx, pgPool, schema, pgSchema, false, cfg.PreserveDefaults, typeMap, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	dataCfg := migrateDataConfig{
		Src:                 src,
		SrcDSN:              sqliteFile,
		Pool:                pgPool,
		Schema:              schema,
		PGSchema:            pgSchema,
		Workers:             1,
		TypeMap:             typeMap,
		SourceSnapshotMode:  "none",
		ChunkSize:           2,
		Resume:              true,
		ConfigDir:           tmpDir,
		ResumeCompatibility: compat,
	}

	err = migrateData(ctx, dataCfg)
	if err == nil {
		t.Fatal("expected migrateData to fail on invalid timestamp chunk")
	}
	low := strings.ToLower(err.Error())
	if !strings.Contains(low, "timestamp") && !strings.Contains(low, "invalid") && !strings.Contains(low, "parse") && !strings.Contains(low, "date/time") && !strings.Contains(low, "datetime") {
		t.Fatalf("unexpected migrateData error (want timestamp/parse failure): %v", err)
	}

	assertRowCount(t, pgPool, pgSchema, "events", 2)

	cpPath := checkpointPath(tmpDir)
	state, err := loadCheckpoint(cpPath)
	if err != nil {
		t.Fatalf("loadCheckpoint: %v", err)
	}
	if state == nil || state.Tables["events"] == nil {
		t.Fatalf("checkpoint state = %+v, want events progress", state)
	}
	if !state.isChunkCompleted("events", 0) {
		t.Fatalf("checkpoint should mark first chunk completed: %+v", state.Tables["events"])
	}
	if state.isChunkCompleted("events", 1) {
		t.Fatalf("checkpoint should not mark failed chunk completed: %+v", state.Tables["events"])
	}

	fixDB, err := sql.Open("sqlite", sqliteFile)
	if err != nil {
		t.Fatalf("open sqlite for fix: %v", err)
	}
	if _, err := fixDB.Exec(`UPDATE events SET happened_at = '2024-01-04 04:05:06' WHERE id = 4`); err != nil {
		fixDB.Close()
		t.Fatalf("fix source row: %v", err)
	}
	fixDB.Close()

	if err := migrateData(ctx, dataCfg); err != nil {
		t.Fatalf("resume migrateData: %v", err)
	}
	if _, err := os.Stat(cpPath); !os.IsNotExist(err) {
		t.Fatalf("checkpoint file should be removed after successful resume, stat err=%v", err)
	}

	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	assertRowCount(t, pgPool, pgSchema, "events", 5)

	seqName := generatedSequenceName(Table{PGName: "events"}, Column{PGName: "id"})
	var (
		lastValue int64
		isCalled  bool
	)
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT last_value, is_called FROM %s", pgQualifiedIdent(pgSchema, seqName)),
	).Scan(&lastValue, &isCalled)
	if err != nil {
		t.Fatalf("query resumed sequence state: %v", err)
	}
	if lastValue != 6 || isCalled {
		t.Fatalf("sequence state after resume = last_value:%d is_called:%t, want last_value:6 is_called:false", lastValue, isCalled)
	}

	var nextID int64
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s.events (happened_at, note) VALUES ('2024-01-06 06:07:08', 'after-resume') RETURNING id", pgIdent(pgSchema)),
	).Scan(&nextID)
	if err != nil {
		t.Fatalf("insert after resume: %v", err)
	}
	if nextID != 6 {
		t.Fatalf("next inserted id after resume = %d, want 6", nextID)
	}
}

// --- Standalone pgferry validate (live source + target) ---

func TestIntegration_MySQL_ValidateStandaloneRowCount(t *testing.T) {
	mysqlDSN, pgDSN := requireMySQLAndPostgresDSNs(t)

	db, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer db.Close()
	seedMySQLNoOrphans(t, db)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_validate_standalone_mysql")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "mysql"
dsn = %q

[target]
dsn = %q
`, pgSchema, mysqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)
	runValidateFromConfig(t, cfgPath)
}

func TestIntegration_SQLite_ValidateStandaloneRowCount(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}
	tmpDir := t.TempDir()
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_validate_standalone_sqlite")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "sqlite"
dsn = %q

[target]
dsn = %q
`, pgSchema, sqliteFile, pgDSN))

	runMigrationFromConfig(t, cfgPath)
	runValidateFromConfig(t, cfgPath)
}

// --- MSSQL: single_tx, validation, schema/data split ---

func TestIntegration_MSSQL_SingleTx(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)
	ctx := context.Background()

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_single_tx")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
workers = 4
chunk_size = 2
source_snapshot_mode = "single_tx"

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 3)
	assertRowCount(t, pgPool, pgSchema, "orders", 3)
	assertFKExists(t, pgPool, pgSchema, "orders", "users")

	var displayName string
	err = pgPool.QueryRow(ctx, fmt.Sprintf("SELECT display_name FROM %s.users WHERE user_id = 1", pgIdent(pgSchema))).Scan(&displayName)
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if displayName != "Alice" {
		t.Fatalf("display_name = %q, want Alice", displayName)
	}
}

func TestIntegration_MSSQL_ValidationRowCount(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_validate_rows")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 3)
	assertRowCount(t, pgPool, pgSchema, "orders", 3)
}

func TestIntegration_MSSQL_ValidationSampledHash(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_validate_hash")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "sampled_hash"

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 3)
	assertRowCount(t, pgPool, pgSchema, "orders", 3)
}

func TestIntegration_MSSQL_SchemaOnly(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_schema_only")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
schema_only = true

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 0)
	assertRowCount(t, pgPool, pgSchema, "orders", 0)

	for _, tbl := range []string{"users", "orders", "exact_numerics", "special_types"} {
		assertPKExists(t, pgPool, pgSchema, tbl)
	}
	assertFKExists(t, pgPool, pgSchema, "orders", "users")

	// Skip INSERT probe: sales.Users carries computed columns that are awkward to
	// populate manually; empty-table + DDL assertions are enough here.
}

func TestIntegration_MSSQL_SchemaOnlyThenDataOnly(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)
	ctx := context.Background()

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_split")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	schemaOnlyCfg := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
schema_only = true

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, schemaOnlyCfg)

	assertRowCount(t, pgPool, pgSchema, "users", 0)

	dataOnlyCfg := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
data_only = true

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, dataOnlyCfg)

	assertRowCount(t, pgPool, pgSchema, "users", 3)
	assertRowCount(t, pgPool, pgSchema, "orders", 3)
	assertFKExists(t, pgPool, pgSchema, "orders", "users")

	var insertedID int64
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s.users (external_id, display_name, display_name_upper, email, is_active, version_token) VALUES ($1, $2, $3, $4, $5, $6) RETURNING user_id", pgIdent(pgSchema)),
		"22222222-3333-4444-5555-666666666666",
		"MSSQLSplitProbe",
		"MSSQLSPLITPROBE",
		"mssql-split@example.com",
		true,
		[]byte{0, 0, 0, 0, 0, 0, 0, 5},
	).Scan(&insertedID)
	if err != nil {
		t.Fatalf("insert MSSQL split-mode user after data_only: %v", err)
	}
	if insertedID != 4 {
		t.Fatalf("inserted user_id after MSSQL split-mode data_only reset = %d, want 4", insertedID)
	}
}

func TestIntegration_MSSQL_DataOnly_PrecreatedSchema(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)
	ctx := context.Background()

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	src, schema := introspectMSSQLSchemaForTest(t, mssqlDSN)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_data_only")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	preCfg := &MigrationConfig{
		Schema:           pgSchema,
		SchemaOnly:       true,
		PreserveDefaults: true,
		CleanOrphans:     true,
		TypeMapping:      defaultTypeMappingConfig(),
	}
	preCfg.TypeMapping.SpatialMode = "wkt_text"
	if err := createTables(ctx, pgPool, schema, pgSchema, false, preCfg.PreserveDefaults, preCfg.TypeMapping, src); err != nil {
		t.Fatalf("precreate tables: %v", err)
	}
	if err := postMigrate(ctx, pgPool, schema, preCfg); err != nil {
		t.Fatalf("precreate post-migrate: %v", err)
	}

	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE TABLE %s.manual_marker (note text)", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create marker table: %v", err)
	}
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("INSERT INTO %s.manual_marker (note) VALUES ('keep-me')", pgIdent(pgSchema))); err != nil {
		t.Fatalf("insert marker row: %v", err)
	}

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
data_only = true

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)

	assertRowCount(t, pgPool, pgSchema, "users", 3)

	var insertedID int64
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s.users (external_id, display_name, display_name_upper, email, is_active, version_token) VALUES ($1, $2, $3, $4, $5, $6) RETURNING user_id", pgIdent(pgSchema)),
		"33333333-4444-5555-6666-777777777777",
		"MSSQLPrecreatedProbe",
		"MSSQLPRECREATEDPROBE",
		"mssql-precreated@example.com",
		true,
		[]byte{0, 0, 0, 0, 0, 0, 0, 6},
	).Scan(&insertedID)
	if err != nil {
		t.Fatalf("insert MSSQL precreated-schema user after data_only: %v", err)
	}
	if insertedID != 4 {
		t.Fatalf("inserted user_id after MSSQL precreated-schema data_only reset = %d, want 4", insertedID)
	}

	var markerCount int
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s.manual_marker", pgIdent(pgSchema)),
	).Scan(&markerCount)
	if err != nil {
		t.Fatalf("count marker rows: %v", err)
	}
	if markerCount != 1 {
		t.Errorf("manual_marker row count: got %d, want 1", markerCount)
	}
}

func TestIntegration_MSSQL_ValidateStandaloneRowCount(t *testing.T) {
	mssqlDSN, pgDSN := requireMSSQLAndPostgresDSNs(t)

	mssqlDB, err := sql.Open("sqlserver", mssqlDSN)
	if err != nil {
		t.Fatalf("open mssql: %v", err)
	}
	defer mssqlDB.Close()
	seedMSSQL(t, mssqlDB)

	pgPool := openIntegrationPGPool(t, pgDSN)
	t.Cleanup(func() { pgPool.Close() })

	pgSchema := integrationSchemaName("inttest_mssql_validate_standalone")
	ensureDroppedSchema(t, pgPool, pgSchema)
	t.Cleanup(func() { dropSchema(t, pgPool, pgSchema) })

	tmpDir := t.TempDir()
	cfgPath := writeIntegrationConfig(t, tmpDir, fmt.Sprintf(`schema = %q
validation = "row_count"

[source]
type = "mssql"
dsn = %q
source_schema = "sales"

[type_mapping]
spatial_mode = "wkt_text"

[target]
dsn = %q
`, pgSchema, mssqlDSN, pgDSN))

	runMigrationFromConfig(t, cfgPath)
	runValidateFromConfig(t, cfgPath)
}
