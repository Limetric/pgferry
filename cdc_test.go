package main

import (
	"strings"
	"testing"
	"time"
)

// validBaseConfig returns a MigrationConfig with all required fields set so
// that finalizeConfig does not fail on unrelated missing fields.
func validBaseConfig() MigrationConfig {
	cfg := defaultMigrationConfig()
	cfg.Schema = "public"
	cfg.Source = SourceConfig{Type: "mysql", DSN: "root@tcp(localhost)/test"}
	cfg.Target = TargetConfig{DSN: "postgres://localhost/test"}
	return cfg
}

func TestCDCConfig_DefaultMode(t *testing.T) {
	cfg := validBaseConfig()
	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("expected no error for default mode, got: %v", err)
	}
	if cfg.Mode != "default" {
		t.Errorf("Mode = %q, want %q", cfg.Mode, "default")
	}
}

func TestCDCConfig_InvalidMode(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "streaming"

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "mode must be one of") {
		t.Fatalf("expected mode error, got: %v", err)
	}
}

func TestCDCConfig_CDCRequiresMySQL(t *testing.T) {
	for _, sourceType := range []string{"sqlite", "mariadb", "mssql"} {
		t.Run(sourceType, func(t *testing.T) {
			cfg := validBaseConfig()
			cfg.Mode = "cdc"
			switch sourceType {
			case "sqlite":
				cfg.Source = SourceConfig{Type: "sqlite", DSN: "/tmp/test.db"}
			case "mariadb":
				cfg.Source = SourceConfig{Type: "mariadb", DSN: "root@tcp(localhost)/test"}
			case "mssql":
				cfg.Source = SourceConfig{Type: "mssql", DSN: "sqlserver://localhost/test"}
			}

			err := finalizeConfig(&cfg, t.TempDir())
			if err == nil {
				t.Fatalf("expected error for cdc mode with %s source", sourceType)
			}
			if !strings.Contains(err.Error(), "only supported for mysql") {
				t.Fatalf("expected mysql-only error, got: %v", err)
			}
		})
	}
}

func TestCDCConfig_CDCIncompatibleWithSchemaOnly(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.SchemaOnly = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for cdc + schema_only")
	}
	if !strings.Contains(err.Error(), "incompatible with schema_only") {
		t.Fatalf("expected schema_only incompatibility error, got: %v", err)
	}
}

func TestCDCConfig_CDCIncompatibleWithDataOnly(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.DataOnly = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for cdc + data_only")
	}
	if !strings.Contains(err.Error(), "incompatible with data_only") {
		t.Fatalf("expected data_only incompatibility error, got: %v", err)
	}
}

func TestCDCConfig_CDCRejectsExplicitSnapshotModeNone(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.SourceSnapshotMode = "none"
	cfg.cdcSnapshotModeExplicit = true

	err := finalizeConfig(&cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for cdc + explicit source_snapshot_mode=none")
	}
	if !strings.Contains(err.Error(), "requires source_snapshot_mode") {
		t.Fatalf("expected snapshot mode error, got: %v", err)
	}
}

func TestCDCConfig_CDCForcesSingleTxWhenSnapshotModeNotSet(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	// cdcSnapshotModeExplicit is false (default), so it should be forced to single_tx

	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.SourceSnapshotMode != "single_tx" {
		t.Errorf("SourceSnapshotMode = %q, want %q", cfg.SourceSnapshotMode, "single_tx")
	}
}

func TestCDCConfig_CDCExplicitSingleTxAllowed(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.SourceSnapshotMode = "single_tx"
	cfg.cdcSnapshotModeExplicit = true

	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("expected no error for cdc + explicit single_tx, got: %v", err)
	}
}

func TestCDCConfig_DefaultBatchSize(t *testing.T) {
	cfg := defaultMigrationConfig()
	if cfg.CDCBatchSize != 500 {
		t.Errorf("CDCBatchSize = %d, want 500", cfg.CDCBatchSize)
	}
}

func TestCDCConfig_DefaultFlushInterval(t *testing.T) {
	cfg := defaultMigrationConfig()
	if cfg.CDCFlushInterval != 200*time.Millisecond {
		t.Errorf("CDCFlushInterval = %v, want 200ms", cfg.CDCFlushInterval)
	}
}

func TestCDCConfig_CDCDefaultsApplied(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.CDCBatchSize = 0
	cfg.CDCFlushInterval = 0

	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.CDCBatchSize != 500 {
		t.Errorf("CDCBatchSize = %d, want 500 after default applied", cfg.CDCBatchSize)
	}
	if cfg.CDCFlushInterval != 200*time.Millisecond {
		t.Errorf("CDCFlushInterval = %v, want 200ms after default applied", cfg.CDCFlushInterval)
	}
}

func TestCDCConfig_CDCCustomBatchSizePreserved(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.CDCBatchSize = 1000
	cfg.CDCFlushInterval = 500 * time.Millisecond

	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.CDCBatchSize != 1000 {
		t.Errorf("CDCBatchSize = %d, want 1000", cfg.CDCBatchSize)
	}
	if cfg.CDCFlushInterval != 500*time.Millisecond {
		t.Errorf("CDCFlushInterval = %v, want 500ms", cfg.CDCFlushInterval)
	}
}
