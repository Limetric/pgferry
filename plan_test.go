package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlanReport_Empty(t *testing.T) {
	schema := &Schema{}
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}

	report := buildPlanReport(schema, nil, mysqlSrc, cfg)

	if len(report.SourceObjects.Views) != 0 {
		t.Errorf("views = %d, want 0", len(report.SourceObjects.Views))
	}
	if len(report.GeneratedColumns) != 0 {
		t.Errorf("generated columns = %d, want 0", len(report.GeneratedColumns))
	}
	if len(report.SkippedIndexes) != 0 {
		t.Errorf("skipped indexes = %d, want 0", len(report.SkippedIndexes))
	}
}

func TestBuildPlanReport_Full(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "users",
				PGName:     "users",
				Columns: []Column{
					{SourceName: "id", PGName: "id", DataType: "int"},
					{SourceName: "full_name", PGName: "full_name", DataType: "varchar", Extra: "VIRTUAL GENERATED", GenerationExpression: "concat(`first_name`,' ',`last_name`)"},
				},
				Indexes: []Index{
					{Name: "idx_ft", SourceName: "idx_ft", Type: "FULLTEXT", Columns: []string{"full_name"}},
					{Name: "idx_normal", SourceName: "idx_normal", Type: "BTREE", Columns: []string{"id"}},
				},
			},
		},
	}
	objs := &SourceObjects{
		Views:    []string{"v_active_users"},
		Routines: []string{"FUNCTION calc_score"},
		Triggers: []string{"trg_audit"},
	}
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}

	report := buildPlanReport(schema, objs, mysqlSrc, cfg)

	if len(report.SourceObjects.Views) != 1 || report.SourceObjects.Views[0] != "v_active_users" {
		t.Errorf("views = %v, want [v_active_users]", report.SourceObjects.Views)
	}
	if len(report.SourceObjects.Routines) != 1 {
		t.Errorf("routines = %d, want 1", len(report.SourceObjects.Routines))
	}
	if len(report.SourceObjects.Triggers) != 1 {
		t.Errorf("triggers = %d, want 1", len(report.SourceObjects.Triggers))
	}
	if len(report.GeneratedColumns) != 1 {
		t.Fatalf("generated columns = %d, want 1", len(report.GeneratedColumns))
	}
	gc := report.GeneratedColumns[0]
	if gc.Table != "users" || gc.Column != "full_name" {
		t.Errorf("generated column = %+v", gc)
	}
	if gc.Expression != "concat(`first_name`,' ',`last_name`)" {
		t.Errorf("generated column expression = %q, want source formula", gc.Expression)
	}
	if len(report.SkippedIndexes) != 1 {
		t.Fatalf("skipped indexes = %d, want 1", len(report.SkippedIndexes))
	}
	if report.SkippedIndexes[0].Index != "idx_ft" {
		t.Errorf("skipped index = %+v", report.SkippedIndexes[0])
	}
}

func TestWritePlanText_Empty(t *testing.T) {
	report := &PlanReport{}
	var buf bytes.Buffer
	writePlanText(&buf, report)

	got := buf.String()
	if !strings.Contains(got, "No manual follow-up items detected.") {
		t.Errorf("empty report should say no items detected, got:\n%s", got)
	}
}

func TestWritePlanText_WithContent(t *testing.T) {
	report := &PlanReport{
		RequiredExtensions: []PlanRequiredExtension{
			{Name: "citext", Feature: "ci_as_citext", Mode: "create_if_missing"},
		},
		SourceObjects: PlanSourceObjects{
			Views: []string{"v_users"},
		},
		UnsupportedColumns: []PlanUnsupportedColumn{
			{Table: "mystery", Column: "payload", SourceType: "geometry", Reason: "unsupported MySQL type \"geometry\""},
		},
		GeneratedColumns: []PlanGeneratedColumn{
			{Table: "orders", Column: "total", Expression: "VIRTUAL GENERATED"},
		},
		SkippedIndexes: []PlanSkippedIndex{
			{Table: "products", Index: "idx_ft_name", Reason: "index type \"FULLTEXT\" is not supported"},
		},
	}

	var buf bytes.Buffer
	writePlanText(&buf, report)
	got := buf.String()

	for _, want := range []string{
		"## Required Extensions (1)",
		"citext",
		"create it if missing",
		"## Source Objects",
		"v_users",
		"after_all",
		"## Unsupported Columns (1)",
		"mystery.payload",
		"## Generated Columns (1)",
		"orders.total",
		"after_data",
		"## Skipped Indexes (1)",
		"products.idx_ft_name",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("text output missing %q, got:\n%s", want, got)
		}
	}
}

