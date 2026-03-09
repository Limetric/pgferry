package main

import (
	"context"
	"database/sql"
	"fmt"
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
}

// planChunks divides the [min, max] key range into chunks of approximately chunkSize.
// Returns a single chunk covering the full range if the range is smaller than chunkSize.
func planChunks(min, max, chunkSize int64) []Chunk {
	if chunkSize <= 0 {
		chunkSize = 100000
	}

	// Guard against int64 overflow: if max-min would overflow when adding 1,
	// the range is larger than any chunkSize so we skip the single-chunk path.
	rangeSize := max - min // safe: max >= min
	if rangeSize < chunkSize {
		return []Chunk{{
			Index:      0,
			LowerBound: min,
			UpperBound: max,
			IsLast:     true,
		}}
	}

	var chunks []Chunk
	for lower := min; lower <= max; {
		upper := lower + chunkSize
		isLast := upper > max
		if isLast {
			upper = max
		}
		chunks = append(chunks, Chunk{
			Index:      len(chunks),
			LowerBound: lower,
			UpperBound: upper,
			IsLast:     isLast,
		})
		if isLast {
			break
		}
		lower = upper
	}
	return chunks
}

// buildChunkedSelectQuery builds a SELECT query for a single chunk of a table.
func buildChunkedSelectQuery(src SourceDB, table Table, key ChunkKey, chunk Chunk, typeMap TypeMappingConfig) string {
	cols := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = columnSelectExpr(src, col, typeMap)
	}

	quotedKey := src.QuoteIdentifier(key.SourceColumn)
	tableName := src.QuoteIdentifier(table.SourceName)

	if chunk.IsLast {
		return fmt.Sprintf("SELECT %s FROM %s WHERE %s >= %d AND %s <= %d ORDER BY %s",
			strings.Join(cols, ", "), tableName,
			quotedKey, chunk.LowerBound,
			quotedKey, chunk.UpperBound,
			quotedKey)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s >= %d AND %s < %d ORDER BY %s",
		strings.Join(cols, ", "), tableName,
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
		if isNumericChunkableType(col, src) {
			return &ChunkKey{
				SourceColumn: col.SourceName,
				PGColumn:     col.PGName,
			}
		}
		return nil
	}
	return nil
}

// isNumericChunkableType returns true if the column has a numeric integer type
// suitable for range-based chunking. Unsigned bigint is excluded because its
// values can exceed int64 range, causing scan failures in queryMinMax.
func isNumericChunkableType(col Column, src SourceDB) bool {
	switch src.Name() {
	case "MySQL":
		isUnsigned := strings.Contains(strings.ToLower(col.ColumnType), "unsigned")
		if col.DataType == "bigint" && isUnsigned {
			return false
		}
		switch col.DataType {
		case "tinyint", "smallint", "mediumint", "int", "bigint":
			return true
		}
	case "SQLite":
		dt := strings.ToUpper(normalizeAffinity(col.ColumnType))
		switch dt {
		case "INTEGER", "INT", "SMALLINT", "TINYINT", "MEDIUMINT", "BIGINT":
			return true
		}
	case "MSSQL":
		switch col.DataType {
		case "tinyint", "smallint", "int", "bigint":
			return true
		}
	}
	return false
}

// queryMinMax queries the MIN and MAX values of the chunk key column.
// Returns (min, max, hasRows, error). If the table is empty, hasRows is false.
func queryMinMax(ctx context.Context, source dbQuerier, src SourceDB, table Table, key ChunkKey) (int64, int64, bool, error) {
	query := fmt.Sprintf("SELECT MIN(%s), MAX(%s) FROM %s",
		src.QuoteIdentifier(key.SourceColumn),
		src.QuoteIdentifier(key.SourceColumn),
		src.QuoteIdentifier(table.SourceName))

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
