package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointCompatibilityFingerprint_Deterministic(t *testing.T) {
	summary := checkpointCompatibilitySummary{
		SourceType:   "mysql",
		SourceDBName: "appdb",
		TargetSchema: "public",
		ChunkSize:    100000,
		TypeMapping:  defaultTypeMappingConfig(),
	}

	fp1, err := checkpointCompatibilityFingerprint(summary)
	if err != nil {
		t.Fatal(err)
	}
	fp2, err := checkpointCompatibilityFingerprint(summary)
	if err != nil {
		t.Fatal(err)
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprint not deterministic: %q vs %q", fp1, fp2)
	}
	if fp1 == "" {
		t.Fatal("fingerprint should not be empty")
	}
}

func TestCheckpointCompatibilityFingerprint_DifferentConfigsProduceDifferentFingerprints(t *testing.T) {
	base := checkpointCompatibilitySummary{
		SourceType:   "mysql",
		SourceDBName: "appdb",
		TargetSchema: "public",
		ChunkSize:    100000,
		TypeMapping:  defaultTypeMappingConfig(),
	}

	fp1, _ := checkpointCompatibilityFingerprint(base)

	modified := base
	modified.ChunkSize = 50000
	fp2, _ := checkpointCompatibilityFingerprint(modified)

	if fp1 == fp2 {
		t.Fatal("different chunk sizes should produce different fingerprints")
	}
}

func TestHashCheckpointTable_DeterministicAndDistinct(t *testing.T) {
	t1 := Table{
		SourceName: "users",
		PGName:     "users",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int"},
			{SourceName: "name", PGName: "name", DataType: "varchar"},
		},
	}

	h1, err := hashCheckpointTable(t1)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := hashCheckpointTable(t1)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}

	// Different column type should produce a different hash
	t2 := t1
	t2.Columns = []Column{
		{SourceName: "id", PGName: "id", DataType: "bigint"},
		{SourceName: "name", PGName: "name", DataType: "varchar"},
	}
	h3, err := hashCheckpointTable(t2)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h3 {
		t.Fatal("different column types should produce different hashes")
	}

	// Different nullability should produce a different hash
	t3 := t1
	t3.Columns = []Column{
		{SourceName: "id", PGName: "id", DataType: "int"},
		{SourceName: "name", PGName: "name", DataType: "varchar", Nullable: true},
	}
	h4, err := hashCheckpointTable(t3)
	if err != nil {
		t.Fatal(err)
	}
	if h1 == h4 {
		t.Fatal("different nullability should produce different hashes")
	}
}

func TestHashCheckpointTable_IncludesPrimaryKey(t *testing.T) {
	base := Table{
		SourceName: "users",
		PGName:     "users",
		Columns:    []Column{{SourceName: "id", PGName: "id", DataType: "int"}},
	}

	withPK := base
	withPK.PrimaryKey = &Index{Columns: []string{"id"}}

	h1, _ := hashCheckpointTable(base)
	h2, _ := hashCheckpointTable(withPK)
	if h1 == h2 {
		t.Fatal("adding a primary key should change the table hash")
	}
}

