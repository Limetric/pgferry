package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildPlanReport_Empty(t *testing.T) {
	schema := &Schema{}
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig()}

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

	if len(report.SourceObjects.Views) != 0 {
		t.Errorf("views = %d, want 0", len(report.SourceObjects.Views))
	}
	if len(report.GeneratedColumns) != 0 {
		t.Errorf("generated columns = %d, want 0", len(report.GeneratedColumns))
	}
	if len(report.SkippedIndexes) != 0 {
		t.Errorf("skipped indexes = %d, want 0", len(report.SkippedIndexes))
	}
	if len(report.OrphanCleanupCandidates) != 0 {
		t.Errorf("orphan cleanup candidates = %d, want 0", len(report.OrphanCleanupCandidates))
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
				ForeignKeys: []ForeignKey{
					{Name: "fk_users_account", Columns: []string{"account_id"}, RefPGTable: "accounts", RefColumns: []string{"id"}, DeleteRule: "SET NULL"},
				},
			},
		},
	}
	objs := &SourceObjects{
		Views:    []string{"v_active_users"},
		Routines: []string{"FUNCTION calc_score"},
		Triggers: []string{"trg_audit"},
	}
	cfg := &MigrationConfig{TypeMapping: defaultTypeMappingConfig(), CleanOrphans: true}

	report := buildPlanReport(schema, objs, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

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
	if len(report.OrphanCleanupCandidates) != 1 {
		t.Fatalf("orphan cleanup candidates = %d, want 1", len(report.OrphanCleanupCandidates))
	}
	if report.OrphanCleanupCandidates[0].Action != "set_null" {
		t.Errorf("orphan cleanup candidate action = %q, want %q", report.OrphanCleanupCandidates[0].Action, "set_null")
	}
}

func TestBuildPlanReport_CollectsDefaultSemanticWarnings(t *testing.T) {
	cfg := &MigrationConfig{
		Source:           SourceConfig{Type: "sqlite"},
		PreserveDefaults: true,
		TypeMapping:      defaultTypeMappingConfig(),
	}
	defaultExpr := "(datetime('now'))"
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{
						SourceName: "created_at",
						PGName:     "created_at",
						DataType:   "datetime",
						ColumnType: "DATETIME",
						Default:    &defaultExpr,
					},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &sqliteSourceDB{}, cfg, effectiveTypeMapping(cfg))

	if len(report.SchemaSemanticWarnings) != 1 {
		t.Fatalf("schema semantic warnings = %d, want 1", len(report.SchemaSemanticWarnings))
	}
	got := report.SchemaSemanticWarnings[0]
	if got.Category != "defaults" {
		t.Fatalf("category = %q, want defaults", got.Category)
	}
	if got.ObjectName != "events.created_at" {
		t.Fatalf("object name = %q, want events.created_at", got.ObjectName)
	}
	if !strings.Contains(got.Reason, "datetime('now')") {
		t.Fatalf("reason = %q, want skipped default detail", got.Reason)
	}
}

func TestBuildPlanReport_SuppressesDefaultSemanticWarningsWhenPreserveDefaultsDisabled(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "sqlite"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	defaultExpr := "(datetime('now'))"
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{
						SourceName: "created_at",
						PGName:     "created_at",
						DataType:   "datetime",
						ColumnType: "DATETIME",
						Default:    &defaultExpr,
					},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &sqliteSourceDB{}, cfg, effectiveTypeMapping(cfg))

	if len(report.SchemaSemanticWarnings) != 0 {
		t.Fatalf("schema semantic warnings = %d, want 0", len(report.SchemaSemanticWarnings))
	}
}

