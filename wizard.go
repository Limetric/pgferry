package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

var generatedConfigRunner = runMigrationWithConfig

type wizardPrompter struct {
	in  *bufio.Reader
	out io.Writer
}

type wizardOption struct {
	key string
}

func runGenerateWizard(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	w := wizardPrompter{
		in:  bufio.NewReader(cmd.InOrStdin()),
		out: cmd.OutOrStdout(),
	}

	fmt.Fprintln(w.out, "pgferry config wizard")
	fmt.Fprintln(w.out, "Press Enter to accept the default shown in brackets.")
	fmt.Fprintln(w.out)

	cfg, err := collectGeneratedConfig(&w, cwd)
	if err != nil {
		return err
	}

	rendered := renderConfigTOML(cfg)
	fmt.Fprintln(w.out, "Generated config:")
	fmt.Fprintln(w.out, rendered)

	action, err := w.promptChoice("Next step", []wizardOption{
		{key: "write"},
		{key: "run"},
		{key: "write_run"},
	}, "write")
	if err != nil {
		return err
	}

	if action == "write" || action == "write_run" {
		outputPath, err := w.promptString("Output file", "migration.toml", validateRequired)
		if err != nil {
			return err
		}
		absPath, err := filepath.Abs(outputPath)
		if err != nil {
			return fmt.Errorf("resolve output path: %w", err)
		}
		if err := maybeConfirmOverwrite(&w, absPath); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, []byte(rendered), 0644); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		if err := finalizeConfig(cfg, filepath.Dir(absPath)); err != nil {
			return err
		}
		fmt.Fprintf(w.out, "Saved %s\n", absPath)
	}

	if action == "run" || action == "write_run" {
		fmt.Fprintln(w.out, "Starting migration...")
		if err := generatedConfigRunner(cfg); err != nil {
			return err
		}
	}

	return nil
}

