package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sort"
	"strings"
)

const (
	copyRiskLargeNonChunkableRows   int64   = 1_000_000
	copyRiskHighChunkCountThreshold         = 128
	copyRiskPoorDensityThreshold    float64 = 0.10
	copyRiskPoorDensityMinChunks            = 16
	copyRiskBigintRowsThreshold     int64   = 5_000_000
	copyRiskBigintChunkThreshold            = 64
)

// PlanCopyRiskFinding describes a table whose runtime COPY behavior is likely
// to be operationally risky even though migration remains supported.
type PlanCopyRiskFinding struct {
	Category            string  `json:"category"`
	Severity            string  `json:"severity"`
	Table               string  `json:"table"`
	Chunkable           bool    `json:"chunkable"`
	ChunkKey            string  `json:"chunk_key,omitempty"`
	ChunkKeyType        string  `json:"chunk_key_type,omitempty"`
	Reason              string  `json:"reason"`
	EstimatedRows       int64   `json:"estimated_rows"`
	MinPK               *int64  `json:"min_pk,omitempty"`
	MaxPK               *int64  `json:"max_pk,omitempty"`
	EstimatedChunkCount int     `json:"estimated_chunk_count,omitempty"`
	RangeDensity        float64 `json:"range_density,omitempty"`
	Recommendation      string  `json:"recommendation"`
}

func queryExactRowCount(ctx context.Context, source dbQuerier, src SourceDB, table Table) (int64, error) {
	rows, err := source.QueryContext(ctx, buildSourceCountQuery(src, table))
	if err != nil {
		return 0, fmt.Errorf("query row count for %s: %w", table.SourceName, err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, fmt.Errorf("query row count for %s: %w", table.SourceName, err)
		}
		return 0, nil
	}

	var count sql.NullInt64
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan row count for %s: %w", table.SourceName, err)
	}
	if !count.Valid {
		return 0, nil
	}
	return count.Int64, nil
}

func collectCopyRiskFindings(ctx context.Context, source dbQuerier, src SourceDB, schema *Schema, chunkSize int64) ([]PlanCopyRiskFinding, error) {
	if schema == nil {
		return []PlanCopyRiskFinding{}, nil
	}

	findings := make([]PlanCopyRiskFinding, 0, len(schema.Tables))
	for _, table := range schema.Tables {
		rowCount, err := queryExactRowCount(ctx, source, src, table)
		if err != nil {
			return nil, err
		}
		if rowCount == 0 {
			continue
		}

		key := chunkKeyForTable(table, src)
		if key == nil {
			findings = append(findings, analyzeCopyRiskTable(table, src, rowCount, chunkSize, nil, 0, 0)...)
			continue
		}

		min, max, hasRows, err := queryMinMax(ctx, source, src, table, *key)
		if err != nil {
			return nil, err
		}
		if !hasRows {
			continue
		}
		findings = append(findings, analyzeCopyRiskTable(table, src, rowCount, chunkSize, key, min, max)...)
	}

	sortCopyRiskFindings(findings)
	return findings, nil
}

func analyzeCopyRiskTable(table Table, src SourceDB, rowCount int64, chunkSize int64, key *ChunkKey, minPK, maxPK int64) []PlanCopyRiskFinding {
	if rowCount <= 0 {
		return nil
	}

	chunkSize = normalizedChunkSize(chunkSize)
	findings := make([]PlanCopyRiskFinding, 0, 3)

	if key == nil {
		if rowCount >= copyRiskLargeNonChunkableRows {
			findings = append(findings, PlanCopyRiskFinding{
				Category:       "non_chunkable_large_table",
				Severity:       "high",
				Table:          table.PGName,
				Chunkable:      false,
				Reason:         nonChunkableTableReason(table, src),
				EstimatedRows:  rowCount,
				Recommendation: "This table will copy as one unit. Plan for coarse resume granularity, or migrate it in a dedicated window so it does not dominate restart cost.",
			})
		}
		return findings
	}

	chunkKeyType := chunkKeyDataType(table, key)
	chunkCount := estimateChunkCount(minPK, maxPK, chunkSize)
	rangeWidth := keyRangeWidth(minPK, maxPK)
	density := 1.0
	if rangeWidth > 0 {
		density = float64(rowCount) / float64(rangeWidth)
	}

	if chunkCount >= copyRiskHighChunkCountThreshold {
		findings = append(findings, PlanCopyRiskFinding{
			Category:            "high_chunk_count",
			Severity:            "high",
			Table:               table.PGName,
			Chunkable:           true,
			ChunkKey:            key.PGColumn,
			ChunkKeyType:        chunkKeyType,
			Reason:              fmt.Sprintf("The current chunk plan is estimated to create %d chunks across %d rows for PK range %d..%d.", chunkCount, rowCount, minPK, maxPK),
			EstimatedRows:       rowCount,
			MinPK:               int64Ptr(minPK),
			MaxPK:               int64Ptr(maxPK),
			EstimatedChunkCount: chunkCount,
			RangeDensity:        density,
			Recommendation:      "Expect many chunk checkpoints and restarts for this table. Test with production-like data, increase chunk_size carefully, or migrate it in a separate run.",
		})
	}

	if chunkCount >= copyRiskPoorDensityMinChunks && density < copyRiskPoorDensityThreshold {
		findings = append(findings, PlanCopyRiskFinding{
			Category:            "poor_range_density",
			Severity:            "medium",
			Table:               table.PGName,
			Chunkable:           true,
			ChunkKey:            key.PGColumn,
			ChunkKeyType:        chunkKeyType,
			Reason:              fmt.Sprintf("The chunk key range %d..%d spans %d possible values for %d rows (%.2f%% density), so many chunks may be mostly empty.", minPK, maxPK, rangeWidth, rowCount, density*100),
			EstimatedRows:       rowCount,
			MinPK:               int64Ptr(minPK),
			MaxPK:               int64Ptr(maxPK),
			EstimatedChunkCount: chunkCount,
			RangeDensity:        density,
			Recommendation:      "chunk_size controls key-range width, not rows per chunk. Validate throughput on production-like data, or isolate this table so sparse ranges do not surprise cutover timing.",
		})
	}

	if strings.EqualFold(chunkKeyType, "bigint") && rowCount >= copyRiskBigintRowsThreshold && chunkCount >= copyRiskBigintChunkThreshold && density < copyRiskPoorDensityThreshold {
		findings = append(findings, PlanCopyRiskFinding{
			Category:            "suspicious_chunk_key_type",
			Severity:            "medium",
			Table:               table.PGName,
			Chunkable:           true,
			ChunkKey:            key.PGColumn,
			ChunkKeyType:        chunkKeyType,
			Reason:              fmt.Sprintf("The table is technically chunkable on bigint key %s, but large bigint keyspaces can hide operational cliffs when ids are sparse or heavily deleted.", key.PGColumn),
			EstimatedRows:       rowCount,
			MinPK:               int64Ptr(minPK),
			MaxPK:               int64Ptr(maxPK),
			EstimatedChunkCount: chunkCount,
			RangeDensity:        density,
			Recommendation:      "Review the observed range and density before relying on chunk-level restart behavior. If this table is critical, benchmark it separately with production-like data.",
		})
	}

	return findings
}

