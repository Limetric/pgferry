package main

import "testing"

func TestCollectUnsupportedTypeErrors(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				MySQLName: "users",
				Columns: []Column{
					{MySQLName: "id", DataType: "int", ColumnType: "int"},
					{MySQLName: "shape", DataType: "geometry", ColumnType: "geometry"},
				},
			},
			{
				MySQLName: "events",
				Columns: []Column{
					{MySQLName: "payload", DataType: "json", ColumnType: "json"},
					{MySQLName: "point", DataType: "point", ColumnType: "point"},
				},
			},
		},
	}

	errs := collectUnsupportedTypeErrors(schema, defaultTypeMappingConfig())
	if len(errs) != 2 {
		t.Fatalf("collectUnsupportedTypeErrors len = %d, want 2 (%v)", len(errs), errs)
	}
}

func TestCollectUnsupportedTypeErrors_UnknownAsText(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				MySQLName: "users",
				Columns: []Column{
					{MySQLName: "shape", DataType: "geometry", ColumnType: "geometry"},
				},
			},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.UnknownAsText = true

	errs := collectUnsupportedTypeErrors(schema, tm)
	if len(errs) != 0 {
		t.Fatalf("collectUnsupportedTypeErrors len = %d, want 0 (%v)", len(errs), errs)
	}
}
