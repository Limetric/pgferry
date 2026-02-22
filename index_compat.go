package main

import "fmt"

func indexUnsupportedReason(idx Index) (string, bool) {
	if idx.HasExpression {
		return "expression index key-parts are not currently supported", true
	}
	if idx.HasPrefix {
		return "prefix indexes (SUB_PART) are not currently supported", true
	}
	if idx.Type != "" && idx.Type != "BTREE" {
		return fmt.Sprintf("index type %q is not supported", idx.Type), true
	}
	if len(idx.Columns) == 0 {
		return "index has no plain column key-parts", true
	}
	return "", false
}

func collectIndexCompatibilityWarnings(schema *Schema) []string {
	var warnings []string
	for _, t := range schema.Tables {
		for _, idx := range t.Indexes {
			if reason, unsupported := indexUnsupportedReason(idx); unsupported {
				warnings = append(warnings,
					fmt.Sprintf("%s.%s (%s): %s", t.MySQLName, idx.MySQLName, idx.Name, reason),
				)
			}
		}
	}
	return warnings
}
