package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalizeConfig_SchemaOnlyAndDataOnlyMutuallyExclusive(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.SchemaOnly = true
	cfg.DataOnly = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for schema_only + data_only")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got: %v", err)
	}
}

func TestFinalizeConfig_ResumeIncompatibleWithRecreate(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.Resume = true
	cfg.OnSchemaExists = "recreate"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for resume + recreate")
	}
	if !strings.Contains(err.Error(), "resume is incompatible with on_schema_exists=recreate") {
		t.Fatalf("expected resume/recreate error, got: %v", err)
	}
}

func TestFinalizeConfig_ResumeIncompatibleWithUse(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.Resume = true
	cfg.OnSchemaExists = "use"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for resume + use")
	}
	if !strings.Contains(err.Error(), "resume is incompatible with on_schema_exists=use") {
		t.Fatalf("expected resume/use error, got: %v", err)
	}
}

func TestFinalizeConfig_ResumeIncompatibleWithSchemaOnly(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.Resume = true
	cfg.SchemaOnly = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for resume + schema_only")
	}
	if !strings.Contains(err.Error(), "resume is incompatible with schema_only") {
		t.Fatalf("expected resume/schema_only error, got: %v", err)
	}
}

func TestFinalizeConfig_ResumeIncompatibleWithUnlogged(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.Resume = true
	cfg.UnloggedTables = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for resume + unlogged_tables")
	}
	if !strings.Contains(err.Error(), "resume is incompatible with unlogged_tables") {
		t.Fatalf("expected resume/unlogged error, got: %v", err)
	}
}

func TestFinalizeConfig_InvalidOnSchemaExists(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.OnSchemaExists = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid on_schema_exists")
	}
}

func TestFinalizeConfig_InvalidSourceSnapshotMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.SourceSnapshotMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid source_snapshot_mode")
	}
}

func TestFinalizeConfig_InvalidTableFilterMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TableFilterMode = "prefix"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid table_filter_mode")
	}
	if !strings.Contains(err.Error(), "table_filter_mode must be one of") {
		t.Fatalf("expected table_filter_mode error, got: %v", err)
	}
}

func TestFinalizeConfig_SQLiteSingleTxRejected(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "sqlite", DSN: "/tmp/test.db"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.SourceSnapshotMode = "single_tx"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for SQLite + single_tx")
	}
	if !strings.Contains(err.Error(), "not supported for sqlite") {
		t.Fatalf("expected sqlite snapshot error, got: %v", err)
	}
}

func TestFinalizeConfig_InvalidEnumMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.EnumMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid enum_mode")
	}
}

func TestFinalizeConfig_InvalidSetMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.SetMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid set_mode")
	}
}

func TestFinalizeConfig_InvalidBitMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.BitMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid bit_mode")
	}
}

func TestFinalizeConfig_InvalidTimeMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.TimeMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid time_mode")
	}
}

func TestFinalizeConfig_InvalidZeroDateMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.ZeroDateMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid zero_date_mode")
	}
}

func TestFinalizeConfig_InvalidSpatialMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.SpatialMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid spatial_mode")
	}
}

func TestFinalizeConfig_InvalidValidationMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.Validation = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid validation mode")
	}
}

func TestFinalizeConfig_InvalidCleanOrphansMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.CleanOrphansMode = "invalid"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid clean_orphans_mode")
	}
}

func TestFinalizeConfig_NegativeCleanOrphansMaxRows(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.CleanOrphansMaxRows = -1

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for negative clean_orphans_max_rows")
	}
}

func TestFinalizeConfig_Binary16UUIDModeRequiresBinary16AsUUID(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.TypeMapping.Binary16UUIDMode = "mysql_uuid_to_bin_swap"
	cfg.TypeMapping.Binary16AsUUID = false

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for binary16_uuid_mode without binary16_as_uuid")
	}
	if !strings.Contains(err.Error(), "binary16_uuid_mode requires binary16_as_uuid") {
		t.Fatalf("expected binary16 requirement error, got: %v", err)
	}
}

func TestFinalizeConfig_PostGISRequiresMySQL(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "sqlite", DSN: "/tmp/test.db"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.PostGIS.Enabled = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for postgis with sqlite")
	}
	if !strings.Contains(err.Error(), "only supported for mysql") {
		t.Fatalf("expected mysql-only error, got: %v", err)
	}
}

