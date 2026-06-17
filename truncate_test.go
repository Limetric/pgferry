package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestTruncateTargetTablesBeforeCopy_GeneratesSingleNonCascadeStatement(t *testing.T) {
	exec := &recordingStatementExecutor{}
	schema := &Schema{Tables: []Table{
		{PGName: "users"},
		{PGName: `order"items`},
	}}

	if err := truncateTargetTablesBeforeCopy(context.Background(), exec, schema, "app"); err != nil {
		t.Fatalf("truncateTargetTablesBeforeCopy() error: %v", err)
	}

	want := `TRUNCATE TABLE "app"."users", "app"."order""items"`
	if len(exec.calls) != 1 || exec.calls[0] != want {
		t.Fatalf("exec calls = %v, want [%q]", exec.calls, want)
	}
}

func TestTruncateTargetTablesBeforeCopy_LogsNonCascadeScope(t *testing.T) {
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	exec := &recordingStatementExecutor{}
	schema := &Schema{Tables: []Table{
		{PGName: "orders"},
		{PGName: "order_items"},
	}}

	if err := truncateTargetTablesBeforeCopy(context.Background(), exec, schema, "app"); err != nil {
		t.Fatalf("truncateTargetTablesBeforeCopy() error: %v", err)
	}

	want := `truncating target tables before COPY without CASCADE: "app"."orders", "app"."order_items"`
	if !strings.Contains(buf.String(), want) {
		t.Fatalf("log output = %q, want %q", buf.String(), want)
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
	truncateSQL := `TRUNCATE TABLE "app"."users"`
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

func TestTruncateTargetTablesOnceBeforeCopy_DiscoversConfiguredSchemasAndCascades(t *testing.T) {
	exec := &recordingQueryExecutor{
		scalarsByQuery: map[string]any{
			discoverTargetTablesForTruncateSQL: []truncateTargetSchemaTables{
				{Schema: "schema_a", Exists: true, Tables: []string{`"schema_a"."accounts"`}},
				{Schema: "schema_b", Exists: true, Tables: []string{`"schema_b"."orders"`}},
			},
		},
	}

	if err := truncateTargetTablesOnceBeforeCopy(context.Background(), exec, []string{"schema_b", "schema_a"}); err != nil {
		t.Fatalf("truncateTargetTablesOnceBeforeCopy() error: %v", err)
	}

	if len(exec.queryArgs) != 1 {
		t.Fatalf("query args = %v, want one discovery query", exec.queryArgs)
	}
	gotSchemas, ok := exec.queryArgs[0].([]string)
	if !ok {
		t.Fatalf("discovery arg type = %T, want []string", exec.queryArgs[0])
	}
	if strings.Join(gotSchemas, ",") != "schema_b,schema_a" {
		t.Fatalf("discovery schemas = %v, want configured order", gotSchemas)
	}

	want := `TRUNCATE TABLE "schema_a"."accounts", "schema_b"."orders" CASCADE`
	if len(exec.calls) != 1 || exec.calls[0] != want {
		t.Fatalf("exec calls = %v, want [%q]", exec.calls, want)
	}
}

func TestTruncateTargetTablesOnceBeforeCopy_RejectsConfiguredSchemaWithNoTables(t *testing.T) {
	exec := &recordingQueryExecutor{
		scalarsByQuery: map[string]any{
			discoverTargetTablesForTruncateSQL: []truncateTargetSchemaTables{
				{Schema: "empty", Exists: true, Tables: nil},
			},
		},
	}

	err := truncateTargetTablesOnceBeforeCopy(context.Background(), exec, []string{"empty"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `schema "empty" has no target tables to truncate`) {
		t.Fatalf("error = %v, want empty schema detail", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("exec calls = %v, want none", exec.calls)
	}
}

func TestTruncateTargetTablesOnceBeforeCopy_WrapsDiscoveryError(t *testing.T) {
	execErr := errors.New("catalog unavailable")
	exec := &recordingQueryExecutor{
		errByQuery: map[string]error{discoverTargetTablesForTruncateSQL: execErr},
	}

	err := truncateTargetTablesOnceBeforeCopy(context.Background(), exec, []string{"app"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, execErr) {
		t.Fatalf("expected error to wrap execErr, got %v", err)
	}
	if !strings.Contains(err.Error(), "discover target tables to truncate") {
		t.Fatalf("error = %v, want discovery context", err)
	}
}

func TestTruncateTargetTablesOnceBeforeCopy_RejectsMissingConfiguredSchema(t *testing.T) {
	exec := &recordingQueryExecutor{
		scalarsByQuery: map[string]any{
			discoverTargetTablesForTruncateSQL: []truncateTargetSchemaTables{
				{Schema: "schema_a", Exists: true, Tables: []string{`"schema_a"."accounts"`}},
				{Schema: "schema_b", Exists: false, Tables: nil},
			},
		},
	}

	err := truncateTargetTablesOnceBeforeCopy(context.Background(), exec, []string{"schema_a", "schema_b"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `schema "schema_b" was not found`) {
		t.Fatalf("error = %v, want missing schema detail", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("exec calls = %v, want none", exec.calls)
	}
}

func TestRunTruncateBeforeCopyRejectsUnhandledMode(t *testing.T) {
	exec := &recordingQueryExecutor{}
	cfg := &MigrationConfig{TruncateBeforeCopy: truncateBeforeCopyMode("future")}

	err := runTruncateBeforeCopy(context.Background(), exec, cfg, &Schema{Tables: []Table{{PGName: "users"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `internal: unhandled truncate_before_copy mode "future"`) {
		t.Fatalf("error = %v, want unhandled mode detail", err)
	}
	if len(exec.calls) != 0 {
		t.Fatalf("exec calls = %v, want none", exec.calls)
	}
}

type recordingQueryExecutor struct {
	recordingStatementExecutor
	scalarsByQuery map[string]any
	queryArgs      []any
	errByQuery     map[string]error
}

func (r *recordingQueryExecutor) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	r.queryArgs = append(r.queryArgs, args...)
	if err := r.errByQuery[sql]; err != nil {
		return fakeRow{err: err}
	}
	return fakeRow{value: r.scalarsByQuery[sql]}
}

type fakeRow struct {
	value any
	err   error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("fakeRow expects one destination")
	}
	switch d := dest[0].(type) {
	case *string:
		if r.value == nil {
			*d = "[]"
			return nil
		}
		switch value := r.value.(type) {
		case string:
			*d = value
		default:
			data, err := json.Marshal(value)
			if err != nil {
				return err
			}
			*d = string(data)
		}
		return nil
	default:
		return errors.New("unsupported fakeRow destination")
	}
}
