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

func TestIntegration_MySQL(t *testing.T) {
	mysqlDSN := os.Getenv("MYSQL_DSN")
	pgDSN := os.Getenv("POSTGRES_DSN")
	if mysqlDSN == "" || pgDSN == "" {
		t.Skip("MYSQL_DSN and POSTGRES_DSN env vars required")
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
	src := &mysqlSourceDB{}
	mysqlDB2, err := src.OpenDB(mysqlDSN)
	if err != nil {
		t.Fatalf("open mysql for introspection: %v", err)
	}
	defer mysqlDB2.Close()
	mysqlDB2.SetMaxOpenConns(1)

	dbName, err := src.ExtractDBName(mysqlDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}

	schema, err := src.IntrospectSchema(mysqlDB2, dbName)
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

[source]
type = "mysql"
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
	if err := createTables(ctx, pgPool, schema, pgSchema, cfg.UnloggedTables, cfg.PreserveDefaults, cfg.TypeMapping, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.BeforeData, "before_data"); err != nil {
		t.Fatalf("before_data hooks: %v", err)
	}

	if err := migrateData(ctx, src, mysqlDSN, pgPool, schema, pgSchema, cfg.Workers, cfg.TypeMapping, cfg.SourceSnapshotMode); err != nil {
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
		t.Skip("MYSQL_DSN and POSTGRES_DSN env vars required")
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

	src := &mysqlSourceDB{}
	roMySQL, err := src.OpenDB(roDSN)
	if err != nil {
		t.Fatalf("open mysql readonly connection: %v", err)
	}
	defer roMySQL.Close()

	_, err = roMySQL.ExecContext(ctx, "INSERT INTO users (name, email) VALUES ('ReadOnlyProbe', NULL)")
	if err == nil {
		t.Fatal("expected INSERT to fail for read-only MySQL user")
	}

	schema, err := src.IntrospectSchema(roMySQL, dbName)
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
		Schema:             pgSchema,
		OnSchemaExists:     "error",
		SourceSnapshotMode: "none",
		UnloggedTables:     false,
		Workers:            2,
		TypeMapping:        defaultTypeMappingConfig(),
		Hooks: HooksConfig{
			BeforeFk: []string{"testdata/cleanup.sql"},
		},
	}

	if err := createTables(ctx, pgPool, schema, pgSchema, cfg.UnloggedTables, cfg.PreserveDefaults, cfg.TypeMapping, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := migrateData(ctx, src, roDSN, pgPool, schema, pgSchema, cfg.Workers, cfg.TypeMapping, cfg.SourceSnapshotMode); err != nil {
		t.Fatalf("migrateData with readonly user: %v", err)
	}
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)
}

func TestIntegration_SQLite(t *testing.T) {
	pgDSN := os.Getenv("POSTGRES_DSN")
	if pgDSN == "" {
		t.Skip("POSTGRES_DSN env var required")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create and seed SQLite database
	sqliteFile := filepath.Join(tmpDir, "test.db")
	seedSQLite(t, sqliteFile)

	src := &sqliteSourceDB{}
	sqliteDB, err := src.OpenDB(sqliteFile)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer sqliteDB.Close()

	dbName, err := src.ExtractDBName(sqliteFile)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}
	t.Logf("SQLite db name: %s", dbName)

	schema, err := src.IntrospectSchema(sqliteDB, dbName)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	sqliteDB.Close()

	if len(schema.Tables) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(schema.Tables))
	}

	// --- Prepare PG ---
	pgPool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	defer pgPool.Close()

	const pgSchema = "inttest_sqlite"

	_, _ = pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		pgPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	})

	tomlContent := fmt.Sprintf(`schema = %q
workers = 1

[source]
type = "sqlite"
dsn = %q

[postgres]
dsn = %q
`, pgSchema, sqliteFile, pgDSN)

	cfgPath := filepath.Join(tmpDir, "migration.toml")
	if err := os.WriteFile(cfgPath, []byte(tomlContent), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if err := createTables(ctx, pgPool, schema, pgSchema, cfg.UnloggedTables, cfg.PreserveDefaults, cfg.TypeMapping, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	if err := migrateData(ctx, src, sqliteFile, pgPool, schema, pgSchema, cfg.Workers, cfg.TypeMapping, cfg.SourceSnapshotMode); err != nil {
		t.Fatalf("migrateData: %v", err)
	}

	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	// --- Assertions ---
	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)

	for _, tbl := range []string{"users", "posts", "comments"} {
		assertPKExists(t, pgPool, pgSchema, tbl)
	}

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

func seedSQLite(t *testing.T, dbPath string) {
	t.Helper()

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite for seeding: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT
		)`,
		`CREATE TABLE posts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			body TEXT,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE TABLE comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			content TEXT,
			FOREIGN KEY (post_id) REFERENCES posts(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,

		"INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')",
		"INSERT INTO users (name, email) VALUES ('Bob', NULL)",
		"INSERT INTO users (name, email) VALUES ('Charlie', 'charlie@example.com')",
		"INSERT INTO users (name, email) VALUES ('Diana', 'diana@example.com')",
		"INSERT INTO users (name, email) VALUES ('Eve', NULL)",

		"INSERT INTO posts (user_id, title, body) VALUES (1, 'First Post', 'Hello world')",
		"INSERT INTO posts (user_id, title, body) VALUES (2, 'Bobs Post', 'Content here')",
		"INSERT INTO posts (user_id, title, body) VALUES (3, 'Thoughts', 'Some thoughts')",
		"INSERT INTO posts (user_id, title, body) VALUES (4, 'Update', NULL)",
		"INSERT INTO posts (user_id, title, body) VALUES (5, 'Hello', 'Eve here')",

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
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed sqlite %q: %v", stmt[:min(len(stmt), 60)], err)
		}
	}
}

