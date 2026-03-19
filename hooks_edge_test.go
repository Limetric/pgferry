package main

import (
	"reflect"
	"testing"
)

func TestSplitStatements_UnclosedDollarQuote(t *testing.T) {
	// Unclosed dollar-quoted block at EOF should be treated as a single statement
	got := splitStatements("$tag$ unclosed content")
	want := []string{"$tag$ unclosed content"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(unclosed dollar quote) = %v, want %v", got, want)
	}
}

func TestSplitStatements_UnclosedBlockComment(t *testing.T) {
	// Unclosed block comment at EOF should be treated as a single statement
	got := splitStatements("/* unclosed comment SELECT 1;")
	want := []string{"/* unclosed comment SELECT 1;"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(unclosed block comment) = %v, want %v", got, want)
	}
}

func TestSplitStatements_AdjacentDollarQuotedBlocks(t *testing.T) {
	got := splitStatements("DO $$stmt1$$; DO $$stmt2$$;")
	want := []string{"DO $$stmt1$$", "DO $$stmt2$$"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(adjacent $$) = %v, want %v", got, want)
	}
}

func TestSplitStatements_NumericDollarTag(t *testing.T) {
	// $123$ should NOT be recognized as a dollar-quote tag (tag must start with letter or underscore)
	got := splitStatements("SELECT $123$; SELECT 2")
	// $123$ is not a valid dollar-tag, so semicolon splits normally
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(got), got)
	}
}

func TestSplitStatements_LineCommentAtEOFNoNewline(t *testing.T) {
	got := splitStatements("SELECT 1 -- trailing comment")
	want := []string{"SELECT 1 -- trailing comment"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(line comment EOF) = %v, want %v", got, want)
	}
}

func TestSplitStatements_EmptyBlockComment(t *testing.T) {
	got := splitStatements("/**/SELECT 1; SELECT 2")
	want := []string{"/**/SELECT 1", "SELECT 2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(empty block comment) = %v, want %v", got, want)
	}
}

func TestSplitStatements_EscapedDoubleQuotes(t *testing.T) {
	got := splitStatements(`SELECT "col""with""quotes" FROM t; SELECT 2`)
	want := []string{`SELECT "col""with""quotes" FROM t`, "SELECT 2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(escaped double quotes) = %v, want %v", got, want)
	}
}

func TestSplitStatements_NestedDollarQuoteWithSemicolon(t *testing.T) {
	// PostgreSQL doesn't actually support nested dollar quoting — $inner$ inside
	// $outer$ is literal text in the outer block. This tests the parser's behavior
	// on pathological input: it should still correctly find the outer closing tag
	// and split statements at the top-level semicolons.
	sql := "DO $outer$ BEGIN DO $inner$SELECT 1;$inner$; END; $outer$; SELECT 2;"
	got := splitStatements(sql)
	if len(got) != 2 {
		t.Fatalf("expected 2 statements, got %d: %v", len(got), got)
	}
	if got[1] != "SELECT 2" {
		t.Errorf("second statement = %q, want 'SELECT 2'", got[1])
	}
}

func TestSplitStatements_OnlySemicolons(t *testing.T) {
	got := splitStatements(";;;")
	if len(got) != 0 {
		t.Errorf("expected 0 statements for ';;;', got %v", got)
	}
}

func TestSplitStatements_DollarQuoteInSingleQuote(t *testing.T) {
	// Dollar signs inside a single-quoted string should not trigger dollar quoting
	got := splitStatements("SELECT '$$not a tag$$'; SELECT 2")
	want := []string{"SELECT '$$not a tag$$'", "SELECT 2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitStatements(dollar in single quote) = %v, want %v", got, want)
	}
}

func TestParseDollarTag_ValidTags(t *testing.T) {
	tests := []struct {
		sql  string
		i    int
		tag  string
		ok   bool
	}{
		{"$$body$$", 0, "$$", true},
		{"$fn$body$fn$", 0, "$fn$", true},
		{"$_tag$body$_tag$", 0, "$_tag$", true},
		{"$tag123$body$tag123$", 0, "$tag123$", true},
	}
	for _, tt := range tests {
		tag, ok := parseDollarTag(tt.sql, tt.i)
		if ok != tt.ok || tag != tt.tag {
			t.Errorf("parseDollarTag(%q, %d) = (%q, %v), want (%q, %v)", tt.sql, tt.i, tag, ok, tt.tag, tt.ok)
		}
	}
}

func TestParseDollarTag_InvalidTags(t *testing.T) {
	tests := []struct {
		sql string
		i   int
	}{
		{"$123$body", 0},      // starts with digit
		{"$", 0},              // lone dollar
		{"abc", 0},            // no dollar
		{"$-tag$body", 0},     // hyphen not allowed
	}
	for _, tt := range tests {
		_, ok := parseDollarTag(tt.sql, tt.i)
		if ok {
			t.Errorf("parseDollarTag(%q, %d) should return false", tt.sql, tt.i)
		}
	}
}
