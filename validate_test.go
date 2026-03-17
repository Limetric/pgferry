package main

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestValidateMigration_NoneMode(t *testing.T) {
	results, err := validateMigration(context.Background(), nil, "", nil, nil, "", "none", 4, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for mode=none, got %v", results)
	}
}

func TestValidateMigration_EmptyMode(t *testing.T) {
	results, err := validateMigration(context.Background(), nil, "", nil, nil, "", "", 4, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for empty mode, got %v", results)
	}
}

func TestValidationResult_MatchLogic(t *testing.T) {
	tests := []struct {
		source, target int64
		wantMatch      bool
	}{
		{100, 100, true},
		{100, 99, false},
		{0, 0, true},
		{100, 0, false},
	}
	for _, tt := range tests {
		r := ValidationResult{
			Table:       "test",
			SourceCount: tt.source,
			TargetCount: tt.target,
			CountMatch:  tt.source == tt.target,
		}
		if r.CountMatch != tt.wantMatch {
			t.Errorf("source=%d target=%d: CountMatch=%t, want %t",
				tt.source, tt.target, r.CountMatch, tt.wantMatch)
		}
	}
}

func TestValidationWorkers(t *testing.T) {
	tests := []struct {
		name       string
		workers    int
		maxWorkers int // 0 = no cap (like MySQL)
		want       int
	}{
		{"mysql no cap", 8, 0, 8},
		{"sqlite capped to 1", 8, 1, 1},
		{"workers already at max", 1, 1, 1},
		{"zero workers defaults to 1", 0, 0, 1},
		{"negative workers defaults to 1", -1, 0, 1},
		{"source cap lower than workers", 4, 2, 2},
		{"source cap higher than workers", 2, 4, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := &stubSourceDB{maxWorkers: tt.maxWorkers}
			got := validationWorkers(tt.workers, src)
			if got != tt.want {
				t.Errorf("validationWorkers(%d, maxWorkers=%d) = %d, want %d",
					tt.workers, tt.maxWorkers, got, tt.want)
			}
		})
	}
}

func TestBuildSourceCountQuery_MSSQLWithSourceSchema(t *testing.T) {
	src := &mssqlSourceDB{sourceSchema: "sales"}
	table := Table{SourceName: "orders"}

	got := buildSourceCountQuery(src, table)
	want := "SELECT COUNT(*) FROM [sales].[orders]"
	if got != want {
		t.Fatalf("buildSourceCountQuery() = %q, want %q", got, want)
	}
}

func TestValidationSampleOffsets_Deterministic(t *testing.T) {
	got1 := validationSampleOffsets(1000, 16)
	got2 := validationSampleOffsets(1000, 16)
	if len(got1) != len(got2) {
		t.Fatalf("offset lengths differ: %d vs %d", len(got1), len(got2))
	}
	for i := range got1 {
		if got1[i] != got2[i] {
			t.Fatalf("offset[%d] = %d, want %d", i, got1[i], got2[i])
		}
	}
	if got1[0] != 0 {
		t.Fatalf("first offset = %d, want 0", got1[0])
	}
	if got1[len(got1)-1] != 999 {
		t.Fatalf("last offset = %d, want 999", got1[len(got1)-1])
	}
}

