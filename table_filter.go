package main

import (
	"fmt"
	"log"
	"path"
	"strings"
)

type schemaFilterReport struct {
	TotalTables        int
	SelectedTables     []string
	SkippedTables      []string
	OverlappingTables  []string
	SkippedForeignKeys []skippedForeignKey
}

type skippedForeignKey struct {
	Table    string
	Name     string
	RefTable string
	Reason   string
}

type columnFilterReport struct {
	TotalColumns       int
	ExcludedColumns    []string
	SkippedPrimaryKeys []skippedSchemaIndex
	SkippedIndexes     []skippedSchemaIndex
	SkippedForeignKeys []skippedForeignKey
}

type skippedSchemaIndex struct {
	Table  string
	Name   string
	Reason string
}

func hasTableFilters(cfg *MigrationConfig) bool {
	return cfg != nil && (len(cfg.IncludeTables) > 0 || len(cfg.ExcludeTables) > 0)
}

func hasColumnFilters(cfg *MigrationConfig) bool {
	return cfg != nil && len(cfg.ExcludeColumns) > 0
}

// Some unit tests and internal callers build MigrationConfig values directly
// without running finalizeConfig first, so keep an exact-mode fallback here.
func effectiveTableFilterMode(cfg *MigrationConfig) string {
	if cfg == nil {
		return "exact"
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.TableFilterMode))
	if mode == "" {
		return "exact"
	}
	return mode
}

func effectiveColumnFilterMode(cfg *MigrationConfig) string {
	if cfg == nil {
		return "exact"
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.ColumnFilterMode))
	if mode == "" {
		return "exact"
	}
	return mode
}

// filterTriggersBySelectedTables keeps triggers whose Table is in the filtered schema's
// selected source tables (case-insensitive key, same as include/exclude matching).
// Triggers with an empty Table are kept (source could not resolve the table); they are not
// silently dropped when filters are active.
func filterTriggersBySelectedTables(triggers []SourceTrigger, schema *Schema) []SourceTrigger {
	if len(triggers) == 0 {
		return []SourceTrigger{}
	}
	if schema == nil || len(schema.Tables) == 0 {
		return append([]SourceTrigger(nil), triggers...)
	}
	selected := make(map[string]struct{}, len(schema.Tables))
	for _, t := range schema.Tables {
		selected[normalizeTableFilterKey(t.SourceName)] = struct{}{}
	}
	out := make([]SourceTrigger, 0, len(triggers))
	for _, tr := range triggers {
		if tr.Table == "" {
			out = append(out, tr)
			continue
		}
		if _, ok := selected[normalizeTableFilterKey(tr.Table)]; ok {
			out = append(out, tr)
		}
	}
	return out
}

