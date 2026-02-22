package main

import "fmt"

// isGeneratedColumn detects MySQL generated columns from the Extra field.
// This is MySQL-specific but safe for other sources since their Extra field won't match.
func isGeneratedColumn(col Column) bool {
	return isMySQLGeneratedColumn(col)
}

func collectGeneratedColumnWarnings(schema *Schema) []string {
	if schema == nil {
		return nil
	}

	var warnings []string
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if !isGeneratedColumn(col) {
				continue
			}
			warnings = append(warnings, fmt.Sprintf(
				"generated column %s.%s (%s) will be materialized as plain data; generation expression is not recreated",
				t.SourceName, col.SourceName, col.Extra,
			))
		}
	}
	return warnings
}
