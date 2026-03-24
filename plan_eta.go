package main

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// PlanETA is a conservative, copy-phase-only time estimate derived from plan
// introspection. It is not an end-to-end migration ETA.
type PlanETA struct {
	Scope       string `json:"scope,omitempty"`
	Available   bool   `json:"available"`
	LowSeconds  int64  `json:"low_seconds,omitempty"`
	HighSeconds int64  `json:"high_seconds,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
	BasisRows   int64  `json:"basis_rows,omitempty"`
	// BasisWorkers is effective COPY parallelism used for the estimate (e.g. 1 when source_snapshot_mode is single_tx).
	BasisWorkers      int      `json:"basis_workers,omitempty"`
	Assumptions       []string `json:"assumptions,omitempty"`
	UnavailableReason string   `json:"unavailable_reason,omitempty"`
}

const planETAExclusionAssumption = "excludes validation, indexes, foreign keys, hooks, sequences, triggers, and orphan cleanup"

// Throughput bounds per worker (rows/sec). The band is intentionally wide.
const (
	planETAOptimisticRowsPerSecPerWorker  int64 = 4000
	planETAPessimisticRowsPerSecPerWorker int64 = 2000
)

// ensureReportETA fills ETA when absent (e.g. pgferry plan --input on JSON saved before this field
// existed). Fresh runs already set report.ETA in buildPlanReport.
func ensureReportETA(report *PlanReport) {
	if report == nil || report.ETA != nil {
		return
	}
	e := computePlanETA(report)
	report.ETA = &e
}

func computePlanETA(r *PlanReport) PlanETA {
	out := PlanETA{Scope: "copy_only"}
	if r == nil {
		out.UnavailableReason = "report is nil"
		return out
	}
	s := r.Summary
	if !planSummaryHasData(s) {
		out.UnavailableReason = "plan summary incomplete"
		return out
	}
	if !s.CopyRiskAnalysis {
		out.UnavailableReason = "copy_risk_analysis is disabled"
		return out
	}
	if s.TotalEstimatedRows < 0 {
		out.UnavailableReason = "estimated row count is invalid"
		return out
	}
	if s.TotalEstimatedRows == 0 {
		out.UnavailableReason = "estimated row count is zero"
		return out
	}

	rows := s.TotalEstimatedRows
	workers := s.Workers
	if workers < 1 {
		workers = 1
	}

	effectiveWorkers := workers
	if strings.EqualFold(strings.TrimSpace(s.SnapshotMode), "single_tx") {
		effectiveWorkers = 1
	}

	denomLow := planETAPessimisticRowsPerSecPerWorker * int64(effectiveWorkers)
	denomHigh := planETAOptimisticRowsPerSecPerWorker * int64(effectiveWorkers)
	if denomLow < 1 {
		denomLow = 1
	}
	if denomHigh < 1 {
		denomHigh = 1
	}

	// Fast scenario = optimistic rate; slow scenario = pessimistic rate.
	lowSeconds := divideCeil64(rows, denomHigh)
	highSeconds := divideCeil64(rows, denomLow)
	if lowSeconds < 1 {
		lowSeconds = 1
	}
	if highSeconds < lowSeconds {
		highSeconds = lowSeconds
	}

	var assumptions []string
	assumptions = append(assumptions, planETAExclusionAssumption)

	nHighFindings := countCopyRiskFindingsBySeverity(r.CopyRiskFindings, "high")
	nPoorFindings := countCopyRiskFindingsByCategory(r.CopyRiskFindings, "poor_range_density")
	nNonChunkFindings := countCopyRiskFindingsByCategory(r.CopyRiskFindings, "non_chunkable_large_table")

	highTables := countDistinctTablesWithSeverity(r.CopyRiskFindings, "high")
	poorTables := countDistinctTablesWithCategory(r.CopyRiskFindings, "poor_range_density")
	nonChunkTables := countDistinctTablesWithCategory(r.CopyRiskFindings, "non_chunkable_large_table")

	hiMul := planETACopyRiskHighBoundMultiplier(nHighFindings, nPoorFindings, nNonChunkFindings)
	highSeconds = scaleHighSecondsByPercentMultiplier(highSeconds, hiMul)
	if highSeconds < lowSeconds {
		highSeconds = lowSeconds
	}

	if strings.EqualFold(strings.TrimSpace(s.SnapshotMode), "single_tx") {
		assumptions = append(assumptions, "single_tx widens the estimate because data copy is sequential")
	}
	if highTables > 0 {
		plural := "s"
		if highTables == 1 {
			plural = ""
		}
		assumptions = append(assumptions, fmt.Sprintf("widened because %d table%s have high-severity copy-risk findings", highTables, plural))
	}
	if poorTables > 0 {
		plural := "s"
		if poorTables == 1 {
			plural = ""
		}
		assumptions = append(assumptions, fmt.Sprintf("widened because %d table%s have poor range density findings", poorTables, plural))
	}
	if nonChunkTables > 0 {
		plural := "s"
		if nonChunkTables == 1 {
			plural = ""
		}
		assumptions = append(assumptions, fmt.Sprintf("widened because %d large non-chunkable table%s", nonChunkTables, plural))
	}

	out.Available = true
	out.LowSeconds = lowSeconds
	out.HighSeconds = highSeconds
	out.Confidence = "low"
	out.BasisRows = rows
	out.BasisWorkers = effectiveWorkers
	out.Assumptions = assumptions
	return out
}

// planETACopyRiskHighBoundMultiplier returns a percent-style multiplier (e.g. 135 = 1.35×) applied to
// the pessimistic duration bound. Each stacked finding category widens the slow bound a step.
func planETACopyRiskHighBoundMultiplier(nHighFindings, nPoorFindings, nNonChunkFindings int) int64 {
	hiMul := int64(100)
	for range min(5, nHighFindings) {
		hiMul = hiMul * 110 / 100
	}
	for range min(8, nPoorFindings) {
		hiMul = hiMul * 104 / 100
	}
	for range min(3, nNonChunkFindings) {
		hiMul = hiMul * 115 / 100
	}
	return hiMul
}

// scaleHighSecondsByPercentMultiplier multiplies highSeconds by hiMul/100 (hiMul is typically ~100–400).
// Guards int64 overflow on the multiply step.
func scaleHighSecondsByPercentMultiplier(highSeconds, hiMul int64) int64 {
	if hiMul <= 0 {
		return highSeconds
	}
	if hiMul > 0 && highSeconds > math.MaxInt64/hiMul {
		return math.MaxInt64
	}
	return highSeconds * hiMul / 100
}

func divideCeil64(a, b int64) int64 {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}

func countCopyRiskFindingsBySeverity(findings []PlanCopyRiskFinding, sev string) int {
	n := 0
	for _, f := range findings {
		if strings.EqualFold(f.Severity, sev) {
			n++
		}
	}
	return n
}

func countCopyRiskFindingsByCategory(findings []PlanCopyRiskFinding, cat string) int {
	n := 0
	for _, f := range findings {
		if strings.EqualFold(f.Category, cat) {
			n++
		}
	}
	return n
}

func countDistinctTablesWithSeverity(findings []PlanCopyRiskFinding, sev string) int {
	seen := make(map[string]struct{})
	for _, f := range findings {
		if strings.EqualFold(f.Severity, sev) {
			seen[f.Table] = struct{}{}
		}
	}
	return len(seen)
}

func countDistinctTablesWithCategory(findings []PlanCopyRiskFinding, cat string) int {
	seen := make(map[string]struct{})
	for _, f := range findings {
		if strings.EqualFold(f.Category, cat) {
			seen[f.Table] = struct{}{}
		}
	}
	return len(seen)
}

func formatPlanETADurationWindow(lowSec, highSec int64) string {
	return formatPlanETASeconds(lowSec) + "-" + formatPlanETASeconds(highSec)
}

func formatPlanETASeconds(sec int64) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%dm", sec/60)
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh%dm", h, m)
}

func writePlanETAText(w io.Writer, eta *PlanETA) {
	if eta == nil {
		return
	}
	fmt.Fprintf(w, "### ETA (copy phase only)\n\n")
	if !eta.Available {
		if eta.UnavailableReason != "" {
			fmt.Fprintf(w, "ETA unavailable (%s)\n\n", eta.UnavailableReason)
		} else {
			fmt.Fprintf(w, "ETA unavailable\n\n")
		}
		return
	}
	fmt.Fprintf(w, "  estimated_copy_window: %s\n", formatPlanETADurationWindow(eta.LowSeconds, eta.HighSeconds))
	fmt.Fprintf(w, "  confidence: %s\n", eta.Confidence)
	fmt.Fprintf(w, "  basis: %s estimated rows across %d effective worker(s)\n", formatInt64Thousands(eta.BasisRows), eta.BasisWorkers)
	fmt.Fprintf(w, "  notes:\n")
	for _, a := range eta.Assumptions {
		fmt.Fprintf(w, "    - %s\n", a)
	}
	fmt.Fprintln(w)
}

func writePlanETAMarkdown(w io.Writer, eta *PlanETA) {
	if eta == nil {
		return
	}
	fmt.Fprintln(w, "## ETA (copy phase only)")
	fmt.Fprintln(w)
	if !eta.Available {
		if eta.UnavailableReason != "" {
			fmt.Fprintf(w, "ETA unavailable (%s)\n\n", markdownEscape(eta.UnavailableReason))
		} else {
			fmt.Fprintln(w, "ETA unavailable")
			fmt.Fprintln(w)
		}
		return
	}
	tableRows := [][]string{
		{"Estimated copy window", markdownCode(formatPlanETADurationWindow(eta.LowSeconds, eta.HighSeconds))},
		{"Confidence", markdownEscape(eta.Confidence)},
		{"Basis", fmt.Sprintf("%s estimated rows across %d effective worker(s)", formatInt64Thousands(eta.BasisRows), eta.BasisWorkers)},
	}
	writeMarkdownTable(w, []string{"Field", "Value"}, tableRows)
	fmt.Fprintln(w, "### Notes")
	fmt.Fprintln(w)
	for _, a := range eta.Assumptions {
		fmt.Fprintf(w, "- %s\n", markdownEscape(a))
	}
	fmt.Fprintln(w)
}
