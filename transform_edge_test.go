package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

// --- MySQL TransformValue edge cases ---

func TestMySQLTransformValue_NilPassthrough(t *testing.T) {
	col := Column{DataType: "int"}
	got, err := mysqlTransformValue(nil, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMySQLTransformValue_Binary16WrongSize(t *testing.T) {
	col := Column{DataType: "binary", ColumnType: "binary(16)"}
	tm := defaultTypeMappingConfig()
	tm.Binary16AsUUID = true

	// 15 bytes - too short
	_, err := mysqlTransformValue([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, col, tm)
	if err == nil {
		t.Fatal("expected error for 15-byte input")
	}
	if !strings.Contains(err.Error(), "16-byte binary UUID") {
		t.Fatalf("expected UUID size error, got: %v", err)
	}

	// 17 bytes - too long
	_, err = mysqlTransformValue([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}, col, tm)
	if err == nil {
		t.Fatal("expected error for 17-byte input")
	}

	// Non-byte input
	_, err = mysqlTransformValue(int64(42), col, tm)
	if err == nil {
		t.Fatal("expected error for non-byte input")
	}
}

func TestMySQLTransformValue_TinyInt1Boolean_NonBinaryValues(t *testing.T) {
	col := Column{DataType: "tinyint", ColumnType: "tinyint(1)"}
	tm := defaultTypeMappingConfig()
	tm.TinyInt1AsBoolean = true

	// int64 value > 1 should error
	_, err := mysqlTransformValue(int64(2), col, tm)
	if err == nil {
		t.Fatal("expected error for tinyint(1) value 2")
	}

	// int64 value -1 should error
	_, err = mysqlTransformValue(int64(-1), col, tm)
	if err == nil {
		t.Fatal("expected error for tinyint(1) value -1")
	}

	// bool passthrough
	got, err := mysqlTransformValue(true, col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}

	// Unsupported type
	_, err = mysqlTransformValue(float64(1.0), col, tm)
	if err == nil {
		t.Fatal("expected error for float64 input to tinyint(1) boolean")
	}

	// []byte "0" and "1"
	got, err = mysqlTransformValue([]byte("0"), col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != false {
		t.Fatalf("expected false for []byte('0'), got %v", got)
	}

	// []byte non-0/1
	_, err = mysqlTransformValue([]byte("2"), col, tm)
	if err == nil {
		t.Fatal("expected error for []byte('2') tinyint(1) boolean")
	}
}

func TestMySQLTransformValue_BitEmptyBytes(t *testing.T) {
	col := Column{DataType: "bit", ColumnType: "bit(8)"}
	tm := defaultTypeMappingConfig()
	tm.BitMode = "bit"

	// Non-byte input should error
	_, err := mysqlTransformValue(int64(42), col, tm)
	if err == nil {
		t.Fatal("expected error for non-byte BIT input")
	}
	if !strings.Contains(err.Error(), "expected []byte for BIT") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMySQLTransformValue_BitDefaultWidth(t *testing.T) {
	// Column with no parseable bit width; should default to len(b)*8
	col := Column{DataType: "bit", ColumnType: "bit"}
	tm := defaultTypeMappingConfig()
	tm.BitMode = "bit"

	got, err := mysqlTransformValue([]byte{0b10110011}, col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != "10110011" {
		t.Fatalf("got %q, want %q", got, "10110011")
	}
}

func TestMySQLTransformValue_TimeEmptyString(t *testing.T) {
	col := Column{DataType: "time"}
	tm := defaultTypeMappingConfig()
	tm.TimeMode = "time"

	// Whitespace-only should return empty after trim
	got, err := mysqlTransformValue("   ", col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestMySQLTransformValue_SetTextArray(t *testing.T) {
	col := Column{DataType: "set", ColumnType: "set('a','b','c')"}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array"

	// Empty string should return empty array
	got, err := mysqlTransformValue("", col, tm)
	if err != nil {
		t.Fatal(err)
	}
	arr, ok := got.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", got)
	}
	if len(arr) != 0 {
		t.Fatalf("expected empty array, got %v", arr)
	}

	// Normal value
	got, err = mysqlTransformValue("a,b", col, tm)
	if err != nil {
		t.Fatal(err)
	}
	arr = got.([]string)
	if len(arr) != 2 || arr[0] != "a" || arr[1] != "b" {
		t.Fatalf("expected [a b], got %v", arr)
	}

	// Unsupported type for set
	_, err = mysqlTransformValue(int64(42), col, tm)
	if err == nil {
		t.Fatal("expected error for int64 set input")
	}
}

func TestMySQLTransformValue_ZeroDate_ErrorMode(t *testing.T) {
	col := Column{DataType: "date", SourceName: "created"}
	tm := defaultTypeMappingConfig()
	tm.ZeroDateMode = "error"

	// time.Time zero value
	_, err := mysqlTransformValue(time.Time{}, col, tm)
	if err == nil {
		t.Fatal("expected error for zero date in error mode")
	}
	if !strings.Contains(err.Error(), "zero date") {
		t.Fatalf("expected zero date error, got: %v", err)
	}
}

func TestMySQLTransformValue_ZeroDatetime_NullMode(t *testing.T) {
	col := Column{DataType: "datetime", SourceName: "updated"}
	tm := defaultTypeMappingConfig()
	tm.ZeroDateMode = "null"

	got, err := mysqlTransformValue(time.Time{}, col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil for zero datetime in null mode, got %v", got)
	}
}

// --- MariaDB TransformValue edge cases ---

func TestMariaDBTransformValue_UUIDColumn(t *testing.T) {
	col := Column{DataType: "uuid"}
	got, err := mariadbTransformValue("  550E8400-E29B-41D4-A716-446655440000  ", col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string, got %T", got)
	}
	if s != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("got %q, want lowercase trimmed UUID", s)
	}
}

func TestMariaDBTransformValue_JSONColumnTypeAlias(t *testing.T) {
	// MariaDB stores JSON as longtext with ColumnType="json" alias
	col := Column{DataType: "longtext", ColumnType: "json"}
	tm := defaultTypeMappingConfig()
	tm.SanitizeJSONNullBytes = true

	got, err := mariadbTransformValue("hello\x00world", col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != "helloworld" {
		t.Fatalf("expected null bytes stripped, got %q", got)
	}
}

// --- MSSQL TransformValue edge cases ---

func TestMSSQLTransformValue_NilPassthrough(t *testing.T) {
	col := Column{DataType: "int"}
	got, err := mssqlTransformValue(nil, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestMSSQLTransformValue_UUIDAllZeros(t *testing.T) {
	col := Column{DataType: "uniqueidentifier"}
	got, err := mssqlTransformValue(make([]byte, 16), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "00000000-0000-0000-0000-000000000000" {
		t.Fatalf("got %q, want all-zero UUID", got)
	}
}

func TestMSSQLTransformValue_UUIDAllFF(t *testing.T) {
	col := Column{DataType: "uniqueidentifier"}
	b := make([]byte, 16)
	for i := range b {
		b[i] = 0xff
	}
	got, err := mssqlTransformValue(b, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "ffffffff-ffff-ffff-ffff-ffffffffffff" {
		t.Fatalf("got %q, want all-ff UUID", got)
	}
}

func TestMSSQLTransformValue_UUIDString(t *testing.T) {
	col := Column{DataType: "uniqueidentifier"}
	got, err := mssqlTransformValue("550E8400-E29B-41D4-A716-446655440000", col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("got %q, want lowercase UUID string", got)
	}
}

func TestMSSQLTransformValue_UUIDNonStandardByteLength(t *testing.T) {
	// Known limitation: non-16-byte []byte input for uniqueidentifier passes
	// through as a raw string, which PostgreSQL's uuid type will reject during
	// COPY. This test documents the current behavior so a future fix (returning
	// an error for unexpected byte lengths) is detected.
	col := Column{DataType: "uniqueidentifier"}
	got, err := mssqlTransformValue([]byte("hello"), col, defaultTypeMappingConfig())
	if err != nil {
		// If a future change makes this an error, that's the correct fix.
		return
	}
	if got != "hello" {
		t.Fatalf("got %q, want passthrough for non-16-byte input", got)
	}
}

func TestMSSQLTransformValue_MoneyNegative(t *testing.T) {
	col := Column{DataType: "money"}
	got, err := mssqlTransformValue(float64(-123.4567), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "-123.4567" {
		t.Fatalf("got %q, want '-123.4567'", got)
	}
}

func TestMSSQLTransformValue_MoneyZero(t *testing.T) {
	col := Column{DataType: "money"}
	got, err := mssqlTransformValue(float64(0), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.0000" {
		t.Fatalf("got %q, want '0.0000'", got)
	}
}

func TestMSSQLTransformValue_SmallMoneyFromBytes(t *testing.T) {
	col := Column{DataType: "smallmoney"}
	got, err := mssqlTransformValue([]byte("99.99"), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "99.99" {
		t.Fatalf("got %q, want '99.99'", got)
	}
}

func TestMSSQLTransformValue_MoneyFromString(t *testing.T) {
	col := Column{DataType: "money"}
	got, err := mssqlTransformValue("1234.5678", col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "1234.5678" {
		t.Fatalf("got %q, want '1234.5678'", got)
	}
}

func TestMSSQLTransformValue_MoneyInfinity(t *testing.T) {
	// Known limitation: mssqlTransformValue passes Infinity through as "+Inf",
	// which PostgreSQL's numeric type rejects during COPY. This test documents
	// the current behavior so a future fix (returning an error) is detected.
	col := Column{DataType: "money"}
	got, err := mssqlTransformValue(math.Inf(1), col, defaultTypeMappingConfig())
	if err != nil {
		// If a future change makes this an error, that's the correct fix.
		return
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string, got %T", got)
	}
	if s != "+Inf" && s != "Inf" {
		t.Fatalf("got %q for Infinity", s)
	}
}

func TestMSSQLTransformValue_MoneyNaN(t *testing.T) {
	// Known limitation: mssqlTransformValue passes NaN through as "NaN",
	// which PostgreSQL's numeric type rejects during COPY. This test documents
	// the current behavior so a future fix (returning an error) is detected.
	col := Column{DataType: "money"}
	got, err := mssqlTransformValue(math.NaN(), col, defaultTypeMappingConfig())
	if err != nil {
		// If a future change makes this an error, that's the correct fix.
		return
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected string, got %T", got)
	}
	if s != "NaN" {
		t.Fatalf("got %q for NaN", s)
	}
}

func TestMSSQLTransformValue_BitPassthrough(t *testing.T) {
	col := Column{DataType: "bit"}

	// bool true
	got, err := mssqlTransformValue(true, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != true {
		t.Fatalf("got %v, want true", got)
	}

	// nil
	got, err = mssqlTransformValue(nil, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestMSSQLTransformValue_StringNullBytes(t *testing.T) {
	col := Column{DataType: "nvarchar"}
	tm := defaultTypeMappingConfig()

	// Multiple consecutive null bytes
	got, err := mssqlTransformValue("hello\x00\x00world", col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != "helloworld" {
		t.Fatalf("got %q, want 'helloworld'", got)
	}

	// Null byte at boundaries
	got, err = mssqlTransformValue("\x00start\x00", col, tm)
	if err != nil {
		t.Fatal(err)
	}
	if got != "start" {
		t.Fatalf("got %q, want 'start'", got)
	}
}

func TestMSSQLTransformValue_DefaultPassthrough(t *testing.T) {
	col := Column{DataType: "int"}
	got, err := mssqlTransformValue(int64(42), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(42) {
		t.Fatalf("got %v, want 42", got)
	}
}

// --- MSSQL MapDefault edge cases ---

func TestMSSQLMapDefault_MixedCaseFunctions(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"getdate()", "CURRENT_TIMESTAMP"},
		{"GETDATE()", "CURRENT_TIMESTAMP"},
		{"GetDate()", "CURRENT_TIMESTAMP"},
		{"newid()", "gen_random_uuid()"},
		{"NEWID()", "gen_random_uuid()"},
		{"newsequentialid()", "gen_random_uuid()"},
		{"SUSER_SNAME()", "CURRENT_USER"},
	}

	for _, tt := range tests {
		// Wrap in parens as MSSQL stores defaults
		input := "(" + tt.input + ")"
		col := Column{Default: &input, DataType: "datetime"}
		got, err := mssqlMapDefault(col, "timestamp", defaultTypeMappingConfig())
		if err != nil {
			t.Fatalf("mssqlMapDefault(%q): %v", input, err)
		}
		if got != tt.want {
			t.Errorf("mssqlMapDefault(%q) = %q, want %q", input, got, tt.want)
		}
	}
}

func TestMSSQLMapDefault_UnicodePrefix(t *testing.T) {
	input := "(N'hello')"
	col := Column{Default: &input, DataType: "nvarchar"}
	got, err := mssqlMapDefault(col, "text", defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "'hello'" {
		t.Fatalf("got %q, want 'hello' (N prefix stripped)", got)
	}
}

func TestMSSQLMapDefault_LowercaseUnicodePrefix(t *testing.T) {
	input := "(n'world')"
	col := Column{Default: &input, DataType: "nvarchar"}
	got, err := mssqlMapDefault(col, "text", defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "'world'" {
		t.Fatalf("got %q, want 'world' (n prefix stripped)", got)
	}
}

func TestMSSQLMapDefault_NumericEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain zero", "((0))", "0"},
		{"decimal", "((1.5))", "1.5"},
		{"negative", "((-42))", "-42"},
		{"leading zeros", "((007))", "007"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := Column{Default: &tt.input, DataType: "int"}
			got, err := mssqlMapDefault(col, "integer", defaultTypeMappingConfig())
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMSSQLMapDefault_BooleanBit(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"((0))", "FALSE"},
		{"((1))", "TRUE"},
	}
	for _, tt := range tests {
		col := Column{Default: &tt.input, DataType: "bit"}
		got, err := mssqlMapDefault(col, "boolean", defaultTypeMappingConfig())
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("mssqlMapDefault(%q) for boolean = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMSSQLMapDefault_NullDefault(t *testing.T) {
	col := Column{Default: nil, DataType: "int"}
	got, err := mssqlMapDefault(col, "integer", defaultTypeMappingConfig())
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty for nil default", got)
	}
}

// --- SQLite MapDefault edge cases ---

func TestSQLiteIsNumericLiteral_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"42", true},
		{"-42", true},
		{"+42", true},
		{"3.14", true},
		{"-3.14", true},
		{"1.2.3", false},  // multiple dots
		{"-", false},      // sign only
		{"+", false},      // sign only
		{"-.5", true},     // valid: sign + dot + digits
		{".5", true},      // dot then digits is valid numeric literal
		{"007", true},     // leading zeros
		{"abc", false},
		{"1e5", false},    // no scientific notation support
		{"1 2", false},    // space
	}

	for _, tt := range tests {
		got := isNumericLiteral(tt.input)
		if got != tt.want {
			t.Errorf("isNumericLiteral(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSQLiteMapDefault_NullHandling(t *testing.T) {
	tests := []string{"NULL", "null", "Null", "nULL"}
	for _, raw := range tests {
		col := Column{Default: &raw, DataType: "text"}
		got, err := sqliteMapDefault(col, "text")
		if err != nil {
			t.Fatalf("sqliteMapDefault(%q): %v", raw, err)
		}
		if got != "" {
			t.Errorf("sqliteMapDefault(%q) = %q, want empty (NULL handled)", raw, got)
		}
	}
}

func TestSQLiteMapDefault_BooleanKeywords(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"TRUE", "TRUE"},
		{"FALSE", "FALSE"},
		{"true", "TRUE"},
		{"false", "FALSE"},
		{"True", "TRUE"},
		{"False", "FALSE"},
	}
	for _, tt := range tests {
		col := Column{Default: &tt.input}
		got, err := sqliteMapDefault(col, "boolean")
		if err != nil {
			t.Fatalf("sqliteMapDefault(%q): %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("sqliteMapDefault(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSQLiteMapDefault_StringLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "'hello'", "'hello'"},
		{"empty", "''", "''"},
		{"escaped quotes", "'it''s'", "'it''s'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := Column{Default: &tt.input}
			got, err := sqliteMapDefault(col, "text")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSQLiteMapDefault_NumericAsBoolean(t *testing.T) {
	zero := "0"
	col := Column{Default: &zero}
	got, err := sqliteMapDefault(col, "boolean")
	if err != nil {
		t.Fatal(err)
	}
	if got != "FALSE" {
		t.Fatalf("got %q, want FALSE", got)
	}

	one := "1"
	col.Default = &one
	got, err = sqliteMapDefault(col, "boolean")
	if err != nil {
		t.Fatal(err)
	}
	if got != "TRUE" {
		t.Fatalf("got %q, want TRUE", got)
	}
}

func TestSQLiteMapDefault_NilDefault(t *testing.T) {
	col := Column{Default: nil}
	got, err := sqliteMapDefault(col, "text")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty for nil default", got)
	}
}

func TestSQLiteMapDefault_SpecialFunctions(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP"},
		{"CURRENT_DATE", "CURRENT_DATE"},
		{"CURRENT_TIME", "CURRENT_TIME"},
	}
	for _, tt := range tests {
		col := Column{Default: &tt.input}
		got, err := sqliteMapDefault(col, "timestamp")
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Fatalf("got %q, want %q", got, tt.want)
		}
	}
}

