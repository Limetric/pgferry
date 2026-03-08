package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateDataConfig holds parameters for data migration.
type migrateDataConfig struct {
	Src                SourceDB
	SrcDSN             string
	Pool               *pgxpool.Pool
	Schema             *Schema
	PGSchema           string
	Workers            int
	TypeMap            TypeMappingConfig
	SourceSnapshotMode string
	ChunkSize          int64
	Resume             bool
	ConfigDir          string
}

// migrateData streams data from the source to PostgreSQL for all tables using parallel workers.
func migrateData(ctx context.Context, cfg migrateDataConfig) error {
	switch cfg.SourceSnapshotMode {
	case "single_tx":
		return migrateDataSingleTx(ctx, cfg.Src, cfg.SrcDSN, cfg.Pool, cfg.Schema, cfg.PGSchema, cfg.TypeMap, cfg.ChunkSize, cfg.Resume, cfg.ConfigDir)
	default:
		return migrateDataParallel(ctx, cfg.Src, cfg.SrcDSN, cfg.Pool, cfg.Schema, cfg.PGSchema, cfg.Workers, cfg.TypeMap, cfg.ChunkSize, cfg.Resume, cfg.ConfigDir)
	}
}

func migrateDataParallel(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, workers int, typeMap TypeMappingConfig, chunkSize int64, resume bool, configDir string) error {
	// Plan chunks for each table
	plans, err := buildChunkPlans(ctx, src, srcDSN, schema, chunkSize)
	if err != nil {
		return err
	}

	// Load or create checkpoint
	cpPath := checkpointPath(configDir)
	var cp *CheckpointState
	if resume {
		cp, err = loadCheckpoint(cpPath)
		if err != nil {
			return fmt.Errorf("load checkpoint: %w", err)
		}
		if cp != nil {
			log.Printf("resuming from checkpoint (started %s)", cp.StartedAt.Format(time.RFC3339))
		}
	}
	if cp == nil {
		cp = newCheckpointState()
	}

	var cpMu sync.Mutex

	// Pre-compute skip sets from checkpoint BEFORE launching workers to avoid
	// concurrent map reads (in skip checks) racing with map writes (in workers).
	skipTables := make(map[string]bool)       // tables fully done
	skipChunks := make(map[string]map[int]bool) // table → chunk indices done
	if resume {
		for name, tc := range cp.Tables {
			if tc.FullTableDone {
				skipTables[name] = true
			}
			if len(tc.CompletedChunks) > 0 {
				s := make(map[int]bool, len(tc.CompletedChunks))
				for idx := range tc.CompletedChunks {
					s[idx] = true
				}
				skipChunks[name] = s
			}
		}
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	// Count total work items for error channel sizing
	totalWork := 0
	for _, plan := range plans {
		if plan.ChunkKey != nil {
			totalWork += len(plan.Chunks)
		} else {
			totalWork++
		}
	}
	errCh := make(chan error, totalWork)

	for _, plan := range plans {
		if plan.ChunkKey == nil {
			// Non-chunkable: fall back to full-table copy
			if skipTables[plan.Table.SourceName] {
				log.Printf("  [%s] skipping (completed in previous run)", plan.Table.SourceName)
				continue
			}
			wg.Add(1)
			go func(t Table) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				count, copyErr := migrateTableFull(ctx, src, srcDSN, pool, t, pgSchema, typeMap)
				if copyErr != nil {
					errCh <- fmt.Errorf("table %s: %w", t.SourceName, copyErr)
					return
				}
				cpMu.Lock()
				cp.recordFullTable(t.SourceName, count)
				if saveErr := saveCheckpoint(cpPath, cp); saveErr != nil {
					log.Printf("WARN: failed to save checkpoint: %v", saveErr)
				}
				cpMu.Unlock()
			}(plan.Table)
		} else {
			// Chunkable: dispatch each chunk
			tableSkips := skipChunks[plan.Table.SourceName]
			for _, chunk := range plan.Chunks {
				if tableSkips[chunk.Index] {
					continue
				}
				wg.Add(1)
				go func(t Table, key ChunkKey, c Chunk, chunkCount int) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					count, copyErr := migrateChunk(ctx, src, srcDSN, pool, t, pgSchema, typeMap, key, c)
					if copyErr != nil {
						errCh <- fmt.Errorf("table %s chunk %d: %w", t.SourceName, c.Index, copyErr)
						return
					}
					cpMu.Lock()
					cp.recordChunk(t.SourceName, c.Index, count, chunkCount)
					if saveErr := saveCheckpoint(cpPath, cp); saveErr != nil {
						log.Printf("WARN: failed to save checkpoint: %v", saveErr)
					}
					cpMu.Unlock()
				}(plan.Table, *plan.ChunkKey, chunk, len(plan.Chunks))
			}
		}
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		for _, e := range errs {
			log.Printf("ERROR: %v", e)
		}
		return fmt.Errorf("%d chunk(s)/table(s) failed migration", len(errs))
	}

	// All succeeded — remove checkpoint
	if err := deleteCheckpoint(cpPath); err != nil {
		log.Printf("WARN: failed to delete checkpoint: %v", err)
	}
	return nil
}

