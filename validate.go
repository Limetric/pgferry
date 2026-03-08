package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidationResult holds the outcome of a single table validation.
type ValidationResult struct {
	Table       string
	SourceCount int64
	TargetCount int64
	CountMatch  bool
}

// validateMigration runs post-load validation comparing source and target row counts.
func validateMigration(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, mode string) ([]ValidationResult, error) {
	if mode == "none" || mode == "" {
		return nil, nil
	}

	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return nil, fmt.Errorf("open source for validation: %w", err)
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(1)

	var results []ValidationResult
	var mismatches int

	for _, t := range schema.Tables {
		result := ValidationResult{Table: t.SourceName}

		// Count source rows
		srcQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", src.QuoteIdentifier(t.SourceName))
		srcRows, err := srcDB.QueryContext(ctx, srcQuery)
		if err != nil {
			return nil, fmt.Errorf("count source rows for %s: %w", t.SourceName, err)
		}
		if srcRows.Next() {
			if err := srcRows.Scan(&result.SourceCount); err != nil {
				srcRows.Close()
				return nil, fmt.Errorf("scan source count for %s: %w", t.SourceName, err)
			}
		}
		srcRows.Close()

		// Count target rows
		pgQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", pgIdent(pgSchema), pgIdent(t.PGName))
		if err := pool.QueryRow(ctx, pgQuery).Scan(&result.TargetCount); err != nil {
			return nil, fmt.Errorf("count target rows for %s: %w", t.PGName, err)
		}

		result.CountMatch = result.SourceCount == result.TargetCount
		if !result.CountMatch {
			mismatches++
			log.Printf("  MISMATCH: %s — source=%d target=%d", t.SourceName, result.SourceCount, result.TargetCount)
		} else {
			log.Printf("  OK: %s — %d rows", t.SourceName, result.SourceCount)
		}
		results = append(results, result)
	}

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
