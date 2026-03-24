package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// MigrateOptions configures migrate execution (CLI passes flags; wizard uses zero value).
type MigrateOptions struct {
	// LogFormat is "text" (default) or "json". When "json", a single JSON summary
	// line is written to stdout at the end; human diagnostics remain on stderr.
	LogFormat string
}

// migrateJSONSummary is the machine-readable migrate run outcome (stdout when --log-format json).
type migrateJSONSummary struct {
	Version        string `json:"version"`
	DurationMs     int64  `json:"duration_ms"`
	Mode           string `json:"mode"`
	Validation     string `json:"validation"`
	Success        bool   `json:"success"`
	Error          string `json:"error,omitempty"`
	Stage          string `json:"stage,omitempty"`
	TablesMigrated int    `json:"tables_migrated,omitempty"`
}

// migrateOptionsFromCmd reads --log-format (default text). cmd may be nil (tests).
// If the flag is not registered on cmd (e.g. bare cobra.Command in tests), text is used.
func migrateOptionsFromCmd(cmd *cobra.Command) (MigrateOptions, error) {
	s := "text"
	if cmd != nil && cmd.Flags().Lookup("log-format") != nil {
		v, err := cmd.Flags().GetString("log-format")
		if err != nil {
			return MigrateOptions{}, err
		}
		s = v
	}
	lf, err := parseMigrateLogFormat(s)
	if err != nil {
		return MigrateOptions{}, err
	}
	return MigrateOptions{LogFormat: lf}, nil
}

func parseMigrateLogFormat(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return "text", nil
	}
	switch v {
	case "text":
		return "text", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("--log-format must be text or json")
	}
}

func migrateModeString(cfg *MigrationConfig) string {
	if cfg.SchemaOnly {
		return "schema_only"
	}
	if cfg.DataOnly {
		return "data_only"
	}
	return "full"
}

// writeMigrateJSONSummaryLine writes one JSON object to w when opts.LogFormat is json.
// migrationErr is the error returned from the migration (nil on success).
func writeMigrateJSONSummaryLine(w io.Writer, opts MigrateOptions, cfg *MigrationConfig, start time.Time, tableCount int, stage string, migrationErr error) error {
	if opts.LogFormat != "json" {
		return nil
	}
	summary := migrateJSONSummary{
		Version:        versionString(),
		DurationMs:     time.Since(start).Milliseconds(),
		Mode:           migrateModeString(cfg),
		Validation:     cfg.Validation,
		Success:        migrationErr == nil,
		TablesMigrated: tableCount,
	}
	if migrationErr != nil {
		summary.Error = migrationErr.Error()
		summary.Stage = stage
	}
	b, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("migrate json summary: %w", err)
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("write migrate json summary: %w", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write migrate json summary: %w", err)
	}
	return nil
}
