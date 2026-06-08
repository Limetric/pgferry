package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	migrateLogLevelVerbose = "verbose"
	migrateLogLevelTable   = "table"
	migrateLogLevelSchema  = "schema"
)

// MigrateOptions configures migrate execution (CLI passes flags; wizard uses zero value).
type MigrateOptions struct {
	// LogFormat is "text" (default) or "json". When "json", a single JSON summary
	// line is written to stdout at the end; human diagnostics remain on stderr.
	LogFormat string
	// LogLevel controls row-copy progress detail: verbose logs per-chunk progress,
	// table logs one row-copy start/done pair per table, and schema suppresses row-copy detail.
	LogLevel string
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

// migrateOptionsFromCmd reads migrate flags. cmd may be nil (tests).
// If flags are not registered on cmd (e.g. bare cobra.Command in tests), defaults are used.
func migrateOptionsFromCmd(cmd *cobra.Command) (MigrateOptions, error) {
	logFormat := "text"
	if v, ok := commandFlagString(cmd, "log-format"); ok {
		logFormat = v
	}
	lf, err := parseMigrateLogFormat(logFormat)
	if err != nil {
		return MigrateOptions{}, err
	}

	logLevel := migrateLogLevelVerbose
	if v, ok := commandFlagString(cmd, "log-level"); ok {
		logLevel = v
	}
	ll, err := parseMigrateLogLevel(logLevel)
	if err != nil {
		return MigrateOptions{}, err
	}
	if quiet, ok, err := commandFlagBool(cmd, "quiet"); ok {
		if err != nil {
			return MigrateOptions{}, err
		}
		if quiet {
			if commandFlagChanged(cmd, "log-level") {
				return MigrateOptions{}, fmt.Errorf("cannot combine --quiet with --log-level")
			}
			ll = migrateLogLevelTable
		}
	}

	return MigrateOptions{LogFormat: lf, LogLevel: ll}, nil
}

func commandFlagString(cmd *cobra.Command, name string) (string, bool) {
	if cmd == nil {
		return "", false
	}
	flag := cmd.Flag(name)
	if flag == nil {
		return "", false
	}
	return flag.Value.String(), true
}

func commandFlagBool(cmd *cobra.Command, name string) (bool, bool, error) {
	if cmd == nil {
		return false, false, nil
	}
	switch {
	case cmd.Flags().Lookup(name) != nil:
		v, err := cmd.Flags().GetBool(name)
		return v, true, err
	case cmd.PersistentFlags().Lookup(name) != nil:
		v, err := cmd.PersistentFlags().GetBool(name)
		return v, true, err
	case cmd.InheritedFlags().Lookup(name) != nil:
		v, err := cmd.InheritedFlags().GetBool(name)
		return v, true, err
	default:
		return false, false, nil
	}
}

func commandFlagChanged(cmd *cobra.Command, name string) bool {
	if cmd == nil {
		return false
	}
	flag := cmd.Flag(name)
	return flag != nil && flag.Changed
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

func parseMigrateLogLevel(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return migrateLogLevelVerbose, nil
	}
	switch v {
	case migrateLogLevelVerbose, migrateLogLevelTable, migrateLogLevelSchema:
		return v, nil
	default:
		return "", fmt.Errorf("--log-level must be verbose, table, or schema")
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
