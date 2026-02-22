package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var configPath string

var rootCmd = &cobra.Command{
	Use:   "pgferry [config.toml]",
	Short: "MySQL to PostgreSQL migration tool",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMigration,
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
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

	log.Printf("pgferry — MySQL → PostgreSQL migration")
	log.Printf("config: workers=%d batch_size=%d schema=%s", cfg.Workers, cfg.BatchSize, cfg.Schema)

	// 1. Connect to MySQL (for schema introspection only)
	log.Printf("connecting to MySQL...")
	mysqlDB, err := sql.Open("mysql", cfg.MySQL.DSN+"?parseTime=true&loc=UTC&interpolateParams=true")
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer mysqlDB.Close()
	mysqlDB.SetMaxOpenConns(1)

	if err := mysqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
	}

	// Extract database name from DSN for INFORMATION_SCHEMA queries
	dbName, err := extractMySQLDBName(cfg.MySQL.DSN)
	if err != nil {
		return err
	}

	// 2. Introspect MySQL schema
	log.Printf("introspecting MySQL schema '%s'...", dbName)
	schema, err := introspectSchema(mysqlDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema: %w", err)
	}
	log.Printf("found %d tables", len(schema.Tables))
	for _, t := range schema.Tables {
		log.Printf("  %s → %s (%d cols, %d indexes, %d fks)",
			t.MySQLName, t.PGName, len(t.Columns), len(t.Indexes), len(t.ForeignKeys))
	}

	// Close introspection connection — data migration opens its own per-table connections
	mysqlDB.Close()

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

	// 4. Create schema, drop if exists
	log.Printf("creating schema '%s'...", cfg.Schema)
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(cfg.Schema))); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(cfg.Schema))); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}

	// 5. Create bare UNLOGGED tables (no PKs, FKs, indexes)
	log.Printf("creating tables...")
	if err := createTables(ctx, pgPool, schema, cfg.Schema); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	// 6. before_data hooks
	if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.BeforeData, "before_data"); err != nil {
		return fmt.Errorf("before_data hooks: %w", err)
	}

	// 7. Migrate data (parallel goroutines)
	log.Printf("migrating data with %d workers...", cfg.Workers)
	if err := migrateData(ctx, cfg.MySQL.DSN, pgPool, schema, cfg.Schema, cfg.Workers, cfg.BatchSize); err != nil {
		return fmt.Errorf("migrate data: %w", err)
	}

	// 8. after_data hooks
	if err := loadAndExecSQLFiles(ctx, pgPool, cfg, cfg.Hooks.AfterData, "after_data"); err != nil {
		return fmt.Errorf("after_data hooks: %w", err)
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
