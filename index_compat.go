package main

import "fmt"

func indexUnsupportedReason(table Table, idx Index, typeMap TypeMappingConfig) (string, bool) {
	if idx.HasExpression {
		return "expression index key-parts are not currently supported", true
	}
	if idx.HasPrefix {
		return "prefix indexes (SUB_PART) are not currently supported", true
	}
	if len(idx.Columns) == 0 {
		return "index has no plain column key-parts", true
	}
	if idx.Type == "SPATIAL" && isMySQLSpatialIndex(table, idx) {
		if len(idx.Columns) != 1 {
			return "multi-column SPATIAL indexes are not currently supported", true
		}
		if idx.Unique {
			return "unique SPATIAL indexes are not currently supported", true
		}
		if !typeMap.UsePostGIS {
			return "SPATIAL indexes require [postgis].enabled = true", true
		}
		return "", false
	}
	if idx.Type != "" && idx.Type != "BTREE" {
		return fmt.Sprintf("index type %q is not supported", idx.Type), true
	}
	return "", false
}

func collectIndexCompatibilityWarnings(schema *Schema, typeMap TypeMappingConfig) []string {
	var warnings []string
	for _, t := range schema.Tables {
		for _, idx := range t.Indexes {
			if reason, unsupported := indexUnsupportedReason(t, idx, typeMap); unsupported {
				warnings = append(warnings,
					fmt.Sprintf("%s.%s (%s): %s", t.SourceName, idx.SourceName, idx.Name, reason),
				)
			}
		}
	}
	return warnings
}

func isMySQLSpatialIndex(table Table, idx Index) bool {
	for _, name := range idx.Columns {
		col, ok := findColumnByPGName(table, name)
		if ok && isMySQLSpatialType(col.DataType) {
			return true
		}
	}
	return false
}

func findColumnByPGName(table Table, pgName string) (Column, bool) {
	for _, col := range table.Columns {
		if col.PGName == pgName {
			return col, true
		}
	}
	return Column{}, false
}
