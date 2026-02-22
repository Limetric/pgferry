package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.toml")

	content := `
schema = "myschema"
on_schema_exists = "recreate"
unlogged_tables = true
preserve_defaults = true
add_unsigned_checks = true
replicate_on_update_current_timestamp = true
workers = 8

[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/testdb"

[postgres]
dsn = "postgres://user:pass@localhost:5432/testdb"

[hooks]
before_data = ["pre.sql"]
after_data = []
before_fk = ["cleanup.sql"]
after_all = ["post.sql"]
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.MySQL.DSN != "root:root@tcp(127.0.0.1:3306)/testdb" {
		t.Errorf("MySQL.DSN = %q", cfg.MySQL.DSN)
	}
	if cfg.Postgres.DSN != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("Postgres.DSN = %q", cfg.Postgres.DSN)
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
	if len(cfg.Hooks.BeforeFk) != 1 || cfg.Hooks.BeforeFk[0] != "cleanup.sql" {
		t.Errorf("Hooks.BeforeFk = %v", cfg.Hooks.BeforeFk)
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

	[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
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
	if cfg.UnloggedTables {
		t.Errorf("default UnloggedTables = %t, want false", cfg.UnloggedTables)
	}
	if cfg.PreserveDefaults {
		t.Errorf("default PreserveDefaults = %t, want false", cfg.PreserveDefaults)
	}
	if cfg.AddUnsignedChecks {
		t.Errorf("default AddUnsignedChecks = %t, want false", cfg.AddUnsignedChecks)
	}
	if cfg.ReplicateOnUpdateCurrentTimestamp {
		t.Errorf("default ReplicateOnUpdateCurrentTimestamp = %t, want false", cfg.ReplicateOnUpdateCurrentTimestamp)
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
	if cfg.TypeMapping.TinyInt1AsBoolean {
		t.Errorf("default TypeMapping.TinyInt1AsBoolean = %t, want false", cfg.TypeMapping.TinyInt1AsBoolean)
	}
	if cfg.TypeMapping.Binary16AsUUID {
		t.Errorf("default TypeMapping.Binary16AsUUID = %t, want false", cfg.TypeMapping.Binary16AsUUID)
	}
	if cfg.TypeMapping.DatetimeAsTimestamptz {
		t.Errorf("default TypeMapping.DatetimeAsTimestamptz = %t, want false", cfg.TypeMapping.DatetimeAsTimestamptz)
	}
	if cfg.TypeMapping.JSONAsJSONB {
		t.Errorf("default TypeMapping.JSONAsJSONB = %t, want false", cfg.TypeMapping.JSONAsJSONB)
	}
	if !cfg.TypeMapping.SanitizeJSONNullBytes {
		t.Errorf("default TypeMapping.SanitizeJSONNullBytes = %t, want true", cfg.TypeMapping.SanitizeJSONNullBytes)
	}
	if cfg.TypeMapping.UnknownAsText {
		t.Errorf("default TypeMapping.UnknownAsText = %t, want false", cfg.TypeMapping.UnknownAsText)
	}
}

func TestLoadConfig_TypeMappingOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "type_mapping.toml")

	content := `
schema = "target"

[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
tinyint1_as_boolean = true
binary16_as_uuid = true
datetime_as_timestamptz = true
json_as_jsonb = true
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
	if !cfg.TypeMapping.JSONAsJSONB {
		t.Errorf("TypeMapping.JSONAsJSONB = %t, want true", cfg.TypeMapping.JSONAsJSONB)
	}
	if cfg.TypeMapping.SanitizeJSONNullBytes {
		t.Errorf("TypeMapping.SanitizeJSONNullBytes = %t, want false", cfg.TypeMapping.SanitizeJSONNullBytes)
	}
	if !cfg.TypeMapping.UnknownAsText {
		t.Errorf("TypeMapping.UnknownAsText = %t, want true", cfg.TypeMapping.UnknownAsText)
	}
}

func TestLoadConfig_WorkersNonPositiveUsesDefault(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "workers.toml")

	content := `
schema = "target"
workers = 0

[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
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

	if err := os.WriteFile(cfgFile, []byte(`schema = "x"`+"\n"+`[mysql]`), 0644); err != nil {
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
[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
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

[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
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

[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
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
