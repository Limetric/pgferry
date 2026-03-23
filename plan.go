package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/spf13/cobra"
)

// errInvalidPlanFormat is returned when plan output format is not text or json.
var errInvalidPlanFormat = errors.New("--format must be text or json")

var planOutputDir string
var planFormat string
var planFailOn string
var planInputPath string

var planCmd = &cobra.Command{
	Use:   "plan [migration.toml] | plan --input <report.json>",
	Short: "Analyze source schema and generate a migration plan report",
	Long: `Analyze the source database schema and produce a report of objects that
require manual follow-up: views, routines, triggers, generated columns,
and skipped indexes.

Use --input with a JSON report from a previous --format json run to re-render
or apply --fail-on checks without connecting to the source. --input cannot be
combined with a migration config (--config or a positional TOML path); use one
or the other.

Optionally generates hook skeleton files in the specified output directory.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPlan,
}

var planConfigPath string

func init() {
	planCmd.Flags().StringVar(&planConfigPath, "config", "", "path to migration TOML config file")
	planCmd.Flags().StringVar(&planInputPath, "input", "", "read a previously saved JSON plan report instead of connecting to the source")
	planCmd.Flags().StringVar(&planOutputDir, "output-dir", "", "directory to write hook skeleton files")
	planCmd.Flags().StringVar(&planFormat, "format", "text", "output format: text or json")
	// fail-on=none keeps exit 0 when introspection succeeds; errors/warnings gate CI on unsupported columns or high copy-risk severity.
	planCmd.Flags().StringVar(&planFailOn, "fail-on", "none", "exit non-zero on findings: none, errors, or warnings")
}

// PlanOptions configures plan execution (wizard and tests pass explicit values; CLI uses flags via runPlan).
type PlanOptions struct {
	// FailOn is none, errors, or warnings (see --fail-on). Empty behaves like none.
	FailOn string
	// Format is text or json. Empty means text.
	Format string
	// OutputDir is the directory for hook skeleton files; empty skips writing hooks.
	OutputDir string
}

// PlanFindingsError is returned when --fail-on is triggered. main exits non-zero and prints Error() to stderr
// (text mode also prints the same summary to stdout after the report).
type PlanFindingsError struct {
	UnsupportedColumns int
	HighSeverityRisks  int
}

func (e *PlanFindingsError) Error() string {
	return planFindingsFailSummary(e.UnsupportedColumns, e.HighSeverityRisks)
}

func planFindingsFailSummary(unsupportedCols, highSeverityRisks int) string {
	var parts []string
	if unsupportedCols > 0 {
		parts = append(parts, fmt.Sprintf("%d unsupported column(s)", unsupportedCols))
	}
	if highSeverityRisks > 0 {
		parts = append(parts, fmt.Sprintf("%d high-severity copy risk finding(s)", highSeverityRisks))
	}
	// shouldFailPlan guarantees at least one of these counts is non-zero.
	if len(parts) == 0 {
		return "FAIL"
	}
	return "FAIL: " + strings.Join(parts, ", ")
}

func parsePlanFailOn(s string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	if v == "" {
		return "none", nil
	}
	switch v {
	case "none", "errors", "warnings":
		return v, nil
	default:
		return "", fmt.Errorf("--fail-on must be none, errors, or warnings")
	}
}

// wizardPlanOptions is the interactive wizard default: human-readable plan, never CI gate.
func wizardPlanOptions() PlanOptions {
	return PlanOptions{Format: "text", FailOn: "none"}
}

func shouldFailPlan(report *PlanReport, level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "errors":
		return len(report.UnsupportedColumns) > 0
	case "warnings":
		if len(report.UnsupportedColumns) > 0 {
			return true
		}
		for _, r := range report.CopyRiskFindings {
			if strings.EqualFold(r.Severity, "high") {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func countHighSeverityCopyRisks(report *PlanReport) int {
	n := 0
	for _, r := range report.CopyRiskFindings {
		if strings.EqualFold(r.Severity, "high") {
			n++
		}
	}
	return n
}

// PlanTableChunkInfo describes how one table will be copied at chunk granularity
// when copy risk analysis is enabled (same probes as copy risk; empty otherwise).
type PlanTableChunkInfo struct {
	Table               string `json:"table"`
	EstimatedRows       int64  `json:"estimated_rows"`
	Chunkable           bool   `json:"chunkable"`
	ChunkKey            string `json:"chunk_key,omitempty"`
	ChunkKeyType        string `json:"chunk_key_type,omitempty"`
	MinPK               *int64 `json:"min_pk,omitempty"`
	MaxPK               *int64 `json:"max_pk,omitempty"`
	EstimatedChunks     int    `json:"estimated_chunks,omitempty"`
	FullTableCopyReason string `json:"full_table_copy_reason,omitempty"`
}

// PlanSkippedForeignKey describes a foreign key omitted because include/exclude table filtering
// dropped the FK or its referenced table from the migrated set.
type PlanSkippedForeignKey struct {
	Table    string `json:"table"`
	Name     string `json:"name"`
	RefTable string `json:"ref_table"`
	Reason   string `json:"reason"`
}

// PlanTableFilterReport summarizes include/exclude table filtering applied before plan analysis.
type PlanTableFilterReport struct {
	TotalTables        int                     `json:"total_tables"`
	SelectedTables     []string                `json:"selected_tables"`
	SkippedTables      []string                `json:"skipped_tables"`
	OverlappingTables  []string                `json:"overlapping_tables"`
	SkippedForeignKeys []PlanSkippedForeignKey `json:"skipped_foreign_keys"`
}

// PlanSummary is a high-level snapshot of the planned migration (config echo + table counts).
type PlanSummary struct {
	SourceType         string `json:"source_type"`
	SourceDatabase     string `json:"source_database"`
	TargetSchema       string `json:"target_schema"`
	TableCount         int    `json:"table_count"`
	TotalEstimatedRows int64  `json:"total_estimated_rows,omitempty"`
	Workers            int    `json:"workers"`
	IndexWorkers       int    `json:"index_workers"`
	ChunkSize          int64  `json:"chunk_size"`
	UnloggedTables     bool   `json:"unlogged_tables"`
	Resume             bool   `json:"resume"`
	Validation         string `json:"validation"`
	SnapshotMode       string `json:"source_snapshot_mode"`
	CopyRiskAnalysis   bool   `json:"copy_risk_analysis"`
	PreserveDefaults   bool   `json:"preserve_defaults"`
	CleanOrphans       bool   `json:"clean_orphans"`
	SnakeCaseIDs       bool   `json:"snake_case_identifiers"`
}

// PlanReport holds all findings from the plan analysis.
type PlanReport struct {
	Summary                 PlanSummary                  `json:"summary"`
	TableFilterReport       *PlanTableFilterReport       `json:"table_filter_report,omitempty"`
	RequiredExtensions      []PlanRequiredExtension      `json:"required_extensions"`
	CopyRiskFindings        []PlanCopyRiskFinding        `json:"copy_risk_findings"`
	TableChunkPlan          []PlanTableChunkInfo         `json:"table_chunk_plan"`
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

// PlanSourceTrigger is a trigger on the source database with its source table.
type PlanSourceTrigger struct {
	Name  string `json:"name"`
	Table string `json:"table,omitempty"`
}

// PlanSourceObjects holds non-table source objects.
type PlanSourceObjects struct {
	Views    []string            `json:"views"`
	Routines []string            `json:"routines"`
	Triggers []PlanSourceTrigger `json:"triggers"`
}

// MarshalJSON emits empty arrays for nil slices (including triggers) so saved reports stay consistent.
func (p PlanSourceObjects) MarshalJSON() ([]byte, error) {
	type out struct {
		Views    []string            `json:"views"`
		Routines []string            `json:"routines"`
		Triggers []PlanSourceTrigger `json:"triggers"`
	}
	return json.Marshal(out{
		Views:    ensureStringSlice(p.Views),
		Routines: ensureStringSlice(p.Routines),
		Triggers: ensurePlanTriggersSlice(p.Triggers),
	})
}

func ensurePlanTriggersSlice(t []PlanSourceTrigger) []PlanSourceTrigger {
	if t == nil {
		return []PlanSourceTrigger{}
	}
	return t
}

// UnmarshalJSON accepts legacy trigger lists as JSON string arrays or the new object form.
func (p *PlanSourceObjects) UnmarshalJSON(data []byte) error {
	var raw struct {
		Views    []string        `json:"views"`
		Routines []string        `json:"routines"`
		Triggers json.RawMessage `json:"triggers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.Views = ensureStringSlice(raw.Views)
	p.Routines = ensureStringSlice(raw.Routines)
	if len(raw.Triggers) == 0 || string(bytes.TrimSpace(raw.Triggers)) == "null" {
		p.Triggers = []PlanSourceTrigger{}
		return nil
	}
	var strs []string
	if err := json.Unmarshal(raw.Triggers, &strs); err == nil {
		p.Triggers = make([]PlanSourceTrigger, len(strs))
		for i, s := range strs {
			p.Triggers[i] = PlanSourceTrigger{Name: s}
		}
		return nil
	}
	var objs []PlanSourceTrigger
	if err := json.Unmarshal(raw.Triggers, &objs); err != nil {
		return fmt.Errorf("source_objects.triggers: expected string array or object array: %w", err)
	}
	p.Triggers = objs
	return nil
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
	switch planFormat {
	case "text", "json":
	default:
		return errInvalidPlanFormat
	}

	failOn, err := parsePlanFailOn(planFailOn)
	if err != nil {
		return err
	}

	opts := PlanOptions{
		FailOn:    failOn,
		Format:    planFormat,
		OutputDir: planOutputDir,
	}

	if planInputPath != "" {
		if planConfigPath != "" {
			return fmt.Errorf("cannot use --config with --input")
		}
		if len(args) > 0 {
			return fmt.Errorf("cannot specify a config file when using --input")
		}
		if planOutputDir != "" {
			return fmt.Errorf("cannot use --output-dir with --input")
		}
		return runPlanFromInput(planInputPath, cmd.OutOrStdout(), opts)
	}

	cfgPath := planConfigPath
	if len(args) > 0 {
		cfgPath = args[0]
	}
	if cfgPath == "" {
		return fmt.Errorf("config file required: pgferry plan <migration.toml> or pgferry plan --config <migration.toml>")
	}

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	return runPlanWithConfig(cfg, cmd.OutOrStdout(), opts)
}

