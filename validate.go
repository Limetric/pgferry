package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	validationModeNone        = "none"
	validationModeRowCount    = "row_count"
	validationModeSampledHash = "sampled_hash"

	// validationSampleRows keeps sampled_hash bounded so validation stays
	// meaningfully stronger than row_count without becoming a second full scan.
	validationSampleRows = 16
)

const (
	validationKindBool        = "bool"
	validationKindText        = "text"
	validationKindNumericText = "numeric_text"
	validationKindJSON        = "json"
	validationKindBytea       = "bytea"
	validationKindTextArray   = "text_array"
	validationKindDate        = "date"
	validationKindTimestamp   = "timestamp"
	validationKindTimestamptz = "timestamptz"
	validationKindTime        = "time"
)

// ValidationResult holds the outcome of a single table validation.
type ValidationResult struct {
	Table                string
	SourceCount          int64
	TargetCount          int64
	CountMatch           bool
	SampleRowsChecked    int
	SampleColumnsChecked int
	SampleMatch          bool
	SampleSkippedReason  string
	SampleMismatch       string
}

type validationColumn struct {
	Column Column
	Kind   string
	PGType string
}

type validationKey struct {
	RawValues        []any
	TargetFragments  []string
	DisplayFragments []string
}

type sampledHashBackend interface {
	sourceCount(context.Context, Table) (int64, error)
	targetCount(context.Context, Table) (int64, error)
	sampleKeys(context.Context, Table, []validationColumn, []int64) ([]validationKey, error)
	sourceFragments(context.Context, Table, []validationColumn, validationKey) ([]string, error)
	targetFragments(context.Context, Table, []validationColumn, []validationColumn, validationKey) ([]string, error)
}

type liveValidationBackend struct {
	src      SourceDB
	srcDB    *sql.DB
	pool     *pgxpool.Pool
	pgSchema string
	typeMap  TypeMappingConfig
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

func validationModeSummary(mode string) string {
	switch mode {
	case validationModeRowCount:
		return "row_count verifies only per-table row cardinality"
	case validationModeSampledHash:
		return fmt.Sprintf("sampled_hash verifies row counts plus bounded content fingerprints for up to %d deterministic primary-key-addressable rows per table", validationSampleRows)
	default:
		return "validation disabled"
	}
}

func validationCaveats(mode, sourceSnapshotMode string) []string {
	if mode == validationModeNone || mode == "" {
		return nil
	}

	caveats := []string{
		"validation runs after after_data hooks, so hook-driven target changes are included in what gets checked",
		"validation re-reads the source after COPY; if the source changed during or after the load, the result may compare against a newer source state than the copied data",
	}
	if sourceSnapshotMode == "single_tx" {
		caveats = append(caveats, "source_snapshot_mode=single_tx applies only to the COPY phase; validation does not read from the earlier snapshot")
	}
	if mode == validationModeRowCount {
		caveats = append(caveats, "row_count does not compare row contents, transformed values, or per-row semantics")
	} else if mode == validationModeSampledHash {
		caveats = append(caveats, "sampled_hash is intentionally bounded: it samples deterministic rows and only compares columns with supported canonical fingerprints")
	}
	return caveats
}

func logValidationPlan(mode, sourceSnapshotMode string) {
	if mode == validationModeNone || mode == "" {
		return
	}
	log.Printf("validation mode: %s", validationModeSummary(mode))
	for _, caveat := range validationCaveats(mode, sourceSnapshotMode) {
		log.Printf("validation caveat: %s", caveat)
	}
}

func buildSourceCountQuery(src SourceDB, table Table) string {
	return fmt.Sprintf("SELECT COUNT(*) FROM %s", src.SourceTableRef(table))
}

func (b *liveValidationBackend) sourceCount(ctx context.Context, table Table) (int64, error) {
	var count int64
	if err := b.srcDB.QueryRowContext(ctx, buildSourceCountQuery(b.src, table)).Scan(&count); err != nil {
		return 0, fmt.Errorf("count source rows for %s: %w", table.SourceName, err)
	}
	return count, nil
}

func (b *liveValidationBackend) targetCount(ctx context.Context, table Table) (int64, error) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", pgIdent(b.pgSchema), pgIdent(table.PGName))
	if err := b.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("count target rows for %s: %w", table.PGName, err)
	}
	return count, nil
}

