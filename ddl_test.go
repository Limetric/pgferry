package main

import (
	"strings"
	"testing"
)

var mysqlSrc = &mysqlSourceDB{}

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

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
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

	ddl, err := generateCreateTable(table, "app", true, false, defaultTypeMappingConfig(), mysqlSrc)
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

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
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

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
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

	_, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err == nil {
		t.Fatal("expected error for unsupported MySQL type")
	}
}

func TestGenerateCreateTable_PreserveDefaults(t *testing.T) {
	table := Table{
		PGName: "defaults_demo",
		Columns: []Column{
			{PGName: "count", DataType: "int", ColumnType: "int", Nullable: false, Default: strPtr("0")},
			{PGName: "status", DataType: "varchar", ColumnType: "varchar(20)", CharMaxLen: 20, Nullable: false, Default: strPtr("new")},
			{PGName: "created_at", DataType: "timestamp", ColumnType: "timestamp", Nullable: false, Default: strPtr("CURRENT_TIMESTAMP")},
			{PGName: "metadata", DataType: "json", ColumnType: "json", Nullable: true, Default: strPtr("{}")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.JSONAsJSONB = true

	ddl, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if !strings.Contains(ddl, "count integer DEFAULT 0 NOT NULL") {
		t.Fatalf("expected numeric default in DDL, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "status varchar(20) DEFAULT 'new' NOT NULL") {
		t.Fatalf("expected text default in DDL, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "created_at timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL") {
		t.Fatalf("expected timestamp default in DDL, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, "metadata jsonb DEFAULT '{}'::jsonb") {
		t.Fatalf("expected jsonb default in DDL, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_PreserveDefaultsUnsupported(t *testing.T) {
	table := Table{
		PGName: "bad_defaults",
		Columns: []Column{
			{PGName: "enabled", DataType: "tinyint", ColumnType: "tinyint(1)", Precision: 1, Nullable: false, Default: strPtr("2")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.TinyInt1AsBoolean = true

	_, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err == nil {
		t.Fatal("expected error for unsupported boolean default")
	}
}

func TestGenerateCreateTable_NoPreserveDefaultsSkipsDefaults(t *testing.T) {
	table := Table{
		PGName: "no_defaults",
		Columns: []Column{
			{PGName: "name", DataType: "varchar", ColumnType: "varchar(20)", CharMaxLen: 20, Nullable: false, Default: strPtr("alice")},
		},
	}
	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if strings.Contains(ddl, "DEFAULT") {
		t.Fatalf("expected defaults to be skipped when preserve_defaults=false, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_EnumCheckMode(t *testing.T) {
	table := Table{
		PGName: "enum_demo",
		Columns: []Column{
			{PGName: "status", DataType: "enum", ColumnType: "enum('new','used')", Nullable: false},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "check"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, "CHECK (status IN ('new', 'used'))") {
		t.Fatalf("expected enum CHECK clause, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_SetArrayDefault(t *testing.T) {
	table := Table{
		PGName: "set_demo",
		Columns: []Column{
			{PGName: "flags", DataType: "set", ColumnType: "set('a','b','c')", Nullable: false, Default: strPtr("a,c")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array"

	ddl, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, "flags text[] DEFAULT ARRAY['a', 'c']::text[] NOT NULL") {
		t.Fatalf("expected set text[] default, got:\n%s", ddl)
	}
}

func strPtr(s string) *string {
	return &s
}
