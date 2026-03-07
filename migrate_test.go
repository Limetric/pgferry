package main

import "testing"

func TestNewRowSourcePreallocatesBuffers(t *testing.T) {
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "email"},
			{SourceName: "created_at"},
		},
	}

	rs := newRowSource(nil, table, &sqliteSourceDB{}, defaultTypeMappingConfig())

	if len(rs.scanDest) != len(table.Columns) {
		t.Fatalf("scanDest len = %d, want %d", len(rs.scanDest), len(table.Columns))
	}
	if len(rs.scanPtrs) != len(table.Columns) {
		t.Fatalf("scanPtrs len = %d, want %d", len(rs.scanPtrs), len(table.Columns))
	}
	if len(rs.values) != len(table.Columns) {
		t.Fatalf("values len = %d, want %d", len(rs.values), len(table.Columns))
	}

	for i := range rs.scanDest {
		ptr, ok := rs.scanPtrs[i].(*any)
		if !ok {
			t.Fatalf("scanPtrs[%d] type = %T, want *any", i, rs.scanPtrs[i])
		}
		if ptr != &rs.scanDest[i] {
			t.Fatalf("scanPtrs[%d] does not point at scanDest[%d]", i, i)
		}
	}
}
