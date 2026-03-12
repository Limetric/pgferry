package main

import (
	"strings"
	"testing"
)

func TestResetSequenceStatements_QuotesSchemaAndSequenceRegclass(t *testing.T) {
	table := Table{PGName: "events"}
	col := Column{PGName: "id", Extra: "auto_increment"}

	stmts := resetSequenceStatements("order", table, col)
	if len(stmts) != 3 {
		t.Fatalf("statement count = %d, want 3", len(stmts))
	}

	if !strings.Contains(stmts[0], `CREATE SEQUENCE IF NOT EXISTS "order".events_id_seq`) {
		t.Fatalf("create sequence statement = %q", stmts[0])
	}
	if !strings.Contains(stmts[1], `SELECT setval('"order".events_id_seq'::regclass`) {
		t.Fatalf("setval statement = %q", stmts[1])
	}
	if !strings.Contains(stmts[2], `SET DEFAULT nextval('"order".events_id_seq'::regclass)`) {
		t.Fatalf("nextval statement = %q", stmts[2])
	}
}

func TestResetSequenceStatements_QuotesNonTrivialSequenceName(t *testing.T) {
	table := Table{PGName: "audit"}
	col := Column{PGName: "event-id", Extra: "auto_increment"}

	stmts := resetSequenceStatements("app", table, col)
	if !strings.Contains(stmts[0], `app."audit_event-id_seq"`) {
		t.Fatalf("create sequence statement = %q", stmts[0])
	}
	if !strings.Contains(stmts[1], `'app."audit_event-id_seq"'::regclass`) {
		t.Fatalf("setval statement = %q", stmts[1])
	}
	if !strings.Contains(stmts[2], `'app."audit_event-id_seq"'::regclass`) {
		t.Fatalf("nextval statement = %q", stmts[2])
	}
}

func TestResetSequenceStatements_QuotesReservedColumnName(t *testing.T) {
	table := Table{PGName: "audit"}
	col := Column{PGName: "collation", Extra: "auto_increment"}

	stmts := resetSequenceStatements("app", table, col)
	if !strings.Contains(stmts[1], `SELECT MAX("collation") FROM app.audit`) {
		t.Fatalf("setval statement = %q", stmts[1])
	}
	if !strings.Contains(stmts[2], `ALTER TABLE app.audit ALTER COLUMN "collation" SET DEFAULT`) {
		t.Fatalf("nextval statement = %q", stmts[2])
	}
}
