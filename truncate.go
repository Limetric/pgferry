package main

import (
	"context"
	"fmt"
	"log"
	"strings"
)

const discoverTargetTablesForTruncateSQL = `
SELECT COALESCE(
	array_agg(format('%I.%I', n.nspname, c.relname) ORDER BY n.nspname, c.relname),
	ARRAY[]::text[]
)
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = ANY($1)
  AND c.relkind IN ('r', 'p')
  AND NOT EXISTS (
	SELECT 1
	FROM pg_inherits i
	WHERE i.inhrelid = c.oid
  )`

func truncateTargetTablesBeforeCopy(ctx context.Context, exec statementExecutor, schema *Schema, pgSchema string) error {
	if schema == nil || len(schema.Tables) == 0 {
		return nil
	}

	targets := make([]string, len(schema.Tables))
	for i, t := range schema.Tables {
		targets[i] = fmt.Sprintf("%s.%s", pgIdent(pgSchema), pgIdent(t.PGName))
	}

	q := fmt.Sprintf("TRUNCATE TABLE %s", strings.Join(targets, ", "))
	log.Printf("truncating target tables before COPY without CASCADE: %s", strings.Join(targets, ", "))
	return execSQL(ctx, exec, "truncate target tables before copy", q)
}

func truncateTargetTablesOnceBeforeCopy(ctx context.Context, exec queryExecutor, schemas []string) error {
	if len(schemas) == 0 {
		return nil
	}

	var targets []string
	if err := exec.QueryRow(ctx, discoverTargetTablesForTruncateSQL, schemas).Scan(&targets); err != nil {
		return fmt.Errorf("discover target tables to truncate: %w", err)
	}
	if len(targets) == 0 {
		log.Printf("truncate_before_copy=once: no target tables found in schemas: %s", strings.Join(quoteSchemaNamesForLog(schemas), ", "))
		return nil
	}

	q := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", strings.Join(targets, ", "))
	log.Printf("truncate_before_copy=once: truncating target tables with CASCADE: %s", strings.Join(targets, ", "))
	return execSQL(ctx, exec, "truncate target tables before copy once", q)
}

func quoteSchemaNamesForLog(schemas []string) []string {
	quoted := make([]string, len(schemas))
	for i, schema := range schemas {
		quoted[i] = pgIdent(schema)
	}
	return quoted
}
