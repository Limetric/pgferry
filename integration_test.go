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
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegration(t *testing.T) {
	mysqlDSN := os.Getenv("MYSQL_DSN")
	pgDSN := os.Getenv("POSTGRES_DSN")
	if mysqlDSN == "" || pgDSN == "" {
		t.Fatal("MYSQL_DSN and POSTGRES_DSN env vars required")
	}

	ctx := context.Background()

	// --- Seed MySQL ---
	mysqlDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer mysqlDB.Close()

	seedMySQL(t, mysqlDB)

	// Close seeding connection; introspection needs its own
	mysqlDB.Close()

	// --- Introspect ---
	mysqlDB2, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true")
	if err != nil {
		t.Fatalf("open mysql for introspection: %v", err)
	}
	defer mysqlDB2.Close()
	mysqlDB2.SetMaxOpenConns(1)

	dbName, err := extractMySQLDBName(mysqlDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}

	schema, err := introspectSchema(mysqlDB2, dbName)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	mysqlDB2.Close()

	if len(schema.Tables) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(schema.Tables))
	}

	// --- Prepare PG ---
	pgPool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	defer pgPool.Close()

	const pgSchema = "inttest"

	_, _ = pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		pgPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	})

	// --- Write temp config ---
	tmpDir := t.TempDir()

	// Copy cleanup.sql into tmpDir so hook path resolution works
	cleanupSQL, err := os.ReadFile("testdata/cleanup.sql")
	if err != nil {
		t.Fatalf("read cleanup.sql: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "cleanup.sql"), cleanupSQL, 0644); err != nil {
		t.Fatalf("write cleanup.sql: %v", err)
	}

	tomlContent := fmt.Sprintf(`schema = %q
workers = 2

[mysql]
dsn = %q

[postgres]
dsn = %q

[hooks]
before_data = []
after_data = []
before_fk = ["cleanup.sql"]
after_all = []
`, pgSchema, mysqlDSN, pgDSN)

	cfgPath := filepath.Join(tmpDir, "migration.toml")
	if err := os.WriteFile(cfgPath, []byte(tomlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// --- Run pipeline ---
	if err := createTables(ctx, pgPool, schema, pgSchema, cfg.UnloggedTables); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.BeforeData, "before_data"); err != nil {
		t.Fatalf("before_data hooks: %v", err)
	}

	if err := migrateData(ctx, mysqlDSN, pgPool, schema, pgSchema, cfg.Workers); err != nil {
		t.Fatalf("migrateData: %v", err)
	}

	if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.AfterData, "after_data"); err != nil {
		t.Fatalf("after_data hooks: %v", err)
	}

	// postMigrate runs: SET LOGGED, PKs, indexes, before_fk hooks (cleanup), FKs, sequences, triggers
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	// --- Assertions ---

	// Row counts
	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10) // 2 orphans deleted by before_fk hook

	// Primary keys exist on each table
	for _, tbl := range []string{"users", "posts", "comments"} {
		assertPKExists(t, pgPool, pgSchema, tbl)
	}

	// Foreign keys
	assertFKExists(t, pgPool, pgSchema, "posts", "users")
	assertFKExists(t, pgPool, pgSchema, "comments", "posts")
	assertFKExists(t, pgPool, pgSchema, "comments", "users")

	// Spot-check data
	var name string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT name FROM %s.users WHERE id = 1", pgIdent(pgSchema)),
	).Scan(&name)
	if err != nil {
		t.Fatalf("spot-check query: %v", err)
	}
	if name != "Alice" {
		t.Errorf("expected user 1 name 'Alice', got %q", name)
	}
}

func TestIntegration_MySQLReadOnlyUser(t *testing.T) {
	mysqlDSN := os.Getenv("MYSQL_DSN")
	pgDSN := os.Getenv("POSTGRES_DSN")
	if mysqlDSN == "" || pgDSN == "" {
		t.Fatal("MYSQL_DSN and POSTGRES_DSN env vars required")
	}

	ctx := context.Background()

	adminMySQL, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql admin connection: %v", err)
	}
	defer adminMySQL.Close()

	seedMySQL(t, adminMySQL)

	dbName, err := extractMySQLDBName(mysqlDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}

	roUser := fmt.Sprintf("pgferry_ro_%d", time.Now().UnixNano())
	roPass := "pgferry_ro_pw"
	if err := createReadOnlyMySQLUser(ctx, adminMySQL, dbName, roUser, roPass); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "access denied") {
			t.Skipf("skipping read-only user test: insufficient MySQL privileges to create users (%v)", err)
		}
		t.Fatalf("create read-only user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminMySQL.ExecContext(context.Background(), fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", roUser))
	})

	roDSN, err := buildReadOnlyUserDSN(mysqlDSN, roUser, roPass)
	if err != nil {
		t.Fatalf("build readonly DSN: %v", err)
	}

	roQueryDSN, err := mysqlDSNWithReadOptions(roDSN)
	if err != nil {
		t.Fatalf("prepare readonly query DSN: %v", err)
	}

	roMySQL, err := sql.Open("mysql", roQueryDSN)
	if err != nil {
		t.Fatalf("open mysql readonly connection: %v", err)
	}
	defer roMySQL.Close()

	_, err = roMySQL.ExecContext(ctx, "INSERT INTO users (name, email) VALUES ('ReadOnlyProbe', NULL)")
	if err == nil {
		t.Fatal("expected INSERT to fail for read-only MySQL user")
	}

	schema, err := introspectSchema(roMySQL, dbName)
	if err != nil {
		t.Fatalf("introspect with readonly user: %v", err)
	}

	pgPool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	defer pgPool.Close()

	const pgSchema = "inttest_ro"
	_, _ = pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		pgPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	})

	cfg := &MigrationConfig{
		Schema:         pgSchema,
		OnSchemaExists: "error",
		UnloggedTables: false,
		Workers:        2,
		Hooks:          HooksConfig{},
	}

	if err := createTables(ctx, pgPool, schema, pgSchema, cfg.UnloggedTables); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := migrateData(ctx, roDSN, pgPool, schema, pgSchema, cfg.Workers); err != nil {
		t.Fatalf("migrateData with readonly user: %v", err)
	}
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 12)
}

