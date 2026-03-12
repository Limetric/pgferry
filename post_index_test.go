package main

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestPlanIndexJobs_SkipsUnsupported(t *testing.T) {
	schema := &Schema{Tables: []Table{
		{
			SourceName: "posts",
			PGName:     "posts",
			Indexes: []Index{
				{Name: "idx_title", SourceName: "idx_title", Type: "BTREE", Columns: []string{"title"}},
				{Name: "idx_body_ft", SourceName: "idx_body_ft", Type: "FULLTEXT", Columns: []string{"body"}},
				{Name: "idx_prefix", SourceName: "idx_prefix", Type: "BTREE", Columns: []string{"name"}, HasPrefix: true},
			},
		},
		{
			SourceName: "users",
			PGName:     "users",
			Indexes: []Index{
				{Name: "idx_email", SourceName: "idx_email", Type: "BTREE", Columns: []string{"email"}, Unique: true},
			},
		},
	}}

	jobs, skipped := planIndexJobs(schema, "public", defaultTypeMappingConfig())
	if len(jobs) != 2 {
		t.Fatalf("planIndexJobs() returned %d jobs, want 2", len(jobs))
	}
	if skipped != 2 {
		t.Fatalf("planIndexJobs() skipped=%d, want 2", skipped)
	}

	// Verify order: posts.idx_title then users.idx_email
	if jobs[0].index.Name != "idx_title" {
		t.Errorf("jobs[0].index.Name = %q, want %q", jobs[0].index.Name, "idx_title")
	}
	if jobs[1].index.Name != "idx_email" {
		t.Errorf("jobs[1].index.Name = %q, want %q", jobs[1].index.Name, "idx_email")
	}
}

func TestPlanIndexJobs_EmptySchema(t *testing.T) {
	schema := &Schema{Tables: []Table{}}
	jobs, skipped := planIndexJobs(schema, "public", defaultTypeMappingConfig())
	if len(jobs) != 0 {
		t.Fatalf("planIndexJobs() returned %d jobs, want 0", len(jobs))
	}
	if skipped != 0 {
		t.Fatalf("planIndexJobs() skipped=%d, want 0", skipped)
	}
}

func TestPlanIndexJobs_AllUnsupported(t *testing.T) {
	schema := &Schema{Tables: []Table{
		{
			SourceName: "t1",
			PGName:     "t1",
			Indexes: []Index{
				{Name: "idx_ft", SourceName: "idx_ft", Type: "FULLTEXT", Columns: []string{"body"}},
				{Name: "idx_expr", SourceName: "idx_expr", Type: "BTREE", HasExpression: true},
			},
		},
	}}

	jobs, skipped := planIndexJobs(schema, "public", defaultTypeMappingConfig())
	if len(jobs) != 0 {
		t.Fatalf("planIndexJobs() returned %d jobs, want 0", len(jobs))
	}
	if skipped != 2 {
		t.Fatalf("planIndexJobs() skipped=%d, want 2", skipped)
	}
}

func TestPlanIndexJobs_PreservesTableAndIndexMetadata(t *testing.T) {
	schema := &Schema{Tables: []Table{
		{
			SourceName: "orders",
			PGName:     "orders",
			Indexes: []Index{
				{
					Name:         "idx_status",
					SourceName:   "idx_status",
					Type:         "BTREE",
					Columns:      []string{"status"},
					ColumnOrders: []string{"ASC"},
					Unique:       true,
				},
			},
		},
	}}

	jobs, _ := planIndexJobs(schema, "public", defaultTypeMappingConfig())
	if len(jobs) != 1 {
		t.Fatalf("planIndexJobs() returned %d jobs, want 1", len(jobs))
	}

	job := jobs[0]
	if job.table.PGName != "orders" {
		t.Errorf("job.table.PGName = %q, want %q", job.table.PGName, "orders")
	}
	if !job.index.Unique {
		t.Error("job.index.Unique = false, want true")
	}
	if job.index.ColumnOrders[0] != "ASC" {
		t.Errorf("job.index.ColumnOrders[0] = %q, want %q", job.index.ColumnOrders[0], "ASC")
	}
}

