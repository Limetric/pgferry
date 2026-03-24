package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var configPath string

// migrateLogFormatFlagShort is the short --help line; full JSON field list is in migrateCmd.Long.
const migrateLogFormatFlagShort = "migrate progress: text or json"

// migrateLogFormatFlagDesc documents JSON mode for migrateCmd.Long and site docs.
const migrateLogFormatFlagDesc = `With json, one JSON object is written to stdout when the run finishes; human messages remain on stderr. JSON fields: version, duration_ms, mode, validation, success, error, stage (on failure), tables_migrated (omitted when zero).`

var rootCmd = &cobra.Command{
	Use:   "pgferry [migration.toml]",
	Short: "Source database to PostgreSQL migration tool",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRoot,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print pgferry version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), versionString())
	},
}

var migrateCmd = &cobra.Command{
	Use:     "migrate [migration.toml]",
	Aliases: []string{"run"},
	Short:   "Run a migration from a TOML config file",
	Long:    "Run a migration from a TOML config file.\n\n" + migrateLogFormatFlagDesc,
	Args:    cobra.MaximumNArgs(1),
	RunE:    runMigration,
}

var generateCmd = &cobra.Command{
	Use:     "wizard",
	Aliases: []string{"generate", "init"},
	Short:   "Launch an interactive config wizard",
	Args:    cobra.NoArgs,
	RunE:    runGenerateWizard,
}

var rootMigrationRunner = runMigration
var rootWizardRunner = runGenerateWizard
var rootWizardModeChecker = canLaunchWizardInteractively

