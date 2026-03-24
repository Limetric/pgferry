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
	opts, err = migrateOptionsFromCmd(&cobra.Command{})
	if err != nil {
		t.Fatalf("bare cmd: %v", err)
	}
	if opts.LogFormat != "text" {
		t.Fatalf("bare cmd: LogFormat = %q, want text", opts.LogFormat)
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
