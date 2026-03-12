package main

import "testing"

func TestCollectRequiredExtensions_None(t *testing.T) {
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}
	reqs := collectRequiredExtensions(&Schema{}, mysqlSrc, cfg)
	if len(reqs) != 0 {
		t.Fatalf("required extensions = %d, want 0", len(reqs))
	}
}

func TestCollectRequiredExtensions_Citext(t *testing.T) {
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}
	cfg.TypeMapping.CIAsCitext = true
	schema := &Schema{
		Tables: []Table{
			{
				Columns: []Column{
					{SourceName: "name", PGName: "name", DataType: "varchar", ColumnType: "varchar(50)", CharMaxLen: 50, Collation: "utf8mb4_general_ci"},
				},
			},
		},
	}

	reqs := collectRequiredExtensions(schema, mysqlSrc, cfg)
	if len(reqs) != 1 {
		t.Fatalf("required extensions = %d, want 1", len(reqs))
	}
	if reqs[0].Name != "citext" {
		t.Fatalf("extension name = %q, want citext", reqs[0].Name)
	}
	if !reqs[0].CreateIfMissing {
		t.Fatal("citext should be auto-created when needed")
	}
}

func TestCollectRequiredExtensions_PostGIS(t *testing.T) {
	cfg := &MigrationConfig{
		TypeMapping: defaultTypeMappingConfig(),
		PostGIS:     PostGISConfig{Enabled: true, CreateExtension: true},
	}
	schema := &Schema{
		Tables: []Table{
			{
				Columns: []Column{
					{SourceName: "shape", PGName: "shape", DataType: "polygon", ColumnType: "polygon"},
				},
			},
		},
	}

	reqs := collectRequiredExtensions(schema, mysqlSrc, cfg)
	if len(reqs) != 1 {
		t.Fatalf("required extensions = %d, want 1", len(reqs))
	}
	if reqs[0].Name != "postgis" {
		t.Fatalf("extension name = %q, want postgis", reqs[0].Name)
	}
	if !reqs[0].CreateIfMissing {
		t.Fatal("postgis should allow creation when configured")
	}
}