func runPlanWithConfig(cfg *MigrationConfig, out io.Writer, opts PlanOptions) error {
	format := opts.Format
	if format == "" {
		format = "text"
	}
	switch format {
	case "text", "json":
	default:
		return errInvalidPlanFormat
	}
	failOn, err := parsePlanFailOn(opts.FailOn)
	if err != nil {
		return err
	}
	outputDir := opts.OutputDir

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
	if hasTableFilters(cfg) && sourceObjects != nil {
		sourceObjects.Triggers = filterTriggersBySelectedTables(sourceObjects.Triggers, schema)
	}

	typeMap := effectiveTypeMapping(cfg)
	semanticWarnings, err := introspectSourceSchemaSemanticWarnings(sourceDB, src, dbName)
	if err != nil {
		return fmt.Errorf("introspect schema semantics: %w", err)
	}
	copyRisks := []PlanCopyRiskFinding{}
	tableChunkPlan := []PlanTableChunkInfo{}
	if cfg.CopyRiskAnalysis {
		logCopyRiskProbeStart(len(schema.Tables))
		if findings, chunks, err := collectCopyRiskFindingsAndTableChunkPlan(ctx, sourceDB, src, schema, cfg.ChunkSize); err != nil {
			log.Printf("WARN: copy risk analysis skipped: %v", err)
		} else {
			copyRisks = findings
			tableChunkPlan = chunks
		}
	}
	summary := buildPlanSummary(schema, cfg, dbName, copyRisks, cfg.CopyRiskAnalysis)
	report := buildPlanReport(schema, sourceObjects, semanticWarnings, copyRisks, tableChunkPlan, src, cfg, typeMap, summary)
	if hasTableFilters(cfg) {
		report.TableFilterReport = newPlanTableFilterReport(filterReport)
	}

	if err := writePlanReportOutput(report, out, format); err != nil {
		return err
	}
	if outputDir != "" {
		if err := writeHookSkeletons(outputDir, report, cfg.Schema); err != nil {
			return fmt.Errorf("write hook skeletons: %w", err)
		}
		log.Printf("hook skeletons written to %s", outputDir)
	}
	return applyPlanFailOn(report, out, format, failOn)
}