func (b *liveValidationBackend) sampleKeys(ctx context.Context, table Table, keyCols []validationColumn, offsets []int64) ([]validationKey, error) {
	keys := make([]validationKey, 0, len(offsets))
	for _, offset := range offsets {
		query := buildSourceSampleKeyQuery(b.src, table, keyCols, offset)
		rawValues, err := querySQLRowValues(ctx, b.srcDB, query)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("sample key at offset %d not found", offset)
			}
			return nil, fmt.Errorf("read sample key at offset %d: %w", offset, err)
		}
		if len(rawValues) != len(keyCols) {
			return nil, fmt.Errorf("sample key at offset %d returned %d value(s), want %d", offset, len(rawValues), len(keyCols))
		}

		targetFragments := make([]string, len(keyCols))
		displayFragments := make([]string, len(keyCols))
		for i, keyCol := range keyCols {
			transformed, err := b.src.TransformValue(rawValues[i], keyCol.Column, b.typeMap)
			if err != nil {
				return nil, fmt.Errorf("transform sample key %s at offset %d: %w", keyCol.Column.SourceName, offset, err)
			}
			targetFragments[i], err = canonicalizeValidationFragment(transformed, keyCol.Kind)
			if err != nil {
				return nil, fmt.Errorf("canonicalize sample key %s at offset %d: %w", keyCol.Column.SourceName, offset, err)
			}
			displayFragments[i], err = renderValidationDisplay(transformed, keyCol.Kind)
			if err != nil {
				return nil, fmt.Errorf("render sample key %s at offset %d: %w", keyCol.Column.SourceName, offset, err)
			}
		}

		keys = append(keys, validationKey{
			RawValues:        rawValues,
			TargetFragments:  targetFragments,
			DisplayFragments: displayFragments,
		})
	}
	return keys, nil
}

func (b *liveValidationBackend) sourceFragments(ctx context.Context, table Table, cols []validationColumn, key validationKey) ([]string, error) {
	query, args := buildSourceValidationRowQuery(b.src, table, cols, key.RawValues, primaryKeyColumns(table), b.typeMap)
	rawValues, err := querySQLRowValues(ctx, b.srcDB, query, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("source row not found for key %s", formatValidationKey(table, key))
		}
		return nil, fmt.Errorf("read source row for key %s: %w", formatValidationKey(table, key), err)
	}
	return canonicalizeSourceRow(rawValues, cols, b.src, b.typeMap)
}

func (b *liveValidationBackend) targetFragments(ctx context.Context, table Table, keyCols []validationColumn, cols []validationColumn, key validationKey) ([]string, error) {
	query, args := buildTargetValidationRowQuery(b.pgSchema, table, keyCols, cols, key.TargetFragments)
	values, err := queryPGRowValues(ctx, b.pool, query, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("target row not found for key %s", formatValidationKey(table, key))
		}
		return nil, fmt.Errorf("read target row for key %s: %w", formatValidationKey(table, key), err)
	}

	fragments := make([]string, len(values))
	for i, val := range values {
		if val == nil {
			fragments[i] = "null"
			continue
		}
		switch v := val.(type) {
		case string:
			fragments[i] = v
		case []byte:
			fragments[i] = string(v)
		default:
			fragments[i] = fmt.Sprint(v)
		}
	}
	return fragments, nil
}

