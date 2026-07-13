package main

import (
	"context"
	"database/sql"
	"errors"
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
	LogLevel           string
	// ResumeCompatibility is used only when Resume=true to validate that an
	// existing checkpoint still matches the current migration shape.
	ResumeCompatibility checkpointCompatibility
}

// migrateData streams data from the source to PostgreSQL for all tables using parallel workers.
func migrateData(ctx context.Context, cfg migrateDataConfig) error {
	switch cfg.SourceSnapshotMode {
	case "single_tx":
		return migrateDataSingleTx(ctx, cfg)
	default:
		return migrateDataParallel(ctx, cfg)
	}
}

func migrateDataParallel(ctx context.Context, cfg migrateDataConfig) error {
	// Plan chunks for each table
	plans, err := buildChunkPlans(ctx, cfg.Src, cfg.SrcDSN, cfg.Schema, cfg.ChunkSize, cfg.TypeMap, cfg.Workers, cfg.LogLevel)
	if err != nil {
		return err
	}

	// Create checkpoint manager: noop when resume is disabled to avoid
	// all checkpoint file I/O in the hot path.
	cpPath := checkpointPath(cfg.ConfigDir)
	var mgr checkpointManager
	if cfg.Resume {
		pm, mgrErr := newPersistentCheckpointManager(cpPath, &cfg.ResumeCompatibility)
		if mgrErr != nil {
			return fmt.Errorf("load checkpoint: %w", mgrErr)
		}
		mgr = pm
	} else {
		mgr = &noopCheckpointManager{path: cpPath}
	}

	workItems := buildParallelMigrationWorkItems(plans, mgr)
	progress := newMigrationProgressLogger(cfg.LogLevel, workItems)
	if err := runParallelMigrationWorkers(
		ctx,
		cfg.Workers,
		func() (migrationWorkerSource, error) {
			return openMigrationSourceDB(cfg.Src, cfg.SrcDSN)
		},
		workItems,
		mgr,
		func(ctx context.Context, source dbQuerier, item migrationWorkItem) (int64, error) {
			if item.ChunkKey == nil {
				return migrateTableFromSourceFull(ctx, cfg.Src, source, cfg.Pool, item.Table, cfg.PGSchema, cfg.TypeMap, item.PGCopyColumns, cfg.LogLevel)
			}
			progress.StartChunkedTable(item.Table.SourceName)
			count, err := migrateChunkFromSource(ctx, cfg.Src, source, cfg.Pool, item.Table, cfg.PGSchema, cfg.TypeMap, *item.ChunkKey, item.Chunk, item.ColumnSelectList, item.PGCopyColumns, cfg.LogLevel)
			if err != nil {
				return 0, err
			}
			progress.FinishChunk(item.Table.SourceName, count)
			return count, nil
		},
	); err != nil {
		// Flush partial progress so a resumed run can skip completed work.
		if flushErr := mgr.Flush(); flushErr != nil {
			log.Printf("WARN: failed to save checkpoint: %v", flushErr)
		}
		return err
	}

	// All succeeded — remove checkpoint file (no flush needed; there is
	// nothing to resume and any batched state can be discarded).
	if err := mgr.Cleanup(); err != nil {
		log.Printf("WARN: failed to delete checkpoint: %v", err)
	}
	return nil
}

type migrationWorkItem struct {
	Table            Table
	ChunkKey         *ChunkKey
	Chunk            Chunk
	ChunkCount       int
	ColumnSelectList string   // pre-joined SELECT list for chunked reads; empty for full-table items
	PGCopyColumns    []string // PG column names for COPY; same order as table.Columns
}

type migrationWorkerSource interface {
	dbQuerier
	Close() error
}

type migrationWorkExecutor func(context.Context, dbQuerier, migrationWorkItem) (int64, error)

