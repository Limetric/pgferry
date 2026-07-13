package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"
)

// ChunkKey describes the column used for range-based chunking of a table.
type ChunkKey struct {
	SourceColumn string // source column name used for range partitioning
	PGColumn     string // corresponding PG column name
}

// Chunk represents a single bounded range of a table to copy.
type Chunk struct {
	Index      int   // chunk ordinal (0-based)
	LowerBound int64 // inclusive lower bound
	UpperBound int64 // exclusive upper bound (except for the last chunk)
	IsLast     bool  // true if this is the final chunk (uses <= instead of <)
}

// ChunkPlan describes the full chunking strategy for one table.
type ChunkPlan struct {
	Table     Table
	ChunkKey  *ChunkKey // nil means the table is not chunkable
	Chunks    []Chunk
	ChunkSize int64
	// ColumnSelectList is the pre-joined SELECT list (columnSelectExpr per column)
	// for chunked reads. Empty when ChunkKey is nil.
	ColumnSelectList string
	// PGCopyColumns is the ordered list of PostgreSQL column names for COPY,
	// one per table.Columns entry. Populated for every plan.
	PGCopyColumns []string
	// KeyMin and KeyMax are the chunk key's MIN/MAX that Chunks were planned over.
	// Chunk ordinals are only meaningful relative to this range: chunk i covers
	// [KeyMin + i*ChunkSize, ...), so a resume against a moved range would have the
	// same ordinal denote a different slice of the table.
	KeyMin int64
	KeyMax int64
	// HasRows is false when the table was empty at plan time.
	HasRows bool
}

// maxPlannedChunks bounds how many chunks a single table may be split into.
// Chunks are planned over the key *range*, not the row count, so a sparse key
// space (snowflake IDs, a jumped AUTO_INCREMENT, timestamp-derived keys) can
// imply an astronomical chunk count for a table holding very few rows. Rather
// than allocating and querying that many chunks, planChunks widens the chunk
// size so the count stays bounded. A dense table only reaches this limit above
// ~1e11 rows at the default chunk size, where the widening is a no-op anyway.
const maxPlannedChunks = 1_000_000

// keySpan returns the inclusive width of [min, max] minus one, i.e. the distance
// max-min, as an exact uint64. Modular uint64 subtraction preserves the true
// distance even when the range crosses zero or touches the int64 extremes, where
// signed max-min would overflow.
func keySpan(min, max int64) uint64 {
	if max < min {
		return 0
	}
	return uint64(max) - uint64(min)
}

// estimatedChunkCount returns how many chunks planChunks produces for [min, max]
// with the given positive chunkSize. For a span smaller than chunkSize it returns 1
// (single chunk path). Returns 0 if the count does not fit in int (caller should
// skip preallocation).
func estimatedChunkCount(min, max, chunkSize int64) int {
	if chunkSize <= 0 || max < min {
		return 0
	}
	span := keySpan(min, max)
	if span < uint64(chunkSize) {
		return 1
	}
	q := span / uint64(chunkSize)
	if q == math.MaxUint64 {
		return 0
	}
	n := q + 1
	if n > uint64(math.MaxInt) {
		return 0
	}
	return int(n)
}

// effectiveChunkSize widens chunkSize when the key range would otherwise be split
// into more than maxPlannedChunks chunks. Returns the size to actually plan with.
func effectiveChunkSize(min, max, chunkSize int64) int64 {
	if chunkSize <= 0 || max < min {
		return chunkSize
	}
	span := keySpan(min, max)
	// planChunks emits span/chunkSize + 1 chunks, so the count stays within the
	// limit exactly while span/chunkSize < maxPlannedChunks.
	if span/uint64(chunkSize) < uint64(maxPlannedChunks) {
		return chunkSize
	}
	widened := span/uint64(maxPlannedChunks-1) + 1
	if widened > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(widened)
}

