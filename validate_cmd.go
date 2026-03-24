package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
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
	cfgPath := validateConfigPath
	if len(args) > 0 {
		cfgPath = args[0]
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

	sourceDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)

	pgPool, err := openValidateTargetPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pgPool.Close()

	if err := pingValidateConnections(ctx, src, sourceDB, pgPool); err != nil {
		return err
	}

	dbName, err := src.ExtractDBName(cfg.Source.DSN)
	if err != nil {
		return err
	}

	log.Printf("introspecting %s schema '%s'...", src.Name(), dbName)
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema: %w", err)
	}
	filteredSchema, filterReport, err := filterSchemaTables(schema, cfg)
	if err != nil {
		return fmt.Errorf("filter schema tables: %w", err)
	}
	schema = filteredSchema
	if hasTableFilters(cfg) {
		logTableFilterReport(filterReport)
	}

	typeMap := effectiveTypeMapping(cfg)

	// Release the introspection handle before validation opens its own source connections.
	sourceDB.Close()

	logValidationPlan(mode, cfg.SourceSnapshotMode)
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

func pingValidateConnections(ctx context.Context, src SourceDB, sourceDB pingContextDB, pgPool *pgxpool.Pool) error {
	srcLabel := strings.ToLower(src.Name())
	log.Printf("pinging %s and PostgreSQL...", srcLabel)

	pingCtx, pingCancel := context.WithTimeout(ctx, connectivityPingTimeout)
	defer pingCancel()

	var srcPingErr error
	var pgPingErr error
	var g errgroup.Group
	g.Go(func() error {
		if err := sourceDB.PingContext(pingCtx); err != nil {
			srcPingErr = fmt.Errorf("ping %s: %w", srcLabel, err)
		}
		return nil
	})
	g.Go(func() error {
		if err := pgPool.Ping(pingCtx); err != nil {
			pgPingErr = fmt.Errorf("ping postgres: %w", err)
		}
		return nil
	})
	g.Wait()

	if err := errors.Join(srcPingErr, pgPingErr); err != nil {
		return err
	}
	return nil
}

type pingContextDB interface {
	PingContext(context.Context) error
}
