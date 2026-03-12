package main

import (
	"strings"
	"testing"
)

var mysqlSrc = &mysqlSourceDB{}

func TestGenerateCreateTable(t *testing.T) {
	table := Table{
		PGName: "users",
		Columns: []Column{
			{PGName: "identifier", DataType: "binary", Precision: 16, Nullable: false},
			{PGName: "secret", DataType: "varchar", CharMaxLen: 150, Nullable: false},
			{PGName: "enabled", DataType: "tinyint", Precision: 1, Nullable: false},
			{PGName: "email_address", DataType: "varchar", CharMaxLen: 150, Nullable: true},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	// Should be logged by default
	if strings.Contains(ddl, "UNLOGGED") {
		t.Error("DDL should not contain UNLOGGED by default")
	}

	// Should have schema prefix
	if !strings.Contains(ddl, `"app"."users"`) {
		t.Error(`DDL should reference "app"."users"`)
	}

	// uuid type for binary(16)
	if !strings.Contains(ddl, `"identifier" bytea NOT NULL`) {
		t.Errorf("DDL should map binary(16) to bytea by default, got:\n%s", ddl)
	}

	// boolean for tinyint(1)
	if !strings.Contains(ddl, `"enabled" smallint NOT NULL`) {
		t.Errorf("DDL should map tinyint(1) to smallint by default, got:\n%s", ddl)
	}

	// nullable column should not have NOT NULL
	if strings.Contains(ddl, `"email_address" varchar(150) NOT NULL`) {
		t.Error("nullable column should not have NOT NULL")
	}
}

func TestGenerateCreateTable_Unlogged(t *testing.T) {
	table := Table{
		PGName: "users",
		Columns: []Column{
			{PGName: "id", DataType: "int", Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", true, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, `CREATE UNLOGGED TABLE "app"."users"`) {
		t.Errorf("DDL should be unlogged when enabled, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_DefaultLoggedPrefix(t *testing.T) {
	table := Table{
		PGName: "accounts",
		Columns: []Column{
			{PGName: "id", DataType: "int", Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.HasPrefix(ddl, `CREATE TABLE "app"."accounts" (`) {
		t.Fatalf("expected logged CREATE TABLE prefix, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_AlwaysQuotesIdentifiers(t *testing.T) {
	table := Table{
		PGName: "user",
		Columns: []Column{
			{PGName: "order", DataType: "int", Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if !strings.Contains(ddl, `"user"`) {
		t.Errorf("DDL should quote table identifier 'user', got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"order"`) {
		t.Errorf("DDL should quote column identifier 'order', got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_QuotesCollationIdentifier(t *testing.T) {
	table := Table{
		PGName: "articles",
		Columns: []Column{
			{PGName: "collation", DataType: "varchar", CharMaxLen: 64, Nullable: false},
		},
	}

	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if !strings.Contains(ddl, `"collation" varchar(64) NOT NULL`) {
		t.Fatalf("DDL should quote identifier 'collation', got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_UnknownTypeErrors(t *testing.T) {
	table := Table{
		PGName: "mystery",
		Columns: []Column{
			{PGName: "shape", DataType: "geometry", ColumnType: "geometry", Nullable: true},
		},
	}

	_, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err == nil {
		t.Fatal("expected error for unsupported MySQL type")
	}
}

func TestGenerateCreateTable_PreserveDefaults(t *testing.T) {
	table := Table{
		PGName: "defaults_demo",
		Columns: []Column{
			{PGName: "count", DataType: "int", ColumnType: "int", Nullable: false, Default: strPtr("0")},
			{PGName: "status", DataType: "varchar", ColumnType: "varchar(20)", CharMaxLen: 20, Nullable: false, Default: strPtr("new")},
			{PGName: "created_at", DataType: "timestamp", ColumnType: "timestamp", Nullable: false, Default: strPtr("CURRENT_TIMESTAMP")},
			{PGName: "metadata", DataType: "json", ColumnType: "json", Nullable: true, Default: strPtr("{}")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.JSONAsJSONB = true

	ddl, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if !strings.Contains(ddl, `"count" integer DEFAULT 0 NOT NULL`) {
		t.Fatalf("expected numeric default in DDL, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"status" varchar(20) DEFAULT 'new' NOT NULL`) {
		t.Fatalf("expected text default in DDL, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"created_at" timestamptz DEFAULT CURRENT_TIMESTAMP NOT NULL`) {
		t.Fatalf("expected timestamp default in DDL, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `"metadata" jsonb DEFAULT '{}'::jsonb`) {
		t.Fatalf("expected jsonb default in DDL, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_PreserveDefaultsUnsupported(t *testing.T) {
	table := Table{
		PGName: "bad_defaults",
		Columns: []Column{
			{PGName: "enabled", DataType: "tinyint", ColumnType: "tinyint(1)", Precision: 1, Nullable: false, Default: strPtr("2")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.TinyInt1AsBoolean = true

	_, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err == nil {
		t.Fatal("expected error for unsupported boolean default")
	}
}

func TestGenerateCreateTable_NoPreserveDefaultsSkipsDefaults(t *testing.T) {
	table := Table{
		PGName: "no_defaults",
		Columns: []Column{
			{PGName: "name", DataType: "varchar", ColumnType: "varchar(20)", CharMaxLen: 20, Nullable: false, Default: strPtr("alice")},
		},
	}
	ddl, err := generateCreateTable(table, "app", false, false, defaultTypeMappingConfig(), mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if strings.Contains(ddl, "DEFAULT") {
		t.Fatalf("expected defaults to be skipped when preserve_defaults=false, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_EnumCheckMode(t *testing.T) {
	table := Table{
		PGName: "enum_demo",
		Columns: []Column{
			{PGName: "status", DataType: "enum", ColumnType: "enum('new','used')", Nullable: false},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "check"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, `CHECK ("status" IN ('new', 'used'))`) {
		t.Fatalf("expected enum CHECK clause, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_SetArrayDefault(t *testing.T) {
	table := Table{
		PGName: "set_demo",
		Columns: []Column{
			{PGName: "flags", DataType: "set", ColumnType: "set('a','b','c')", Nullable: false, Default: strPtr("a,c")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array"

	ddl, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, `"flags" text[] DEFAULT ARRAY['a', 'c']::text[] NOT NULL`) {
		t.Fatalf("expected set text[] default, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CollateAutoWithBin(t *testing.T) {
	table := Table{
		PGName: "collation_demo",
		Columns: []Column{
			{PGName: "code", DataType: "varchar", CharMaxLen: 50, Nullable: false, Collation: "utf8mb4_bin"},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, `COLLATE "C"`) {
		t.Fatalf("expected COLLATE \"C\" for _bin collation, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CollateModeNone(t *testing.T) {
	table := Table{
		PGName: "no_collate",
		Columns: []Column{
			{PGName: "name", DataType: "varchar", CharMaxLen: 100, Nullable: false, Collation: "utf8mb4_bin"},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "none"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if strings.Contains(ddl, "COLLATE") {
		t.Fatalf("expected no COLLATE when mode=none, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CollateUserMap(t *testing.T) {
	table := Table{
		PGName: "mapped_collation",
		Columns: []Column{
			{PGName: "title", DataType: "text", Nullable: true, Collation: "utf8mb4_general_ci"},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"
	tm.CollationMap = map[string]string{
		"utf8mb4_general_ci": "und-x-icu",
	}

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, `COLLATE "und-x-icu"`) {
		t.Fatalf("expected user-mapped COLLATE in DDL, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CollateNotOnNonText(t *testing.T) {
	table := Table{
		PGName: "int_collation",
		Columns: []Column{
			{PGName: "count", DataType: "int", Nullable: false, Collation: "utf8mb4_bin"},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if strings.Contains(ddl, "COLLATE") {
		t.Fatalf("COLLATE should not appear on non-text column, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CIAsCitext(t *testing.T) {
	table := Table{
		PGName: "citext_demo",
		Columns: []Column{
			{PGName: "name", DataType: "varchar", CharMaxLen: 100, Nullable: false, Collation: "utf8mb4_general_ci"},
			{PGName: "code", DataType: "varchar", CharMaxLen: 50, Nullable: false, Collation: "utf8mb4_bin"},
			{PGName: "count", DataType: "int", Nullable: false, Collation: "utf8mb4_general_ci"},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.CIAsCitext = true

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	// _ci text column → citext
	if !strings.Contains(ddl, `"name" citext NOT NULL`) {
		t.Errorf("expected citext for _ci varchar column, got:\n%s", ddl)
	}

	// _bin text column → unchanged (varchar)
	if strings.Contains(ddl, `"code" citext`) {
		t.Errorf("_bin column should not become citext, got:\n%s", ddl)
	}

	// non-text column → unchanged even with _ci collation
	if strings.Contains(ddl, `"count" citext`) {
		t.Errorf("non-text column should not become citext, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CIAsCitextDisabled(t *testing.T) {
	table := Table{
		PGName: "disabled_demo",
		Columns: []Column{
			{PGName: "name", DataType: "varchar", CharMaxLen: 100, Nullable: false, Collation: "utf8mb4_general_ci"},
		},
	}
	tm := defaultTypeMappingConfig()
	// CIAsCitext defaults to false

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if strings.Contains(ddl, "citext") {
		t.Errorf("citext should not appear when ci_as_citext=false, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_CIAsCitextWithCollationMap(t *testing.T) {
	table := Table{
		PGName: "map_wins",
		Columns: []Column{
			{PGName: "title", DataType: "text", Nullable: true, Collation: "utf8mb4_general_ci"},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.CIAsCitext = true
	tm.CollationMap = map[string]string{
		"utf8mb4_general_ci": "und-x-icu",
	}

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	// collation_map takes precedence over citext
	if strings.Contains(ddl, "citext") {
		t.Errorf("collation_map should take precedence over ci_as_citext, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_SetArrayCheckMode(t *testing.T) {
	table := Table{
		PGName: "set_check_demo",
		Columns: []Column{
			{PGName: "flags", DataType: "set", ColumnType: "set('a','b','c')", Nullable: false},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array_check"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, "text[]") {
		t.Errorf("expected text[] type, got:\n%s", ddl)
	}
	if !strings.Contains(ddl, `CHECK ("flags" <@ ARRAY['a', 'b', 'c']::text[])`) {
		t.Errorf("expected set CHECK constraint, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_SetArrayCheckDefault(t *testing.T) {
	table := Table{
		PGName: "set_check_default",
		Columns: []Column{
			{PGName: "tags", DataType: "set", ColumnType: "set('x','y','z')", Nullable: false, Default: strPtr("x,z")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.SetMode = "text_array_check"

	ddl, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}
	if !strings.Contains(ddl, "DEFAULT ARRAY['x', 'z']::text[]") {
		t.Errorf("expected array default in DDL, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_EnumNativeMode(t *testing.T) {
	table := Table{
		PGName: "enum_native_demo",
		Columns: []Column{
			{PGName: "status", DataType: "enum", ColumnType: "enum('new','used')", Nullable: false},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	typeName := pgEnumTypeName([]string{"new", "used"})
	expected := `"app"."` + typeName + `"`
	if !strings.Contains(ddl, expected) {
		t.Errorf("expected schema-qualified enum type %q in DDL, got:\n%s", expected, ddl)
	}
	// Should NOT have CHECK clause
	if strings.Contains(ddl, "CHECK") {
		t.Errorf("native enum mode should not produce CHECK clause, got:\n%s", ddl)
	}
}

func TestGenerateCreateTable_EnumNativeReusesType(t *testing.T) {
	// Two columns with the same enum definition should produce the same type name
	table := Table{
		PGName: "reuse_demo",
		Columns: []Column{
			{PGName: "status1", DataType: "enum", ColumnType: "enum('a','b')", Nullable: false},
			{PGName: "status2", DataType: "enum", ColumnType: "enum('b','a')", Nullable: false},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"

	ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	// Both should use the same type name since pgEnumTypeName sorts values
	typeName := pgEnumTypeName([]string{"a", "b"})
	count := strings.Count(ddl, typeName)
	if count != 2 {
		t.Errorf("expected type %s to appear 2 times, got %d in DDL:\n%s", typeName, count, ddl)
	}
}

func TestPgEnumTypeName_Deterministic(t *testing.T) {
	// Same values in different order produce the same name
	name1 := pgEnumTypeName([]string{"c", "a", "b"})
	name2 := pgEnumTypeName([]string{"a", "b", "c"})
	if name1 != name2 {
		t.Errorf("pgEnumTypeName order-dependent: %q != %q", name1, name2)
	}

	// Different values produce different names
	name3 := pgEnumTypeName([]string{"x", "y"})
	if name1 == name3 {
		t.Errorf("pgEnumTypeName collision: %q == %q", name1, name3)
	}

	// Prefix and length check (16 hex digits for 64-bit hash)
	if !strings.HasPrefix(name1, "pgferry_enum_") {
		t.Errorf("pgEnumTypeName missing prefix: %q", name1)
	}
	if len(name1) != len("pgferry_enum_")+16 {
		t.Errorf("pgEnumTypeName length = %d, want %d (prefix + 16 hex digits)", len(name1), len("pgferry_enum_")+16)
	}
}

func TestCreateEnumTypes_CrossTableDedup(t *testing.T) {
	// Two tables with the same enum values in different order should produce
	// exactly one PG type with deterministic (sorted) declaration order.
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "t1",
				Columns: []Column{
					{PGName: "status", DataType: "enum", ColumnType: "enum('b','a')"},
				},
			},
			{
				PGName: "t2",
				Columns: []Column{
					{PGName: "status", DataType: "enum", ColumnType: "enum('a','b')"},
				},
			},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"

	// Both should map to the same type name
	name1 := pgEnumTypeName([]string{"b", "a"})
	name2 := pgEnumTypeName([]string{"a", "b"})
	if name1 != name2 {
		t.Fatalf("cross-table enum type names differ: %q vs %q", name1, name2)
	}

	// Verify both columns reference the same type in DDL
	for _, table := range schema.Tables {
		ddl, err := generateCreateTable(table, "app", false, false, tm, mysqlSrc)
		if err != nil {
			t.Fatalf("generateCreateTable(%s): %v", table.PGName, err)
		}
		if !strings.Contains(ddl, name1) {
			t.Errorf("table %s DDL missing enum type %s:\n%s", table.PGName, name1, ddl)
		}
	}
}

func TestGenerateCreateTable_EnumNativeDefault(t *testing.T) {
	table := Table{
		PGName: "enum_default_demo",
		Columns: []Column{
			{PGName: "status", DataType: "enum", ColumnType: "enum('active','inactive')", Nullable: false, Default: strPtr("active")},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"

	ddl, err := generateCreateTable(table, "app", false, true, tm, mysqlSrc)
	if err != nil {
		t.Fatalf("generateCreateTable() error: %v", err)
	}

	if !strings.Contains(ddl, "DEFAULT 'active'") {
		t.Errorf("expected DEFAULT 'active' in DDL, got:\n%s", ddl)
	}
}

func strPtr(s string) *string {
	return &s
}
