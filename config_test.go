package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.toml")

	content := `
schema = "myschema"
on_schema_exists = "recreate"
source_snapshot_mode = "single_tx"
unlogged_tables = true
preserve_defaults = true
add_unsigned_checks = true
replicate_on_update_current_timestamp = true
workers = 8
truncate_before_copy = true
column_collision_mode = "auto"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/testdb"

[target]
dsn = "postgres://user:pass@localhost:5432/testdb"

[hooks]
before_data = ["pre.sql"]
after_data = []
before_fk = ["cleanup.sql"]
after_all = ["post.sql"]

[column_renames]
"Orders.RowVersion" = "row_version_target"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Source.Type != "mysql" {
		t.Errorf("Source.Type = %q", cfg.Source.Type)
	}
	if cfg.Source.DSN != "root:root@tcp(127.0.0.1:3306)/testdb" {
		t.Errorf("Source.DSN = %q", cfg.Source.DSN)
	}
	if cfg.Target.DSN != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("Postgres.DSN = %q", cfg.Target.DSN)
	}
	if cfg.Schema != "myschema" {
		t.Errorf("Schema = %q, want %q", cfg.Schema, "myschema")
	}
	if cfg.Workers != 8 {
		t.Errorf("Workers = %d, want 8", cfg.Workers)
	}
	if cfg.OnSchemaExists != "recreate" {
		t.Errorf("OnSchemaExists = %q, want %q", cfg.OnSchemaExists, "recreate")
	}
	if cfg.SourceSnapshotMode != "single_tx" {
		t.Errorf("SourceSnapshotMode = %q, want %q", cfg.SourceSnapshotMode, "single_tx")
	}
	if !cfg.UnloggedTables {
		t.Errorf("UnloggedTables = %t, want true", cfg.UnloggedTables)
	}
	if !cfg.PreserveDefaults {
		t.Errorf("PreserveDefaults = %t, want true", cfg.PreserveDefaults)
	}
	if !cfg.AddUnsignedChecks {
		t.Errorf("AddUnsignedChecks = %t, want true", cfg.AddUnsignedChecks)
	}
	if !cfg.ReplicateOnUpdateCurrentTimestamp {
		t.Errorf("ReplicateOnUpdateCurrentTimestamp = %t, want true", cfg.ReplicateOnUpdateCurrentTimestamp)
	}
	if cfg.TruncateBeforeCopy != truncateBeforeCopyPerRun {
		t.Errorf("TruncateBeforeCopy = %s, want %s", cfg.TruncateBeforeCopy, truncateBeforeCopyPerRun)
	}
	if cfg.ColumnCollisionMode != "auto" {
		t.Errorf("ColumnCollisionMode = %q, want auto", cfg.ColumnCollisionMode)
	}
	if len(cfg.Hooks.BeforeFk) != 1 || cfg.Hooks.BeforeFk[0] != "cleanup.sql" {
		t.Errorf("Hooks.BeforeFk = %v", cfg.Hooks.BeforeFk)
	}
	if cfg.ColumnRenames["Orders.RowVersion"] != "row_version_target" {
		t.Errorf("ColumnRenames = %v", cfg.ColumnRenames)
	}
	if cfg.configDir != dir {
		t.Errorf("configDir = %q, want %q", cfg.configDir, dir)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "minimal.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	// on_schema_exists and workers omitted — defaults should apply
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Schema != "target" {
		t.Errorf("Schema = %q, want %q", cfg.Schema, "target")
	}
	if cfg.OnSchemaExists != "error" {
		t.Errorf("default OnSchemaExists = %q, want %q", cfg.OnSchemaExists, "error")
	}
	if cfg.SourceSnapshotMode != "none" {
		t.Errorf("default SourceSnapshotMode = %q, want %q", cfg.SourceSnapshotMode, "none")
	}
	if !cfg.UnloggedTables {
		t.Errorf("default UnloggedTables = %t, want true", cfg.UnloggedTables)
	}
	if !cfg.PreserveDefaults {
		t.Errorf("default PreserveDefaults = %t, want true", cfg.PreserveDefaults)
	}
	if cfg.AddUnsignedChecks {
		t.Errorf("default AddUnsignedChecks = %t, want false", cfg.AddUnsignedChecks)
	}
	if cfg.ReplicateOnUpdateCurrentTimestamp {
		t.Errorf("default ReplicateOnUpdateCurrentTimestamp = %t, want false", cfg.ReplicateOnUpdateCurrentTimestamp)
	}
	if cfg.TruncateBeforeCopy != truncateBeforeCopyOff {
		t.Errorf("default TruncateBeforeCopy = %s, want %s", cfg.TruncateBeforeCopy, truncateBeforeCopyOff)
	}
	if !cfg.CleanOrphans {
		t.Errorf("default CleanOrphans = %t, want true", cfg.CleanOrphans)
	}
	if cfg.CleanOrphansMode != "apply" {
		t.Errorf("default CleanOrphansMode = %q, want %q", cfg.CleanOrphansMode, "apply")
	}
	if cfg.CleanOrphansMaxRows != 0 {
		t.Errorf("default CleanOrphansMaxRows = %d, want 0", cfg.CleanOrphansMaxRows)
	}
	if cfg.IdentifierCase != "snake" {
		t.Errorf("default IdentifierCase = %q, want %q", cfg.IdentifierCase, "snake")
	}
	if cfg.ColumnCollisionMode != "error" {
		t.Errorf("default ColumnCollisionMode = %q, want %q", cfg.ColumnCollisionMode, "error")
	}
	wantWorkers := runtime.NumCPU()
	if wantWorkers < 1 {
		wantWorkers = 1
	}
	if wantWorkers > 8 {
		wantWorkers = 8
	}
	if cfg.Workers != wantWorkers {
		t.Errorf("default Workers = %d, want %d", cfg.Workers, wantWorkers)
	}
	if cfg.IndexWorkers != wantWorkers {
		t.Errorf("default IndexWorkers = %d, want %d (should default to Workers)", cfg.IndexWorkers, wantWorkers)
	}
	if cfg.TypeMapping.TinyInt1AsBoolean {
		t.Errorf("default TypeMapping.TinyInt1AsBoolean = %t, want false", cfg.TypeMapping.TinyInt1AsBoolean)
	}
	if cfg.TypeMapping.Binary16AsUUID {
		t.Errorf("default TypeMapping.Binary16AsUUID = %t, want false", cfg.TypeMapping.Binary16AsUUID)
	}
	if cfg.TypeMapping.DatetimeAsTimestamptz {
		t.Errorf("default TypeMapping.DatetimeAsTimestamptz = %t, want false", cfg.TypeMapping.DatetimeAsTimestamptz)
	}
	if !cfg.TypeMapping.JSONAsJSONB {
		t.Errorf("default TypeMapping.JSONAsJSONB = %t, want true", cfg.TypeMapping.JSONAsJSONB)
	}
	if cfg.TypeMapping.EnumMode != "check" {
		t.Errorf("default TypeMapping.EnumMode = %q, want %q", cfg.TypeMapping.EnumMode, "check")
	}
	if cfg.TypeMapping.SetMode != "text" {
		t.Errorf("default TypeMapping.SetMode = %q, want %q", cfg.TypeMapping.SetMode, "text")
	}
	if !cfg.TypeMapping.WidenUnsignedIntegers {
		t.Errorf("default TypeMapping.WidenUnsignedIntegers = %t, want true", cfg.TypeMapping.WidenUnsignedIntegers)
	}
	if !cfg.TypeMapping.SanitizeJSONNullBytes {
		t.Errorf("default TypeMapping.SanitizeJSONNullBytes = %t, want true", cfg.TypeMapping.SanitizeJSONNullBytes)
	}
	if cfg.TypeMapping.VarcharAsText {
		t.Errorf("default TypeMapping.VarcharAsText = %t, want false", cfg.TypeMapping.VarcharAsText)
	}
	if cfg.TypeMapping.UnknownAsText {
		t.Errorf("default TypeMapping.UnknownAsText = %t, want false", cfg.TypeMapping.UnknownAsText)
	}
	if cfg.TypeMapping.CollationMode != "none" {
		t.Errorf("default TypeMapping.CollationMode = %q, want %q", cfg.TypeMapping.CollationMode, "none")
	}
	if cfg.TypeMapping.CollationMap != nil {
		t.Errorf("default TypeMapping.CollationMap = %v, want nil", cfg.TypeMapping.CollationMap)
	}
	if cfg.TypeMapping.CIAsCitext {
		t.Errorf("default TypeMapping.CIAsCitext = %t, want false", cfg.TypeMapping.CIAsCitext)
	}
	if cfg.PostGIS.Enabled {
		t.Errorf("default PostGIS.Enabled = %t, want false", cfg.PostGIS.Enabled)
	}
	if cfg.PostGIS.CreateExtension {
		t.Errorf("default PostGIS.CreateExtension = %t, want false", cfg.PostGIS.CreateExtension)
	}
	if cfg.Source.Charset != "utf8mb4" {
		t.Errorf("default Source.Charset = %q, want %q", cfg.Source.Charset, "utf8mb4")
	}
}

func TestLoadConfig_ResumeRejectsTruncateBeforeCopy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		truncateMode string
		wantError    string
	}{
		{name: "true", truncateMode: "true", wantError: "resume is incompatible with truncate_before_copy=true"},
		{name: "once", truncateMode: `"once"`, wantError: "resume is incompatible with truncate_before_copy=once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "resume-truncate.toml")

			content := fmt.Sprintf(`
