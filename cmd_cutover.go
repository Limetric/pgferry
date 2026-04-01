package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var cutoverWait bool
var cutoverTimeout time.Duration

var cutoverCmd = &cobra.Command{
	Use:   "cutover [migration.toml]",
	Short: "Check replication lag and report cutover readiness",
	Long:  "Compare the CDC checkpoint against the MySQL binlog head. With --wait, block until lag reaches zero.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCutover,
}

func init() {
	cutoverCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
	cutoverCmd.Flags().BoolVar(&cutoverWait, "wait", false, "block until replication lag reaches zero")
	cutoverCmd.Flags().DurationVar(&cutoverTimeout, "timeout", 5*time.Minute, "maximum time to wait (only with --wait)")
}

func runCutover(cmd *cobra.Command, args []string) error {
	cfgPath, err := resolveOptionalConfigPath(configPath, args)
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return missingMigrationConfigError()
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	if cfg.Mode != "cdc" {
		return fmt.Errorf("cutover requires mode = \"cdc\" in config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[cutover] received %s, shutting down...", sig)
		cancel()
	}()

	pgPool, err := pgxpool.New(ctx, cfg.Target.DSN)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pgPool.Close()

	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}
	srcDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}
	defer srcDB.Close()

	if cutoverWait {
		return runCutoverWait(ctx, pgPool, srcDB, cfg.Schema, cutoverTimeout)
	}
	return runCutoverCheck(ctx, pgPool, srcDB, cfg.Schema)
}

func runCutoverCheck(ctx context.Context, pgPool *pgxpool.Pool, srcDB *sql.DB, pgSchema string) error {
	checkpoint, err := readCDCCheckpoint(ctx, pgPool, pgSchema)
	if err != nil {
		return fmt.Errorf("read CDC checkpoint: %w", err)
	}

	mysqlPos, err := captureBinlogPosition(ctx, srcDB)
	if err != nil {
		return err
	}

	lag := computeByteLag(mysqlPos, checkpoint)
	if lag != 0 {
		log.Printf("[cutover] lag=%s, last_applied=%s ago",
			formatLag(lag),
			time.Since(checkpoint.LastApplied).Round(time.Second),
		)
		return fmt.Errorf("replication lag is not zero (lag=%s)", formatLag(lag))
	}

	printCutoverReady(checkpoint)
	return nil
}

func runCutoverWait(ctx context.Context, pgPool *pgxpool.Pool, srcDB *sql.DB, pgSchema string, timeout time.Duration) error {
	log.Printf("[cutover] Waiting for replication lag to reach zero...")

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout: replication lag did not reach zero within %s", timeout)
		}

		checkpoint, err := readCDCCheckpoint(ctx, pgPool, pgSchema)
		if err != nil {
			return fmt.Errorf("read CDC checkpoint: %w", err)
		}

		mysqlPos, err := captureBinlogPosition(ctx, srcDB)
		if err != nil {
			return err
		}

		lag := computeByteLag(mysqlPos, checkpoint)
		if lag == 0 {
			printCutoverReady(checkpoint)
			return nil
		}

		log.Printf("[cutover] lag=%s, last_applied=%s ago",
			formatLag(lag),
			time.Since(checkpoint.LastApplied).Round(time.Second),
		)

		time.Sleep(1 * time.Second)
	}
}

// lagDifferentFile is a sentinel indicating the checkpoint and MySQL head are
// on different binlog files, so byte-level lag cannot be computed.
const lagDifferentFile int64 = -1

func computeByteLag(mysqlPos CDCPosition, checkpoint *CDCCheckpointRow) int64 {
	if mysqlPos.File != checkpoint.BinlogFile {
		return lagDifferentFile
	}
	return int64(mysqlPos.Pos) - checkpoint.BinlogPos
}

func formatLag(lag int64) string {
	if lag == lagDifferentFile {
		return "behind (different binlog file)"
	}
	return "~" + humanize.IBytes(uint64(lag))
}

func printCutoverReady(checkpoint *CDCCheckpointRow) {
	log.Printf("[cutover] lag=0, all events applied")
	if checkpoint.EventsSkipped > 0 {
		log.Printf("[cutover] WARNING: %d event(s) were skipped during replication — target may not be fully in sync", checkpoint.EventsSkipped)
	}
	log.Printf("[cutover] Cutover ready. Source and target are in sync.")
	log.Printf("[cutover]   Binlog position: %s:%d", checkpoint.BinlogFile, checkpoint.BinlogPos)
	log.Printf("[cutover]   Events applied: %s", humanize.Comma(checkpoint.EventsApplied))
	log.Printf("[cutover]   Events skipped: %d", checkpoint.EventsSkipped)
	log.Printf("[cutover]")
	log.Printf("[cutover] Next steps:")
	log.Printf("[cutover]   1. Stop writes to MySQL")
	log.Printf("[cutover]   2. Run 'pgferry cutover' again to confirm zero lag")
	log.Printf("[cutover]   3. Point your application to PostgreSQL")
}
