package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestMigrateOptionsFromCmd(t *testing.T) {
	opts, err := migrateOptionsFromCmd(nil)
	if err != nil {
		t.Fatalf("nil cmd: %v", err)
	}
	if opts.LogFormat != "text" {
		t.Fatalf("nil cmd: LogFormat = %q, want text", opts.LogFormat)
	}
	if opts.LogLevel != migrateLogLevelVerbose {
		t.Fatalf("nil cmd: LogLevel = %q, want verbose", opts.LogLevel)
	}
	opts, err = migrateOptionsFromCmd(&cobra.Command{})
	if err != nil {
		t.Fatalf("bare cmd: %v", err)
	}
	if opts.LogFormat != "text" {
		t.Fatalf("bare cmd: LogFormat = %q, want text", opts.LogFormat)
	}
	if opts.LogLevel != migrateLogLevelVerbose {
		t.Fatalf("bare cmd: LogLevel = %q, want verbose", opts.LogLevel)
	}
}

func TestMigrateOptionsFromCmdLogLevelAndQuiet(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("log-format", "text", "")
	cmd.Flags().String("log-level", "verbose", "")
	cmd.Flags().BoolP("quiet", "q", false, "")

	if err := cmd.Flags().Set("log-level", "table"); err != nil {
		t.Fatalf("set log-level: %v", err)
	}
	opts, err := migrateOptionsFromCmd(cmd)
	if err != nil {
		t.Fatalf("log-level table: %v", err)
	}
	if opts.LogLevel != migrateLogLevelTable {
		t.Fatalf("LogLevel = %q, want table", opts.LogLevel)
	}

	if err := cmd.Flags().Set("log-level", "verbose"); err != nil {
		t.Fatalf("set log-level: %v", err)
	}
	if err := cmd.Flags().Set("quiet", "true"); err != nil {
		t.Fatalf("set quiet: %v", err)
	}
	opts, err = migrateOptionsFromCmd(cmd)
	if err != nil {
		t.Fatalf("quiet: %v", err)
	}
	if opts.LogLevel != migrateLogLevelTable {
		t.Fatalf("quiet LogLevel = %q, want table", opts.LogLevel)
	}
}

func TestMigrateOptionsFromCmdReadsInheritedFlags(t *testing.T) {
	parent := &cobra.Command{Use: "pgferry"}
	child := &cobra.Command{Use: "migrate"}
	parent.PersistentFlags().String("log-format", "text", "")
	parent.PersistentFlags().String("log-level", migrateLogLevelVerbose, "")
	parent.PersistentFlags().BoolP("quiet", "q", false, "")
	parent.AddCommand(child)

	if err := parent.PersistentFlags().Set("log-format", "json"); err != nil {
		t.Fatalf("set log-format: %v", err)
	}
	if err := parent.PersistentFlags().Set("quiet", "true"); err != nil {
		t.Fatalf("set quiet: %v", err)
	}

	opts, err := migrateOptionsFromCmd(child)
	if err != nil {
		t.Fatalf("inherited flags: %v", err)
	}
	if opts.LogFormat != "json" {
		t.Fatalf("LogFormat = %q, want json", opts.LogFormat)
	}
	if opts.LogLevel != migrateLogLevelTable {
		t.Fatalf("LogLevel = %q, want table", opts.LogLevel)
	}
}

func TestParseMigrateLogFormat(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"text", "text", false},
		{"TEXT", "text", false},
		{"json", "json", false},
		{"", "text", false},
		{"  json  ", "json", false},
		{"yaml", "", true},
	}
	for _, tt := range tests {
		got, err := parseMigrateLogFormat(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseMigrateLogFormat(%q) err = nil, want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseMigrateLogFormat(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseMigrateLogFormat(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseMigrateLogLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"verbose", migrateLogLevelVerbose, false},
		{"VERBOSE", migrateLogLevelVerbose, false},
		{"table", migrateLogLevelTable, false},
		{"schema", migrateLogLevelSchema, false},
		{"", migrateLogLevelVerbose, false},
		{"  table  ", migrateLogLevelTable, false},
		{"debug", "", true},
	}
	for _, tt := range tests {
		got, err := parseMigrateLogLevel(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("parseMigrateLogLevel(%q) err = nil, want error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("parseMigrateLogLevel(%q): %v", tt.in, err)
		}
		if got != tt.want {
			t.Fatalf("parseMigrateLogLevel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWriteMigrateJSONSummaryLine(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultMigrationConfig()
	cfg.Validation = "row_count"
	start := time.Now().Add(-2 * time.Second)

	err := writeMigrateJSONSummaryLine(&buf, MigrateOptions{LogFormat: "text"}, &cfg, start, 0, "source", nil)
	if err != nil {
		t.Fatalf("text mode: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("text mode wrote %d bytes, want 0", buf.Len())
	}

	buf.Reset()
	migrationErr := errors.New("ping postgres: connection refused")
	err = writeMigrateJSONSummaryLine(&buf, MigrateOptions{LogFormat: "json"}, &cfg, start, 5, "target", migrationErr)
	if err != nil {
		t.Fatalf("json failure: %v", err)
	}
	var got migrateJSONSummary
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Success {
		t.Fatal("Success = true, want false")
	}
	if got.Error != migrationErr.Error() {
		t.Fatalf("Error = %q, want %q", got.Error, migrationErr.Error())
	}
	if got.Stage != "target" {
		t.Fatalf("Stage = %q, want target", got.Stage)
	}
	if got.TablesMigrated != 5 {
		t.Fatalf("TablesMigrated = %d, want 5", got.TablesMigrated)
	}
	if got.Validation != "row_count" {
		t.Fatalf("Validation = %q", got.Validation)
	}
	if got.Mode != "full" {
		t.Fatalf("Mode = %q, want full", got.Mode)
	}
	if got.DurationMs < 1000 {
		t.Fatalf("DurationMs = %d, want >= 1000", got.DurationMs)
	}
	if !strings.Contains(buf.String(), "\n") {
		t.Fatal("expected newline-terminated line")
	}
}

func TestMigrateModeString(t *testing.T) {
	cfg := defaultMigrationConfig()
	if migrateModeString(&cfg) != "full" {
		t.Fatalf("full: got %q", migrateModeString(&cfg))
	}
	cfg.SchemaOnly = true
	if migrateModeString(&cfg) != "schema_only" {
		t.Fatalf("schema_only: got %q", migrateModeString(&cfg))
	}
	cfg.SchemaOnly = false
	cfg.DataOnly = true
	if migrateModeString(&cfg) != "data_only" {
		t.Fatalf("data_only: got %q", migrateModeString(&cfg))
	}
}
