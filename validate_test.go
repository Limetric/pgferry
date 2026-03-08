package main

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

// mockQuerier implements dbQuerier for testing.
type mockQuerier struct {
	rows map[string]int64 // query → count
}

func (m *mockQuerier) QueryContext(_ context.Context, query string, _ ...any) (*sql.Rows, error) {
	return nil, fmt.Errorf("mockQuerier.QueryContext not implemented for: %s", query)
}

func TestValidateMigration_NoneMode(t *testing.T) {
	results, err := validateMigration(context.Background(), nil, "", nil, nil, "", "none")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for mode=none, got %v", results)
	}
}

func TestValidateMigration_EmptyMode(t *testing.T) {
	results, err := validateMigration(context.Background(), nil, "", nil, nil, "", "")
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
