package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestAnalyzeCopyRiskTable_LargeNonChunkable(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "audit_log",
		PGName:     "audit_log",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "varchar", ColumnType: "varchar(36)"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	findings := analyzeCopyRiskTable(table, src, 2_000_000, 100000, nil, 0, 0, false)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}

	got := findings[0]
	if got.Category != "non_chunkable_large_table" {
		t.Fatalf("category = %q, want non_chunkable_large_table", got.Category)
	}
	if got.Chunkable {
		t.Fatalf("chunkable = true, want false")
	}
	if !strings.Contains(got.Reason, "non-chunkable type") {
		t.Fatalf("reason = %q, want non-chunkable type detail", got.Reason)
	}
}

func TestAnalyzeCopyRiskTable_HighChunkCount(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "events",
		PGName:     "events",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int", ColumnType: "int"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
	key := &ChunkKey{SourceColumn: "id", PGColumn: "id"}

	findings := analyzeCopyRiskTable(table, src, 20_000_000, 100000, key, 1, 20_000_000, true)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}

	got := findings[0]
	if got.Category != "high_chunk_count" {
		t.Fatalf("category = %q, want high_chunk_count", got.Category)
	}
	if got.EstimatedChunkCount != 200 {
		t.Fatalf("estimated chunk count = %d, want 200", got.EstimatedChunkCount)
	}
	if got.MinPK == nil || *got.MinPK != 1 || got.MaxPK == nil || *got.MaxPK != 20_000_000 {
		t.Fatalf("range = %v..%v, want 1..20000000", got.MinPK, got.MaxPK)
	}
}

func TestAnalyzeCopyRiskTable_PoorRangeDensity(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "sessions",
		PGName:     "sessions",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int", ColumnType: "int"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
	key := &ChunkKey{SourceColumn: "id", PGColumn: "id"}

	findings := analyzeCopyRiskTable(table, src, 1_000, 10000, key, 1, 1_000_000, true)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}

	got := findings[0]
	if got.Category != "poor_range_density" {
		t.Fatalf("category = %q, want poor_range_density", got.Category)
	}
	if got.RangeDensity >= copyRiskPoorDensityThreshold {
		t.Fatalf("range density = %f, want below %f", got.RangeDensity, copyRiskPoorDensityThreshold)
	}
}

func TestAnalyzeCopyRiskTable_DenseChunkableTableHasNoWarning(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "orders",
		PGName:     "orders",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int", ColumnType: "int"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
	key := &ChunkKey{SourceColumn: "id", PGColumn: "id"}

	findings := analyzeCopyRiskTable(table, src, 1_000, 100000, key, 1, 1_000, true)
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %+v", len(findings), findings)
	}
}

func TestAnalyzeCopyRiskTable_SuspiciousBigintChunkKey(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "ledger",
		PGName:     "ledger",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "bigint"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
	key := &ChunkKey{SourceColumn: "id", PGColumn: "id"}

	findings := analyzeCopyRiskTable(table, src, 8_000_000, 100000, key, 1, 8_000_000, true)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].Category != "suspicious_chunk_key_type" {
		t.Fatalf("category = %q, want suspicious_chunk_key_type", findings[0].Category)
	}
}

func TestSortCopyRiskFindings_DeterministicSeverityAndNameOrder(t *testing.T) {
	findings := []PlanCopyRiskFinding{
		{Category: "poor_range_density", Severity: "medium", Table: "zeta"},
		{Category: "high_chunk_count", Severity: "high", Table: "beta"},
		{Category: "non_chunkable_large_table", Severity: "high", Table: "alpha"},
	}

	sortCopyRiskFindings(findings)

	got := []string{findings[0].Table + ":" + findings[0].Category, findings[1].Table + ":" + findings[1].Category, findings[2].Table + ":" + findings[2].Category}
	want := []string{"alpha:non_chunkable_large_table", "beta:high_chunk_count", "zeta:poor_range_density"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted findings = %v, want %v", got, want)
		}
	}
}

func TestLogCopyRiskFindings(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logCopyRiskFindings([]PlanCopyRiskFinding{
		{
			Category:            "high_chunk_count",
			Severity:            "high",
			Table:               "events",
			Chunkable:           true,
			ChunkKey:            "id",
			Reason:              "The current chunk plan is estimated to create 200 chunks across 20000000 rows for PK range 1..20000000.",
			EstimatedRows:       20_000_000,
			MinPK:               int64Ptr(1),
			MaxPK:               int64Ptr(20_000_000),
			EstimatedChunkCount: 200,
			Recommendation:      "Benchmark this table separately.",
		},
	}, 100000)

	out := buf.String()
	for _, want := range []string{
		"copy risk report: 1 finding(s) across 1 table(s)",
		"chunk_size=100000 is key-range width",
		"events [HIGH]",
		"estimated_chunks=200",
		"recommendation=Benchmark this table separately.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q:\n%s", want, out)
		}
	}
}
