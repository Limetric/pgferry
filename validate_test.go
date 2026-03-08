package main

import (
	"context"
	"testing"
)

func TestValidateMigration_NoneMode(t *testing.T) {
	results, err := validateMigration(context.Background(), nil, "", nil, nil, "", "none", 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for mode=none, got %v", results)
	}
}

func TestValidateMigration_EmptyMode(t *testing.T) {
	results, err := validateMigration(context.Background(), nil, "", nil, nil, "", "", 4)
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

// stubSourceDB is a minimal SourceDB stub for unit tests that only need MaxWorkers.
type stubSourceDB struct {
	maxWorkers int
	mysqlSourceDB
}

func (s *stubSourceDB) MaxWorkers() int { return s.maxWorkers }