// validateMigration runs post-load validation according to the configured mode.
// Tables are validated in parallel with bounded concurrency. The workers parameter
// controls maximum parallelism and is capped by source backend limits (e.g., SQLite
// is always single-threaded).
func validateMigration(ctx context.Context, src SourceDB, srcDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, mode string, workers int, typeMap TypeMappingConfig) ([]ValidationResult, error) {
	if mode == validationModeNone || mode == "" {
		return nil, nil
	}

	workers = validationWorkers(workers, src)

	srcDB, err := src.OpenDB(srcDSN)
	if err != nil {
		return nil, fmt.Errorf("open source for validation: %w", err)
	}
	defer srcDB.Close()
	srcDB.SetMaxOpenConns(workers)
	srcDB.SetMaxIdleConns(workers)

	backend := &liveValidationBackend{
		src:      src,
		srcDB:    srcDB,
		pool:     pool,
		pgSchema: pgSchema,
		typeMap:  typeMap,
	}

	start := time.Now()
	results := make([]ValidationResult, len(schema.Tables))

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

			var result ValidationResult
			var err error
			switch mode {
			case validationModeRowCount:
				result, err = validateRowCountTable(ctx, backend, tbl)
			case validationModeSampledHash:
				result, err = validateSampledHashTable(ctx, backend, src, tbl, typeMap)
			default:
				err = fmt.Errorf("unsupported validation mode %q", mode)
			}
			if err != nil {
				setErr(err)
				return
			}
			results[idx] = result
		}(i, t)
	}

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	rowCountMismatches := 0
	sampledHashMismatches := 0
	sampledHashSkipped := 0

	for _, r := range results {
		switch {
		case !r.CountMatch:
			rowCountMismatches++
			log.Printf("  MISMATCH: %s — source=%d target=%d", r.Table, r.SourceCount, r.TargetCount)
		case mode == validationModeSampledHash && r.SampleMismatch != "":
			sampledHashMismatches++
			log.Printf("  MISMATCH: %s — %d rows, %s", r.Table, r.SourceCount, r.SampleMismatch)
		case mode == validationModeSampledHash && r.SampleSkippedReason != "":
			sampledHashSkipped++
			log.Printf("  OK: %s — %d rows; sampled_hash skipped (%s)", r.Table, r.SourceCount, r.SampleSkippedReason)
		case mode == validationModeSampledHash:
			log.Printf("  OK: %s — %d rows; sampled_hash matched %d row(s) across %d column(s)", r.Table, r.SourceCount, r.SampleRowsChecked, r.SampleColumnsChecked)
		default:
			log.Printf("  OK: %s — %d rows", r.Table, r.SourceCount)
		}
	}

	log.Printf("  validated %d table(s) in %s (workers=%d)", len(schema.Tables), time.Since(start).Round(time.Millisecond), workers)
	if mode == validationModeSampledHash && sampledHashSkipped > 0 {
		log.Printf("  sampled_hash fallback: %d table(s) used row_count only", sampledHashSkipped)
	}

	if rowCountMismatches > 0 || sampledHashMismatches > 0 {
		names := make([]string, 0, rowCountMismatches+sampledHashMismatches)
		for _, r := range results {
			if !r.CountMatch || r.SampleMismatch != "" {
				names = append(names, r.Table)
			}
		}

		if mode == validationModeSampledHash && sampledHashMismatches > 0 && rowCountMismatches > 0 {
			return results, fmt.Errorf("validation failed: %d row count mismatch(es) and %d sampled_hash mismatch(es): %s", rowCountMismatches, sampledHashMismatches, strings.Join(names, ", "))
		}
		if mode == validationModeSampledHash && sampledHashMismatches > 0 {
			return results, fmt.Errorf("validation failed: %d sampled_hash mismatch(es): %s", sampledHashMismatches, strings.Join(names, ", "))
		}
		return results, fmt.Errorf("validation failed: row count mismatch on %d table(s): %s", rowCountMismatches, strings.Join(names, ", "))
	}

	return results, nil
}

func validateRowCountTable(ctx context.Context, backend *liveValidationBackend, table Table) (ValidationResult, error) {
	result := ValidationResult{Table: table.SourceName}

	var err error
	result.SourceCount, err = backend.sourceCount(ctx, table)
	if err != nil {
		return ValidationResult{}, err
	}
	result.TargetCount, err = backend.targetCount(ctx, table)
	if err != nil {
		return ValidationResult{}, err
	}
	result.CountMatch = result.SourceCount == result.TargetCount
	return result, nil
}