func buildParallelMigrationWorkItems(plans []ChunkPlan, mgr checkpointManager) []migrationWorkItem {
	workItems := make([]migrationWorkItem, 0, len(plans))
	for _, plan := range plans {
		if plan.ChunkKey == nil {
			if mgr.IsTableDone(plan.Table.SourceName) {
				log.Printf("  [%s] skipping (completed in previous run)", plan.Table.SourceName)
				continue
			}
			workItems = append(workItems, migrationWorkItem{Table: plan.Table, PGCopyColumns: plan.PGCopyColumns})
			continue
		}

		for _, chunk := range plan.Chunks {
			if mgr.IsChunkCompleted(plan.Table.SourceName, chunk.Index) {
				continue
			}
			workItems = append(workItems, migrationWorkItem{
				Table:            plan.Table,
				ChunkKey:         plan.ChunkKey,
				Chunk:            chunk,
				ChunkCount:       len(plan.Chunks),
				ColumnSelectList: plan.ColumnSelectList,
				PGCopyColumns:    plan.PGCopyColumns,
			})
		}
	}
	return workItems
}

func runParallelMigrationWorkers(ctx context.Context, workers int, openSource func() (migrationWorkerSource, error), workItems []migrationWorkItem, mgr checkpointManager, execute migrationWorkExecutor) error {
	if len(workItems) == 0 {
		return nil
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(workItems) {
		workers = len(workItems)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Buffered channel decouples enqueue from workers; size is bounded by work list
	// and a small multiple of workers for light prefetch (correctness unchanged).
	bufSize := workers * 2
	if bufSize > len(workItems) {
		bufSize = len(workItems)
	}
	if bufSize < 1 {
		bufSize = 1
	}
	workCh := make(chan migrationWorkItem, bufSize)
	var wg sync.WaitGroup

	var firstErr error
	var errOnce sync.Once
	var otherErrsMu sync.Mutex
	var otherErrs []error
	recordErr := func(err error) {
		isFirst := false
		errOnce.Do(func() {
			firstErr = err
			isFirst = true
			cancel()
		})
		if !isFirst {
			otherErrsMu.Lock()
			otherErrs = append(otherErrs, err)
			otherErrsMu.Unlock()
		}
	}

	for workerID := 0; workerID < workers; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			var source migrationWorkerSource
			defer func() {
				if source != nil {
					source.Close()
				}
			}()

			for {
				select {
				case <-ctx.Done():
					return
				case item, ok := <-workCh:
					if !ok {
						return
					}

					// Retry transient connection failures. Each chunk COPY is its own
					// transaction and rolls back cleanly on error, so re-running a work
					// item cannot double-insert rows. Only the parallel path retries:
					// single_tx holds a long-lived snapshot that reconnecting would
					// silently replace.
					var count int64
					failed := false
					for attempt := 1; ; attempt++ {
						if source == nil {
							var openErr error
							source, openErr = openSource()
							if openErr != nil {
								if attempt < maxCopyAttempts && isTransientError(openErr) {
									log.Printf("  WARN: open source worker: %v (attempt %d/%d), retrying in %s",
										openErr, attempt, maxCopyAttempts, copyRetryBackoff(attempt))
									if waitBeforeRetry(ctx, attempt) != nil {
										return
									}
									continue
								}
								recordErr(fmt.Errorf("open source worker: %w", openErr))
								return
							}
						}

						var err error
						count, err = execute(ctx, source, item)
						if err == nil {
							break
						}
						if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
							return
						}
						if attempt < maxCopyAttempts && isTransientError(err) {
							log.Printf("  WARN: %v (attempt %d/%d), retrying in %s",
								formatMigrationWorkError(item, err), attempt, maxCopyAttempts, copyRetryBackoff(attempt))
							// The connection may be dead; drop it so the next attempt redials.
							source.Close()
							source = nil
							if waitBeforeRetry(ctx, attempt) != nil {
								return
							}
							continue
						}
						recordErr(formatMigrationWorkError(item, err))
						failed = true
						break
					}
					if failed {
						return
					}
					recordMigrationWorkResult(mgr, item, count)
				}
			}
		}()
	}