schema = "target"
resume = true
truncate_before_copy = %s

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`, tc.truncateMode)
			if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}

			_, err := loadConfig(cfgFile)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v, want resume/truncate incompatibility", err)
			}
		})
	}
}

func TestLoadConfig_SchemaOnlyRejectsTruncateBeforeCopy(t *testing.T) {
	for _, tc := range []struct {
		name         string
		truncateMode string
	}{
		{name: "true", truncateMode: "true"},
		{name: "once", truncateMode: `"once"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "schema-only-truncate.toml")

			content := fmt.Sprintf(`
schema = "target"
schema_only = true
truncate_before_copy = %s

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`, tc.truncateMode)
			if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}

			_, err := loadConfig(cfgFile)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), "truncate_before_copy is incompatible with schema_only") {
				t.Fatalf("error = %v, want schema_only/truncate incompatibility", err)
			}
		})
	}
}

func TestLoadConfig_DSNEnvOverrides(t *testing.T) {
	tests := []struct {
		name        string
		sourceDSN   string
		targetDSN   string
		env         map[string]string
		wantSource  string
		wantTarget  string
		wantErrText string
	}{
		{
			name:       "unset env keeps toml values",
			sourceDSN:  "root:root@tcp(127.0.0.1:3306)/from_toml",
			targetDSN:  "postgres://user:pass@localhost:5432/from_toml",
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_toml",
			wantTarget: "postgres://user:pass@localhost:5432/from_toml",
		},
		{
			name:       "source env overrides toml",
			sourceDSN:  "root:root@tcp(127.0.0.1:3306)/from_toml",
			targetDSN:  "postgres://user:pass@localhost:5432/from_toml",
			env:        map[string]string{"PGFERRY_SOURCE_DSN": "root:root@tcp(127.0.0.1:3306)/from_env"},
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_env",
			wantTarget: "postgres://user:pass@localhost:5432/from_toml",
		},
		{
			name:       "target env overrides toml",
			sourceDSN:  "root:root@tcp(127.0.0.1:3306)/from_toml",
			targetDSN:  "postgres://user:pass@localhost:5432/from_toml",
			env:        map[string]string{"PGFERRY_TARGET_DSN": "postgres://user:pass@localhost:5432/from_env"},
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_toml",
			wantTarget: "postgres://user:pass@localhost:5432/from_env",
		},
		{
			name:       "both env vars override toml",
			sourceDSN:  "root:root@tcp(127.0.0.1:3306)/from_toml",
			targetDSN:  "postgres://user:pass@localhost:5432/from_toml",
			env:        map[string]string{"PGFERRY_SOURCE_DSN": "root:root@tcp(127.0.0.1:3306)/from_env", "PGFERRY_TARGET_DSN": "postgres://user:pass@localhost:5432/from_env"},
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_env",
			wantTarget: "postgres://user:pass@localhost:5432/from_env",
		},
		{
			name:       "empty env does not override",
			sourceDSN:  "root:root@tcp(127.0.0.1:3306)/from_toml",
			targetDSN:  "postgres://user:pass@localhost:5432/from_toml",
			env:        map[string]string{"PGFERRY_SOURCE_DSN": "   ", "PGFERRY_TARGET_DSN": ""},
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_toml",
			wantTarget: "postgres://user:pass@localhost:5432/from_toml",
		},
		{
			name:       "source env can supply omitted toml dsn",
			targetDSN:  "postgres://user:pass@localhost:5432/from_toml",
			env:        map[string]string{"PGFERRY_SOURCE_DSN": "root:root@tcp(127.0.0.1:3306)/from_env"},
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_env",
			wantTarget: "postgres://user:pass@localhost:5432/from_toml",
		},
		{
			name:       "target env can supply omitted toml dsn",
			sourceDSN:  "root:root@tcp(127.0.0.1:3306)/from_toml",
			env:        map[string]string{"PGFERRY_TARGET_DSN": "postgres://user:pass@localhost:5432/from_env"},
			wantSource: "root:root@tcp(127.0.0.1:3306)/from_toml",
			wantTarget: "postgres://user:pass@localhost:5432/from_env",
		},
		{
			name:        "missing source dsn still errors when env unset",
			targetDSN:   "postgres://user:pass@localhost:5432/from_toml",
			wantErrText: "source.dsn is required",
		},
		{
			name:        "missing target dsn still errors when env unset",
			sourceDSN:   "root:root@tcp(127.0.0.1:3306)/from_toml",
			wantErrText: "target.dsn is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "migration.toml")

			var b strings.Builder
			b.WriteString("schema = \"app\"\n\n")
			b.WriteString("[source]\n")
			b.WriteString("type = \"mysql\"\n")
			if tt.sourceDSN != "" {
				b.WriteString("dsn = " + strconv.Quote(tt.sourceDSN) + "\n")
			}
			b.WriteString("\n[target]\n")
			if tt.targetDSN != "" {
				b.WriteString("dsn = " + strconv.Quote(tt.targetDSN) + "\n")
			}

			if err := os.WriteFile(cfgFile, []byte(b.String()), 0644); err != nil {
				t.Fatal(err)
			}

			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			cfg, err := loadConfig(cfgFile)
			if tt.wantErrText != "" {
				if err == nil {
					t.Fatalf("loadConfig() error = nil, want substring %q", tt.wantErrText)
				}
				if !strings.Contains(err.Error(), tt.wantErrText) {
					t.Fatalf("loadConfig() error = %v, want substring %q", err, tt.wantErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadConfig() error: %v", err)
			}
			if cfg.Source.DSN != tt.wantSource {
				t.Fatalf("Source.DSN = %q, want %q", cfg.Source.DSN, tt.wantSource)
			}
			if cfg.Target.DSN != tt.wantTarget {
				t.Fatalf("Target.DSN = %q, want %q", cfg.Target.DSN, tt.wantTarget)
			}
		})
	}
}

func TestLoadConfig_TypeMappingOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "type_mapping.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
tinyint1_as_boolean = true
binary16_as_uuid = true
datetime_as_timestamptz = true
json_as_jsonb = false
varchar_as_text = true
enum_mode = "check"
set_mode = "text_array"
sanitize_json_null_bytes = false
unknown_as_text = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if !cfg.TypeMapping.TinyInt1AsBoolean {
		t.Errorf("TypeMapping.TinyInt1AsBoolean = %t, want true", cfg.TypeMapping.TinyInt1AsBoolean)
	}
	if !cfg.TypeMapping.Binary16AsUUID {
		t.Errorf("TypeMapping.Binary16AsUUID = %t, want true", cfg.TypeMapping.Binary16AsUUID)
	}
	if !cfg.TypeMapping.DatetimeAsTimestamptz {
		t.Errorf("TypeMapping.DatetimeAsTimestamptz = %t, want true", cfg.TypeMapping.DatetimeAsTimestamptz)
	}
	if cfg.TypeMapping.JSONAsJSONB {
		t.Errorf("TypeMapping.JSONAsJSONB = %t, want false", cfg.TypeMapping.JSONAsJSONB)
	}
	if cfg.TypeMapping.EnumMode != "check" {
		t.Errorf("TypeMapping.EnumMode = %q, want %q", cfg.TypeMapping.EnumMode, "check")
	}
	if cfg.TypeMapping.SetMode != "text_array" {
		t.Errorf("TypeMapping.SetMode = %q, want %q", cfg.TypeMapping.SetMode, "text_array")
	}
	if !cfg.TypeMapping.VarcharAsText {
		t.Errorf("TypeMapping.VarcharAsText = %t, want true", cfg.TypeMapping.VarcharAsText)
	}
	if cfg.TypeMapping.SanitizeJSONNullBytes {
		t.Errorf("TypeMapping.SanitizeJSONNullBytes = %t, want false", cfg.TypeMapping.SanitizeJSONNullBytes)
	}
	if !cfg.TypeMapping.UnknownAsText {
		t.Errorf("TypeMapping.UnknownAsText = %t, want true", cfg.TypeMapping.UnknownAsText)
	}
}

