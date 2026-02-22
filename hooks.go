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
// and semicolons inside quotes/comments/dollar-quoted blocks.
func splitStatements(sql string) []string {
	var stmts []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	inLineComment := false
	blockCommentDepth := 0
	dollarTag := ""

	for i := 0; i < len(sql); i++ {
		c := sql[i]

		// Inside -- line comment
		if inLineComment {
			current.WriteByte(c)
			if c == '\n' {
				inLineComment = false
			}
			continue
		}

		// Inside /* ... */ block comment (nested)
		if blockCommentDepth > 0 {
			current.WriteByte(c)
			if c == '/' && i+1 < len(sql) && sql[i+1] == '*' {
				current.WriteByte(sql[i+1])
				i++
				blockCommentDepth++
				continue
			}
			if c == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				current.WriteByte(sql[i+1])
				i++
				blockCommentDepth--
			}
			continue
		}

		// Inside single-quoted literal
		if inSingleQuote {
			current.WriteByte(c)
			if c == '\'' {
				// Handle escaped quotes ('')
				if i+1 < len(sql) && sql[i+1] == '\'' {
					current.WriteByte(sql[i+1])
					i++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		// Inside double-quoted identifier
		if inDoubleQuote {
			current.WriteByte(c)
			if c == '"' {
				// Handle escaped double quote ("")
				if i+1 < len(sql) && sql[i+1] == '"' {
					current.WriteByte(sql[i+1])
					i++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		// Inside dollar-quoted body
		if dollarTag != "" {
			if strings.HasPrefix(sql[i:], dollarTag) {
				current.WriteString(dollarTag)
				i += len(dollarTag) - 1
				dollarTag = ""
				continue
			}
			current.WriteByte(c)
			continue
		}

		// Not inside any quoted/commented context.
		switch {
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			current.WriteByte(c)
			current.WriteByte(sql[i+1])
			i++
			inLineComment = true
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			current.WriteByte(c)
			current.WriteByte(sql[i+1])
			i++
			blockCommentDepth = 1
		case c == '\'':
			current.WriteByte(c)
			inSingleQuote = true
		case c == '"':
			current.WriteByte(c)
			inDoubleQuote = true
		case c == '$':
			if tag, ok := parseDollarTag(sql, i); ok {
				current.WriteString(tag)
				i += len(tag) - 1
				dollarTag = tag
				continue
			}
			current.WriteByte(c)
		case c == ';':
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

func parseDollarTag(sql string, i int) (string, bool) {
	if i >= len(sql) || sql[i] != '$' {
		return "", false
	}
	// $$...$$
	if i+1 < len(sql) && sql[i+1] == '$' {
		return "$$", true
	}

	// $tag$...$tag$ where tag uses identifier chars.
	j := i + 1
	if j >= len(sql) || !isDollarTagStart(sql[j]) {
		return "", false
	}
	for j < len(sql) && isDollarTagChar(sql[j]) {
		j++
	}
	if j < len(sql) && sql[j] == '$' {
		return sql[i : j+1], true
	}
	return "", false
}

func isDollarTagStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDollarTagChar(c byte) bool {
	return isDollarTagStart(c) || (c >= '0' && c <= '9')
}
