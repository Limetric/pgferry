package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// buildUpsertSQL builds an INSERT ... ON CONFLICT DO UPDATE for the given table.
func buildUpsertSQL(pgSchema string, table Table) string {
	cols := make([]string, len(table.Columns))
	params := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = pgIdent(col.PGName)
		params[i] = fmt.Sprintf("$%d", i+1)
	}

	pkSet := make(map[string]bool, len(table.PrimaryKey.Columns))
	for _, pk := range table.PrimaryKey.Columns {
		pkSet[pk] = true
	}

	pkCols := make([]string, len(table.PrimaryKey.Columns))
	for i, pk := range table.PrimaryKey.Columns {
		pkCols[i] = pgIdent(pk)
	}

	var setClauses []string
	for _, col := range table.Columns {
		if pkSet[col.PGName] {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = EXCLUDED.%s", pgIdent(col.PGName), pgIdent(col.PGName)))
	}

	return fmt.Sprintf(
		"INSERT INTO %s.%s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		pgIdent(pgSchema),
		pgIdent(table.PGName),
		strings.Join(cols, ", "),
		strings.Join(params, ", "),
		strings.Join(pkCols, ", "),
		strings.Join(setClauses, ", "),
	)
}

// buildDeleteSQL builds a DELETE WHERE pk = $1 [AND pk2 = $2 ...].
func buildDeleteSQL(pgSchema string, table Table) string {
	var whereClauses []string
	for i, pk := range table.PrimaryKey.Columns {
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", pgIdent(pk), i+1))
	}
	return fmt.Sprintf(
		"DELETE FROM %s.%s WHERE %s",
		pgIdent(pgSchema),
		pgIdent(table.PGName),
		strings.Join(whereClauses, " AND "),
	)
}

// pkColumnPositions returns the ordinal positions (0-based) of PK columns within table.Columns.
func pkColumnPositions(table Table) []int {
	pkSet := make(map[string]bool, len(table.PrimaryKey.Columns))
	for _, pk := range table.PrimaryKey.Columns {
		pkSet[pk] = true
	}
	var positions []int
	for i, col := range table.Columns {
		if pkSet[col.PGName] {
			positions = append(positions, i)
		}
	}
	return positions
}

// extractPKValues pulls the PK column values from a full row.
func extractPKValues(row []any, pkPositions []int) []any {
	vals := make([]any, len(pkPositions))
	for i, pos := range pkPositions {
		vals[i] = row[pos]
	}
	return vals
}

// CDCApplier applies batches of CDC events to PostgreSQL with co-transactional checkpoint updates.
type CDCApplier struct {
	pool     *pgxpool.Pool
	pgSchema string
	tables   map[string]Table
	src      SourceDB
	typeMap  TypeMappingConfig

	upsertCache map[string]string
	deleteCache map[string]string
	pkPosCache  map[string][]int

	applied atomic.Int64
	skipped atomic.Int64
}

func NewCDCApplier(pool *pgxpool.Pool, pgSchema string, tables map[string]Table, src SourceDB, typeMap TypeMappingConfig) *CDCApplier {
	upsertCache := make(map[string]string, len(tables))
	deleteCache := make(map[string]string, len(tables))
	pkPosCache := make(map[string][]int, len(tables))
	for name, table := range tables {
		if table.PrimaryKey == nil || len(table.PrimaryKey.Columns) == 0 {
			continue
		}
		upsertCache[name] = buildUpsertSQL(pgSchema, table)
		deleteCache[name] = buildDeleteSQL(pgSchema, table)
		pkPosCache[name] = pkColumnPositions(table)
	}
	return &CDCApplier{
		pool:        pool,
		pgSchema:    pgSchema,
		tables:      tables,
		src:         src,
		typeMap:     typeMap,
		upsertCache: upsertCache,
		deleteCache: deleteCache,
		pkPosCache:  pkPosCache,
	}
}

func (a *CDCApplier) ApplyBatch(ctx context.Context, events []*CDCEvent, pos CDCPosition) error {
	const maxRetries = 3

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			sleep := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			time.Sleep(sleep)
		}
		lastErr = a.applyBatchOnce(ctx, events, pos)
		if lastErr == nil {
			return nil
		}
		log.Printf("[replicate] WARN: batch apply attempt %d failed: %v", attempt+1, lastErr)
	}
	return fmt.Errorf("batch apply failed after %d retries: %w", maxRetries, lastErr)
}

func (a *CDCApplier) applyBatchOnce(ctx context.Context, events []*CDCEvent, pos CDCPosition) error {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batchApplied := int64(0)
	batchSkipped := int64(0)

	for _, ev := range events {
		upsertSQL, hasUpsert := a.upsertCache[ev.Table]
		deleteSQL, hasDelete := a.deleteCache[ev.Table]
		pkPositions := a.pkPosCache[ev.Table]

		if !hasUpsert || !hasDelete {
			continue
		}

		for _, row := range ev.Rows {
			var execErr error
			switch ev.Operation {
			case CDCInsert, CDCUpdate:
				_, execErr = tx.Exec(ctx, upsertSQL, row...)
			case CDCDelete:
				pkVals := extractPKValues(row, pkPositions)
				_, execErr = tx.Exec(ctx, deleteSQL, pkVals...)
			}

			if execErr != nil {
				log.Printf("[replicate] WARN: skip event table=%s op=%s err=%v", ev.Table, ev.Operation, execErr)
				batchSkipped++
				continue
			}
			batchApplied++
		}
	}

	totalApplied := a.applied.Load() + batchApplied
	totalSkipped := a.skipped.Load() + batchSkipped
	if err := updateCDCCheckpointTx(ctx, tx, a.pgSchema, pos, totalApplied, totalSkipped); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit batch: %w", err)
	}

	a.applied.Add(batchApplied)
	a.skipped.Add(batchSkipped)
	return nil
}

func (a *CDCApplier) Stats() (applied, skipped int64) {
	return a.applied.Load(), a.skipped.Load()
}
