package main

import "testing"

func TestCollectUnsupportedTypeErrors_UnknownAsText_PerSource(t *testing.T) {
	// Columns that error without unknown_as_text; with the flag, mapping succeeds as text.
	tests := []struct {
		name   string
		mapper typeMapper
		schema *Schema
	}{
		{
			name:   "mysql_geometry",
			mapper: mysqlMapType,
			schema: &Schema{Tables: []Table{{SourceName: "t", Columns: []Column{{SourceName: "g", DataType: "geometry", ColumnType: "geometry"}}}}},
		},
		{
			name:   "mariadb_geometry",
			mapper: mariadbMapType,
			schema: &Schema{Tables: []Table{{SourceName: "t", Columns: []Column{{SourceName: "g", DataType: "geometry", ColumnType: "geometry"}}}}},
		},
		{
			name:   "mssql_cursor",
			mapper: mssqlMapType,
			schema: &Schema{Tables: []Table{{SourceName: "t", Columns: []Column{{SourceName: "c", DataType: "cursor", ColumnType: "cursor"}}}}},
		},
		{
			name:   "sqlite_unlisted_affinity",
			mapper: sqliteMapType,
			schema: &Schema{Tables: []Table{{SourceName: "t", Columns: []Column{{SourceName: "x", ColumnType: "vendor_specific_frob"}}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := defaultTypeMappingConfig()
			if n := len(collectUnsupportedTypeErrors(tt.schema, def, tt.mapper)); n == 0 {
				t.Fatalf("expected unsupported type errors without unknown_as_text, got 0")
			}
			tm := def
			tm.UnknownAsText = true
			if n := len(collectUnsupportedTypeErrors(tt.schema, tm, tt.mapper)); n != 0 {
				t.Fatalf("with unknown_as_text, want no errors, got %d: %v", n, collectUnsupportedTypeErrors(tt.schema, tm, tt.mapper))
			}
			col := tt.schema.Tables[0].Columns[0]
			got, err := tt.mapper(col, tm)
			if err != nil {
				t.Fatalf("MapType with unknown_as_text: %v", err)
			}
			if got != "text" {
				t.Fatalf("MapType with unknown_as_text = %q, want text", got)
			}
		})
	}
}

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
