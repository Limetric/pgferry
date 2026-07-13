package main

import "strings"

// sqliteCheckConstraint is a CHECK constraint recovered from a table's DDL.
type sqliteCheckConstraint struct {
	Name string // declared CONSTRAINT name, empty when anonymous
	Expr string // the parenthesized expression, without the outer parentheses
}

// extractSQLiteCheckConstraints pulls CHECK constraints out of a CREATE TABLE
// statement. SQLite exposes no catalog view for them — they exist only as text in
// sqlite_master.sql — so the DDL is scanned directly. String literals, quoted
// identifiers ('..', "..", [..], `..`) and comments are skipped so a CHECK
// appearing inside them is not mistaken for a constraint.
func extractSQLiteCheckConstraints(ddl string) []sqliteCheckConstraint {
	var out []sqliteCheckConstraint
	pendingName := ""

	i := 0
	for i < len(ddl) {
		c := ddl[i]

		switch {
		case c == '\'', c == '"', c == '`':
			i = skipSQLiteQuoted(ddl, i, c, c)
			continue
		case c == '[':
			i = skipSQLiteQuoted(ddl, i, '[', ']')
			continue
		case c == '-' && i+1 < len(ddl) && ddl[i+1] == '-':
			for i < len(ddl) && ddl[i] != '\n' {
				i++
			}
			continue
		case c == '/' && i+1 < len(ddl) && ddl[i+1] == '*':
			i += 2
			for i+1 < len(ddl) && !(ddl[i] == '*' && ddl[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}

		if !isSQLiteWordByte(c) {
			i++
			continue
		}

		start := i
		for i < len(ddl) && isSQLiteWordByte(ddl[i]) {
			i++
		}
		word := ddl[start:i]

		switch {
		case strings.EqualFold(word, "CONSTRAINT"):
			name, next := readSQLiteIdentifier(ddl, i)
			pendingName = name
			i = next
		case strings.EqualFold(word, "CHECK"):
			expr, next, ok := readSQLiteParenExpr(ddl, i)
			if !ok {
				pendingName = ""
				continue
			}
			out = append(out, sqliteCheckConstraint{Name: pendingName, Expr: strings.TrimSpace(expr)})
			pendingName = ""
			i = next
		default:
			// Any other keyword ends a pending CONSTRAINT <name> that turned out not
			// to introduce a CHECK (a named PRIMARY KEY or UNIQUE, for example).
			if !strings.EqualFold(word, "NOT") && !strings.EqualFold(word, "NULL") {
				pendingName = ""
			}
		}
	}

	return out
}

// skipSQLiteQuoted returns the index just past a quoted run that starts at i with
// open, ending at the matching close. A doubled close quote is an escape.
func skipSQLiteQuoted(s string, i int, open, close byte) int {
	if i >= len(s) || s[i] != open {
		return i + 1
	}
	i++
	for i < len(s) {
		if s[i] == close {
			if i+1 < len(s) && s[i+1] == close {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

// readSQLiteIdentifier reads the next identifier at or after i, unwrapping quotes.
func readSQLiteIdentifier(s string, i int) (string, int) {
	for i < len(s) && isSQLiteSpaceByte(s[i]) {
		i++
	}
	if i >= len(s) {
		return "", i
	}

	switch s[i] {
	case '"', '\'', '`':
		quote := s[i]
		end := skipSQLiteQuoted(s, i, quote, quote)
		inner := s[i+1 : max(i+1, end-1)]
		return strings.ReplaceAll(inner, string(quote)+string(quote), string(quote)), end
	case '[':
		end := skipSQLiteQuoted(s, i, '[', ']')
		return s[i+1 : max(i+1, end-1)], end
	}

	start := i
	for i < len(s) && isSQLiteWordByte(s[i]) {
		i++
	}
	return s[start:i], i
}

// readSQLiteParenExpr reads a parenthesized expression starting at or after i,
// returning its contents without the outer parentheses. Nested parentheses,
// string literals and quoted identifiers are respected.
func readSQLiteParenExpr(s string, i int) (string, int, bool) {
	for i < len(s) && isSQLiteSpaceByte(s[i]) {
		i++
	}
	if i >= len(s) || s[i] != '(' {
		return "", i, false
	}

	depth := 0
	start := i + 1
	for i < len(s) {
		switch c := s[i]; {
		case c == '\'', c == '"', c == '`':
			i = skipSQLiteQuoted(s, i, c, c)
			continue
		case c == '[':
			i = skipSQLiteQuoted(s, i, '[', ']')
			continue
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return s[start:i], i + 1, true
			}
		}
		i++
	}
	return "", i, false
}

func isSQLiteWordByte(c byte) bool {
	return c == '_' || c == '$' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

func isSQLiteSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
