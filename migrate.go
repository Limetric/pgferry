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
	Src                 SourceDB
	SrcDSN              string
	Pool                *pgxpool.Pool
	Schema              *Schema
	PGSchema            string
	Workers             int
	TypeMap             TypeMappingConfig
	SourceSnapshotMode  string
	ChunkSize           int64
	Resume              bool
	ConfigDir           string
	ResumeCompatibility checkpointCompatibility
}

// migrateData streams data from the source to PostgreSQL for all tables using parallel workers.
func migrateData(ctx context.Context, cfg migrateDataConfig) error {
	switch cfg.SourceSnapshotMode {
	case "single_tx":
		return migrateDataSingleTx(ctx, cfg.Src, cfg.SrcDSN, cfg.Pool, cfg.Schema, cfg.PGSchema, cfg.TypeMap, cfg.ChunkSize, cfg.Resume, cfg.ConfigDir, cfg.ResumeCompatibility)
	default:
		return migrateDataParallel(ctx, cfg.Src, cfg.SrcDSN, cfg.Pool, cfg.Schema, cfg.PGSchema, cfg.Workers, cfg.TypeMap, cfg.ChunkSize, cfg.Resume, cfg.ConfigDir, cfg.ResumeCompatibility)
	}
}

func migrateDataParallel(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, workers int, typeMap TypeMappingConfig, chunkSize int64, resume bool, configDir string, compat checkpointCompatibility) error {
	// Plan chunks for each table
	plans, err := buildChunkPlans(ctx, src, srcDSN, schema, chunkSize)
	if err != nil {
		return err
	}

	// Create checkpoint manager: noop when resume is disabled to avoid
	// all checkpoint file I/O in the hot path.
	cpPath := checkpointPath(configDir)
	var mgr checkpointManager
	if resume {
		pm, mgrErr := newPersistentCheckpointManager(cpPath, &compat)
		if mgrErr != nil {
			return fmt.Errorf("load checkpoint: %w", mgrErr)
		}
		mgr = pm
	} else {
		mgr = &noopCheckpointManager{path: cpPath}
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
			if mgr.IsTableDone(plan.Table.SourceName) {
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
				mgr.RecordFullTable(t.SourceName, count)
			}(plan.Table)
		} else {
			// Chunkable: dispatch each chunk
			for _, chunk := range plan.Chunks {
				if mgr.IsChunkCompleted(plan.Table.SourceName, chunk.Index) {
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
					mgr.RecordChunk(t.SourceName, c.Index, count, chunkCount)
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
		// Flush partial progress so a resumed run can skip completed work.
		if flushErr := mgr.Flush(); flushErr != nil {
			log.Printf("WARN: failed to save checkpoint: %v", flushErr)
		}
		for _, e := range errs {
			log.Printf("ERROR: %v", e)
		}
		return fmt.Errorf("%d chunk(s)/table(s) failed migration", len(errs))
	}

	// All succeeded — remove checkpoint file (no flush needed; there is
	// nothing to resume and any batched state can be discarded).
	if err := mgr.Cleanup(); err != nil {
		log.Printf("WARN: failed to delete checkpoint: %v", err)
	}
	return nil
}

func migrateDataSingleTx(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, typeMap TypeMappingConfig, chunkSize int64, resume bool, configDir string, compat checkpointCompatibility) error {
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	var tx *sql.Tx
	switch src.Name() {
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
	cpPath := checkpointPath(configDir)
	var mgr checkpointManager
	if resume {
		pm, mgrErr := newPersistentCheckpointManager(cpPath, &compat)
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
	for _, t := range schema.Tables {
		key := chunkKeyForTable(t, src)
		if key == nil {
			// Not chunkable — full-table copy
			if mgr.IsTableDone(t.SourceName) {
				log.Printf("  [%s] skipping (completed in previous run)", t.SourceName)
				continue
			}
			count, copyErr := migrateTableFromSourceFull(ctx, src, tx, pool, t, pgSchema, typeMap)
			if copyErr != nil {
				return fmt.Errorf("table %s: %w", t.SourceName, copyErr)
			}
			mgr.RecordFullTable(t.SourceName, count)
			continue
		}

		// Chunkable — run chunks sequentially within the transaction
		min, max, hasRows, mmErr := queryMinMax(ctx, tx, src, t, *key)
		if mmErr != nil {
			return mmErr
		}
		if !hasRows {
			log.Printf("  [%s] empty table, skipping", t.SourceName)
			mgr.RecordFullTable(t.SourceName, 0)
			continue
		}

		chunks := planChunks(min, max, chunkSize)
		log.Printf("  [%s] %d chunks (key=%s, range=%d..%d)", t.SourceName, len(chunks), key.SourceColumn, min, max)
		for _, chunk := range chunks {
			if mgr.IsChunkCompleted(t.SourceName, chunk.Index) {
				continue
			}
			count, copyErr := migrateChunkFromSource(ctx, src, tx, pool, t, pgSchema, typeMap, *key, chunk)
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

	query := buildSourceSelectQuery(src, table, typeMap)
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

	query := buildChunkedSelectQuery(src, table, key, chunk, typeMap)
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
				Table:     t,
				ChunkKey:  key,
				Chunks:    []Chunk{{Index: 0, IsLast: true}},
				ChunkSize: chunkSize,
			})
			chunkable++
			totalChunks++
			continue
		}

		chunks := planChunks(min, max, chunkSize)
		plans = append(plans, ChunkPlan{
			Table:     t,
			ChunkKey:  key,
			Chunks:    chunks,
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
	switch src.Name() {
	case "MySQL":
		if isMySQLSpatialType(col.DataType) && typeMap.UsePostGIS {
			return mysqlPostGISSelectExpr(src, quoted)
		}
		if isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkt_text" {
			return fmt.Sprintf("ST_AsText(%s) AS %s", quoted, quoted)
		}
	case "MSSQL":
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
	mysqlSrc := src.(*mysqlSourceDB)
	wkbExpr := fmt.Sprintf("ST_AsWKB(%s)", quoted)
	if mysqlSrc.supportsAxisOrderOption() {
		wkbExpr = fmt.Sprintf("ST_AsWKB(%s, 'axis-order=long-lat')", quoted)
	}
	sridExpr := fmt.Sprintf("ST_SRID(%s)", quoted)
	return fmt.Sprintf(
		"CONCAT(CHAR((%[1]s) & 255 USING binary), CHAR(((%[1]s) >> 8) & 255 USING binary), CHAR(((%[1]s) >> 16) & 255 USING binary), CHAR(((%[1]s) >> 24) & 255 USING binary), %[2]s) AS %[3]s",
		sridExpr, wkbExpr, quoted,
	)
}
