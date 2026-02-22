package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createTables generates and executes CREATE TABLE DDL for all tables.
// Tables are created with no PKs, FKs, or indexes for speed.
func createTables(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string, unlogged bool, preserveDefaults bool, typeMap TypeMappingConfig) error {
	for _, t := range schema.Tables {
		ddl, err := generateCreateTable(t, pgSchema, unlogged, preserveDefaults, typeMap)
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
func generateCreateTable(t Table, pgSchema string, unlogged bool, preserveDefaults bool, typeMap TypeMappingConfig) (string, error) {
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

		if preserveDefaults && col.Default != nil {
			dflt, err := mapDefault(col, pgType, typeMap)
			if err != nil {
				return "", fmt.Errorf("column %s default: %w", col.PGName, err)
			}
			if dflt != "" {
				fmt.Fprintf(&b, " DEFAULT %s", dflt)
			}
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

func mapDefault(col Column, pgType string, typeMap TypeMappingConfig) (string, error) {
	if col.Default == nil {
		return "", nil
	}

	raw := strings.TrimSpace(*col.Default)
	if strings.EqualFold(raw, "null") {
		return "", nil
	}

	lower := strings.ToLower(raw)
	switch lower {
	case "current_timestamp", "current_timestamp()", "now()", "localtimestamp", "localtimestamp()":
		return "CURRENT_TIMESTAMP", nil
	}

	if strings.HasPrefix(lower, "current_timestamp(") && strings.HasSuffix(lower, ")") {
		return strings.ToUpper(raw), nil
	}

	unquoted := mysqlDefaultUnquote(raw)

	switch {
	case pgType == "boolean":
		switch unquoted {
		case "0":
			return "FALSE", nil
		case "1":
			return "TRUE", nil
		default:
			return "", fmt.Errorf("unsupported boolean default %q", raw)
		}

	case isNumericType(pgType):
		if _, err := strconv.ParseFloat(unquoted, 64); err != nil {
			return "", fmt.Errorf("unsupported numeric default %q", raw)
		}
		return unquoted, nil

	case pgType == "json" || pgType == "jsonb":
		return fmt.Sprintf("%s::%s", pgLiteral(unquoted), pgType), nil

	case pgType == "bytea":
		return "", fmt.Errorf("bytea defaults are not supported (value %q)", raw)

	case strings.HasPrefix(pgType, "timestamp"), pgType == "date", strings.HasPrefix(pgType, "time"):
		return pgLiteral(unquoted), nil

	case strings.HasPrefix(pgType, "char"), strings.HasPrefix(pgType, "varchar"), pgType == "text", pgType == "uuid":
		if pgType == "uuid" && typeMap.Binary16AsUUID {
			// binary(16) uuid defaults are uncommon in MySQL and hard to infer safely from metadata.
			return "", fmt.Errorf("uuid defaults are not supported for binary16_as_uuid (value %q)", raw)
		}
		return pgLiteral(unquoted), nil

	default:
		// Safe fallback for mapped textual types.
		return pgLiteral(unquoted), nil
	}
}

func mysqlDefaultUnquote(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		inner := v[1 : len(v)-1]
		return strings.ReplaceAll(inner, "''", "'")
	}
	return v
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