func TestCollectDefaultSemanticWarning_EdgeCases(t *testing.T) {
	type testCase struct {
		name    string
		col     Column
		wantOK  bool
		wantRaw string
	}

	defaultExpr := "(datetime('now'))"
	emptyDefault := ""
	nullDefault := "null"

	tests := []testCase{
		{
			name:   "nil default",
			col:    Column{SourceName: "created_at", PGName: "created_at", DataType: "datetime", ColumnType: "DATETIME"},
			wantOK: false,
		},
		{
			name:   "empty default",
			col:    Column{SourceName: "created_at", PGName: "created_at", DataType: "datetime", ColumnType: "DATETIME", Default: &emptyDefault},
			wantOK: false,
		},
		{
			name:   "null default",
			col:    Column{SourceName: "created_at", PGName: "created_at", DataType: "datetime", ColumnType: "DATETIME", Default: &nullDefault},
			wantOK: false,
		},
		{
			name:    "expression default",
			col:     Column{SourceName: "created_at", PGName: "created_at", DataType: "datetime", ColumnType: "DATETIME", Default: &defaultExpr},
			wantOK:  true,
			wantRaw: "datetime('now')",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := collectDefaultSemanticWarning(
				Table{PGName: "events"},
				tt.col,
				&sqliteSourceDB{},
				defaultTypeMappingConfig(),
			)
			if ok != tt.wantOK {
				t.Fatalf("ok = %t, want %t", ok, tt.wantOK)
			}
			if tt.wantRaw != "" && !strings.Contains(got.Reason, tt.wantRaw) {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantRaw)
			}
		})
	}
}

func TestBuildPlanReport_MergesIntrospectedSchemaSemanticWarnings(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mysql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	report := buildPlanReport(
		&Schema{
			Tables: []Table{
				{SourceName: "Orders", PGName: "orders"},
			},
		},
		nil,
		[]SchemaSemanticWarning{
			{
				Category:            "constraints",
				ObjectType:          "constraint",
				ObjectName:          "orders.chk_total",
				Disposition:         "skipped",
				Reason:              `MySQL CHECK constraint "chk_total" is not migrated automatically.`,
				RecommendedFollowUp: "Recreate the CHECK constraint in PostgreSQL DDL or hook SQL after loading data.",
			},
		},
		nil,
		mysqlSrc,
		cfg,
		effectiveTypeMapping(cfg),
	)

	if len(report.SchemaSemanticWarnings) != 1 {
		t.Fatalf("schema semantic warnings = %d, want 1", len(report.SchemaSemanticWarnings))
	}
	if got := report.SchemaSemanticWarnings[0].ObjectName; got != "orders.chk_total" {
		t.Fatalf("object name = %q, want orders.chk_total", got)
	}
}

