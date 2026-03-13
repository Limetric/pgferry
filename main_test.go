package main

import (
	"errors"
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
	want := "config file required: pgferry <config.toml>, pgferry migrate <config.toml>, or pgferry wizard"
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
