package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestTruncateTargetTablesBeforeCopy_GeneratesSingleCascadeStatement(t *testing.T) {
	exec := &recordingStatementExecutor{}
	schema := &Schema{Tables: []Table{
		{PGName: "users"},
		{PGName: `order"items`},
	}}

	if err := truncateTargetTablesBeforeCopy(context.Background(), exec, schema, "app"); err != nil {
		t.Fatalf("truncateTargetTablesBeforeCopy() error: %v", err)
	}

	want := `TRUNCATE TABLE "app"."users", "app"."order""items" CASCADE`
	if len(exec.calls) != 1 || exec.calls[0] != want {
		t.Fatalf("exec calls = %v, want [%q]", exec.calls, want)
	}
}

func TestTruncateTargetTablesBeforeCopy_SkipsEmptySchema(t *testing.T) {
	exec := &recordingStatementExecutor{}

	if err := truncateTargetTablesBeforeCopy(context.Background(), exec, &Schema{}, "app"); err != nil {
		t.Fatalf("truncateTargetTablesBeforeCopy() error: %v", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("exec calls = %v, want none", exec.calls)
	}
}

func TestTruncateTargetTablesBeforeCopy_WrapsExecError(t *testing.T) {
	truncateSQL := `TRUNCATE TABLE "app"."users" CASCADE`
	execErr := errors.New("permission denied")
	exec := &recordingStatementExecutor{
		errByQuery: map[string]error{truncateSQL: execErr},
	}

	err := truncateTargetTablesBeforeCopy(context.Background(), exec, &Schema{Tables: []Table{{PGName: "users"}}}, "app")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, execErr) {
		t.Fatalf("expected error to wrap execErr, got %v", err)
	}
	if !strings.Contains(err.Error(), truncateSQL) {
		t.Fatalf("error = %v, want SQL detail", err)
	}
}
