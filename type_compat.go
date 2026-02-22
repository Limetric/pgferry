package main

import "fmt"

func collectUnsupportedTypeErrors(schema *Schema, typeMap TypeMappingConfig) []string {
	if schema == nil {
		return nil
	}

	var errs []string
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if _, err := mapType(col, typeMap); err != nil {
				errs = append(errs, fmt.Sprintf("%s.%s (%s): %v", t.MySQLName, col.MySQLName, col.ColumnType, err))
			}
		}
	}
	return errs
}
