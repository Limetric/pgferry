package main

import (
	"strings"
	"testing"
)

func TestGenerateCreateTable(t *testing.T) {
	table := Table{
		PGName: "users",
		Columns: []Column{
			{PGName: "identifier", DataType: "binary", Precision: 16, Nullable: false},
			{PGName: "secret", DataType: "varchar", CharMaxLen: 150, Nullable: false},
			{PGName: "enabled", DataType: "tinyint", Precision: 1, Nullable: false},
			{PGName: "email_address", DataType: "varchar", CharMaxLen: 150, Nullable: true},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	// Should be logged by default
	if strings.Contains(ddl, "UNLOGGED") {
		t.Error("DDL should not contain UNLOGGED by default")
	}

	// Should have schema prefix
	if !strings.Contains(ddl, "app.users") {
		t.Error("DDL should reference app.users")
	}

	// uuid type for binary(16)
	if !strings.Contains(ddl, "identifier bytea NOT NULL") {
		t.Errorf("DDL should map binary(16) to bytea by default, got:\n%s", ddl)
	}

	// boolean for tinyint(1)
	if !strings.Contains(ddl, "enabled smallint NOT NULL") {
		t.Errorf("DDL should map tinyint(1) to smallint by default, got:\n%s", ddl)
	}

	// nullable column should not have NOT NULL
	if strings.Contains(ddl, "email_address varchar(150) NOT NULL") {
		t.Error("nullable column should not have NOT NULL")
	}
}

func TestGenerateCreateTable_Unlogged(t *testing.T) {
	table := Table{
		PGName: "users",
		Columns: []Column{
			{PGName: "id", DataType: "int", Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", true, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, "CREATE UNLOGGED TABLE app.users") {
		t.Errorf("DDL should be unlogged when enabled, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_DefaultLoggedPrefix(t *testing.T) {
	table := Table{
		PGName: "accounts",
		Columns: []Column{
			{PGName: "id", DataType: "int", Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.HasPrefix(ddl, "CREATE TABLE app.accounts (") {
		t.Fatalf("expected logged CREATE TABLE prefix, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_ReservedWords(t *testing.T) {
	table := Table{
		PGName: "user",
		Columns: []Column{
			{PGName: "order", DataType: "int", Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if !strings.Contains(ddl, `"user"`) {
		t.Errorf("DDL should quote reserved word 'user', got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"order"`) {
		t.Errorf("DDL should quote reserved word 'order', got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_UnknownTypeErrors(t *testing.T) {
	table := Table{
		PGName: "mystery",
		Columns: []Column{
			{PGName: "shape", DataType: "geometry", ColumnType: "geometry", Nullable: true},
		},
	}

	_, err := generateCreateTable(table, "app", false, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected error for unsupported MySQL type")
	}
}