func validateSampledHashTable(ctx context.Context, backend sampledHashBackend, src SourceDB, table Table, typeMap TypeMappingConfig) (ValidationResult, error) {
	result := ValidationResult{Table: table.SourceName}

	var err error
	result.SourceCount, err = backend.sourceCount(ctx, table)
	if err != nil {
		return ValidationResult{}, err
	}
	result.TargetCount, err = backend.targetCount(ctx, table)
	if err != nil {
		return ValidationResult{}, err
	}
	result.CountMatch = result.SourceCount == result.TargetCount
	if !result.CountMatch || result.SourceCount == 0 {
		if result.SourceCount == 0 && result.TargetCount == 0 {
			result.SampleMatch = true
		}
		return result, nil
	}

	keyCols, reason, err := validationPrimaryKeyColumns(table, src, typeMap)
	if err != nil {
		return ValidationResult{}, err
	}
	if reason != "" {
		result.SampleSkippedReason = reason
		return result, nil
	}

	compareCols, err := validationComparableColumns(table, src, typeMap)
	if err != nil {
		return ValidationResult{}, err
	}
	if len(compareCols) == 0 {
		result.SampleSkippedReason = "no columns with supported sampled_hash fingerprinting"
		return result, nil
	}

	return runSampledHashComparisons(ctx, backend, table, keyCols, compareCols, result)
}

func validateSampledHashTableWithColumns(ctx context.Context, backend sampledHashBackend, table Table, keyCols []validationColumn, compareCols []validationColumn) (ValidationResult, error) {
	result := ValidationResult{Table: table.SourceName}

	var err error
	result.SourceCount, err = backend.sourceCount(ctx, table)
	if err != nil {
		return ValidationResult{}, err
	}
	result.TargetCount, err = backend.targetCount(ctx, table)
	if err != nil {
		return ValidationResult{}, err
	}
	result.CountMatch = result.SourceCount == result.TargetCount
	if !result.CountMatch || result.SourceCount == 0 {
		if result.SourceCount == 0 && result.TargetCount == 0 {
			result.SampleMatch = true
		}
		return result, nil
	}

	return runSampledHashComparisons(ctx, backend, table, keyCols, compareCols, result)
}

func runSampledHashComparisons(ctx context.Context, backend sampledHashBackend, table Table, keyCols []validationColumn, compareCols []validationColumn, result ValidationResult) (ValidationResult, error) {
	offsets := validationSampleOffsets(result.SourceCount, validationSampleRows)
	keys, err := backend.sampleKeys(ctx, table, keyCols, offsets)
	if err != nil {
		return ValidationResult{}, err
	}

	result.SampleRowsChecked = len(keys)
	result.SampleColumnsChecked = len(compareCols)
	result.SampleMatch = true

	for _, key := range keys {
		sourceFragments, err := backend.sourceFragments(ctx, table, compareCols, key)
		if err != nil {
			return ValidationResult{}, err
		}
		targetFragments, err := backend.targetFragments(ctx, table, keyCols, compareCols, key)
		if err != nil {
			return ValidationResult{}, err
		}

		for i := range compareCols {
			if sourceFragments[i] == targetFragments[i] {
				continue
			}
			result.SampleMatch = false
			result.SampleMismatch = fmt.Sprintf(
				"sampled_hash mismatch at key %s on column %s (source=%s target=%s, row_source_hash=%s row_target_hash=%s)",
				formatValidationKey(table, key),
				compareCols[i].Column.SourceName,
				sourceFragments[i],
				targetFragments[i],
				hashValidationFragments(compareCols, sourceFragments),
				hashValidationFragments(compareCols, targetFragments),
			)
			return result, nil
		}
	}

	return result, nil
}