enqueue:
	for _, item := range workItems {
		select {
		case <-ctx.Done():
			break enqueue
		case workCh <- item:
		}
	}
	close(workCh)
	wg.Wait()

	if firstErr != nil {
		log.Printf("ERROR: %v", firstErr)
		for _, err := range otherErrs {
			log.Printf("ERROR: %v", err)
		}
		return firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func formatMigrationWorkError(item migrationWorkItem, err error) error {
	if item.ChunkKey == nil {
		return fmt.Errorf("table %s: %w", item.Table.SourceName, err)
	}
	return fmt.Errorf("table %s chunk %d: %w", item.Table.SourceName, item.Chunk.Index, err)
}

func recordMigrationWorkResult(mgr checkpointManager, item migrationWorkItem, count int64) {
	if item.ChunkKey == nil {
		mgr.RecordFullTable(item.Table.SourceName, count)
		return
	}
	mgr.RecordChunk(item.Table.SourceName, item.Chunk.Index, count, item.ChunkCount)
}

func migrateDataSingleTx(ctx context.Context, cfg migrateDataConfig) error {
	srcDB, err := cfg.Src.OpenDB(cfg.SrcDSN)
	if err != nil {
		return err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	var tx *sql.Tx
	switch cfg.Src.Name() {
	case "MSSQL":
		dbName, nameErr := cfg.Src.ExtractDBName(cfg.SrcDSN)
		if nameErr != nil {
			return fmt.Errorf("mssql single_tx: %w", nameErr)
		}
		// snapshot_isolation_state 1 = ALLOW_SNAPSHOT_ISOLATION is ON (see sys.databases).
		// Skip ALTER when already enabled so read-only logins can still use single_tx.
		var snapState int
		if err := srcDB.QueryRowContext(ctx, "SELECT snapshot_isolation_state FROM sys.databases WHERE name = DB_NAME()").Scan(&snapState); err != nil {
			return fmt.Errorf("mssql single_tx: read snapshot_isolation_state: %w", err)
		}
		if snapState != 1 {
			alter := fmt.Sprintf("ALTER DATABASE %s SET ALLOW_SNAPSHOT_ISOLATION ON", cfg.Src.QuoteIdentifier(dbName))
			if _, err := srcDB.ExecContext(ctx, alter); err != nil {
				return fmt.Errorf("enable allow_snapshot_isolation on source database %q (required for source_snapshot_mode=single_tx when not already enabled; login needs ALTER on the database): %w", dbName, err)
			}
		}
		if _, err := srcDB.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL SNAPSHOT"); err != nil {
			return fmt.Errorf("set source transaction isolation: %w", err)
		}
		// go-mssqldb rejects ReadOnly transactions ("read-only transactions are not supported").
		// Snapshot isolation still gives a consistent point-in-time view; we only issue SELECTs.
		tx, err = srcDB.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelSnapshot,
			ReadOnly:  false,
		})
	default:
		if _, err := srcDB.ExecContext(ctx, "SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
			return fmt.Errorf("set source transaction isolation: %w", err)
		}
		tx, err = srcDB.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelRepeatableRead,
			ReadOnly:  true,
		})
	}
	if err != nil {
		return fmt.Errorf("begin source transaction: %w", err)
	}
	defer tx.Rollback()

	// Create checkpoint manager: noop when resume is disabled.
	cpPath := checkpointPath(cfg.ConfigDir)
	var mgr checkpointManager
	if cfg.Resume {
		pm, mgrErr := newPersistentCheckpointManager(cpPath, &cfg.ResumeCompatibility)
		if mgrErr != nil {
			return fmt.Errorf("load checkpoint: %w", mgrErr)
		}
		mgr = pm
	} else {
		mgr = &noopCheckpointManager{path: cpPath}
	}

	// On error, flush partial checkpoint progress so a resumed run can skip
	// completed work. This is a no-op when resume=false (noop manager).
	success := false
	defer func() {
		if !success {
			if flushErr := mgr.Flush(); flushErr != nil {
				log.Printf("WARN: failed to save checkpoint: %v", flushErr)
			}
		}
	}()

	log.Printf("source snapshot enabled: single_tx (sequential table copy)")
	for _, t := range cfg.Schema.Tables {
		key := chunkKeyForTable(t, cfg.Src)
		if key == nil {
			// Not chunkable — full-table copy
			if mgr.IsTableDone(t.SourceName) {
				log.Printf("  [%s] skipping (completed in previous run)", t.SourceName)
				continue
			}
			count, copyErr := migrateTableFromSourceFull(ctx, cfg.Src, tx, cfg.Pool, t, cfg.PGSchema, cfg.TypeMap, tablePGCopyColumns(t), cfg.LogLevel)
			if copyErr != nil {
				return fmt.Errorf("table %s: %w", t.SourceName, copyErr)
			}
			mgr.RecordFullTable(t.SourceName, count)
			continue
		}

		// Chunkable — run chunks sequentially within the transaction
		min, max, hasRows, mmErr := queryMinMax(ctx, tx, cfg.Src, t, *key)
		if mmErr != nil {
			return mmErr
		}
		if !hasRows {
			log.Printf("  [%s] empty table, skipping", t.SourceName)
			mgr.RecordFullTable(t.SourceName, 0)
			continue
		}

		chunks := planChunks(min, max, cfg.ChunkSize)
		colSelectList := buildColumnSelectList(cfg.Src, t, cfg.TypeMap)
		pgCols := tablePGCopyColumns(t)
		chunksToCopy := pendingChunks(t.SourceName, chunks, mgr)
		if len(chunksToCopy) == 0 {
			continue
		}
		if isVerboseMigrateLogLevel(cfg.LogLevel) {
			log.Printf("  [%s] %d chunks (key=%s, range=%d..%d)", t.SourceName, len(chunks), key.SourceColumn, min, max)
		}
		if isTableMigrateLogLevel(cfg.LogLevel) {
			log.Printf("  [%s] starting row copy", t.SourceName)
		}
		var copied int64
		for _, chunk := range chunksToCopy {
			count, copyErr := migrateChunkFromSource(ctx, cfg.Src, tx, cfg.Pool, t, cfg.PGSchema, cfg.TypeMap, *key, chunk, colSelectList, pgCols, cfg.LogLevel)
			if copyErr != nil {
				return fmt.Errorf("table %s chunk %d: %w", t.SourceName, chunk.Index, copyErr)
			}
			copied += count
			mgr.RecordChunk(t.SourceName, chunk.Index, count, len(chunks))
		}
		if isTableMigrateLogLevel(cfg.LogLevel) {
			log.Printf("  [%s] done (%d rows copied)", t.SourceName, copied)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source transaction: %w", err)
	}

	success = true
	// All succeeded — remove checkpoint file (no flush needed; there is
	// nothing to resume and any batched state can be discarded).
	if err := mgr.Cleanup(); err != nil {
		log.Printf("WARN: failed to delete checkpoint: %v", err)
	}
	return nil
}

