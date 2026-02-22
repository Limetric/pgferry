package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateData streams data from the source to PostgreSQL for all tables using parallel workers.
func migrateData(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, workers int, typeMap TypeMappingConfig, sourceSnapshotMode string) error {
	switch sourceSnapshotMode {
	case "single_tx":
		return migrateDataSingleTx(ctx, src, srcDSN, pool, schema, pgSchema, typeMap)
	default:
		return migrateDataParallel(ctx, src, srcDSN, pool, schema, pgSchema, workers, typeMap)
	}
}

func migrateDataParallel(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, workers int, typeMap TypeMappingConfig) error {
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	errCh := make(chan error, len(schema.Tables))

	for _, t := range schema.Tables {
		wg.Add(1)
		go func(t Table) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := migrateTable(ctx, src, srcDSN, pool, t, pgSchema, typeMap); err != nil {
				errCh <- fmt.Errorf("table %s: %w", t.SourceName, err)
			}
		}(t)
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
		return fmt.Errorf("%d table(s) failed migration", len(errs))
	}
	return nil
}

func migrateDataSingleTx(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, typeMap TypeMappingConfig) error {
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

	log.Printf("source snapshot enabled: single_tx (sequential table copy)")
	for _, t := range schema.Tables {
		if err := migrateTableFromSource(ctx, src, tx, pool, t, pgSchema, typeMap); err != nil {
			return fmt.Errorf("table %s: %w", t.SourceName, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source transaction: %w", err)
	}
	return nil
}

// migrateTable streams one table from source to PG via COPY protocol.
func migrateTable(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig) error {
	// Own source connection (short-lived)
	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return err
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)
	srcDB.SetMaxIdleConns(1)

	return migrateTableFromSource(ctx, src, srcDB, pool, table, pgSchema, typeMap)
}

type dbQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func migrateTableFromSource(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig) error {
	// Count rows for progress
	var totalRows int64
	quotedTable := src.QuoteIdentifier(table.SourceName)
	err := source.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", quotedTable)).Scan(&totalRows)
	if err != nil {
		return fmt.Errorf("count rows: %w", err)
	}
	log.Printf("  [%s] %d rows to migrate", table.SourceName, totalRows)

	if totalRows == 0 {
		log.Printf("  [%s] done (empty)", table.SourceName)
		return nil
	}

	// Build PG column names
	pgColumns := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		pgColumns[i] = col.PGName
	}

	// Acquire PG connection for COPY
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire pg conn: %w", err)
	}
	defer conn.Release()

	// Stream source rows via COPY protocol
	rows, err := source.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s", quotedTable))
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	rs := &rowSource{
		rows:        rows,
		table:       table,
		copied:      new(atomic.Int64),
		total:       totalRows,
		src:         src,
		typeMapping: typeMap,
		tableName:   table.SourceName,
		lastLog:     time.Now(),
	}

	count, err := conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{pgSchema, table.PGName},
		pgColumns,
		rs,
	)
	if err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	log.Printf("  [%s] done (%d rows copied)", table.SourceName, count)
	return nil
}

// rowSource implements pgx.CopyFromSource by reading from source rows.
type rowSource struct {
	rows        *sql.Rows
	table       Table
	values      []any
	err         error
	copied      *atomic.Int64
	total       int64
	src         SourceDB
	typeMapping TypeMappingConfig
	tableName   string
	lastLog     time.Time
}

func (r *rowSource) Next() bool {
	if !r.rows.Next() {
		r.err = r.rows.Err()
		return false
	}

	// Create scan destinations
	numCols := len(r.table.Columns)
	dest := make([]any, numCols)
	ptrs := make([]any, numCols)
	for i := range dest {
		ptrs[i] = &dest[i]
	}

	if err := r.rows.Scan(ptrs...); err != nil {
		r.err = err
		return false
	}

	// Transform values
	r.values = make([]any, numCols)
	for i, col := range r.table.Columns {
		v, err := r.src.TransformValue(dest[i], col, r.typeMapping)
		if err != nil {
			r.err = fmt.Errorf("column %s: %w", col.SourceName, err)
			return false
		}
		r.values[i] = v
	}

	n := r.copied.Add(1)
	if now := time.Now(); now.Sub(r.lastLog) >= 10*time.Second {
		pct := float64(n) / float64(r.total) * 100
		log.Printf("  [%s] progress: %d/%d rows (%.1f%%)", r.tableName, n, r.total, pct)
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