func filterSchemaTables(schema *Schema, cfg *MigrationConfig) (*Schema, schemaFilterReport, error) {
	report := schemaFilterReport{}
	if schema == nil {
		return &Schema{}, report, nil
	}

	report.TotalTables = len(schema.Tables)
	if !hasTableFilters(cfg) {
		return schema, report, nil
	}
	mode := effectiveTableFilterMode(cfg)

	tableKeys := make(map[string]string, len(schema.Tables))
	for _, table := range schema.Tables {
		key := normalizeTableFilterKey(table.SourceName)
		if prev, ok := tableKeys[key]; ok {
			return nil, report, fmt.Errorf("source schema contains ambiguous table names %q and %q; include/exclude filters match source tables case-insensitively", prev, table.SourceName)
		}
		tableKeys[key] = table.SourceName
	}

	if missing, err := missingTableFilterEntries(mode, cfg.IncludeTables, schema.Tables); err != nil {
		return nil, report, err
	} else if len(missing) > 0 {
		return nil, report, fmt.Errorf("include_tables entries did not match any source table: %s", strings.Join(missing, ", "))
	}
	if missing, err := missingTableFilterEntries(mode, cfg.ExcludeTables, schema.Tables); err != nil {
		return nil, report, err
	} else if len(missing) > 0 {
		return nil, report, fmt.Errorf("exclude_tables entries did not match any source table: %s", strings.Join(missing, ", "))
	}

	selected := make(map[string]bool, len(schema.Tables))
	if len(cfg.IncludeTables) == 0 {
		for key := range tableKeys {
			selected[key] = true
		}
	} else if mode == "exact" {
		excluded := make(map[string]string, len(cfg.ExcludeTables))
		for _, name := range cfg.ExcludeTables {
			excluded[normalizeTableFilterKey(name)] = name
		}
		for _, name := range cfg.IncludeTables {
			key := normalizeTableFilterKey(name)
			selected[key] = true
			if excludeName, ok := excluded[key]; ok {
				report.OverlappingTables = append(report.OverlappingTables, fmt.Sprintf("%s (excluded by %q)", name, excludeName))
			}
		}
	} else {
		for _, table := range schema.Tables {
			matched, err := tableMatchesAnyFilterEntry(mode, table.SourceName, cfg.IncludeTables)
			if err != nil {
				return nil, report, err
			}
			if matched {
				selected[normalizeTableFilterKey(table.SourceName)] = true
			}
		}
	}

	if mode == "exact" {
		for _, name := range cfg.ExcludeTables {
			delete(selected, normalizeTableFilterKey(name))
		}
	} else {
		for _, table := range schema.Tables {
			excludeName, err := firstMatchingTableFilterEntry(mode, table.SourceName, cfg.ExcludeTables)
			if err != nil {
				return nil, report, err
			}
			if excludeName == "" {
				continue
			}
			key := normalizeTableFilterKey(table.SourceName)
			if selected[key] && len(cfg.IncludeTables) > 0 {
				report.OverlappingTables = append(report.OverlappingTables, fmt.Sprintf("%s (excluded by %q)", table.SourceName, excludeName))
			}
			delete(selected, key)
		}
	}

	if len(selected) == 0 {
		return nil, report, fmt.Errorf("table filters excluded every source table; adjust include_tables/exclude_tables")
	}

	filtered := &Schema{Tables: make([]Table, 0, len(selected))}
	for _, table := range schema.Tables {
		key := normalizeTableFilterKey(table.SourceName)
		if !selected[key] {
			report.SkippedTables = append(report.SkippedTables, table.SourceName)
			continue
		}

		cloned := cloneTable(table)
		cloned.ForeignKeys = nil
		for _, fk := range table.ForeignKeys {
			keep, reason := shouldKeepFilteredForeignKey(fk, cfg, selected)
			if keep {
				cloned.ForeignKeys = append(cloned.ForeignKeys, cloneForeignKey(fk))
				continue
			}
			report.SkippedForeignKeys = append(report.SkippedForeignKeys, skippedForeignKey{
				Table:    table.SourceName,
				Name:     fk.Name,
				RefTable: fk.RefTable,
				Reason:   reason,
			})
		}

		filtered.Tables = append(filtered.Tables, cloned)
		report.SelectedTables = append(report.SelectedTables, table.SourceName)
	}

	return filtered, report, nil
}

