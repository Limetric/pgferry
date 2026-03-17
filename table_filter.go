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
	SkippedForeignKeys []skippedForeignKey
}

type skippedForeignKey struct {
	Table    string
	Name     string
	RefTable string
}

func hasTableFilters(cfg *MigrationConfig) bool {
	return cfg != nil && (len(cfg.IncludeTables) > 0 || len(cfg.ExcludeTables) > 0)
}

func filterSchemaTables(schema *Schema, cfg *MigrationConfig) (*Schema, schemaFilterReport, error) {
	report := schemaFilterReport{}
	if schema == nil {
		return nil, report, nil
	}

	report.TotalTables = len(schema.Tables)
	if !hasTableFilters(cfg) {
		cloned := cloneSchema(schema)
		for _, table := range cloned.Tables {
			report.SelectedTables = append(report.SelectedTables, table.SourceName)
		}
		return cloned, report, nil
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
	if len(cfg.IncludeTables) == 0 {
		for key := range tableKeys {
			selected[key] = true
		}
	} else {
		for _, name := range cfg.IncludeTables {
			selected[normalizeTableFilterKey(name)] = true
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
		cloned.ForeignKeys = cloned.ForeignKeys[:0]
		for _, fk := range table.ForeignKeys {
			if selected[normalizeTableFilterKey(fk.RefTable)] {
				cloned.ForeignKeys = append(cloned.ForeignKeys, cloneForeignKey(fk))
				continue
			}
			report.SkippedForeignKeys = append(report.SkippedForeignKeys, skippedForeignKey{
				Table:    table.SourceName,
				Name:     fk.Name,
				RefTable: fk.RefTable,
			})
		}

		filtered.Tables = append(filtered.Tables, cloned)
		report.SelectedTables = append(report.SelectedTables, table.SourceName)
	}

	return filtered, report, nil
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

func cloneSchema(schema *Schema) *Schema {
	if schema == nil {
		return nil
	}

	cloned := &Schema{Tables: make([]Table, 0, len(schema.Tables))}
	for _, table := range schema.Tables {
		cloned.Tables = append(cloned.Tables, cloneTable(table))
	}
	return cloned
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
	if len(report.SkippedTables) > 0 {
		log.Printf("table filter: skipped %d table(s): %s", len(report.SkippedTables), strings.Join(report.SkippedTables, ", "))
	}
	if len(report.SkippedForeignKeys) == 0 {
		return
	}

	log.Printf("table filter: skipped %d foreign key(s) that reference excluded tables", len(report.SkippedForeignKeys))
	for _, fk := range report.SkippedForeignKeys {
		log.Printf("  WARN: skipping foreign key %s on %s because referenced table %s is excluded", fk.Name, fk.Table, fk.RefTable)
	}
}
