package main

import (
	"database/sql"
	"strings"
	"testing"
)

func TestMySQLIndexHasPrefix(t *testing.T) {
	tests := []struct {
		name      string
		indexType string
		subPart   sql.NullInt64
		want      bool
	}{
		{
			name:      "no sub part",
			indexType: "BTREE",
			subPart:   sql.NullInt64{},
			want:      false,
		},
		{
			name:      "btree sub part",
			indexType: "BTREE",
			subPart:   sql.NullInt64{Int64: 16, Valid: true},
			want:      true,
		},
		{
			name:      "spatial sub part metadata ignored",
			indexType: "SPATIAL",
			subPart:   sql.NullInt64{Int64: 32, Valid: true},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mysqlIndexHasPrefix(tt.indexType, tt.subPart); got != tt.want {
				t.Fatalf("mysqlIndexHasPrefix(%q, %+v) = %t, want %t", tt.indexType, tt.subPart, got, tt.want)
			}
		})
	}
}

func TestMariaDBJSONAliasColumnFromCheckClause(t *testing.T) {
	tests := []struct {
		clause string
		want   string
	}{
		{"json_valid(`payload`)", "payload"},
		{"CHECK (json_valid(payload))", "payload"},
		{"check (json_valid((\"payload\")))", "payload"},
		{"price > 0", ""},
	}

	for _, tt := range tests {
		if got := mariaDBJSONAliasColumnFromCheckClause(tt.clause); got != tt.want {
			t.Fatalf("mariaDBJSONAliasColumnFromCheckClause(%q) = %q, want %q", tt.clause, got, tt.want)
		}
	}
}

func TestAnnotateMariaDBJSONColumns(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "events",
				Columns: []Column{
					{SourceName: "payload", DataType: "longtext", ColumnType: "longtext"},
					{SourceName: "note", DataType: "longtext", ColumnType: "longtext"},
				},
			},
		},
	}

	annotateMariaDBJSONColumns(schema, map[string]map[string]bool{
		"events": {"payload": true},
	})

	if got := schema.Tables[0].Columns[0].DataType; got != "json" {
		t.Fatalf("payload DataType = %q, want json", got)
	}
	if got := schema.Tables[0].Columns[0].ColumnType; got != "json" {
		t.Fatalf("payload ColumnType = %q, want json", got)
	}
	if got := schema.Tables[0].Columns[1].DataType; got != "longtext" {
		t.Fatalf("note DataType = %q, want longtext", got)
	}
}

func TestMySQLViewDefinition(t *testing.T) {
	if got := mysqlViewDefinition("v_users", ""); got != "" {
		t.Fatalf("mysqlViewDefinition(empty) = %q, want empty", got)
	}
	got := mysqlViewDefinition("v_users", " SELECT 1 ")
	if !strings.Contains(got, "CREATE VIEW `v_users` AS\nSELECT 1") {
		t.Fatalf("mysqlViewDefinition() = %q", got)
	}
}

func TestMySQLRoutineDefinition(t *testing.T) {
	if got := mysqlRoutineDefinition("FUNCTION", "f", ""); got != "" {
		t.Fatalf("mysqlRoutineDefinition(empty) = %q, want empty", got)
	}
	got := mysqlRoutineDefinition("FUNCTION", "f", "RETURN 'x';")
	if !strings.Contains(got, "CREATE FUNCTION `f` AS\nRETURN 'x';") {
		t.Fatalf("mysqlRoutineDefinition() = %q", got)
	}
	if !strings.Contains(got, "parameters and returns are omitted") {
		t.Fatalf("mysqlRoutineDefinition() missing note, got %q", got)
	}
}

func TestMySQLTriggerDefinition(t *testing.T) {
	if got := mysqlTriggerDefinition("trg", "users", "before", "insert", ""); got != "" {
		t.Fatalf("mysqlTriggerDefinition(empty action) = %q, want empty", got)
	}
	got := mysqlTriggerDefinition("trg", "users", "before", "insert", "SET NEW.name = 'x';")
	if !strings.Contains(got, "CREATE TRIGGER `trg` BEFORE INSERT ON `users` FOR EACH ROW") {
		t.Fatalf("mysqlTriggerDefinition() = %q", got)
	}
	if !strings.Contains(got, "SET NEW.name = 'x';") {
		t.Fatalf("mysqlTriggerDefinition() missing action, got %q", got)
	}
}

func TestNormalizeMariaDBSchemaColumns_JSONColumnType(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				Columns: []Column{
					{SourceName: "j", DataType: "longtext", ColumnType: "  JSON "},
				},
			},
		},
	}
	normalizeMariaDBSchemaColumns(schema)
	if got := schema.Tables[0].Columns[0].DataType; got != "json" {
		t.Fatalf("DataType = %q, want json", got)
	}
}

func TestMySQLForeignKeysByTableQueryJoinsReferentialConstraintsOnTableName(t *testing.T) {
	if !strings.Contains(mysqlForeignKeysByTableQuery, "AND kcu.TABLE_NAME = rc.TABLE_NAME") {
		t.Fatalf("mysqlForeignKeysByTableQuery must join REFERENTIAL_CONSTRAINTS on TABLE_NAME to avoid duplicating same-named foreign keys across tables")
	}
}