func pendingChunks(tableName string, chunks []Chunk, mgr checkpointManager) []Chunk {
	pending := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if mgr.IsChunkCompleted(tableName, chunk.Index) {
			continue
		}
		pending = append(pending, chunk)
	}
	return pending
}

func openMigrationSourceDB(src SourceDB, srcDSN string) (*sql.DB, error) {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return nil, err
	}
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)
	return srcDB, nil
}

const maxChunkPlanningWorkers = 16

// chunkPlanningWorkers returns the effective worker count for chunk planning,
// reusing migration workers while capping planning fan-out to avoid placing
// unexpected load on small source instances.
func chunkPlanningWorkers(workers int, src SourceDB) int {
	if srcMax := src.MaxWorkers(); srcMax > 0 && workers > srcMax {
		workers = srcMax
	}
	if workers > maxChunkPlanningWorkers {
		workers = maxChunkPlanningWorkers
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

type dbQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func migrateTableFromSourceFull(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, pgCopyColumns []string, logLevel string) (int64, error) {
	if shouldLogTableRowCopy(logLevel) {
		log.Printf("  [%s] starting row copy", table.SourceName)
	}

	query := buildSourceSelectQuery(src, table, typeMap)
	count, err := copyFromSource(ctx, source, pool, table, pgSchema, typeMap, src, query, pgCopyColumns, logLevel)
	if err != nil {
		return 0, err
	}

	if shouldLogTableRowCopy(logLevel) {
		log.Printf("  [%s] done (%d rows copied)", table.SourceName, count)
	}
	return count, nil
}

// migrateChunkFromSource copies a single chunk using an existing source querier.
func migrateChunkFromSource(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, key ChunkKey, chunk Chunk, columnSelectList string, pgCopyColumns []string, logLevel string) (int64, error) {
	logChunkProgress(logLevel, table.SourceName, chunk.Index, "starting", 0)

	query := buildChunkedSelectQuery(src, table, key, chunk, columnSelectList)
	count, err := copyFromSource(ctx, source, pool, table, pgSchema, typeMap, src, query, pgCopyColumns, logLevel)
	if err != nil {
		return 0, err
	}

	logChunkProgress(logLevel, table.SourceName, chunk.Index, "done", count)
	return count, nil
}

type migrationProgressLogger struct {
	logLevel string
	mu       sync.Mutex
	tables   map[string]*migrationTableProgress
}

type migrationTableProgress struct {
	totalChunks    int
	finishedChunks int
	copiedRows     int64
	started        bool
	done           bool
}

func newMigrationProgressLogger(logLevel string, workItems []migrationWorkItem) *migrationProgressLogger {
	p := &migrationProgressLogger{
		logLevel: logLevel,
		tables:   make(map[string]*migrationTableProgress),
	}
	if !isTableMigrateLogLevel(logLevel) {
		return p
	}
	for _, item := range workItems {
		if item.ChunkKey == nil {
			continue
		}
		tableName := item.Table.SourceName
		if p.tables[tableName] == nil {
			p.tables[tableName] = &migrationTableProgress{}
		}
		p.tables[tableName].totalChunks++
	}
	return p
}

func (p *migrationProgressLogger) StartChunkedTable(tableName string) {
	if p == nil || !isTableMigrateLogLevel(p.logLevel) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	table := p.tables[tableName]
	if table == nil || table.started {
		return
	}
	table.started = true
	log.Printf("  [%s] starting row copy", tableName)
}

func (p *migrationProgressLogger) FinishChunk(tableName string, copiedRows int64) {
	if p == nil || !isTableMigrateLogLevel(p.logLevel) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	table := p.tables[tableName]
	if table == nil || table.done {
		return
	}
	table.finishedChunks++
	table.copiedRows += copiedRows
	if table.finishedChunks == table.totalChunks {
		table.done = true
		log.Printf("  [%s] done (%d rows copied)", tableName, table.copiedRows)
	}
}

func logChunkProgress(logLevel, tableName string, chunkIndex int, state string, rows int64) {
	if !isVerboseMigrateLogLevel(logLevel) {
		return
	}
	if state == "done" {
		log.Printf("  [%s] chunk %d done (%d rows)", tableName, chunkIndex, rows)
		return
	}
	log.Printf("  [%s] chunk %d %s", tableName, chunkIndex, state)
}

func isVerboseMigrateLogLevel(logLevel string) bool {
	return logLevel == "" || logLevel == migrateLogLevelVerbose
}

func isTableMigrateLogLevel(logLevel string) bool {
	return logLevel == migrateLogLevelTable
}

func shouldLogTableRowCopy(logLevel string) bool {
	return isVerboseMigrateLogLevel(logLevel) || isTableMigrateLogLevel(logLevel)
}

func shouldLogRowCopyProgress(logLevel string) bool {
	return isVerboseMigrateLogLevel(logLevel)
}

// tablePGCopyColumns returns PostgreSQL column names in table.Columns order for COPY.
func tablePGCopyColumns(table Table) []string {
	out := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		out[i] = col.PGName
	}
	return out
}

// copyFromSource runs a SELECT query on the source and streams results into PG via COPY.
func copyFromSource(ctx context.Context, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, src SourceDB, query string, pgColumns []string, logLevel string) (int64, error) {
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

	rs := newRowSource(rows, table, src, typeMap, logLevel)

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
// typeMap must be the same validated TypeMappingConfig used for the data migration so
// ColumnSelectList matches buildSourceSelectQuery / columnSelectExpr semantics.
func buildChunkPlans(ctx context.Context, src SourceDB, srcDSN string, schema *Schema, chunkSize int64, typeMap TypeMappingConfig, workers int, logLevel string) ([]ChunkPlan, error) {
	return buildChunkPlansWithDeps(
		ctx,
		src,
		schema,
		chunkSize,
		typeMap,
		chunkPlanningWorkers(workers, src),
		logLevel,
		chunkPlanningDeps{
			openSource: func() (migrationWorkerSource, error) {
				srcDB, err := openMigrationSourceDB(src, srcDSN)
				if err != nil {
					return nil, fmt.Errorf("open source for chunk planning: %w", err)
				}
				return srcDB, nil
			},
			queryMinMax: queryMinMax,
		},
	)
}

type chunkPlanningDeps struct {
	openSource  func() (migrationWorkerSource, error)
	queryMinMax func(context.Context, dbQuerier, SourceDB, Table, ChunkKey) (int64, int64, bool, error)
}

type chunkPlanningJob struct {
	tableIndex       int
	table            Table
	key              ChunkKey
	columnSelectList string
	pgCopyColumns    []string
}

func buildChunkPlansWithDeps(ctx context.Context, src SourceDB, schema *Schema, chunkSize int64, typeMap TypeMappingConfig, workers int, logLevel string, deps chunkPlanningDeps) ([]ChunkPlan, error) {
	plans := make([]ChunkPlan, len(schema.Tables))
	jobs := make([]chunkPlanningJob, 0, len(schema.Tables))
	nonChunkable := 0

	for i, t := range schema.Tables {
		pgCols := tablePGCopyColumns(t)
		key := chunkKeyForTable(t, src)
		if key == nil {
			plans[i] = ChunkPlan{Table: t, ChunkSize: chunkSize, PGCopyColumns: pgCols}
			nonChunkable++
			continue
		}

		jobs = append(jobs, chunkPlanningJob{
			tableIndex:       i,
			table:            t,
			key:              *key,
			columnSelectList: buildColumnSelectList(src, t, typeMap),
			pgCopyColumns:    pgCols,
		})
	}

	if len(jobs) == 0 {
		if nonChunkable > 0 {
			log.Printf("chunk plan: no tables with chunkable primary keys, using full-table copy for all %d table(s)", nonChunkable)
		}
		return plans, nil
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobCh := make(chan chunkPlanningJob, workers)
	var wg sync.WaitGroup

	var firstErr error
	var errOnce sync.Once
	var otherErrsMu sync.Mutex
	var otherErrs []error
	recordErr := func(err error) {
		isFirst := false
		errOnce.Do(func() {
			firstErr = err
			isFirst = true
			cancel()
		})
		if !isFirst {
			otherErrsMu.Lock()
			otherErrs = append(otherErrs, err)
			otherErrsMu.Unlock()
		}
	}

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()

			var source migrationWorkerSource
			defer func() {
				if source != nil {
					source.Close()
				}
			}()

			for {
				select {
				case <-ctx.Done():
					return
				case job, ok := <-jobCh:
					if !ok {
						return
					}

					if source == nil {
						var err error
						source, err = deps.openSource()
						if err != nil {
							recordErr(fmt.Errorf("open source for chunk planning on %s: %w", job.table.SourceName, err))
							return
						}
					}

					min, max, hasRows, err := deps.queryMinMax(ctx, source, src, job.table, job.key)
					if err != nil {
						if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
							return
						}
						recordErr(err)
						return
					}

					if !hasRows {
						plans[job.tableIndex] = ChunkPlan{
							Table:            job.table,
							ChunkKey:         &ChunkKey{SourceColumn: job.key.SourceColumn, PGColumn: job.key.PGColumn},
							Chunks:           []Chunk{{Index: 0, IsLast: true}},
							ChunkSize:        chunkSize,
							ColumnSelectList: job.columnSelectList,
							PGCopyColumns:    job.pgCopyColumns,
						}
						continue
					}

					chunks := planChunks(min, max, chunkSize)
					plans[job.tableIndex] = ChunkPlan{
						Table:            job.table,
						ChunkKey:         &ChunkKey{SourceColumn: job.key.SourceColumn, PGColumn: job.key.PGColumn},
						Chunks:           chunks,
						ChunkSize:        chunkSize,
						ColumnSelectList: job.columnSelectList,
						PGCopyColumns:    job.pgCopyColumns,
					}
					if isVerboseMigrateLogLevel(logLevel) {
						log.Printf("  [%s] %d chunks (key=%s, range=%d..%d)", job.table.SourceName, len(chunks), job.key.SourceColumn, min, max)
					}
				}
			}
		}()
	}

enqueue:
	for _, job := range jobs {
		select {
		case <-ctx.Done():
			break enqueue
		case jobCh <- job:
		}
	}
	close(jobCh)
	wg.Wait()

	if firstErr != nil {
		otherErrsMu.Lock()
		defer otherErrsMu.Unlock()
		return nil, errors.Join(append([]error{firstErr}, otherErrs...)...)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	totalChunks := 0
	for _, job := range jobs {
		totalChunks += len(plans[job.tableIndex].Chunks)
	}

	if len(jobs) > 0 {
		log.Printf("chunk plan: %d chunkable table(s) (%d total chunks), %d non-chunkable table(s)", len(jobs), totalChunks, nonChunkable)
	}
	if nonChunkable > 0 && len(jobs) == 0 {
		log.Printf("chunk plan: no tables with chunkable primary keys, using full-table copy for all %d table(s)", nonChunkable)
	}

	return plans, nil
}

// progressLogTimeSampleRows controls how often rowSource.Next samples wall time
// for the ~10s progress log. time.Now() is not called on every row.
const progressLogTimeSampleRows int64 = 1024

func shouldSampleProgressLogTime(copied int64) bool {
	return copied == 1 || copied%progressLogTimeSampleRows == 0
}

// rowSource implements pgx.CopyFromSource by reading from source rows.
type rowSource struct {
	rows         *sql.Rows
	table        Table
	scanDest     []any
	scanPtrs     []any
	transformers []valueTransformer
	values       []any
	err          error
	copied       int64
	tableName    string
	lastLog      time.Time
	logLevel     string
}

func newRowSource(rows *sql.Rows, table Table, src SourceDB, typeMap TypeMappingConfig, logLevel string) *rowSource {
	numCols := len(table.Columns)
	scanDest := make([]any, numCols)
	scanPtrs := make([]any, numCols)
	for i := range scanDest {
		scanPtrs[i] = &scanDest[i]
	}

	return &rowSource{
		rows:         rows,
		table:        table,
		scanDest:     scanDest,
		scanPtrs:     scanPtrs,
		transformers: buildRowValueTransformers(table, src, typeMap),
		values:       make([]any, numCols),
		tableName:    table.SourceName,
		lastLog:      time.Now(),
		logLevel:     logLevel,
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
		v, err := r.transformers[i](r.scanDest[i])
		if err != nil {
			r.err = fmt.Errorf("column %s: %w", col.SourceName, err)
			return false
		}
		r.values[i] = v
	}

	r.copied++
	if shouldLogRowCopyProgress(r.logLevel) && shouldSampleProgressLogTime(r.copied) {
		if now := time.Now(); now.Sub(r.lastLog) >= 10*time.Second {
			log.Printf("  [%s] progress: %d rows copied", r.tableName, r.copied)
			r.lastLog = now
		}
	}
	return true
}

func (r *rowSource) Values() ([]any, error) {
	return r.values, nil
}

func (r *rowSource) Err() error {
	return r.err
}

func buildSourceSelectQuery(src SourceDB, table Table, typeMap TypeMappingConfig) string {
	cols := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = columnSelectExpr(src, col, typeMap)
	}
	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(cols, ", "), src.SourceTableRef(table))
}

