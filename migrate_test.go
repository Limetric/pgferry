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

func TestBuildSourceSelectQuery_UsesExplicitQuotedColumnsInOrder(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "Order"},
			{SourceName: "created-at"},
		},
	}

	got := buildSourceSelectQuery(src, table)
	want := "SELECT `id`, `Order`, `created-at` FROM `users`"
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}

func TestBuildSourceSelectQuery_IncludesGeneratedColumns(t *testing.T) {
	src := &sqliteSourceDB{}
	table := Table{
		SourceName: "metrics",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "computed_value", Extra: "VIRTUAL GENERATED"},
			{SourceName: "stored_total", Extra: "STORED GENERATED"},
		},
	}

	got := buildSourceSelectQuery(src, table)
	want := `SELECT "id", "computed_value", "stored_total" FROM "metrics"`
	if got != want {
		t.Fatalf("buildSourceSelectQuery() = %q, want %q", got, want)
	}
}