func filterSchemaColumns(schema *Schema, cfg *MigrationConfig) (*Schema, columnFilterReport, error) {
	report := columnFilterReport{}
	if schema == nil {
		return &Schema{}, report, nil
	}

	for _, table := range schema.Tables {
		report.TotalColumns += len(table.Columns)
	}
	if !hasColumnFilters(cfg) {
		return schema, report, nil
	}
	mode := effectiveColumnFilterMode(cfg)

	if missing, err := missingColumnFilterEntries(mode, cfg.ExcludeColumns, schema.Tables); err != nil {
		return nil, report, err
	} else if len(missing) > 0 {
		return nil, report, fmt.Errorf("exclude_columns entries did not match any source column in the migrated schema (table may have been excluded by table filters): %s", strings.Join(missing, ", "))
	}

	filtered := &Schema{Tables: make([]Table, 0, len(schema.Tables))}
	keptPGColumnsByTable := make([]map[string]bool, 0, len(schema.Tables))
	for _, table := range schema.Tables {
		cloned := Table{
			SourceName: table.SourceName,
			PGName:     table.PGName,
		}
		keptColumns := make([]Column, 0, len(table.Columns))
		keptPGColumns := make(map[string]bool, len(table.Columns))

		for _, col := range table.Columns {
			exclude, err := columnMatchesAnyFilterEntry(mode, table.SourceName, col.SourceName, cfg.ExcludeColumns)
			if err != nil {
				return nil, report, err
			}
			if exclude {
				report.ExcludedColumns = append(report.ExcludedColumns, fmt.Sprintf("%s.%s", table.SourceName, col.SourceName))
				continue
			}
			keptColumns = append(keptColumns, col)
			keptPGColumns[normalizeTableFilterKey(col.PGName)] = true
		}
		if len(keptColumns) == 0 {
			return nil, report, fmt.Errorf("exclude_columns removed every column from table %s; adjust exclude_columns", table.SourceName)
		}
		cloned.Columns = keptColumns

		if table.PrimaryKey != nil {
			pk := cloneIndex(*table.PrimaryKey)
			cloned.PrimaryKey = &pk
		}
		if cloned.PrimaryKey != nil && !indexColumnsExist(*cloned.PrimaryKey, keptPGColumns) {
			report.SkippedPrimaryKeys = append(report.SkippedPrimaryKeys, skippedSchemaIndex{
				Table:  table.SourceName,
				Name:   cloned.PrimaryKey.Name,
				Reason: "references an excluded column",
			})
			cloned.PrimaryKey = nil
		}

		cloned.Indexes = nil
		for _, idx := range table.Indexes {
			if !indexColumnsExist(idx, keptPGColumns) {
				report.SkippedIndexes = append(report.SkippedIndexes, skippedSchemaIndex{
					Table:  table.SourceName,
					Name:   idx.Name,
					Reason: "references an excluded column",
				})
				continue
			}
			cloned.Indexes = append(cloned.Indexes, cloneIndex(idx))
		}

		cloned.ForeignKeys = nil

		filtered.Tables = append(filtered.Tables, cloned)
		keptPGColumnsByTable = append(keptPGColumnsByTable, keptPGColumns)
	}

	// filtered.Tables remains parallel to schema.Tables because the first pass
	// errors instead of omitting a table when all of its columns are excluded.
	for i, table := range schema.Tables {
		keptPGColumns := keptPGColumnsByTable[i]
		for _, fk := range table.ForeignKeys {
			keep, reason := shouldKeepColumnFilteredForeignKey(fk, filtered, keptPGColumns)
			if keep {
				filtered.Tables[i].ForeignKeys = append(filtered.Tables[i].ForeignKeys, cloneForeignKey(fk))
				continue
			}
			report.SkippedForeignKeys = append(report.SkippedForeignKeys, skippedForeignKey{
				Table:    table.SourceName,
				Name:     fk.Name,
				RefTable: fk.RefTable,
				Reason:   reason,
			})
		}
	}

	return filtered, report, nil
}

func missingColumnFilterEntries(mode string, entries []string, tables []Table) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	var missing []string
	for _, entry := range entries {
		matched, err := columnFilterEntryMatchesAnyColumn(mode, entry, tables)
		if err != nil {
			return nil, err
		}
		if !matched {
			missing = append(missing, entry)
		}
	}
	return missing, nil
}

