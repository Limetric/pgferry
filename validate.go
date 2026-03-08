package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidationResult holds the outcome of a single table validation.
type ValidationResult struct {
	Table       string
	SourceCount int64
	TargetCount int64
	CountMatch  bool
}

// validationWorkers returns the effective worker count for validation,
// capping based on source backend limits (e.g., SQLite requires a single worker).
func validationWorkers(workers int, src SourceDB) int {
	if max := src.MaxWorkers(); max > 0 && workers > max {
		workers = max
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

// validateMigration runs post-load validation comparing source and target row counts.
// Tables are validated in parallel with bounded concurrency. The workers parameter
// controls maximum parallelism and is capped by source backend limits (e.g., SQLite
// is always single-threaded).
func validateMigration(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, mode string, workers int) ([]ValidationResult, error) {
	if mode == "none" || mode == "" {
		return nil, nil
	}

	workers = validationWorkers(workers, src)

	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return nil, fmt.Errorf("open source for validation: %w", err)
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(workers)

	start := time.Now()
	results := make([]ValidationResult, len(schema.Tables))

	// Use a cancellable context so a failure in one goroutine stops the rest.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	setErr := func(err error) {
		errOnce.Do(func() { firstErr = err })
		cancel()
	}

	for i, t := range schema.Tables {
		wg.Add(1)
		go func(idx int, tbl Table) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			result := ValidationResult{Table: tbl.SourceName}

			// Count source rows
			srcQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", src.QuoteIdentifier(tbl.SourceName))
			if err := srcDB.QueryRowContext(ctx, srcQuery).Scan(&result.SourceCount); err != nil {
				setErr(fmt.Errorf("count source rows for %s: %w", tbl.SourceName, err))
				return
			}

			// Count target rows
			pgQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", pgIdent(pgSchema), pgIdent(tbl.PGName))
			if err := pool.QueryRow(ctx, pgQuery).Scan(&result.TargetCount); err != nil {
				setErr(fmt.Errorf("count target rows for %s: %w", tbl.PGName, err))
				return
			}

			result.CountMatch = result.SourceCount == result.TargetCount
			results[idx] = result
		}(i, t)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	// Report results deterministically (in original table order)
	var mismatches int
	for _, r := range results {
		if !r.CountMatch {
			mismatches++
			log.Printf("  MISMATCH: %s — source=%d target=%d", r.Table, r.SourceCount, r.TargetCount)
		} else {
			log.Printf("  OK: %s — %d rows", r.Table, r.SourceCount)
		}
	}

	log.Printf("  validated %d table(s) in %s (workers=%d)", len(schema.Tables), time.Since(start).Round(time.Millisecond), workers)

	if mismatches > 0 {
		var names []string
		for _, r := range results {
			if !r.CountMatch {
				names = append(names, r.Table)
			}
		}
		return results, fmt.Errorf("validation failed: row count mismatch on %d table(s): %s",
			mismatches, strings.Join(names, ", "))
	}
	return results, nil
}
