package main

import "testing"

func TestNewConfiguredSourceDB_MSSQLSourceSchema(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mssql"
	cfg.Source.SourceSchema = "sales"
	cfg.SnakeCaseIdentifiers = true

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	mssqlSrc, ok := src.(*mssqlSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *mssqlSourceDB", src)
	}

	if mssqlSrc.sourceSchema != "sales" {
		t.Fatalf("sourceSchema = %q, want sales", mssqlSrc.sourceSchema)
	}
	if !mssqlSrc.snakeCaseIDs {
		t.Fatal("snakeCaseIDs = false, want true")
	}
}

func TestNewConfiguredSourceDB_MySQLAppliesCharsetAndIdentifiers(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.Charset = "latin1"
	cfg.SnakeCaseIdentifiers = false

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	mysqlSrc, ok := src.(*mysqlSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *mysqlSourceDB", src)
	}

	if mysqlSrc.charset != "latin1" {
		t.Fatalf("charset = %q, want latin1", mysqlSrc.charset)
	}
	if mysqlSrc.snakeCaseIDs {
		t.Fatal("snakeCaseIDs = true, want false")
	}
}

func TestNewConfiguredSourceDB_MariaDBAppliesCharsetAndIdentifiers(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mariadb"
	cfg.Source.Charset = "latin1"
	cfg.SnakeCaseIdentifiers = false

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	mariaSrc, ok := src.(*mariadbSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *mariadbSourceDB", src)
	}

	if mariaSrc.charset != "latin1" {
		t.Fatalf("charset = %q, want latin1", mariaSrc.charset)
	}
	if mariaSrc.snakeCaseIDs {
		t.Fatal("snakeCaseIDs = true, want false")
	}
	if mariaSrc.Name() != "MariaDB" {
		t.Fatalf("Name() = %q, want MariaDB", mariaSrc.Name())
	}
}

func TestNewConfiguredSourceDB_SQLiteAppliesIdentifiers(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "sqlite"
	cfg.SnakeCaseIdentifiers = false

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	sqliteSrc, ok := src.(*sqliteSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *sqliteSourceDB", src)
	}

	if sqliteSrc.snakeCaseIDs {
		t.Fatal("snakeCaseIDs = true, want false")
	}
}

func TestNewConfiguredSourceDB_InvalidType(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "oracle"

	_, err := newConfiguredSourceDB(&cfg)
	if err == nil {
		t.Fatal("expected error for invalid source type")
	}
	if got, want := err.Error(), `unsupported source type "oracle" (must be mysql, mariadb, sqlite, or mssql)`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}