func columnFilterEntryMatchesAnyColumn(mode, entry string, tables []Table) (bool, error) {
	for _, table := range tables {
		for _, col := range table.Columns {
			matched, err := columnFilterEntryMatches(mode, entry, table.SourceName, col.SourceName)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

func columnMatchesAnyFilterEntry(mode, tableName, columnName string, entries []string) (bool, error) {
	for _, entry := range entries {
		matched, err := columnFilterEntryMatches(mode, entry, tableName, columnName)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func columnFilterEntryMatches(mode, entry, tableName, columnName string) (bool, error) {
	tablePattern, columnPattern, qualified := splitColumnFilterEntry(entry)
	if mode == "glob" {
		if qualified {
			tableMatched, err := path.Match(normalizeTableFilterKey(tablePattern), normalizeTableFilterKey(tableName))
			if err != nil {
				return false, fmt.Errorf("match column filter %q against %s.%s: %w", entry, tableName, columnName, err)
			}
			if !tableMatched {
				return false, nil
			}
		}
		columnMatched, err := path.Match(normalizeTableFilterKey(columnPattern), normalizeTableFilterKey(columnName))
		if err != nil {
			return false, fmt.Errorf("match column filter %q against %s.%s: %w", entry, tableName, columnName, err)
		}
		return columnMatched, nil
	}

	if qualified && normalizeTableFilterKey(tablePattern) != normalizeTableFilterKey(tableName) {
		return false, nil
	}
	return normalizeTableFilterKey(columnPattern) == normalizeTableFilterKey(columnName), nil
}

func splitColumnFilterEntry(entry string) (string, string, bool) {
	tableName, columnName, qualified := strings.Cut(strings.TrimSpace(entry), ".")
	if !qualified {
		return "", tableName, false
	}
	return tableName, columnName, true
}

func indexColumnsExist(idx Index, keptPGColumns map[string]bool) bool {
	if idx.HasExpression {
		return true
	}
	for _, col := range idx.Columns {
		if !keptPGColumns[normalizeTableFilterKey(col)] {
			return false
		}
	}
	return true
}

func shouldKeepColumnFilteredForeignKey(fk ForeignKey, schema *Schema, keptLocalPGColumns map[string]bool) (bool, string) {
	for _, col := range fk.Columns {
		if !keptLocalPGColumns[normalizeTableFilterKey(col)] {
			return false, fmt.Sprintf("local column %s is excluded", col)
		}
	}
	refTable := findSchemaTableBySourceName(schema, fk.RefTable)
	if refTable == nil {
		return true, ""
	}
	for _, col := range fk.RefColumns {
		if !tableHasPGColumn(*refTable, col) {
			return false, fmt.Sprintf("referenced column %s is excluded", col)
		}
	}
	return true, ""
}

func findSchemaTableBySourceName(schema *Schema, sourceName string) *Table {
	if schema == nil {
		return nil
	}
	for i := range schema.Tables {
		if strings.EqualFold(schema.Tables[i].SourceName, sourceName) {
			return &schema.Tables[i]
		}
	}
	return nil
}

func tableHasPGColumn(table Table, pgName string) bool {
	for _, col := range table.Columns {
		if strings.EqualFold(col.PGName, pgName) {
			return true
		}
	}
	return false
}

func shouldKeepFilteredForeignKey(fk ForeignKey, cfg *MigrationConfig, selected map[string]bool) (bool, string) {
	if fk.RefSchema != "" && !strings.EqualFold(strings.TrimSpace(fk.RefSchema), strings.TrimSpace(cfg.Source.SourceSchema)) {
		return false, fmt.Sprintf("referenced table %s is in schema %q, outside the migrated schema %q", fk.RefTable, fk.RefSchema, cfg.Source.SourceSchema)
	}
	if !selected[normalizeTableFilterKey(fk.RefTable)] {
		return false, fmt.Sprintf("referenced table %s is not in the selected table set", fk.RefTable)
	}
	return true, ""
}

func missingTableFilterEntries(mode string, entries []string, tables []Table) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	tableNames := sourceTableNames(tables)
	var missing []string
	for _, name := range entries {
		matched, err := filterEntryMatchesAnyTable(mode, name, tableNames)
		if err != nil {
			return nil, err
		}
		if !matched {
			missing = append(missing, name)
		}
	}
	return missing, nil
}

func sourceTableNames(tables []Table) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.SourceName)
	}
	return names
}

func filterEntryMatchesAnyTable(mode, entry string, tableNames []string) (bool, error) {
	for _, tableName := range tableNames {
		matched, err := tableFilterEntryMatches(mode, entry, tableName)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func tableMatchesAnyFilterEntry(mode, tableName string, entries []string) (bool, error) {
	entry, err := firstMatchingTableFilterEntry(mode, tableName, entries)
	if err != nil {
		return false, err
	}
	return entry != "", nil
}

func firstMatchingTableFilterEntry(mode, tableName string, entries []string) (string, error) {
	for _, entry := range entries {
		matched, err := tableFilterEntryMatches(mode, entry, tableName)
		if err != nil {
			return "", err
		}
		if matched {
			return entry, nil
		}
	}
	return "", nil
}

func tableFilterEntryMatches(mode, entry, tableName string) (bool, error) {
	switch mode {
	case "glob":
		matched, err := path.Match(normalizeTableFilterKey(entry), normalizeTableFilterKey(tableName))
		if err != nil {
			return false, fmt.Errorf("match table filter %q against %q: %w", entry, tableName, err)
		}
		return matched, nil
	default:
		return normalizeTableFilterKey(entry) == normalizeTableFilterKey(tableName), nil
	}
}

func cloneTable(table Table) Table {
	cloned := Table{
		SourceName: table.SourceName,
		PGName:     table.PGName,
		Columns:    append([]Column(nil), table.Columns...),
		Indexes:    make([]Index, 0, len(table.Indexes)),
	}

	if table.PrimaryKey != nil {
		pk := cloneIndex(*table.PrimaryKey)
		cloned.PrimaryKey = &pk
	}
	for _, idx := range table.Indexes {
		cloned.Indexes = append(cloned.Indexes, cloneIndex(idx))
	}
	for _, fk := range table.ForeignKeys {
		cloned.ForeignKeys = append(cloned.ForeignKeys, cloneForeignKey(fk))
	}

	return cloned
}

func cloneIndex(idx Index) Index {
	cloned := idx
	cloned.Columns = append([]string(nil), idx.Columns...)
	cloned.ColumnOrders = append([]string(nil), idx.ColumnOrders...)
	return cloned
}

func cloneForeignKey(fk ForeignKey) ForeignKey {
	cloned := fk
	cloned.Columns = append([]string(nil), fk.Columns...)
	cloned.RefColumns = append([]string(nil), fk.RefColumns...)
	return cloned
}

func logTableFilterReport(report schemaFilterReport) {
	log.Printf("table filter: selected %d of %d table(s)", len(report.SelectedTables), report.TotalTables)
	for _, overlap := range report.OverlappingTables {
		log.Printf("  WARN: table filter overlap: %s appears in both include_tables and exclude_tables; exclude_tables wins", overlap)
	}
	if len(report.SkippedTables) > 0 {
		log.Printf("table filter: skipped %d table(s): %s", len(report.SkippedTables), strings.Join(report.SkippedTables, ", "))
	}
	if len(report.SkippedForeignKeys) == 0 {
		return
	}

	log.Printf("table filter: skipped %d foreign key(s) during table filtering", len(report.SkippedForeignKeys))
	for _, fk := range report.SkippedForeignKeys {
		log.Printf("  WARN: skipping foreign key %s on %s because %s", fk.Name, fk.Table, fk.Reason)
	}
}

func logColumnFilterReport(report columnFilterReport) {
	log.Printf("column filter: excluded %d of %d column(s)", len(report.ExcludedColumns), report.TotalColumns)
	for _, col := range report.ExcludedColumns {
		log.Printf("  excluded column %s", col)
	}
	for _, pk := range report.SkippedPrimaryKeys {
		log.Printf("  WARN: skipping primary key %s on %s because %s", pk.Name, pk.Table, pk.Reason)
	}
	for _, idx := range report.SkippedIndexes {
		log.Printf("  WARN: skipping index %s on %s because %s", idx.Name, idx.Table, idx.Reason)
	}
	for _, fk := range report.SkippedForeignKeys {
		log.Printf("  WARN: skipping foreign key %s on %s because %s", fk.Name, fk.Table, fk.Reason)
	}
}