func TestValidateCheckpointCompatibility_MatchingFingerprints(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, compat)
	if err != nil {
		t.Fatalf("expected no error for matching fingerprints, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_NilState(t *testing.T) {
	compat := testCheckpointCompatibility()
	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), nil, compat)
	if err != nil {
		t.Fatalf("expected no error for nil state, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_EmptyExpectedFingerprint(t *testing.T) {
	state := newCheckpointState()
	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, checkpointCompatibility{})
	if err != nil {
		t.Fatalf("expected no error for empty expected fingerprint, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_SourceTypeChanged(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	changed := *compat.Summary
	changed.SourceType = "mariadb"
	changedCompat := testCheckpointCompatibilityWithSummary(changed)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, changedCompat)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source type changed") {
		t.Fatalf("expected source type change message, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_DatabaseChanged(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	changed := *compat.Summary
	changed.SourceDBName = "otherdb"
	changedCompat := testCheckpointCompatibilityWithSummary(changed)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, changedCompat)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source database changed") {
		t.Fatalf("expected database change message, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_TargetSchemaChanged(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	changed := *compat.Summary
	changed.TargetSchema = "other_schema"
	changedCompat := testCheckpointCompatibilityWithSummary(changed)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, changedCompat)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "target schema changed") {
		t.Fatalf("expected target schema change message, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_SnakeCaseChanged(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	changed := *compat.Summary
	changed.SnakeCaseIdentifiers = !changed.SnakeCaseIdentifiers
	changedCompat := testCheckpointCompatibilityWithSummary(changed)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, changedCompat)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "snake_case_identifiers changed") {
		t.Fatalf("expected snake_case change message, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_TypeMappingChanged(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	changed := *compat.Summary
	changed.TypeMapping.TinyInt1AsBoolean = true
	changedCompat := testCheckpointCompatibilityWithSummary(changed)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, changedCompat)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tinyint1_as_boolean changed") {
		t.Fatalf("expected type mapping change message, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_MissingSummary(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	noSummary := checkpointCompatibility{
		Fingerprint: "different-fp",
	}

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, noSummary)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing resume compatibility metadata") {
		t.Fatalf("expected missing metadata message, got: %v", err)
	}
}

func TestValidateCheckpointCompatibility_MaxReasonsLimited(t *testing.T) {
	compat := testCheckpointCompatibility()
	state := newCheckpointStateWithCompatibility(&compat)

	// Change 10 individually valid fields to produce > 8 diff reasons,
	// exercising the truncation logic that caps output at maxReasons.
	changed := *compat.Summary
	changed.SourceType = "mssql"
	changed.SourceDBName = "otherdb"
	changed.SourceSchema = "sales"
	changed.TargetSchema = "other"
	changed.SourceSnapshotMode = "single_tx"
	changed.ChunkSize = 1
	changed.SnakeCaseIdentifiers = true
	changed.UnloggedTables = true
	changed.TypeMapping.TinyInt1AsBoolean = true
	changed.TypeMapping.Binary16AsUUID = true
	changedCompat := testCheckpointCompatibilityWithSummary(changed)

	err := validateCheckpointCompatibility(filepath.Join(t.TempDir(), "cp.json"), state, changedCompat)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "more compatibility differences omitted") {
		t.Fatalf("expected truncation message, got: %v", err)
	}
}

func TestCheckpointTypeMappingDiff_NoChanges(t *testing.T) {
	tm := defaultTypeMappingConfig()
	reasons := checkpointTypeMappingDiff(tm, tm)
	if len(reasons) != 0 {
		t.Fatalf("expected 0 reasons, got %d: %v", len(reasons), reasons)
	}
}

