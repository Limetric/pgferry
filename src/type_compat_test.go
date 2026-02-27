package main

import "testing"

func TestCollectUnsupportedTypeErrors(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "users",
				Columns: []Column{
					{SourceName: "id", DataType: "int", ColumnType: "int"},
					{SourceName: "shape", DataType: "geometry", ColumnType: "geometry"},
				},
			},
			{
				SourceName: "events",
				Columns: []Column{
					{SourceName: "payload", DataType: "json", ColumnType: "json"},
					{SourceName: "point", DataType: "point", ColumnType: "point"},
				},
			},
		},
	}

	errs := collectUnsupportedTypeErrors(schema, defaultTypeMappingConfig(), mysqlMapType)
	if len(errs) != 2 {
		t.Fatalf("collectUnsupportedTypeErrors len = %d, want 2 (%v)", len(errs), errs)
	}
}

func TestCollectUnsupportedTypeErrors_UnknownAsText(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "users",
				Columns: []Column{
					{SourceName: "shape", DataType: "geometry", ColumnType: "geometry"},
				},
			},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.UnknownAsText = true

	errs := collectUnsupportedTypeErrors(schema, tm, mysqlMapType)
	if len(errs) != 0 {
		t.Fatalf("collectUnsupportedTypeErrors len = %d, want 0 (%v)", len(errs), errs)
	}
}