// --- MySQL seed helpers ---

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

		"INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')",
		"INSERT INTO users (name, email) VALUES ('Bob', NULL)",
		"INSERT INTO users (name, email) VALUES ('Charlie', 'charlie@example.com')",
		"INSERT INTO users (name, email) VALUES ('Diana', 'diana@example.com')",
		"INSERT INTO users (name, email) VALUES ('Eve', NULL)",

		"INSERT INTO posts (user_id, title, body) VALUES (1, 'First Post', 'Hello world')",
		"INSERT INTO posts (user_id, title, body) VALUES (2, 'Bobs Post', 'Content here')",
		"INSERT INTO posts (user_id, title, body) VALUES (3, 'Thoughts', 'Some thoughts')",
		"INSERT INTO posts (user_id, title, body) VALUES (4, 'Update', NULL)",
		"INSERT INTO posts (user_id, title, body) VALUES (5, 'Hello', 'Eve here')",

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

func seedSakila(t *testing.T, db *sql.DB) {
	t.Helper()

	stmts := []string{
		"SET FOREIGN_KEY_CHECKS=0",

		// Drop in reverse dependency order
		"DROP TABLE IF EXISTS payment",
		"DROP TABLE IF EXISTS rental",
		"DROP TABLE IF EXISTS inventory",
		"DROP TABLE IF EXISTS film_text",
		"DROP TABLE IF EXISTS film_category",
		"DROP TABLE IF EXISTS film_actor",
		"DROP TABLE IF EXISTS customer",
		"DROP TABLE IF EXISTS store",
		"DROP TABLE IF EXISTS staff",
		"DROP TABLE IF EXISTS film",
		"DROP TABLE IF EXISTS language",
		"DROP TABLE IF EXISTS category",
		"DROP TABLE IF EXISTS address",
		"DROP TABLE IF EXISTS city",
		"DROP TABLE IF EXISTS country",
		"DROP TABLE IF EXISTS actor",

		// --- DDL ---
		`CREATE TABLE actor (
			actor_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			first_name VARCHAR(45) NOT NULL,
			last_name VARCHAR(45) NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (actor_id),
			KEY idx_actor_last_name (last_name)
		)`,

		`CREATE TABLE country (
			country_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			country VARCHAR(50) NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (country_id)
		)`,

		`CREATE TABLE city (
			city_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			city VARCHAR(50) NOT NULL,
			country_id SMALLINT UNSIGNED NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (city_id),
			KEY idx_fk_country_id (country_id),
			CONSTRAINT fk_city_country FOREIGN KEY (country_id) REFERENCES country (country_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE address (
			address_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			address VARCHAR(50) NOT NULL,
			address2 VARCHAR(50) NULL DEFAULT NULL,
			district VARCHAR(20) NOT NULL,
			city_id SMALLINT UNSIGNED NOT NULL,
			postal_code VARCHAR(10) NULL DEFAULT NULL,
			phone VARCHAR(20) NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (address_id),
			KEY idx_fk_city_id (city_id),
			CONSTRAINT fk_address_city FOREIGN KEY (city_id) REFERENCES city (city_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE category (
			category_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
			name VARCHAR(25) NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (category_id)
		)`,

		`CREATE TABLE language (
			language_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
			name CHAR(20) NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (language_id)
		)`,

		`CREATE TABLE film (
			film_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			title VARCHAR(128) NOT NULL,
			description TEXT NULL DEFAULT NULL,
			release_year YEAR NULL DEFAULT NULL,
			language_id TINYINT UNSIGNED NOT NULL,
			original_language_id TINYINT UNSIGNED NULL DEFAULT NULL,
			rental_duration TINYINT UNSIGNED NOT NULL DEFAULT 3,
			rental_rate DECIMAL(4,2) NOT NULL DEFAULT 4.99,
			length SMALLINT UNSIGNED NULL DEFAULT NULL,
			replacement_cost DECIMAL(5,2) NOT NULL DEFAULT 19.99,
			rating ENUM('G','PG','PG-13','R','NC-17') NULL DEFAULT 'G',
			special_features SET('Trailers','Commentaries','Deleted Scenes','Behind the Scenes') NULL DEFAULT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (film_id),
			KEY idx_title (title(10)),
			KEY idx_fk_language_id (language_id),
			KEY idx_fk_original_language_id (original_language_id),
			CONSTRAINT fk_film_language FOREIGN KEY (language_id) REFERENCES language (language_id) ON UPDATE CASCADE,
			CONSTRAINT fk_film_language_original FOREIGN KEY (original_language_id) REFERENCES language (language_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE film_actor (
			actor_id SMALLINT UNSIGNED NOT NULL,
			film_id SMALLINT UNSIGNED NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (actor_id, film_id),
			KEY idx_fk_film_id (film_id),
			CONSTRAINT fk_film_actor_actor FOREIGN KEY (actor_id) REFERENCES actor (actor_id) ON UPDATE CASCADE,
			CONSTRAINT fk_film_actor_film FOREIGN KEY (film_id) REFERENCES film (film_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE film_category (
			film_id SMALLINT UNSIGNED NOT NULL,
			category_id TINYINT UNSIGNED NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (film_id, category_id),
			CONSTRAINT fk_film_category_film FOREIGN KEY (film_id) REFERENCES film (film_id) ON UPDATE CASCADE,
			CONSTRAINT fk_film_category_category FOREIGN KEY (category_id) REFERENCES category (category_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE film_text (
			film_id INT NOT NULL,
			title VARCHAR(255) NOT NULL,
			description TEXT NULL DEFAULT NULL,
			PRIMARY KEY (film_id),
			FULLTEXT KEY idx_title_description (title, description)
		)`,

		`CREATE TABLE staff (
			staff_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
			first_name VARCHAR(45) NOT NULL,
			last_name VARCHAR(45) NOT NULL,
			address_id SMALLINT UNSIGNED NOT NULL,
			picture MEDIUMBLOB NULL DEFAULT NULL,
			email VARCHAR(50) NULL DEFAULT NULL,
			store_id TINYINT UNSIGNED NOT NULL,
			active TINYINT(1) NOT NULL DEFAULT 1,
			username VARCHAR(16) NOT NULL,
			password VARCHAR(40) NULL DEFAULT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (staff_id),
			KEY idx_fk_store_id (store_id),
			KEY idx_fk_address_id (address_id),
			CONSTRAINT fk_staff_address FOREIGN KEY (address_id) REFERENCES address (address_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE store (
			store_id TINYINT UNSIGNED NOT NULL AUTO_INCREMENT,
			manager_staff_id TINYINT UNSIGNED NOT NULL,
			address_id SMALLINT UNSIGNED NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (store_id),
			UNIQUE KEY idx_unique_manager (manager_staff_id),
			KEY idx_fk_address_id2 (address_id),
			CONSTRAINT fk_store_staff FOREIGN KEY (manager_staff_id) REFERENCES staff (staff_id) ON UPDATE CASCADE,
			CONSTRAINT fk_store_address FOREIGN KEY (address_id) REFERENCES address (address_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE customer (
			customer_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			store_id TINYINT UNSIGNED NOT NULL,
			first_name VARCHAR(45) NOT NULL,
			last_name VARCHAR(45) NOT NULL,
			email VARCHAR(50) NULL DEFAULT NULL,
			address_id SMALLINT UNSIGNED NOT NULL,
			active TINYINT(1) NOT NULL DEFAULT 1,
			create_date DATETIME NOT NULL,
			last_update TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (customer_id),
			KEY idx_fk_store_id (store_id),
			KEY idx_fk_address_id (address_id),
			KEY idx_last_name (last_name),
			CONSTRAINT fk_customer_address FOREIGN KEY (address_id) REFERENCES address (address_id) ON UPDATE CASCADE,
			CONSTRAINT fk_customer_store FOREIGN KEY (store_id) REFERENCES store (store_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE inventory (
			inventory_id MEDIUMINT UNSIGNED NOT NULL AUTO_INCREMENT,
			film_id SMALLINT UNSIGNED NOT NULL,
			store_id TINYINT UNSIGNED NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (inventory_id),
			KEY idx_fk_film_id (film_id),
			KEY idx_store_id_film_id (store_id, film_id),
			CONSTRAINT fk_inventory_store FOREIGN KEY (store_id) REFERENCES store (store_id) ON UPDATE CASCADE,
			CONSTRAINT fk_inventory_film FOREIGN KEY (film_id) REFERENCES film (film_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE rental (
			rental_id INT NOT NULL AUTO_INCREMENT,
			rental_date DATETIME NOT NULL,
			inventory_id MEDIUMINT UNSIGNED NOT NULL,
			customer_id SMALLINT UNSIGNED NOT NULL,
			return_date DATETIME NULL DEFAULT NULL,
			staff_id TINYINT UNSIGNED NOT NULL,
			last_update TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (rental_id),
			UNIQUE KEY idx_rental_uq (rental_date, inventory_id, customer_id),
			KEY idx_fk_inventory_id (inventory_id),
			KEY idx_fk_customer_id (customer_id),
			KEY idx_fk_staff_id (staff_id),
			CONSTRAINT fk_rental_staff FOREIGN KEY (staff_id) REFERENCES staff (staff_id) ON UPDATE CASCADE,
			CONSTRAINT fk_rental_inventory FOREIGN KEY (inventory_id) REFERENCES inventory (inventory_id) ON UPDATE CASCADE,
			CONSTRAINT fk_rental_customer FOREIGN KEY (customer_id) REFERENCES customer (customer_id) ON UPDATE CASCADE
		)`,

		`CREATE TABLE payment (
			payment_id SMALLINT UNSIGNED NOT NULL AUTO_INCREMENT,
			customer_id SMALLINT UNSIGNED NOT NULL,
			staff_id TINYINT UNSIGNED NOT NULL,
			rental_id INT NULL DEFAULT NULL,
			amount DECIMAL(5,2) NOT NULL,
			payment_date DATETIME NOT NULL,
			last_update TIMESTAMP NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (payment_id),
			KEY idx_fk_staff_id (staff_id),
			KEY idx_fk_customer_id (customer_id),
			KEY fk_payment_rental (rental_id),
			CONSTRAINT fk_payment_rental FOREIGN KEY (rental_id) REFERENCES rental (rental_id) ON DELETE SET NULL ON UPDATE CASCADE,
			CONSTRAINT fk_payment_customer FOREIGN KEY (customer_id) REFERENCES customer (customer_id) ON UPDATE CASCADE,
			CONSTRAINT fk_payment_staff FOREIGN KEY (staff_id) REFERENCES staff (staff_id) ON UPDATE CASCADE
		)`,

		// --- Seed data (FK-safe order, FK_CHECKS=0 handles circular staff↔store) ---

		// country
		"INSERT INTO country (country_id, country) VALUES (1, 'United States')",
		"INSERT INTO country (country_id, country) VALUES (2, 'Canada')",

		// city
		"INSERT INTO city (city_id, city, country_id) VALUES (1, 'San Francisco', 1)",
		"INSERT INTO city (city_id, city, country_id) VALUES (2, 'Toronto', 2)",

		// address
		"INSERT INTO address (address_id, address, district, city_id, postal_code, phone) VALUES (1, '123 Main St', 'California', 1, '94102', '5551234567')",
		"INSERT INTO address (address_id, address, district, city_id, postal_code, phone) VALUES (2, '456 Queen St', 'Ontario', 2, 'M5V2A8', '4161234567')",
		"INSERT INTO address (address_id, address, district, city_id, postal_code, phone) VALUES (3, '789 Market St', 'California', 1, '94103', '5559876543')",

		// language
		"INSERT INTO language (language_id, name) VALUES (1, 'English')",

		// category
		"INSERT INTO category (category_id, name) VALUES (1, 'Action')",
		"INSERT INTO category (category_id, name) VALUES (2, 'Comedy')",

		// actor
		"INSERT INTO actor (actor_id, first_name, last_name) VALUES (1, 'PENELOPE', 'GUINESS')",
		"INSERT INTO actor (actor_id, first_name, last_name) VALUES (2, 'NICK', 'WAHLBERG')",
		"INSERT INTO actor (actor_id, first_name, last_name) VALUES (3, 'ED', 'CHASE')",

		// staff (circular FK with store — FK_CHECKS=0 handles it)
		"INSERT INTO staff (staff_id, first_name, last_name, address_id, picture, email, store_id, active, username, password) VALUES (1, 'Mike', 'Hillyer', 1, NULL, 'mike@sakilastaff.com', 1, 1, 'Mike', NULL)",
		"INSERT INTO staff (staff_id, first_name, last_name, address_id, picture, email, store_id, active, username, password) VALUES (2, 'Jon', 'Stephens', 2, X'89504E470D0A1A0A', 'jon@sakilastaff.com', 1, 1, 'Jon', NULL)",

		// store
		"INSERT INTO store (store_id, manager_staff_id, address_id) VALUES (1, 1, 1)",

		// film
		"INSERT INTO film (film_id, title, description, release_year, language_id, rental_duration, rental_rate, length, replacement_cost, rating, special_features) VALUES (1, 'ACADEMY DINOSAUR', 'An epic drama', 2006, 1, 6, 0.99, 86, 20.99, 'PG', 'Deleted Scenes,Behind the Scenes')",
		"INSERT INTO film (film_id, title, description, release_year, language_id, rental_duration, rental_rate, length, replacement_cost, rating, special_features) VALUES (2, 'ACE GOLDFINGER', 'A stunning epistle', 2006, 1, 3, 4.99, 48, 12.99, 'G', 'Trailers')",
		"INSERT INTO film (film_id, title, description, release_year, language_id, rental_duration, rental_rate, length, replacement_cost, rating, special_features) VALUES (3, 'ADAPTATION HOLES', 'An astounding drama', 2006, 1, 7, 2.99, 50, 18.99, 'NC-17', 'Trailers,Deleted Scenes')",

		// customer
		"INSERT INTO customer (customer_id, store_id, first_name, last_name, email, address_id, active, create_date) VALUES (1, 1, 'MARY', 'SMITH', 'mary.smith@sakilacustomer.org', 1, 1, '2006-02-14 22:04:36')",
		"INSERT INTO customer (customer_id, store_id, first_name, last_name, email, address_id, active, create_date) VALUES (2, 1, 'PATRICIA', 'JOHNSON', 'patricia.johnson@sakilacustomer.org', 2, 1, '2006-02-14 22:04:37')",
		"INSERT INTO customer (customer_id, store_id, first_name, last_name, email, address_id, active, create_date) VALUES (3, 1, 'LINDA', 'WILLIAMS', 'linda.williams@sakilacustomer.org', 3, 0, '2006-02-14 22:04:37')",

		// film_actor
		"INSERT INTO film_actor (actor_id, film_id) VALUES (1, 1)",
		"INSERT INTO film_actor (actor_id, film_id) VALUES (1, 2)",
		"INSERT INTO film_actor (actor_id, film_id) VALUES (2, 1)",
		"INSERT INTO film_actor (actor_id, film_id) VALUES (3, 3)",

		// film_category
		"INSERT INTO film_category (film_id, category_id) VALUES (1, 1)",
		"INSERT INTO film_category (film_id, category_id) VALUES (2, 1)",
		"INSERT INTO film_category (film_id, category_id) VALUES (3, 2)",

		// film_text
		"INSERT INTO film_text (film_id, title, description) VALUES (1, 'ACADEMY DINOSAUR', 'An epic drama')",
		"INSERT INTO film_text (film_id, title, description) VALUES (2, 'ACE GOLDFINGER', 'A stunning epistle')",
		"INSERT INTO film_text (film_id, title, description) VALUES (3, 'ADAPTATION HOLES', 'An astounding drama')",

		// inventory
		"INSERT INTO inventory (inventory_id, film_id, store_id) VALUES (1, 1, 1)",
		"INSERT INTO inventory (inventory_id, film_id, store_id) VALUES (2, 1, 1)",
		"INSERT INTO inventory (inventory_id, film_id, store_id) VALUES (3, 2, 1)",
		"INSERT INTO inventory (inventory_id, film_id, store_id) VALUES (4, 3, 1)",

		// rental
		"INSERT INTO rental (rental_id, rental_date, inventory_id, customer_id, return_date, staff_id) VALUES (1, '2005-05-24 22:54:33', 1, 1, '2005-05-26 22:04:30', 1)",
		"INSERT INTO rental (rental_id, rental_date, inventory_id, customer_id, return_date, staff_id) VALUES (2, '2005-05-24 23:03:39', 2, 2, '2005-05-28 19:40:33', 1)",
		"INSERT INTO rental (rental_id, rental_date, inventory_id, customer_id, return_date, staff_id) VALUES (3, '2005-05-25 00:00:00', 3, 3, NULL, 2)",

		// payment
		"INSERT INTO payment (payment_id, customer_id, staff_id, rental_id, amount, payment_date) VALUES (1, 1, 1, 1, 2.99, '2005-05-25 11:30:37')",
		"INSERT INTO payment (payment_id, customer_id, staff_id, rental_id, amount, payment_date) VALUES (2, 2, 1, 2, 4.99, '2005-05-25 11:30:37')",
		"INSERT INTO payment (payment_id, customer_id, staff_id, rental_id, amount, payment_date) VALUES (3, 3, 2, 3, 0.99, '2005-05-25 11:30:37')",

		"SET FOREIGN_KEY_CHECKS=1",
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed sakila %q: %v", stmt[:min(len(stmt), 80)], err)
		}
	}
}