func validationPrimaryKeyColumns(table Table, src SourceDB, typeMap TypeMappingConfig) ([]validationColumn, string, error) {
	if table.PrimaryKey == nil || len(table.PrimaryKey.Columns) == 0 {
		return nil, "table has no primary key", nil
	}

	cols := make([]validationColumn, 0, len(table.PrimaryKey.Columns))
	for _, pgName := range table.PrimaryKey.Columns {
		col, ok := findColumnByPGName(table, pgName)
		if !ok {
			return nil, "", fmt.Errorf("primary key column %s not found on table %s", pgName, table.SourceName)
		}

		pgType, err := src.MapType(col, typeMap)
		if err != nil {
			return nil, "", fmt.Errorf("map primary key column %s on %s: %w", col.SourceName, table.SourceName, err)
		}

		kind, ok := validationKindForPGType(pgType)
		if !ok {
			return nil, fmt.Sprintf("primary key column %s maps to unsupported validation type %s", col.SourceName, pgType), nil
		}

		cols = append(cols, validationColumn{Column: col, Kind: kind, PGType: pgType})
	}
	return cols, "", nil
}

func validationComparableColumns(table Table, src SourceDB, typeMap TypeMappingConfig) ([]validationColumn, error) {
	cols := make([]validationColumn, 0, len(table.Columns))
	for _, col := range table.Columns {
		pgType, err := src.MapType(col, typeMap)
		if err != nil {
			return nil, fmt.Errorf("map validation column %s on %s: %w", col.SourceName, table.SourceName, err)
		}
		kind, ok := validationKindForPGType(pgType)
		if !ok {
			continue
		}
		cols = append(cols, validationColumn{Column: col, Kind: kind, PGType: pgType})
	}
	return cols, nil
}

func validationKindForPGType(pgType string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(pgType))
	switch {
	case normalized == "boolean":
		return validationKindBool, true
	case normalized == "smallint", normalized == "integer", normalized == "bigint",
		normalized == "real", normalized == "double precision",
		strings.HasPrefix(normalized, "numeric("), normalized == "numeric":
		return validationKindNumericText, true
	case normalized == "text", normalized == "citext",
		strings.HasPrefix(normalized, "char("), normalized == "char",
		strings.HasPrefix(normalized, "character("), normalized == "character",
		strings.HasPrefix(normalized, "varchar("), normalized == "varchar",
		strings.HasPrefix(normalized, "character varying("), normalized == "character varying":
		return validationKindText, true
	case normalized == "uuid":
		return validationKindText, true
	case normalized == "bytea":
		return validationKindBytea, true
	case normalized == "json", normalized == "jsonb":
		return validationKindJSON, true
	case normalized == "text[]":
		return validationKindTextArray, true
	case normalized == "date":
		return validationKindDate, true
	case normalized == "timestamp":
		return validationKindTimestamp, true
	case normalized == "timestamptz":
		return validationKindTimestamptz, true
	case normalized == "time":
		return validationKindTime, true
	default:
		return "", false
	}
}

func validationSampleOffsets(total int64, limit int) []int64 {
	if total <= 0 || limit <= 0 {
		return nil
	}
	if total <= int64(limit) {
		offsets := make([]int64, total)
		for i := range offsets {
			offsets[i] = int64(i)
		}
		return offsets
	}

	offsets := make([]int64, 0, limit)
	seen := make(map[int64]struct{}, limit)
	for i := 0; i < limit; i++ {
		offset := int64(i) * (total - 1) / int64(limit-1)
		if _, ok := seen[offset]; ok {
			continue
		}
		seen[offset] = struct{}{}
		offsets = append(offsets, offset)
	}
	return offsets
}

func buildSourceSampleKeyQuery(src SourceDB, table Table, keyCols []validationColumn, offset int64) string {
	selectCols := make([]string, len(keyCols))
	orderBy := make([]string, len(keyCols))
	for i, col := range keyCols {
		quoted := src.QuoteIdentifier(col.Column.SourceName)
		selectCols[i] = quoted
		orderBy[i] = quoted
	}

	switch src.Name() {
	case "MSSQL":
		return fmt.Sprintf(
			"SELECT %s FROM %s ORDER BY %s OFFSET %d ROWS FETCH NEXT 1 ROWS ONLY",
			strings.Join(selectCols, ", "),
			src.SourceTableRef(table),
			strings.Join(orderBy, ", "),
			offset,
		)
	default:
		return fmt.Sprintf(
			"SELECT %s FROM %s ORDER BY %s LIMIT 1 OFFSET %d",
			strings.Join(selectCols, ", "),
			src.SourceTableRef(table),
			strings.Join(orderBy, ", "),
			offset,
		)
	}
}