func migrateDataSingleTx(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, typeMap TypeMappingConfig, chunkSize int64, resume bool, configDir string) error {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	if _, err := srcDB.ExecContext(ctx, "SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return fmt.Errorf("set source transaction isolation: %w", err)
	}

	tx, err := srcDB.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("begin source transaction: %w", err)
	}
	defer tx.Rollback()

	// Load or create checkpoint
	cpPath := checkpointPath(configDir)
	var cp *CheckpointState
	if resume {
		cp, err = loadCheckpoint(cpPath)
		if err != nil {
			return fmt.Errorf("load checkpoint: %w", err)
		}
		if cp != nil {
			log.Printf("resuming from checkpoint (started %s)", cp.StartedAt.Format(time.RFC3339))
		}
	}
	if cp == nil {
		cp = newCheckpointState()
	}

	log.Printf("source snapshot enabled: single_tx (sequential table copy)")
	for _, t := range schema.Tables {
		key := chunkKeyForTable(t, src)
		if key == nil {
			// Not chunkable — full-table copy
			if resume && cp.isTableDone(t.SourceName) {
				log.Printf("  [%s] skipping (completed in previous run)", t.SourceName)
				continue
			}
			count, copyErr := migrateTableFromSourceFull(ctx, src, tx, pool, t, pgSchema, typeMap)
			if copyErr != nil {
				return fmt.Errorf("table %s: %w", t.SourceName, copyErr)
			}
			cp.recordFullTable(t.SourceName, count)
			if saveErr := saveCheckpoint(cpPath, cp); saveErr != nil {
				log.Printf("WARN: failed to save checkpoint: %v", saveErr)
			}
			continue
		}

		// Chunkable — run chunks sequentially within the transaction
		min, max, hasRows, mmErr := queryMinMax(ctx, tx, src, t, *key)
		if mmErr != nil {
			return mmErr
		}
		if !hasRows {
			log.Printf("  [%s] empty table, skipping", t.SourceName)
			cp.recordFullTable(t.SourceName, 0)
			if saveErr := saveCheckpoint(cpPath, cp); saveErr != nil {
				log.Printf("WARN: failed to save checkpoint: %v", saveErr)
			}
			continue
		}

		chunks := planChunks(min, max, chunkSize)
		log.Printf("  [%s] %d chunks (key=%s, range=%d..%d)", t.SourceName, len(chunks), key.SourceColumn, min, max)
		for _, chunk := range chunks {
			if resume && cp.isChunkCompleted(t.SourceName, chunk.Index) {
				continue
			}
			count, copyErr := migrateChunkFromSource(ctx, src, tx, pool, t, pgSchema, typeMap, *key, chunk)
			if copyErr != nil {
				return fmt.Errorf("table %s chunk %d: %w", t.SourceName, chunk.Index, copyErr)
			}
			cp.recordChunk(t.SourceName, chunk.Index, count, len(chunks))
			if saveErr := saveCheckpoint(cpPath, cp); saveErr != nil {
				log.Printf("WARN: failed to save checkpoint: %v", saveErr)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source transaction: %w", err)
	}

	// All succeeded — remove checkpoint
	if err := deleteCheckpoint(cpPath); err != nil {
		log.Printf("WARN: failed to delete checkpoint: %v", err)
	}
	return nil
}

// migrateTableFull streams one table from source to PG via COPY protocol using its own connection.
func migrateTableFull(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig) (int64, error) {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return 0, err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	return migrateTableFromSourceFull(ctx, src, srcDB, pool, table, pgSchema, typeMap)
}

type dbQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func migrateTableFromSourceFull(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig) (int64, error) {
	log.Printf("  [%s] starting row copy", table.SourceName)

	query := buildSourceSelectQuery(src, table)
	count, err := copyFromSource(ctx, source, pool, table, pgSchema, typeMap, src, query)
	if err != nil {
		return 0, err
	}

	log.Printf("  [%s] done (%d rows copied)", table.SourceName, count)
	return count, nil
}

// migrateChunk copies a single chunk of a table using its own source connection.
func migrateChunk(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, key ChunkKey, chunk Chunk) (int64, error) {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return 0, err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	return migrateChunkFromSource(ctx, src, srcDB, pool, table, pgSchema, typeMap, key, chunk)
}

// migrateChunkFromSource copies a single chunk using an existing source querier.
func migrateChunkFromSource(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, key ChunkKey, chunk Chunk) (int64, error) {
	log.Printf("  [%s] chunk %d starting", table.SourceName, chunk.Index)

	query := buildChunkedSelectQuery(src, table, key, chunk)
	count, err := copyFromSource(ctx, source, pool, table, pgSchema, typeMap, src, query)
	if err != nil {
		return 0, err
	}

	log.Printf("  [%s] chunk %d done (%d rows)", table.SourceName, chunk.Index, count)
	return count, nil
}

// copyFromSource runs a SELECT query on the source and streams results into PG via COPY.
func copyFromSource(ctx context.Context, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, src SourceDB, query string) (int64, error) {
	pgColumns := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		pgColumns[i] = col.PGName
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire pg conn: %w", err)
	}
	defer conn.Release()

	rows, err := source.QueryContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	rs := newRowSource(rows, table, src, typeMap)

	count, err := conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{pgSchema, table.PGName},
		pgColumns,
		rs,
	)
	if err != nil {
		return 0, fmt.Errorf("copy: %w", err)
	}
	return count, nil
}