func init() {
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
	rootCmd.PersistentFlags().String("log-format", "text", migrateLogFormatFlagShort)
	migrateCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(planCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		var planFindings *PlanFindingsError
		if errors.As(err, &planFindings) {
			// Mirror stdout summary so CI logs/stderr captures the gate reason (JSON mode only writes this here).
			fmt.Fprintln(os.Stderr, planFindings.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runRoot(cmd *cobra.Command, args []string) error {
	if resolveMigrationConfigPath(args) != "" {
		return rootMigrationRunner(cmd, args)
	}
	if rootWizardModeChecker(cmd) {
		return rootWizardRunner(cmd, args)
	}
	return missingMigrationConfigError()
}

func runMigration(cmd *cobra.Command, args []string) error {
	cfgPath := resolveMigrationConfigPath(args)
	if cfgPath == "" {
		return missingMigrationConfigError()
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	opts, err := migrateOptionsFromCmd(cmd)
	if err != nil {
		return err
	}
	return runMigrationWithConfig(cfg, opts)
}

func resolveMigrationConfigPath(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return configPath
}

func missingMigrationConfigError() error {
	return fmt.Errorf("config file required: pgferry <migration.toml>, pgferry migrate <migration.toml>, or pgferry wizard")
}

func canLaunchWizardInteractively(cmd *cobra.Command) bool {
	return isInteractiveDevice(cmd.InOrStdin()) && isInteractiveDevice(cmd.OutOrStdout())
}

func isInteractiveDevice(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func runMigrationWithConfig(cfg *MigrationConfig, opts MigrateOptions) (err error) {
	ctx := context.Background()
	start := time.Now()
	stage := "source"
	tableCount := 0

	defer func() {
		writeErr := writeMigrateJSONSummaryLine(os.Stdout, opts, cfg, start, tableCount, stage, err)
		if writeErr != nil {
			if err == nil {
				err = writeErr
			} else {
				fmt.Fprintf(os.Stderr, "%v\n", writeErr)
			}
		}
	}()

	// Force unlogged_tables=false in split modes (no bulk load benefit)
	if cfg.SchemaOnly || cfg.DataOnly {
		cfg.UnloggedTables = false
	}

	// Initialize source database backend
	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}

	log.Printf("pgferry — %s → PostgreSQL migration", src.Name())
	mode := "full"
	if cfg.SchemaOnly {
		mode = "schema_only"
	} else if cfg.DataOnly {
		mode = "data_only"
	}
	log.Printf(
		"config: mode=%s workers=%d index_workers=%d schema=%s on_schema_exists=%s source_snapshot_mode=%s unlogged_tables=%t preserve_defaults=%t add_unsigned_checks=%t clean_orphans=%t clean_orphans_mode=%s clean_orphans_max_rows=%d snake_case_identifiers=%t replicate_on_update_current_timestamp=%t chunk_size=%d copy_risk_analysis=%t resume=%t validation=%s",
		mode,
		cfg.Workers,
		cfg.IndexWorkers,
		cfg.Schema,
		cfg.OnSchemaExists,
		cfg.SourceSnapshotMode,
		cfg.UnloggedTables,
		cfg.PreserveDefaults,
		cfg.AddUnsignedChecks,
		cfg.CleanOrphans,
		cfg.CleanOrphansMode,
		cfg.CleanOrphansMaxRows,
		cfg.SnakeCaseIdentifiers,
		cfg.ReplicateOnUpdateCurrentTimestamp,
		cfg.ChunkSize,
		cfg.CopyRiskAnalysis,
		cfg.Resume,
		cfg.Validation,
	)

	// 1. Connect to source (for schema introspection only)
	log.Printf("connecting to %s...", src.Name())
	sourceDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)

	if err := sourceDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping %s: %w", strings.ToLower(src.Name()), err)
	}

	dbName, err := src.ExtractDBName(cfg.Source.DSN)
	if err != nil {
		return err
	}

	// 2. Introspect source schema
	stage = "introspect"
	log.Printf("introspecting %s schema '%s'...", src.Name(), dbName)
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema: %w", err)
	}
	stage = "filter"
	filteredSchema, filterReport, err := filterSchemaTables(schema, cfg)
	if err != nil {
		return fmt.Errorf("filter schema tables: %w", err)
	}
	schema = filteredSchema
	tableCount = len(schema.Tables)
	if hasTableFilters(cfg) {
		logTableFilterReport(filterReport)
		if cfg.Resume {
			log.Printf("table filter: resume enabled; changing include_tables/exclude_tables between runs will invalidate the checkpoint")
		}
	}
	log.Printf("found %d tables", len(schema.Tables))
	for _, t := range schema.Tables {
		log.Printf("  %s → %s (%d cols, %d indexes, %d fks)",
			t.SourceName, t.PGName, len(t.Columns), len(t.Indexes), len(t.ForeignKeys))
	}
	if sourceObjects, err := src.IntrospectSourceObjects(sourceDB, dbName); err != nil {
		log.Printf("WARN: failed to introspect non-table source objects: %v", err)
	} else if warnings := sourceObjectWarnings(sourceObjects); len(warnings) > 0 {
		log.Printf("source object report:")
		for _, w := range warnings {
			log.Printf("  WARN: %s", w)
		}
	}
	if cfg.CleanOrphans && !cfg.SchemaOnly {
		totalFKs, deleteFKs, setNullFKs := orphanCleanupCandidateCounts(schema)
		threshold := "disabled"
		if cfg.CleanOrphansMaxRows > 0 {
			threshold = fmt.Sprintf("%d", cfg.CleanOrphansMaxRows)
		}
		log.Printf("orphan cleanup plan: mode=%s max_rows=%s eligible_fks=%d (DELETE=%d SET NULL=%d)",
			cfg.CleanOrphansMode, threshold, totalFKs, deleteFKs, setNullFKs)
	} else if !cfg.SchemaOnly && (cfg.CleanOrphansMode != "apply" || cfg.CleanOrphansMaxRows > 0) {
		threshold := "disabled"
		if cfg.CleanOrphansMaxRows > 0 {
			threshold = fmt.Sprintf("%d", cfg.CleanOrphansMaxRows)
		}
		log.Printf("orphan cleanup disabled: clean_orphans_mode=%s and clean_orphans_max_rows=%s are ignored because clean_orphans=false",
			cfg.CleanOrphansMode, threshold)
	}
	typeMap := effectiveTypeMapping(cfg)
	var resumeCompatibility checkpointCompatibility
	if cfg.Resume {
		resumeCompatibility, err = buildCheckpointCompatibility(cfg, schema, src, dbName, typeMap)
		if err != nil {
			return fmt.Errorf("build resume compatibility: %w", err)
		}
	}
	if warnings := collectIndexCompatibilityWarnings(schema, typeMap); len(warnings) > 0 {
		log.Printf("index compatibility report: %d index(es) may require manual handling", len(warnings))
		for _, w := range warnings {
			log.Printf("  WARN: %s", w)
		}
	}
	if warnings := collectGeneratedColumnWarnings(schema); len(warnings) > 0 {
		log.Printf("generated column report: %d generated column(s) need manual expression migration", len(warnings))
		for _, w := range warnings {
			log.Printf("  WARN: %s", w)
		}
	}
	if warnings := collectCollationWarnings(schema, typeMap); len(warnings) > 0 {
		log.Printf("charset/collation report:")
		for _, w := range warnings {
			log.Printf("  WARN: %s", w)
		}
	}
	if !cfg.SchemaOnly && cfg.CopyRiskAnalysis {
		logCopyRiskProbeStart(len(schema.Tables))
		// Startup copy-risk probing is advisory only during live migrations.
		// Operators still get signal when probes succeed, but transient source
		// errors here should not block a migration whose core semantics are
		// otherwise unchanged.
		if copyRisks, err := collectCopyRiskFindings(ctx, sourceDB, src, schema, cfg.ChunkSize); err != nil {
			log.Printf("WARN: copy risk analysis skipped: %v", err)
		} else {
			logCopyRiskFindings(copyRisks, cfg.ChunkSize)
		}
	}
	if typeErrs := collectUnsupportedTypeErrors(schema, typeMap, src.MapType); len(typeErrs) > 0 {
		var b strings.Builder
		b.WriteString("unsupported source column types detected:\n")
		for _, e := range typeErrs {
			b.WriteString("  - ")
			b.WriteString(e)
			b.WriteByte('\n')
		}
		b.WriteString("Hint: set [type_mapping].unknown_as_text = true to coerce unknown types to text.")
		return errors.New(b.String())
	}
	if err := validateGeneratedIdentifiers(schema, cfg, typeMap); err != nil {
		return err
	}
	logTemporalWarnings(collectTemporalWarnings(schema, cfg.Source.Type, typeMap))

	// Close introspection connection — data migration opens its own connections
	sourceDB.Close()

	// 3. Connect to PostgreSQL
	stage = "target"
	log.Printf("connecting to PostgreSQL...")
	poolCfg, poolWarning, err := buildTargetPoolConfig(cfg)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	if poolWarning != "" {
		log.Printf("WARN: %s", poolWarning)
	}
	pgPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pgPool.Close()

	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	// Validate extension-backed features before any schema or data work. This
	// intentionally also runs in schema_only and data_only modes because
	// geometry/citext DDL and COPY both depend on the target extension being
	// present.
	if reqs := collectRequiredExtensions(schema, src, cfg, typeMap); len(reqs) > 0 {
		log.Printf("validating required PostgreSQL extensions...")
		if err := ensureRequiredExtensions(ctx, pgPool, reqs); err != nil {
			return err
		}
	}

	// 4. Create schema based on configured conflict behavior
	if !cfg.DataOnly {
		stage = "schema"
		log.Printf("preparing schema '%s'...", cfg.Schema)
		if err := prepareTargetSchema(ctx, pgPool, cfg.Schema, cfg.OnSchemaExists); err != nil {
			return err
		}

		// 5a. Create native enum types if configured (must precede table creation)
		if typeMap.EnumMode == "native" {
			log.Printf("creating enum types...")
			if err := createEnumTypes(ctx, pgPool, schema, cfg.Schema, typeMap, cfg.Source.Type); err != nil {
				return fmt.Errorf("create enum types: %w", err)
			}
		}

		// 5b. Create bare tables (no PKs, FKs, indexes)
		log.Printf("creating tables...")
		if err := createTables(ctx, pgPool, schema, cfg.Schema, cfg.UnloggedTables, cfg.PreserveDefaults, typeMap, src); err != nil {
			return fmt.Errorf("create tables: %w", err)
		}
	}

	if !cfg.SchemaOnly {
		stage = "data"
		err := runDataMigrationPhase(
			cfg.DataOnly,
			log.Printf,
			func() error {
				return preflightDataOnlyTriggerControl(
					ctx,
					func(ctx context.Context) (rollbackExecutor, error) {
						return pgPool.Begin(ctx)
					},
					schema,
					cfg.Schema,
				)
			},
			func(enable bool) error {
				return setTriggers(ctx, pgPool, schema, cfg.Schema, enable)
			},
			func() error {
				return loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.BeforeData, "before_data")
			},
			func() error {
				if cfg.SourceSnapshotMode == "single_tx" {
					log.Printf("migrating data with source_snapshot_mode=single_tx (sequential)")
				} else {
					log.Printf("migrating data with %d workers...", cfg.Workers)
				}
				return migrateData(ctx, migrateDataConfig{
					Src:                 src,
					SrcDSN:              cfg.Source.DSN,
					Pool:                pgPool,
					Schema:              schema,
					PGSchema:            cfg.Schema,
					Workers:             cfg.Workers,
					TypeMap:             typeMap,
					SourceSnapshotMode:  cfg.SourceSnapshotMode,
					ChunkSize:           cfg.ChunkSize,
					Resume:              cfg.Resume,
					ConfigDir:           cfg.configDir,
					ResumeCompatibility: resumeCompatibility,
				})
			},
			func() error {
				return loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.AfterData, "after_data")
			},
		)
		if err != nil {
			return err
		}
	}

	// Validation
	if cfg.Validation != "none" && !cfg.SchemaOnly {
		stage = "validation"
		logValidationPlan(cfg.Validation, cfg.SourceSnapshotMode)
		log.Printf("running post-load validation (mode=%s)...", cfg.Validation)
		if _, err := validateMigration(ctx, src, cfg.Source.DSN, pgPool, schema, cfg.Schema, cfg.Validation, cfg.Workers, typeMap); err != nil {
			return fmt.Errorf("validation: %w", err)
		}
		log.Printf("validation passed")
	}

	// 9. Post-migration: SET LOGGED, PKs, indexes, hooks, FKs, sequences, triggers
	stage = "post_migrate"
	log.Printf("running post-migration steps...")
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		return fmt.Errorf("post-migrate: %w", err)
	}

	log.Printf("migration completed in %s", time.Since(start).Round(time.Millisecond))
	return nil
}

