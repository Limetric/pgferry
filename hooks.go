package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// loadAndExecSQLFiles reads each SQL file, expands {{schema}}, and executes every statement.
func loadAndExecSQLFiles(ctx context.Context, pool *pgxpool.Pool, cfg *MigrationConfig, files []string, phase string) error {
	if len(files) == 0 {
		return nil
	}
	log.Printf("  running %s hooks (%d files)...", phase, len(files))

	for _, f := range files {
		path := cfg.resolvePath(f)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("hook %s: read %s: %w", phase, f, err)
		}

		sql := strings.ReplaceAll(string(data), "{{schema}}", cfg.Schema)
		stmts := splitStatements(sql)

		log.Printf("    %s: %d statements", f, len(stmts))
		for i, stmt := range stmts {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("hook %s: %s: statement %d: %w\nSQL: %s", phase, f, i+1, err, stmt)
			}
		}
	}
	return nil
}

// splitStatements splits SQL text on semicolons, ignoring empty entries
// and content inside single-quoted strings.
func splitStatements(sql string) []string {
	var stmts []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch {
		case c == '\'' && !inQuote:
			inQuote = true
			current.WriteByte(c)
		case c == '\'' && inQuote:
			// Handle escaped quotes ('')
			if i+1 < len(sql) && sql[i+1] == '\'' {
				current.WriteByte(c)
				current.WriteByte(c)
				i++
			} else {
				inQuote = false
				current.WriteByte(c)
			}
		case c == ';' && !inQuote:
			s := strings.TrimSpace(current.String())
			if s != "" {
				stmts = append(stmts, s)
			}
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}

	// Trailing statement without semicolon
	if s := strings.TrimSpace(current.String()); s != "" {
		stmts = append(stmts, s)
	}

	return stmts
}
