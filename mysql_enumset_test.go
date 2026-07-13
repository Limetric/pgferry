package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseMySQLEnumSetValues(t *testing.T) {
	tests := []struct {
		in   string
		want []string
		err  bool
	}{
		{"enum('new','used')", []string{"new", "used"}, false},
		{"set('a','b','c')", []string{"a", "b", "c"}, false},
		{"enum('it''s','ok')", []string{"it's", "ok"}, false},
		{"enum('a\\'b','c')", []string{"a'b", "c"}, false},
		{"enum(bad)", nil, true},
	}

	for _, tt := range tests {
		got, err := parseMySQLEnumSetValues(tt.in)
		if tt.err {
			if err == nil {
				t.Fatalf("parseMySQLEnumSetValues(%q) expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseMySQLEnumSetValues(%q) error: %v", tt.in, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("parseMySQLEnumSetValues(%q) = %#v, want %#v", tt.in, got, tt.want)
		}
	}
}

func TestParseMySQLSetDefault(t *testing.T) {
	got := parseMySQLSetDefault("a,b,c")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseMySQLSetDefault() = %#v, want %#v", got, want)
	}

	got = parseMySQLSetDefault("")
	if len(got) != 0 {
		t.Fatalf("parseMySQLSetDefault(empty) = %#v, want empty", got)
	}
}

func TestParseMySQLSetDefault_PreservesLeadingSpaces(t *testing.T) {
	// MySQL preserves leading spaces in SET member values. Trimming them here
	// produced a DEFAULT that the generated CHECK constraint rejected, because the
	// CHECK is built from parseMySQLEnumSetValues, which does not trim.
	got := parseMySQLSetDefault(" a,b")
	want := []string{" a", "b"}
	if len(got) != len(want) {
		t.Fatalf("parseMySQLSetDefault(%q) = %q, want %q", " a,b", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseMySQLSetDefault(%q) = %q, want %q", " a,b", got, want)
		}
	}
}

func TestSetDefaultMatchesGeneratedCheckConstraint(t *testing.T) {
	// The DEFAULT and the CHECK must agree on member spelling, or the first insert
	// violates the column's own constraint.
	col := Column{
		SourceName: "flags",
		PGName:     "flags",
		DataType:   "set",
		ColumnType: "set(' a','b')",
		Default:    strPtr(" a"),
	}
	typeMap := defaultTypeMappingConfig()
	typeMap.SetMode = "text_array_check"

	check, err := setArrayCheckClause(col, typeMap, "mysql")
	if err != nil {
		t.Fatalf("setArrayCheckClause: %v", err)
	}
	def, err := mysqlMapDefault(col, "text[]", typeMap)
	if err != nil {
		t.Fatalf("mysqlMapDefault: %v", err)
	}

	// The default renders ARRAY['<member>']; that member must appear in the CHECK.
	if !strings.Contains(check, `' a'`) {
		t.Fatalf("CHECK clause lost the leading space: %s", check)
	}
	if !strings.Contains(def, `' a'`) {
		t.Fatalf("DEFAULT does not match the CHECK member spelling: default=%s check=%s", def, check)
	}
}
