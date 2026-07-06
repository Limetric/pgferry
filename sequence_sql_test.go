package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestResetSequenceStatements_QuotesSchemaAndSequenceRegclass(t *testing.T) {
	table := Table{PGName: "events"}
	col := Column{PGName: "id", Extra: "auto_increment"}

	stmts := resetSequenceStatements("order", table, col)
	if len(stmts) != 3 {
		t.Fatalf("statement count = %d, want 3", len(stmts))
	}

	if !strings.Contains(stmts[0], `CREATE SEQUENCE IF NOT EXISTS "order"."events_id_seq"`) {
		t.Fatalf("create sequence statement = %q", stmts[0])
	}
	if !strings.Contains(stmts[1], `SELECT setval('"order"."events_id_seq"'::regclass`) {
		t.Fatalf("setval statement = %q", stmts[1])
	}
	if !strings.Contains(stmts[2], `SET DEFAULT nextval('"order"."events_id_seq"'::regclass)`) {
		t.Fatalf("nextval statement = %q", stmts[2])
	}
}

func TestResetSequenceStatements_QuotesNonTrivialSequenceName(t *testing.T) {
	table := Table{PGName: "audit"}
	col := Column{PGName: "event-id", Extra: "auto_increment"}

	stmts := resetSequenceStatements("app", table, col)
	if !strings.Contains(stmts[0], `"app"."audit_event-id_seq"`) {
		t.Fatalf("create sequence statement = %q", stmts[0])
	}
	if !strings.Contains(stmts[1], `'"app"."audit_event-id_seq"'::regclass`) {
		t.Fatalf("setval statement = %q", stmts[1])
	}
	if !strings.Contains(stmts[2], `'"app"."audit_event-id_seq"'::regclass`) {
		t.Fatalf("nextval statement = %q", stmts[2])
	}
}

func TestResetSequenceStatements_QuotesReservedColumnName(t *testing.T) {
	table := Table{PGName: "audit"}
	col := Column{PGName: "collation", Extra: "auto_increment"}

	stmts := resetSequenceStatements("app", table, col)
	if !strings.Contains(stmts[1], `SELECT MAX("collation") FROM "app"."audit"`) {
		t.Fatalf("setval statement = %q", stmts[1])
	}
	if !strings.Contains(stmts[2], `ALTER TABLE "app"."audit" ALTER COLUMN "collation" SET DEFAULT`) {
		t.Fatalf("nextval statement = %q", stmts[2])
	}
}

func TestDataOnlySequenceLookupSQL_ResolvesAttachedThenConventionName(t *testing.T) {
	table := Table{PGName: "Orders"}
	col := Column{PGName: "Id", Extra: "auto_increment"}

	q := dataOnlySequenceLookupSQL("myapp", table, col)
	if !strings.Contains(q, `pg_get_serial_sequence('"myapp"."Orders"', 'Id')`) {
		t.Fatalf("lookup should resolve the attached sequence first, got: %q", q)
	}
	if !strings.Contains(q, `to_regclass('"myapp"."Orders_Id_seq"')`) {
		t.Fatalf("lookup should fall back to the pgferry naming convention, got: %q", q)
	}
	if !strings.Contains(q, "COALESCE(") {
		t.Fatalf("lookup should prefer the attached sequence via COALESCE, got: %q", q)
	}
}

func TestDataOnlySequenceLookupSQL_EscapesQuotes(t *testing.T) {
	table := Table{PGName: "o'brien"}
	col := Column{PGName: "id", Extra: "auto_increment"}

	q := dataOnlySequenceLookupSQL("app", table, col)
	if !strings.Contains(q, `'"app"."o''brien"'`) {
		t.Fatalf("table literal should escape single quotes, got: %q", q)
	}
}

// fakeSequenceRow implements pgx.Row for the sequence lookup query.
type fakeSequenceRow struct {
	seq *string
	err error
}

func (r fakeSequenceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("expected one destination")
	}
	out, ok := dest[0].(**string)
	if !ok {
		return errors.New("expected **string destination")
	}
	*out = r.seq
	return nil
}

// fakeSequenceExecutor implements queryExecutor for resetAttachedSequence tests.
type fakeSequenceExecutor struct {
	row       fakeSequenceRow
	querySQLs []string
	execSQLs  []string
	execErr   error
}

func (f *fakeSequenceExecutor) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	f.querySQLs = append(f.querySQLs, sql)
	return f.row
}

func (f *fakeSequenceExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execSQLs = append(f.execSQLs, sql)
	if f.execErr != nil {
		return pgconn.CommandTag{}, f.execErr
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func TestResetAttachedSequence_UsesResolvedSequenceName(t *testing.T) {
	seq := `myapp."OrderIdSequence"`
	exec := &fakeSequenceExecutor{row: fakeSequenceRow{seq: &seq}}
	table := Table{PGName: "Orders"}
	col := Column{PGName: "Id", Extra: "auto_increment"}

	if err := resetAttachedSequence(context.Background(), exec, "myapp", table, col); err != nil {
		t.Fatalf("resetAttachedSequence() error: %v", err)
	}
	if len(exec.execSQLs) != 1 {
		t.Fatalf("exec calls = %v, want exactly one setval", exec.execSQLs)
	}
	if !strings.Contains(exec.execSQLs[0], `SELECT setval('myapp."OrderIdSequence"'::regclass`) {
		t.Fatalf("setval should target the resolved sequence, got: %q", exec.execSQLs[0])
	}
	if !strings.Contains(exec.execSQLs[0], `COALESCE((SELECT MAX("Id") FROM "myapp"."Orders"), 0) + 1, false)`) {
		t.Fatalf("setval should advance to max+1, got: %q", exec.execSQLs[0])
	}
}

func TestResetAttachedSequence_NoSequenceFailsWithClearError(t *testing.T) {
	exec := &fakeSequenceExecutor{row: fakeSequenceRow{seq: nil}}
	table := Table{PGName: "Orders"}
	col := Column{PGName: "Id", Extra: "auto_increment"}

	err := resetAttachedSequence(context.Background(), exec, "myapp", table, col)
	if err == nil {
		t.Fatal("expected error when no sequence exists for the column")
	}
	if len(exec.execSQLs) != 0 {
		t.Fatalf("no setval should run when the sequence is missing, got: %v", exec.execSQLs)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"myapp"."Orders"."Id"`) {
		t.Fatalf("error should name the column, got: %q", msg)
	}
	if !strings.Contains(msg, "Orders_Id_seq") {
		t.Fatalf("error should mention the convention name that was tried, got: %q", msg)
	}
}

func TestResetAttachedSequence_LookupErrorIsWrapped(t *testing.T) {
	lookupErr := errors.New("connection lost")
	exec := &fakeSequenceExecutor{row: fakeSequenceRow{err: lookupErr}}
	table := Table{PGName: "orders"}
	col := Column{PGName: "id", Extra: "auto_increment"}

	err := resetAttachedSequence(context.Background(), exec, "app", table, col)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, lookupErr) {
		t.Fatalf("error should wrap the lookup failure, got: %v", err)
	}
	if len(exec.execSQLs) != 0 {
		t.Fatalf("no setval should run when the lookup fails, got: %v", exec.execSQLs)
	}
}