func writePlanReportOutput(report *PlanReport, out io.Writer, format string) error {
	if format == "" {
		format = "text"
	}
	switch format {
	case "json":
		return writePlanJSON(out, report)
	case "text":
		writePlanText(out, report)
		return nil
	default:
		return errInvalidPlanFormat
	}
}

func applyPlanFailOn(report *PlanReport, out io.Writer, format string, failOn string) error {
	if !shouldFailPlan(report, failOn) {
		return nil
	}
	highRisks := countHighSeverityCopyRisks(report)
	nUnsupported := len(report.UnsupportedColumns)
	if format == "" {
		format = "text"
	}
	if format == "text" {
		fmt.Fprintln(out, planFindingsFailSummary(nUnsupported, highRisks))
	}
	return &PlanFindingsError{
		UnsupportedColumns: nUnsupported,
		HighSeverityRisks:  highRisks,
	}
}

// renderPlanReport writes the plan report in the requested format and applies --fail-on.
// It validates format and fail-on again so callers other than runPlan (e.g. tests) get
// the same checks as the CLI path; runPlan already validates before runPlanFromInput.
func renderPlanReport(report *PlanReport, out io.Writer, opts PlanOptions) error {
	format := opts.Format
	if format == "" {
		format = "text"
	}
	failOn, err := parsePlanFailOn(opts.FailOn)
	if err != nil {
		return err
	}
	switch format {
	case "text", "json":
	default:
		return errInvalidPlanFormat
	}
	if err := writePlanReportOutput(report, out, format); err != nil {
		return err
	}
	return applyPlanFailOn(report, out, format, failOn)
}

