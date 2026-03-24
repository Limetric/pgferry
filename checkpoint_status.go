package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
)

var checkpointStatusConfigPath string

var checkpointCmd = &cobra.Command{
	Use:   "checkpoint",
	Short: "Inspect resume checkpoint files",
}

var checkpointStatusCmd = &cobra.Command{
	Use:   "status [migration.toml]",
	Short: "Print human-readable progress from the resume checkpoint file",
	Long: `Load a migration TOML, resolve the adjacent pgferry checkpoint file, and
print per-table resume progress plus any stored compatibility metadata.

Does not connect to any database.

Pass the config as a positional argument or via --config, not both.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCheckpointStatus,
}

func init() {
	checkpointStatusCmd.Flags().StringVar(&checkpointStatusConfigPath, "config", "", "path to migration TOML config file")
	rootCmd.AddCommand(checkpointCmd)
	checkpointCmd.AddCommand(checkpointStatusCmd)
}

func runCheckpointStatus(cmd *cobra.Command, args []string) error {
	cfgPath, err := resolveOptionalConfigPath(checkpointStatusConfigPath, args)
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return missingCheckpointStatusConfigError()
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	cpPath := checkpointPath(cfg.configDir)
	state, err := loadCheckpoint(cpPath)
	if err != nil {
		return err
	}

	return writeCheckpointStatusText(cmd.OutOrStdout(), cpPath, state)
}

func missingCheckpointStatusConfigError() error {
	return fmt.Errorf("config file required: pgferry checkpoint status <migration.toml> or pgferry checkpoint status --config <migration.toml>")
}

func writeCheckpointStatusText(out io.Writer, path string, state *CheckpointState) error {
	if state == nil {
		fmt.Fprintf(out, "checkpoint: %s\n", path)
		fmt.Fprintln(out, "status: missing")
		fmt.Fprintln(out, "message: no checkpoint file found; resume may be disabled or this may be the first run")
		return nil
	}

	fmt.Fprintf(out, "checkpoint: %s\n", path)
	fmt.Fprintln(out, "status: present")
	fmt.Fprintf(out, "version: %d\n", state.Version)
	if state.StartedAt.IsZero() {
		fmt.Fprintln(out, "started_at: (unknown)")
	} else {
		fmt.Fprintf(out, "started_at: %s\n", state.StartedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(out, "tables: %d\n", len(state.Tables))
	fmt.Fprintln(out)

	tableNames := make([]string, 0, len(state.Tables))
	for name := range state.Tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	for _, name := range tableNames {
		tc := state.Tables[name]
		fmt.Fprintf(out, "%s:\n", name)
		fmt.Fprintf(out, "  full_table_done: %s\n", yesNo(tc.FullTableDone))
		fmt.Fprintf(out, "  chunks_completed: %d/%d\n", len(tc.CompletedChunks), tc.ChunkCount)
		fmt.Fprintf(out, "  total_rows_copied: %d\n", tc.TotalRowsCopied)
	}

	if state.Compatibility == nil || state.Compatibility.Summary == nil {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "compatibility: absent")
		return nil
	}

	summary := state.Compatibility.Summary
	fmt.Fprintln(out)
	fmt.Fprintln(out, "compatibility:")
	fmt.Fprintf(out, "  fingerprint: %s\n", state.Compatibility.Fingerprint)
	fmt.Fprintf(out, "  source_type: %s\n", summary.SourceType)
	if summary.SourceDBName != "" {
		fmt.Fprintf(out, "  source_db_name: %s\n", summary.SourceDBName)
	}
	if summary.SourceSchema != "" {
		fmt.Fprintf(out, "  source_schema: %s\n", summary.SourceSchema)
	}
	fmt.Fprintf(out, "  target_schema: %s\n", summary.TargetSchema)
	fmt.Fprintf(out, "  source_snapshot_mode: %s\n", summary.SourceSnapshotMode)
	fmt.Fprintf(out, "  snake_case_identifiers: %s\n", yesNo(summary.SnakeCaseIdentifiers))
	fmt.Fprintf(out, "  schema_only: %s\n", yesNo(summary.SchemaOnly))
	fmt.Fprintf(out, "  data_only: %s\n", yesNo(summary.DataOnly))
	fmt.Fprintf(out, "  unlogged_tables: %s\n", yesNo(summary.UnloggedTables))
	fmt.Fprintf(out, "  chunk_size: %d\n", summary.ChunkSize)
	fmt.Fprintf(out, "  hooks: %d\n", len(summary.Hooks))
	fmt.Fprintf(out, "  tables: %d\n", len(summary.Tables))
	return nil
}
