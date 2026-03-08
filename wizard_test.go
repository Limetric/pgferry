package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunGenerateWizardWritesConfig(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "migration.toml")

	input := strings.Join([]string{
		"sqlite",
		"/tmp/source.db",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"write",
		outputPath,
	}, "\n") + "\n"

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&bytes.Buffer{})

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}

	text := string(data)
	if !strings.Contains(text, `schema = "source"`) {
		t.Fatalf("generated config missing schema, got:\n%s", text)
	}
	if !strings.Contains(text, "[source]\ntype = \"sqlite\"") {
		t.Fatalf("generated config missing source section, got:\n%s", text)
	}
	if !strings.Contains(text, "[target]\ndsn = \"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable\"") {
		t.Fatalf("generated config missing target section, got:\n%s", text)
	}
}

func TestRunGenerateWizardRunsGeneratedConfig(t *testing.T) {
	dir := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prevWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	var gotCfg *MigrationConfig
	prevRunner := generatedConfigRunner
	generatedConfigRunner = func(cfg *MigrationConfig) error {
		gotCfg = cfg
		return nil
	}
	t.Cleanup(func() {
		generatedConfigRunner = prevRunner
	})

	input := strings.Join([]string{
		"",
		"root:root@tcp(127.0.0.1:3306)/sakila",
		"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"",
		"run",
	}, "\n") + "\n"

	cmd := &cobra.Command{}
	cmd.SetIn(strings.NewReader(input))
	cmd.SetOut(&bytes.Buffer{})

	if err := runGenerateWizard(cmd, nil); err != nil {
		t.Fatalf("runGenerateWizard() error: %v", err)
	}
	if gotCfg == nil {
		t.Fatal("expected generated config runner to be called")
	}
	if gotCfg.Source.Type != "mysql" {
		t.Fatalf("Source.Type = %q, want mysql", gotCfg.Source.Type)
	}
	if gotCfg.Schema != "sakila" {
		t.Fatalf("Schema = %q, want sakila", gotCfg.Schema)
	}
	if gotCfg.Workers != effectiveDefaultWorkers("mysql") {
		t.Fatalf("Workers = %d, want %d", gotCfg.Workers, effectiveDefaultWorkers("mysql"))
	}
	if gotCfg.configDir != dir {
		t.Fatalf("configDir = %q, want %q", gotCfg.configDir, dir)
	}
}

func TestRenderConfigTOMLIncludesOnlyConfiguredOverrides(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "root:root@tcp(127.0.0.1:3306)/sakila"
	cfg.Target.DSN = "postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable"
	cfg.Schema = "sakila"
	cfg.OnSchemaExists = "recreate"
	cfg.TypeMapping.JSONAsJSONB = true
	cfg.TypeMapping.UnknownAsText = true
	cfg.TypeMapping.TinyInt1AsBoolean = true
	cfg.Workers = 2

	rendered := renderConfigTOML(&cfg)

	if !strings.Contains(rendered, `on_schema_exists = "recreate"`) {
		t.Fatalf("expected on_schema_exists override, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "workers = 2") {
		t.Fatalf("expected workers override, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[type_mapping]") {
		t.Fatalf("expected type_mapping section, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "json_as_jsonb = true") {
		t.Fatalf("expected json_as_jsonb override, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "tinyint1_as_boolean = true") {
		t.Fatalf("expected tinyint1_as_boolean override, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "preserve_defaults = true") {
		t.Fatalf("did not expect default preserve_defaults in output, got:\n%s", rendered)
	}
}
