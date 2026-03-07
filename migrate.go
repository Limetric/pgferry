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
}

func migrateTableFromSource(ctx context.Context, src SourceDB, source dbQuerier, pool *pgxpool.Pool, table Table, pgSchema string, typeMap TypeMappingConfig) error {
	log.Printf("  [%s] starting row copy", table.SourceName)

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
	rows, err := source.QueryContext(ctx, buildSourceSelectQuery(src, table))
	if err != nil {
		return fmt.Errorf("select: %w", err)
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
		return fmt.Errorf("copy: %w", err)
	}

	log.Printf("  [%s] done (%d rows copied)", table.SourceName, count)
	return nil
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