func TestIntegration_MySQLSakila(t *testing.T) {
	mysqlDSN := os.Getenv("MYSQL_DSN")
	pgDSN := os.Getenv("POSTGRES_DSN")
	if mysqlDSN == "" || pgDSN == "" {
		t.Skip("MYSQL_DSN and POSTGRES_DSN env vars required")
	}

	ctx := context.Background()

	// --- Seed MySQL ---
	mysqlDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer mysqlDB.Close()

	seedSakila(t, mysqlDB)
	mysqlDB.Close()

	// --- Introspect ---
	src := &mysqlSourceDB{}
	mysqlDB2, err := src.OpenDB(mysqlDSN)
	if err != nil {
		t.Fatalf("open mysql for introspection: %v", err)
	}
	defer mysqlDB2.Close()
	mysqlDB2.SetMaxOpenConns(1)

	dbName, err := src.ExtractDBName(mysqlDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}

	schema, err := src.IntrospectSchema(mysqlDB2, dbName)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	mysqlDB2.Close()

	if len(schema.Tables) != 16 {
		var names []string
		for _, tbl := range schema.Tables {
			names = append(names, tbl.SourceName)
		}
		t.Fatalf("expected 16 tables, got %d: %v", len(schema.Tables), names)
	}

	// --- Prepare PG ---
	pgPool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	defer pgPool.Close()

	const pgSchema = "inttest_sakila"

	_, _ = pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		pgPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	})

	// --- Write temp config ---
	tmpDir := t.TempDir()

	tomlContent := fmt.Sprintf(`schema = %q
workers = 4

[source]
type = "mysql"
dsn = %q

[postgres]
dsn = %q

[type_mapping]
tinyint1_as_boolean = true
enum_mode = "check"
set_mode = "text_array"
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
	if err := createTables(ctx, pgPool, schema, pgSchema, cfg.UnloggedTables, cfg.PreserveDefaults, cfg.TypeMapping, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	if err := migrateData(ctx, src, mysqlDSN, pgPool, schema, pgSchema, cfg.Workers, cfg.TypeMapping, cfg.SourceSnapshotMode); err != nil {
		t.Fatalf("migrateData: %v", err)
	}

	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	// --- Row count assertions ---
	rowCounts := map[string]int{
		"actor": 3, "country": 2, "city": 2, "address": 3,
		"category": 2, "language": 1, "film": 3, "film_actor": 4,
		"film_category": 3, "film_text": 3, "staff": 2, "store": 1,
		"customer": 3, "inventory": 4, "rental": 3, "payment": 3,
	}
	for tbl, want := range rowCounts {
		assertRowCount(t, pgPool, pgSchema, tbl, want)
	}

	// --- Primary keys on all 16 tables ---
	for tbl := range rowCounts {
		assertPKExists(t, pgPool, pgSchema, tbl)
	}

	// --- Foreign keys ---
	fks := [][2]string{
		{"film_actor", "film"},
		{"film_actor", "actor"},
		{"film_category", "film"},
		{"film_category", "category"},
		{"rental", "customer"},
		{"rental", "inventory"},
		{"rental", "staff"},
		{"payment", "rental"},
		{"payment", "customer"},
		{"payment", "staff"},
		{"city", "country"},
		{"inventory", "film"},
		{"inventory", "store"},
		{"customer", "store"},
		{"customer", "address"},
		{"film", "language"},
		{"address", "city"},
		{"staff", "address"},
		{"store", "staff"},
		{"store", "address"},
	}
	for _, fk := range fks {
		assertFKExists(t, pgPool, pgSchema, fk[0], fk[1])
	}

	// --- Type mapping assertions ---
	assertColumnType(t, pgPool, pgSchema, "film", "rating", "text")
	assertColumnType(t, pgPool, pgSchema, "film", "special_features", "ARRAY")
	assertColumnType(t, pgPool, pgSchema, "film", "rental_rate", "numeric")
	assertColumnType(t, pgPool, pgSchema, "film", "release_year", "integer")
	assertColumnType(t, pgPool, pgSchema, "customer", "active", "boolean")
	assertColumnType(t, pgPool, pgSchema, "staff", "active", "boolean")
	assertColumnType(t, pgPool, pgSchema, "staff", "picture", "bytea")
	assertColumnType(t, pgPool, pgSchema, "language", "name", "character varying")

	// --- CHECK constraint on enum ---
	assertCheckExists(t, pgPool, pgSchema, "film", "rating")

	// --- Data spot-checks ---

	// DECIMAL roundtrip
	var rentalRate string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT rental_rate::text FROM %s.film WHERE film_id = 1", pgIdent(pgSchema)),
	).Scan(&rentalRate)
	if err != nil {
		t.Fatalf("spot-check rental_rate: %v", err)
	}
	if rentalRate != "0.99" {
		t.Errorf("film 1 rental_rate: got %q, want %q", rentalRate, "0.99")
	}

	// ENUM value readable as text
	var rating string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT rating FROM %s.film WHERE film_id = 3", pgIdent(pgSchema)),
	).Scan(&rating)
	if err != nil {
		t.Fatalf("spot-check rating: %v", err)
	}
	if rating != "NC-17" {
		t.Errorf("film 3 rating: got %q, want %q", rating, "NC-17")
	}

	// SET value stored as text array
	var features []string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT special_features FROM %s.film WHERE film_id = 1", pgIdent(pgSchema)),
	).Scan(&features)
	if err != nil {
		t.Fatalf("spot-check special_features: %v", err)
	}
	if len(features) != 2 || features[0] != "Deleted Scenes" || features[1] != "Behind the Scenes" {
		t.Errorf("film 1 special_features: got %v, want [Deleted Scenes, Behind the Scenes]", features)
	}

	// Boolean roundtrip (active customer vs inactive)
	var active bool
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT active FROM %s.customer WHERE customer_id = 3", pgIdent(pgSchema)),
	).Scan(&active)
	if err != nil {
		t.Fatalf("spot-check customer active: %v", err)
	}
	if active != false {
		t.Errorf("customer 3 active: got %v, want false", active)
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

func assertColumnType(t *testing.T, pool *pgxpool.Pool, schema, table, column, wantType string) {
	t.Helper()
	var got string
	err := pool.QueryRow(context.Background(), `
		SELECT data_type FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2 AND column_name = $3
	`, schema, table, column).Scan(&got)
	if err != nil {
		t.Fatalf("check column type %s.%s.%s: %v", schema, table, column, err)
	}
	if got != wantType {
		t.Errorf("%s.%s.%s type: got %q, want %q", schema, table, column, got, wantType)
	}
}

func assertCheckExists(t *testing.T, pool *pgxpool.Pool, schema, table, substr string) {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*) FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		JOIN pg_class r ON r.oid = c.conrelid
		WHERE n.nspname = $1 AND r.relname = $2 AND c.contype = 'c'
		  AND pg_get_constraintdef(c.oid) ILIKE '%' || $3 || '%'
	`, schema, table, substr).Scan(&count)
	if err != nil {
		t.Fatalf("check CHECK constraint on %s.%s containing %q: %v", schema, table, substr, err)
	}
	if count == 0 {
		t.Errorf("no CHECK constraint on %s.%s containing %q", schema, table, substr)
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