// buildChunkPlans creates chunk plans for all tables by querying MIN/MAX on chunkable tables.
func buildChunkPlans(ctx context.Context, src SourceDB, srcDSN string, schema *Schema, chunkSize int64) ([]ChunkPlan, error) {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return nil, fmt.Errorf("open source for chunk planning: %w", err)
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	var plans []ChunkPlan
	var chunkable, nonChunkable int
	totalChunks := 0

	for _, t := range schema.Tables {
		key := chunkKeyForTable(t, src)
		if key == nil {
			nonChunkable++
			plans = append(plans, ChunkPlan{Table: t, ChunkSize: chunkSize})
			continue
		}

		min, max, hasRows, err := queryMinMax(ctx, srcDB, src, t, *key)
		if err != nil {
			return nil, err
		}
		if !hasRows {
			// Empty table — single empty plan
			plans = append(plans, ChunkPlan{
				Table:    t,
				ChunkKey: key,
				Chunks:   []Chunk{{Index: 0, IsLast: true}},
				ChunkSize: chunkSize,
			})
			chunkable++
			totalChunks++
			continue
		}

		chunks := planChunks(min, max, chunkSize)
		plans = append(plans, ChunkPlan{
			Table:    t,
			ChunkKey: key,
			Chunks:   chunks,
			ChunkSize: chunkSize,
		})
		chunkable++
		totalChunks += len(chunks)
		log.Printf("  [%s] %d chunks (key=%s, range=%d..%d)", t.SourceName, len(chunks), key.SourceColumn, min, max)
	}

	if chunkable > 0 {
		log.Printf("chunk plan: %d chunkable table(s) (%d total chunks), %d non-chunkable table(s)", chunkable, totalChunks, nonChunkable)
	}
	if nonChunkable > 0 && chunkable == 0 {
		log.Printf("chunk plan: no tables with chunkable primary keys, using full-table copy for all %d table(s)", nonChunkable)
	}

	return plans, nil
}

// rowSource implements pgx.CopyFromSource by reading from source rows.
type rowSource struct {
	rows        *sql.Rows
	table       Table
	scanDest    []any
	scanPtrs    []any
	values      []any
	err         error
	copied      int64
	src         SourceDB
	typeMapping TypeMappingConfig
	tableName   string
	lastLog     time.Time
}

func newRowSource(rows *sql.Rows, table Table, src SourceDB, typeMap TypeMappingConfig) *rowSource {
	numCols := len(table.Columns)
	scanDest := make([]any, numCols)
	scanPtrs := make([]any, numCols)
	for i := range scanDest {
		scanPtrs[i] = &scanDest[i]
	}

	return &rowSource{
		rows:        rows,
		table:       table,
		scanDest:    scanDest,
		scanPtrs:    scanPtrs,
		values:      make([]any, numCols),
		src:         src,
		typeMapping: typeMap,
		tableName:   table.SourceName,
		lastLog:     time.Now(),
	}
}

func (r *rowSource) Next() bool {
	if !r.rows.Next() {
		r.err = r.rows.Err()
		return false
	}

	if err := r.rows.Scan(r.scanPtrs...); err != nil {
		r.err = err
		return false
	}

	for i, col := range r.table.Columns {
		v, err := r.src.TransformValue(r.scanDest[i], col, r.typeMapping)
		if err != nil {
			r.err = fmt.Errorf("column %s: %w", col.SourceName, err)
			return false
		}
		r.values[i] = v
	}

	r.copied++
	if now := time.Now(); now.Sub(r.lastLog) >= 10*time.Second {
		log.Printf("  [%s] progress: %d rows copied", r.tableName, r.copied)
		r.lastLog = now
	}
	return true
}

func (r *rowSource) Values() ([]any, error) {
	return r.values, nil
}

func (r *rowSource) Err() error {
	return r.err
}

func buildSourceSelectQuery(src SourceDB, table Table) string {
	cols := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = src.QuoteIdentifier(col.SourceName)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), src.QuoteIdentifier(table.SourceName))
}
