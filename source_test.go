package main

import "testing"

func TestNewConfiguredSourceDB_MSSQLSourceSchema(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mssql"
	cfg.Source.Charset = "utf8mb4"
	cfg.Source.SourceSchema = "sales"

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
