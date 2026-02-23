package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "pgferry [config.toml]",
	Short: "Source database to PostgreSQL migration tool",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMigration,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print pgferry version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, _ []string) {
		fmt.Fprintln(cmd.OutOrStdout(), versionString())
	},
}

func init() {
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigration(cmd *cobra.Command, args []string) error {
	// Resolve config path: positional arg takes precedence over --config flag
	cfgPath := configPath
	if len(args) > 0 {
		cfgPath = args[0]
	}
	if cfgPath == "" {
		return fmt.Errorf("config file required: pgferry <config.toml> or pgferry --config <config.toml>")
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	ctx := context.Background()
	start := time.Now()

	// Force unlogged_tables=false in split modes (no bulk load benefit)
	if cfg.SchemaOnly || cfg.DataOnly {
		cfg.UnloggedTables = false
	}

	// Initialize source database backend
	src, err := newSourceDB(cfg.Source.Type)
	if err != nil {
		return err
	}
	src.SetSnakeCaseIdentifiers(cfg.SnakeCaseIdentifiers)

	log.Printf("pgferry — %s → PostgreSQL migration", src.Name())
	mode := "full"
	if cfg.SchemaOnly {
		mode = "schema_only"
	} else if cfg.DataOnly {
		mode = "data_only"
	}
	log.Printf(
		"config: mode=%s workers=%d schema=%s on_schema_exists=%s source_snapshot_mode=%s unlogged_tables=%t preserve_defaults=%t add_unsigned_checks=%t snake_case_identifiers=%t replicate_on_update_current_timestamp=%t",
		mode,
		cfg.Workers,
		cfg.Schema,
		cfg.OnSchemaExists,
		cfg.SourceSnapshotMode,
		cfg.UnloggedTables,
		cfg.PreserveDefaults,
		cfg.AddUnsignedChecks,
		cfg.SnakeCaseIdentifiers,
		cfg.ReplicateOnUpdateCurrentTimestamp,
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
	log.Printf("introspecting %s schema '%s'...", src.Name(), dbName)
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema: %w", err)
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
	if warnings := collectIndexCompatibilityWarnings(schema); len(warnings) > 0 {
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
	if typeErrs := collectUnsupportedTypeErrors(schema, cfg.TypeMapping, src.MapType); len(typeErrs) > 0 {
		var b strings.Builder
		b.WriteString("unsupported source column types detected:\n")
		for _, e := range typeErrs {
			b.WriteString("  - ")
			b.WriteString(e)
			b.WriteByte('\n')
		}
		b.WriteString("Hint: set [type_mapping].unknown_as_text = true to coerce unknown types to text.")
		return fmt.Errorf("%s", b.String())
	}

	// Close introspection connection — data migration opens its own connections
	sourceDB.Close()

	// 3. Connect to PostgreSQL
	log.Printf("connecting to PostgreSQL...")
	pgPool, err := pgxpool.New(ctx, cfg.Postgres.DSN)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pgPool.Close()

	if err := pgPool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	// 4. Create schema based on configured conflict behavior
	if !cfg.DataOnly {
		log.Printf("preparing schema '%s'...", cfg.Schema)
		if err := prepareTargetSchema(ctx, pgPool, cfg.Schema, cfg.OnSchemaExists); err != nil {
			return err
		}

		// 5. Create bare tables (no PKs, FKs, indexes)
		log.Printf("creating tables...")
		if err := createTables(ctx, pgPool, schema, cfg.Schema, cfg.UnloggedTables, cfg.PreserveDefaults, cfg.TypeMapping, src); err != nil {
			return fmt.Errorf("create tables: %w", err)
		}
	}

	if !cfg.SchemaOnly {
		// In data_only mode, FKs are already in place from the schema_only run.
		// Disable triggers to prevent FK violations during parallel COPY.
		if cfg.DataOnly {
			log.Printf("disabling triggers for data load...")
			if err := setTriggers(ctx, pgPool, schema, cfg.Schema, false); err != nil {
				return fmt.Errorf("disable triggers: %w", err)
			}
		}

		// 6. before_data hooks
		if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.BeforeData, "before_data"); err != nil {
			return fmt.Errorf("before_data hooks: %w", err)
		}

		// 7. Migrate data
		if cfg.SourceSnapshotMode == "single_tx" {
			log.Printf("migrating data with source_snapshot_mode=single_tx (sequential)")
		} else {
			log.Printf("migrating data with %d workers...", cfg.Workers)
		}
		if err := migrateData(ctx, src, cfg.Source.DSN, pgPool, schema, cfg.Schema, cfg.Workers, cfg.TypeMapping, cfg.SourceSnapshotMode); err != nil {
			return fmt.Errorf("migrate data: %w", err)
		}

		// 8. after_data hooks
		if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.AfterData, "after_data"); err != nil {
			return fmt.Errorf("after_data hooks: %w", err)
		}

		if cfg.DataOnly {
			log.Printf("re-enabling triggers...")
			if err := setTriggers(ctx, pgPool, schema, cfg.Schema, true); err != nil {
				return fmt.Errorf("enable triggers: %w", err)
			}
		}
	}

	// 9. Post-migration: SET LOGGED, PKs, indexes, hooks, FKs, sequences, triggers
	log.Printf("running post-migration steps...")
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		return fmt.Errorf("post-migrate: %w", err)
	}

	log.Printf("migration completed in %s", time.Since(start).Round(time.Millisecond))
	return nil
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
