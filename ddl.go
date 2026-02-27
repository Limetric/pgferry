package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createTables generates and executes CREATE TABLE DDL for all tables.
// Tables are created with no PKs, FKs, or indexes for speed.
func createTables(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string, unlogged bool, preserveDefaults bool, typeMap TypeMappingConfig, src SourceDB) error {
	for _, t := range schema.Tables {
		ddl, err := generateCreateTable(t, pgSchema, unlogged, preserveDefaults, typeMap, src)
		if err != nil {
			return fmt.Errorf("build create table %s: %w", t.PGName, err)
		}
		log.Printf("  creating %s.%s", pgSchema, t.PGName)
		if _, err := pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("create table %s: %w\nDDL: %s", t.PGName, err, ddl)
		}
	}
	return nil
}

// generateCreateTable produces a CREATE TABLE statement.
func generateCreateTable(t Table, pgSchema string, unlogged bool, preserveDefaults bool, typeMap TypeMappingConfig, src SourceDB) (string, error) {
	var b strings.Builder
	tableKind := "TABLE"
	if unlogged {
		tableKind = "UNLOGGED TABLE"
	}
	fmt.Fprintf(&b, "CREATE %s %s.%s (\n", tableKind, pgIdent(pgSchema), pgIdent(t.PGName))

	for i, col := range t.Columns {
		pgType, err := src.MapType(col, typeMap)
		if err != nil {
			return "", fmt.Errorf("column %s: %w", col.PGName, err)
		}
		pgType = pgTypeForCollation(col, pgType, typeMap)
		fmt.Fprintf(&b, "  %s %s", pgIdent(col.PGName), pgType)

		if isTextLikePGType(pgType) {
			if collate := pgCollationClause(col, typeMap); collate != "" {
				fmt.Fprintf(&b, " %s", collate)
			}
		}

		if preserveDefaults && col.Default != nil {
			dflt, err := src.MapDefault(col, pgType, typeMap)
			if err != nil {
				return "", fmt.Errorf("column %s default: %w", col.PGName, err)
			}
			if dflt != "" {
				fmt.Fprintf(&b, " DEFAULT %s", dflt)
			}
		}

		checkClause, err := enumCheckClause(col, typeMap)
		if err != nil {
			return "", fmt.Errorf("column %s enum check: %w", col.PGName, err)
		}
		if checkClause != "" {
			b.WriteByte(' ')
			b.WriteString(checkClause)
		}

		if !col.Nullable {
			b.WriteString(" NOT NULL")
		}

		if i < len(t.Columns)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}

	b.WriteString(")")
	return b.String(), nil
}

// ensureCitextExtension creates the citext extension if it does not exist.
func ensureCitextExtension(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS citext")
	if err != nil {
		return fmt.Errorf("create citext extension: %w", err)
	}
	return nil
}

func pgLiteral(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func isNumericType(pgType string) bool {
	switch {
	case pgType == "smallint", pgType == "integer", pgType == "bigint", pgType == "real", pgType == "double precision":
		return true
	case strings.HasPrefix(pgType, "numeric("), pgType == "numeric":
		return true
	default:
		return false
	}
}

func enumCheckClause(col Column, typeMap TypeMappingConfig) (string, error) {
	if col.DataType != "enum" || typeMap.EnumMode != "check" {
		return "", nil
	}
	values, err := parseMySQLEnumSetValues(col.ColumnType)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return "", nil
	}
	lits := make([]string, len(values))
	for i, v := range values {
		lits[i] = pgLiteral(v)
	}
	return fmt.Sprintf("CHECK (%s IN (%s))", pgIdent(col.PGName), strings.Join(lits, ", ")), nil
}