func TestCheckpointTypeMappingDiff_DetectsAllFieldChanges(t *testing.T) {
	saved := defaultTypeMappingConfig()
	current := defaultTypeMappingConfig()
	current.TinyInt1AsBoolean = true
	current.Binary16AsUUID = true
	current.DatetimeAsTimestamptz = true
	current.JSONAsJSONB = false

	reasons := checkpointTypeMappingDiff(saved, current)
	if len(reasons) < 4 {
		t.Fatalf("expected at least 4 reasons, got %d: %v", len(reasons), reasons)
	}

	for _, want := range []string{"tinyint1_as_boolean", "binary16_as_uuid", "datetime_as_timestamptz", "json_as_jsonb"} {
		found := false
		for _, r := range reasons {
			if strings.Contains(r, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing reason for %s", want)
		}
	}
}

func TestCheckpointCollationMapDiff_NilVsEmpty(t *testing.T) {
	// nil and empty map should produce no differences
	reasons := checkpointCollationMapDiff(nil, map[string]string{})
	if len(reasons) != 0 {
		t.Fatalf("expected 0 reasons for nil vs empty, got %d: %v", len(reasons), reasons)
	}
}

func TestCheckpointCollationMapDiff_NilVsPopulated(t *testing.T) {
	reasons := checkpointCollationMapDiff(nil, map[string]string{"utf8_general_ci": "en_US.utf8"})
	if len(reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d: %v", len(reasons), reasons)
	}
	if !strings.Contains(reasons[0], "added") {
		t.Fatalf("expected 'added' message, got: %s", reasons[0])
	}
}

func TestCheckpointCollationMapDiff_Removed(t *testing.T) {
	reasons := checkpointCollationMapDiff(map[string]string{"utf8_general_ci": "en_US.utf8"}, nil)
	if len(reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d: %v", len(reasons), reasons)
	}
	if !strings.Contains(reasons[0], "removed") {
		t.Fatalf("expected 'removed' message, got: %s", reasons[0])
	}
}

func TestCheckpointCollationMapDiff_Changed(t *testing.T) {
	reasons := checkpointCollationMapDiff(
		map[string]string{"utf8_general_ci": "en_US.utf8"},
		map[string]string{"utf8_general_ci": "C"},
	)
	if len(reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d: %v", len(reasons), reasons)
	}
	if !strings.Contains(reasons[0], "changed") {
		t.Fatalf("expected 'changed' message, got: %s", reasons[0])
	}
}

func TestCheckpointHookCompatibilityDiff_NoChanges(t *testing.T) {
	hooks := []checkpointCompatibilityHook{
		{Phase: "before_data", Path: "setup.sql", SHA256: "abc123"},
	}
	reasons := checkpointHookCompatibilityDiff(hooks, hooks)
	if len(reasons) != 0 {
		t.Fatalf("expected 0 reasons, got %d: %v", len(reasons), reasons)
	}
}

func TestCheckpointHookCompatibilityDiff_Added(t *testing.T) {
	saved := []checkpointCompatibilityHook{}
	current := []checkpointCompatibilityHook{
		{Phase: "before_data", Path: "new.sql", SHA256: "abc"},
	}
	reasons := checkpointHookCompatibilityDiff(saved, current)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "added") {
		t.Fatalf("expected 'added' reason, got: %v", reasons)
	}
}

func TestCheckpointHookCompatibilityDiff_Removed(t *testing.T) {
	saved := []checkpointCompatibilityHook{
		{Phase: "before_data", Path: "old.sql", SHA256: "abc"},
	}
	current := []checkpointCompatibilityHook{}
	reasons := checkpointHookCompatibilityDiff(saved, current)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "removed") {
		t.Fatalf("expected 'removed' reason, got: %v", reasons)
	}
}

func TestCheckpointHookCompatibilityDiff_ContentChanged(t *testing.T) {
	saved := []checkpointCompatibilityHook{
		{Phase: "before_data", Path: "hook.sql", SHA256: "abc"},
	}
	current := []checkpointCompatibilityHook{
		{Phase: "before_data", Path: "hook.sql", SHA256: "def"},
	}
	reasons := checkpointHookCompatibilityDiff(saved, current)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "changed") {
		t.Fatalf("expected 'changed' reason, got: %v", reasons)
	}
}

func TestCheckpointTableCompatibilityDiff_Added(t *testing.T) {
	saved := []checkpointCompatibilityTable{}
	current := []checkpointCompatibilityTable{
		{SourceName: "orders", PGName: "orders", TableHash: "h1"},
	}
	reasons := checkpointTableCompatibilityDiff(saved, current)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "table added") {
		t.Fatalf("expected 'table added', got: %v", reasons)
	}
}

func TestCheckpointTableCompatibilityDiff_Removed(t *testing.T) {
	saved := []checkpointCompatibilityTable{
		{SourceName: "orders", PGName: "orders", TableHash: "h1"},
	}
	current := []checkpointCompatibilityTable{}
	reasons := checkpointTableCompatibilityDiff(saved, current)
	if len(reasons) != 1 || !strings.Contains(reasons[0], "table removed") {
		t.Fatalf("expected 'table removed', got: %v", reasons)
	}
}

func TestCheckpointTableCompatibilityDiff_PGNameChanged(t *testing.T) {
	saved := []checkpointCompatibilityTable{
		{SourceName: "Orders", PGName: "orders", TableHash: "h1"},
	}
	current := []checkpointCompatibilityTable{
		{SourceName: "Orders", PGName: "order_items", TableHash: "h1"},
	}
	reasons := checkpointTableCompatibilityDiff(saved, current)
	found := false
	for _, r := range reasons {
		if strings.Contains(r, "target table name changed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'target table name changed', got: %v", reasons)
	}
}

