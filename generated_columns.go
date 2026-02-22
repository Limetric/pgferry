package main

import "fmt"

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
				t.MySQLName, col.MySQLName, col.Extra,
			))
		}
	}
	return warnings
}