func TestFinalizeConfig_PostGISIncompatibleWithSpatialMode(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.PostGIS.Enabled = true
	cfg.TypeMapping.SpatialMode = "wkb_bytea"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for postgis + spatial_mode")
	}
	if !strings.Contains(err.Error(), "incompatible with type_mapping.spatial_mode") {
		t.Fatalf("expected incompatible error, got: %v", err)
	}
}

func TestFinalizeConfig_PostGISCreateExtensionRequiresEnabled(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.PostGIS.CreateExtension = true
	cfg.PostGIS.Enabled = false

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for create_extension without enabled")
	}
	if !strings.Contains(err.Error(), "create_extension requires postgis.enabled") {
		t.Fatalf("expected create_extension error, got: %v", err)
	}
}

func TestFinalizeConfig_CharsetRejectedForNonMySQL(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "sqlite", DSN: "/tmp/test.db", Charset: "latin1"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for charset on sqlite")
	}
	if !strings.Contains(err.Error(), "MySQL/MariaDB-only") {
		t.Fatalf("expected charset error, got: %v", err)
	}
}

func TestFinalizeConfig_MSSQLSourceSchemaDefault(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mssql", DSN: "sqlserver://localhost/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}

	err := finalizeConfig(&cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source.SourceSchema != "dbo" {
		t.Fatalf("SourceSchema = %q, want 'dbo'", cfg.Source.SourceSchema)
	}
}

func TestFinalizeConfig_MSSQLSourceSchemaWhitespace(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mssql", DSN: "sqlserver://localhost/test", SourceSchema: "  "}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}

	err := finalizeConfig(&cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source.SourceSchema != "dbo" {
		t.Fatalf("SourceSchema = %q, want 'dbo' for whitespace input", cfg.Source.SourceSchema)
	}
}

func TestFinalizeConfig_MSSQLSourceSchemaCustom(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mssql", DSN: "sqlserver://localhost/test", SourceSchema: "sales"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}

	err := finalizeConfig(&cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source.SourceSchema != "sales" {
		t.Fatalf("SourceSchema = %q, want 'sales'", cfg.Source.SourceSchema)
	}
}

func TestFinalizeConfig_MissingSourceDSN(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing source DSN")
	}
	if !strings.Contains(err.Error(), "source.dsn is required") {
		t.Fatalf("expected source DSN error, got: %v", err)
	}
}

func TestFinalizeConfig_MissingTargetDSN(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{}

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing target DSN")
	}
	if !strings.Contains(err.Error(), "target.dsn is required") {
		t.Fatalf("expected target DSN error, got: %v", err)
	}
}

func TestFinalizeConfig_PostGISMariaDBRejected(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mariadb", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	cfg.PostGIS.Enabled = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for MariaDB + PostGIS")
	}
	if !strings.Contains(err.Error(), "mariadb") {
		t.Fatalf("expected MariaDB-specific error, got: %v", err)
	}
}

func TestNormalizeTableFilterEntries_Empty(t *testing.T) {
	got, err := normalizeTableFilterEntries("include_tables", "exact", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestNormalizeTableFilterEntries_EmptyName(t *testing.T) {
	_, err := normalizeTableFilterEntries("include_tables", "exact", []string{""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestNormalizeTableFilterEntries_GlobRejected(t *testing.T) {
	_, err := normalizeTableFilterEntries("include_tables", "exact", []string{"user*"})
	if err == nil {
		t.Fatal("expected error for glob pattern")
	}
	if !strings.Contains(err.Error(), "glob patterns are not supported") {
		t.Fatalf("expected glob error, got: %v", err)
	}
}

func TestNormalizeTableFilterEntries_Duplicates(t *testing.T) {
	_, err := normalizeTableFilterEntries("include_tables", "exact", []string{"Users", "users"})
	if err == nil {
		t.Fatal("expected error for case-insensitive duplicates")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got: %v", err)
	}
}

func TestLoadConfig_MinimalValid(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	toml := `schema = "public"
[source]
type = "mysql"
dsn = "root@tcp(localhost)/test"
[target]
dsn = "postgres://localhost/test"
`
	if err := os.WriteFile(configPath, []byte(toml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.Schema != "public" {
		t.Fatalf("Schema = %q, want 'public'", cfg.Schema)
	}
}
