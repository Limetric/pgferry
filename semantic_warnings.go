package main

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// SchemaSemanticWarning describes source schema semantics that are skipped or
// otherwise require manual follow-up even though the migration can proceed.
type SchemaSemanticWarning struct {
	Category            string `json:"category"`
	ObjectType          string `json:"object_type"`
	ObjectName          string `json:"object_name"`
	Disposition         string `json:"disposition"`
	Reason              string `json:"reason"`
	RecommendedFollowUp string `json:"recommended_follow_up"`
}

type sourceSchemaSemanticWarningIntrospector interface {
	IntrospectSchemaSemanticWarnings(db *sql.DB, dbName string) ([]SchemaSemanticWarning, error)
}

func introspectSourceSchemaSemanticWarnings(db *sql.DB, src SourceDB, dbName string) ([]SchemaSemanticWarning, error) {
	introspector, ok := src.(sourceSchemaSemanticWarningIntrospector)
	if !ok {
		return []SchemaSemanticWarning{}, nil
	}
	warnings, err := introspector.IntrospectSchemaSemanticWarnings(db, dbName)
	if err != nil {
		return nil, err
	}
	return ensureSchemaSemanticWarnings(warnings), nil
}

func collectSchemaSemanticWarnings(schema *Schema, src SourceDB, preserveDefaults bool, typeMap TypeMappingConfig, introspected []SchemaSemanticWarning) []SchemaSemanticWarning {
	warnings := append([]SchemaSemanticWarning{}, introspected...)
	if schema == nil || src == nil {
		sortSchemaSemanticWarnings(warnings)
		return warnings
	}

	if preserveDefaults {
		for _, table := range schema.Tables {
			for _, col := range table.Columns {
				warning, ok := collectDefaultSemanticWarning(table, col, src, typeMap)
				if ok {
					warnings = append(warnings, warning)
				}
			}
		}
	}

	sortSchemaSemanticWarnings(warnings)
	return warnings
}

func collectDefaultSemanticWarning(table Table, col Column, src SourceDB, typeMap TypeMappingConfig) (SchemaSemanticWarning, bool) {
	if col.Default == nil {
		return SchemaSemanticWarning{}, false
	}

	rawDefault := strings.TrimSpace(*col.Default)
	if rawDefault == "" || strings.EqualFold(rawDefault, "null") {
		return SchemaSemanticWarning{}, false
	}

	pgType, err := src.MapType(col, typeMap)
	if err != nil {
		return SchemaSemanticWarning{}, false
	}
	pgType = pgTypeForCollation(col, pgType, typeMap)

	mappedDefault, err := src.MapDefault(col, pgType, typeMap)
	if err != nil || mappedDefault != "" {
		return SchemaSemanticWarning{}, false
	}

	return SchemaSemanticWarning{
		Category:    "defaults",
		ObjectType:  "column",
		ObjectName:  table.PGName + "." + col.PGName,
		Disposition: "skipped",
		Reason: fmt.Sprintf(
			"%s default %q is not recreated automatically and will be omitted from the PostgreSQL column definition.",
			src.Name(),
			compactSemanticDetail(rawDefault),
		),
		RecommendedFollowUp: "Recreate the PostgreSQL DEFAULT manually if future inserts depend on it.",
	}, true
}

func ensureSchemaSemanticWarnings(warnings []SchemaSemanticWarning) []SchemaSemanticWarning {
	if warnings == nil {
		return []SchemaSemanticWarning{}
	}
	return warnings
}

func compactSemanticDetail(detail string) string {
	const maxSemanticDetailLen = 120

	compacted := strings.Join(strings.Fields(strings.TrimSpace(detail)), " ")
	if len(compacted) <= maxSemanticDetailLen {
		return compacted
	}
	if maxSemanticDetailLen <= 3 {
		return compacted[:maxSemanticDetailLen]
	}
	return compacted[:maxSemanticDetailLen-3] + "..."
}

func sortSchemaSemanticWarnings(warnings []SchemaSemanticWarning) {
	sort.Slice(warnings, func(i, j int) bool {
		a := warnings[i]
		b := warnings[j]
		if schemaSemanticWarningCategoryRank(a.Category) != schemaSemanticWarningCategoryRank(b.Category) {
			return schemaSemanticWarningCategoryRank(a.Category) < schemaSemanticWarningCategoryRank(b.Category)
		}
		if a.ObjectType != b.ObjectType {
			return a.ObjectType < b.ObjectType
		}
		if a.ObjectName != b.ObjectName {
			return a.ObjectName < b.ObjectName
		}
		if a.Disposition != b.Disposition {
			return a.Disposition < b.Disposition
		}
		if a.Reason != b.Reason {
			return a.Reason < b.Reason
		}
		return a.RecommendedFollowUp < b.RecommendedFollowUp
	})
}

func schemaSemanticWarningCategoryRank(category string) int {
	switch category {
	case "defaults":
		return 0
	case "constraints":
		return 1
	case "comments":
		return 2
	case "partitioning":
		return 3
	default:
		return 4
	}
}

func schemaSemanticWarningCategoryTitle(category string) string {
	switch category {
	case "defaults":
		return "Defaults"
	case "constraints":
		return "Constraints"
	case "comments":
		return "Comments and Metadata"
	case "partitioning":
		return "Partitioning"
	default:
		if category == "" {
			return "Other"
		}
		return strings.ToUpper(category[:1]) + category[1:]
	}
}