func TestBuildPlanReport_FiltersSchemaSemanticWarningsToSelectedTables(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mysql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	report := buildPlanReport(
		&Schema{
			Tables: []Table{
				{SourceName: "Orders", PGName: "orders"},
			},
		},
		nil,
		[]SchemaSemanticWarning{
			{
				Category:            "constraints",
				ObjectType:          "constraint",
				ObjectName:          "orders.chk_total",
				Disposition:         "skipped",
				Reason:              `MySQL CHECK constraint "chk_total" is not migrated automatically.`,
				RecommendedFollowUp: "Recreate the CHECK constraint in PostgreSQL DDL or hook SQL after loading data.",
			},
			{
				Category:            "comments",
				ObjectType:          "column",
				ObjectName:          "customers.email",
				Disposition:         "skipped",
				Reason:              `MySQL column comment "Primary email" is not migrated automatically.`,
				RecommendedFollowUp: "Recreate the comment with PostgreSQL COMMENT ON statements if operators rely on it.",
			},
			{
				Category:            "constraints",
				ObjectType:          "schema",
				ObjectName:          "",
				Disposition:         "unavailable",
				Reason:              "MySQL CHECK constraint metadata is unavailable on this server, so pgferry could not inspect source CHECK constraints automatically.",
				RecommendedFollowUp: "Review source CHECK constraints manually if the schema relies on them.",
			},
		},
		nil,
		mysqlSrc,
		cfg,
		effectiveTypeMapping(cfg),
	)

	if len(report.SchemaSemanticWarnings) != 2 {
		t.Fatalf("schema semantic warnings = %d, want 2", len(report.SchemaSemanticWarnings))
	}
	if got := report.SchemaSemanticWarnings[0].ObjectName; got != "orders.chk_total" {
		t.Fatalf("first warning object = %q, want orders.chk_total", got)
	}
	if got := report.SchemaSemanticWarnings[1].Disposition; got != "unavailable" {
		t.Fatalf("second warning disposition = %q, want unavailable", got)
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
		CopyRiskFindings: []PlanCopyRiskFinding{
			{
				Category:            "poor_range_density",
				Severity:            "medium",
				Table:               "sessions",
				Chunkable:           true,
				ChunkKey:            "id",
				ChunkKeyType:        "bigint",
				Reason:              "The chunk key range 1..1000000 spans 1000000 possible values for 1000 rows (0.10% density), so many chunks may be mostly empty.",
				EstimatedRows:       1000,
				MinPK:               int64Ptr(1),
				MaxPK:               int64Ptr(1_000_000),
				EstimatedChunkCount: 100,
				RangeDensity:        0.001,
				Recommendation:      "Validate throughput on production-like data.",
			},
		},
		SourceObjects: PlanSourceObjects{
			Views: []string{"v_users"},
		},
		UnsupportedColumns: []PlanUnsupportedColumn{
			{Table: "mystery", Column: "payload", SourceType: "geometry", Reason: "unsupported MySQL type \"geometry\""},
		},
		SchemaSemanticWarnings: []SchemaSemanticWarning{
			{
				Category:            "defaults",
				ObjectType:          "column",
				ObjectName:          "events.created_at",
				Disposition:         "skipped",
				Reason:              `SQLite default "(datetime('now'))" is not recreated automatically and will be omitted from the PostgreSQL column definition.`,
				RecommendedFollowUp: "Recreate the PostgreSQL DEFAULT manually if future inserts depend on it.",
			},
		},
		GeneratedColumns: []PlanGeneratedColumn{
			{Table: "orders", Column: "total", Expression: "VIRTUAL GENERATED"},
		},
		SkippedIndexes: []PlanSkippedIndex{
			{Table: "products", Index: "idx_ft_name", Reason: "index type \"FULLTEXT\" is not supported"},
		},
		OrphanCleanupCandidates: []PlanOrphanCleanupCandidate{
			{Table: "orders", ForeignKey: "fk_orders_customer", Columns: []string{"customer_id"}, RefTable: "customers", RefColumns: []string{"id"}, Action: "delete"},
		},
		TemporalWarnings: []PlanTemporalWarning{
			{
				Category:    "mysql_datetime_without_timezone",
				Summary:     "2 MySQL datetime column(s) will map to PostgreSQL timestamp without timezone semantics; review whether type_mapping.datetime_as_timestamptz = true is more appropriate.",
				Columns:     2,
				Examples:    []string{"orders.created_at", "orders.updated_at"},
				Remediation: `Use type_mapping.datetime_as_timestamptz = true when those values represent real instants instead of local wall-clock timestamps.`,
			},
		},
	}

	var buf bytes.Buffer
	writePlanText(&buf, report)
	got := buf.String()

	for _, want := range []string{
		"## Required Extensions (1)",
		"citext",
		"create it if missing",
		"## Copy Risk Findings (1)",
		"sessions [MEDIUM] Poor Range Density",
		"eligible on id (bigint)",
		"estimated_chunks=100",
		"density=0.10%",
		"Validate throughput on production-like data.",
		"## Source Objects",
		"v_users",
		"after_all",
		"## Unsupported Columns (1)",
		"mystery.payload",
		"## Schema Semantic Warnings (1)",
		"Defaults (1):",
		`events.created_at [skipped]`,
		`SQLite default "(datetime('now'))"`,
		"## Generated Columns (1)",
		"orders.total",
		"after_data",
		"## Skipped Indexes (1)",
		"products.idx_ft_name",
		"## Orphan Cleanup Candidates (1)",
		"orders.fk_orders_customer",
		"DELETE",
		"## Temporal Warnings (1)",
		"orders.created_at",
		"type_mapping.datetime_as_timestamptz = true",
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
		CopyRiskFindings: []PlanCopyRiskFinding{
			{
				Category:            "high_chunk_count",
				Severity:            "high",
				Table:               "events",
				Chunkable:           true,
				ChunkKey:            "id",
				ChunkKeyType:        "int",
				Reason:              "The current chunk plan is estimated to create 200 chunks across 20000000 rows for PK range 1..20000000.",
				EstimatedRows:       20_000_000,
				MinPK:               int64Ptr(1),
				MaxPK:               int64Ptr(20_000_000),
				EstimatedChunkCount: 200,
				RangeDensity:        1,
				Recommendation:      "Benchmark the table separately.",
			},
		},
		SourceObjects: PlanSourceObjects{
			Views:    []string{"v_users"},
			Routines: []string{"FUNCTION foo"},
		},
		SchemaSemanticWarnings: []SchemaSemanticWarning{
			{
				Category:            "constraints",
				ObjectType:          "constraint",
				ObjectName:          "orders.chk_total",
				Disposition:         "skipped",
				Reason:              `MySQL CHECK constraint "chk_total" is not migrated automatically. Definition: total >= 0`,
				RecommendedFollowUp: "Recreate the CHECK constraint in PostgreSQL DDL or hook SQL after loading data.",
			},
		},
		GeneratedColumns: []PlanGeneratedColumn{
			{Table: "t1", Column: "c1", Expression: "STORED GENERATED"},
		},
		SkippedIndexes: []PlanSkippedIndex{
			{Table: "t2", Index: "idx_x", Reason: "prefix indexes (SUB_PART) are not currently supported"},
		},
		OrphanCleanupCandidates: []PlanOrphanCleanupCandidate{
			{Table: "child", ForeignKey: "fk_child_parent", Columns: []string{"parent_id"}, RefTable: "parent", RefColumns: []string{"id"}, Action: "delete"},
		},
		TemporalWarnings: []PlanTemporalWarning{
			{
				Category:    "mysql_time_mode_time",
				Summary:     "1 MySQL TIME column(s) will map to PostgreSQL time; negative durations or values outside 00:00:00-23:59:59 can fail or drift semantically.",
				Columns:     1,
				Examples:    []string{"sessions.elapsed"},
				Remediation: `Use type_mapping.time_mode = "interval" for durations or "text" to preserve source literals exactly.`,
			},
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
	if len(decoded.CopyRiskFindings) != 1 {
		t.Errorf("copy risk findings = %d", len(decoded.CopyRiskFindings))
	}
	if len(decoded.GeneratedColumns) != 1 {
		t.Errorf("generated columns = %d", len(decoded.GeneratedColumns))
	}
	if len(decoded.SchemaSemanticWarnings) != 1 {
		t.Errorf("schema semantic warnings = %d", len(decoded.SchemaSemanticWarnings))
	}
	if len(decoded.SkippedIndexes) != 1 {
		t.Errorf("skipped indexes = %d", len(decoded.SkippedIndexes))
	}
	if len(decoded.OrphanCleanupCandidates) != 1 {
		t.Errorf("orphan cleanup candidates = %d", len(decoded.OrphanCleanupCandidates))
	}
	if len(decoded.TemporalWarnings) != 1 {
		t.Errorf("temporal warnings = %d", len(decoded.TemporalWarnings))
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

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

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
	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

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

func TestRunPlanWithConfig_CopyRiskAnalysisDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan-copy-risk.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, payload) VALUES (1, 'a'), (1000000, 'b')`); err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	prevFormat := planFormat
	planFormat = "json"
	t.Cleanup(func() {
		planFormat = prevFormat
	})

	cfg := &MigrationConfig{
		Schema:           "app",
		Source:           SourceConfig{Type: "sqlite", DSN: dbPath},
		CopyRiskAnalysis: false,
		TypeMapping:      defaultTypeMappingConfig(),
	}

	var buf bytes.Buffer
	if err := runPlanWithConfig(cfg, &buf, PlanOptions{Format: planFormat}); err != nil {
		t.Fatalf("runPlanWithConfig() error: %v", err)
	}

	var report PlanReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if len(report.CopyRiskFindings) != 0 {
		t.Fatalf("copy risk findings = %d, want 0", len(report.CopyRiskFindings))
	}
}

func TestRunPlanFailOnErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan-fail-on-errors.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE weird (id INTEGER PRIMARY KEY, x FROBNOZZ)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	cfg := &MigrationConfig{
		Schema:      "app",
		Source:      SourceConfig{Type: "sqlite", DSN: dbPath},
		TypeMapping: defaultTypeMappingConfig(),
	}

	var buf bytes.Buffer
	err = runPlanWithConfig(cfg, &buf, PlanOptions{Format: "text", FailOn: "errors"})
	var pf *PlanFindingsError
	if !errors.As(err, &pf) {
		t.Fatalf("runPlanWithConfig() error = %v, want *PlanFindingsError", err)
	}
	if pf.UnsupportedColumns != 1 {
		t.Fatalf("UnsupportedColumns = %d, want 1", pf.UnsupportedColumns)
	}
	out := buf.String()
	if !strings.Contains(out, "Unsupported Columns") || !strings.Contains(out, "FAIL: 1 unsupported column(s)") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestRunPlanFailOnWarnings_CopyRiskHigh(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan-fail-on-warnings.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, payload) VALUES (1, 'a'), (127001, 'b')`); err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	cfg := &MigrationConfig{
		Schema:           "app",
		Source:           SourceConfig{Type: "sqlite", DSN: dbPath},
		CopyRiskAnalysis: true,
		ChunkSize:        1000,
		TypeMapping:      defaultTypeMappingConfig(),
	}

	var buf bytes.Buffer
	err = runPlanWithConfig(cfg, &buf, PlanOptions{Format: "text", FailOn: "warnings"})
	var pf *PlanFindingsError
	if !errors.As(err, &pf) {
		t.Fatalf("runPlanWithConfig() error = %v, want *PlanFindingsError", err)
	}
	if pf.HighSeverityRisks == 0 {
		t.Fatalf("HighSeverityRisks = %d, want > 0", pf.HighSeverityRisks)
	}
	if !strings.Contains(buf.String(), "FAIL:") {
		t.Fatalf("expected FAIL summary in output:\n%s", buf.String())
	}
}

func TestRunPlanFailOnNone(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan-fail-on-none.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE weird (id INTEGER PRIMARY KEY, x FROBNOZZ)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	cfg := &MigrationConfig{
		Schema:      "app",
		Source:      SourceConfig{Type: "sqlite", DSN: dbPath},
		TypeMapping: defaultTypeMappingConfig(),
	}

	var buf bytes.Buffer
	if err := runPlanWithConfig(cfg, &buf, PlanOptions{Format: "text", FailOn: "none"}); err != nil {
		t.Fatalf("runPlanWithConfig() error: %v", err)
	}
	if !strings.Contains(buf.String(), "Unsupported Columns") {
		t.Fatalf("expected unsupported section in output:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "FAIL:") {
		t.Fatalf("did not want FAIL line:\n%s", buf.String())
	}
}

func TestRunPlanFailOnErrors_IgnoresCopyRiskOnly(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan-fail-on-errors-copy.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, payload) VALUES (1, 'a'), (127001, 'b')`); err != nil {
		t.Fatalf("insert rows: %v", err)
	}

	cfg := &MigrationConfig{
		Schema:           "app",
		Source:           SourceConfig{Type: "sqlite", DSN: dbPath},
		CopyRiskAnalysis: true,
		ChunkSize:        1000,
		TypeMapping:      defaultTypeMappingConfig(),
	}

	var buf bytes.Buffer
	if err := runPlanWithConfig(cfg, &buf, PlanOptions{Format: "text", FailOn: "errors"}); err != nil {
		t.Fatalf("runPlanWithConfig() error: %v", err)
	}
	if !strings.Contains(buf.String(), "Copy Risk Findings") {
		t.Fatalf("expected copy risk section:\n%s", buf.String())
	}
}

func TestRunPlanFailOn_JSONWrittenBeforeError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan-fail-on-json.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE weird (id INTEGER PRIMARY KEY, x FROBNOZZ)`); err != nil {
		t.Fatalf("create table: %v", err)
	}

	cfg := &MigrationConfig{
		Schema:      "app",
		Source:      SourceConfig{Type: "sqlite", DSN: dbPath},
		TypeMapping: defaultTypeMappingConfig(),
	}

	var buf bytes.Buffer
	err = runPlanWithConfig(cfg, &buf, PlanOptions{Format: "json", FailOn: "errors"})
	var pf *PlanFindingsError
	if !errors.As(err, &pf) {
		t.Fatalf("runPlanWithConfig() error = %v, want *PlanFindingsError", err)
	}
	var report PlanReport
	if err := json.Unmarshal(buf.Bytes(), &report); err != nil {
		t.Fatalf("decode report JSON: %v", err)
	}
	if len(report.UnsupportedColumns) != 1 {
		t.Fatalf("unsupported columns = %d, want 1", len(report.UnsupportedColumns))
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

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))
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

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))
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