// columnSelectExpr returns the SQL expression for selecting a column.
// For most columns this is just the quoted name, but spatial columns in
// wkt_text mode use ST_AsText() to produce Well-Known Text output.
func columnSelectExpr(src SourceDB, col Column, typeMap TypeMappingConfig) string {
	quoted := src.QuoteIdentifier(col.SourceName)
	switch {
	case isMySQLFamilySource(src):
		if isMySQLSpatialType(col.DataType) && typeMap.UsePostGIS {
			return mysqlPostGISSelectExpr(src, quoted)
		}
		if isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkt_text" {
			return fmt.Sprintf("ST_AsText(%s) AS %s", quoted, quoted)
		}
	case sourceTypeForDB(src) == "mssql":
		switch {
		case col.DataType == "hierarchyid":
			return fmt.Sprintf("%s.ToString() AS %s", quoted, quoted)
		case isMSSQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkt_text":
			return fmt.Sprintf("%s.STAsText() AS %s", quoted, quoted)
		case isMSSQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkb_bytea":
			return fmt.Sprintf("%s.STAsBinary() AS %s", quoted, quoted)
		case col.DataType == "sql_variant":
			return fmt.Sprintf("TRY_CAST(%s AS nvarchar(max)) AS %s", quoted, quoted)
		case col.DataType == "money" || col.DataType == "smallmoney":
			// money is an exact 8-byte scaled integer spanning 19 significant digits,
			// but go-mssqldb decodes it to float64 (~15-17 digits), so magnitudes above
			// roughly 9e11 lose sub-cent precision before the value ever reaches the
			// transformer. Convert server-side so the exact decimal text is what travels.
			return fmt.Sprintf("CONVERT(varchar(41), %s) AS %s", quoted, quoted)
		}
	}
	return quoted
}

func mysqlPostGISSelectExpr(src SourceDB, quoted string) string {
	mysqlSrc := mysqlFamilyBaseSource(src)
	wkbExpr := fmt.Sprintf("ST_AsWKB(%s)", quoted)
	// Real MariaDB configs never reach this path because [postgis] is rejected
	// during config validation. Keep the nil-safe fallback for test doubles.
	if mysqlSrc != nil && mysqlSrc.supportsAxisOrderOption() {
		wkbExpr = fmt.Sprintf("ST_AsWKB(%s, 'axis-order=long-lat')", quoted)
	}
	sridExpr := fmt.Sprintf("ST_SRID(%s)", quoted)
	return fmt.Sprintf(
		"CONCAT(CHAR((%[1]s) & 255 USING binary), CHAR(((%[1]s) >> 8) & 255 USING binary), CHAR(((%[1]s) >> 16) & 255 USING binary), CHAR(((%[1]s) >> 24) & 255 USING binary), %[2]s) AS %[3]s",
		sridExpr, wkbExpr, quoted,
	)
}
