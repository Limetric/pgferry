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

type Config struct {
	MySQLDSN    string
	PostgresDSN string
	Workers     int
	BatchSize   int
	Schema      string
}

var cfg Config

var rootCmd = &cobra.Command{
	Use:   "pgferry",
	Short: "MySQL to PostgreSQL migration tool",
	RunE:  runMigration,
}

func init() {
	rootCmd.Flags().StringVar(&cfg.MySQLDSN, "mysql", "", "MySQL DSN (e.g. root:root@tcp(127.0.0.1:3306)/dbname)")
	rootCmd.Flags().StringVar(&cfg.PostgresDSN, "postgres", "", "PostgreSQL DSN (e.g. postgres://user:pass@host:5432/dbname)")
	rootCmd.Flags().IntVar(&cfg.Workers, "workers", 4, "number of parallel table migrations")
	rootCmd.Flags().IntVar(&cfg.BatchSize, "batch-size", 50000, "rows per SELECT batch")
	rootCmd.Flags().StringVar(&cfg.Schema, "schema", "app", "target PostgreSQL schema")

	rootCmd.MarkFlagRequired("mysql")
	rootCmd.MarkFlagRequired("postgres")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runMigration(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	start := time.Now()

	log.Printf("pgferry — MySQL → PostgreSQL migration")
	log.Printf("config: workers=%d batch_size=%d schema=%s", cfg.Workers, cfg.BatchSize, cfg.Schema)

	// 1. Connect to MySQL (for schema introspection only)
	log.Printf("connecting to MySQL...")
	mysqlDB, err := sql.Open("mysql", cfg.MySQLDSN+"?parseTime=true&loc=UTC&interpolateParams=true")
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer mysqlDB.Close()
	mysqlDB.SetMaxOpenConns(1)

	if err := mysqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping mysql: %w", err)
	}

	// Extract database name from DSN for INFORMATION_SCHEMA queries
	dbName, err := extractMySQLDBName(cfg.MySQLDSN)
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
	pgPool, err := pgxpool.New(ctx, cfg.PostgresDSN)
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

	// 6. Migrate data (parallel goroutines)
	log.Printf("migrating data with %d workers...", cfg.Workers)
	if err := migrateData(ctx, cfg.MySQLDSN, pgPool, schema, cfg.Schema, cfg.Workers, cfg.BatchSize); err != nil {
		return fmt.Errorf("migrate data: %w", err)
	}

	// 7. Post-migration: SET LOGGED, PKs, indexes, orphan cleanup, FKs, sequences, triggers
	log.Printf("running post-migration steps...")
	if err := postMigrate(ctx, pgPool, schema, cfg.Schema); err != nil {
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
