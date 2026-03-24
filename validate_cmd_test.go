package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunValidate_UsesPositionalConfig(t *testing.T) {
	cfgPath := writeValidateTestConfig(t, "validation = \"row_count\"\n")

	prevRunner := validateConfigRunner
	prevConfigPath := validateConfigPath
	validateConfigRunner = func(cfg *MigrationConfig) error {
		if cfg.Validation != validationModeRowCount {
			t.Fatalf("Validation = %q, want %q", cfg.Validation, validationModeRowCount)
		}
		if cfg.Schema != "app" {
			t.Fatalf("Schema = %q, want %q", cfg.Schema, "app")
		}
		return fmt.Errorf("validate called")
	}
	t.Cleanup(func() {
		validateConfigRunner = prevRunner
		validateConfigPath = prevConfigPath
	})

	err := runValidate(&cobra.Command{}, []string{cfgPath})
	if err == nil || err.Error() != "validate called" {
		t.Fatalf("runValidate() error = %v, want validate called", err)
	}
}

func TestRunValidate_UsesConfigFlag(t *testing.T) {
	cfgPath := writeValidateTestConfig(t, "validation = \"sampled_hash\"\n")

	prevRunner := validateConfigRunner
	prevConfigPath := validateConfigPath
	validateConfigPath = cfgPath
	validateConfigRunner = func(cfg *MigrationConfig) error {
		if cfg.Validation != validationModeSampledHash {
			t.Fatalf("Validation = %q, want %q", cfg.Validation, validationModeSampledHash)
		}
		return fmt.Errorf("validate called")
	}
	t.Cleanup(func() {
		validateConfigRunner = prevRunner
		validateConfigPath = prevConfigPath
	})

	err := runValidate(&cobra.Command{}, nil)
	if err == nil || err.Error() != "validate called" {
		t.Fatalf("runValidate() error = %v, want validate called", err)
	}
}

func TestRunValidate_RejectsConfigFlagAndPositionalPathTogether(t *testing.T) {
	cfgPath := writeValidateTestConfig(t, "validation = \"row_count\"\n")

	prevConfigPath := validateConfigPath
	t.Cleanup(func() {
		validateConfigPath = prevConfigPath
	})
	validateConfigPath = cfgPath

	err := runValidate(&cobra.Command{}, []string{"other.toml"})
	if err == nil {
		t.Fatal("runValidate() error = nil, want conflict error")
	}
	if !strings.Contains(err.Error(), "provide either a positional migration.toml path or --config, not both") {
		t.Fatalf("runValidate() error = %q, want config conflict", err.Error())
	}
}

func TestRunValidate_MissingConfig(t *testing.T) {
	prevConfigPath := validateConfigPath
	t.Cleanup(func() {
		validateConfigPath = prevConfigPath
	})
	validateConfigPath = ""

	err := runValidate(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runValidate() error = nil, want missing config error")
	}
	want := "config file required: pgferry validate <migration.toml> or pgferry validate --config <migration.toml>"
	if err.Error() != want {
		t.Fatalf("runValidate() error = %q, want %q", err.Error(), want)
	}
}

func TestRunValidate_NonexistentConfigFile(t *testing.T) {
	prevConfigPath := validateConfigPath
	t.Cleanup(func() {
		validateConfigPath = prevConfigPath
	})
	validateConfigPath = ""

	err := runValidate(&cobra.Command{}, []string{"does-not-exist.toml"})
	if err == nil {
		t.Fatal("runValidate() error = nil, want read config error")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Fatalf("runValidate() error = %q, want read config error", err.Error())
	}
}

func TestRunValidateWithConfig_RejectsDisabledValidation(t *testing.T) {
	err := runValidateWithConfig(&MigrationConfig{Validation: validationModeNone})
	if err == nil {
		t.Fatal("runValidateWithConfig() error = nil, want disabled validation error")
	}
	if !strings.Contains(err.Error(), "validation disabled") {
		t.Fatalf("runValidateWithConfig() error = %q, want disabled validation message", err.Error())
	}
}

func TestLogStandaloneValidationPlan(t *testing.T) {
	var buf strings.Builder
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logStandaloneValidationPlan(validationModeSampledHash)

	out := buf.String()
	for _, want := range []string{
		"validation mode: sampled_hash",
		"current source state",
		"does not rerun after_data hooks",
		"samples deterministic rows",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q:\n%s", want, out)
		}
	}
	for _, notWant := range []string{
		"single_tx applies only to the COPY phase",
		"re-reads the source after COPY",
	} {
		if strings.Contains(out, notWant) {
			t.Fatalf("log output unexpectedly contains %q:\n%s", notWant, out)
		}
	}
}

func writeValidateTestConfig(t *testing.T, extra string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "migration.toml")
	text := strings.Join([]string{
		"schema = \"app\"",
		extra,
		"",
		"[source]",
		"type = \"mysql\"",
		"dsn = \"root:root@tcp(127.0.0.1:3306)/source\"",
		"",
		"[target]",
		"dsn = \"postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable\"",
	}, "\n")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}
