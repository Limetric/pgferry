package main

import (
	"database/sql"
	"testing"
)

func TestExtractSQLiteCheckConstraints(t *testing.T) {
	tests := []struct {
		name  string
		ddl   string
		want  []sqliteCheckConstraint
		count int
	}{
		{
			name: "column-level anonymous check",
			ddl:  `CREATE TABLE orders (id INTEGER PRIMARY KEY, status TEXT CHECK (status IN ('new','paid')))`,
			want: []sqliteCheckConstraint{{Name: "", Expr: `status IN ('new','paid')`}},
		},
		{
			name: "named table-level check",
			ddl:  `CREATE TABLE orders (id INTEGER, total REAL, CONSTRAINT total_positive CHECK (total > 0))`,
			want: []sqliteCheckConstraint{{Name: "total_positive", Expr: "total > 0"}},
		},
		{
			name: "multiple checks",
			ddl: `CREATE TABLE t (
				a INTEGER CHECK (a > 0),
				b INTEGER,
				CONSTRAINT b_range CHECK (b BETWEEN 1 AND 10)
			)`,
			want: []sqliteCheckConstraint{
				{Name: "", Expr: "a > 0"},
				{Name: "b_range", Expr: "b BETWEEN 1 AND 10"},
			},
		},
		{
			name: "nested parentheses",
			ddl:  `CREATE TABLE t (a INT, b INT, CHECK ((a + b) > (a * 2)))`,
			want: []sqliteCheckConstraint{{Name: "", Expr: "(a + b) > (a * 2)"}},
		},
		{
			name: "CHECK inside a string literal is not a constraint",
			ddl:  `CREATE TABLE t (note TEXT DEFAULT 'CHECK (this is prose)')`,
			want: nil,
		},
		{
			name: "CHECK inside a quoted identifier is not a constraint",
			ddl:  `CREATE TABLE t ("CHECK" TEXT, [check] TEXT)`,
			want: nil,
		},
		{
			name: "check with a string containing a close paren",
			ddl:  `CREATE TABLE t (s TEXT CHECK (s <> ')'))`,
			want: []sqliteCheckConstraint{{Name: "", Expr: `s <> ')'`}},
		},
		{
			name: "quoted constraint name",
			ddl:  `CREATE TABLE t (a INT, CONSTRAINT "my check" CHECK (a > 0))`,
			want: []sqliteCheckConstraint{{Name: "my check", Expr: "a > 0"}},
		},
		{
			name: "no checks",
			ddl:  `CREATE TABLE t (a INTEGER PRIMARY KEY, b TEXT NOT NULL)`,
			want: nil,
		},
		{
			name: "named unique does not leak its name onto a later check",
			ddl:  `CREATE TABLE t (a INT, b INT, CONSTRAINT a_uniq UNIQUE (a), CHECK (b > 0))`,
			want: []sqliteCheckConstraint{{Name: "", Expr: "b > 0"}},
		},
		{
			name: "comments are skipped",
			ddl: `CREATE TABLE t (
				-- CHECK (commented out)
				a INT /* CHECK (also not real) */,
				CHECK (a > 0)
			)`,
			want: []sqliteCheckConstraint{{Name: "", Expr: "a > 0"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSQLiteCheckConstraints(tt.ddl)
			if len(got) != len(tt.want) {
				t.Fatalf("extractSQLiteCheckConstraints() = %+v, want %+v", got, tt.want)
			}
			for i := range tt.want {
				if got[i].Name != tt.want[i].Name || got[i].Expr != tt.want[i].Expr {
					t.Errorf("check %d = {Name:%q Expr:%q}, want {Name:%q Expr:%q}",
						i, got[i].Name, got[i].Expr, tt.want[i].Name, tt.want[i].Expr)
				}
			}
		})
	}
}

// TestSQLiteSemanticWarnings_AgainstRealSQLite drives the introspector against a
// real SQLite database, so the DDL that sqlite_master actually stores (which may
// differ from what was typed) is what gets parsed.
func TestSQLiteSemanticWarnings_AgainstRealSQLite(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	ddl := []string{
		`CREATE TABLE orders (
			id INTEGER PRIMARY KEY,
			status TEXT CHECK (status IN ('new','paid','shipped')),
			total REAL,
			CONSTRAINT total_positive CHECK (total > 0)
		)`,
		`CREATE TABLE plain (id INTEGER PRIMARY KEY, name TEXT NOT NULL)`,
	}
	for _, stmt := range ddl {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}

	src := &sqliteSourceDB{}
	warnings, err := src.IntrospectSchemaSemanticWarnings(db, "")
	if err != nil {
		t.Fatalf("IntrospectSchemaSemanticWarnings: %v", err)
	}

	if len(warnings) != 2 {
		t.Fatalf("got %d warnings, want 2 (one per CHECK constraint on orders): %+v", len(warnings), warnings)
	}
	for _, w := range warnings {
		if w.Category != "constraints" {
			t.Errorf("warning category = %q, want constraints", w.Category)
		}
		if w.Disposition != "skipped" {
			t.Errorf("warning disposition = %q, want skipped", w.Disposition)
		}
		if got := schemaSemanticWarningTableName(w); got != "orders" {
			t.Errorf("warning table = %q, want orders (so plan filtering by table works)", got)
		}
	}

	// The introspector must be reachable through the generic interface, which is
	// what plan/migrate actually call.
	if _, ok := SourceDB(src).(sourceSchemaSemanticWarningIntrospector); !ok {
		t.Fatal("sqliteSourceDB does not satisfy sourceSchemaSemanticWarningIntrospector")
	}
	generic, err := introspectSourceSchemaSemanticWarnings(db, src, "")
	if err != nil {
		t.Fatalf("introspectSourceSchemaSemanticWarnings: %v", err)
	}
	if len(generic) != 2 {
		t.Fatalf("generic path returned %d warnings, want 2", len(generic))
	}
}
