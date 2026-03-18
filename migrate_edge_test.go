package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func TestRunParallelMigrationWorkers_EmptyWorkItems(t *testing.T) {
	err := runParallelMigrationWorkers(
		context.Background(),
		4,
		func() (migrationWorkerSource, error) {
			t.Fatal("openSource should not be called for empty work items")
			return nil, nil
		},
		nil,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			t.Fatal("execute should not be called for empty work items")
			return 0, nil
		},
	)
	if err != nil {
		t.Fatalf("expected nil error for empty work items, got: %v", err)
	}
}

func TestRunParallelMigrationWorkers_WorkerCountCappedToWorkItems(t *testing.T) {
	workItems := []migrationWorkItem{
		{Table: Table{SourceName: "only_one"}},
	}

	var mu sync.Mutex
	openCalls := 0
	err := runParallelMigrationWorkers(
		context.Background(),
		100, // far more workers than items
		func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			openCalls++
			return &fakeMigrationWorkerSource{id: openCalls, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			return 1, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only 1 worker should have been created since there's only 1 work item
	if openCalls != 1 {
		t.Fatalf("openSource calls = %d, want 1 (capped to work item count)", openCalls)
	}
}

func TestRunParallelMigrationWorkers_WorkerCountDefaultsToOneWhenZero(t *testing.T) {
	workItems := []migrationWorkItem{
		{Table: Table{SourceName: "a"}},
		{Table: Table{SourceName: "b"}},
	}

	var mu sync.Mutex
	openCalls := 0
	err := runParallelMigrationWorkers(
		context.Background(),
		0, // should default to 1
		func() (migrationWorkerSource, error) {
			mu.Lock()
			defer mu.Unlock()
			openCalls++
			return &fakeMigrationWorkerSource{id: openCalls, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		workItems,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			return 1, nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if openCalls != 1 {
		t.Fatalf("openSource calls = %d, want 1 (workers=0 defaults to 1)", openCalls)
	}
}

func TestRunParallelMigrationWorkers_LazySourceOpenFailure(t *testing.T) {
	workItems := []migrationWorkItem{
		{Table: Table{SourceName: "a"}},
		{Table: Table{SourceName: "b"}},
	}

	err := runParallelMigrationWorkers(
		context.Background(),
		2,
		func() (migrationWorkerSource, error) {
			return nil, errors.New("connection refused")
		},
		workItems,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, _ migrationWorkItem) (int64, error) {
			return 0, errors.New("should not be called")
		},
	)
	if err == nil {
		t.Fatal("expected error when openSource fails")
	}
	if got := err.Error(); !containsSubstring(got, "open source worker") {
		t.Fatalf("error = %q, want substring 'open source worker'", got)
	}
}

func TestRunParallelMigrationWorkers_ContextCancellationDuringEnqueue(t *testing.T) {
	// Create many work items but cancel context externally.
	// Use 2 workers so one can process while the other is blocked,
	// and the first error causes the enqueue loop to break.
	items := make([]migrationWorkItem, 50)
	for i := range items {
		items[i] = migrationWorkItem{Table: Table{SourceName: fmt.Sprintf("t%d", i)}}
	}

	var mu sync.Mutex
	processed := 0
	err := runParallelMigrationWorkers(
		context.Background(),
		2,
		func() (migrationWorkerSource, error) {
			return &fakeMigrationWorkerSource{id: 1, closeMu: &mu, closeCounts: map[int]int{}}, nil
		},
		items,
		&fakeMigrationCheckpointManager{},
		func(ctx context.Context, _ dbQuerier, item migrationWorkItem) (int64, error) {
			mu.Lock()
			processed++
			mu.Unlock()
			// First item fails, which should cancel context and stop enqueue
			if item.Table.SourceName == "t0" {
				return 0, fmt.Errorf("forced failure")
			}
			// Other items wait for cancellation
			<-ctx.Done()
			return 0, ctx.Err()
		},
	)

	if err == nil {
		t.Fatal("expected error after failure")
	}
	if processed >= len(items) {
		t.Fatalf("processed all %d items despite cancellation, want early stop", processed)
	}
}

func TestBuildParallelMigrationWorkItems_AllSkippedReturnsEmpty(t *testing.T) {
	plans := []ChunkPlan{
		{Table: Table{SourceName: "users"}},
		{Table: Table{SourceName: "profiles"}},
	}
	mgr := &fakeMigrationCheckpointManager{
		doneTables: map[string]bool{"users": true, "profiles": true},
	}

	items := buildParallelMigrationWorkItems(plans, mgr)
	if len(items) != 0 {
		t.Fatalf("expected 0 work items when all tables are done, got %d", len(items))
	}
}

func TestFormatMigrationWorkError_FullTable(t *testing.T) {
	item := migrationWorkItem{Table: Table{SourceName: "users"}}
	err := formatMigrationWorkError(item, errors.New("boom"))
	if got := err.Error(); got != "table users: boom" {
		t.Fatalf("error = %q, want 'table users: boom'", got)
	}
}

func TestFormatMigrationWorkError_Chunk(t *testing.T) {
	item := migrationWorkItem{
		Table:    Table{SourceName: "orders"},
		ChunkKey: &ChunkKey{SourceColumn: "id"},
		Chunk:    Chunk{Index: 5},
	}
	err := formatMigrationWorkError(item, errors.New("timeout"))
	if got := err.Error(); got != "table orders chunk 5: timeout" {
		t.Fatalf("error = %q, want 'table orders chunk 5: timeout'", got)
	}
}

func TestRecordMigrationWorkResult_FullTable(t *testing.T) {
	mgr := &fakeMigrationCheckpointManager{}
	item := migrationWorkItem{Table: Table{SourceName: "users"}}
	recordMigrationWorkResult(mgr, item, 1000)
	if len(mgr.recordedFull) != 1 || mgr.recordedFull[0] != "users" {
		t.Fatalf("recorded full = %v, want [users]", mgr.recordedFull)
	}
}

func TestRecordMigrationWorkResult_Chunk(t *testing.T) {
	mgr := &fakeMigrationCheckpointManager{}
	item := migrationWorkItem{
		Table:      Table{SourceName: "orders"},
		ChunkKey:   &ChunkKey{SourceColumn: "id"},
		Chunk:      Chunk{Index: 3},
		ChunkCount: 5,
	}
	recordMigrationWorkResult(mgr, item, 500)
	if len(mgr.recordedChunk) != 1 || mgr.recordedChunk[0] != "orders:3" {
		t.Fatalf("recorded chunk = %v, want [orders:3]", mgr.recordedChunk)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