func buildSourceValidationRowQuery(src SourceDB, table Table, cols []validationColumn, rawKeyValues []any, keyCols []Column, typeMap TypeMappingConfig) (string, []any) {
	selectExprs := make([]string, len(cols))
	for i, col := range cols {
		selectExprs[i] = columnSelectExpr(src, col.Column, typeMap)
	}

	where := make([]string, len(keyCols))
	for i, keyCol := range keyCols {
		where[i] = fmt.Sprintf("%s = %s", src.QuoteIdentifier(keyCol.SourceName), sourcePlaceholder(src, i+1))
	}

	return fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s",
		strings.Join(selectExprs, ", "),
		src.SourceTableRef(table),
		strings.Join(where, " AND "),
	), rawKeyValues
}

func buildTargetValidationRowQuery(pgSchema string, table Table, keyCols []validationColumn, cols []validationColumn, keyFragments []string) (string, []any) {
	selectExprs := make([]string, len(cols))
	for i, col := range cols {
		selectExprs[i] = targetValidationExpr(col)
	}

	where := make([]string, len(keyCols))
	args := make([]any, len(keyFragments))
	for i, keyCol := range keyCols {
		where[i] = fmt.Sprintf("%s = $%d", targetValidationExpr(keyCol), i+1)
		args[i] = keyFragments[i]
	}

	return fmt.Sprintf(
		"SELECT %s FROM %s.%s WHERE %s LIMIT 1",
		strings.Join(selectExprs, ", "),
		pgIdent(pgSchema),
		pgIdent(table.PGName),
		strings.Join(where, " AND "),
	), args
}

func sourcePlaceholder(src SourceDB, index int) string {
	if src.Name() == "MSSQL" {
		return fmt.Sprintf("@p%d", index)
	}
	return "?"
}

func targetValidationExpr(col validationColumn) string {
	quoted := pgIdent(col.Column.PGName)
	switch col.Kind {
	case validationKindBool:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(%s)::text END", quoted, quoted)
	case validationKindText:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(%s)::text END", quoted, quoted)
	case validationKindNumericText:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json((%s)::text)::text END", quoted, quoted)
	case validationKindJSON:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE (%s)::jsonb::text END", quoted, quoted)
	case validationKindBytea:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(encode(%s, 'hex'))::text END", quoted, quoted)
	case validationKindTextArray:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE array_to_json(%s)::text END", quoted, quoted)
	case validationKindDate:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(to_char(%s, 'YYYY-MM-DD'))::text END", quoted, quoted)
	case validationKindTimestamp:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(to_char(%s, 'YYYY-MM-DD\"T\"HH24:MI:SS.US'))::text END", quoted, quoted)
	case validationKindTimestamptz:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(to_char(%s AT TIME ZONE 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS.US\"Z\"'))::text END", quoted, quoted)
	case validationKindTime:
		return fmt.Sprintf("CASE WHEN %s IS NULL THEN NULL ELSE to_json(to_char(%s, 'HH24:MI:SS.US'))::text END", quoted, quoted)
	default:
		return quoted
	}
}

func querySQLRowValues(ctx context.Context, db *sql.DB, query string, args ...any) ([]any, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}

	values := make([]any, len(columns))
	scanPtrs := make([]any, len(columns))
	for i := range values {
		scanPtrs[i] = &values[i]
	}
	if err := rows.Scan(scanPtrs...); err != nil {
		return nil, err
	}
	return values, rows.Err()
}

func queryPGRowValues(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]any, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}

	values, err := rows.Values()
	if err != nil {
		return nil, err
	}
	return values, rows.Err()
}

func canonicalizeSourceRow(rawValues []any, cols []validationColumn, src SourceDB, typeMap TypeMappingConfig) ([]string, error) {
	fragments := make([]string, len(cols))
	for i, col := range cols {
		transformed, err := src.TransformValue(rawValues[i], col.Column, typeMap)
		if err != nil {
			return nil, fmt.Errorf("transform %s: %w", col.Column.SourceName, err)
		}
		fragments[i], err = canonicalizeValidationFragment(transformed, col.Kind)
		if err != nil {
			return nil, fmt.Errorf("canonicalize %s: %w", col.Column.SourceName, err)
		}
	}
	return fragments, nil
}

