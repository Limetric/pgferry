package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunRoot_NoArgsInteractiveLaunchesWizard(t *testing.T) {
	prevWizardRunner := rootWizardRunner
	prevMigrationRunner := rootMigrationRunner
	prevWizardModeChecker := rootWizardModeChecker
	rootWizardRunner = func(cmd *cobra.Command, args []string) error {
		return errors.New("wizard called")
	}
	rootMigrationRunner = func(cmd *cobra.Command, args []string) error {
		t.Fatal("migration runner should not be called")
		return nil
	}
	rootWizardModeChecker = func(cmd *cobra.Command) bool { return true }
	t.Cleanup(func() {
		rootWizardRunner = prevWizardRunner
		rootMigrationRunner = prevMigrationRunner
		rootWizardModeChecker = prevWizardModeChecker
		configPath = ""
	})

	err := runRoot(&cobra.Command{}, nil)
	if err == nil || err.Error() != "wizard called" {
		t.Fatalf("runRoot() error = %v, want wizard called", err)
	}
}

func TestRunRoot_WithConfigRunsMigration(t *testing.T) {
	prevWizardRunner := rootWizardRunner
	prevMigrationRunner := rootMigrationRunner
	prevWizardModeChecker := rootWizardModeChecker
	rootWizardRunner = func(cmd *cobra.Command, args []string) error {
		t.Fatal("wizard runner should not be called")
		return nil
	}
	rootMigrationRunner = func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 || args[0] != "migration.toml" {
			t.Fatalf("args = %v, want [migration.toml]", args)
		}
		return errors.New("migration called")
	}
	rootWizardModeChecker = func(cmd *cobra.Command) bool { return true }
	t.Cleanup(func() {
		rootWizardRunner = prevWizardRunner
		rootMigrationRunner = prevMigrationRunner
		rootWizardModeChecker = prevWizardModeChecker
		configPath = ""
	})

	err := runRoot(&cobra.Command{}, []string{"migration.toml"})
	if err == nil || err.Error() != "migration called" {
		t.Fatalf("runRoot() error = %v, want migration called", err)
	}
}

func TestRunRoot_NoArgsNonInteractiveReturnsConfigError(t *testing.T) {
	prevWizardModeChecker := rootWizardModeChecker
	rootWizardModeChecker = func(cmd *cobra.Command) bool { return false }
	t.Cleanup(func() {
		rootWizardModeChecker = prevWizardModeChecker
		configPath = ""
	})

	err := runRoot(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("runRoot() error = nil, want error")
	}
	want := "config file required: pgferry <migration.toml>, pgferry migrate <migration.toml>, or pgferry wizard"
	if err.Error() != want {
		t.Fatalf("runRoot() error = %q, want %q", err.Error(), want)
	}
}

func TestRunMigration_UsesConfigFlag(t *testing.T) {
	prev := configPath
	configPath = "migration.toml"
	t.Cleanup(func() {
		configPath = prev
	})

	got := resolveMigrationConfigPath(nil)
	if got != "migration.toml" {
		t.Fatalf("resolveMigrationConfigPath(nil) = %q, want migration.toml", got)
	}
}

func TestBuildTargetPoolConfig_AutoSizesMaxConnsWhenUnset(t *testing.T) {
	cfg := &MigrationConfig{
		Target:       TargetConfig{DSN: "postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable"},
		Workers:      12,
		IndexWorkers: 4,
	}

	poolCfg, warning, err := buildTargetPoolConfig(cfg)
	if err != nil {
		t.Fatalf("buildTargetPoolConfig() error = %v", err)
	}
	if got, want := poolCfg.MaxConns, int32(12); got != want {
		t.Fatalf("MaxConns = %d, want %d", got, want)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
}

func TestBuildTargetPoolConfig_PreservesExplicitMaxConnsWhenHigher(t *testing.T) {
	cfg := &MigrationConfig{
		Target:       TargetConfig{DSN: "user=postgres password=postgres host=127.0.0.1 port=5432 dbname=target sslmode=disable pool_max_conns=50"},
		Workers:      4,
		IndexWorkers: 6,
	}

	poolCfg, warning, err := buildTargetPoolConfig(cfg)
	if err != nil {
		t.Fatalf("buildTargetPoolConfig() error = %v", err)
	}
	if got, want := poolCfg.MaxConns, int32(50); got != want {
		t.Fatalf("MaxConns = %d, want %d", got, want)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want empty", warning)
	}
}

func TestBuildTargetPoolConfig_PreservesExplicitMaxConnsWhenLowerAndWarns(t *testing.T) {
	cfg := &MigrationConfig{
		Target:       TargetConfig{DSN: "postgres://postgres:postgres@127.0.0.1:5432/target?sslmode=disable&pool_max_conns=5"},
		Workers:      8,
		IndexWorkers: 12,
	}

	poolCfg, warning, err := buildTargetPoolConfig(cfg)
	if err != nil {
		t.Fatalf("buildTargetPoolConfig() error = %v", err)
	}
	if got, want := poolCfg.MaxConns, int32(5); got != want {
		t.Fatalf("MaxConns = %d, want %d", got, want)
	}
	if !strings.Contains(warning, "pool_max_conns=5") {
		t.Fatalf("warning = %q, want explicit pool_max_conns value", warning)
	}
	if !strings.Contains(warning, "12") {
		t.Fatalf("warning = %q, want effective concurrency", warning)
	}
}

func TestRunStartupCopyRiskAnalysis_LogsProgress(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "startup-copy-risk.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY, payload TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO events (id, payload) VALUES (1, 'a')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "events",
				PGName:     "events",
				Columns: []Column{
					{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "BIGINT"},
				},
				PrimaryKey: &Index{Columns: []string{"id"}},
			},
		},
	}

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	runStartupCopyRiskAnalysis(context.Background(), db, &sqliteSourceDB{}, schema, 10000)
	out := buf.String()
	if !strings.Contains(out, "copy risk analysis: probing 1 table(s)") {
		t.Fatalf("missing start log:\n%s", out)
	}
	if !strings.Contains(out, "copy risk analysis: completed 1/1 table(s)") {
		t.Fatalf("missing completion log:\n%s", out)
	}
}