func TestLoadConfig_TableFilters(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
include_tables = [" Orders ", "order_items"]
exclude_tables = ["audit_log"]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if got := strings.Join(cfg.IncludeTables, ","); got != "Orders,order_items" {
		t.Fatalf("IncludeTables = %q, want Orders,order_items", got)
	}
	if got := strings.Join(cfg.ExcludeTables, ","); got != "audit_log" {
		t.Fatalf("ExcludeTables = %q, want audit_log", got)
	}
}

func TestLoadConfig_ColumnFilters(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
column_filter_mode = "glob"
exclude_columns = [" RowVersion ", "orders.sys_*"]

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@127.0.0.1:1433?database=db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if got := cfg.ColumnFilterMode; got != "glob" {
		t.Fatalf("ColumnFilterMode = %q, want glob", got)
	}
	if got := strings.Join(cfg.ExcludeColumns, ","); got != "RowVersion,orders.sys_*" {
		t.Fatalf("ExcludeColumns = %q, want RowVersion,orders.sys_*", got)
	}
}

func TestLoadConfig_ColumnFiltersRejectGlobPatternsInExactMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
exclude_columns = ["rv_*"]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "glob patterns are not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_ColumnFiltersRejectDuplicateAfterNormalization(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
exclude_columns = ["Orders.RowVersion", " orders.rowversion "]

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@127.0.0.1:1433?database=db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "duplicate column filter") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_ColumnFiltersRejectMultiDotEntries(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
exclude_columns = ["dbo.Orders.RowVersion"]

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@127.0.0.1:1433?database=db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "only ColumnName or TableName.ColumnName are supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_TableFiltersRejectDuplicateAfterNormalization(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
include_tables = ["Orders", " orders "]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "duplicate table name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_TableFiltersRejectGlobPatterns(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
exclude_tables = ["tmp_*"]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "glob patterns are not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_TableFiltersAllowGlobMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
table_filter_mode = "glob"
include_tables = [" app_* ", "audit_?"]
exclude_tables = ["app_tmp_*"]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if got := cfg.TableFilterMode; got != "glob" {
		t.Fatalf("TableFilterMode = %q, want glob", got)
	}
	if got := strings.Join(cfg.IncludeTables, ","); got != "app_*,audit_?" {
		t.Fatalf("IncludeTables = %q, want app_*,audit_?", got)
	}
	if got := strings.Join(cfg.ExcludeTables, ","); got != "app_tmp_*" {
		t.Fatalf("ExcludeTables = %q, want app_tmp_*", got)
	}
}

