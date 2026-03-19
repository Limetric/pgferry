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
	plans, err := buildChunkPlans(ctx, cfg.Src, cfg.SrcDSN, cfg.Schema, cfg.ChunkSize, cfg.TypeMap)
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
				return migrateTableFromSourceFull(ctx, cfg.Src, source, cfg.Pool, item.Table, cfg.PGSchema, cfg.TypeMap, item.PGCopyColumns)
			}
			return migrateChunkFromSource(ctx, cfg.Src, source, cfg.Pool, item.Table, cfg.PGSchema, cfg.TypeMap, *item.ChunkKey, item.Chunk, item.ColumnSelectList, item.PGCopyColumns)
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

					if source == nil {
						var err error
						source, err = openSource()
						if err != nil {
							recordErr(fmt.Errorf("open source worker: %w", err))
							return
						}
					}

					count, err := execute(ctx, source, item)
					if err != nil {
						if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
							return
						}
						recordErr(formatMigrationWorkError(item, err))
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
		if _, err := srcDB.ExecContext(ctx, "SET TRANSACTION ISOLATION LEVEL SNAPSHOT"); err != nil {
			return fmt.Errorf("set source transaction isolation (hint: ensure ALTER DATABASE ... SET ALLOW_SNAPSHOT_ISOLATION ON): %w", err)
		}
		tx, err = srcDB.BeginTx(ctx, &sql.TxOptions{
			Isolation: sql.LevelSnapshot,
			ReadOnly:  true,
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
			count, copyErr := migrateTableFromSourceFull(ctx, cfg.Src, tx, cfg.Pool, t, cfg.PGSchema, cfg.TypeMap, tablePGCopyColumns(t))
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
		log.Printf("  [%s] %d chunks (key=%s, range=%d..%d)", t.SourceName, len(chunks), key.SourceColumn, min, max)
		for _, chunk := range chunks {
			if mgr.IsChunkCompleted(t.SourceName, chunk.Index) {
				continue
			}
			count, copyErr := migrateChunkFromSource(ctx, cfg.Src, tx, cfg.Pool, t, cfg.PGSchema, cfg.TypeMap, *key, chunk, colSelectList, pgCols)
			if copyErr != nil {
				return fmt.Errorf("table %s chunk %d: %w", t.SourceName, chunk.Index, copyErr)
			}
			mgr.RecordChunk(t.SourceName, chunk.Index, count, len(chunks))
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

func openMigrationSourceDB(src SourceDB, srcDSN string) (*sql.DB, error) {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return nil, err
	}
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)
	return srcDB, nil
}

type dbQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func migrateTableFromSourceFull(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, pgCopyColumns []string) (int64, error) {
	log.Printf("  [%s] starting row copy", table.SourceName)

	query := buildSourceSelectQuery(src, table, typeMap)
	count, err := copyFromSource(ctx, source, pool, table, pgSchema, typeMap, src, query, pgCopyColumns)
	if err != nil {
		return 0, err
	}

	log.Printf("  [%s] done (%d rows copied)", table.SourceName, count)
	return count, nil
}

// migrateChunkFromSource copies a single chunk using an existing source querier.
func migrateChunkFromSource(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, key ChunkKey, chunk Chunk, columnSelectList string, pgCopyColumns []string) (int64, error) {
	log.Printf("  [%s] chunk %d starting", table.SourceName, chunk.Index)

	query := buildChunkedSelectQuery(src, table, key, chunk, columnSelectList)
	count, err := copyFromSource(ctx, source, pool, table, pgSchema, typeMap, src, query, pgCopyColumns)
	if err != nil {
		return 0, err
	}

	log.Printf("  [%s] chunk %d done (%d rows)", table.SourceName, chunk.Index, count)
	return count, nil
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
func copyFromSource(ctx context.Context, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig, src SourceDB, query string, pgColumns []string) (int64, error) {
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
func buildChunkPlans(ctx context.Context, src SourceDB, srcDSN string, schema *Schema, chunkSize int64, typeMap TypeMappingConfig) ([]ChunkPlan, error) {
	srcDB, err := openMigrationSourceDB(src, srcDSN)
	if err != nil {
		return nil, fmt.Errorf("open source for chunk planning: %w", err)
	}
	defer srcDB.Close()

	var plans []ChunkPlan
	var chunkable, nonChunkable int
	totalChunks := 0

	for _, t := range schema.Tables {
		key := chunkKeyForTable(t, src)
		if key == nil {
			nonChunkable++
			plans = append(plans, ChunkPlan{Table: t, ChunkSize: chunkSize, PGCopyColumns: tablePGCopyColumns(t)})
			continue
		}

		min, max, hasRows, err := queryMinMax(ctx, srcDB, src, t, *key)
		if err != nil {
			return nil, err
		}
		if !hasRows {
			// Empty table — single empty plan
			plans = append(plans, ChunkPlan{
				Table:            t,
				ChunkKey:         key,
				Chunks:           []Chunk{{Index: 0, IsLast: true}},
				ChunkSize:        chunkSize,
				ColumnSelectList: buildColumnSelectList(src, t, typeMap),
				PGCopyColumns:    tablePGCopyColumns(t),
			})
			chunkable++
			totalChunks++
			continue
		}

		chunks := planChunks(min, max, chunkSize)
		plans = append(plans, ChunkPlan{
			Table:            t,
			ChunkKey:         key,
			Chunks:           chunks,
			ChunkSize:        chunkSize,
			ColumnSelectList: buildColumnSelectList(src, t, typeMap),
			PGCopyColumns:    tablePGCopyColumns(t),
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

// progressLogTimeSampleRows controls how often rowSource.Next samples wall time
// for the ~10s progress log. time.Now() is not called on every row.
const progressLogTimeSampleRows int64 = 1024

func shouldSampleProgressLogTime(copied int64) bool {
	return copied == 1 || copied%progressLogTimeSampleRows == 0
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
	if shouldSampleProgressLogTime(r.copied) {
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
			return fmt.Sprintf("CAST(%s AS nvarchar(max)) AS %s", quoted, quoted)
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
