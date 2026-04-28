package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var validateConfigPath string

var validateCmd = &cobra.Command{
	Use:   "validate [migration.toml]",
	Short: "Re-run post-load validation without migrating again",
	Long: `Re-run post-load validation against an existing migration target.

Use this after a previous migrate run when you want to verify row counts or
sampled hashes again without re-running schema creation, data copy, hooks,
checkpoints, or post-migrate DDL.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runValidate,
}

var validateConfigRunner = runValidateWithConfig

func init() {
	validateCmd.Flags().StringVar(&validateConfigPath, "config", "", "path to migration TOML config file")
	rootCmd.AddCommand(validateCmd)
}

func runValidate(_ *cobra.Command, args []string) error {
	cfgPath, err := resolveOptionalConfigPath(validateConfigPath, args)
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return missingValidateConfigError()
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	return validateConfigRunner(cfg)
}

func missingValidateConfigError() error {
	return fmt.Errorf("config file required: pgferry validate <migration.toml> or pgferry validate --config <migration.toml>")
}

func runValidateWithConfig(cfg *MigrationConfig) error {
	mode := strings.TrimSpace(cfg.Validation)
	if mode == "" || mode == validationModeNone {
		return fmt.Errorf("validation disabled in config: set validation = %q or %q before running pgferry validate", validationModeRowCount, validationModeSampledHash)
	}

	ctx := context.Background()

	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}

	log.Printf("pgferry validate — %s source vs PostgreSQL target", src.Name())

	pgPool, err := openValidateTargetPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pgPool.Close()

	schema, err := loadValidateSchema(ctx, src, pgPool, cfg)
	if err != nil {
		return err
	}

	typeMap := effectiveTypeMapping(cfg)
	logStandaloneValidationPlan(mode)
	log.Printf("running standalone validation (mode=%s)...", mode)
	if _, err := validateMigration(ctx, src, cfg.Source.DSN, pgPool, schema, cfg.Schema, mode, cfg.Workers, typeMap); err != nil {
		return fmt.Errorf("validation: %w", err)
	}

	log.Printf("validation passed")
	return nil
}

func openValidateTargetPool(ctx context.Context, cfg *MigrationConfig) (*pgxpool.Pool, error) {
	poolCfg, poolWarning, err := buildTargetPoolConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if poolWarning != "" {
		log.Printf("WARN: %s", poolWarning)
	}
	pgPool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	return pgPool, nil
}

func loadValidateSchema(ctx context.Context, src SourceDB, pgPool *pgxpool.Pool, cfg *MigrationConfig) (*Schema, error) {
	sourceDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return nil, err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)

	if err := pingSourceAndTarget(ctx, src, sourceDB, pgPool); err != nil {
		return nil, err
	}

	dbName, err := src.ExtractDBName(cfg.Source.DSN)
	if err != nil {
		return nil, err
	}

	log.Printf("introspecting %s schema '%s'...", src.Name(), dbName)
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		return nil, fmt.Errorf("introspect schema: %w", err)
	}
	filteredSchema, filterReport, err := filterSchemaTables(schema, cfg)
	if err != nil {
		return nil, fmt.Errorf("filter schema tables: %w", err)
	}
	if hasTableFilters(cfg) {
		logTableFilterReport(filterReport)
	}
	filteredSchema, columnFilterReport, err := filterSchemaColumns(filteredSchema, cfg)
	if err != nil {
		return nil, fmt.Errorf("filter schema columns: %w", err)
	}
	if hasColumnFilters(cfg) {
		logColumnFilterReport(columnFilterReport)
	}
	return filteredSchema, nil
}

func logStandaloneValidationPlan(mode string) {
	log.Printf("validation mode: %s", validationModeSummary(mode))
	log.Printf("validation caveat: standalone validate compares the current source state against already-loaded target data; if the source changed after migrate, mismatches may reflect drift after the original load")
	log.Printf("validation caveat: standalone validate does not rerun after_data hooks; validate the same target state those hooks already produced")
	switch mode {
	case validationModeRowCount:
		log.Printf("validation caveat: row_count does not compare row contents, transformed values, or per-row semantics")
	case validationModeSampledHash:
		log.Printf("validation caveat: sampled_hash is intentionally bounded: it samples deterministic rows and only compares columns with supported canonical fingerprints")
	}
}
