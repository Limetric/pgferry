package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeSchemaRow struct {
	exists bool
	err    error
}

func (r fakeSchemaRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("expected one destination")
	}
	b, ok := dest[0].(*bool)
	if !ok {
		return errors.New("expected *bool destination")
	}
	*b = r.exists
	return nil
}

type fakeSchemaExec struct {
	exists        bool
	queryErr      error
	execCalls     []string
	execErrByStmt map[string]error
}

func (f *fakeSchemaExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, sql)
	if err, ok := f.execErrByStmt[sql]; ok {
		return pgconn.CommandTag{}, err
	}
	return pgconn.CommandTag{}, nil
}

func (f *fakeSchemaExec) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeSchemaRow{exists: f.exists, err: f.queryErr}
}

func TestPrepareTargetSchema_ErrorModeSchemaExists(t *testing.T) {
	exec := &fakeSchemaExec{exists: true}
	err := prepareTargetSchema(context.Background(), exec, "app", "error")
	if err == nil {
		t.Fatal("expected error when schema exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(exec.execCalls) != 0 {
		t.Fatalf("expected no Exec calls, got %d", len(exec.execCalls))
	}
}

func TestPrepareTargetSchema_ErrorModeSchemaMissingCreates(t *testing.T) {
	exec := &fakeSchemaExec{exists: false}
	err := prepareTargetSchema(context.Background(), exec, "app", "error")
	if err != nil {
		t.Fatalf("prepareTargetSchema() error: %v", err)
	}
	if len(exec.execCalls) != 1 {
		t.Fatalf("expected one Exec call, got %d", len(exec.execCalls))
	}
	if exec.execCalls[0] != "CREATE SCHEMA app" {
		t.Fatalf("unexpected SQL: %s", exec.execCalls[0])
	}
}

func TestPrepareTargetSchema_RecreateDropsThenCreates(t *testing.T) {
	exec := &fakeSchemaExec{}
	err := prepareTargetSchema(context.Background(), exec, "app", "recreate")
	if err != nil {
		t.Fatalf("prepareTargetSchema() error: %v", err)
	}
	if len(exec.execCalls) != 2 {
		t.Fatalf("expected two Exec calls, got %d", len(exec.execCalls))
	}
	if exec.execCalls[0] != "DROP SCHEMA IF EXISTS app CASCADE" {
		t.Fatalf("unexpected first SQL: %s", exec.execCalls[0])
	}
	if exec.execCalls[1] != "CREATE SCHEMA app" {
		t.Fatalf("unexpected second SQL: %s", exec.execCalls[1])
	}
}

func TestPrepareTargetSchema_UnsupportedMode(t *testing.T) {
	exec := &fakeSchemaExec{}
	err := prepareTargetSchema(context.Background(), exec, "app", "merge")
	if err == nil {
		t.Fatal("expected error for unsupported mode")
	}
	if !strings.Contains(err.Error(), "unsupported on_schema_exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareTargetSchema_QueryError(t *testing.T) {
	exec := &fakeSchemaExec{queryErr: errors.New("db offline")}
	err := prepareTargetSchema(context.Background(), exec, "app", "error")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "check schema existence") {
		t.Fatalf("unexpected error: %v", err)
	}
}
