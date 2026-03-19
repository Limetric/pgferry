package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

var planOutputDir string
var planFormat string

var planCmd = &cobra.Command{
	Use:   "plan [migration.toml]",
	Short: "Analyze source schema and generate a migration plan report",
	Long: `Analyze the source database schema and produce a report of objects that
require manual follow-up: views, routines, triggers, generated columns,
and skipped indexes.

Optionally generates hook skeleton files in the specified output directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlan,
}

var planConfigPath string

func init() {
	planCmd.Flags().StringVar(&planConfigPath, "config", "", "path to migration TOML config file")
	planCmd.Flags().StringVar(&planOutputDir, "output-dir", "", "directory to write hook skeleton files")
	planCmd.Flags().StringVar(&planFormat, "format", "text", "output format: text or json")
}

// PlanReport holds all findings from the plan analysis.
type PlanReport struct {
	RequiredExtensions      []PlanRequiredExtension      `json:"required_extensions"`
	SourceObjects           PlanSourceObjects            `json:"source_objects"`
	UnsupportedColumns      []PlanUnsupportedColumn      `json:"unsupported_columns"`
	SchemaSemanticWarnings  []SchemaSemanticWarning      `json:"schema_semantic_warnings"`
	GeneratedColumns        []PlanGeneratedColumn        `json:"generated_columns"`
	SkippedIndexes          []PlanSkippedIndex           `json:"skipped_indexes"`
	OrphanCleanupCandidates []PlanOrphanCleanupCandidate `json:"orphan_cleanup_candidates"`
	TemporalWarnings        []PlanTemporalWarning        `json:"temporal_warnings"`
	CollationWarnings       []string                     `json:"collation_warnings"`
}

type PlanRequiredExtension struct {
	Name    string `json:"name"`
	Feature string `json:"feature"`
	Mode    string `json:"mode"`
}

// PlanSourceObjects holds non-table source objects.
type PlanSourceObjects struct {
	Views    []string `json:"views"`
	Routines []string `json:"routines"`
	Triggers []string `json:"triggers"`
}

type PlanUnsupportedColumn struct {
	Table      string `json:"table"`
	Column     string `json:"column"`
	SourceType string `json:"source_type"`
	Reason     string `json:"reason"`
}

// PlanGeneratedColumn describes a generated column that needs manual expression migration.
type PlanGeneratedColumn struct {
	Table      string `json:"table"`
	Column     string `json:"column"`
	Expression string `json:"expression"`
}

// PlanSkippedIndex describes an index that cannot be automatically migrated.
type PlanSkippedIndex struct {
	Table  string `json:"table"`
	Index  string `json:"index"`
	Reason string `json:"reason"`
}

type PlanOrphanCleanupCandidate struct {
	Table      string   `json:"table"`
	ForeignKey string   `json:"foreign_key"`
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	Action     string   `json:"action"`
}

type PlanTemporalWarning struct {
	Category    string   `json:"category"`
	Summary     string   `json:"summary"`
	Columns     int      `json:"columns"`
	Examples    []string `json:"examples"`
	Remediation string   `json:"remediation"`
}

func runPlan(cmd *cobra.Command, args []string) error {
	cfgPath := planConfigPath
	if len(args) > 0 {
		cfgPath = args[0]
	}
	if cfgPath == "" {
		return fmt.Errorf("config file required: pgferry plan <migration.toml> or pgferry plan --config <migration.toml>")
	}

	switch planFormat {
	case "text", "json":
	default:
		return fmt.Errorf("--format must be text or json")
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	return runPlanWithConfig(cfg, cmd.OutOrStdout())
}

func runPlanWithConfig(cfg *MigrationConfig, out io.Writer) error {
	format := planFormat
	if format == "" {
		format = "text"
	}

	ctx := context.Background()

	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}

	log.Printf("pgferry plan — %s source analysis", src.Name())

	sourceDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return err
	}
	defer sourceDB.Close()
	sourceDB.SetMaxOpenConns(1)

	if err := sourceDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping %s: %w", strings.ToLower(src.Name()), err)
	}

	dbName, err := src.ExtractDBName(cfg.Source.DSN)
	if err != nil {
		return err
	}

	log.Printf("introspecting %s schema '%s'...", src.Name(), dbName)
	schema, err := src.IntrospectSchema(sourceDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema: %w", err)
	}
	filteredSchema, filterReport, err := filterSchemaTables(schema, cfg)
	if err != nil {
		return fmt.Errorf("filter schema tables: %w", err)
	}
	schema = filteredSchema
	if hasTableFilters(cfg) {
		logTableFilterReport(filterReport)
	}

	sourceObjects, err := src.IntrospectSourceObjects(sourceDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect source objects: %w", err)
	}

	typeMap := effectiveTypeMapping(cfg)
	semanticWarnings, err := introspectSourceSchemaSemanticWarnings(sourceDB, src, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema semantics: %w", err)
	}
	report := buildPlanReport(schema, sourceObjects, semanticWarnings, src, cfg, typeMap)

	if format == "json" {
		if err := writePlanJSON(out, report); err != nil {
			return err
		}
	} else {
		writePlanText(out, report)
	}

	if planOutputDir != "" {
		if err := writeHookSkeletons(planOutputDir, report, cfg.Schema); err != nil {
			return fmt.Errorf("write hook skeletons: %w", err)
		}
		log.Printf("hook skeletons written to %s", planOutputDir)
	}

	return nil
}

func buildPlanReport(schema *Schema, sourceObjects *SourceObjects, semanticWarnings []SchemaSemanticWarning, src SourceDB, cfg *MigrationConfig, typeMap TypeMappingConfig) *PlanReport {
	report := &PlanReport{
		RequiredExtensions:      []PlanRequiredExtension{},
		UnsupportedColumns:      []PlanUnsupportedColumn{},
		SchemaSemanticWarnings:  []SchemaSemanticWarning{},
		GeneratedColumns:        []PlanGeneratedColumn{},
		SkippedIndexes:          []PlanSkippedIndex{},
		OrphanCleanupCandidates: []PlanOrphanCleanupCandidate{},
		TemporalWarnings:        []PlanTemporalWarning{},
		CollationWarnings:       []string{},
	}

	for _, req := range collectRequiredExtensions(schema, src, cfg, typeMap) {
		mode := "require_existing"
		if req.CreateIfMissing {
			mode = "create_if_missing"
		}
		report.RequiredExtensions = append(report.RequiredExtensions, PlanRequiredExtension{
			Name:    req.Name,
			Feature: req.Feature,
			Mode:    mode,
		})
	}

	// Source objects
	if sourceObjects != nil {
		report.SourceObjects.Views = ensureStringSlice(sourceObjects.Views)
		report.SourceObjects.Routines = ensureStringSlice(sourceObjects.Routines)
		report.SourceObjects.Triggers = ensureStringSlice(sourceObjects.Triggers)
	} else {
		report.SourceObjects.Views = []string{}
		report.SourceObjects.Routines = []string{}
		report.SourceObjects.Triggers = []string{}
	}

	if src != nil {
		for _, t := range schema.Tables {
			for _, col := range t.Columns {
				if _, err := src.MapType(col, typeMap); err != nil {
					report.UnsupportedColumns = append(report.UnsupportedColumns, PlanUnsupportedColumn{
						Table:      t.PGName,
						Column:     col.PGName,
						SourceType: col.ColumnType,
						Reason:     err.Error(),
					})
				}
			}
		}
	}

	report.SchemaSemanticWarnings = collectSchemaSemanticWarnings(schema, src, typeMap, semanticWarnings)

	// Generated columns
	for _, t := range schema.Tables {
		for _, col := range t.Columns {
			if !isGeneratedColumn(col) {
				continue
			}
			expr := col.GenerationExpression
			if expr == "" {
				expr = col.Extra
			}
			report.GeneratedColumns = append(report.GeneratedColumns, PlanGeneratedColumn{
				Table:      t.PGName,
				Column:     col.PGName,
				Expression: expr,
			})
		}
	}

	// Skipped indexes
	for _, t := range schema.Tables {
		for _, idx := range t.Indexes {
			if reason, unsupported := indexUnsupportedReason(t, idx, typeMap); unsupported {
				report.SkippedIndexes = append(report.SkippedIndexes, PlanSkippedIndex{
					Table:  t.PGName,
					Index:  idx.Name,
					Reason: reason,
				})
			}
		}
	}

	if cfg.CleanOrphans {
		for _, t := range schema.Tables {
			for _, fk := range t.ForeignKeys {
				report.OrphanCleanupCandidates = append(report.OrphanCleanupCandidates, PlanOrphanCleanupCandidate{
					Table:      t.PGName,
					ForeignKey: fk.Name,
					Columns:    append([]string(nil), fk.Columns...),
					RefTable:   fk.RefPGTable,
					RefColumns: append([]string(nil), fk.RefColumns...),
					Action:     orphanCleanupAction(fk),
				})
			}
		}
	}
	sourceType := ""
	if cfg != nil {
		sourceType = cfg.Source.Type
	}
	report.TemporalWarnings = collectTemporalWarnings(schema, sourceType, typeMap)

	// Collation warnings
	if warnings := collectCollationWarnings(schema, typeMap); len(warnings) > 0 {
		report.CollationWarnings = warnings
	}

	return report
}

func ensureStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func writePlanJSON(w io.Writer, report *PlanReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func writePlanText(w io.Writer, report *PlanReport) {
	hasContent := false

	if len(report.RequiredExtensions) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Required Extensions (%d)\n\n", len(report.RequiredExtensions))
		for _, req := range report.RequiredExtensions {
			action := "must already exist on the target"
			if req.Mode == "create_if_missing" {
				action = "pgferry will create it if missing"
			}
			fmt.Fprintf(w, "  - %s (%s): %s\n", req.Name, req.Feature, action)
		}
		fmt.Fprintln(w)
	}

	// Source objects
	objs := &report.SourceObjects
	if len(objs.Views) > 0 || len(objs.Routines) > 0 || len(objs.Triggers) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Source Objects (require manual migration)\n\n")
		if len(objs.Views) > 0 {
			fmt.Fprintf(w, "Views (%d):\n", len(objs.Views))
			for _, v := range objs.Views {
				fmt.Fprintf(w, "  - %s\n", v)
			}
			fmt.Fprintf(w, "  Recommended hook phase: after_all\n\n")
		}
		if len(objs.Routines) > 0 {
			fmt.Fprintf(w, "Routines (%d):\n", len(objs.Routines))
			for _, r := range objs.Routines {
				fmt.Fprintf(w, "  - %s\n", r)
			}
			fmt.Fprintf(w, "  Recommended hook phase: after_all\n\n")
		}
		if len(objs.Triggers) > 0 {
			fmt.Fprintf(w, "Triggers (%d):\n", len(objs.Triggers))
			for _, t := range objs.Triggers {
				fmt.Fprintf(w, "  - %s\n", t)
			}
			fmt.Fprintf(w, "  Recommended hook phase: after_all\n\n")
		}
	}

	if len(report.UnsupportedColumns) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Unsupported Columns (%d)\n\n", len(report.UnsupportedColumns))
		fmt.Fprintf(w, "These columns cannot be migrated automatically with the current configuration.\n\n")
		for _, uc := range report.UnsupportedColumns {
			fmt.Fprintf(w, "  - %s.%s (%s): %s\n", uc.Table, uc.Column, uc.SourceType, uc.Reason)
		}
		fmt.Fprintln(w)
	}

	if len(report.SchemaSemanticWarnings) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Schema Semantic Warnings (%d)\n\n", len(report.SchemaSemanticWarnings))
		fmt.Fprintf(w, "These items do not stop the migration, but pgferry will skip source semantics or leave them for manual recreation.\n\n")
		for _, category := range orderedSchemaSemanticWarningCategories(report.SchemaSemanticWarnings) {
			fmt.Fprintf(w, "%s (%d):\n", schemaSemanticWarningCategoryTitle(category), countSchemaSemanticWarningsByCategory(report.SchemaSemanticWarnings, category))
			for _, warning := range report.SchemaSemanticWarnings {
				if warning.Category != category {
					continue
				}
				fmt.Fprintf(w, "  - %s [%s]: %s\n", warning.ObjectName, warning.Disposition, warning.Reason)
				if warning.RecommendedFollowUp != "" {
					fmt.Fprintf(w, "    Follow-up: %s\n", warning.RecommendedFollowUp)
				}
			}
			fmt.Fprintln(w)
		}
	}

	// Generated columns
	if len(report.GeneratedColumns) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Generated Columns (%d)\n\n", len(report.GeneratedColumns))
		fmt.Fprintf(w, "These columns will be materialized as plain data. Generation expressions\n")
		fmt.Fprintf(w, "must be recreated manually in PostgreSQL.\n\n")
		for _, gc := range report.GeneratedColumns {
			fmt.Fprintf(w, "  - %s.%s (%s)\n", gc.Table, gc.Column, gc.Expression)
		}
		fmt.Fprintf(w, "  Recommended hook phase: after_data\n\n")
	}

	// Skipped indexes
	if len(report.SkippedIndexes) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Skipped Indexes (%d)\n\n", len(report.SkippedIndexes))
		fmt.Fprintf(w, "These indexes cannot be migrated automatically and need manual recreation.\n\n")
		for _, si := range report.SkippedIndexes {
			fmt.Fprintf(w, "  - %s.%s: %s\n", si.Table, si.Index, si.Reason)
		}
		fmt.Fprintf(w, "  Recommended hook phase: after_all\n\n")
	}

	if len(report.OrphanCleanupCandidates) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Orphan Cleanup Candidates (%d)\n\n", len(report.OrphanCleanupCandidates))
		fmt.Fprintf(w, "These foreign keys are eligible for automatic orphan cleanup inspection before PostgreSQL foreign keys are created.\n")
		fmt.Fprintf(w, "Actions are based on each FK's ON DELETE rule. Row counts are determined during migration runtime.\n\n")
		for _, risk := range report.OrphanCleanupCandidates {
			fmt.Fprintf(w, "  - %s.%s (%s) -> %s (%s): %s\n",
				risk.Table,
				risk.ForeignKey,
				strings.Join(risk.Columns, ", "),
				risk.RefTable,
				strings.Join(risk.RefColumns, ", "),
				orphanCleanupActionLabel(risk.Action))
		}
		fmt.Fprintln(w)
	}

	if len(report.TemporalWarnings) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Temporal Warnings (%d)\n\n", len(report.TemporalWarnings))
		fmt.Fprintf(w, "These mappings are valid, but they can change application-visible time or timezone semantics. They are advisory and do not block execution.\n\n")
		for _, tw := range report.TemporalWarnings {
			fmt.Fprintf(w, "  - %s\n", tw.Summary)
			if len(tw.Examples) > 0 {
				fmt.Fprintf(w, "    Examples: %s\n", strings.Join(tw.Examples, ", "))
			}
			if tw.Remediation != "" {
				fmt.Fprintf(w, "    Review: %s\n", tw.Remediation)
			}
		}
		fmt.Fprintln(w)
	}

	// Collation warnings
	if len(report.CollationWarnings) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Collation Warnings (%d)\n\n", len(report.CollationWarnings))
		for _, cw := range report.CollationWarnings {
			fmt.Fprintf(w, "  - %s\n", cw)
		}
		fmt.Fprintln(w)
	}

	if !hasContent {
		fmt.Fprintln(w, "No manual follow-up items detected.")
	}
}

// writeHookSkeletons creates hook SQL skeleton files in the output directory.
func writeHookSkeletons(dir string, report *PlanReport, schema string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	type hookFile struct {
		name    string
		content string
	}

	var files []hookFile

	// before_data: rarely needed for plan items, but generate if useful
	if body := buildBeforeDataSkeleton(); body != "" {
		files = append(files, hookFile{"before_data.sql", body})
	}

	// after_data: generated columns
	if body := buildAfterDataSkeleton(report, schema); body != "" {
		files = append(files, hookFile{"after_data.sql", body})
	}

	// before_fk: rarely needed, skip unless there's content
	if body := buildBeforeFkSkeleton(); body != "" {
		files = append(files, hookFile{"before_fk.sql", body})
	}

	// after_all: views, routines, triggers, skipped indexes
	if body := buildAfterAllSkeleton(report, schema); body != "" {
		files = append(files, hookFile{"after_all.sql", body})
	}

	if len(files) == 0 {
		return nil
	}

	for _, f := range files {
		path := filepath.Join(dir, f.name)
		if err := os.WriteFile(path, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}

	return nil
}

func buildBeforeDataSkeleton() string {
	return ""
}

func buildBeforeFkSkeleton() string {
	return ""
}

func buildAfterDataSkeleton(report *PlanReport, schema string) string {
	if len(report.GeneratedColumns) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("-- after_data hook: generated column expressions\n")
	b.WriteString("-- These columns were materialized as plain data during migration.\n")
	b.WriteString("-- Recreate generation expressions or computed columns as needed.\n")
	b.WriteString("--\n")
	b.WriteString("-- Schema: {{schema}}\n\n")

	// Group by table for readability
	byTable := groupGeneratedColumnsByTable(report.GeneratedColumns)
	for _, table := range sortedGeneratedColumnTables(byTable) {
		cols := byTable[table]
		fmt.Fprintf(&b, "-- Table: %s\n", table)
		for _, gc := range cols {
			fmt.Fprintf(&b, "-- TODO: ALTER TABLE %s.%s\n", pgIdent("{{schema}}"), pgIdent(gc.Table))
			fmt.Fprintf(&b, "--        ALTER COLUMN %s SET EXPRESSION AS (...);\n", pgIdent(gc.Column))
			fmt.Fprintf(&b, "-- Source expression: %s\n", gc.Expression)
		}
		b.WriteByte('\n')
	}

	return b.String()
}

func buildAfterAllSkeleton(report *PlanReport, schema string) string {
	objs := &report.SourceObjects
	hasObjects := len(objs.Views) > 0 || len(objs.Routines) > 0 || len(objs.Triggers) > 0
	hasIndexes := len(report.SkippedIndexes) > 0

	if !hasObjects && !hasIndexes {
		return ""
	}

	var b strings.Builder
	b.WriteString("-- after_all hook: objects requiring manual migration\n")
	b.WriteString("-- Schema: {{schema}}\n\n")

	if len(objs.Views) > 0 {
		b.WriteString("-- Views\n")
		b.WriteString("-- Recreate these views in PostgreSQL syntax.\n")
		for _, v := range objs.Views {
			fmt.Fprintf(&b, "-- TODO: CREATE VIEW %s.%s AS ...;\n", pgIdent("{{schema}}"), pgIdent(v))
		}
		b.WriteByte('\n')
	}

	if len(objs.Routines) > 0 {
		b.WriteString("-- Routines (functions/procedures)\n")
		b.WriteString("-- Rewrite these in PL/pgSQL or another PostgreSQL procedural language.\n")
		for _, r := range objs.Routines {
			fmt.Fprintf(&b, "-- TODO: %s — rewrite for PostgreSQL\n", r)
		}
		b.WriteByte('\n')
	}

	if len(objs.Triggers) > 0 {
		b.WriteString("-- Triggers\n")
		b.WriteString("-- Recreate these triggers using PostgreSQL trigger functions.\n")
		for _, t := range objs.Triggers {
			fmt.Fprintf(&b, "-- TODO: CREATE TRIGGER %s ...;\n", pgIdent(t))
		}
		b.WriteByte('\n')
	}

	if hasIndexes {
		b.WriteString("-- Skipped Indexes\n")
		b.WriteString("-- These indexes could not be migrated automatically.\n")
		for _, si := range report.SkippedIndexes {
			fmt.Fprintf(&b, "-- TODO: CREATE INDEX ON %s.%s ...;\n", pgIdent("{{schema}}"), pgIdent(si.Table))
			fmt.Fprintf(&b, "--   Source: %s.%s — %s\n", si.Table, si.Index, si.Reason)
		}
		b.WriteByte('\n')
	}

	return b.String()
}

func groupGeneratedColumnsByTable(cols []PlanGeneratedColumn) map[string][]PlanGeneratedColumn {
	m := make(map[string][]PlanGeneratedColumn)
	for _, c := range cols {
		m[c.Table] = append(m[c.Table], c)
	}
	return m
}

func sortedGeneratedColumnTables(m map[string][]PlanGeneratedColumn) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func orderedSchemaSemanticWarningCategories(warnings []SchemaSemanticWarning) []string {
	seen := make(map[string]bool)
	categories := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if seen[warning.Category] {
			continue
		}
		seen[warning.Category] = true
		categories = append(categories, warning.Category)
	}
	sort.Slice(categories, func(i, j int) bool {
		return schemaSemanticWarningCategoryRank(categories[i]) < schemaSemanticWarningCategoryRank(categories[j])
	})
	return categories
}

func countSchemaSemanticWarningsByCategory(warnings []SchemaSemanticWarning, category string) int {
	count := 0
	for _, warning := range warnings {
		if warning.Category == category {
			count++
		}
	}
	return count
}
