package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateData streams data from MySQL to PostgreSQL for all tables using parallel workers.
func migrateData(ctx context.Context, mysqlDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, workers int) error {
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	errCh := make(chan error, len(schema.Tables))

	fullDSN, err := mysqlDSNWithReadOptions(mysqlDSN)
	if err != nil {
		return err
	}

	for _, t := range schema.Tables {
		wg.Add(1)
		go func(t Table) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := migrateTable(ctx, fullDSN, pool, t, pgSchema); err != nil {
				errCh <- fmt.Errorf("table %s: %w", t.MySQLName, err)
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

// migrateTable streams one table from MySQL to PG via COPY protocol.
func migrateTable(ctx context.Context, mysqlDSN string, pool *pgxpool.Pool, table Table, pgSchema string) error {
	// Own MySQL connection (short-lived)
	mysqlConn, err := sql.Open("mysql", mysqlDSN)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer mysqlConn.Close()
	mysqlConn.SetMaxOpenConns(1)

	// Count rows for progress
	var totalRows int64
	err = mysqlConn.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM `%s`", table.MySQLName)).Scan(&totalRows)
	if err != nil {
		return fmt.Errorf("count rows: %w", err)
	}
	log.Printf("  [%s] %d rows to migrate", table.MySQLName, totalRows)

	if totalRows == 0 {
		log.Printf("  [%s] done (empty)", table.MySQLName)
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

	// Stream MySQL rows via COPY protocol
	rows, err := mysqlConn.QueryContext(ctx, fmt.Sprintf("SELECT * FROM `%s`", table.MySQLName))
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	src := &rowSource{
		rows:   rows,
		table:  table,
		copied: new(atomic.Int64),
		total:  totalRows,
	}

	count, err := conn.Conn().CopyFrom(
		ctx,
		pgx.Identifier{pgSchema, table.PGName},
		pgColumns,
		src,
	)
	if err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	log.Printf("  [%s] done (%d rows copied)", table.MySQLName, count)
	return nil
}

// rowSource implements pgx.CopyFromSource by reading from MySQL rows.
type rowSource struct {
	rows   *sql.Rows
	table  Table
	values []any
	err    error
	copied *atomic.Int64
	total  int64
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
		r.values[i] = transformValue(dest[i], col)
	}

	r.copied.Add(1)
	return true
}

func (r *rowSource) Values() ([]any, error) {
	return r.values, nil
}

func (r *rowSource) Err() error {
	return r.err
}
