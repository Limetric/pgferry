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