func TestBuildPlanReport_TemporalWarnings_MySQL(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mysql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "duration", DataType: "time"},
					{PGName: "opened_at", DataType: "datetime"},
					{PGName: "replicated_at", DataType: "timestamp"},
					{PGName: "business_date", DataType: "date"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 4 {
		t.Fatalf("temporal warnings = %d, want 4", len(report.TemporalWarnings))
	}

	gotCategories := make([]string, 0, len(report.TemporalWarnings))
	for _, warning := range report.TemporalWarnings {
		gotCategories = append(gotCategories, warning.Category)
	}

	for _, want := range []string{
		"mysql_time_mode_time",
		"mysql_zero_date_mode_null",
		"mysql_datetime_without_timezone",
		"mysql_timestamp_to_timestamptz",
	} {
		if !slices.Contains(gotCategories, want) {
			t.Fatalf("missing temporal warning category %q in %v", want, gotCategories)
		}
	}
}

func TestBuildPlanReport_TemporalWarnings_MySQLIntervalMode(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mysql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	cfg.TypeMapping.TimeMode = "interval"

	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "duration", DataType: "time"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 1 {
		t.Fatalf("temporal warnings = %d, want 1", len(report.TemporalWarnings))
	}
	if got := report.TemporalWarnings[0].Category; got != "mysql_time_mode_interval" {
		t.Fatalf("time warning category = %q, want mysql_time_mode_interval", got)
	}
	if strings.Contains(report.TemporalWarnings[0].Summary, "00:00:00-23:59:59") {
		t.Fatalf("interval warning should not use the default time-mode wording: %q", report.TemporalWarnings[0].Summary)
	}
}