func buildTargetPoolConfig(cfg *MigrationConfig) (*pgxpool.Config, string, error) {
	connCfg, err := pgx.ParseConfig(cfg.Target.DSN)
	if err != nil {
		return nil, "", fmt.Errorf("parse postgres pool config: %w", err)
	}

	explicitMaxConns := int32(0)
	explicitPoolMaxConns := false
	if s, ok := connCfg.Config.RuntimeParams["pool_max_conns"]; ok {
		explicitPoolMaxConns = true
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return nil, "", fmt.Errorf("parse postgres pool config: pool_max_conns=%q: %w", s, err)
		}
		explicitMaxConns = int32(n)
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.Target.DSN)
	if err != nil {
		return nil, "", fmt.Errorf("parse postgres pool config: %w", err)
	}

	peak := effectiveTargetPoolConcurrency(cfg)
	if explicitPoolMaxConns {
		if explicitMaxConns < peak {
			return poolCfg, fmt.Sprintf(
				"target DSN sets pool_max_conns=%d below effective migration concurrency %d; parallel COPY and index creation may wait on the PostgreSQL pool",
				explicitMaxConns,
				peak,
			), nil
		}
		return poolCfg, "", nil
	}

	if poolCfg.MaxConns < peak {
		poolCfg.MaxConns = peak
	}
	return poolCfg, "", nil
}

