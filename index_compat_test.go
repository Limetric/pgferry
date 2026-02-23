package main

import (
	"strings"
	"testing"
)

func TestIndexUnsupportedReason(t *testing.T) {
	tests := []struct {
		name string
		idx  Index
		ok   bool
	}{
		{"plain btree", Index{SourceName: "idx_a", Type: "BTREE", Columns: []string{"a"}}, false},
		{"prefix index", Index{SourceName: "idx_p", Type: "BTREE", Columns: []string{"a"}, HasPrefix: true}, true},
		{"expression index", Index{SourceName: "idx_e", Type: "BTREE", HasExpression: true}, true},
		{"fulltext", Index{SourceName: "idx_f", Type: "FULLTEXT", Columns: []string{"body"}}, true},
		{"no columns", Index{SourceName: "idx_n", Type: "BTREE"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, unsupported := indexUnsupportedReason(tt.idx)
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

	warnings := collectIndexCompatibilityWarnings(schema)
	if len(warnings) != 1 {
		t.Fatalf("warnings len=%d, want 1 (%v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "posts.idx_body") {
		t.Fatalf("unexpected warning content: %q", warnings[0])
	}
}

func TestQuotedOrderedColumnList(t *testing.T) {
	got := quotedOrderedColumnList([]string{"a", "b", "c"}, []string{"ASC", "DESC", "ASC"})
	want := "a, b DESC, c"
	if got != want {
		t.Fatalf("quotedOrderedColumnList()=%q, want %q", got, want)
	}
}
