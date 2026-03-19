package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRollbackExecutor struct {
	calls       []string
	errByQuery  map[string]error
	rolledBack  bool
	rollbackErr error
}

func (f *fakeRollbackExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.calls = append(f.calls, sql)
	if err := f.errByQuery[sql]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("ALTER TABLE"), nil
}

func (f *fakeRollbackExecutor) Rollback(_ context.Context) error {
	f.rolledBack = true
	return f.rollbackErr
}

func TestPreflightDataOnlyTriggerControl_SuccessRollsBackEachProbe(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{PGName: "users"},
			{PGName: "posts"},
		},
	}
	txs := []*fakeRollbackExecutor{
		{},
		{},
	}
	beginCalls := 0

	err := preflightDataOnlyTriggerControl(context.Background(), func(context.Context) (rollbackExecutor, error) {
		tx := txs[beginCalls]
		beginCalls++
		return tx, nil
	}, schema, "app")
	if err != nil {
		t.Fatalf("preflightDataOnlyTriggerControl() error: %v", err)
	}
	if beginCalls != len(txs) {
		t.Fatalf("begin calls = %d, want %d", beginCalls, len(txs))
	}

	wantSQL := []string{
		`ALTER TABLE "app"."users" DISABLE TRIGGER ALL`,
		`ALTER TABLE "app"."posts" DISABLE TRIGGER ALL`,
	}
	for i, tx := range txs {
		if !tx.rolledBack {
			t.Fatalf("tx %d was not rolled back", i)
		}
		if len(tx.calls) != 1 || tx.calls[0] != wantSQL[i] {
			t.Fatalf("tx %d calls = %v, want [%s]", i, tx.calls, wantSQL[i])
		}
	}
}

func TestPreflightDataOnlyTriggerControl_FailureReturnsGuidanceAndRollsBack(t *testing.T) {
	probeSQL := `ALTER TABLE "app"."users" DISABLE TRIGGER ALL`
	tx := &fakeRollbackExecutor{
		errByQuery: map[string]error{
			probeSQL: errors.New("permission denied"),
		},
	}

	err := preflightDataOnlyTriggerControl(context.Background(), func(context.Context) (rollbackExecutor, error) {
		return tx, nil
	}, &Schema{
		Tables: []Table{{PGName: "users"}},
	}, "app")
	if err == nil {
		t.Fatal("expected error")
	}
	if !tx.rolledBack {
		t.Fatal("expected failed probe transaction to be rolled back")
	}
	for _, want := range []string{
		`table app.users`,
		"preflight failed before COPY started",
		"no data was copied",
		"probe transaction was rolled back",
		"disable and re-enable triggers",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestPreflightDataOnlyTriggerControl_FailureWithRollbackErrorDoesNotClaimSuccess(t *testing.T) {
	probeSQL := `ALTER TABLE "app"."users" DISABLE TRIGGER ALL`
	tx := &fakeRollbackExecutor{
		errByQuery: map[string]error{
			probeSQL: errors.New("permission denied"),
		},
		rollbackErr: errors.New("rollback failed"),
	}

	err := preflightDataOnlyTriggerControl(context.Background(), func(context.Context) (rollbackExecutor, error) {
		return tx, nil
	}, &Schema{
		Tables: []Table{{PGName: "users"}},
	}, "app")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "probe transaction was rolled back") {
		t.Fatalf("error %q should not claim rollback succeeded", err)
	}
	for _, want := range []string{
		"attempted to roll back the probe transaction, but that rollback failed",
		"rollback trigger-control preflight for table app.users: rollback failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestSetTriggers_DisableFailureIncludesPrivilegeGuidance(t *testing.T) {
	sql := `ALTER TABLE "app"."users" DISABLE TRIGGER ALL`
	exec := &recordingStatementExecutor{
		errByQuery: map[string]error{
			sql: errors.New("permission denied"),
		},
	}

	err := setTriggers(context.Background(), exec, &Schema{
		Tables: []Table{{PGName: "users"}},
	}, "app", false)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		`table app.users`,
		"data_only requires permission to disable and re-enable triggers",
		"DISABLE/ENABLE TRIGGER ALL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "table app.users: users:") {
		t.Fatalf("error %q should not repeat the unqualified table name", err)
	}
}

func TestSetTriggers_EnableFailureIncludesInspectionGuidance(t *testing.T) {
	sql := `ALTER TABLE "app"."users" ENABLE TRIGGER ALL`
	exec := &recordingStatementExecutor{
		errByQuery: map[string]error{
			sql: errors.New("permission denied"),
		},
	}

	err := setTriggers(context.Background(), exec, &Schema{
		Tables: []Table{{PGName: "users"}},
	}, "app", true)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		`table app.users`,
		"attempted to restore trigger state",
		"triggers are enabled before retrying",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "inspect app.users") {
		t.Fatalf("error %q should not repeat the table name in the guidance text", err)
	}
}