func effectiveTargetPoolConcurrency(cfg *MigrationConfig) int32 {
	peak := cfg.Workers
	if cfg.IndexWorkers > peak {
		peak = cfg.IndexWorkers
	}
	if peak < 1 {
		peak = 1
	}
	return int32(peak)
}

func runDataMigrationPhase(
	dataOnly bool,
	logf func(string, ...any),
	preflightTriggerControl func() error,
	setTriggers func(enable bool) error,
	beforeData func() error,
	migrate func() error,
	afterData func() error,
) (err error) {
	if !dataOnly {
		if err := beforeData(); err != nil {
			return fmt.Errorf("before_data hooks: %w", err)
		}
		if err := migrate(); err != nil {
			return fmt.Errorf("migrate data: %w", err)
		}
		if err := afterData(); err != nil {
			return fmt.Errorf("after_data hooks: %w", err)
		}
		return nil
	}

	logf("preflighting trigger control for data_only...")
	if err := preflightTriggerControl(); err != nil {
		return fmt.Errorf("preflight data_only trigger control: %w", err)
	}

	logf("disabling triggers for data load...")
	if err := setTriggers(false); err != nil {
		return fmt.Errorf("disable triggers: %w", err)
	}

	defer func() {
		if err != nil {
			logf("data load failed; attempting to re-enable triggers...")
		} else {
			logf("re-enabling triggers...")
		}
		if enableErr := setTriggers(true); enableErr != nil {
			enableErr = fmt.Errorf("enable triggers: %w", enableErr)
			if err != nil {
				err = errors.Join(err, enableErr)
				return
			}
			err = enableErr
		}
	}()

	if err := beforeData(); err != nil {
		return fmt.Errorf("before_data hooks: %w", err)
	}
	if err := migrate(); err != nil {
		return fmt.Errorf("migrate data: %w", err)
	}
	if err := afterData(); err != nil {
		return fmt.Errorf("after_data hooks: %w", err)
	}
	return nil
}

