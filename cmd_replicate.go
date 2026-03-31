package main

import (
	"context"
	"errors"
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

var replicateCmd = &cobra.Command{
	Use:   "replicate [migration.toml]",
	Short: "Tail MySQL binlog and apply changes to PostgreSQL",
	Long:  "Start CDC replication from the binlog position captured during migrate. Runs until interrupted (Ctrl+C).",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runReplicate,
}

func init() {
	replicateCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
}

func runReplicate(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("replicate requires mode = \"cdc\" in config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[replicate] received %s, shutting down gracefully...", sig)
		cancel()
	}()

	pgPool, err := pgxpool.New(ctx, cfg.Target.DSN)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pgPool.Close()

	checkpoint, err := readCDCCheckpoint(ctx, pgPool, cfg.Schema)
	if err != nil {
		return fmt.Errorf("read CDC checkpoint (has migrate been run with mode = \"cdc\"?): %w", err)
	}

	startPos := checkpoint.Position()
	log.Printf("[replicate] starting from binlog=%s:%d applied=%d skipped=%d",
		startPos.File, startPos.Pos, checkpoint.EventsApplied, checkpoint.EventsSkipped)

	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}
	srcDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}
	dbName, err := src.ExtractDBName(cfg.Source.DSN)
	if err != nil {
		return fmt.Errorf("extract source db name: %w", err)
	}
	schema, err := src.IntrospectSchema(srcDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect source schema: %w", err)
	}
	srcDB.Close()

	filteredSchema, _, filterErr := filterSchemaTables(schema, cfg)
	if filterErr != nil {
		return fmt.Errorf("filter tables: %w", filterErr)
	}

	tables := make(map[string]Table)
	for _, t := range filteredSchema.Tables {
		if t.PrimaryKey == nil || len(t.PrimaryKey.Columns) == 0 {
			log.Printf("[replicate] WARN: skipping table %s (no primary key)", t.SourceName)
			continue
		}
		tables[t.SourceName] = t
	}

	if len(tables) == 0 {
		return fmt.Errorf("no tables with primary keys found for replication")
	}
	log.Printf("[replicate] tracking %d tables", len(tables))

	reader, err := NewBinlogReader(BinlogReaderConfig{
		DSN:      cfg.Source.DSN,
		ServerID: cfg.CDCServerID,
		StartPos: startPos,
		Tables:   tables,
		Src:      src,
		TypeMap:  cfg.TypeMapping,
		DBName:   dbName,
	})
	if err != nil {
		return fmt.Errorf("start binlog reader: %w", err)
	}
	defer reader.Close()

	applier := NewCDCApplier(pgPool, cfg.Schema, tables, src, cfg.TypeMapping)
	batcher := newCDCBatcher(cfg.CDCBatchSize)

	applier.applied.Store(checkpoint.EventsApplied)
	applier.skipped.Store(checkpoint.EventsSkipped)

	log.Printf("[replicate] replication started")
	return runReplicateLoop(ctx, reader, batcher, applier, cfg.CDCFlushInterval)
}

func runReplicateLoop(ctx context.Context, reader *BinlogReader, batcher *CDCBatcher, applier *CDCApplier, flushInterval time.Duration) error {
	const statusInterval = 10 * time.Second

	lastFlush := time.Now()
	lastApply := time.Now()
	lastStatus := time.Now()

	for {
		select {
		case <-ctx.Done():
			if batch := batcher.Flush(); batch != nil {
				bgCtx := context.Background()
				if err := applier.ApplyBatch(bgCtx, batch, batcher.Position()); err != nil {
					log.Printf("[replicate] WARN: final flush failed: %v", err)
				}
			}
			applied, skipped := applier.Stats()
			log.Printf("[replicate] shutdown complete. applied=%d skipped=%d", applied, skipped)
			return nil
		default:
		}

		readCtx, readCancel := context.WithTimeout(ctx, flushInterval)
		ev, err := reader.ReadEvent(readCtx)
		readCancel()

		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("read binlog event: %w", err)
		}

		if ev != nil {
			if batch := batcher.Add(ev); batch != nil {
				if err := applier.ApplyBatch(ctx, batch, batcher.Position()); err != nil {
					return err
				}
				lastFlush = time.Now()
				lastApply = time.Now()
			}
		}

		if batcher.Len() > 0 && time.Since(lastFlush) >= flushInterval {
			if batch := batcher.Flush(); batch != nil {
				if err := applier.ApplyBatch(ctx, batch, batcher.Position()); err != nil {
					return err
				}
				lastFlush = time.Now()
				lastApply = time.Now()
			}
		}

		if time.Since(lastStatus) >= statusInterval {
			applied, skipped := applier.Stats()
			pos := batcher.Position()
			if pos.File == "" {
				pos = reader.pos
			}
			log.Printf("[replicate] binlog=%s:%d | applied=%s | skipped=%d | last_applied=%s ago",
				pos.File, pos.Pos,
				humanize.Comma(applied),
				skipped,
				time.Since(lastApply).Round(time.Second),
			)
			lastStatus = time.Now()
		}
	}
}
