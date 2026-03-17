package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeOrphanRow struct {
	count int64
	err   error
}

func (r fakeOrphanRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("expected one destination")
	}
	ptr, ok := dest[0].(*int64)
	if !ok {
		return errors.New("expected *int64 destination")
	}
	*ptr = r.count
	return nil
}

type fakeOrphanExec struct {
	counts       map[string]int64
	queryErr     error
	execCalls    []string
	execErrBySQL map[string]error
}

func (f *fakeOrphanExec) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if f.queryErr != nil {
		return fakeOrphanRow{err: f.queryErr}
	}
	count, ok := f.counts[sql]
	if !ok {
		return fakeOrphanRow{err: fmt.Errorf("unexpected query: %s", sql)}
	}
	return fakeOrphanRow{count: count}
}

func (f *fakeOrphanExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, sql)
	if err := f.execErrBySQL[sql]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("UPDATE 2"), nil
}

func orphanCleanupFixture(deleteRule string) (Table, ForeignKey) {
	table := Table{PGName: "child"}
	fk := ForeignKey{
		Name:       "fk_child_parent",
		Columns:    []string{"tenant_id", "user_id"},
		RefPGTable: "parent",
		RefColumns: []string{"tenant_id", "id"},
		DeleteRule: deleteRule,
	}
	table.ForeignKeys = []ForeignKey{fk}
	return table, fk
}

func TestForeignKeyAllNotNullPredicate(t *testing.T) {
	got := foreignKeyAllNotNullPredicate([]string{"tenant_id", "user_id"})
	want := `c."tenant_id" IS NOT NULL AND c."user_id" IS NOT NULL`
	if got != want {
		t.Fatalf("foreignKeyAllNotNullPredicate() = %q, want %q", got, want)
	}
}

func TestBuildCleanOrphansSQL_CompositeDeleteUsesAllColumnsNonNull(t *testing.T) {
	table := Table{PGName: "child"}
	fk := ForeignKey{
		Name:       "fk_child_parent",
		Columns:    []string{"tenant_id", "user_id"},
		RefPGTable: "parent",
		RefColumns: []string{"tenant_id", "id"},
		DeleteRule: "CASCADE",
	}

	got := buildCleanOrphansSQL("app", table, fk)
	if !strings.Contains(got, `(c."tenant_id" IS NOT NULL AND c."user_id" IS NOT NULL)`) {
		t.Fatalf("expected all-columns non-null predicate, got:\n%s", got)
	}
	if strings.Contains(got, " IS NOT NULL OR ") {
		t.Fatalf("did not expect OR-based null predicate, got:\n%s", got)
	}
	if !strings.Contains(got, `DELETE FROM "app"."child" c`) {
		t.Fatalf("expected DELETE statement, got:\n%s", got)
	}
}

func TestBuildCleanOrphansSQL_SetNullUsesAllColumnsNonNull(t *testing.T) {
	table := Table{PGName: "child"}
	fk := ForeignKey{
		Name:       "fk_child_parent",
		Columns:    []string{"tenant_id", "user_id"},
		RefPGTable: "parent",
		RefColumns: []string{"tenant_id", "id"},
		DeleteRule: "SET NULL",
	}

	got := buildCleanOrphansSQL("app", table, fk)
	if !strings.Contains(got, `UPDATE "app"."child" c SET "tenant_id" = NULL, "user_id" = NULL`) {
		t.Fatalf("expected UPDATE ... SET NULL statement, got:\n%s", got)
	}
	if !strings.Contains(got, `(c."tenant_id" IS NOT NULL AND c."user_id" IS NOT NULL)`) {
		t.Fatalf("expected all-columns non-null predicate, got:\n%s", got)
	}
	if strings.Contains(got, " IS NOT NULL OR ") {
		t.Fatalf("did not expect OR-based null predicate, got:\n%s", got)
	}
}

func TestBuildCleanOrphansCountSQL_CompositeUsesAllColumnsNonNull(t *testing.T) {
	table, fk := orphanCleanupFixture("CASCADE")

	got := buildCleanOrphansCountSQL("app", table, fk)
	if !strings.Contains(got, `SELECT COUNT(*) FROM "app"."child" c`) {
		t.Fatalf("expected count statement, got:\n%s", got)
	}
	if !strings.Contains(got, `(c."tenant_id" IS NOT NULL AND c."user_id" IS NOT NULL)`) {
		t.Fatalf("expected all-columns non-null predicate, got:\n%s", got)
	}
	if strings.Contains(got, " IS NOT NULL OR ") {
		t.Fatalf("did not expect OR-based null predicate, got:\n%s", got)
	}
}

func TestBuildCleanOrphansSQL_SingleColumnStillWorks(t *testing.T) {
	table := Table{PGName: "child"}
	fk := ForeignKey{
		Name:       "fk_child_parent",
		Columns:    []string{"parent_id"},
		RefPGTable: "parent",
		RefColumns: []string{"id"},
		DeleteRule: "NO ACTION",
	}

	got := buildCleanOrphansSQL("app", table, fk)
	if !strings.Contains(got, `(c."parent_id" IS NOT NULL)`) {
		t.Fatalf("expected single-column non-null predicate, got:\n%s", got)
	}
	if !strings.Contains(got, `p."id" = c."parent_id"`) {
		t.Fatalf("expected join predicate, got:\n%s", got)
	}
}