func collectGeneratedConfig(w *wizardPrompter, configDir string) (*MigrationConfig, error) {
	cfg := defaultMigrationConfig()

	sourceType, err := w.promptChoice("Source type", []wizardOption{
		{key: "mysql"},
		{key: "sqlite"},
	}, "mysql")
	if err != nil {
		return nil, err
	}
	cfg.Source.Type = sourceType

	sourcePrompt := "Source DSN"
	if sourceType == "sqlite" {
		sourcePrompt = "SQLite path or file: URI"
	}
	cfg.Source.DSN, err = w.promptString(sourcePrompt, "", validateRequired)
	if err != nil {
		return nil, err
	}

	cfg.Target.DSN, err = w.promptString("PostgreSQL DSN", "", validateRequired)
	if err != nil {
		return nil, err
	}

	schemaDefault := suggestSchemaName(sourceType, cfg.Source.DSN)
	cfg.Schema, err = w.promptString("Target schema", schemaDefault, validateRequired)
	if err != nil {
		return nil, err
	}

	mode, err := w.promptChoice("Migration mode", []wizardOption{
		{key: "full"},
		{key: "schema_only"},
		{key: "data_only"},
	}, "full")
	if err != nil {
		return nil, err
	}
	cfg.SchemaOnly = mode == "schema_only"
	cfg.DataOnly = mode == "data_only"

	cfg.OnSchemaExists, err = w.promptChoice("If target schema already exists", []wizardOption{
		{key: "error"},
		{key: "recreate"},
	}, cfg.OnSchemaExists)
	if err != nil {
		return nil, err
	}

	if sourceType == "mysql" {
		cfg.SourceSnapshotMode, err = w.promptChoice("Source snapshot mode", []wizardOption{
			{key: "none"},
			{key: "single_tx"},
		}, cfg.SourceSnapshotMode)
		if err != nil {
			return nil, err
		}
	} else {
		cfg.SourceSnapshotMode = "none"
		fmt.Fprintln(w.out, "SQLite source detected: source_snapshot_mode is fixed to none and workers are capped at 1.")
	}

	if !cfg.SchemaOnly && !cfg.DataOnly {
		cfg.UnloggedTables, err = w.promptBool("Use UNLOGGED tables during bulk load", cfg.UnloggedTables)
		if err != nil {
			return nil, err
		}
	}

	cfg.PreserveDefaults, err = w.promptBool("Preserve source DEFAULT values", cfg.PreserveDefaults)
	if err != nil {
		return nil, err
	}

	cfg.SnakeCaseIdentifiers, err = w.promptBool("Convert identifiers to snake_case", cfg.SnakeCaseIdentifiers)
	if err != nil {
		return nil, err
	}

	if !cfg.SchemaOnly {
		cfg.CleanOrphans, err = w.promptBool("Clean orphaned rows before adding foreign keys", cfg.CleanOrphans)
		if err != nil {
			return nil, err
		}
	}

	defaultWorkers := effectiveDefaultWorkers(sourceType)
	if sourceType == "mysql" {
		cfg.Workers, err = w.promptInt("Parallel workers", defaultWorkers, 1)
		if err != nil {
			return nil, err
		}
	} else {
		cfg.Workers = defaultWorkers
	}

	cfg.TypeMapping.JSONAsJSONB, err = w.promptBool("Map JSON columns to jsonb", cfg.TypeMapping.JSONAsJSONB)
	if err != nil {
		return nil, err
	}

	cfg.TypeMapping.UnknownAsText, err = w.promptBool("Map unknown source types to text instead of failing", cfg.TypeMapping.UnknownAsText)
	if err != nil {
		return nil, err
	}

	if sourceType == "mysql" {
		cfg.TypeMapping.TinyInt1AsBoolean, err = w.promptBool("Map tinyint(1) to boolean", cfg.TypeMapping.TinyInt1AsBoolean)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.DatetimeAsTimestamptz, err = w.promptBool("Map datetime to timestamptz", cfg.TypeMapping.DatetimeAsTimestamptz)
		if err != nil {
			return nil, err
		}
		cfg.AddUnsignedChecks, err = w.promptBool("Add unsigned integer CHECK constraints", cfg.AddUnsignedChecks)
		if err != nil {
			return nil, err
		}
		cfg.ReplicateOnUpdateCurrentTimestamp, err = w.promptBool("Emulate ON UPDATE CURRENT_TIMESTAMP", cfg.ReplicateOnUpdateCurrentTimestamp)
		if err != nil {
			return nil, err
		}
	}

	if err := finalizeConfig(&cfg, configDir); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func maybeConfirmOverwrite(w *wizardPrompter, path string) error {
	if _, err := os.Stat(path); err == nil {
		overwrite, promptErr := w.promptBool(fmt.Sprintf("Overwrite %s", path), false)
		if promptErr != nil {
			return promptErr
		}
		if !overwrite {
			return fmt.Errorf("refusing to overwrite existing file %s", path)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat output path: %w", err)
	}
	return nil
}

func renderConfigTOML(cfg *MigrationConfig) string {
	var b strings.Builder
	writeLine := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
		b.WriteByte('\n')
	}
	writeSection := func(name string) {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		writeLine("[%s]", name)
	}

	defaults := defaultMigrationConfig()

	writeLine("schema = %s", strconv.Quote(cfg.Schema))
	if cfg.OnSchemaExists != defaults.OnSchemaExists {
		writeLine("on_schema_exists = %s", strconv.Quote(cfg.OnSchemaExists))
	}
	if cfg.SchemaOnly {
		writeLine("schema_only = true")
	}
	if cfg.DataOnly {
		writeLine("data_only = true")
	}
	if cfg.SourceSnapshotMode != defaults.SourceSnapshotMode {
		writeLine("source_snapshot_mode = %s", strconv.Quote(cfg.SourceSnapshotMode))
	}
	if cfg.UnloggedTables {
		writeLine("unlogged_tables = true")
	}
	if !cfg.PreserveDefaults {
		writeLine("preserve_defaults = false")
	}
	if cfg.AddUnsignedChecks {
		writeLine("add_unsigned_checks = true")
	}
	if !cfg.CleanOrphans {
		writeLine("clean_orphans = false")
	}
	if !cfg.SnakeCaseIdentifiers {
		writeLine("snake_case_identifiers = false")
	}
	if cfg.ReplicateOnUpdateCurrentTimestamp {
		writeLine("replicate_on_update_current_timestamp = true")
	}
	if cfg.Workers != effectiveDefaultWorkers(cfg.Source.Type) {
		writeLine("workers = %d", cfg.Workers)
	}

	writeSection("source")
	writeLine("type = %s", strconv.Quote(cfg.Source.Type))
	writeLine("dsn = %s", strconv.Quote(cfg.Source.DSN))
	if cfg.Source.Type == "mysql" && cfg.Source.Charset != "" && cfg.Source.Charset != "utf8mb4" {
		writeLine("charset = %s", strconv.Quote(cfg.Source.Charset))
	}

	writeSection("target")
	writeLine("dsn = %s", strconv.Quote(cfg.Target.DSN))

	typeMappingLines := renderTypeMappingLines(cfg.TypeMapping, cfg.Source.Type)
	if len(typeMappingLines) > 0 {
		writeSection("type_mapping")
		for _, line := range typeMappingLines {
			writeLine(line)
		}
	}
	if len(cfg.TypeMapping.CollationMap) > 0 {
		keys := make([]string, 0, len(cfg.TypeMapping.CollationMap))
		for key := range cfg.TypeMapping.CollationMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		writeSection("type_mapping.collation_map")
		for _, key := range keys {
			writeLine("%s = %s", key, strconv.Quote(cfg.TypeMapping.CollationMap[key]))
		}
	}

	if len(cfg.Hooks.BeforeData) > 0 || len(cfg.Hooks.AfterData) > 0 || len(cfg.Hooks.BeforeFk) > 0 || len(cfg.Hooks.AfterAll) > 0 {
		writeSection("hooks")
		if len(cfg.Hooks.BeforeData) > 0 {
			writeLine("before_data = %s", tomlStringArray(cfg.Hooks.BeforeData))
		}
		if len(cfg.Hooks.AfterData) > 0 {
			writeLine("after_data = %s", tomlStringArray(cfg.Hooks.AfterData))
		}
		if len(cfg.Hooks.BeforeFk) > 0 {
			writeLine("before_fk = %s", tomlStringArray(cfg.Hooks.BeforeFk))
		}
		if len(cfg.Hooks.AfterAll) > 0 {
			writeLine("after_all = %s", tomlStringArray(cfg.Hooks.AfterAll))
		}
	}

	return b.String()
}

func renderTypeMappingLines(cfg TypeMappingConfig, sourceType string) []string {
	defaults := defaultTypeMappingConfig()
	var lines []string

	if cfg.JSONAsJSONB != defaults.JSONAsJSONB {
		lines = append(lines, fmt.Sprintf("json_as_jsonb = %t", cfg.JSONAsJSONB))
	}
	if cfg.SanitizeJSONNullBytes != defaults.SanitizeJSONNullBytes {
		lines = append(lines, fmt.Sprintf("sanitize_json_null_bytes = %t", cfg.SanitizeJSONNullBytes))
	}
	if cfg.UnknownAsText != defaults.UnknownAsText {
		lines = append(lines, fmt.Sprintf("unknown_as_text = %t", cfg.UnknownAsText))
	}

	if sourceType != "mysql" {
		return lines
	}

	if cfg.TinyInt1AsBoolean != defaults.TinyInt1AsBoolean {
		lines = append(lines, fmt.Sprintf("tinyint1_as_boolean = %t", cfg.TinyInt1AsBoolean))
	}
	if cfg.Binary16AsUUID != defaults.Binary16AsUUID {
		lines = append(lines, fmt.Sprintf("binary16_as_uuid = %t", cfg.Binary16AsUUID))
	}
	if cfg.DatetimeAsTimestamptz != defaults.DatetimeAsTimestamptz {
		lines = append(lines, fmt.Sprintf("datetime_as_timestamptz = %t", cfg.DatetimeAsTimestamptz))
	}
	if cfg.VarcharAsText != defaults.VarcharAsText {
		lines = append(lines, fmt.Sprintf("varchar_as_text = %t", cfg.VarcharAsText))
	}
	if cfg.WidenUnsignedIntegers != defaults.WidenUnsignedIntegers {
		lines = append(lines, fmt.Sprintf("widen_unsigned_integers = %t", cfg.WidenUnsignedIntegers))
	}
	if cfg.EnumMode != defaults.EnumMode {
		lines = append(lines, fmt.Sprintf("enum_mode = %s", strconv.Quote(cfg.EnumMode)))
	}
	if cfg.SetMode != defaults.SetMode {
		lines = append(lines, fmt.Sprintf("set_mode = %s", strconv.Quote(cfg.SetMode)))
	}
	if cfg.CollationMode != defaults.CollationMode {
		lines = append(lines, fmt.Sprintf("collation_mode = %s", strconv.Quote(cfg.CollationMode)))
	}
	if cfg.CIAsCitext != defaults.CIAsCitext {
		lines = append(lines, fmt.Sprintf("ci_as_citext = %t", cfg.CIAsCitext))
	}

	return lines
}

func tomlStringArray(values []string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Quote(v))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func suggestSchemaName(sourceType, dsn string) string {
	src, err := newSourceDB(sourceType)
	if err != nil {
		return "app"
	}
	name, err := src.ExtractDBName(dsn)
	if err != nil {
		return "app"
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "app"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}

	schema := strings.Trim(b.String(), "_")
	if schema == "" {
		return "app"
	}
	if schema[0] >= '0' && schema[0] <= '9' {
		return "app_" + schema
	}
	return schema
}

func effectiveDefaultWorkers(sourceType string) int {
	workers := defaultWorkers()
	src, err := newSourceDB(sourceType)
	if err != nil {
		return workers
	}
	if max := src.MaxWorkers(); max > 0 && workers > max {
		return max
	}
	return workers
}

func validateRequired(v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("value is required")
	}
	return nil
}