func TestCheckpointTableCompatibilityDiff_ChunkKeyChanged(t *testing.T) {
	saved := []checkpointCompatibilityTable{
		{SourceName: "orders", PGName: "orders", ChunkKey: "id", TableHash: "h1"},
	}
	current := []checkpointCompatibilityTable{
		{SourceName: "orders", PGName: "orders", ChunkKey: "order_id", TableHash: "h1"},
	}
	reasons := checkpointTableCompatibilityDiff(saved, current)
	found := false
	for _, r := range reasons {
		if strings.Contains(r, "chunk key changed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'chunk key changed', got: %v", reasons)
	}
}

func TestCheckpointTableCompatibilityDiff_SchemaChanged(t *testing.T) {
	saved := []checkpointCompatibilityTable{
		{SourceName: "orders", PGName: "orders", TableHash: "h1"},
	}
	current := []checkpointCompatibilityTable{
		{SourceName: "orders", PGName: "orders", TableHash: "h2"},
	}
	reasons := checkpointTableCompatibilityDiff(saved, current)
	found := false
	for _, r := range reasons {
		if strings.Contains(r, "table schema changed") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'table schema changed', got: %v", reasons)
	}
}

func TestCloneCheckpointCompatibility_Nil(t *testing.T) {
	cloned := cloneCheckpointCompatibility(nil)
	if cloned != nil {
		t.Fatal("expected nil clone")
	}
}

func TestCloneCheckpointCompatibility_DeepCopy(t *testing.T) {
	original := testCheckpointCompatibility()
	original.Summary.TypeMapping.CollationMap = map[string]string{"a": "b"}

	cloned := cloneCheckpointCompatibility(&original)

	if cloned.Fingerprint != original.Fingerprint {
		t.Fatal("fingerprint mismatch")
	}
	if cloned.Summary == original.Summary {
		t.Fatal("summary should be a different pointer")
	}

	// Mutate clone's collation map — should not affect original
	cloned.Summary.TypeMapping.CollationMap["a"] = "changed"
	if original.Summary.TypeMapping.CollationMap["a"] != "b" {
		t.Fatal("mutating clone should not affect original")
	}

	// Mutate clone's hooks — should not affect original
	original.Summary.Hooks = []checkpointCompatibilityHook{{Phase: "before_data"}}
	cloned2 := cloneCheckpointCompatibility(&original)
	cloned2.Summary.Hooks = append(cloned2.Summary.Hooks, checkpointCompatibilityHook{Phase: "after_all"})
	if len(original.Summary.Hooks) != 1 {
		t.Fatal("mutating clone hooks should not affect original")
	}
}

func TestCloneCheckpointCompatibility_NilSummary(t *testing.T) {
	original := checkpointCompatibility{Fingerprint: "abc"}
	cloned := cloneCheckpointCompatibility(&original)
	if cloned.Summary != nil {
		t.Fatal("expected nil summary in clone")
	}
	if cloned.Fingerprint != "abc" {
		t.Fatal("fingerprint should be cloned")
	}
}

func TestBuildCheckpointCompatibility_BasicFields(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql"}
	cfg.SourceSnapshotMode = "none"
	cfg.ChunkSize = 100000

	schema := &Schema{Tables: []Table{
		{SourceName: "users", PGName: "users", Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int"},
		}},
	}}

	src := &mysqlSourceDB{}
	typeMap := effectiveTypeMapping(&cfg)

	compat, err := buildCheckpointCompatibility(&cfg, schema, src, "testdb", typeMap)
	if err != nil {
		t.Fatalf("buildCheckpointCompatibility: %v", err)
	}
	if compat.Fingerprint == "" {
		t.Fatal("fingerprint should not be empty")
	}
	if compat.Summary == nil {
		t.Fatal("summary should not be nil")
	}
	if compat.Summary.SourceType != "mysql" {
		t.Fatalf("SourceType = %q, want mysql", compat.Summary.SourceType)
	}
	if compat.Summary.SourceDBName != "testdb" {
		t.Fatalf("SourceDBName = %q, want testdb", compat.Summary.SourceDBName)
	}
	if len(compat.Summary.Tables) != 1 {
		t.Fatalf("tables count = %d, want 1", len(compat.Summary.Tables))
	}
}

