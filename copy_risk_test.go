package main

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"path/filepath"
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

	findings := analyzeCopyRiskTable(table, src, 2_000_000, 100000, nil, 0, 0)
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

func TestNonChunkableTableReason_PKColumnMissing(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "audit_log",
		PGName:     "audit_log",
		Columns: []Column{
			{SourceName: "payload", PGName: "payload", DataType: "text", ColumnType: "text"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	got := nonChunkableTableReason(table, src)
	if !strings.Contains(got, `Primary key column id was not found`) {
		t.Fatalf("reason = %q, want missing PK column detail", got)
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

	findings := analyzeCopyRiskTable(table, src, 20_000_000, 100000, key, 1, 20_000_000)
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

	findings := analyzeCopyRiskTable(table, src, 1_000, 10000, key, 1, 1_000_000)
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

func TestAnalyzeCopyRiskTable_PoorRangeDensity_NegativePKRange(t *testing.T) {
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

	findings := analyzeCopyRiskTable(table, src, 100, 1000, key, -50_000, 50_000)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
	if findings[0].EstimatedChunkCount != 101 {
		t.Fatalf("estimated chunk count = %d, want 101", findings[0].EstimatedChunkCount)
	}
	if findings[0].RangeDensity <= 0 || findings[0].RangeDensity >= 0.01 {
		t.Fatalf("range density = %f, want small positive density", findings[0].RangeDensity)
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

	findings := analyzeCopyRiskTable(table, src, 1_000, 100000, key, 1, 1_000)
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %+v", len(findings), findings)
	}
}

func TestAnalyzeCopyRiskTable_BigintPoorDensityAddsRecommendation(t *testing.T) {
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

	findings := analyzeCopyRiskTable(table, src, 8_000_000, 100000, key, 1, 100_000_000)
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2", len(findings))
	}

	var sawPoorDensity bool
	for _, finding := range findings {
		if finding.Category == "poor_range_density" {
			sawPoorDensity = true
			if !strings.Contains(finding.Recommendation, "bigint key") {
				t.Fatalf("recommendation = %q, want bigint-specific guidance", finding.Recommendation)
			}
		}
	}
	if !sawPoorDensity {
		t.Fatalf("missing poor_range_density in %+v", findings)
	}
}

func TestAnalyzeCopyRiskTable_DenseBigintTableDoesNotAddWarning(t *testing.T) {
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

	findings := analyzeCopyRiskTable(table, src, 8_000_000, 100000, key, 1, 8_000_000)
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0: %+v", len(findings), findings)
	}
}

func TestCollectCopyRiskFindings_SQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "copy-risk.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE sparse_events (id INTEGER PRIMARY KEY, payload TEXT)`,
		`INSERT INTO sparse_events (id, payload) VALUES (1, 'a'), (1000000, 'b')`,
		`CREATE TABLE small_uuid_table (id TEXT PRIMARY KEY, payload TEXT)`,
		`INSERT INTO small_uuid_table (id, payload) VALUES ('a', 'x'), ('b', 'y')`,
		`CREATE TABLE empty_events (id INTEGER PRIMARY KEY, payload TEXT)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "sparse_events",
				PGName:     "sparse_events",
				Columns: []Column{
					{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "BIGINT"},
				},
				PrimaryKey: &Index{Columns: []string{"id"}},
			},
			{
				SourceName: "small_uuid_table",
				PGName:     "small_uuid_table",
				Columns: []Column{
					{SourceName: "id", PGName: "id", DataType: "text", ColumnType: "TEXT"},
				},
				PrimaryKey: &Index{Columns: []string{"id"}},
			},
			{
				SourceName: "empty_events",
				PGName:     "empty_events",
				Columns: []Column{
					{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "BIGINT"},
				},
				PrimaryKey: &Index{Columns: []string{"id"}},
			},
		},
	}

	findings, err := collectCopyRiskFindings(context.Background(), db, &sqliteSourceDB{}, schema, 10000)
	if err != nil {
		t.Fatalf("collectCopyRiskFindings() error: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %+v", len(findings), findings)
	}
	if findings[0].Table != "sparse_events" || findings[0].Category != "poor_range_density" {
		t.Fatalf("first finding = %+v, want sparse_events poor_range_density", findings[0])
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

func TestEstimateChunkCount_GuardsInvalidRange(t *testing.T) {
	if got := estimateChunkCount(10, 1, 100); got != 0 {
		t.Fatalf("estimateChunkCount(10, 1, 100) = %d, want 0", got)
	}
}

func TestBuildSourceCountQuery_ReusedByCopyRisk(t *testing.T) {
	src := &sqliteSourceDB{}
	table := Table{SourceName: "events"}
	if got := buildSourceCountQuery(src, table); got != `SELECT COUNT(*) FROM "events"` {
		t.Fatalf("buildSourceCountQuery() = %q", got)
	}
}

func TestBuildPlanTableChunkInfo_Chunkable(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "orders",
		PGName:     "orders",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "bigint", ColumnType: "bigint"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}
	key := &ChunkKey{SourceColumn: "id", PGColumn: "id"}

	got := buildPlanTableChunkInfo(table, src, 1_000_000, 100_000, key, 1, 1_000_000)
	if !got.Chunkable || got.ChunkKey != "id" || got.ChunkKeyType != "bigint" {
		t.Fatalf("chunk info = %+v", got)
	}
	if got.EstimatedChunks != 10 {
		t.Fatalf("estimated chunks = %d, want 10", got.EstimatedChunks)
	}
	if got.MinPK == nil || *got.MinPK != 1 || got.MaxPK == nil || *got.MaxPK != 1_000_000 {
		t.Fatalf("range = %v..%v", got.MinPK, got.MaxPK)
	}
}

func TestBuildPlanTableChunkInfo_NonChunkable(t *testing.T) {
	src := &mysqlSourceDB{}
	table := Table{
		SourceName: "sessions",
		PGName:     "sessions",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "varchar", ColumnType: "varchar(36)"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	got := buildPlanTableChunkInfo(table, src, 456, 100_000, nil, 0, 0)
	if got.Chunkable || got.FullTableCopyReason == "" {
		t.Fatalf("chunk info = %+v", got)
	}
	if got.EstimatedRows != 456 {
		t.Fatalf("rows = %d", got.EstimatedRows)
	}
}
