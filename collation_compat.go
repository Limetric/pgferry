package main

import (
	"fmt"
	"sort"
	"strings"
)

// isCICollation reports whether the collation name has a _ci suffix, indicating
// case-insensitive behavior.
func isCICollation(collation string) bool {
	return strings.HasSuffix(strings.ToLower(collation), "_ci")
}

// ciCollationHandled reports whether a _ci collation is being handled (either
// by ci_as_citext or by a collation_map entry), meaning warnings should be
// suppressed.
func ciCollationHandled(collation string, typeMap TypeMappingConfig) bool {
	if _, mapped := typeMap.CollationMap[collation]; mapped {
		return true
	}
	return typeMap.CIAsCitext
}

// pgTypeForCollation returns citext for text-like columns with _ci collations
// when ci_as_citext is enabled. If the collation has an explicit collation_map
// entry, the user chose COLLATE instead — return pgType unchanged.
func pgTypeForCollation(col Column, pgType string, typeMap TypeMappingConfig) string {
	if !typeMap.CIAsCitext {
		return pgType
	}
	if !isCICollation(col.Collation) {
		return pgType
	}
	if _, mapped := typeMap.CollationMap[col.Collation]; mapped {
		return pgType
	}
	if !isTextLikePGType(pgType) {
		return pgType
	}
	return "citext"
}

// collectCollationWarnings reports charset/collation information found in the
// introspected schema. It warns about case-insensitive collations (_ci suffix)
// that will become case-sensitive in PostgreSQL unless handled by collation_map
// or ci_as_citext.
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
			if isCICollation(col.Collation) {
				ciCounts[col.Collation]++
				if uniqueCols[col.PGName] {
					if !ciCollationHandled(col.Collation, typeMap) {
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
		if ciCollationHandled(coll, typeMap) {
			continue
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

	// _ci columns handled by citext don't need a COLLATE clause
	if typeMap.CIAsCitext && isCICollation(col.Collation) {
		return ""
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