func TestWritePlanJSON(t *testing.T) {
	report := &PlanReport{
		RequiredExtensions: []PlanRequiredExtension{
			{Name: "citext", Feature: "ci_as_citext", Mode: "create_if_missing"},
		},
		SourceObjects: PlanSourceObjects{
			Views:    []string{"v_users"},
			Routines: []string{"FUNCTION foo"},
		},
		GeneratedColumns: []PlanGeneratedColumn{
			{Table: "t1", Column: "c1", Expression: "STORED GENERATED"},
		},
		SkippedIndexes: []PlanSkippedIndex{
			{Table: "t2", Index: "idx_x", Reason: "prefix indexes (SUB_PART) are not currently supported"},
		},
		CollationWarnings: []string{"some warning"},
	}

	var buf bytes.Buffer
	if err := writePlanJSON(&buf, report); err != nil {
		t.Fatalf("writePlanJSON: %v", err)
	}

	var decoded PlanReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}

	if len(decoded.SourceObjects.Views) != 1 || decoded.SourceObjects.Views[0] != "v_users" {
		t.Errorf("views = %v", decoded.SourceObjects.Views)
	}
	if len(decoded.RequiredExtensions) != 1 {
		t.Errorf("required extensions = %d", len(decoded.RequiredExtensions))
	}
	if len(decoded.GeneratedColumns) != 1 {
		t.Errorf("generated columns = %d", len(decoded.GeneratedColumns))
	}
	if len(decoded.SkippedIndexes) != 1 {
		t.Errorf("skipped indexes = %d", len(decoded.SkippedIndexes))
	}
	if len(decoded.CollationWarnings) != 1 {
		t.Errorf("collation warnings = %d", len(decoded.CollationWarnings))
	}
}

func TestWritePlanJSON_Deterministic(t *testing.T) {
	report := &PlanReport{
		SourceObjects: PlanSourceObjects{
			Views:    []string{"b_view", "a_view"},
			Routines: []string{"FUNCTION z", "FUNCTION a"},
		},
		GeneratedColumns: []PlanGeneratedColumn{
			{Table: "t1", Column: "c1", Expression: "expr1"},
			{Table: "t2", Column: "c2", Expression: "expr2"},
		},
	}

	var buf1, buf2 bytes.Buffer
	writePlanJSON(&buf1, report)
	writePlanJSON(&buf2, report)

	if buf1.String() != buf2.String() {
		t.Error("JSON output is not deterministic")
	}
}

func TestWriteHookSkeletons_Empty(t *testing.T) {
	dir := t.TempDir()
	report := &PlanReport{}
	if err := writeHookSkeletons(dir, report, "public"); err != nil {
		t.Fatalf("writeHookSkeletons: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no files for empty report, got %d", len(entries))
	}
}