func TestLoadConfig_TableFiltersRejectInvalidGlobPattern(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
table_filter_mode = "glob"
exclude_tables = ["tmp_["]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid glob pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_TableFiltersRejectEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "filters.toml")

	content := `
schema = "target"
include_tables = ["   "]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "entries must be non-empty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_SchemaOnly(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "schema_only.toml")

	content := `
schema = "target"
schema_only = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if !cfg.SchemaOnly {
		t.Errorf("SchemaOnly = %t, want true", cfg.SchemaOnly)
	}
	if cfg.DataOnly {
		t.Errorf("DataOnly = %t, want false", cfg.DataOnly)
	}
}

func TestLoadConfig_DataOnly(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "data_only.toml")

	content := `
schema = "target"
data_only = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.SchemaOnly {
		t.Errorf("SchemaOnly = %t, want false", cfg.SchemaOnly)
	}
	if !cfg.DataOnly {
		t.Errorf("DataOnly = %t, want true", cfg.DataOnly)
	}
}

func TestLoadConfig_SchemaOnlyAndDataOnlyConflict(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "conflict.toml")

	content := `
schema = "target"
schema_only = true
data_only = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error when both schema_only and data_only are true")
	}
}

func TestLoadConfig_WorkersNonPositiveUsesDefault(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "workers.toml")

	content := `
schema = "target"
workers = 0

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	wantWorkers := defaultWorkers()
	if cfg.Workers != wantWorkers {
		t.Errorf("Workers = %d, want %d", cfg.Workers, wantWorkers)
	}
}

func TestLoadConfig_MissingDSN(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad.toml")

	content := `schema = "x"