func TestCleanOrphans_ReportModeReturnsErrorWithoutMutation(t *testing.T) {
	table, fk := orphanCleanupFixture("SET NULL")
	schema := &Schema{Tables: []Table{table}}
	countSQL := buildCleanOrphansCountSQL("app", table, fk)
	exec := &fakeOrphanExec{
		counts: map[string]int64{
			countSQL: 2,
		},
	}

	err := cleanOrphans(context.Background(), exec, schema, "app", "report", 0)
	if err == nil {
		t.Fatal("expected report mode to abort when orphan rows are detected")
	}
	if !strings.Contains(err.Error(), "report mode found 1 orphan-cleanup action(s) affecting 2 row(s)") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.execCalls) != 0 {
		t.Fatalf("expected no mutation in report mode, got %d exec call(s)", len(exec.execCalls))
	}
}

func TestCleanOrphans_ReportModeReturnsErrorWithoutMutationWhenNoOrphansExist(t *testing.T) {
	table, fk := orphanCleanupFixture("SET NULL")
	schema := &Schema{Tables: []Table{table}}
	countSQL := buildCleanOrphansCountSQL("app", table, fk)
	exec := &fakeOrphanExec{
		counts: map[string]int64{
			countSQL: 0,
		},
	}

	err := cleanOrphans(context.Background(), exec, schema, "app", "report", 0)
	if err == nil {
		t.Fatal("expected report mode to abort even when no orphan rows are detected")
	}
	if !strings.Contains(err.Error(), "report mode found 0 orphan-cleanup action(s) affecting 0 row(s)") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.execCalls) != 0 {
		t.Fatalf("expected no mutation in report mode, got %d exec call(s)", len(exec.execCalls))
	}
}

func TestCleanOrphans_ApplyModeExecutesMutation(t *testing.T) {
	table, fk := orphanCleanupFixture("CASCADE")
	schema := &Schema{Tables: []Table{table}}
	countSQL := buildCleanOrphansCountSQL("app", table, fk)
	applySQL := buildCleanOrphansSQL("app", table, fk)
	exec := &fakeOrphanExec{
		counts: map[string]int64{
			countSQL: 3,
		},
	}

	if err := cleanOrphans(context.Background(), exec, schema, "app", "apply", 0); err != nil {
		t.Fatalf("cleanOrphans() error: %v", err)
	}
	if len(exec.execCalls) != 1 {
		t.Fatalf("expected one mutation, got %d", len(exec.execCalls))
	}
	if exec.execCalls[0] != applySQL {
		t.Fatalf("unexpected SQL:\n%s", exec.execCalls[0])
	}
}

func TestCleanOrphans_ThresholdExceededAbortsBeforeMutation(t *testing.T) {
	table, fk := orphanCleanupFixture("CASCADE")
	schema := &Schema{Tables: []Table{table}}
	countSQL := buildCleanOrphansCountSQL("app", table, fk)
	exec := &fakeOrphanExec{
		counts: map[string]int64{
			countSQL: 11,
		},
	}

	err := cleanOrphans(context.Background(), exec, schema, "app", "apply", 10)
	if err == nil {
		t.Fatal("expected threshold error")
	}
	if !strings.Contains(err.Error(), "clean_orphans_max_rows=10") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.execCalls) != 0 {
		t.Fatalf("expected no mutation when threshold is exceeded, got %d exec call(s)", len(exec.execCalls))
	}
}

func TestCleanOrphans_ThresholdEqualityAllowsMutation(t *testing.T) {
	table, fk := orphanCleanupFixture("CASCADE")
	schema := &Schema{Tables: []Table{table}}
	countSQL := buildCleanOrphansCountSQL("app", table, fk)
	applySQL := buildCleanOrphansSQL("app", table, fk)
	exec := &fakeOrphanExec{
		counts: map[string]int64{
			countSQL: 11,
		},
	}

	if err := cleanOrphans(context.Background(), exec, schema, "app", "apply", 11); err != nil {
		t.Fatalf("cleanOrphans() error: %v", err)
	}
	if len(exec.execCalls) != 1 || exec.execCalls[0] != applySQL {
		t.Fatalf("expected one mutation with matching SQL, got %v", exec.execCalls)
	}
}

func TestCleanOrphans_ApplyModeExecError(t *testing.T) {
	table, fk := orphanCleanupFixture("CASCADE")
	schema := &Schema{Tables: []Table{table}}
	countSQL := buildCleanOrphansCountSQL("app", table, fk)
	applySQL := buildCleanOrphansSQL("app", table, fk)
	exec := &fakeOrphanExec{
		counts: map[string]int64{
			countSQL: 3,
		},
		execErrBySQL: map[string]error{
			applySQL: errors.New("write failed"),
		},
	}

	err := cleanOrphans(context.Background(), exec, schema, "app", "apply", 0)
	if err == nil {
		t.Fatal("expected apply mode exec error")
	}
	if !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}
