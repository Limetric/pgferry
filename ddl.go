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
func createTables(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string, unlogged bool, typeMap TypeMappingConfig) error {
	for _, t := range schema.Tables {
		ddl, err := generateCreateTable(t, pgSchema, unlogged, typeMap)
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
func generateCreateTable(t Table, pgSchema string, unlogged bool, typeMap TypeMappingConfig) (string, error) {
	var b strings.Builder
	tableKind := "TABLE"
	if unlogged {
		tableKind = "UNLOGGED TABLE"
	}
	fmt.Fprintf(&b, "CREATE %s %s.%s (\n", tableKind, pgIdent(pgSchema), pgIdent(t.PGName))

	for i, col := range t.Columns {
		pgType, err := mapType(col, typeMap)
		if err != nil {
			return "", fmt.Errorf("column %s: %w", col.PGName, err)
		}
		fmt.Fprintf(&b, "  %s %s", pgIdent(col.PGName), pgType)

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