func TestBuildPlanReport_TemporalWarnings_MySQLTextModeSuppressesTimeWarning(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mysql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	cfg.TypeMapping.TimeMode = "text"

	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "duration", DataType: "time"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 0 {
		t.Fatalf("temporal warnings = %d, want 0", len(report.TemporalWarnings))
	}
}

func TestBuildPlanReport_TemporalWarnings_MySQLDatetimeAsTimestamptz(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mysql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	cfg.TypeMapping.DatetimeAsTimestamptz = true

	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "opened_at", DataType: "datetime"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, mysqlSrc, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 2 {
		t.Fatalf("temporal warnings = %d, want 2", len(report.TemporalWarnings))
	}
	gotCategories := make([]string, 0, len(report.TemporalWarnings))
	for _, warning := range report.TemporalWarnings {
		gotCategories = append(gotCategories, warning.Category)
	}
	for _, want := range []string{"mysql_zero_date_mode_null", "mysql_datetime_to_timestamptz"} {
		if !slices.Contains(gotCategories, want) {
			t.Fatalf("missing temporal warning category %q in %v", want, gotCategories)
		}
	}
}

func TestBuildPlanReport_TemporalWarnings_MariaDB(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mariadb"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "duration", DataType: "time"},
					{PGName: "opened_at", DataType: "datetime"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &mariadbSourceDB{}, cfg, effectiveTypeMapping(cfg))

	gotCategories := make([]string, 0, len(report.TemporalWarnings))
	for _, warning := range report.TemporalWarnings {
		gotCategories = append(gotCategories, warning.Category)
	}

	for _, want := range []string{
		"mariadb_time_mode_time",
		"mariadb_zero_date_mode_null",
		"mariadb_datetime_without_timezone",
	} {
		if !slices.Contains(gotCategories, want) {
			t.Fatalf("missing temporal warning category %q in %v", want, gotCategories)
		}
	}
	if len(report.TemporalWarnings) != 3 {
		t.Fatalf("temporal warnings = %d, want 3", len(report.TemporalWarnings))
	}
}

