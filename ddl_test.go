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

	ddl := generateCreateTable(table, "app")

	// Should be UNLOGGED
	if !strings.Contains(ddl, "UNLOGGED") {
		t.Error("DDL should contain UNLOGGED")
	}

	// Should have schema prefix
	if !strings.Contains(ddl, "app.users") {
		t.Error("DDL should reference app.users")
	}

	// uuid type for binary(16)
	if !strings.Contains(ddl, "identifier uuid NOT NULL") {
		t.Errorf("DDL should map binary(16) to uuid, got:\n%s", ddl)
	}

	// boolean for tinyint(1)
	if !strings.Contains(ddl, "enabled boolean NOT NULL") {
		t.Errorf("DDL should map tinyint(1) to boolean, got:\n%s", ddl)
	}

	// nullable column should not have NOT NULL
	if strings.Contains(ddl, "email_address varchar(150) NOT NULL") {
		t.Error("nullable column should not have NOT NULL")
	}
}

func TestGenerateCreateTable_ReservedWords(t *testing.T) {
	table := Table{
		PGName: "user",
		Columns: []Column{
			{PGName: "order", DataType: "int", Nullable: false},
		},
	}

	ddl := generateCreateTable(table, "app")

	if !strings.Contains(ddl, `"user"`) {
		t.Errorf("DDL should quote reserved word 'user', got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"order"`) {
		t.Errorf("DDL should quote reserved word 'order', got:\n%s", ddl)
	}
}