func runPlanFromInput(path string, out io.Writer, opts PlanOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read plan input: %w", err)
	}
	var report PlanReport
	// Unknown JSON fields are ignored so newer pgferry versions can extend PlanReport
	// without breaking older binaries re-reading saved reports.
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("decode plan input: %w", err)
	}
	return renderPlanReport(&report, out, opts)
}

// buildPlanSummary copies migration config into PlanSummary. copyRiskEnabled is
// passed explicitly (not inferred only inside this function) so callers and tests
// can control whether TotalEstimatedRows is filled from copyRisks without
// mutating cfg.
func buildPlanSummary(schema *Schema, cfg *MigrationConfig, dbName string, copyRisks []PlanCopyRiskFinding, copyRiskEnabled bool) PlanSummary {
	if cfg == nil {
		return PlanSummary{}
	}
	s := PlanSummary{
		SourceType:       cfg.Source.Type,
		SourceDatabase:   dbName,
		TargetSchema:     cfg.Schema,
		Workers:          cfg.Workers,
		IndexWorkers:     cfg.IndexWorkers,
		ChunkSize:        cfg.ChunkSize,
		UnloggedTables:   cfg.UnloggedTables,
		Resume:           cfg.Resume,
		Validation:       cfg.Validation,
		SnapshotMode:     cfg.SourceSnapshotMode,
		CopyRiskAnalysis: cfg.CopyRiskAnalysis,
		PreserveDefaults: cfg.PreserveDefaults,
		CleanOrphans:     cfg.CleanOrphans,
		SnakeCaseIDs:     cfg.SnakeCaseIdentifiers,
	}
	if schema != nil {
		s.TableCount = len(schema.Tables)
	}
	if copyRiskEnabled {
		s.TotalEstimatedRows = sumCopyRiskEstimatedRowsByTable(copyRisks)
	}
	return s
}

