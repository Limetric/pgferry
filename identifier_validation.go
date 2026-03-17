package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type identifierNamespaceOptions struct {
	allowEquivalentReuse bool
	skipIfAllClass       string
}

type identifierEntry struct {
	origin string
	key    string
	class  string
}

type identifierNamespace struct {
	label   string
	options identifierNamespaceOptions
	entries map[string][]identifierEntry
}

type identifierCollision struct {
	label   string
	name    string
	origins []string
}

type identifierCollector struct {
	namespaces map[string]*identifierNamespace
}

func newIdentifierCollector() *identifierCollector {
	return &identifierCollector{namespaces: make(map[string]*identifierNamespace)}
}

func (c *identifierCollector) add(scopeKey, label, name, origin, key, class string, opts identifierNamespaceOptions) {
	ns, ok := c.namespaces[scopeKey]
	if !ok {
		ns = &identifierNamespace{
			label:   label,
			options: opts,
			entries: make(map[string][]identifierEntry),
		}
		c.namespaces[scopeKey] = ns
	} else if ns.label != label || ns.options != opts {
		panic(fmt.Sprintf("identifier namespace %q registered with inconsistent metadata", scopeKey))
	}
	ns.entries[name] = append(ns.entries[name], identifierEntry{
		origin: origin,
		key:    key,
		class:  class,
	})
}