// seedMySQL creates the test schema and inserts seed data.
func seedMySQL(t *testing.T, db *sql.DB) {
	t.Helper()

	stmts := []string{
		"DROP TABLE IF EXISTS comments",
		"DROP TABLE IF EXISTS posts",
		"DROP TABLE IF EXISTS users",

		`CREATE TABLE users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			email VARCHAR(200) NULL
		)`,
		`CREATE TABLE posts (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id INT NOT NULL,
			title VARCHAR(200) NOT NULL,
			body TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE TABLE comments (
			id INT AUTO_INCREMENT PRIMARY KEY,
			post_id INT NOT NULL,
			user_id INT NOT NULL,
			content TEXT,
			FOREIGN KEY (post_id) REFERENCES posts(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,

		// Users
		"INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')",
		"INSERT INTO users (name, email) VALUES ('Bob', NULL)",
		"INSERT INTO users (name, email) VALUES ('Charlie', 'charlie@example.com')",
		"INSERT INTO users (name, email) VALUES ('Diana', 'diana@example.com')",
		"INSERT INTO users (name, email) VALUES ('Eve', NULL)",

		// Posts (one per user)
		"INSERT INTO posts (user_id, title, body) VALUES (1, 'First Post', 'Hello world')",
		"INSERT INTO posts (user_id, title, body) VALUES (2, 'Bobs Post', 'Content here')",
		"INSERT INTO posts (user_id, title, body) VALUES (3, 'Thoughts', 'Some thoughts')",
		"INSERT INTO posts (user_id, title, body) VALUES (4, 'Update', NULL)",
		"INSERT INTO posts (user_id, title, body) VALUES (5, 'Hello', 'Eve here')",

		// Valid comments (10)
		"INSERT INTO comments (post_id, user_id, content) VALUES (1, 2, 'Nice post!')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (1, 3, 'Great read')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (2, 1, 'Thanks Bob')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (2, 4, 'Interesting')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (3, 5, 'I agree')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (3, 1, 'Me too')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (4, 2, 'Good update')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (4, 3, 'Thanks')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (5, 1, 'Welcome Eve')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (5, 4, 'Hi Eve!')",

		// Disable FK checks to insert orphan comments (post_id references non-existent posts)
		"SET FOREIGN_KEY_CHECKS=0",
		"INSERT INTO comments (post_id, user_id, content) VALUES (999, 1, 'Orphan 1')",
		"INSERT INTO comments (post_id, user_id, content) VALUES (998, 2, 'Orphan 2')",
		"SET FOREIGN_KEY_CHECKS=1",
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed mysql %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}
}

func createReadOnlyMySQLUser(ctx context.Context, db *sql.DB, dbName, user, password string) error {
	stmts := []string{
		fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", user),
		fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", user, password),
		fmt.Sprintf("GRANT SELECT, SHOW VIEW ON `%s`.* TO '%s'@'%%'", dbName, user),
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func buildReadOnlyUserDSN(baseDSN, user, password string) (string, error) {
	cfg, err := mysql.ParseDSN(baseDSN)
	if err != nil {
		return "", err
	}
	cfg.User = user
	cfg.Passwd = password
	return cfg.FormatDSN(), nil
}

func assertRowCount(t *testing.T, pool *pgxpool.Pool, schema, table string, want int) {
	t.Helper()
	var got int
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", pgIdent(schema), pgIdent(table))
	if err := pool.QueryRow(context.Background(), q).Scan(&got); err != nil {
		t.Fatalf("count %s.%s: %v", schema, table, err)
	}
	if got != want {
		t.Errorf("%s.%s row count: got %d, want %d", schema, table, got, want)
	}
}

func assertPKExists(t *testing.T, pool *pgxpool.Pool, schema, table string) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		JOIN pg_class r ON r.oid = c.conrelid
		WHERE n.nspname = $1 AND r.relname = $2 AND c.contype = 'p'
	`, schema, table).Scan(&count)
	if err != nil {
		t.Fatalf("check PK on %s.%s: %v", schema, table, err)
	}
	if count == 0 {
		t.Errorf("no primary key found on %s.%s", schema, table)
	}
}

func assertFKExists(t *testing.T, pool *pgxpool.Pool, schema, fromTable, toTable string) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		JOIN pg_class src ON src.oid = c.conrelid
		JOIN pg_class dst ON dst.oid = c.confrelid
		WHERE n.nspname = $1 AND src.relname = $2 AND dst.relname = $3 AND c.contype = 'f'
	`, schema, fromTable, toTable).Scan(&count)
	if err != nil {
		t.Fatalf("check FK %s.%s→%s: %v", schema, fromTable, toTable, err)
	}
	if count == 0 {
		t.Errorf("no foreign key from %s.%s → %s.%s", schema, fromTable, schema, toTable)
	}
}
