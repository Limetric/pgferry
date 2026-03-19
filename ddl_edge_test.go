package main

import (
	"strings"
	"testing"
)

func TestEnumCheckClause_EmptyValues(t *testing.T) {
	col := Column{DataType: "enum", ColumnType: "enum()"}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "check"

	got, err := enumCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty clause for empty enum, got %q", got)
	}
}

func TestEnumCheckClause_NotEnumColumn(t *testing.T) {
	col := Column{DataType: "varchar"}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "check"

	got, err := enumCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty for non-enum column, got %q", got)
	}
}

func TestEnumCheckClause_TextModeSkips(t *testing.T) {
	col := Column{DataType: "enum", ColumnType: "enum('a','b')"}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "text"

	got, err := enumCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty for text mode, got %q", got)
	}
}

func TestEnumCheckClause_WithValues(t *testing.T) {
	col := Column{DataType: "enum", PGName: "status", ColumnType: "enum('active','inactive')"}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "check"

	got, err := enumCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "CHECK") {
		t.Fatalf("expected CHECK clause, got %q", got)
	}
	if !strings.Contains(got, "'active'") || !strings.Contains(got, "'inactive'") {
		t.Fatalf("expected enum values in CHECK, got %q", got)
	}
}

func TestSetArrayCheckClause_EmptyValues(t *testing.T) {
	col := Column{DataType: "set", ColumnType: "set()"}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array_check"

	got, err := setArrayCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty clause for empty set, got %q", got)
	}
}

func TestSetArrayCheckClause_NotSetColumn(t *testing.T) {
	col := Column{DataType: "varchar"}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array_check"

	got, err := setArrayCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty for non-set column, got %q", got)
	}
}

func TestSetArrayCheckClause_TextModeSkips(t *testing.T) {
	col := Column{DataType: "set", ColumnType: "set('a','b')"}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text"

	got, err := setArrayCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty for text mode, got %q", got)
	}
}

func TestSetArrayCheckClause_WithValues(t *testing.T) {
	col := Column{DataType: "set", PGName: "tags", ColumnType: "set('a','b','c')"}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array_check"

	got, err := setArrayCheckClause(col, tm, "mysql")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "CHECK") {
		t.Fatalf("expected CHECK clause, got %q", got)
	}
	if !strings.Contains(got, "<@") {
		t.Fatalf("expected array containment operator, got %q", got)
	}
}

func TestPgEnumTypeName_EmptyValues(t *testing.T) {
	name := pgEnumTypeName([]string{})
	if !strings.HasPrefix(name, "pgferry_enum_") {
		t.Fatalf("expected pgferry_enum_ prefix, got %q", name)
	}
}

func TestPgEnumTypeName_SpecialCharValues(t *testing.T) {
	// Enum values with quotes and special chars should still hash correctly
	name := pgEnumTypeName([]string{"it's", "a \"test\"", ""})
	if !strings.HasPrefix(name, "pgferry_enum_") {
		t.Fatalf("expected pgferry_enum_ prefix, got %q", name)
	}
}

func TestPgLiteral_BasicEscaping(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", "'it''s'"},
		{"", "''"},
		{"a'b'c", "'a''b''c'"},
	}
	for _, tt := range tests {
		got := pgLiteral(tt.input)
		if got != tt.want {
			t.Errorf("pgLiteral(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsNumericType_Coverage(t *testing.T) {
	trueTypes := []string{"smallint", "integer", "bigint", "real", "double precision", "numeric", "numeric(10,2)"}
	for _, tp := range trueTypes {
		if !isNumericType(tp) {
			t.Errorf("isNumericType(%q) = false, want true", tp)
		}
	}

	falseTypes := []string{"text", "varchar(255)", "boolean", "bytea", "json", "timestamp"}
	for _, tp := range falseTypes {
		if isNumericType(tp) {
			t.Errorf("isNumericType(%q) = true, want false", tp)
		}
	}
}

func TestGenerateCreateTable_ZeroColumns(t *testing.T) {
	table := Table{SourceName: "empty", PGName: "empty"}
	src := &mysqlSourceDB{}

	got, err := generateCreateTable(table, "public", false, false, defaultTypeMappingConfig(), src)
	if err != nil {
		t.Fatal(err)
	}
	// Should produce a valid CREATE TABLE with no columns
	if !strings.Contains(got, "CREATE TABLE") {
		t.Fatalf("expected CREATE TABLE, got: %s", got)
	}
}
