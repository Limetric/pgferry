package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestComputePlanETA_CopyRiskDisabled(t *testing.T) {
	r := &PlanReport{
		Summary: PlanSummary{
			SourceType:       "mysql",
			CopyRiskAnalysis: false,
			Workers:          4,
		},
	}
	e := computePlanETA(r)
	if e.Available {
		t.Fatalf("Available = true, want false")
	}
	if !strings.Contains(e.UnavailableReason, "copy_risk_analysis") {
		t.Fatalf("reason = %q", e.UnavailableReason)
	}
	if e.Scope != "copy_only" {
		t.Fatalf("Scope = %q", e.Scope)
	}
}

func TestComputePlanETA_ZeroRows(t *testing.T) {
	r := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: 0,
			Workers:            8,
		},
	}
	e := computePlanETA(r)
	if e.Available {
		t.Fatal("expected unavailable")
	}
	if !strings.Contains(e.UnavailableReason, "zero") {
		t.Fatalf("reason = %q", e.UnavailableReason)
	}
}

func TestComputePlanETA_ParallelBaseline(t *testing.T) {
	const rows int64 = 48_200_000
	r := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: rows,
			Workers:            8,
			SnapshotMode:       "none",
		},
	}
	e := computePlanETA(r)
	if !e.Available {
		t.Fatalf("Available = false: %s", e.UnavailableReason)
	}
	if e.LowSeconds != divideCeil64(rows, planETAOptimisticRowsPerSecPerWorker*8) {
		t.Fatalf("LowSeconds = %d", e.LowSeconds)
	}
	if e.HighSeconds != divideCeil64(rows, planETAPessimisticRowsPerSecPerWorker*8) {
		t.Fatalf("HighSeconds = %d", e.HighSeconds)
	}
	if e.HighSeconds < e.LowSeconds {
		t.Fatalf("range invalid: %d-%d", e.LowSeconds, e.HighSeconds)
	}
	if e.BasisWorkers != 8 || e.BasisRows != rows {
		t.Fatalf("basis=%d rows=%d", e.BasisWorkers, e.BasisRows)
	}
	if e.Confidence != "low" {
		t.Fatalf("confidence = %q", e.Confidence)
	}
	foundExcl := false
	for _, a := range e.Assumptions {
		if strings.Contains(a, "excludes validation") {
			foundExcl = true
		}
	}
	if !foundExcl {
		t.Fatalf("assumptions: %v", e.Assumptions)
	}
}

func TestComputePlanETA_SingleTXWidensVersusParallel(t *testing.T) {
	const rows int64 = 10_000_000
	parallel := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: rows,
			Workers:            8,
			SnapshotMode:       "none",
		},
	}
	single := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: rows,
			Workers:            8,
			SnapshotMode:       "single_tx",
		},
	}
	ePar := computePlanETA(parallel)
	eSingle := computePlanETA(single)
	if ePar.LowSeconds >= eSingle.LowSeconds {
		t.Fatalf("parallel low %d should be < single_tx low %d", ePar.LowSeconds, eSingle.LowSeconds)
	}
	if ePar.HighSeconds >= eSingle.HighSeconds {
		t.Fatalf("parallel high %d should be < single_tx high %d", ePar.HighSeconds, eSingle.HighSeconds)
	}
	if !strings.Contains(strings.Join(eSingle.Assumptions, " "), "single_tx") {
		t.Fatalf("assumptions: %v", eSingle.Assumptions)
	}
}

func TestComputePlanETA_HighRiskFindingsWiden(t *testing.T) {
	base := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: 5_000_000,
			Workers:            4,
			SnapshotMode:       "none",
		},
	}
	e0 := computePlanETA(base)
	risk := &PlanReport{
		Summary: base.Summary,
		CopyRiskFindings: []PlanCopyRiskFinding{
			{Table: "t1", Severity: "high", Category: "high_chunk_count"},
			{Table: "t2", Severity: "high", Category: "high_chunk_count"},
		},
	}
	e1 := computePlanETA(risk)
	if e1.HighSeconds <= e0.HighSeconds {
		t.Fatalf("expected wider high bound: %d vs %d", e0.HighSeconds, e1.HighSeconds)
	}
}

func TestComputePlanETA_JSONRoundTrip(t *testing.T) {
	r := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: 1_000_000,
			Workers:            2,
			SnapshotMode:       "none",
		},
	}
	e := computePlanETA(r)
	r.ETA = &e
	var buf bytes.Buffer
	if err := writePlanJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	var decoded PlanReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ETA == nil || !decoded.ETA.Available {
		t.Fatal("eta missing after decode")
	}
	if decoded.ETA.LowSeconds != e.LowSeconds || decoded.ETA.HighSeconds != e.HighSeconds {
		t.Fatalf("seconds mismatch %v vs %v", decoded.ETA, e)
	}
}

func TestComputePlanETA_InputBackfillMissingEta(t *testing.T) {
	r := &PlanReport{
		Summary: PlanSummary{
			SourceType:         "mysql",
			CopyRiskAnalysis:   true,
			TotalEstimatedRows: 2_000_000,
			Workers:            4,
			SnapshotMode:       "none",
		},
		ETA: nil,
	}
	ensureReportETA(r)
	if r.ETA == nil || !r.ETA.Available {
		t.Fatal("expected backfilled ETA")
	}
}

func TestFormatPlanETADurationWindow(t *testing.T) {
	if got := formatPlanETADurationWindow(1500, 3000); got != "25m-50m" {
		t.Fatalf("got %q", got)
	}
	if got := formatPlanETASeconds(45); got != "45s" {
		t.Fatalf("got %q", got)
	}
}

func TestWritePlanETAText_Unavailable(t *testing.T) {
	e := &PlanETA{
		Scope:             "copy_only",
		Available:         false,
		UnavailableReason: "copy_risk_analysis is disabled",
	}
	var buf bytes.Buffer
	writePlanETAText(&buf, e)
	if !strings.Contains(buf.String(), "ETA unavailable") {
		t.Fatalf("got %q", buf.String())
	}
}
