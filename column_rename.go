package main

import (
	"fmt"
	"sort"
	"strings"
)

func hasColumnRenames(cfg *MigrationConfig) bool {
	return cfg != nil && len(cfg.ColumnRenames) > 0
}

func applyColumnRenames(schema *Schema, cfg *MigrationConfig) (*Schema, error) {
	if schema == nil {
		return nil, nil
	}
	if !hasColumnRenames(cfg) {
		return schema, nil
	}

	renamed := &Schema{Tables: make([]Table, len(schema.Tables))}
	tableIndexes := make(map[string]int, len(schema.Tables))
	for i, table := range schema.Tables {
		renamed.Tables[i] = cloneTable(table)
		key := normalizeTableFilterKey(table.SourceName)
		if prev, ok := tableIndexes[key]; ok {
			return nil, fmt.Errorf("source schema contains ambiguous table names %q and %q; column_renames match source tables case-insensitively", schema.Tables[prev].SourceName, table.SourceName)
		}
		tableIndexes[key] = i
	}

	entries := make([]string, 0, len(cfg.ColumnRenames))
	for entry := range cfg.ColumnRenames {
		entries = append(entries, entry)
	}
	sort.Strings(entries)

	renamesByTable := make(map[int]map[string]string)
	seenSourceColumns := make(map[string]string, len(entries))
	var missingTables []string
	var missingColumns []string

	for _, entry := range entries {
		targetName := strings.TrimSpace(cfg.ColumnRenames[entry])
		if targetName == "" {
			return nil, fmt.Errorf("column_renames entry %q has an empty target column name", entry)
		}
		if len(targetName) > postgresMaxIdentifierBytes {
			return nil, fmt.Errorf("column_renames entry %q: target name %q exceeds PostgreSQL's %d-byte identifier limit", entry, targetName, postgresMaxIdentifierBytes)
		}

		tableName, columnName, err := splitColumnRenameEntry(entry)
		if err != nil {
			return nil, fmt.Errorf("column_renames entry %q must be qualified as TableName.ColumnName", entry)
		}

		tableIdx, ok := tableIndexes[normalizeTableFilterKey(tableName)]
		if !ok {
			missingTables = append(missingTables, entry)
			continue
		}

		colIdx := findSourceColumnIndex(renamed.Tables[tableIdx], columnName)
		if colIdx < 0 {
			missingColumns = append(missingColumns, entry)
			continue
		}

		sourceKey := fmt.Sprintf("%s.%s", normalizeTableFilterKey(renamed.Tables[tableIdx].SourceName), normalizeTableFilterKey(renamed.Tables[tableIdx].Columns[colIdx].SourceName))
		if prev, ok := seenSourceColumns[sourceKey]; ok {
			return nil, fmt.Errorf("column_renames entries %q and %q both target source column %s", prev, entry, sourceKey)
		}
		seenSourceColumns[sourceKey] = entry

		oldPGName := renamed.Tables[tableIdx].Columns[colIdx].PGName
		renamed.Tables[tableIdx].Columns[colIdx].PGName = targetName
		if renamesByTable[tableIdx] == nil {
			renamesByTable[tableIdx] = make(map[string]string)
		}
		renamesByTable[tableIdx][normalizeTableFilterKey(oldPGName)] = targetName
	}

	if len(missingTables) > 0 {
		sort.Strings(missingTables)
		return nil, fmt.Errorf("column_renames entries did not match any source table in the migrated schema (table may have been excluded by table filters): %s", strings.Join(missingTables, ", "))
	}
	if len(missingColumns) > 0 {
		sort.Strings(missingColumns)
		return nil, fmt.Errorf("column_renames entries did not match any source column on matched source tables: %s", strings.Join(missingColumns, ", "))
	}

	for i := range renamed.Tables {
		remapTableColumnReferences(&renamed.Tables[i], renamesByTable[i], renamesByTable, tableIndexes)
	}

	return renamed, nil
}

func splitColumnRenameEntry(entry string) (string, string, error) {
	entry = strings.TrimSpace(entry)
	if strings.Count(entry, ".") != 1 {
		return "", "", fmt.Errorf("invalid column rename entry")
	}
	tableName, columnName, _ := strings.Cut(entry, ".")
	tableName = strings.TrimSpace(tableName)
	columnName = strings.TrimSpace(columnName)
	if tableName == "" || columnName == "" {
		return "", "", fmt.Errorf("invalid column rename entry")
	}
	return tableName, columnName, nil
}

func findSourceColumnIndex(table Table, sourceName string) int {
	for i, col := range table.Columns {
		if strings.EqualFold(col.SourceName, sourceName) {
			return i
		}
	}
	return -1
}

func remapTableColumnReferences(table *Table, localRenames map[string]string, renamesByTable map[int]map[string]string, tableIndexes map[string]int) {
	if table.PrimaryKey != nil {
		remapColumnNames(table.PrimaryKey.Columns, localRenames)
	}
	for i := range table.Indexes {
		remapColumnNames(table.Indexes[i].Columns, localRenames)
	}
	for i := range table.ForeignKeys {
		remapColumnNames(table.ForeignKeys[i].Columns, localRenames)
		if refIdx, ok := tableIndexes[normalizeTableFilterKey(table.ForeignKeys[i].RefTable)]; ok {
			remapColumnNames(table.ForeignKeys[i].RefColumns, renamesByTable[refIdx])
		}
	}
}

func remapColumnNames(columns []string, renames map[string]string) {
	if len(renames) == 0 {
		return
	}
	for i, col := range columns {
		if target, ok := renames[normalizeTableFilterKey(col)]; ok {
			columns[i] = target
		}
	}
}
