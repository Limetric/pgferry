package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"sort"
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
	typeMap = effectiveTypeMappingForSource(typeMap, "mysql")
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
		// Schema-qualify custom enum types created by createEnumTypes
		if typeMap.EnumMode == "native" && col.DataType == "enum" {
			pgType = fmt.Sprintf("%s.%s", pgIdent(pgSchema), pgIdent(pgType))
		}
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

		setCheck, err := setArrayCheckClause(col, typeMap)
		if err != nil {
			return "", fmt.Errorf("column %s set check: %w", col.PGName, err)
		}
		if setCheck != "" {
			b.WriteByte(' ')
			b.WriteString(setCheck)
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

// pgEnumTypeName returns a deterministic PostgreSQL enum type name derived from
// the enum values. Identical value sets produce the same type name, enabling reuse.
func pgEnumTypeName(values []string) string {
	sorted := make([]string, len(values))
	copy(sorted, values)
	sort.Strings(sorted)

	h := fnv.New64a()
	for _, v := range sorted {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("pgferry_enum_%016x", h.Sum64())
}

// createEnumTypes creates PostgreSQL enum types for all enum columns in the schema.
// Identical enum definitions (same value sets) share the same PG type.
func createEnumTypes(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string, typeMap TypeMappingConfig) error {
	typeMap = effectiveTypeMappingForSource(typeMap, "mysql")
	if typeMap.EnumMode != "native" {
		return nil
	}

	created := make(map[string]bool)
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if col.DataType != "enum" {
				continue
			}
			values, err := parseMySQLEnumSetValues(col.ColumnType)
			if err != nil {
				return fmt.Errorf("parse enum values for %s.%s: %w", t.PGName, col.PGName, err)
			}
			typeName := pgEnumTypeName(values)
			if created[typeName] {
				continue
			}
			// Sort values to match the hash order used by pgEnumTypeName,
			// ensuring deterministic PG enum declaration order regardless of
			// which column definition is encountered first.
			sorted := make([]string, len(values))
			copy(sorted, values)
			sort.Strings(sorted)
			lits := make([]string, len(sorted))
			for i, v := range sorted {
				lits[i] = pgLiteral(v)
			}
			// Use DO block with EXCEPTION handler so the statement is safe
			// to re-run on a resumed migration where the type already exists.
			q := fmt.Sprintf(
				"DO $$ BEGIN CREATE TYPE %s.%s AS ENUM (%s); EXCEPTION WHEN duplicate_object THEN NULL; END $$",
				pgIdent(pgSchema), pgIdent(typeName), strings.Join(lits, ", "))
			if _, err := pool.Exec(ctx, q); err != nil {
				return fmt.Errorf("create enum type %s: %w\nSQL: %s", typeName, err, q)
			}
			created[typeName] = true
			log.Printf("    enum type %s (%s)", typeName, strings.Join(lits, ", "))
		}
	}
	return nil
}

func enumCheckClause(col Column, typeMap TypeMappingConfig) (string, error) {
	typeMap = effectiveTypeMappingForSource(typeMap, "mysql")
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

// setArrayCheckClause generates a CHECK constraint ensuring every array element
// is one of the allowed source SET members (text_array_check mode only).
func setArrayCheckClause(col Column, typeMap TypeMappingConfig) (string, error) {
	if col.DataType != "set" || typeMap.SetMode != "text_array_check" {
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
	return fmt.Sprintf("CHECK (%s <@ ARRAY[%s]::text[])", pgIdent(col.PGName), strings.Join(lits, ", ")), nil
}