[source]
type = "mysql"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for missing DSNs")
	}
}

func TestLoadConfig_MissingSchema(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_schema.toml")

	content := `
[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for missing schema")
	}
}

func TestLoadConfig_WhitespaceSchemaRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_schema_ws.toml")

	content := `
schema = "   "

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for whitespace schema")
	}
}

func TestLoadConfig_InvalidOnSchemaExists(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_mode.toml")

	content := `
schema = "target"
on_schema_exists = "merge"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid on_schema_exists")
	}
}

func TestLoadConfig_UseOnSchemaExists(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "use_mode.toml")

	content := `
schema = "target"
on_schema_exists = "use"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.OnSchemaExists != "use" {
		t.Fatalf("OnSchemaExists = %q, want %q", cfg.OnSchemaExists, "use")
	}
}

func TestLoadConfig_InvalidSourceSnapshotMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_snapshot_mode.toml")

	content := `
schema = "target"
source_snapshot_mode = "repeatable_read"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid source_snapshot_mode")
	}
}

func TestLoadConfig_InvalidEnumMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_enum_mode.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
enum_mode = "pg_enum"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid type_mapping.enum_mode")
	}
}

func TestLoadConfig_EnumModeNative(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "enum_native.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
enum_mode = "native"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.TypeMapping.EnumMode != "native" {
		t.Errorf("TypeMapping.EnumMode = %q, want %q", cfg.TypeMapping.EnumMode, "native")
	}
}

func TestLoadConfig_Binary16UUIDModeSwap(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "uuid_swap.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
binary16_as_uuid = true
binary16_uuid_mode = "mysql_uuid_to_bin_swap"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.TypeMapping.Binary16UUIDMode != "mysql_uuid_to_bin_swap" {
		t.Errorf("Binary16UUIDMode = %q, want %q", cfg.TypeMapping.Binary16UUIDMode, "mysql_uuid_to_bin_swap")
	}
}

func TestLoadConfig_Binary16UUIDModeWithoutBinary16Rejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "uuid_swap_no_b16.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
binary16_uuid_mode = "mysql_uuid_to_bin_swap"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for binary16_uuid_mode without binary16_as_uuid")
	}
}

func TestLoadConfig_ZeroDateModeError(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "zero_date.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
zero_date_mode = "error"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.TypeMapping.ZeroDateMode != "error" {
		t.Errorf("ZeroDateMode = %q, want %q", cfg.TypeMapping.ZeroDateMode, "error")
	}
}

func TestLoadConfig_SpatialModeWKBBytea(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "spatial_wkb.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
spatial_mode = "wkb_bytea"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.TypeMapping.SpatialMode != "wkb_bytea" {
		t.Errorf("SpatialMode = %q, want %q", cfg.TypeMapping.SpatialMode, "wkb_bytea")
	}
}

func TestLoadConfig_InvalidSpatialMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_spatial.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
spatial_mode = "postgis"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid spatial_mode")
	}
}

func TestLoadConfig_PostGIS(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "postgis.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[postgis]
enabled = true
create_extension = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if !cfg.PostGIS.Enabled {
		t.Fatal("PostGIS.Enabled = false, want true")
	}
	if !cfg.PostGIS.CreateExtension {
		t.Fatal("PostGIS.CreateExtension = false, want true")
	}
}

func TestLoadConfig_PostGISCreateExtensionRequiresEnabled(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "postgis_bad.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[postgis]
create_extension = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for postgis.create_extension without enabled")
	}
}

func TestLoadConfig_PostGISOnlySupportsMySQL(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "postgis_sqlite.toml")

	content := `
schema = "target"

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"

[postgis]
enabled = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for postgis on sqlite")
	}
}

func TestLoadConfig_PostGISRejectsMariaDBExplicitly(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "postgis_mariadb.toml")

	content := `
schema = "target"

[source]
type = "mariadb"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[postgis]
enabled = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for postgis on mariadb")
	}
	if !strings.Contains(err.Error(), "mariadb supports only type_mapping.spatial_mode fallback modes for now") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_PostGISConflictsWithSpatialMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "postgis_conflict.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[postgis]
enabled = true

[type_mapping]
spatial_mode = "wkt_text"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for postgis + spatial_mode")
	}
}

func TestLoadConfig_InvalidZeroDateMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_zero_date.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
zero_date_mode = "text"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid zero_date_mode")
	}
}

func TestLoadConfig_Binary16UUIDModeInvalid(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "uuid_bad_mode.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
binary16_as_uuid = true
binary16_uuid_mode = "comb"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid binary16_uuid_mode")
	}
}

func TestLoadConfig_SetModeTextArrayCheck(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "set_text_array_check.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
set_mode = "text_array_check"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.TypeMapping.SetMode != "text_array_check" {
		t.Errorf("TypeMapping.SetMode = %q, want %q", cfg.TypeMapping.SetMode, "text_array_check")
	}
}

func TestLoadConfig_InvalidSetMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_set_mode.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
set_mode = "array"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid type_mapping.set_mode")
	}
}

func TestLoadConfig_SQLiteSingleTxRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_single_tx.toml")

	content := `