func newPlanTableFilterReport(r schemaFilterReport) *PlanTableFilterReport {
	out := &PlanTableFilterReport{
		TotalTables: r.TotalTables,
		// Non-nil empty slices so JSON encodes [] instead of null for absent entries.
		SelectedTables:     append(make([]string, 0, len(r.SelectedTables)), r.SelectedTables...),
		SkippedTables:      append(make([]string, 0, len(r.SkippedTables)), r.SkippedTables...),
		OverlappingTables:  append(make([]string, 0, len(r.OverlappingTables)), r.OverlappingTables...),
		SkippedForeignKeys: make([]PlanSkippedForeignKey, 0, len(r.SkippedForeignKeys)),
	}
	for _, fk := range r.SkippedForeignKeys {
		out.SkippedForeignKeys = append(out.SkippedForeignKeys, PlanSkippedForeignKey(fk))
	}
	return out
}

func sumCopyRiskEstimatedRowsByTable(findings []PlanCopyRiskFinding) int64 {
	byTable := make(map[string]int64)
	for _, f := range findings {
		if f.EstimatedRows > byTable[f.Table] {
			byTable[f.Table] = f.EstimatedRows
		}
	}
	var total int64
	for _, n := range byTable {
		total += n
	}
	return total
}

// planSummaryHasData is true when Summary was produced by a connected plan run.
// Reports loaded from older JSON without a summary block decode as the zero
// struct; SourceType stays empty so we skip printing an empty Summary section.
func planSummaryHasData(s PlanSummary) bool {
	return s.SourceType != ""
}

func formatInt64Thousands(n int64) string {
	if n < 0 {
		// Row counts are non-negative; avoid -math.MinInt64 overflow from recursive -n.
		return strconv.FormatInt(n, 10)
	}
	s := strconv.FormatUint(uint64(n), 10)
	if len(s) <= 3 {
		return s
	}
	var chunks []string
	for len(s) > 3 {
		chunks = append(chunks, s[len(s)-3:])
		s = s[:len(s)-3]
	}
	chunks = append(chunks, s)
	for i, j := 0, len(chunks)-1; i < j; i, j = i+1, j-1 {
		chunks[i], chunks[j] = chunks[j], chunks[i]
	}
	return strings.Join(chunks, ",")
}

