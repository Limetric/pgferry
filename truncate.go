package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

const discoverTargetTablesForTruncateSQL = `
SELECT COALESCE(
	jsonb_agg(
		jsonb_build_object(
			'schema', requested.schema_name,
			'exists', n.oid IS NOT NULL,
			'tables', COALESCE(t.tables, ARRAY[]::text[])
		)
		ORDER BY requested.ordinality
	),
	'[]'::jsonb
)::text
FROM unnest($1::text[]) WITH ORDINALITY AS requested(schema_name, ordinality)
LEFT JOIN pg_namespace n ON n.nspname = requested.schema_name
LEFT JOIN LATERAL (
	SELECT array_agg(format('%I.%I', n.nspname, c.relname) ORDER BY c.relname) AS tables
	FROM pg_class c
	WHERE c.relnamespace = n.oid
	  AND c.relkind IN ('r', 'p')
	  AND NOT EXISTS (
		SELECT 1
		FROM pg_inherits i
		WHERE i.inhrelid = c.oid
	  )
) t ON true`

type truncateTargetSchemaTables struct {
	Schema string   `json:"schema"`
	Exists bool     `json:"exists"`
	Tables []string `json:"tables"`
}

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

	var raw string
	if err := exec.QueryRow(ctx, discoverTargetTablesForTruncateSQL, schemas).Scan(&raw); err != nil {
		return fmt.Errorf("discover target tables to truncate: %w", err)
	}
	var schemaTables []truncateTargetSchemaTables
	if err := json.Unmarshal([]byte(raw), &schemaTables); err != nil {
		return fmt.Errorf("decode target tables to truncate: %w", err)
	}

	targets := make([]string, 0)
	for _, schema := range schemaTables {
		if !schema.Exists {
			return fmt.Errorf("truncate_before_copy=once schema %q was not found in the target database", schema.Schema)
		}
		if len(schema.Tables) == 0 {
			return fmt.Errorf("truncate_before_copy=once schema %q has no target tables to truncate", schema.Schema)
		}
		targets = append(targets, schema.Tables...)
	}

	q := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", strings.Join(targets, ", "))
	log.Printf("truncate_before_copy=once: truncating target tables with CASCADE: %s", strings.Join(targets, ", "))
	return execSQL(ctx, exec, "truncate target tables before copy once", q)
}

func runTruncateBeforeCopy(ctx context.Context, exec queryExecutor, cfg *MigrationConfig, schema *Schema) error {
	switch cfg.TruncateBeforeCopy {
	case truncateBeforeCopyOff:
		return nil
	case truncateBeforeCopyPerRun:
		return truncateTargetTablesBeforeCopy(ctx, exec, schema, cfg.Schema)
	case truncateBeforeCopyOnce:
		return truncateTargetTablesOnceBeforeCopy(ctx, exec, cfg.TruncateBeforeCopySchemas)
	default:
		return fmt.Errorf("internal: unhandled truncate_before_copy mode %q", cfg.TruncateBeforeCopy)
	}
}
