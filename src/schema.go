package main

import (
	"database/sql"
	"unicode"
)

// pgReservedWords are PostgreSQL reserved words that must be quoted as identifiers.
var pgReservedWords = map[string]bool{
	"all": true, "analyse": true, "analyze": true, "and": true, "any": true,
	"array": true, "as": true, "asc": true, "authorization": true, "between": true,
	"binary": true, "both": true, "case": true, "cast": true, "check": true,
	"collate": true, "column": true, "constraint": true, "create": true, "cross": true,
	"current_date": true, "current_role": true, "current_time": true,
	"current_timestamp": true, "current_user": true, "default": true, "deferrable": true,
	"desc": true, "distinct": true, "do": true, "else": true, "end": true, "except": true,
	"false": true, "fetch": true, "for": true, "foreign": true, "freeze": true,
	"from": true, "full": true, "grant": true, "group": true, "having": true,
	"ilike": true, "in": true, "initially": true, "inner": true, "intersect": true,
	"into": true, "is": true, "isnull": true, "join": true, "lateral": true,
	"leading": true, "left": true, "like": true, "limit": true, "localtime": true,
	"localtimestamp": true, "natural": true, "not": true, "notnull": true, "null": true,
	"offset": true, "on": true, "only": true, "or": true, "order": true, "outer": true,
	"overlaps": true, "placing": true, "primary": true, "references": true,
	"returning": true, "right": true, "select": true, "session_user": true,
	"similar": true, "some": true, "symmetric": true, "table": true, "then": true,
	"to": true, "trailing": true, "true": true, "union": true, "unique": true,
	"user": true, "using": true, "variadic": true, "verbose": true, "when": true,
	"where": true, "window": true, "with": true,
}

// toSnakeCase converts camelCase/PascalCase to snake_case.
// Consecutive uppercase runs are treated as acronyms:
// "nameASCII" → "name_ascii", "HTMLParser" → "html_parser".
func toSnakeCase(s string) string {
	runes := []rune(s)
	var result []byte
	for i, r := range runes {
		if unicode.IsUpper(r) {
			if i > 0 {
				prev := runes[i-1]
				if unicode.IsLower(prev) || unicode.IsDigit(prev) {
					// lowercase/digit → uppercase boundary: "name|A"
					result = append(result, '_')
				} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
					// uppercase run ending before lowercase: "HTM|L|Parser" → split before L
					result = append(result, '_')
				}
			}
			result = append(result, byte(unicode.ToLower(r)))
		} else {
			result = append(result, byte(r))
		}
	}
	return string(result)
}

// pgNeedsQuoting reports whether a PG identifier needs quoting beyond
// reserved-word checks (e.g. contains hyphens, spaces, uppercase, etc.).
func pgNeedsQuoting(name string) bool {
	for i, r := range name {
		if r >= 'a' && r <= 'z' || r == '_' {
			continue
		}
		if i > 0 && (r >= '0' && r <= '9' || r == '$') {
			continue
		}
		return true
	}
	return false
}

// pgIdent returns a PG-safe identifier, quoting reserved words and names
// that contain characters invalid in unquoted identifiers.
func pgIdent(name string) string {
	if pgReservedWords[name] || pgNeedsQuoting(name) {
		return `"` + name + `"`
	}
	return name
}

// collectStringRows is a helper to collect single-column string results.
func collectStringRows(db *sql.DB, query, param string, out *[]string) error {
	rows, err := db.Query(query, param)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		*out = append(*out, v)
	}
	return rows.Err()
}
