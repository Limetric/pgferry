package main

import (
	"strings"
	"testing"
)

func TestUnsignedCheckExpr(t *testing.T) {
	tests := []struct {
		name string
		col  Column
		tm   TypeMappingConfig
		want string
		ok   bool
	}{
		{
			name: "int unsigned bounded",
			col:  Column{PGName: "id", DataType: "int", ColumnType: "int unsigned"},
			tm:   defaultTypeMappingConfig(),
			want: "id >= 0 AND id <= 4294967295",
			ok:   true,
		},
		{
			name: "decimal unsigned lower-bound only",
			col:  Column{PGName: "amount", DataType: "decimal", ColumnType: "decimal(10,2) unsigned"},
			tm:   defaultTypeMappingConfig(),
			want: "amount >= 0",
			ok:   true,
		},
		{
			name: "signed column skipped",
			col:  Column{PGName: "age", DataType: "int", ColumnType: "int"},
			tm:   defaultTypeMappingConfig(),
			ok:   false,
		},
		{
			name: "tinyint1 bool opt-in skipped",
			col:  Column{PGName: "enabled", DataType: "tinyint", Precision: 3, ColumnType: "tinyint(1) unsigned"},
			tm: TypeMappingConfig{
				TinyInt1AsBoolean: true,
			},
			ok: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := unsignedCheckExpr(tt.col, tt.tm)
			if ok != tt.ok {
				t.Fatalf("unsignedCheckExpr() ok = %t, want %t", ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("unsignedCheckExpr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUnsignedConstraintName(t *testing.T) {
	name := unsignedConstraintName("very_long_table_name_that_needs_truncation_for_postgres_identifiers", "very_long_column_name_that_needs_truncation")
	if len(name) > 63 {
		t.Fatalf("constraint name length = %d, want <= 63", len(name))
	}
	if !strings.HasPrefix(name, "ck_") {
		t.Fatalf("constraint name prefix mismatch: %q", name)
	}
	if !strings.Contains(name, "_unsigned") {
		t.Fatalf("constraint name should include unsigned marker: %q", name)
	}
}
