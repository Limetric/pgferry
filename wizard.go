package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/microsoft/go-mssqldb/msdsn"
	"github.com/spf13/cobra"
)

var generatedConfigRunner = runMigrationWithConfig
var generatedConfigPlanner = runPlanWithConfig
var wizardSourceDSNValidator = validateWizardSourceDSN
var wizardTargetDSNValidator = validateWizardTargetDSN
var wizardSourceConnectionTester = testWizardSourceConnection
var wizardTargetConnectionTester = testWizardTargetConnection

type wizardPrompter struct {
	in     *bufio.Reader
	out    io.Writer
	styles wizardStyles
	blocks int
}

type wizardOption struct {
	key  string
	help string
}

type wizardStyles struct {
	enabled bool
}

var wizardAdvancedOptionsNote = []string{
	"Advanced options not covered by the wizard:",
	"- PostgreSQL extensions / PostGIS support",
	"- Validation, resume, chunk_size, and index_workers",
	"- Hooks, collation overrides, and other source-specific type mapping tweaks",
	"You can still add these manually to the generated TOML before running the migration.",
}

func runGenerateWizard(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	w := wizardPrompter{
		in:     bufio.NewReader(cmd.InOrStdin()),
		out:    cmd.OutOrStdout(),
		styles: newWizardStyles(cmd.OutOrStdout()),
	}

	fmt.Fprintln(w.out, w.styles.header("pgferry config wizard"))
	fmt.Fprintln(w.out, w.styles.muted("Press Enter to accept the default shown in brackets."))
	fmt.Fprintln(w.out)

	cfg, err := collectGeneratedConfig(&w, cwd)
	if err != nil {
		return err
	}

	rendered := renderConfigTOML(cfg)
	fmt.Fprintln(w.out, w.styles.accent("Generated config:"))
	fmt.Fprintln(w.out, rendered)
	fmt.Fprintln(w.out)
	for i, line := range wizardAdvancedOptionsNote {
		if i == 0 {
			fmt.Fprintln(w.out, w.styles.header(line))
			continue
		}
		fmt.Fprintln(w.out, w.styles.muted(line))
	}

	saveConfig, err := w.promptBoolGuided(
		"Save generated config to a file",
		true,
		"Recommended if you want to review, edit, reuse, or version the generated TOML before migrating.",
	)
	if err != nil {
		return err
	}

	if saveConfig {
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
		fmt.Fprintf(w.out, "%s\n", w.styles.success("Saved "+absPath))
	}

	nextStep, err := w.promptChoice("Next step", []wizardOption{
		{key: "stop", help: "Finish here. Use this if you want to inspect or edit the generated config manually."},
		{key: "plan", help: "Analyze the source and print a migration plan report without changing the target database."},
		{key: "run", help: "Start the migration now using the generated config. This will connect to both databases and begin making target-side changes."},
	}, "plan")
	if err != nil {
		return err
	}

	if nextStep == "plan" {
		fmt.Fprintln(w.out, w.styles.accent("Generating migration plan..."))
		if err := generatedConfigPlanner(cfg, w.out); err != nil {
			return fmt.Errorf("run plan: %w", err)
		}
	}

	if nextStep == "run" {
		fmt.Fprintln(w.out, w.styles.accent("Starting migration..."))
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
		{key: "mssql"},
	}, "mysql")
	if err != nil {
		return nil, err
	}
	cfg.Source.Type = sourceType

	cfg.Source.DSN, err = w.promptSourceDSN(sourceType)
	if err != nil {
		return nil, err
	}

	cfg.Target.DSN, err = w.promptTargetDSN()
	if err != nil {
		return nil, err
	}

	schemaDefault := suggestSchemaName(sourceType, cfg.Source.DSN, cfg.Target.DSN)
	cfg.Schema, err = w.promptStringGuided(
		"Target schema",
		schemaDefault,
		"Creates or loads into this PostgreSQL schema. A dedicated schema keeps migrated tables isolated from public and avoids confusion with the target database name.",
		validateRequired,
	)
	if err != nil {
		return nil, err
	}

	mode, err := w.promptChoice("Migration mode", []wizardOption{
		{key: "full", help: "Create tables, copy rows, then add indexes, foreign keys, sequences, and triggers."},
		{key: "schema_only", help: "Create the target schema without copying data. Good for dry runs and DDL review."},
		{key: "data_only", help: "Copy rows into existing compatible tables. Use when the schema is already prepared."},
	}, "full")
	if err != nil {
		return nil, err
	}
	cfg.SchemaOnly = mode == "schema_only"
	cfg.DataOnly = mode == "data_only"

	cfg.OnSchemaExists, err = w.promptChoice("If target schema already exists", []wizardOption{
		{key: "error", help: "Safest default. Stops instead of touching an existing schema."},
		{key: "recreate", help: "Drops and recreates the target schema. Fast for clean reruns, but destructive."},
	}, cfg.OnSchemaExists)
	if err != nil {
		return nil, err
	}

	switch sourceType {
	case "mysql", "mssql":
		cfg.SourceSnapshotMode, err = w.promptChoice("Source snapshot mode", []wizardOption{
			{key: "none", help: "Fastest. Each worker reads independently, so source changes during the run can leak in."},
			{key: "single_tx", help: "Uses one read-only transaction for a consistent snapshot. Safer, but longer-lived and less parallel-friendly."},
		}, cfg.SourceSnapshotMode)
		if err != nil {
			return nil, err
		}
	default:
		cfg.SourceSnapshotMode = "none"
		fmt.Fprintln(w.out, "source_snapshot_mode is fixed to none and workers are capped at 1 for this source type.")
	}

	if sourceType == "mssql" {
		cfg.Source.SourceSchema, err = w.promptStringGuided(
			"MSSQL source schema",
			"dbo",
			"Usually dbo. Change this only if the source tables live in a different SQL Server schema.",
			validateRequired,
		)
		if err != nil {
			return nil, err
		}
	}

	if !cfg.SchemaOnly && !cfg.DataOnly {
		cfg.UnloggedTables, err = w.promptBoolGuided(
			"Use UNLOGGED tables during bulk load",
			cfg.UnloggedTables,
			"Speeds up large loads by reducing WAL, but the tables are crash-unsafe until pgferry switches them back to logged.",
		)
		if err != nil {
			return nil, err
		}
	}

	cfg.PreserveDefaults, err = w.promptBoolGuided(
		"Preserve source DEFAULT values",
		cfg.PreserveDefaults,
		"Keeps source defaults on created tables. Good if the app will keep inserting rows after cutover; less useful if you want the leanest first-pass schema.",
	)
	if err != nil {
		return nil, err
	}

	cfg.SnakeCaseIdentifiers, err = w.promptBoolGuided(
		"Convert identifiers to snake_case",
		cfg.SnakeCaseIdentifiers,
		"Produces cleaner PostgreSQL names, for example OrderItems -> order_items and USER_ID -> user_id. If turned off, pgferry only lowercases names, so OrderItems becomes orderitems instead. Leave it on unless existing SQL or application code depends on the original source naming.",
	)
	if err != nil {
		return nil, err
	}

	if !cfg.SchemaOnly {
		cfg.CleanOrphans, err = w.promptBoolGuided(
			"Clean orphaned rows before adding foreign keys",
			cfg.CleanOrphans,
			"Deletes rows that would break foreign keys so the migration can finish. Turn it off if you would rather fail and inspect the bad data.",
		)
		if err != nil {
			return nil, err
		}
	}

	defaultWorkers := effectiveDefaultWorkers(sourceType)
	if sourceType == "mysql" || sourceType == "mssql" {
		cfg.Workers, err = w.promptIntGuided(
			"Parallel workers",
			defaultWorkers,
			1,
			"More workers usually mean faster copy throughput, but they also put more load on both source and target databases. The default is based on CPU count, capped at 8, and may be reduced further for source-specific limits.",
		)
		if err != nil {
			return nil, err
		}
	} else {
		cfg.Workers = defaultWorkers
	}

	cfg.TypeMapping.JSONAsJSONB, err = w.promptBoolGuided(
		"Map JSON columns to jsonb",
		cfg.TypeMapping.JSONAsJSONB,
		"jsonb is usually the better PostgreSQL type for indexing and querying. If turned off, JSON columns map to json instead.",
	)
	if err != nil {
		return nil, err
	}

	cfg.TypeMapping.UnknownAsText, err = w.promptBoolGuided(
		"Map unknown source types to text instead of failing",
		cfg.TypeMapping.UnknownAsText,
		"Useful for getting a first migration through. If turned off, unsupported source types fail the migration instead of mapping to text.",
	)
	if err != nil {
		return nil, err
	}

	if sourceType == "mssql" {
		cfg.TypeMapping.NvarcharAsText, err = w.promptBoolGuided(
			"Map nvarchar(n) to text",
			cfg.TypeMapping.NvarcharAsText,
			"Simplifies the PostgreSQL schema by avoiding many varying length limits. If turned off, nvarchar(n) maps to varchar(n) instead, while nvarchar(max) still maps to text.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.MoneyAsNumeric, err = w.promptBoolGuided(
			"Map money to numeric",
			cfg.TypeMapping.MoneyAsNumeric,
			"Recommended. money maps to numeric(19,4) and smallmoney to numeric(10,4). If turned off, those columns fall back to text instead.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.XmlAsText, err = w.promptBoolGuided(
			"Map xml to text",
			cfg.TypeMapping.XmlAsText,
			"Easiest portable mapping. Keeps the content, but you lose XML typing and server-side validation. If turned off, xml stays xml in PostgreSQL.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.DatetimeAsTimestamptz, err = w.promptBoolGuided(
			"Map datetime/datetime2 to timestamptz",
			cfg.TypeMapping.DatetimeAsTimestamptz,
			"Use this when the source values represent real instants in time. If turned off, datetime, smalldatetime, and datetime2 map to timestamp instead.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.SpatialMode, err = w.promptChoice("Spatial type mapping", []wizardOption{
			{key: "off", help: "No automatic spatial conversion. Best if you will handle spatial columns manually."},
			{key: "wkb_bytea", help: "Stores spatial values as WKB bytes. Safer fallback when you want to preserve binary geometry data."},
			{key: "wkt_text", help: "Stores spatial values as readable text. Easier to inspect, but larger and less exact than binary."},
		}, cfg.TypeMapping.SpatialMode)
		if err != nil {
			return nil, err
		}
	}

	if sourceType == "mysql" {
		cfg.TypeMapping.TinyInt1AsBoolean, err = w.promptBoolGuided(
			"Map tinyint(1) to boolean",
			cfg.TypeMapping.TinyInt1AsBoolean,
			"Good when tinyint(1) really means true/false. If turned off, tinyint columns map to smallint instead.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.DatetimeAsTimestamptz, err = w.promptBoolGuided(
			"Map datetime to timestamptz",
			cfg.TypeMapping.DatetimeAsTimestamptz,
			"Use this if MySQL datetime values should become timezone-aware instants. If turned off, datetime maps to timestamp instead.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.Binary16AsUUID, err = w.promptBoolGuided(
			"Map binary(16) to uuid",
			cfg.TypeMapping.Binary16AsUUID,
			"Turn this on only when those binary(16) columns really store UUIDs. If turned off, binary(16) stays bytea instead.",
		)
		if err != nil {
			return nil, err
		}
		if cfg.TypeMapping.Binary16AsUUID {
			cfg.TypeMapping.Binary16UUIDMode, err = w.promptChoice("Binary UUID byte order", []wizardOption{
				{key: "rfc4122", help: "Standard UUID byte order. Use for application-stored UUID bytes."},
				{key: "mysql_uuid_to_bin_swap", help: "Use only if the source stored UUIDs with MySQL UUID_TO_BIN(..., 1) byte swapping."},
			}, cfg.TypeMapping.Binary16UUIDMode)
			if err != nil {
				return nil, err
			}
		}
		cfg.TypeMapping.StringUUIDAsUUID, err = w.promptBoolGuided(
			"Map char(36)/varchar(36) to uuid",
			cfg.TypeMapping.StringUUIDAsUUID,
			"Useful when those columns always contain UUID strings. If turned off, they stay varchar(36)-style text columns instead.",
		)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.EnumMode, err = w.promptChoice("Enum mode", []wizardOption{
			{key: "text", help: "Simplest and most portable. Values are stored as text with no PostgreSQL-side enforcement."},
			{key: "check", help: "Adds CHECK constraints for allowed values. Good middle ground between portability and enforcement."},
			{key: "native", help: "Creates PostgreSQL ENUM types. Strongest typing, but future enum changes are more involved."},
		}, cfg.TypeMapping.EnumMode)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.SetMode, err = w.promptChoice("Set mode", []wizardOption{
			{key: "text", help: "Keeps the original comma-separated source value as plain text. Least opinionated."},
			{key: "text_array", help: "More PostgreSQL-native and easier to query, but does not enforce allowed members."},
			{key: "text_array_check", help: "Array storage plus a CHECK against allowed values. Safest, with more DDL."},
		}, cfg.TypeMapping.SetMode)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.BitMode, err = w.promptChoice("BIT(n) mapping", []wizardOption{
			{key: "bytea", help: "Safest fallback when you mainly care about preserving bits as raw bytes."},
			{key: "bit", help: "Fixed-length bit strings. Good when the source width is meaningful and stable."},
			{key: "varbit", help: "Variable-length bit strings. More flexible if widths differ across data."},
		}, cfg.TypeMapping.BitMode)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.TimeMode, err = w.promptChoice("TIME mapping", []wizardOption{
			{key: "time", help: "Best when the source column means time-of-day."},
			{key: "text", help: "Safest for quirky or out-of-range values that might not fit PostgreSQL time cleanly."},
			{key: "interval", help: "Use only when the source column semantically stores durations, not clock times."},
		}, cfg.TypeMapping.TimeMode)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.ZeroDateMode, err = w.promptChoice("Zero-date handling", []wizardOption{
			{key: "null", help: "Practical default. Converts zero dates to NULL so the migration can continue."},
			{key: "error", help: "Strict mode. Stops on zero dates so you can clean the source data explicitly."},
		}, cfg.TypeMapping.ZeroDateMode)
		if err != nil {
			return nil, err
		}
		cfg.TypeMapping.SpatialMode, err = w.promptChoice("Spatial type mapping", []wizardOption{
			{key: "off", help: "No automatic spatial conversion. Choose this if you will handle spatial columns manually or with PostGIS later."},
			{key: "wkb_bytea", help: "Stores geometry as WKB bytes. Better for fidelity than text."},
			{key: "wkt_text", help: "Stores geometry as text. Easier to inspect, but larger and less binary-faithful."},
		}, cfg.TypeMapping.SpatialMode)
		if err != nil {
			return nil, err
		}
		cfg.AddUnsignedChecks, err = w.promptBoolGuided(
			"Add unsigned integer CHECK constraints",
			cfg.AddUnsignedChecks,
			"Preserves MySQL unsigned ranges in PostgreSQL. Better fidelity, but adds extra DDL and can fail if the loaded data already violates those ranges.",
		)
		if err != nil {
			return nil, err
		}
		cfg.ReplicateOnUpdateCurrentTimestamp, err = w.promptBoolGuided(
			"Emulate ON UPDATE CURRENT_TIMESTAMP",
			cfg.ReplicateOnUpdateCurrentTimestamp,
			"Adds PostgreSQL triggers to mimic MySQL auto-updated timestamp columns. Better compatibility, but more objects and some write overhead.",
		)
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
		fmt.Fprintln(w.out, w.styles.warning("Output file already exists."))
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
	if cfg.UnloggedTables != defaults.UnloggedTables {
		writeLine("unlogged_tables = %t", cfg.UnloggedTables)
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
	if cfg.Source.Type == "mssql" && cfg.Source.SourceSchema != "" && cfg.Source.SourceSchema != "dbo" {
		writeLine("source_schema = %s", strconv.Quote(cfg.Source.SourceSchema))
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

	if sourceType == "mssql" {
		if cfg.NvarcharAsText != defaults.NvarcharAsText {
			lines = append(lines, fmt.Sprintf("nvarchar_as_text = %t", cfg.NvarcharAsText))
		}
		if cfg.MoneyAsNumeric != defaults.MoneyAsNumeric {
			lines = append(lines, fmt.Sprintf("money_as_numeric = %t", cfg.MoneyAsNumeric))
		}
		if cfg.XmlAsText != defaults.XmlAsText {
			lines = append(lines, fmt.Sprintf("xml_as_text = %t", cfg.XmlAsText))
		}
		if cfg.DatetimeAsTimestamptz != defaults.DatetimeAsTimestamptz {
			lines = append(lines, fmt.Sprintf("datetime_as_timestamptz = %t", cfg.DatetimeAsTimestamptz))
		}
		if cfg.SpatialMode != defaults.SpatialMode {
			lines = append(lines, fmt.Sprintf("spatial_mode = %s", strconv.Quote(cfg.SpatialMode)))
		}
		return lines
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
	if cfg.BitMode != defaults.BitMode {
		lines = append(lines, fmt.Sprintf("bit_mode = %s", strconv.Quote(cfg.BitMode)))
	}
	if cfg.StringUUIDAsUUID != defaults.StringUUIDAsUUID {
		lines = append(lines, fmt.Sprintf("string_uuid_as_uuid = %t", cfg.StringUUIDAsUUID))
	}
	if cfg.Binary16UUIDMode != defaults.Binary16UUIDMode {
		lines = append(lines, fmt.Sprintf("binary16_uuid_mode = %s", strconv.Quote(cfg.Binary16UUIDMode)))
	}
	if cfg.TimeMode != defaults.TimeMode {
		lines = append(lines, fmt.Sprintf("time_mode = %s", strconv.Quote(cfg.TimeMode)))
	}
	if cfg.ZeroDateMode != defaults.ZeroDateMode {
		lines = append(lines, fmt.Sprintf("zero_date_mode = %s", strconv.Quote(cfg.ZeroDateMode)))
	}
	if cfg.SpatialMode != defaults.SpatialMode {
		lines = append(lines, fmt.Sprintf("spatial_mode = %s", strconv.Quote(cfg.SpatialMode)))
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

func suggestSchemaName(sourceType, sourceDSN, targetDSN string) string {
	src, err := newSourceDB(sourceType)
	if err != nil {
		return "app"
	}
	name, err := src.ExtractDBName(sourceDSN)
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
		schema = "app_" + schema
	}

	targetDBName, err := extractPostgresDBName(targetDSN)
	if err != nil {
		return schema
	}
	if strings.EqualFold(schema, targetDBName) {
		return "app"
	}
	return schema
}

func extractPostgresDBName(dsn string) (string, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(cfg.ConnConfig.Database)
	if name == "" {
		return "", fmt.Errorf("cannot extract database name from PostgreSQL DSN")
	}
	return name, nil
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

func newWizardStyles(out io.Writer) wizardStyles {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return wizardStyles{}
	}
	f, ok := out.(*os.File)
	if !ok {
		return wizardStyles{}
	}
	info, err := f.Stat()
	if err != nil || (info.Mode()&os.ModeCharDevice) == 0 {
		return wizardStyles{}
	}
	return wizardStyles{enabled: true}
}

func (s wizardStyles) wrap(code, text string) string {
	if !s.enabled || text == "" {
		return text
	}
	return code + text + "\033[0m"
}

func (s wizardStyles) header(text string) string  { return s.wrap("\033[1;36m", text) }
func (s wizardStyles) accent(text string) string  { return s.wrap("\033[1;34m", text) }
func (s wizardStyles) muted(text string) string   { return s.wrap("\033[2m", text) }
func (s wizardStyles) prompt(text string) string  { return s.wrap("\033[1m", text) }
func (s wizardStyles) success(text string) string { return s.wrap("\033[32m", text) }
func (s wizardStyles) warning(text string) string { return s.wrap("\033[33m", text) }
func (s wizardStyles) error(text string) string   { return s.wrap("\033[31m", text) }

func wizardSourceDSNPrompt(sourceType string) (string, string) {
	switch sourceType {
	case "mysql":
		return "Source DSN", "root:root@tcp(127.0.0.1:3306)/source_db"
	case "sqlite":
		return "SQLite path or file: URI", "/path/to/database.db"
	case "mssql":
		return "MSSQL DSN", "sqlserver://sa:YourStrong!Pass@127.0.0.1:1433?database=source_db"
	default:
		return "Source DSN", ""
	}
}

func wizardTargetDSNPrompt() (string, string) {
	return "PostgreSQL DSN", "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
}

func validateWizardSourceDSN(sourceType, dsn string) error {
	dsn = strings.TrimSpace(dsn)
	if err := validateRequired(dsn); err != nil {
		return err
	}

	switch sourceType {
	case "mysql":
		if _, err := mysql.ParseDSN(dsn); err != nil {
			return fmt.Errorf("invalid MySQL DSN: %w", err)
		}
	case "sqlite":
		if _, err := sqliteReadOnlyURI(dsn); err != nil {
			return fmt.Errorf("invalid SQLite DSN: %w", err)
		}
	case "mssql":
		if _, err := msdsn.Parse(dsn); err != nil {
			return fmt.Errorf("invalid MSSQL DSN: %w", err)
		}
	default:
		return fmt.Errorf("unsupported source type %q", sourceType)
	}

	src, err := newSourceDB(sourceType)
	if err != nil {
		return err
	}
	if _, err := src.ExtractDBName(dsn); err != nil {
		return err
	}
	return nil
}

func validateWizardTargetDSN(dsn string) error {
	dsn = strings.TrimSpace(dsn)
	if err := validateRequired(dsn); err != nil {
		return err
	}
	if _, err := pgxpool.ParseConfig(dsn); err != nil {
		return fmt.Errorf("invalid PostgreSQL DSN: %w", err)
	}
	return nil
}

func testWizardSourceConnection(sourceType, dsn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var (
		db  *sql.DB
		err error
	)

	switch sourceType {
	case "mysql":
		normalizedDSN, err := normalizedMySQLDSN(dsn, "utf8mb4")
		if err != nil {
			return err
		}
		db, err = sql.Open("mysql", normalizedDSN)
		if err != nil {
			return fmt.Errorf("open mysql: %w", err)
		}
	case "sqlite":
		uri, err := sqliteReadOnlyURI(dsn)
		if err != nil {
			return err
		}
		db, err = sql.Open("sqlite", uri)
		if err != nil {
			return fmt.Errorf("open sqlite: %w", err)
		}
		db.SetMaxOpenConns(1)
	case "mssql":
		db, err = sql.Open("sqlserver", dsn)
		if err != nil {
			return fmt.Errorf("open mssql: %w", err)
		}
	default:
		return fmt.Errorf("unsupported source type %q", sourceType)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping %s: %w", strings.ToLower(sourceType), err)
	}
	return nil
}

func testWizardTargetConnection(dsn string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	return nil
}

func (w *wizardPrompter) promptString(label, defaultValue string, validate func(string) error) (string, error) {
	w.startBlock()
	return w.promptStringInline(label, defaultValue, validate)
}

func (w *wizardPrompter) promptStringInline(label, defaultValue string, validate func(string) error) (string, error) {
	for {
		value, err := w.promptInput(label, defaultValue)
		if err != nil {
			return "", err
		}
		if validate != nil {
			if err := validate(value); err != nil {
				fmt.Fprintf(w.out, "%s\n", w.styles.error(err.Error()))
				continue
			}
		}
		return value, nil
	}
}

func (w *wizardPrompter) startBlock() {
	if w.blocks > 0 {
		fmt.Fprintln(w.out)
	}
	w.blocks++
}

func (w *wizardPrompter) promptInput(label, defaultValue string) (string, error) {
	if defaultValue == "" {
		fmt.Fprintf(w.out, "%s: ", w.styles.prompt(label))
	} else {
		fmt.Fprintf(w.out, "%s [%s]: ", w.styles.prompt(label), defaultValue)
	}

	value, err := w.readLine()
	if err != nil {
		return "", err
	}
	if value == "" {
		value = defaultValue
	}
	return value, nil
}

func (w *wizardPrompter) printGuide(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	fmt.Fprintf(w.out, "  %s\n", w.styles.muted(text))
}

func (w *wizardPrompter) promptStringGuided(label, defaultValue, guide string, validate func(string) error) (string, error) {
	w.startBlock()
	fmt.Fprintln(w.out, w.styles.header(label))
	w.printGuide(guide)
	return w.promptStringInline("Value", defaultValue, validate)
}

func (w *wizardPrompter) promptStringWithExample(label, example, defaultValue string, validate func(string) error) (string, error) {
	w.startBlock()
	fmt.Fprintln(w.out, w.styles.header(label))
	if example != "" {
		fmt.Fprintf(w.out, "  %s\n", w.styles.muted("Example: "+example))
	}
	return w.promptStringInline("Value", defaultValue, validate)
}

func (w *wizardPrompter) promptSourceDSN(sourceType string) (string, error) {
	label, example := wizardSourceDSNPrompt(sourceType)
	defaultValue := ""
	for {
		dsn, err := w.promptStringWithExample(label, example, defaultValue, func(value string) error {
			return wizardSourceDSNValidator(sourceType, value)
		})
		if err != nil {
			return "", err
		}
		defaultValue = dsn

		testNow, err := w.promptBool("Test source connection now (recommended)", true)
		if err != nil {
			return "", err
		}
		if !testNow {
			return dsn, nil
		}

		if err := wizardSourceConnectionTester(sourceType, dsn); err != nil {
			fmt.Fprintf(w.out, "%s\n", w.styles.error("Source connection test failed: "+err.Error()))
			fmt.Fprintln(w.out, w.styles.muted("Press Enter at the DSN prompt to keep the same value, then choose whether to test again."))
			continue
		}

		fmt.Fprintln(w.out, w.styles.success("Source connection OK."))
		return dsn, nil
	}
}

func (w *wizardPrompter) promptTargetDSN() (string, error) {
	label, example := wizardTargetDSNPrompt()
	defaultValue := ""
	for {
		dsn, err := w.promptStringWithExample(label, example, defaultValue, wizardTargetDSNValidator)
		if err != nil {
			return "", err
		}
		defaultValue = dsn

		testNow, err := w.promptBool("Test PostgreSQL connection now (recommended)", true)
		if err != nil {
			return "", err
		}
		if !testNow {
			return dsn, nil
		}

		if err := wizardTargetConnectionTester(dsn); err != nil {
			fmt.Fprintf(w.out, "%s\n", w.styles.error("PostgreSQL connection test failed: "+err.Error()))
			fmt.Fprintln(w.out, w.styles.muted("Press Enter at the DSN prompt to keep the same value, then choose whether to test again."))
			continue
		}

		fmt.Fprintln(w.out, w.styles.success("PostgreSQL connection OK."))
		return dsn, nil
	}
}

func (w *wizardPrompter) promptBool(label string, defaultValue bool) (bool, error) {
	w.startBlock()
	return w.promptBoolInline(label, defaultValue)
}

func (w *wizardPrompter) promptBoolInline(label string, defaultValue bool) (bool, error) {
	hint := "y/N"
	if defaultValue {
		hint = "Y/n"
	}
	for {
		fmt.Fprintf(w.out, "%s [%s]: ", w.styles.prompt(label), hint)
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
			fmt.Fprintln(w.out, w.styles.error("Enter y or n."))
		}
	}
}

func (w *wizardPrompter) promptBoolGuided(label string, defaultValue bool, guide string) (bool, error) {
	w.startBlock()
	fmt.Fprintln(w.out, w.styles.header(label))
	w.printGuide(guide)
	return w.promptBoolInline("Answer", defaultValue)
}

func (w *wizardPrompter) promptInt(label string, defaultValue, min int) (int, error) {
	w.startBlock()
	return w.promptIntInline(label, defaultValue, min)
}

func (w *wizardPrompter) promptIntInline(label string, defaultValue, min int) (int, error) {
	for {
		fmt.Fprintf(w.out, "%s [%d]: ", w.styles.prompt(label), defaultValue)
		value, err := w.readLine()
		if err != nil {
			return 0, err
		}
		if value == "" {
			return defaultValue, nil
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < min {
			fmt.Fprintf(w.out, "%s\n", w.styles.error(fmt.Sprintf("Enter a whole number >= %d.", min)))
			continue
		}
		return n, nil
	}
}

func (w *wizardPrompter) promptIntGuided(label string, defaultValue, min int, guide string) (int, error) {
	w.startBlock()
	fmt.Fprintln(w.out, w.styles.header(label))
	w.printGuide(guide)
	return w.promptIntInline("Value", defaultValue, min)
}

func (w *wizardPrompter) promptChoice(label string, options []wizardOption, defaultValue string) (string, error) {
	w.startBlock()
	keys := make([]string, 0, len(options))
	for _, option := range options {
		keys = append(keys, option.key)
	}

	for {
		fmt.Fprint(w.out, w.styles.header(label))
		if defaultValue != "" {
			fmt.Fprintf(w.out, " %s", w.styles.muted("(default: "+defaultValue+")"))
		}
		fmt.Fprintln(w.out)
		for _, option := range options {
			if strings.TrimSpace(option.help) != "" {
				fmt.Fprintf(w.out, "  %s: %s\n", w.styles.accent(option.key), w.styles.muted(option.help))
			}
		}
		value, err := w.promptInput("Choice ["+strings.Join(keys, "/")+"]", defaultValue)
		if err != nil {
			return "", err
		}
		value = strings.ToLower(value)
		for i, option := range options {
			if value == option.key || value == strconv.Itoa(i+1) {
				return option.key, nil
			}
		}
		fmt.Fprintf(w.out, "%s\n", w.styles.error("Enter one of: "+strings.Join(keys, ", ")))
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