func (w *wizardPrompter) promptString(label, defaultValue string, validate func(string) error) (string, error) {
	for {
		if defaultValue == "" {
			fmt.Fprintf(w.out, "%s: ", label)
		} else {
			fmt.Fprintf(w.out, "%s [%s]: ", label, defaultValue)
		}

		value, err := w.readLine()
		if err != nil {
			return "", err
		}
		if value == "" {
			value = defaultValue
		}
		if validate != nil {
			if err := validate(value); err != nil {
				fmt.Fprintf(w.out, "%s\n", err)
				continue
			}
		}
		return value, nil
	}
}

func (w *wizardPrompter) promptBool(label string, defaultValue bool) (bool, error) {
	hint := "y/N"
	if defaultValue {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(w.out, "%s [%s]: ", label, hint)
		value, err := w.readLine()
		if err != nil {
			return false, err
		}
		if value == "" {
			return defaultValue, nil
		}
		switch strings.ToLower(value) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(w.out, "Enter y or n.")
		}
	}
}

func (w *wizardPrompter) promptInt(label string, defaultValue, min int) (int, error) {
	for {
		fmt.Fprintf(w.out, "%s [%d]: ", label, defaultValue)
		value, err := w.readLine()
		if err != nil {
			return 0, err
		}
		if value == "" {
			return defaultValue, nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < min {
			fmt.Fprintf(w.out, "Enter a whole number >= %d.\n", min)
			continue
		}
		return n, nil
	}
}

func (w *wizardPrompter) promptChoice(label string, options []wizardOption, defaultValue string) (string, error) {
	keys := make([]string, 0, len(options))
	for _, option := range options {
		keys = append(keys, option.key)
	}

	for {
		fmt.Fprintf(w.out, "%s [%s]", label, strings.Join(keys, "/"))
		if defaultValue != "" {
			fmt.Fprintf(w.out, " (default: %s)", defaultValue)
		}
		fmt.Fprint(w.out, ": ")

		value, err := w.readLine()
		if err != nil {
			return "", err
		}
		if value == "" {
			return defaultValue, nil
		}
		value = strings.ToLower(value)
		for i, option := range options {
			if value == option.key || value == strconv.Itoa(i+1) {
				return option.key, nil
			}
		}
		fmt.Fprintf(w.out, "Enter one of: %s\n", strings.Join(keys, ", "))
	}
}

func (w *wizardPrompter) readLine() (string, error) {
	line, err := w.in.ReadString('\n')
	if err != nil {
		if err == io.EOF && len(line) > 0 {
			err = nil
		} else if err == io.EOF {
			return "", fmt.Errorf("wizard cancelled")
		} else {
			return "", err
		}
	}
	return strings.TrimSpace(line), err
}
