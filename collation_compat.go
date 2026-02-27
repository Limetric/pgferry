package main

import (
	"fmt"
	"sort"
	"strings"
)

// collectCollationWarnings reports charset/collation information found in the
// introspected schema. It warns about case-insensitive collations (_ci suffix)
// that will become case-sensitive in PostgreSQL unless a collation_map entry
// overrides them.
func collectCollationWarnings(schema *Schema, typeMap TypeMappingConfig) []string {
	charsets := make(map[string]bool)
	collations := make(map[string]bool)
	// _ci collation → count of text-like columns using it
	ciCounts := make(map[string]int)
	// _ci collation → list of "table.column" with unique/PK indexes
	ciUniqueRefs := make(map[string][]string)

	// Build a lookup: table → set of columns in unique indexes (including PK)
	for _, t := range schema.Tables {
		uniqueCols := make(map[string]bool)
		if t.PrimaryKey != nil {
			for _, c := range t.PrimaryKey.Columns {
				uniqueCols[c] = true
			}
		}
		for _, idx := range t.Indexes {
			if idx.Unique {
				for _, c := range idx.Columns {
					uniqueCols[c] = true
				}
			}
		}

		for _, col := range t.Columns {
			if col.Charset != "" {
				charsets[col.Charset] = true
			}
			if col.Collation == "" {
				continue
			}
			collations[col.Collation] = true
			if strings.HasSuffix(strings.ToLower(col.Collation), "_ci") {
				ciCounts[col.Collation]++
				if uniqueCols[col.PGName] {
					// Check if user provided a mapping — if so, suppress unique index warning
					if _, mapped := typeMap.CollationMap[col.Collation]; !mapped {
						ciUniqueRefs[col.Collation] = append(ciUniqueRefs[col.Collation],
							fmt.Sprintf("%s.%s", t.PGName, col.PGName))
					}
				}
			}
		}
	}

	var warnings []string

	// Summary of distinct charsets found
	if len(charsets) > 0 {
		sorted := sortedKeys(charsets)
		warnings = append(warnings, fmt.Sprintf("source charsets found: %s", strings.Join(sorted, ", ")))
	}

	// Summary of distinct collations found
	if len(collations) > 0 {
		sorted := sortedKeys(collations)
		warnings = append(warnings, fmt.Sprintf("source collations found: %s", strings.Join(sorted, ", ")))
	}

	// Warn about _ci collations (case-insensitive → case-sensitive in PG)
	for _, coll := range sortedKeys(ciCounts) {
		if _, mapped := typeMap.CollationMap[coll]; mapped {
			continue // user provided a mapping; suppress warning
		}
		warnings = append(warnings, fmt.Sprintf(
			"%d column(s) use %s (case-insensitive); PostgreSQL text comparisons are case-sensitive by default",
			ciCounts[coll], coll))
	}

	// Warn about unique indexes on _ci columns without mappings
	for _, coll := range sortedKeys(ciUniqueRefs) {
		refs := ciUniqueRefs[coll]
		warnings = append(warnings, fmt.Sprintf(
			"unique index/PK on %s column(s) with %s — uniqueness semantics may differ: %s",
			coll, coll, strings.Join(refs, ", ")))
	}

	return warnings
}

// pgCollationClause returns a COLLATE clause for a column if collation_mode=auto.
// Returns "" when no clause should be added.
func pgCollationClause(col Column, typeMap TypeMappingConfig) string {
	if typeMap.CollationMode != "auto" {
		return ""
	}
	if col.Collation == "" {
		return ""
	}

	// User-provided mapping takes precedence
	if mapped, ok := typeMap.CollationMap[col.Collation]; ok {
		return fmt.Sprintf(`COLLATE "%s"`, mapped)
	}

	// _bin suffix → deterministic binary collation
	if strings.HasSuffix(strings.ToLower(col.Collation), "_bin") {
		return `COLLATE "C"`
	}

	// For other collations (including _ci), we don't emit a clause.
	// The warning system notifies the user about the semantic difference.
	return ""
}

// isTextLikePGType reports whether a PostgreSQL type is text-like and can
// accept a COLLATE clause.
func isTextLikePGType(pgType string) bool {
	lower := strings.ToLower(pgType)
	switch {
	case lower == "text":
		return true
	case strings.HasPrefix(lower, "varchar"):
		return true
	case strings.HasPrefix(lower, "char"):
		return true
	default:
		return false
	}
}

// sortedKeys returns the keys of a map in sorted order. Works with map[string]bool
// and map[string]int via the generic helper pattern.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
