package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "test.toml")

	content := `
schema = "myschema"
workers = 8
batch_size = 10000

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
	if cfg.BatchSize != 10000 {
		t.Errorf("BatchSize = %d, want 10000", cfg.BatchSize)
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
[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/db"

[postgres]
dsn = "postgres://u:p@h:5432/db"
`
	// schema, workers, batch_size omitted — defaults should apply
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Schema != "app" {
		t.Errorf("default Schema = %q, want %q", cfg.Schema, "app")
	}
	if cfg.Workers != 4 {
		t.Errorf("default Workers = %d, want 4", cfg.Workers)
	}
	if cfg.BatchSize != 50000 {
		t.Errorf("default BatchSize = %d, want 50000", cfg.BatchSize)
	}
}

func TestLoadConfig_MissingDSN(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad.toml")

	if err := os.WriteFile(cfgFile, []byte(`[mysql]`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for missing DSNs")
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