func (c *identifierCollector) collisions() []identifierCollision {
	var out []identifierCollision

	scopeKeys := make([]string, 0, len(c.namespaces))
	for scopeKey := range c.namespaces {
		scopeKeys = append(scopeKeys, scopeKey)
	}
	sort.Strings(scopeKeys)

	for _, scopeKey := range scopeKeys {
		ns := c.namespaces[scopeKey]
		names := make([]string, 0, len(ns.entries))
		for name := range ns.entries {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			entries := ns.entries[name]
			if len(entries) < 2 {
				continue
			}

			if ns.options.skipIfAllClass != "" {
				allSameClass := true
				for _, entry := range entries {
					if entry.class != ns.options.skipIfAllClass {
						allSameClass = false
						break
					}
				}
				if allSameClass {
					continue
				}
			}

			if ns.options.allowEquivalentReuse {
				keys := make(map[string]struct{})
				for _, entry := range entries {
					keys[entry.key] = struct{}{}
				}
				if len(keys) <= 1 {
					continue
				}
			}

			origins := make([]string, 0, len(entries))
			seenOrigins := make(map[string]struct{}, len(entries))
			for _, entry := range entries {
				if _, seen := seenOrigins[entry.origin]; seen {
					continue
				}
				seenOrigins[entry.origin] = struct{}{}
				origins = append(origins, entry.origin)
			}
			sort.Strings(origins)

			out = append(out, identifierCollision{
				label:   ns.label,
				name:    name,
				origins: origins,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].label != out[j].label {
			return out[i].label < out[j].label
		}
		if out[i].name != out[j].name {
			return out[i].name < out[j].name
		}
		return strings.Join(out[i].origins, "\x00") < strings.Join(out[j].origins, "\x00")
	})

	return out
}

func validateGeneratedIdentifiers(schema *Schema, cfg *MigrationConfig, typeMap TypeMappingConfig) error {
	if schema == nil {
		return nil
	}
	if cfg == nil {
		cfg = &MigrationConfig{}
	}

	collector := newIdentifierCollector()
	trackSchemaTypes := !cfg.DataOnly && typeMap.EnumMode == "native"
	tableTypeOrigins := make(map[string][]identifierEntry)

	for _, table := range schema.Tables {
		tableOrigin := describeTableOrigin(table)
		collector.add(
			"schema-relations",
			"schema relation names",
			table.PGName,
			tableOrigin,
			tableOrigin,
			"table",
			identifierNamespaceOptions{},
		)
		if trackSchemaTypes {
			// PostgreSQL table row types live in the same schema-level type namespace
			// as native ENUM types, so enum/type validation must consider both.
			tableTypeOrigins[table.PGName] = append(tableTypeOrigins[table.PGName], identifierEntry{
				origin: describeTableRowTypeOrigin(table),
				key:    table.PGName,
				class:  "table-row-type",
			})
		}

		columnScopeKey := "table-columns:" + table.PGName
		columnScopeLabel := fmt.Sprintf("column names on table %q", table.PGName)
		constraintScopeKey := "table-constraints:" + table.PGName
		constraintScopeLabel := fmt.Sprintf("constraint names on table %q", table.PGName)
		triggerScopeKey := "table-triggers:" + table.PGName
		triggerScopeLabel := fmt.Sprintf("trigger names on table %q", table.PGName)

		for _, col := range table.Columns {
			columnOrigin := describeColumnOrigin(table, col)
			collector.add(
				columnScopeKey,
				columnScopeLabel,
				col.PGName,
				columnOrigin,
				columnOrigin,
				"column",
				identifierNamespaceOptions{},
			)

			if strings.Contains(strings.ToLower(col.Extra), "auto_increment") {
				seqName := generatedSequenceName(table, col)
				collector.add(
					"schema-relations",
					"schema relation names",
					seqName,
					describeSequenceOrigin(table, col),
					describeSequenceOrigin(table, col),
					"sequence",
					identifierNamespaceOptions{},
				)
			}

			if !cfg.DataOnly && cfg.AddUnsignedChecks {
				if _, ok := unsignedCheckExpr(col, typeMap); ok {
					constraintName := unsignedConstraintName(table.PGName, col.PGName)
					collector.add(
						constraintScopeKey,
						constraintScopeLabel,
						constraintName,
						describeUnsignedConstraintOrigin(table, col),
						describeUnsignedConstraintOrigin(table, col),
						"constraint",
						identifierNamespaceOptions{},
					)
				}
			}

			if !cfg.DataOnly && cfg.ReplicateOnUpdateCurrentTimestamp &&
				strings.Contains(strings.ToLower(col.Extra), "on update current_timestamp") {
				funcName := generatedTriggerFunctionName(col)
				collector.add(
					"schema-trigger-functions",
					"schema trigger function names",
					funcName,
					describeTriggerFunctionOrigin(table, col),
					col.PGName,
					"trigger-function",
					identifierNamespaceOptions{allowEquivalentReuse: true},
				)

				triggerName := generatedTriggerName(table, col)
				collector.add(
					triggerScopeKey,
					triggerScopeLabel,
					triggerName,
					describeTriggerOrigin(table, col),
					describeTriggerOrigin(table, col),
					"trigger",
					identifierNamespaceOptions{},
				)
			}

			if !cfg.DataOnly && typeMap.EnumMode == "native" && col.DataType == "enum" {
				values, signature, err := enumTypeIdentity(col)
				if err != nil {
					return fmt.Errorf("enum values for %s: %w", describeColumnOrigin(table, col), err)
				}
				typeName := pgEnumTypeName(values)
				collector.add(
					"schema-types",
					"schema type names",
					typeName,
					describeEnumTypeOrigin(table, col, values),
					signature,
					"enum-type",
					identifierNamespaceOptions{allowEquivalentReuse: true, skipIfAllClass: "table-row-type"},
				)
			}
		}

		if cfg.DataOnly {
			continue
		}

		if table.PrimaryKey != nil {
			pkName := generatedPrimaryKeyName(table)
			collector.add(
				constraintScopeKey,
				constraintScopeLabel,
				pkName,
				describePrimaryKeyOrigin(table),
				describePrimaryKeyOrigin(table),
				"constraint",
				identifierNamespaceOptions{},
			)
			// PostgreSQL also creates a backing index for the PRIMARY KEY using
			// the same identifier, so validate the relation namespace too.
			collector.add(
				"schema-relations",
				"schema relation names",
				pkName,
				describePrimaryKeyIndexOrigin(table),
				describePrimaryKeyIndexOrigin(table),
				"primary-key-index",
				identifierNamespaceOptions{},
			)
		}

		for _, idx := range table.Indexes {
			if _, unsupported := indexUnsupportedReason(table, idx, typeMap); unsupported {
				continue
			}
			idxName := generatedIndexName(table, idx)
			collector.add(
				"schema-relations",
				"schema relation names",
				idxName,
				describeIndexOrigin(table, idx),
				describeIndexOrigin(table, idx),
				"index",
				identifierNamespaceOptions{},
			)
		}

		for _, fk := range table.ForeignKeys {
			fkName := generatedForeignKeyName(fk)
			collector.add(
				constraintScopeKey,
				constraintScopeLabel,
				fkName,
				describeForeignKeyOrigin(table, fk),
				describeForeignKeyOrigin(table, fk),
				"constraint",
				identifierNamespaceOptions{},
			)
		}
	}

	if trackSchemaTypes {
		for typeName, entries := range tableTypeOrigins {
			for _, entry := range entries {
				collector.add(
					"schema-types",
					"schema type names",
					typeName,
					entry.origin,
					entry.key,
					entry.class,
					identifierNamespaceOptions{allowEquivalentReuse: true, skipIfAllClass: "table-row-type"},
				)
			}
		}
	}

	collisions := collector.collisions()
	if len(collisions) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("generated PostgreSQL identifier collisions detected:\n")
	for _, collision := range collisions {
		b.WriteString("  - ")
		b.WriteString(collision.label)
		b.WriteString(": final name ")
		b.WriteString(fmt.Sprintf("%q", collision.name))
		b.WriteString(" is produced by ")
		b.WriteString(strings.Join(collision.origins, "; "))
		b.WriteByte('\n')
	}
	b.WriteString("Hint: rename the conflicting source objects, disable snake_case_identifiers if normalization caused the collision, or migrate the conflicting objects manually with hooks.")

	return errors.New(b.String())
}

func enumTypeIdentity(col Column) ([]string, string, error) {
	values, err := parseMySQLEnumSetValues(col.ColumnType)
	if err != nil {
		return nil, "", err
	}
	sorted := make([]string, len(values))
	copy(sorted, values)
	sort.Strings(sorted)
	return values, strings.Join(sorted, "\x00"), nil
}

func describeTableOrigin(table Table) string {
	return fmt.Sprintf("table %q", sourceLabel(table.SourceName, table.PGName))
}

func describeTableRowTypeOrigin(table Table) string {
	return fmt.Sprintf("table row type for %q", sourceLabel(table.SourceName, table.PGName))
}

func describeColumnOrigin(table Table, col Column) string {
	return fmt.Sprintf("column %q.%q", sourceLabel(table.SourceName, table.PGName), sourceLabel(col.SourceName, col.PGName))
}

func describePrimaryKeyOrigin(table Table) string {
	return fmt.Sprintf("primary key on table %q", sourceLabel(table.SourceName, table.PGName))
}

func describePrimaryKeyIndexOrigin(table Table) string {
	return fmt.Sprintf("primary key index on table %q", sourceLabel(table.SourceName, table.PGName))
}

func describeIndexOrigin(table Table, idx Index) string {
	return fmt.Sprintf("index %q on table %q", sourceLabel(idx.SourceName, idx.Name), sourceLabel(table.SourceName, table.PGName))
}

func describeForeignKeyOrigin(table Table, fk ForeignKey) string {
	return fmt.Sprintf("foreign key %q on table %q", fk.Name, sourceLabel(table.SourceName, table.PGName))
}

func describeSequenceOrigin(table Table, col Column) string {
	return fmt.Sprintf("sequence for column %q.%q", sourceLabel(table.SourceName, table.PGName), sourceLabel(col.SourceName, col.PGName))
}

func describeUnsignedConstraintOrigin(table Table, col Column) string {
	return fmt.Sprintf("unsigned check for column %q.%q", sourceLabel(table.SourceName, table.PGName), sourceLabel(col.SourceName, col.PGName))
}

func describeTriggerFunctionOrigin(table Table, col Column) string {
	return fmt.Sprintf(
		"trigger function for column %q.%q (target column %q)",
		sourceLabel(table.SourceName, table.PGName),
		sourceLabel(col.SourceName, col.PGName),
		col.PGName,
	)
}

func describeTriggerOrigin(table Table, col Column) string {
	return fmt.Sprintf("trigger for column %q.%q", sourceLabel(table.SourceName, table.PGName), sourceLabel(col.SourceName, col.PGName))
}

func describeEnumTypeOrigin(table Table, col Column, values []string) string {
	return fmt.Sprintf(
		"enum type for column %q.%q with values (%s)",
		sourceLabel(table.SourceName, table.PGName),
		sourceLabel(col.SourceName, col.PGName),
		strings.Join(values, ", "),
	)
}

func sourceLabel(sourceName, fallback string) string {
	if sourceName != "" {
		return sourceName
	}
	return fallback
}
