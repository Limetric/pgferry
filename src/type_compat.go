package main

import "fmt"

// typeMapper is a function that maps a source column to a PostgreSQL type.
type typeMapper func(col Column, typeMap TypeMappingConfig) (string, error)

func collectUnsupportedTypeErrors(schema *Schema, typeMap TypeMappingConfig, mapper typeMapper) []string {
	if schema == nil {
		return nil
	}

	var errs []string
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if _, err := mapper(col, typeMap); err != nil {
				errs = append(errs, fmt.Sprintf("%s.%s (%s): %v", t.SourceName, col.SourceName, col.ColumnType, err))
			}
		}
	}
	return errs
}
