package main

import (
	"fmt"
	"strings"
)

// isGeneratedColumn detects generated columns from the Extra field.
// Works for MySQL, SQLite, and MSSQL sources.
func isGeneratedColumn(col Column) bool {
	if isMySQLGeneratedColumn(col) {
		return true
	}
	return strings.EqualFold(col.Extra, "COMPUTED")
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