func TestBuildCheckpointCompatibility_NilSchema(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql"}

	src := &mysqlSourceDB{}
	typeMap := effectiveTypeMapping(&cfg)

	compat, err := buildCheckpointCompatibility(&cfg, nil, src, "testdb", typeMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compat.Summary.Tables != nil {
		t.Fatalf("expected nil tables for nil schema, got %d", len(compat.Summary.Tables))
	}
}

func TestBuildCheckpointCompatibility_EmptySchema(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql"}

	src := &mysqlSourceDB{}
	typeMap := effectiveTypeMapping(&cfg)

	compat, err := buildCheckpointCompatibility(&cfg, &Schema{}, src, "testdb", typeMap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(compat.Summary.Tables) != 0 {
		t.Fatalf("expected 0 tables for empty schema, got %d", len(compat.Summary.Tables))
	}
}

func TestBuildCheckpointCompatibility_HookHashing(t *testing.T) {
	dir := t.TempDir()
	hookFile := filepath.Join(dir, "before.sql")
	if err := os.WriteFile(hookFile, []byte("SELECT 1;"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql"}
	cfg.Hooks.BeforeData = []string{"before.sql"}
	cfg.configDir = dir

	src := &mysqlSourceDB{}
	typeMap := effectiveTypeMapping(&cfg)

	compat1, err := buildCheckpointCompatibility(&cfg, &Schema{}, src, "testdb", typeMap)
	if err != nil {
		t.Fatalf("first build: %v", err)
	}

	// Change hook content
	if err := os.WriteFile(hookFile, []byte("SELECT 2;"), 0644); err != nil {
		t.Fatal(err)
	}

	compat2, err := buildCheckpointCompatibility(&cfg, &Schema{}, src, "testdb", typeMap)
	if err != nil {
		t.Fatalf("second build: %v", err)
	}

	if compat1.Fingerprint == compat2.Fingerprint {
		t.Fatal("changing hook content should change fingerprint")
	}
}

func TestBuildCheckpointCompatibility_HookReadError(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql"}
	cfg.Hooks.BeforeData = []string{"nonexistent.sql"}
	cfg.configDir = t.TempDir()

	src := &mysqlSourceDB{}
	typeMap := effectiveTypeMapping(&cfg)

	_, err := buildCheckpointCompatibility(&cfg, &Schema{}, src, "testdb", typeMap)
	if err == nil {
		t.Fatal("expected error for missing hook file")
	}
	if !strings.Contains(err.Error(), "hash hook") {
		t.Fatalf("expected hash hook error, got: %v", err)
	}
}

func TestCheckpointCompatibilityTables_SortedBySourceName(t *testing.T) {
	schema := &Schema{Tables: []Table{
		{SourceName: "Zebra", PGName: "zebra", Columns: []Column{{SourceName: "id", PGName: "id"}}},
		{SourceName: "Apple", PGName: "apple", Columns: []Column{{SourceName: "id", PGName: "id"}}},
		{SourceName: "Mango", PGName: "mango", Columns: []Column{{SourceName: "id", PGName: "id"}}},
	}}
	src := &mysqlSourceDB{}

	tables, err := checkpointCompatibilityTables(schema, src)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 3 {
		t.Fatalf("expected 3 tables, got %d", len(tables))
	}
	if tables[0].SourceName != "Apple" || tables[1].SourceName != "Mango" || tables[2].SourceName != "Zebra" {
		t.Fatalf("tables not sorted: %v", tables)
	}
}

func TestCheckpointTypeMappingDiff_MSSQLFields(t *testing.T) {
	saved := defaultTypeMappingConfig()
	current := defaultTypeMappingConfig()
	current.NvarcharAsText = true
	current.MoneyAsNumeric = false
	current.XmlAsText = true

	reasons := checkpointTypeMappingDiff(saved, current)
	for _, want := range []string{"nvarchar_as_text", "money_as_numeric", "xml_as_text"} {
		found := false
		for _, r := range reasons {
			if strings.Contains(r, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing reason for %s in %v", want, reasons)
		}
	}
}