schema = "target"
source_snapshot_mode = "single_tx"

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for SQLite + single_tx")
	}
}

func TestLoadConfig_SQLiteWorkersCapped(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_workers.toml")

	content := `
schema = "target"
workers = 8

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Workers != 1 {
		t.Errorf("Workers = %d, want 1 (SQLite caps at 1)", cfg.Workers)
	}
	if cfg.IndexWorkers != 1 {
		t.Errorf("IndexWorkers = %d, want 1 (SQLite caps at 1)", cfg.IndexWorkers)
	}
}

func TestLoadConfig_SQLiteIndexWorkersCapped(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_index_workers.toml")

	content := `
schema = "target"
index_workers = 4

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.IndexWorkers != 1 {
		t.Errorf("IndexWorkers = %d, want 1 (SQLite caps at 1)", cfg.IndexWorkers)
	}
}

func TestLoadConfig_SQLiteMySQLOnlyTypeMappingRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_bad_mapping.toml")

	content := `
schema = "target"

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
tinyint1_as_boolean = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for MySQL-only type mapping option with SQLite source")
	}
}

func TestLoadConfig_InvalidCollationMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_collation_mode.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
collation_mode = "strict"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid collation_mode")
	}
	if !strings.Contains(err.Error(), "collation_mode") {
		t.Errorf("error should mention collation_mode, got: %v", err)
	}
}

func TestLoadConfig_CollationMapParsed(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "collation_map.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
collation_mode = "auto"

[type_mapping.collation_map]
utf8mb4_general_ci = "und-x-icu"
utf8mb4_unicode_ci = "und-x-icu"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.TypeMapping.CollationMode != "auto" {
		t.Errorf("CollationMode = %q, want %q", cfg.TypeMapping.CollationMode, "auto")
	}
	if len(cfg.TypeMapping.CollationMap) != 2 {
		t.Fatalf("CollationMap length = %d, want 2", len(cfg.TypeMapping.CollationMap))
	}
	if cfg.TypeMapping.CollationMap["utf8mb4_general_ci"] != "und-x-icu" {
		t.Errorf("CollationMap[utf8mb4_general_ci] = %q", cfg.TypeMapping.CollationMap["utf8mb4_general_ci"])
	}
}

func TestLoadConfig_SourceCharsetOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "charset.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"
charset = "latin1"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Source.Charset != "latin1" {
		t.Errorf("Source.Charset = %q, want %q", cfg.Source.Charset, "latin1")
	}
}

func TestLoadConfig_MariaDBCharsetOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mariadb_charset.toml")

	content := `
schema = "target"

[source]
type = "mariadb"
dsn = "root:root@tcp(127.0.0.1:3306)/db"
charset = "latin1"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Source.Charset != "latin1" {
		t.Errorf("Source.Charset = %q, want %q", cfg.Source.Charset, "latin1")
	}
}

func TestLoadConfig_SQLiteCharsetRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_charset.toml")

	content := `
schema = "target"

[source]
type = "sqlite"
dsn = "/tmp/test.db"
charset = "latin1"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for SQLite + charset override")
	}
	if !strings.Contains(err.Error(), "MySQL/MariaDB-only") {
		t.Errorf("error should mention MySQL/MariaDB-only, got: %v", err)
	}
}

func TestLoadConfig_SQLiteCollationModeRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_collation_mode.toml")

	content := `
schema = "target"

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
collation_mode = "auto"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for SQLite + collation_mode=auto")
	}
}

func TestLoadConfig_SQLiteCollationMapRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sqlite_collation_map.toml")

	content := `
schema = "target"

[source]
type = "sqlite"
dsn = "/tmp/test.db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping.collation_map]
utf8mb4_general_ci = "und-x-icu"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for SQLite + collation_map")
	}
}

func TestLoadConfig_UnknownKeysRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "unknown_keys.toml")

	content := `
schema = "target"
bogus_option = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for unknown config key")
	}
	if !strings.Contains(err.Error(), "bogus_option") {
		t.Errorf("error should mention the unknown key, got: %v", err)
	}
}

func TestLoadConfig_UnknownNestedKeysRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "unknown_nested.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
json_as_jsonb = true
made_up_flag = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for unknown nested config key")
	}
	if !strings.Contains(err.Error(), "type_mapping.made_up_flag") {
		t.Errorf("error should mention the unknown key, got: %v", err)
	}
}

func TestLoadConfig_UnknownSectionRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "unknown_section.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"

[advanced]
turbo = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for unknown section")
	}
	if !strings.Contains(err.Error(), "advanced") {
		t.Errorf("error should mention the unknown section, got: %v", err)
	}
}

