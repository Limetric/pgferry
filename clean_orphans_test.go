package main

import (
	"strings"
	"testing"
)

func TestForeignKeyAllNotNullPredicate(t *testing.T) {
	got := foreignKeyAllNotNullPredicate([]string{"tenant_id", "user_id"})
	want := "c.tenant_id IS NOT NULL AND c.user_id IS NOT NULL"
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
	if !strings.Contains(got, "(c.tenant_id IS NOT NULL AND c.user_id IS NOT NULL)") {
		t.Fatalf("expected all-columns non-null predicate, got:\n%s", got)
	}
	if strings.Contains(got, " IS NOT NULL OR ") {
		t.Fatalf("did not expect OR-based null predicate, got:\n%s", got)
	}
	if !strings.Contains(got, "DELETE FROM app.child c") {
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
	if !strings.Contains(got, "UPDATE app.child c SET tenant_id = NULL, user_id = NULL") {
		t.Fatalf("expected UPDATE ... SET NULL statement, got:\n%s", got)
	}
	if !strings.Contains(got, "(c.tenant_id IS NOT NULL AND c.user_id IS NOT NULL)") {
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
	if !strings.Contains(got, "(c.parent_id IS NOT NULL)") {
		t.Fatalf("expected single-column non-null predicate, got:\n%s", got)
	}
	if !strings.Contains(got, "p.id = c.parent_id") {
		t.Fatalf("expected join predicate, got:\n%s", got)
	}
}