func canonicalizeValidationFragment(val any, kind string) (string, error) {
	if val == nil {
		return "null", nil
	}

	switch kind {
	case validationKindBool:
		return canonicalizeBoolFragment(val)
	case validationKindText, validationKindNumericText:
		s, err := renderValidationText(val, kind)
		if err != nil {
			return "", err
		}
		return marshalJSONString(s)
	case validationKindJSON:
		return canonicalizeJSONFragment(val)
	case validationKindBytea:
		b, err := asBytes(val)
		if err != nil {
			return "", err
		}
		return marshalJSONString(hex.EncodeToString(b))
	case validationKindTextArray:
		values, err := asStringSlice(val)
		if err != nil {
			return "", err
		}
		out, err := json.Marshal(values)
		if err != nil {
			return "", err
		}
		return string(out), nil
	case validationKindDate, validationKindTimestamp, validationKindTimestamptz, validationKindTime:
		s, err := renderValidationText(val, kind)
		if err != nil {
			return "", err
		}
		return marshalJSONString(s)
	default:
		return "", fmt.Errorf("unsupported validation kind %s", kind)
	}
}

func renderValidationDisplay(val any, kind string) (string, error) {
	if val == nil {
		return "NULL", nil
	}
	switch kind {
	case validationKindBool:
		return renderBoolText(val)
	case validationKindJSON:
		fragment, err := canonicalizeJSONFragment(val)
		if err != nil {
			return "", err
		}
		return fragment, nil
	default:
		return renderValidationText(val, kind)
	}
}

func renderValidationText(val any, kind string) (string, error) {
	switch kind {
	case validationKindBool:
		return renderBoolText(val)
	case validationKindText:
		switch v := val.(type) {
		case string:
			return v, nil
		case []byte:
			return string(v), nil
		default:
			return fmt.Sprint(v), nil
		}
	case validationKindNumericText:
		return renderNumericText(val)
	case validationKindDate:
		return renderDateText(val)
	case validationKindTimestamp:
		return renderTimestampText(val)
	case validationKindTimestamptz:
		return renderTimestamptzText(val)
	case validationKindTime:
		return renderTimeText(val)
	default:
		return "", fmt.Errorf("unsupported text rendering kind %s", kind)
	}
}

func renderNumericText(val any) (string, error) {
	switch v := val.(type) {
	case string:
		return strings.TrimSpace(v), nil
	case []byte:
		return strings.TrimSpace(string(v)), nil
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.Itoa(v), nil
	case int8:
		return strconv.FormatInt(int64(v), 10), nil
	case int16:
		return strconv.FormatInt(int64(v), 10), nil
	case int32:
		return strconv.FormatInt(int64(v), 10), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(v), 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return fmt.Sprint(v), nil
	}
}

func renderBoolText(val any) (string, error) {
	switch v := val.(type) {
	case bool:
		if v {
			return "true", nil
		}
		return "false", nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1":
			return "true", nil
		case "false", "0":
			return "false", nil
		}
	case []byte:
		return renderBoolText(string(v))
	case int:
		return renderBoolText(v != 0)
	case int8:
		return renderBoolText(v != 0)
	case int16:
		return renderBoolText(v != 0)
	case int32:
		return renderBoolText(v != 0)
	case int64:
		return renderBoolText(v != 0)
	case uint:
		return renderBoolText(v != 0)
	case uint8:
		return renderBoolText(v != 0)
	case uint16:
		return renderBoolText(v != 0)
	case uint32:
		return renderBoolText(v != 0)
	case uint64:
		return renderBoolText(v != 0)
	}
	return "", fmt.Errorf("unsupported bool value %T", val)
}

func canonicalizeBoolFragment(val any) (string, error) {
	text, err := renderBoolText(val)
	if err != nil {
		return "", err
	}
	if text == "true" {
		return "true", nil
	}
	return "false", nil
}

