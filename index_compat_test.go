package main

import (
	"strings"
	"testing"
)

func TestIndexUnsupportedReason(t *testing.T) {
	table := Table{
		Columns: []Column{
			{PGName: "geom", DataType: "geometry", ColumnType: "geometry"},
		},
	}
	tests := []struct {
		name string
		idx  Index
		tm   TypeMappingConfig
		ok   bool
	}{
		{"plain btree", Index{SourceName: "idx_a", Type: "BTREE", Columns: []string{"a"}}, defaultTypeMappingConfig(), false},
		{"prefix index", Index{SourceName: "idx_p", Type: "BTREE", Columns: []string{"a"}, HasPrefix: true}, defaultTypeMappingConfig(), true},
		{"expression index", Index{SourceName: "idx_e", Type: "BTREE", HasExpression: true}, defaultTypeMappingConfig(), true},
		{"fulltext", Index{SourceName: "idx_f", Type: "FULLTEXT", Columns: []string{"body"}}, defaultTypeMappingConfig(), true},
		{"no columns", Index{SourceName: "idx_n", Type: "BTREE"}, defaultTypeMappingConfig(), true},
		{"spatial without postgis", Index{SourceName: "idx_geom", Type: "SPATIAL", Columns: []string{"geom"}}, defaultTypeMappingConfig(), true},
		{"spatial with postgis", Index{SourceName: "idx_geom", Type: "SPATIAL", Columns: []string{"geom"}}, TypeMappingConfig{UsePostGIS: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, unsupported := indexUnsupportedReason(table, tt.idx, tt.tm)
			if unsupported != tt.ok {
				t.Fatalf("indexUnsupportedReason() unsupported=%t, want %t", unsupported, tt.ok)
			}
		})
	}
}

func TestCollectIndexCompatibilityWarnings(t *testing.T) {
	schema := &Schema{Tables: []Table{
		{
			SourceName: "posts",
			Indexes: []Index{
				{Name: "posts_title", SourceName: "idx_title", Type: "BTREE", Columns: []string{"title"}},
				{Name: "posts_body_ft", SourceName: "idx_body", Type: "FULLTEXT", Columns: []string{"body"}},
			},
		},
	}}

	warnings := collectIndexCompatibilityWarnings(schema, defaultTypeMappingConfig())
	if len(warnings) != 1 {
		t.Fatalf("warnings len=%d, want 1 (%v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "posts.idx_body") {
		t.Fatalf("unexpected warning content: %q", warnings[0])
	}
}

func TestQuotedOrderedColumnList(t *testing.T) {
	got := quotedOrderedColumnList([]string{"a", "b", "c"}, []string{"ASC", "DESC", "ASC"})
	want := `"a", "b" DESC, "c"`
	if got != want {
		t.Fatalf("quotedOrderedColumnList()=%q, want %q", got, want)
	}
}