func TestWriteHookSkeletons_GeneratedColumns(t *testing.T) {
	dir := t.TempDir()
	report := &PlanReport{
		GeneratedColumns: []PlanGeneratedColumn{
			{Table: "users", Column: "display_name", Expression: "concat(`first`,`last`)"},
		},
	}

	if err := writeHookSkeletons(dir, report, "myschema"); err != nil {
		t.Fatalf("writeHookSkeletons: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "after_data.sql"))
	if err != nil {
		t.Fatalf("read after_data.sql: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "{{schema}}") {
		t.Error("after_data.sql should contain {{schema}} placeholder")
	}
	if !strings.Contains(content, `"{{schema}}"."users"`) {
		t.Error(`after_data.sql should quote the schema placeholder and table identifier`)
	}
	if !strings.Contains(content, "display_name") {
		t.Error("after_data.sql should mention the generated column")
	}
	if !strings.Contains(content, "concat(`first`,`last`)") {
		t.Error("after_data.sql should mention the source expression")
	}
}

func TestWriteHookSkeletons_AfterAll(t *testing.T) {
	dir := t.TempDir()
	report := &PlanReport{
		SourceObjects: PlanSourceObjects{
			Views:    []string{"v_summary"},
			Routines: []string{"FUNCTION calc"},
			Triggers: []string{"trg_audit"},
		},
		SkippedIndexes: []PlanSkippedIndex{
			{Table: "orders", Index: "idx_ft", Reason: "FULLTEXT not supported"},
		},
	}

	if err := writeHookSkeletons(dir, report, "app"); err != nil {
		t.Fatalf("writeHookSkeletons: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "after_all.sql"))
	if err != nil {
		t.Fatalf("read after_all.sql: %v", err)
	}
	content := string(data)

	for _, want := range []string{
		"v_summary",
		"FUNCTION calc",
		"trg_audit",
		"idx_ft",
		"{{schema}}",
		`"{{schema}}"."v_summary"`,
		`"{{schema}}"."orders"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("after_all.sql missing %q", want)
		}
	}
}

func TestWriteHookSkeletons_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "hooks")
	report := &PlanReport{
		SourceObjects: PlanSourceObjects{
			Views: []string{"v_test"},
		},
	}

	if err := writeHookSkeletons(dir, report, "public"); err != nil {
		t.Fatalf("writeHookSkeletons: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "after_all.sql")); err != nil {
		t.Errorf("expected after_all.sql to exist in nested dir: %v", err)
	}
}

func TestBuildPlanReport_NilSourceObjects(t *testing.T) {
	schema := &Schema{}
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}

	report := buildPlanReport(schema, nil, mysqlSrc, cfg)

	if len(report.SourceObjects.Views) != 0 {
		t.Errorf("views should be empty, got %v", report.SourceObjects.Views)
	}
	if len(report.SourceObjects.Routines) != 0 {
		t.Errorf("routines should be empty, got %v", report.SourceObjects.Routines)
	}
	if len(report.SourceObjects.Triggers) != 0 {
		t.Errorf("triggers should be empty, got %v", report.SourceObjects.Triggers)
	}
}

func TestWritePlanJSON_EmptySlices(t *testing.T) {
	schema := &Schema{}
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}
	report := buildPlanReport(schema, nil, mysqlSrc, cfg)

	var buf bytes.Buffer
	if err := writePlanJSON(&buf, report); err != nil {
		t.Fatalf("writePlanJSON: %v", err)
	}

	output := buf.String()

	// Verify empty slices serialize as [] not null
	if strings.Contains(output, ": null") {
		t.Errorf("JSON output contains null values, expected empty arrays:\n%s", output)
	}

	// Source objects should be present as an object
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if _, ok := decoded["source_objects"]; !ok {
		t.Error("missing source_objects key")
	}
}

func TestBuildPlanReport_RequiredExtensionsAndUnsupportedColumns(t *testing.T) {
	cfg := &MigrationConfig{
		TypeMapping: defaultTypeMappingConfig(),
		PostGIS:     PostGISConfig{Enabled: true},
	}
	cfg.TypeMapping.CIAsCitext = true

	schema := &Schema{
		Tables: []Table{
			{
				PGName: "places",
				Columns: []Column{
					{SourceName: "name", PGName: "name", DataType: "varchar", ColumnType: "varchar(100)", CharMaxLen: 100, Collation: "utf8mb4_general_ci"},
					{SourceName: "shape", PGName: "shape", DataType: "point", ColumnType: "point"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, mysqlSrc, cfg)
	if len(report.RequiredExtensions) != 2 {
		t.Fatalf("required extensions = %d, want 2", len(report.RequiredExtensions))
	}
	if len(report.UnsupportedColumns) != 0 {
		t.Fatalf("unsupported columns = %d, want 0", len(report.UnsupportedColumns))
	}
}

func TestBuildPlanReport_PostGISDisabledMarksSpatialUnsupported(t *testing.T) {
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "places",
				Columns: []Column{
					{SourceName: "shape", PGName: "shape", DataType: "geometry", ColumnType: "geometry"},
				},
				Indexes: []Index{
					{Name: "idx_shape", SourceName: "idx_shape", Type: "SPATIAL", Columns: []string{"shape"}},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, mysqlSrc, cfg)
	if len(report.UnsupportedColumns) != 1 {
		t.Fatalf("unsupported columns = %d, want 1", len(report.UnsupportedColumns))
	}
	if len(report.SkippedIndexes) != 1 {
		t.Fatalf("skipped indexes = %d, want 1", len(report.SkippedIndexes))
	}
	if !strings.Contains(report.SkippedIndexes[0].Reason, "[postgis].enabled") {
		t.Fatalf("skipped index reason = %q, want postgis hint", report.SkippedIndexes[0].Reason)
	}
}
