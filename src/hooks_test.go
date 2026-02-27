package main

import (
	"reflect"
	"testing"
)

func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			"single statement",
			"SELECT 1",
			[]string{"SELECT 1"},
		},
		{
			"two statements",
			"SELECT 1; SELECT 2;",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"trailing without semicolon",
			"SELECT 1; SELECT 2",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"empty statements skipped",
			"SELECT 1;; ;SELECT 2;",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"semicolon inside quotes",
			"SELECT 'hello;world'; SELECT 2",
			[]string{"SELECT 'hello;world'", "SELECT 2"},
		},
		{
			"escaped quotes",
			"SELECT 'it''s'; SELECT 2",
			[]string{"SELECT 'it''s'", "SELECT 2"},
		},
		{
			"whitespace trimmed",
			"  SELECT 1  ;  SELECT 2  ;  ",
			[]string{"SELECT 1", "SELECT 2"},
		},
		{
			"empty input",
			"",
			nil,
		},
		{
			"only whitespace",
			"   \n\t  ",
			nil,
		},
		{
			"multiline SQL",
			"DELETE FROM app.users\nWHERE id = 1;\nDELETE FROM app.posts\nWHERE user_id = 1;",
			[]string{"DELETE FROM app.users\nWHERE id = 1", "DELETE FROM app.posts\nWHERE user_id = 1"},
		},
		{
			"comments preserved in statements",
			"-- cleanup\nDELETE FROM t; SELECT 1",
			[]string{"-- cleanup\nDELETE FROM t", "SELECT 1"},
		},
		{
			"dollar-quoted function body",
			"CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; PERFORM 2; END; $$ LANGUAGE plpgsql; SELECT 1;",
			[]string{"CREATE FUNCTION f() RETURNS void AS $$ BEGIN PERFORM 1; PERFORM 2; END; $$ LANGUAGE plpgsql", "SELECT 1"},
		},
		{
			"tagged dollar-quoted body",
			"DO $fn$ BEGIN RAISE NOTICE 'x;y'; END; $fn$; SELECT 2;",
			[]string{"DO $fn$ BEGIN RAISE NOTICE 'x;y'; END; $fn$", "SELECT 2"},
		},
		{
			"block comment with semicolon",
			"/* comment; still comment */ SELECT 1; SELECT 2;",
			[]string{"/* comment; still comment */ SELECT 1", "SELECT 2"},
		},
		{
			"nested block comment with semicolon",
			"/* outer; /* inner; */ done; */ SELECT 1; SELECT 2;",
			[]string{"/* outer; /* inner; */ done; */ SELECT 1", "SELECT 2"},
		},
		{
			"double-quoted identifier with semicolon",
			`SELECT "a;b" FROM t; SELECT 2;`,
			[]string{`SELECT "a;b" FROM t`, "SELECT 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitStatements(tt.sql)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitStatements(%q) =\n  %v\nwant:\n  %v", tt.sql, got, tt.want)
			}
		})
	}
}