// planChunks divides the [min, max] key range into chunks of approximately chunkSize.
// Returns a single chunk covering the full range if the range is smaller than chunkSize.
func planChunks(min, max, chunkSize int64) []Chunk {
	chunkSize = normalizedChunkSize(chunkSize)

	if max < min {
		return []Chunk{{Index: 0, LowerBound: min, UpperBound: max, IsLast: true}}
	}

	if widened := effectiveChunkSize(min, max, chunkSize); widened != chunkSize {
		log.Printf("  WARN: key range %d..%d is too sparse for chunk_size %d "+
			"(would need >%d chunks); widening chunk size to %d",
			min, max, chunkSize, maxPlannedChunks, widened)
		chunkSize = widened
	}

	if keySpan(min, max) < uint64(chunkSize) {
		return []Chunk{{
			Index:      0,
			LowerBound: min,
			UpperBound: max,
			IsLast:     true,
		}}
	}

	var chunks []Chunk
	if wantCap := estimatedChunkCount(min, max, chunkSize); wantCap > 0 {
		chunks = make([]Chunk, 0, wantCap)
	} else {
		chunks = make([]Chunk, 0)
	}

	// Walk the range with uint64 offsets: lower+chunkSize can overflow int64 near
	// the top of the key space, which previously wrapped negative, emitted a chunk
	// whose predicate matched no rows, and sent the loop spinning back up from the
	// bottom of the range.
	for lower := min; ; {
		remaining := uint64(max) - uint64(lower)
		if remaining < uint64(chunkSize) {
			chunks = append(chunks, Chunk{
				Index:      len(chunks),
				LowerBound: lower,
				UpperBound: max,
				IsLast:     true,
			})
			break
		}
		upper := int64(uint64(lower) + uint64(chunkSize))
		chunks = append(chunks, Chunk{
			Index:      len(chunks),
			LowerBound: lower,
			UpperBound: upper,
			IsLast:     false,
		})
		lower = upper
	}
	return chunks
}

// buildColumnSelectList returns the comma-separated SELECT list for table.Columns
// (same expressions as full-table SELECT). Built once per chunkable table.
func buildColumnSelectList(src SourceDB, table Table, typeMap TypeMappingConfig) string {
	cols := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = columnSelectExpr(src, col, typeMap)
	}
	return strings.Join(cols, ", ")
}

// buildChunkedSelectQuery builds a SELECT query for a single chunk of a table.
// columnSelectList must be buildColumnSelectList(src, table, typeMap) for correct semantics.
func buildChunkedSelectQuery(src SourceDB, table Table, key ChunkKey, chunk Chunk, columnSelectList string) string {
	quotedKey := src.QuoteIdentifier(key.SourceColumn)
	tableName := src.SourceTableRef(table)

	if chunk.IsLast {
		return fmt.Sprintf("SELECT %s FROM %s WHERE %s >= %d AND %s <= %d ORDER BY %s",
			columnSelectList, tableName,
			quotedKey, chunk.LowerBound,
			quotedKey, chunk.UpperBound,
			quotedKey)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s >= %d AND %s < %d ORDER BY %s",
		columnSelectList, tableName,
		quotedKey, chunk.LowerBound,
		quotedKey, chunk.UpperBound,
		quotedKey)
}

// chunkKeyForTable returns a ChunkKey if the table has a single-column numeric
// primary key suitable for range-based chunking. Returns nil otherwise.
func chunkKeyForTable(table Table, src SourceDB) *ChunkKey {
	if table.PrimaryKey == nil {
		return nil
	}
	if len(table.PrimaryKey.Columns) != 1 {
		return nil
	}

	// Find the PK column in the table's columns to check its data type
	pkPGName := table.PrimaryKey.Columns[0]
	for _, col := range table.Columns {
		if col.PGName != pkPGName {
			continue
		}
		if !isNumericChunkableType(col, src) {
			return nil
		}
		if !isChunkKeyNullSafe(table, col, src) {
			log.Printf("  [%s] primary key %q may contain NULLs; copying the table in one pass instead of chunking",
				table.SourceName, col.SourceName)
			return nil
		}
		return &ChunkKey{
			SourceColumn: col.SourceName,
			PGColumn:     col.PGName,
		}
	}
	return nil
}

