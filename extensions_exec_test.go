package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeExtensionStatusRow struct {
	installed bool
	available bool
	err       error
}

func (r fakeExtensionStatusRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 2 {
		return errors.New("expected two destinations")
	}
	installed, ok := dest[0].(*bool)
	if !ok {
		return errors.New("expected *bool installed destination")
	}
	available, ok := dest[1].(*bool)
	if !ok {
		return errors.New("expected *bool available destination")
	}
	*installed = r.installed
	*available = r.available
	return nil
}

type fakeExtensionExecutor struct {
	statusByName map[string]fakeExtensionStatusRow
	execCalls    []string
	execErrBySQL map[string]error
}

func (f *fakeExtensionExecutor) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if len(args) != 1 {
		return fakeExtensionStatusRow{err: errors.New("expected extension name argument")}
	}
	name, ok := args[0].(string)
	if !ok {
		return fakeExtensionStatusRow{err: errors.New("expected extension name string")}
	}
	if row, ok := f.statusByName[name]; ok {
		return row
	}
	return fakeExtensionStatusRow{err: errors.New("unexpected extension lookup")}
}

func (f *fakeExtensionExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execCalls = append(f.execCalls, sql)
	if err := f.execErrBySQL[sql]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("CREATE EXTENSION"), nil
}

func TestEnsureRequiredExtensions_InstalledSkipsCreation(t *testing.T) {
	exec := &fakeExtensionExecutor{
		statusByName: map[string]fakeExtensionStatusRow{
			"citext": {installed: true, available: true},
		},
	}

	err := ensureRequiredExtensions(context.Background(), exec, []extensionRequirement{{
		Name:            "citext",
		Feature:         "ci_as_citext",
		CreateIfMissing: true,
	}})
	if err != nil {
		t.Fatalf("ensureRequiredExtensions() error: %v", err)
	}
	if len(exec.execCalls) != 0 {
		t.Fatalf("exec calls = %v, want none", exec.execCalls)
	}
}

func TestEnsureRequiredExtensions_NotAvailableReturnsError(t *testing.T) {
	exec := &fakeExtensionExecutor{
		statusByName: map[string]fakeExtensionStatusRow{
			"postgis": {installed: false, available: false},
		},
	}

	err := ensureRequiredExtensions(context.Background(), exec, []extensionRequirement{{
		Name:            "postgis",
		Feature:         "postgis",
		CreateIfMissing: true,
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `requires PostgreSQL extension "postgis", but it is not available`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRequiredExtensions_CreateDisabledReturnsHint(t *testing.T) {
	exec := &fakeExtensionExecutor{
		statusByName: map[string]fakeExtensionStatusRow{
			"postgis": {installed: false, available: true},
		},
	}

	err := ensureRequiredExtensions(context.Background(), exec, []extensionRequirement{{
		Name:            "postgis",
		Feature:         "postgis",
		CreateIfMissing: false,
		CreateHint:      "or set [postgis].create_extension = true",
	}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "install it first") || !strings.Contains(err.Error(), "create_extension = true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureRequiredExtensions_CreatesMissingExtension(t *testing.T) {
	exec := &fakeExtensionExecutor{
		statusByName: map[string]fakeExtensionStatusRow{
			"citext": {installed: false, available: true},
		},
	}

	err := ensureRequiredExtensions(context.Background(), exec, []extensionRequirement{{
		Name:            "citext",
		Feature:         "ci_as_citext",
		CreateIfMissing: true,
	}})
	if err != nil {
		t.Fatalf("ensureRequiredExtensions() error: %v", err)
	}
	want := `CREATE EXTENSION IF NOT EXISTS "citext"`
	if len(exec.execCalls) != 1 || exec.execCalls[0] != want {
		t.Fatalf("exec calls = %v, want [%s]", exec.execCalls, want)
	}
}