func renderDateText(val any) (string, error) {
	switch v := val.(type) {
	case time.Time:
		return v.Format("2006-01-02"), nil
	case string:
		return strings.TrimSpace(v), nil
	case []byte:
		return strings.TrimSpace(string(v)), nil
	default:
		return "", fmt.Errorf("unsupported date value %T", val)
	}
}

func renderTimestampText(val any) (string, error) {
	switch v := val.(type) {
	case time.Time:
		return v.Format("2006-01-02T15:04:05.000000"), nil
	case string:
		return normalizeTimestampString(v)
	case []byte:
		return normalizeTimestampString(string(v))
	default:
		return "", fmt.Errorf("unsupported timestamp value %T", val)
	}
}

func renderTimestamptzText(val any) (string, error) {
	switch v := val.(type) {
	case time.Time:
		return v.UTC().Format("2006-01-02T15:04:05.000000Z"), nil
	case string:
		return normalizeTimestamptzString(v)
	case []byte:
		return normalizeTimestamptzString(string(v))
	default:
		return "", fmt.Errorf("unsupported timestamptz value %T", val)
	}
}

func renderTimeText(val any) (string, error) {
	switch v := val.(type) {
	case time.Time:
		return v.Format("15:04:05.000000"), nil
	case string:
		return normalizeClockString(v)
	case []byte:
		return normalizeClockString(string(v))
	default:
		return "", fmt.Errorf("unsupported time value %T", val)
	}
}

func normalizeTimestampString(s string) (string, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.Format("2006-01-02T15:04:05.000000"), nil
		}
	}
	return "", fmt.Errorf("cannot normalize timestamp %q", s)
}

func normalizeTimestamptzString(s string) (string, error) {
	s = strings.TrimSpace(s)
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05Z07:00",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02T15:04:05.000000Z"), nil
		}
	}
	return "", fmt.Errorf("cannot normalize timestamptz %q", s)
}

func normalizeClockString(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty time value")
	}
	parts := strings.SplitN(s, ".", 2)
	base := parts[0]
	if len(base) != len("15:04:05") {
		return "", fmt.Errorf("cannot normalize time %q", s)
	}
	fraction := "000000"
	if len(parts) == 2 {
		fraction = parts[1]
		if len(fraction) > 6 {
			fraction = fraction[:6]
		}
		for len(fraction) < 6 {
			fraction += "0"
		}
	}
	return base + "." + fraction, nil
}

func canonicalizeJSONFragment(val any) (string, error) {
	var raw []byte
	switch v := val.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		out, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		raw = out
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", err
	}
	out, err := json.Marshal(decoded)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func asBytes(val any) ([]byte, error) {
	switch v := val.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("expected []byte or string, got %T", val)
	}
}

func asStringSlice(val any) ([]string, error) {
	switch v := val.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("array element %d has type %T", i, item)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected []string, got %T", val)
	}
}

func marshalJSONString(s string) (string, error) {
	out, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func hashValidationFragments(cols []validationColumn, fragments []string) string {
	h := sha256.New()
	for i, fragment := range fragments {
		_, _ = h.Write([]byte(cols[i].Column.PGName))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(fragment))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func primaryKeyColumns(table Table) []Column {
	if table.PrimaryKey == nil {
		return nil
	}
	cols := make([]Column, 0, len(table.PrimaryKey.Columns))
	for _, pgName := range table.PrimaryKey.Columns {
		col, ok := findColumnByPGName(table, pgName)
		if !ok {
			continue
		}
		cols = append(cols, col)
	}
	return cols
}

func formatValidationKey(table Table, key validationKey) string {
	pk := table.PrimaryKey
	if pk == nil {
		return "(no primary key)"
	}

	parts := make([]string, len(pk.Columns))
	for i, colName := range pk.Columns {
		val := "?"
		if i < len(key.DisplayFragments) {
			val = key.DisplayFragments[i]
		}
		parts[i] = fmt.Sprintf("%s=%s", colName, val)
	}
	return strings.Join(parts, ", ")
}