func logTemporalWarnings(warnings []PlanTemporalWarning) {
	if len(warnings) == 0 {
		return
	}

	totalColumns := 0
	for _, warning := range warnings {
		totalColumns += warning.Columns
	}

	log.Printf("temporal semantics report: %d warning category(s) across %d column(s)", len(warnings), totalColumns)
	for _, warning := range warnings {
		log.Printf("WARN: %s", warning.Summary)
	}
}

// extractMySQLDBName pulls the database name from a MySQL DSN.
// Expects format: user:pass@tcp(host:port)/dbname or user:pass@host:port/dbname
func extractMySQLDBName(dsn string) (string, error) {
	// Find the last '/' before any '?' parameters
	paramIdx := len(dsn)
	if i := indexOf(dsn, '?'); i >= 0 {
		paramIdx = i
	}
	slashIdx := lastIndexOf(dsn[:paramIdx], '/')
	if slashIdx < 0 {
		return "", fmt.Errorf("cannot extract database name from DSN: no '/' found")
	}
	dbName := dsn[slashIdx+1 : paramIdx]
	if dbName == "" {
		return "", fmt.Errorf("cannot extract database name from DSN: empty name")
	}
	return dbName, nil
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func lastIndexOf(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

type schemaExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func prepareTargetSchema(ctx context.Context, exec schemaExecutor, schema, onSchemaExists string) error {
	switch onSchemaExists {
	case "recreate":
		if _, err := exec.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(schema))); err != nil {
			return fmt.Errorf("drop schema: %w", err)
		}
		if _, err := exec.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(schema))); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	case "error":
		var exists bool
		if err := exec.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists); err != nil {
			return fmt.Errorf("check schema existence: %w", err)
		}
		if exists {
			return fmt.Errorf("schema %q already exists in target database (on_schema_exists=error)", schema)
		}
		if _, err := exec.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(schema))); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
	default:
		return fmt.Errorf("unsupported on_schema_exists value %q", onSchemaExists)
	}
	return nil
}
