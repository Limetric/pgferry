package main

import (
	"fmt"
	"log"
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

func hasTableFilters(cfg *MigrationConfig) bool {
	return cfg != nil && (len(cfg.IncludeTables) > 0 || len(cfg.ExcludeTables) > 0)
}

// filterTriggersBySelectedTables keeps triggers whose Table is in the filtered schema's
// selected source tables (case-insensitive key, same as include/exclude matching).
func filterTriggersBySelectedTables(triggers []SourceTrigger, schema *Schema) []SourceTrigger {
	if len(triggers) == 0 || schema == nil || len(schema.Tables) == 0 {
		return nil
	}
	selected := make(map[string]struct{}, len(schema.Tables))
	for _, t := range schema.Tables {
		selected[normalizeTableFilterKey(t.SourceName)] = struct{}{}
	}
	out := make([]SourceTrigger, 0, len(triggers))
	for _, tr := range triggers {
		if tr.Table == "" {
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

	tableKeys := make(map[string]string, len(schema.Tables))
	for _, table := range schema.Tables {
		key := normalizeTableFilterKey(table.SourceName)
		if prev, ok := tableKeys[key]; ok {
			return nil, report, fmt.Errorf("source schema contains ambiguous table names %q and %q; include/exclude filters match source tables case-insensitively", prev, table.SourceName)
		}
		tableKeys[key] = table.SourceName
	}

	if missing := missingTableFilterEntries(cfg.IncludeTables, tableKeys); len(missing) > 0 {
		return nil, report, fmt.Errorf("include_tables entries did not match any source table: %s", strings.Join(missing, ", "))
	}
	if missing := missingTableFilterEntries(cfg.ExcludeTables, tableKeys); len(missing) > 0 {
		return nil, report, fmt.Errorf("exclude_tables entries did not match any source table: %s", strings.Join(missing, ", "))
	}

	selected := make(map[string]bool, len(schema.Tables))
	excluded := make(map[string]string, len(cfg.ExcludeTables))
	for _, name := range cfg.ExcludeTables {
		excluded[normalizeTableFilterKey(name)] = name
	}
	if len(cfg.IncludeTables) == 0 {
		for key := range tableKeys {
			selected[key] = true
		}
	} else {
		for _, name := range cfg.IncludeTables {
			key := normalizeTableFilterKey(name)
			selected[key] = true
			if excludeName, ok := excluded[key]; ok {
				report.OverlappingTables = append(report.OverlappingTables, fmt.Sprintf("%s (excluded by %q)", name, excludeName))
			}
		}
	}
	for _, name := range cfg.ExcludeTables {
		delete(selected, normalizeTableFilterKey(name))
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

func shouldKeepFilteredForeignKey(fk ForeignKey, cfg *MigrationConfig, selected map[string]bool) (bool, string) {
	if fk.RefSchema != "" && !strings.EqualFold(strings.TrimSpace(fk.RefSchema), strings.TrimSpace(cfg.Source.SourceSchema)) {
		return false, fmt.Sprintf("referenced table %s is in schema %q, outside the migrated schema %q", fk.RefTable, fk.RefSchema, cfg.Source.SourceSchema)
	}
	if !selected[normalizeTableFilterKey(fk.RefTable)] {
		return false, fmt.Sprintf("referenced table %s is not in the selected table set", fk.RefTable)
	}
	return true, ""
}

func missingTableFilterEntries(entries []string, tableKeys map[string]string) []string {
	if len(entries) == 0 {
		return nil
	}

	var missing []string
	for _, name := range entries {
		if _, ok := tableKeys[normalizeTableFilterKey(name)]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
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