func TestResolvePath(t *testing.T) {
	cfg := &MigrationConfig{configDir: "/home/user/migrations"}

	got := cfg.resolvePath("cleanup.sql")
	want := "/home/user/migrations/cleanup.sql"
	if got != want {
		t.Errorf("resolvePath(relative) = %q, want %q", got, want)
	}

	got = cfg.resolvePath("/absolute/path.sql")
	want = "/absolute/path.sql"
	if got != want {
		t.Errorf("resolvePath(absolute) = %q, want %q", got, want)
	}
}

func TestDefaultWorkers(t *testing.T) {
	got := defaultWorkers()
	if got < 1 || got > 8 {
		t.Fatalf("defaultWorkers() out of bounds: %d", got)
	}

	want := runtime.NumCPU()
	if want < 1 {
		want = 1
	}
	if want > 8 {
		want = 8
	}
	if got != want {
		t.Fatalf("defaultWorkers() = %d, want %d", got, want)
	}
}

func TestLoadConfig_ChunkingDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "chunk_defaults.toml")

	content := `
schema = "target"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.ChunkSize != 100000 {
		t.Errorf("default ChunkSize = %d, want 100000", cfg.ChunkSize)
	}
	if cfg.Resume {
		t.Errorf("default Resume = %t, want false", cfg.Resume)
	}
	if cfg.Validation != "none" {
		t.Errorf("default Validation = %q, want %q", cfg.Validation, "none")
	}
	if !cfg.CopyRiskAnalysis {
		t.Errorf("default CopyRiskAnalysis = %t, want true", cfg.CopyRiskAnalysis)
	}
}

func TestLoadConfig_ChunkingExplicit(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "chunk_explicit.toml")

	content := `
schema = "target"
unlogged_tables = false
chunk_size = 50000
resume = true
validation = "row_count"
clean_orphans_mode = "report"
clean_orphans_max_rows = 12
copy_risk_analysis = false

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.ChunkSize != 50000 {
		t.Errorf("ChunkSize = %d, want 50000", cfg.ChunkSize)
	}
	if !cfg.Resume {
		t.Errorf("Resume = %t, want true", cfg.Resume)
	}
	if cfg.Validation != "row_count" {
		t.Errorf("Validation = %q, want %q", cfg.Validation, "row_count")
	}
	if cfg.CleanOrphansMode != "report" {
		t.Errorf("CleanOrphansMode = %q, want %q", cfg.CleanOrphansMode, "report")
	}
	if cfg.CleanOrphansMaxRows != 12 {
		t.Errorf("CleanOrphansMaxRows = %d, want 12", cfg.CleanOrphansMaxRows)
	}
	if cfg.CopyRiskAnalysis {
		t.Errorf("CopyRiskAnalysis = %t, want false", cfg.CopyRiskAnalysis)
	}
}

func TestLoadConfig_InvalidValidation(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_validation.toml")

	content := `
schema = "target"
validation = "full_hash"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid validation mode")
	}
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("error should mention validation, got: %v", err)
	}
}

func TestLoadConfig_SampledHashValidation(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "sampled_hash.toml")

	content := `
schema = "target"
validation = "sampled_hash"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.Validation != "sampled_hash" {
		t.Fatalf("Validation = %q, want %q", cfg.Validation, "sampled_hash")
	}
}

func TestLoadConfig_InvalidCleanOrphansMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_orphan_mode.toml")

	content := `
schema = "target"
clean_orphans_mode = "warn"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for invalid clean_orphans_mode")
	}
	if !strings.Contains(err.Error(), "clean_orphans_mode") {
		t.Fatalf("error should mention clean_orphans_mode, got: %v", err)
	}
}

func TestLoadConfig_InvalidCleanOrphansMaxRows(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad_orphan_max_rows.toml")

	content := `
schema = "target"
clean_orphans_max_rows = -1

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for negative clean_orphans_max_rows")
	}
	if !strings.Contains(err.Error(), "clean_orphans_max_rows") {
		t.Fatalf("error should mention clean_orphans_max_rows, got: %v", err)
	}
}

func TestLoadConfig_CleanOrphansDisabledAllowsModeAndMaxRows(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "clean_orphans_disabled.toml")

	content := `
schema = "target"
clean_orphans = false
clean_orphans_mode = "report"
clean_orphans_max_rows = 25

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.CleanOrphans {
		t.Fatalf("CleanOrphans = %t, want false", cfg.CleanOrphans)
	}
	if cfg.CleanOrphansMode != "report" {
		t.Fatalf("CleanOrphansMode = %q, want %q", cfg.CleanOrphansMode, "report")
	}
	if cfg.CleanOrphansMaxRows != 25 {
		t.Fatalf("CleanOrphansMaxRows = %d, want 25", cfg.CleanOrphansMaxRows)
	}
}

func TestLoadConfig_ResumeWithRecreateConflict(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "resume_recreate.toml")

	content := `
schema = "target"
resume = true
on_schema_exists = "recreate"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for resume + recreate")
	}
	if !strings.Contains(err.Error(), "resume") {
		t.Errorf("error should mention resume, got: %v", err)
	}
}

