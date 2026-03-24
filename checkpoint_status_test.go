package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCheckpointStatus_PrintsMissingCheckpointMessage(t *testing.T) {
	cfgPath := writeCheckpointStatusTestConfig(t)

	var buf bytes.Buffer
	prevCfg := checkpointStatusConfigPath
	t.Cleanup(func() {
		checkpointStatusConfigPath = prevCfg
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	if err := runCheckpointStatus(cmd, []string{cfgPath}); err != nil {
		t.Fatalf("runCheckpointStatus() error: %v", err)
	}

	out := buf.String()
	wantPath := filepath.Join(filepath.Dir(cfgPath), "pgferry_checkpoint.json")
	if !strings.Contains(out, "status: missing") {
		t.Fatalf("output missing status: missing:\n%s", out)
	}
	if !strings.Contains(out, wantPath) {
		t.Fatalf("output missing checkpoint path %q:\n%s", wantPath, out)
	}
	if !strings.Contains(out, "resume may be disabled or this may be the first run") {
		t.Fatalf("output missing missing-checkpoint guidance:\n%s", out)
	}
	if strings.Contains(out, "root:secret") || strings.Contains(out, "postgres://") {
		t.Fatalf("output leaked DSN material:\n%s", out)
	}
}

func TestCheckpointStatus_PrintsV2CheckpointSummary(t *testing.T) {
	cfgPath := writeCheckpointStatusTestConfig(t)
	writeCheckpointStatusFixture(t, cfgPath, "testdata/checkpoint_status_v2.json")

	var buf bytes.Buffer
	prevCfg := checkpointStatusConfigPath
	t.Cleanup(func() {
		checkpointStatusConfigPath = prevCfg
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	if err := runCheckpointStatus(cmd, []string{cfgPath}); err != nil {
		t.Fatalf("runCheckpointStatus() error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"status: present",
		"version: 2",
		"started_at: 2026-03-24T06:15:00Z",
		"orders:",
		"chunks_completed: 2/3",
		"total_rows_copied: 800",
		"users:",
		"full_table_done: yes",
		"compatibility:",
		"fingerprint: fp-v2",
		"source_type: mysql",
		"source_db_name: appdb",
		"target_schema: app",
		"hooks: 1",
		"tables: 2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "orders:") > strings.Index(out, "users:") {
		t.Fatalf("tables should be sorted alphabetically:\n%s", out)
	}
	if strings.Contains(out, "root:secret") || strings.Contains(out, "postgres://") {
		t.Fatalf("output leaked DSN material:\n%s", out)
	}
}

func TestCheckpointStatus_PrintsLegacyV1CheckpointSummary(t *testing.T) {
	cfgPath := writeCheckpointStatusTestConfig(t)
	writeCheckpointStatusFixture(t, cfgPath, "testdata/checkpoint_status_v1.json")

	var buf bytes.Buffer
	prevCfg := checkpointStatusConfigPath
	t.Cleanup(func() {
		checkpointStatusConfigPath = prevCfg
	})

	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	if err := runCheckpointStatus(cmd, []string{cfgPath}); err != nil {
		t.Fatalf("runCheckpointStatus() error: %v", err)
	}

	out := buf.String()
	for _, want := range []string{
		"status: present",
		"version: 1",
		"legacy_table:",
		"chunks_completed: 1/2",
		"compatibility: absent",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCheckpointStatus_UnsupportedCheckpointVersion(t *testing.T) {
	cfgPath := writeCheckpointStatusTestConfig(t)
	cpPath := filepath.Join(filepath.Dir(cfgPath), "pgferry_checkpoint.json")
	if err := os.WriteFile(cpPath, []byte(`{"version":99,"started_at":"2026-03-24T06:15:00Z","tables":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	err := runCheckpointStatus(checkpointStatusCmd, []string{cfgPath})
	if err == nil {
		t.Fatal("expected unsupported version error")
	}
	if !strings.Contains(err.Error(), "unsupported checkpoint version 99") {
		t.Fatalf("error = %v, want unsupported version message", err)
	}
}

func TestCheckpointStatus_UnknownStartedAtRendersExplicitly(t *testing.T) {
	var buf bytes.Buffer
	state := &CheckpointState{
		Version: checkpointVersion,
		Tables: map[string]*TableCheckpoint{
			"users": {CompletedChunks: map[int]ChunkResult{}},
		},
	}

	if err := writeCheckpointStatusText(&buf, "/tmp/pgferry_checkpoint.json", state); err != nil {
		t.Fatalf("writeCheckpointStatusText() error: %v", err)
	}
	if !strings.Contains(buf.String(), "started_at: (unknown)") {
		t.Fatalf("output missing unknown started_at marker:\n%s", buf.String())
	}
}

func TestCheckpointStatus_MalformedConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "migration.toml")
	if err := os.WriteFile(cfgPath, []byte("schema = [\n"), 0644); err != nil {
		t.Fatal(err)
	}

	err := runCheckpointStatus(&cobra.Command{}, []string{cfgPath})
	if err == nil {
		t.Fatal("expected parse config error")
	}
	if !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("error = %v, want parse config error", err)
	}
}

func writeCheckpointStatusTestConfig(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "migration.toml")
	text := strings.Join([]string{
		"schema = \"app\"",
		"resume = true",
		"unlogged_tables = false",
		"",
		"[source]",
		"type = \"mysql\"",
		"dsn = \"root:secret@tcp(127.0.0.1:3306)/source\"",
		"",
		"[target]",
		"dsn = \"postgres://postgres:secret@127.0.0.1:5432/target?sslmode=disable\"",
	}, "\n")
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func writeCheckpointStatusFixture(t *testing.T, cfgPath, fixture string) {
	t.Helper()

	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixture, err)
	}
	cpPath := filepath.Join(filepath.Dir(cfgPath), "pgferry_checkpoint.json")
	if err := os.WriteFile(cpPath, data, 0644); err != nil {
		t.Fatalf("write checkpoint fixture: %v", err)
	}
}
