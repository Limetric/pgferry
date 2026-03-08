package main

import "testing"

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

	jobs, skipped := planIndexJobs(schema, "public")
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
	jobs, skipped := planIndexJobs(schema, "public")
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

	jobs, skipped := planIndexJobs(schema, "public")
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

	jobs, _ := planIndexJobs(schema, "public")
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
