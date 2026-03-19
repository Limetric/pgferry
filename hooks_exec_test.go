package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingStatementExecutor struct {
	calls      []string
	errByQuery map[string]error
}

func (r *recordingStatementExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.calls = append(r.calls, sql)
	if err := r.errByQuery[sql]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("EXECUTE 1"), nil
}

func TestLoadAndExecSQLFiles_ResolvesRelativePathsAndSubstitutesSchema(t *testing.T) {
	dir := t.TempDir()
	hookPath := filepath.Join(dir, "after_data.sql")
	if err := os.WriteFile(hookPath, []byte(`
		INSERT INTO {{schema}}.audit_log(message) VALUES ('one');
		UPDATE "{{schema}}"."users" SET name = 'Alice';
	`), 0644); err != nil {
		t.Fatalf("write hook: %v", err)
	}

	cfg := &MigrationConfig{Schema: "app", configDir: dir}
	exec := &recordingStatementExecutor{}

	if err := loadAndExecSQLFiles(context.Background(), exec, cfg, []string{"after_data.sql"}, "after_data"); err != nil {
		t.Fatalf("loadAndExecSQLFiles() error: %v", err)
	}

	want := []string{
		"INSERT INTO app.audit_log(message) VALUES ('one')",
		`UPDATE "app"."users" SET name = 'Alice'`,
	}
	if len(exec.calls) != len(want) {
		t.Fatalf("exec calls = %v, want %v", exec.calls, want)
	}
	for i := range want {
		if exec.calls[i] != want[i] {
			t.Fatalf("exec calls[%d] = %q, want %q", i, exec.calls[i], want[i])
		}
	}
}

func TestLoadAndExecSQLFiles_ExecutesFilesInOrder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "01.sql"), []byte("SELECT 1;"), 0644); err != nil {
		t.Fatalf("write 01.sql: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "02.sql"), []byte("SELECT 2;"), 0644); err != nil {
		t.Fatalf("write 02.sql: %v", err)
	}

	cfg := &MigrationConfig{Schema: "app", configDir: dir}
	exec := &recordingStatementExecutor{}

	if err := loadAndExecSQLFiles(context.Background(), exec, cfg, []string{"01.sql", "02.sql"}, "after_all"); err != nil {
		t.Fatalf("loadAndExecSQLFiles() error: %v", err)
	}

	want := []string{"SELECT 1", "SELECT 2"}
	if len(exec.calls) != len(want) {
		t.Fatalf("exec calls = %v, want %v", exec.calls, want)
	}
	for i := range want {
		if exec.calls[i] != want[i] {
			t.Fatalf("exec calls[%d] = %q, want %q", i, exec.calls[i], want[i])
		}
	}
}

func TestLoadAndExecSQLFiles_ReportsFailingStatementNumber(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "before_fk.sql"), []byte("SELECT 1; SELECT 2;"), 0644); err != nil {
		t.Fatalf("write before_fk.sql: %v", err)
	}

	cfg := &MigrationConfig{Schema: "app", configDir: dir}
	exec := &recordingStatementExecutor{
		errByQuery: map[string]error{"SELECT 2": errors.New("boom")},
	}

	err := loadAndExecSQLFiles(context.Background(), exec, cfg, []string{"before_fk.sql"}, "before_fk")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "hook before_fk: before_fk.sql: statement 2") {
		t.Fatalf("error = %v, want failing statement detail", err)
	}
	if !strings.Contains(err.Error(), "SQL: SELECT 2") {
		t.Fatalf("error = %v, want failing SQL", err)
	}
}