// isChunkKeyNullSafe reports whether the chunk key column is guaranteed to hold
// no NULLs. Every chunk predicate is a range comparison (key >= lo AND key <= hi),
// which never matches NULL, and queryMinMax ignores NULLs — so chunking a nullable
// key would silently drop those rows from the migration.
func isChunkKeyNullSafe(table Table, col Column, src SourceDB) bool {
	if !col.Nullable {
		return true
	}
	if sourceTypeForDB(src) != "sqlite" {
		// MySQL and MSSQL force primary key columns NOT NULL.
		return true
	}

	// SQLite only makes a single-column PK an implicitly NOT NULL rowid alias when
	// the declared type is exactly INTEGER. Any other integer-affinity spelling
	// (BIGINT, INT, SMALLINT, ...) is an ordinary column that accepts NULL — a
	// documented legacy quirk. A DESC primary key is not a rowid alias either.
	if !strings.EqualFold(strings.TrimSpace(col.ColumnType), "INTEGER") {
		return false
	}
	for _, order := range table.PrimaryKey.ColumnOrders {
		if strings.EqualFold(strings.TrimSpace(order), "DESC") {
			return false
		}
	}
	return true
}

// isNumericChunkableType returns true if the column has a numeric integer type
// suitable for range-based chunking. Unsigned bigint is excluded because its
// values can exceed int64 range, causing scan failures in queryMinMax.
func isNumericChunkableType(col Column, src SourceDB) bool {
	switch {
	case isMySQLFamilySource(src):
		isUnsigned := strings.Contains(strings.ToLower(col.ColumnType), "unsigned")
		if col.DataType == "bigint" && isUnsigned {
			return false
		}
		switch col.DataType {
		case "tinyint", "smallint", "mediumint", "int", "bigint":
			return true
		}
	case sourceTypeForDB(src) == "sqlite":
		dt := strings.ToUpper(normalizeAffinity(col.ColumnType))
		switch dt {
		case "INTEGER", "INT", "SMALLINT", "TINYINT", "MEDIUMINT", "BIGINT":
			return true
		}
	case sourceTypeForDB(src) == "mssql":
		switch col.DataType {
		case "tinyint", "smallint", "int", "bigint":
			return true
		}
	}
	return false
}

func buildMinMaxQuery(src SourceDB, table Table, key ChunkKey) string {
	return fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM %s",
		src.QuoteIdentifier(key.SourceColumn),
		src.QuoteIdentifier(key.SourceColumn),
		src.SourceTableRef(table))
}

// queryMinMax queries the MIN and MAX values of the chunk key column.
// Returns (min, max, hasRows, error). If the table is empty, hasRows is false.
func queryMinMax(ctx context.Context, source dbQuerier, src SourceDB, table Table, key ChunkKey) (int64, int64, bool, error) {
	query := buildMinMaxQuery(src, table, key)
	rows, err := source.QueryContext(ctx, query)
	if err != nil {
		return 0, 0, false, fmt.Errorf("query min/max for %s: %w", table.SourceName, err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, 0, false, fmt.Errorf("query min/max for %s: %w", table.SourceName, err)
		}
		return 0, 0, false, nil
	}

	var minVal, maxVal sql.NullInt64
	if err := rows.Scan(&minVal, &maxVal); err != nil {
		return 0, 0, false, fmt.Errorf("scan min/max for %s: %w", table.SourceName, err)
	}

	if !minVal.Valid || !maxVal.Valid {
		return 0, 0, false, nil
	}
	return minVal.Int64, maxVal.Int64, true, nil
}
