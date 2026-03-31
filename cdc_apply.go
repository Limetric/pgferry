package main

import (
	"fmt"
	"strings"
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