func TestBuildPlanReport_TemporalWarnings_MSSQL(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mssql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "audit_log",
				Columns: []Column{
					{PGName: "created_at", DataType: "datetime2"},
					{PGName: "recorded_at", DataType: "datetimeoffset"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &mssqlSourceDB{}, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 2 {
		t.Fatalf("temporal warnings = %d, want 2", len(report.TemporalWarnings))
	}
	gotCategories := make([]string, 0, len(report.TemporalWarnings))
	for _, warning := range report.TemporalWarnings {
		gotCategories = append(gotCategories, warning.Category)
	}
	for _, want := range []string{"mssql_datetime_without_timezone", "mssql_datetimeoffset_to_timestamptz"} {
		if !slices.Contains(gotCategories, want) {
			t.Fatalf("missing temporal warning category %q in %v", want, gotCategories)
		}
	}
}

func TestBuildPlanReport_TemporalWarnings_MSSQLDatetimeAsTimestamptz(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "mssql"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	cfg.TypeMapping.DatetimeAsTimestamptz = true

	schema := &Schema{
		Tables: []Table{
			{
				PGName: "audit_log",
				Columns: []Column{
					{PGName: "created_at", DataType: "datetime2"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &mssqlSourceDB{}, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 1 {
		t.Fatalf("temporal warnings = %d, want 1", len(report.TemporalWarnings))
	}
	if report.TemporalWarnings[0].Category != "mssql_datetime_to_timestamptz" {
		t.Fatalf("warning category = %q", report.TemporalWarnings[0].Category)
	}
}

func TestBuildPlanReport_TemporalWarnings_SQLiteNone(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "sqlite"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	cfg.TypeMapping.DatetimeAsTimestamptz = false
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "duration", DataType: "TIME"},
					{PGName: "opened_at", DataType: "DATETIME"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &sqliteSourceDB{}, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 0 {
		t.Fatalf("temporal warnings = %d, want 0", len(report.TemporalWarnings))
	}
}

func TestBuildPlanReport_TemporalWarnings_SQLiteDatetimeAsTimestamptz(t *testing.T) {
	cfg := &MigrationConfig{
		Source:      SourceConfig{Type: "sqlite"},
		TypeMapping: defaultTypeMappingConfig(),
	}
	cfg.TypeMapping.DatetimeAsTimestamptz = true

	schema := &Schema{
		Tables: []Table{
			{
				PGName: "events",
				Columns: []Column{
					{PGName: "opened_at", DataType: "DATETIME"},
					{PGName: "updated_at", DataType: "timestamp"},
				},
			},
		},
	}

	report := buildPlanReport(schema, nil, nil, nil, &sqliteSourceDB{}, cfg, effectiveTypeMapping(cfg))

	if len(report.TemporalWarnings) != 1 {
		t.Fatalf("temporal warnings = %d, want 1", len(report.TemporalWarnings))
	}
	w := report.TemporalWarnings[0]
	if w.Category != "sqlite_datetime_to_timestamptz" {
		t.Fatalf("warning category = %q", w.Category)
	}
	if w.Columns != 2 {
		t.Fatalf("warning columns = %d, want 2", w.Columns)
	}
}
