package main

import (
	"context"
	"fmt"
	"strings"
)

func truncateTargetTablesBeforeCopy(ctx context.Context, exec statementExecutor, schema *Schema, pgSchema string) error {
	if schema == nil || len(schema.Tables) == 0 {
		return nil
	}

	targets := make([]string, len(schema.Tables))
	for i, t := range schema.Tables {
		targets[i] = fmt.Sprintf("%s.%s", pgIdent(pgSchema), pgIdent(t.PGName))
	}

	q := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", strings.Join(targets, ", "))
	return execSQL(ctx, exec, "truncate target tables before copy", q)
}