func TestPlanIndexJobs_PostGISSpatialIndex(t *testing.T) {
	tm := defaultTypeMappingConfig()
	tm.UsePostGIS = true

	schema := &Schema{Tables: []Table{
		{
			SourceName: "places",
			PGName:     "places",
			Columns: []Column{
				{SourceName: "shape", PGName: "shape", DataType: "point", ColumnType: "point"},
			},
			Indexes: []Index{
				{Name: "idx_shape", SourceName: "idx_shape", Type: "SPATIAL", Columns: []string{"shape"}},
			},
		},
	}}

	jobs, skipped := planIndexJobs(schema, "public", tm)
	if len(jobs) != 1 {
		t.Fatalf("planIndexJobs() returned %d jobs, want 1", len(jobs))
	}
	if skipped != 0 {
		t.Fatalf("planIndexJobs() skipped=%d, want 0", skipped)
	}
	if jobs[0].index.Type != "SPATIAL" {
		t.Fatalf("job index type = %q, want SPATIAL", jobs[0].index.Type)
	}
}

func makeTestJobs(n int) []indexJob {
	jobs := make([]indexJob, n)
	for i := range jobs {
		jobs[i] = indexJob{
			table: Table{PGName: fmt.Sprintf("t%d", i)},
			index: Index{Name: fmt.Sprintf("idx_%d", i), Type: "BTREE", Columns: []string{"col"}},
		}
	}
	return jobs
}

func TestExecIndexJobs_SequentialSuccess(t *testing.T) {
	jobs := makeTestJobs(3)
	var count int
	err := execIndexJobs(context.Background(), jobs, 1, func(_ context.Context, _ indexJob) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Fatalf("executed %d jobs, want 3", count)
	}
}

func TestExecIndexJobs_SequentialErrorStops(t *testing.T) {
	jobs := makeTestJobs(5)
	var count int
	err := execIndexJobs(context.Background(), jobs, 1, func(_ context.Context, _ indexJob) error {
		count++
		if count == 2 {
			return fmt.Errorf("fail on job 2")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if count != 2 {
		t.Fatalf("executed %d jobs, want 2 (should stop on first error)", count)
	}
}

func TestExecIndexJobs_ParallelSuccess(t *testing.T) {
	jobs := makeTestJobs(10)
	var count atomic.Int32
	err := execIndexJobs(context.Background(), jobs, 4, func(_ context.Context, _ indexJob) error {
		count.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count.Load() != 10 {
		t.Fatalf("executed %d jobs, want 10", count.Load())
	}
}

func TestExecIndexJobs_ParallelErrorCancelsRemaining(t *testing.T) {
	// Use 2 workers with 20 jobs so that after the error on job 1,
	// the remaining jobs see the cancelled context and bail out.
	jobs := makeTestJobs(20)
	var count atomic.Int32
	err := execIndexJobs(context.Background(), jobs, 2, func(ctx context.Context, _ indexJob) error {
		n := count.Add(1)
		if n == 1 {
			return fmt.Errorf("intentional failure")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error from parallel execution")
	}
	if err.Error() != "intentional failure" {
		t.Fatalf("unexpected error: %v", err)
	}
	// With cancellation, not all 20 jobs should have executed
	executed := count.Load()
	if executed >= 20 {
		t.Fatalf("executed %d jobs; cancellation should have stopped some", executed)
	}
}

func TestExecIndexJobs_ContextAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// A pre-cancelled context should surface context.Canceled so the
	// caller knows index creation did not complete successfully.
	jobs := makeTestJobs(5)
	err := execIndexJobs(ctx, jobs, 4, func(_ context.Context, _ indexJob) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected context.Canceled error, got nil")
	}
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}