func buildPlanReport(schema *Schema, sourceObjects *SourceObjects, semanticWarnings []SchemaSemanticWarning, copyRisks []PlanCopyRiskFinding, tableChunkPlan []PlanTableChunkInfo, src SourceDB, cfg *MigrationConfig, typeMap TypeMappingConfig, summary PlanSummary) *PlanReport {
	report := &PlanReport{
		Summary:                 summary,
		RequiredExtensions:      []PlanRequiredExtension{},
		CopyRiskFindings:        []PlanCopyRiskFinding{},
		TableChunkPlan:          []PlanTableChunkInfo{},
		UnsupportedColumns:      []PlanUnsupportedColumn{},
		SchemaSemanticWarnings:  []SchemaSemanticWarning{},
		GeneratedColumns:        []PlanGeneratedColumn{},
		SkippedIndexes:          []PlanSkippedIndex{},
		OrphanCleanupCandidates: []PlanOrphanCleanupCandidate{},
		TemporalWarnings:        []PlanTemporalWarning{},
		CollationWarnings:       []string{},
	}
	if copyRisks != nil {
		report.CopyRiskFindings = copyRisks
	}
	if tableChunkPlan != nil {
		report.TableChunkPlan = tableChunkPlan
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
		report.SourceObjects.Triggers = planTriggersFromSource(sourceObjects.Triggers)
	} else {
		report.SourceObjects.Views = []string{}
		report.SourceObjects.Routines = []string{}
		report.SourceObjects.Triggers = []PlanSourceTrigger{}
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

	preserveDefaults := false
	if cfg != nil {
		preserveDefaults = cfg.PreserveDefaults
	}
	report.SchemaSemanticWarnings = collectSchemaSemanticWarnings(schema, src, preserveDefaults, typeMap, semanticWarnings)

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

	if cfg != nil && cfg.CleanOrphans {
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

func planTriggersFromSource(tr []SourceTrigger) []PlanSourceTrigger {
	if len(tr) == 0 {
		return []PlanSourceTrigger{}
	}
	out := make([]PlanSourceTrigger, len(tr))
	for i, t := range tr {
		out[i] = PlanSourceTrigger(t)
	}
	return out
}

func writePlanJSON(w io.Writer, report *PlanReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

const (
	minTableChunkPlanTableColWidth = 22
	maxTableChunkPlanTableColWidth = 48
)

func tableChunkPlanTableColumnWidth(plan []PlanTableChunkInfo) int {
	w := len("Table")
	for _, row := range plan {
		if n := len(row.Table); n > w {
			w = n
		}
	}
	if w < minTableChunkPlanTableColWidth {
		w = minTableChunkPlanTableColWidth
	}
	if w > maxTableChunkPlanTableColWidth {
		w = maxTableChunkPlanTableColWidth
	}
	return w
}

func truncateTableChunkPlanTableName(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return s[:width-1] + "…"
}

func writePlanText(w io.Writer, report *PlanReport) {
	if report == nil {
		fmt.Fprintln(w, "No manual follow-up items detected.")
		return
	}

	hasContent := false

	if planSummaryHasData(report.Summary) {
		hasContent = true
		s := report.Summary
		fmt.Fprintf(w, "## Summary\n\n")
		fmt.Fprintf(w, "Source: %s (%s)\n", s.SourceType, s.SourceDatabase)
		fmt.Fprintf(w, "Target schema: %s\n", s.TargetSchema)
		fmt.Fprintf(w, "Tables: %d\n", s.TableCount)
		if s.CopyRiskAnalysis {
			fmt.Fprintf(w, "Estimated rows: %s\n", formatInt64Thousands(s.TotalEstimatedRows))
		}
		fmt.Fprintf(w, "Config: workers=%d index_workers=%d chunk_size=%d resume=%t validation=%s\n",
			s.Workers, s.IndexWorkers, s.ChunkSize, s.Resume, s.Validation)
		fmt.Fprintf(w, "        source_snapshot_mode=%s copy_risk_analysis=%t unlogged_tables=%t preserve_defaults=%t clean_orphans=%t snake_case_identifiers=%t\n",
			s.SnapshotMode, s.CopyRiskAnalysis, s.UnloggedTables, s.PreserveDefaults, s.CleanOrphans, s.SnakeCaseIDs)
		fmt.Fprintln(w)
	}

	if report.TableFilterReport != nil {
		hasContent = true
		tf := report.TableFilterReport
		fmt.Fprintf(w, "## Table Filters\n\n")
		fmt.Fprintf(w, "Selected %d of %d source table(s).\n\n", len(tf.SelectedTables), tf.TotalTables)
		if len(tf.SelectedTables) > 0 {
			fmt.Fprintf(w, "Selected tables (%d):\n", len(tf.SelectedTables))
			for _, name := range tf.SelectedTables {
				fmt.Fprintf(w, "  - %s\n", name)
			}
			fmt.Fprintln(w)
		}
		if len(tf.SkippedTables) > 0 {
			fmt.Fprintf(w, "Skipped tables (%d):\n", len(tf.SkippedTables))
			for _, name := range tf.SkippedTables {
				fmt.Fprintf(w, "  - %s\n", name)
			}
			fmt.Fprintln(w)
		}
		if len(tf.OverlappingTables) > 0 {
			fmt.Fprintf(w, "Overlapping include/exclude entries — exclude_tables wins (%d):\n", len(tf.OverlappingTables))
			for _, line := range tf.OverlappingTables {
				fmt.Fprintf(w, "  - %s\n", line)
			}
			fmt.Fprintln(w)
		}
		if len(tf.SkippedForeignKeys) > 0 {
			fmt.Fprintf(w, "Skipped foreign keys (%d):\n", len(tf.SkippedForeignKeys))
			for _, fk := range tf.SkippedForeignKeys {
				fmt.Fprintf(w, "  - %s.%s -> %s: %s\n", fk.Table, fk.Name, fk.RefTable, fk.Reason)
			}
			fmt.Fprintln(w)
		}
	}

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

	if len(report.CopyRiskFindings) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Copy Risk Findings (%d)\n\n", len(report.CopyRiskFindings))
		fmt.Fprintf(w, "These tables are likely runtime hotspots during COPY. chunk_size is key-range width, not a promise of rows per chunk.\n\n")
		for _, risk := range report.CopyRiskFindings {
			fmt.Fprintf(w, "  - %s [%s] %s: %s\n", risk.Table, strings.ToUpper(risk.Severity), copyRiskCategoryTitle(risk.Category), risk.Reason)
			if risk.Chunkable {
				fmt.Fprintf(w, "    Chunking: eligible on %s", risk.ChunkKey)
				if risk.ChunkKeyType != "" {
					fmt.Fprintf(w, " (%s)", risk.ChunkKeyType)
				}
				if risk.MinPK != nil && risk.MaxPK != nil {
					fmt.Fprintf(w, "; range=%d..%d", *risk.MinPK, *risk.MaxPK)
				}
				if risk.EstimatedChunkCount > 0 {
					fmt.Fprintf(w, "; estimated_chunks=%d", risk.EstimatedChunkCount)
				}
				if risk.RangeDensity > 0 {
					fmt.Fprintf(w, "; density=%.2f%%", risk.RangeDensity*100)
				}
				fmt.Fprintf(w, "; rows=%d\n", risk.EstimatedRows)
			} else {
				fmt.Fprintf(w, "    Chunking: not eligible; rows=%d\n", risk.EstimatedRows)
			}
			fmt.Fprintf(w, "    Recommendation: %s\n", risk.Recommendation)
		}
		fmt.Fprintln(w)
	}

	if len(report.TableChunkPlan) > 0 {
		hasContent = true
		fmt.Fprintf(w, "## Table Chunk Plan (%d)\n\n", len(report.TableChunkPlan))
		fmt.Fprintf(w, "How each non-empty table will be split for COPY (chunk_size is key-range width, not rows per chunk).\n\n")
		tw := tableChunkPlanTableColumnWidth(report.TableChunkPlan)
		fmt.Fprintf(w, "  %-*s %12s %8s %-14s %s\n", tw, "Table", "Rows", "Chunks", "Key", "Type")
		for _, row := range report.TableChunkPlan {
			rows := humanize.Comma(row.EstimatedRows)
			var chunks, keyCol, typeCol string
			if row.Chunkable {
				chunks = fmt.Sprintf("%d", row.EstimatedChunks)
				keyCol = row.ChunkKey
				typeCol = row.ChunkKeyType
			} else {
				chunks = "—"
				keyCol = "(full copy)"
				typeCol = row.FullTableCopyReason
			}
			name := truncateTableChunkPlanTableName(row.Table, tw)
			fmt.Fprintf(w, "  %-*s %12s %8s %-14s %s\n", tw, name, rows, chunks, keyCol, typeCol)
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
			if report.TableFilterReport != nil {
				fmt.Fprintf(w, "  Note: These are all views in the source database. Some may reference\n")
				fmt.Fprintf(w, "  tables outside your include_tables/exclude_tables scope.\n")
			}
			for _, v := range objs.Views {
				fmt.Fprintf(w, "  - %s\n", v)
			}
			fmt.Fprintf(w, "  Recommended hook phase: after_all\n\n")
		}
		if len(objs.Routines) > 0 {
			fmt.Fprintf(w, "Routines (%d):\n", len(objs.Routines))
			if report.TableFilterReport != nil {
				fmt.Fprintf(w, "  Note: These are all routines in the source database. Some may reference\n")
				fmt.Fprintf(w, "  tables outside your include_tables/exclude_tables scope.\n")
			}
			for _, r := range objs.Routines {
				fmt.Fprintf(w, "  - %s\n", r)
			}
			fmt.Fprintf(w, "  Recommended hook phase: after_all\n\n")
		}
		if len(objs.Triggers) > 0 {
			fmt.Fprintf(w, "Triggers (%d):\n", len(objs.Triggers))
			for _, tg := range objs.Triggers {
				if tg.Table != "" {
					fmt.Fprintf(w, "  - %s (on %s)\n", tg.Name, tg.Table)
				} else {
					fmt.Fprintf(w, "  - %s\n", tg.Name)
				}
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
		byCategory := groupSchemaSemanticWarningsByCategory(report.SchemaSemanticWarnings)
		for _, category := range orderedSchemaSemanticWarningCategories(report.SchemaSemanticWarnings) {
			fmt.Fprintf(w, "%s (%d):\n", schemaSemanticWarningCategoryTitle(category), len(byCategory[category]))
			for _, warning := range byCategory[category] {
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

	// before_data: extension setup and other pre-load prerequisites
	if body := buildBeforeDataSkeleton(report); body != "" {
		files = append(files, hookFile{"before_data.sql", body})
	}

	// after_data: generated columns
	if body := buildAfterDataSkeleton(report); body != "" {
		files = append(files, hookFile{"after_data.sql", body})
	}

	// after_all: views, routines, triggers, skipped indexes
	if body := buildAfterAllSkeleton(report); body != "" {
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

func buildBeforeDataSkeleton(report *PlanReport) string {
	if report == nil || len(report.RequiredExtensions) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("-- before_data hook: required PostgreSQL extensions\n")
	b.WriteString("-- Schema: {{schema}}\n\n")
	b.WriteString("-- pgferry requires these extensions for the configured type mappings.\n")

	for _, req := range report.RequiredExtensions {
		feature := sanitizeSQLCommentText(req.Feature)
		switch req.Mode {
		case "create_if_missing":
			fmt.Fprintf(&b, "CREATE EXTENSION IF NOT EXISTS %s; -- %s\n", pgIdent(req.Name), feature)
		case "require_existing":
			fmt.Fprintf(&b, "-- Extension %s must already exist before running pgferry. (%s)\n", pgIdent(req.Name), feature)
		default:
			fmt.Fprintf(&b, "-- Extension %s is required for %s.\n", pgIdent(req.Name), feature)
		}
	}

	return b.String()
}

func buildAfterDataSkeleton(report *PlanReport) string {
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
			fmt.Fprintf(&b, "--        DROP COLUMN %s;\n", pgIdent(gc.Column))
			fmt.Fprintf(&b, "-- TODO: ALTER TABLE %s.%s\n", pgIdent("{{schema}}"), pgIdent(gc.Table))
			fmt.Fprintf(&b, "--        ADD COLUMN %s <type> GENERATED ALWAYS AS (...) STORED;\n", pgIdent(gc.Column))
			fmt.Fprintf(&b, "-- Source expression: %s\n", gc.Expression)
		}
		b.WriteByte('\n')
	}

	return b.String()
}

func buildAfterAllSkeleton(report *PlanReport) string {
	objs := &report.SourceObjects
	hasObjects := len(objs.Views) > 0 || len(objs.Routines) > 0 || len(objs.Triggers) > 0
	hasIndexes := len(report.SkippedIndexes) > 0
	hasUnsupportedColumns := len(report.UnsupportedColumns) > 0

	if !hasObjects && !hasIndexes && !hasUnsupportedColumns {
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
		for _, tg := range objs.Triggers {
			if tg.Table != "" {
				fmt.Fprintf(&b, "-- TODO: CREATE TRIGGER %s ...; -- source table: %s\n", pgIdent(tg.Name), sanitizeSQLCommentText(tg.Table))
			} else {
				fmt.Fprintf(&b, "-- TODO: CREATE TRIGGER %s ...;\n", pgIdent(tg.Name))
			}
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

	if hasUnsupportedColumns {
		b.WriteString("-- Unsupported Columns\n")
		b.WriteString("-- These columns could not be migrated automatically.\n")
		for _, uc := range report.UnsupportedColumns {
			sourceType := sanitizeSQLCommentText(uc.SourceType)
			reason := sanitizeSQLCommentText(uc.Reason)
			fmt.Fprintf(&b, "-- TODO: ALTER TABLE %s.%s ADD COLUMN %s ...;\n", pgIdent("{{schema}}"), pgIdent(uc.Table), pgIdent(uc.Column))
			fmt.Fprintf(&b, "--   Source type: %s\n", sourceType)
			fmt.Fprintf(&b, "--   Reason: %s\n", reason)
		}
		b.WriteByte('\n')
	}

	return b.String()
}

func sanitizeSQLCommentText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
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

func groupSchemaSemanticWarningsByCategory(warnings []SchemaSemanticWarning) map[string][]SchemaSemanticWarning {
	grouped := make(map[string][]SchemaSemanticWarning)
	for _, warning := range warnings {
		grouped[warning.Category] = append(grouped[warning.Category], warning)
	}
	return grouped
}