func TestValidateSampledHashTable_DetectsContentMismatchWithMatchingCounts(t *testing.T) {
	table := Table{
		SourceName: "users",
		PGName:     "users",
		Columns: []Column{
			{SourceName: "id", PGName: "id"},
			{SourceName: "name", PGName: "name"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	keyCols := []validationColumn{{Column: table.Columns[0], Kind: validationKindNumericText, PGType: "integer"}}
	compareCols := []validationColumn{
		{Column: table.Columns[0], Kind: validationKindNumericText, PGType: "integer"},
		{Column: table.Columns[1], Kind: validationKindText, PGType: "text"},
	}

	backend := &sampledHashBackendStub{
		sourceCountValue: 3,
		targetCountValue: 3,
		keys: []validationKey{
			{DisplayFragments: []string{"1"}},
		},
		sourceFragmentsValue: []string{"\"1\"", "\"alice\""},
		targetFragmentsValue: []string{"\"1\"", "\"bob\""},
	}

	got, err := validateSampledHashTableWithColumns(context.Background(), backend, table, keyCols, compareCols)
	if err != nil {
		t.Fatalf("validateSampledHashTableWithColumns() error: %v", err)
	}
	if !got.CountMatch {
		t.Fatal("CountMatch = false, want true")
	}
	if got.SampleMismatch == "" {
		t.Fatal("SampleMismatch = empty, want mismatch detail")
	}
	if !strings.Contains(got.SampleMismatch, "column name") {
		t.Fatalf("SampleMismatch = %q, want column detail", got.SampleMismatch)
	}
}

func TestValidateSampledHashTableWithColumns_MatchDoesNotDoubleCount(t *testing.T) {
	table := Table{
		SourceName: "users",
		PGName:     "users",
		Columns: []Column{
			{SourceName: "id", PGName: "id"},
			{SourceName: "name", PGName: "name"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	keyCols := []validationColumn{{Column: table.Columns[0], Kind: validationKindNumericText, PGType: "integer"}}
	compareCols := []validationColumn{
		{Column: table.Columns[0], Kind: validationKindNumericText, PGType: "integer"},
		{Column: table.Columns[1], Kind: validationKindText, PGType: "text"},
	}

	backend := &sampledHashBackendStub{
		sourceCountValue: 2,
		targetCountValue: 2,
		keys: []validationKey{
			{DisplayFragments: []string{"1"}},
		},
		sourceFragmentsValue: []string{"\"1\"", "\"alice\""},
		targetFragmentsValue: []string{"\"1\"", "\"alice\""},
	}

	got, err := validateSampledHashTableWithColumns(context.Background(), backend, table, keyCols, compareCols)
	if err != nil {
		t.Fatalf("validateSampledHashTableWithColumns() error: %v", err)
	}
	if !got.SampleMatch || got.SampleMismatch != "" {
		t.Fatalf("got mismatch result: %+v", got)
	}
	if backend.sourceCountCalls != 1 || backend.targetCountCalls != 1 {
		t.Fatalf("count calls = source:%d target:%d, want 1 each", backend.sourceCountCalls, backend.targetCountCalls)
	}
}

func TestValidationPlanLogs_Caveats(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	logValidationPlan(validationModeSampledHash, "single_tx")

	out := buf.String()
	for _, want := range []string{
		"validation mode: sampled_hash",
		"after_data hooks",
		"re-reads the source after COPY",
		"single_tx applies only to the COPY phase",
		"samples deterministic rows",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("log output missing %q:\n%s", want, out)
		}
	}
}

// stubSourceDB is a minimal SourceDB stub for unit tests.
// Embeds the interface so only the methods under test need implementing;
// calling any other method panics, signalling unintended use.
type stubSourceDB struct {
	SourceDB   // interface embedding — panics on unimplemented methods
	maxWorkers int
}

func (s *stubSourceDB) MaxWorkers() int { return s.maxWorkers }

type sampledHashBackendStub struct {
	sourceCountValue     int64
	targetCountValue     int64
	keys                 []validationKey
	sourceFragmentsValue []string
	targetFragmentsValue []string
	sourceCountCalls     int
	targetCountCalls     int
}

func (s *sampledHashBackendStub) sourceCount(context.Context, Table) (int64, error) {
	s.sourceCountCalls++
	return s.sourceCountValue, nil
}

func (s *sampledHashBackendStub) targetCount(context.Context, Table) (int64, error) {
	s.targetCountCalls++
	return s.targetCountValue, nil
}

func (s *sampledHashBackendStub) sampleKeys(context.Context, Table, []validationColumn, []int64) ([]validationKey, error) {
	return s.keys, nil
}

func (s *sampledHashBackendStub) sourceFragments(context.Context, Table, []validationColumn, validationKey) ([]string, error) {
	return s.sourceFragmentsValue, nil
}

func (s *sampledHashBackendStub) targetFragments(context.Context, Table, []validationColumn, []validationColumn, validationKey) ([]string, error) {
	return s.targetFragmentsValue, nil
}