func nonChunkableTableReason(table Table, src SourceDB) string {
	if table.PrimaryKey == nil {
		return "No primary key is available, so pgferry will fall back to full-table copy."
	}
	if len(table.PrimaryKey.Columns) != 1 {
		return fmt.Sprintf("The primary key has %d columns, so pgferry cannot range-chunk it.", len(table.PrimaryKey.Columns))
	}

	pkName := table.PrimaryKey.Columns[0]
	for _, col := range table.Columns {
		if col.PGName != pkName {
			continue
		}
		return fmt.Sprintf("Primary key column %s has non-chunkable type %q.", pkName, col.ColumnType)
	}

	return fmt.Sprintf("Primary key column %s was not found in the table definition, so pgferry will fall back to full-table copy.", pkName)
}

func chunkKeyDataType(table Table, key *ChunkKey) string {
	if key == nil {
		return ""
	}
	for _, col := range table.Columns {
		if col.PGName == key.PGColumn {
			if col.ColumnType != "" {
				return col.ColumnType
			}
			return col.DataType
		}
	}
	return ""
}

func normalizedChunkSize(chunkSize int64) int64 {
	if chunkSize <= 0 {
		return 100000
	}
	return chunkSize
}

func estimateChunkCount(minPK, maxPK, chunkSize int64) int {
	chunkSize = normalizedChunkSize(chunkSize)
	if maxPK < minPK {
		return 0
	}
	diff := uint64(maxPK) - uint64(minPK)
	count := diff/uint64(chunkSize) + 1
	maxInt := int(^uint(0) >> 1)
	if count > uint64(maxInt) {
		return maxInt
	}
	return int(count)
}

func keyRangeWidth(minPK, maxPK int64) uint64 {
	diff := uint64(maxPK) - uint64(minPK)
	if diff == ^uint64(0) {
		return ^uint64(0)
	}
	return diff + 1
}

func sortCopyRiskFindings(findings []PlanCopyRiskFinding) {
	sort.Slice(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if copyRiskSeverityRank(left.Severity) != copyRiskSeverityRank(right.Severity) {
			return copyRiskSeverityRank(left.Severity) < copyRiskSeverityRank(right.Severity)
		}
		if left.Table != right.Table {
			return left.Table < right.Table
		}
		return left.Category < right.Category
	})
}

func copyRiskSeverityRank(severity string) int {
	switch severity {
	case "high":
		return 0
	case "medium":
		return 1
	default:
		return 2
	}
}

func logCopyRiskFindings(findings []PlanCopyRiskFinding, chunkSize int64) {
	if len(findings) == 0 {
		return
	}

	tables := make(map[string]struct{}, len(findings))
	for _, finding := range findings {
		tables[finding.Table] = struct{}{}
	}

	log.Printf("copy risk report: %d finding(s) across %d table(s); chunk_size=%d is key-range width, not rows per chunk", len(findings), len(tables), normalizedChunkSize(chunkSize))
	for _, finding := range findings {
		log.Printf("  WARN: %s", formatCopyRiskLogLine(finding))
	}
}

func formatCopyRiskLogLine(finding PlanCopyRiskFinding) string {
	base := fmt.Sprintf("%s [%s] %s", finding.Table, strings.ToUpper(finding.Severity), finding.Reason)
	if !finding.Chunkable {
		return fmt.Sprintf("%s rows=%d recommendation=%s", base, finding.EstimatedRows, finding.Recommendation)
	}

	rangeInfo := ""
	if finding.MinPK != nil && finding.MaxPK != nil {
		rangeInfo = fmt.Sprintf(" range=%d..%d", *finding.MinPK, *finding.MaxPK)
	}
	return fmt.Sprintf("%s rows=%d%s estimated_chunks=%d recommendation=%s", base, finding.EstimatedRows, rangeInfo, finding.EstimatedChunkCount, finding.Recommendation)
}

func copyRiskCategoryTitle(category string) string {
	switch category {
	case "non_chunkable_large_table":
		return "Large Non-Chunkable Table"
	case "high_chunk_count":
		return "High Chunk Count"
	case "poor_range_density":
		return "Poor Range Density"
	case "suspicious_chunk_key_type":
		return "Suspicious Chunk Key Type"
	default:
		return category
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