func TestLoadConfig_ResumeWithUseConflict(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "resume_use.toml")

	content := `
schema = "target"
resume = true
on_schema_exists = "use"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for resume + use")
	}
	if !strings.Contains(err.Error(), "resume") {
		t.Errorf("error should mention resume, got: %v", err)
	}
}

func TestLoadConfig_ResumeWithSchemaOnlyConflict(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "resume_schema_only.toml")

	content := `
schema = "target"
resume = true
schema_only = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for resume + schema_only")
	}
}

func TestLoadConfig_ResumeWithUnloggedConflict(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "resume_unlogged.toml")

	content := `
schema = "target"
resume = true
unlogged_tables = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for resume + unlogged_tables")
	}
	if !strings.Contains(err.Error(), "unlogged_tables") {
		t.Fatalf("error should mention unlogged_tables, got: %v", err)
	}
}

func TestLoadConfig_IndexWorkersExplicit(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "index_workers.toml")

	content := `
schema = "target"
workers = 8
index_workers = 2

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Workers != 8 {
		t.Errorf("Workers = %d, want 8", cfg.Workers)
	}
	if cfg.IndexWorkers != 2 {
		t.Errorf("IndexWorkers = %d, want 2", cfg.IndexWorkers)
	}
}

func TestLoadConfig_TruncateBeforeCopyOnce(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "truncate_once.toml")

	content := `
schema = "schema_a"
data_only = true
truncate_before_copy = "once"
truncate_before_copy_schemas = ["schema_a", "schema_b"]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.TruncateBeforeCopy != truncateBeforeCopyOnce {
		t.Fatalf("TruncateBeforeCopy = %s, want %s", cfg.TruncateBeforeCopy, truncateBeforeCopyOnce)
	}
	if strings.Join(cfg.TruncateBeforeCopySchemas, ",") != "schema_a,schema_b" {
		t.Fatalf("TruncateBeforeCopySchemas = %v, want [schema_a schema_b]", cfg.TruncateBeforeCopySchemas)
	}
}

func TestLoadConfig_TruncateBeforeCopyOnceDefaultsSchemasToTargetSchema(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "truncate_once_default_schemas.toml")

	content := `
schema = "schema_a"
data_only = true
truncate_before_copy = "once"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if strings.Join(cfg.TruncateBeforeCopySchemas, ",") != "schema_a" {
		t.Fatalf("TruncateBeforeCopySchemas = %v, want [schema_a]", cfg.TruncateBeforeCopySchemas)
	}
}

func TestLoadConfig_TruncateBeforeCopySchemasRequiresOnceMode(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "truncate_schemas_without_once.toml")

	content := `
schema = "schema_a"
data_only = true
truncate_before_copy = true
truncate_before_copy_schemas = ["schema_a", "schema_b"]

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `truncate_before_copy_schemas has no effect unless truncate_before_copy = "once"`) {
		t.Fatalf("error = %v, want truncate_before_copy_schemas guidance", err)
	}
}

func TestLoadConfig_IndexWorkersDefaultsToWorkers(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "index_workers_default.toml")

	content := `
schema = "target"
workers = 6

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.IndexWorkers != 6 {
		t.Errorf("IndexWorkers = %d, want 6 (should default to Workers)", cfg.IndexWorkers)
	}
}

func TestLoadConfig_ChunkSizeNonPositiveUsesDefault(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "chunk_size_zero.toml")

	content := `
schema = "target"
chunk_size = 0

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}
	if cfg.ChunkSize != 100000 {
		t.Errorf("ChunkSize = %d, want 100000 (default)", cfg.ChunkSize)
	}
}

func TestIdentifierCase_AcceptsPreserve(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "user:pass@tcp(127.0.0.1:3306)/db"
	cfg.Target.DSN = "postgres://u:p@127.0.0.1/db?sslmode=disable"
	cfg.Schema = "public"
	cfg.IdentifierCase = "preserve"

	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("finalizeConfig error: %v", err)
	}
	if cfg.IdentifierCase != "preserve" {
		t.Fatalf("IdentifierCase after finalize = %q, want preserve", cfg.IdentifierCase)
	}
}

func TestIdentifierCase_RejectsInvalid(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "user:pass@tcp(127.0.0.1:3306)/db"
	cfg.Target.DSN = "postgres://u:p@127.0.0.1/db?sslmode=disable"
	cfg.Schema = "public"
	cfg.IdentifierCase = "camel"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for identifier_case = camel")
	}
	if !strings.Contains(err.Error(), "identifier_case must be one of") {
		t.Fatalf("error = %q, want identifier_case validation message", err.Error())
	}
	if !strings.Contains(err.Error(), "preserve") {
		t.Fatalf("error = %q, want it to list preserve as a valid value", err.Error())
	}
}

func TestColumnCollisionMode_RejectsInvalid(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "user:pass@tcp(127.0.0.1:3306)/db"
	cfg.Target.DSN = "postgres://u:p@127.0.0.1/db?sslmode=disable"
	cfg.Schema = "public"
	cfg.ColumnCollisionMode = "rename"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for column_collision_mode = rename")
	}
	if !strings.Contains(err.Error(), "column_collision_mode must be one of") {
		t.Fatalf("error = %q, want column_collision_mode validation message", err.Error())
	}
	if !strings.Contains(err.Error(), "auto") {
		t.Fatalf("error = %q, want it to list auto as a valid value", err.Error())
	}
}
