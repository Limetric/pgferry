package main

import (
	"database/sql"
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
