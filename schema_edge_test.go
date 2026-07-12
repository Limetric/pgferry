package main

import (
	"testing"
	"unicode/utf8"
)

func TestToSnakeCase_NonASCIIPreserved(t *testing.T) {
	cases := map[string]string{
		"preço":       "preço",
		"café":        "café",
		"名前":          "名前",
		"precioTotal": "precio_total",
		"preçoTotal":  "preço_total",
		"ÉtatCivil":   "état_civil",
	}
	for in, want := range cases {
		got := toSnakeCase(in)
		if got != want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", in, got, want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("toSnakeCase(%q) produced invalid UTF-8: %v", in, []byte(got))
		}
	}
}

func TestToSnakeCase_ConsecutiveUnderscores(t *testing.T) {
	// Input already containing underscores should not deduplicate them
	got := toSnakeCase("name__Case")
	if got != "name__case" {
		t.Fatalf("toSnakeCase('name__Case') = %q, want 'name__case'", got)
	}
}

func TestToSnakeCase_LeadingUnderscore(t *testing.T) {
	got := toSnakeCase("_privateName")
	if got != "_private_name" {
		t.Fatalf("toSnakeCase('_privateName') = %q, want '_private_name'", got)
	}
}

func TestToSnakeCase_LeadingDigit(t *testing.T) {
	got := toSnakeCase("2ndColumn")
	if got != "2nd_column" {
		t.Fatalf("toSnakeCase('2ndColumn') = %q, want '2nd_column'", got)
	}
}

func TestToSnakeCase_AllUppercase(t *testing.T) {
	got := toSnakeCase("URL")
	if got != "url" {
		t.Fatalf("toSnakeCase('URL') = %q, want 'url'", got)
	}
}

func TestToSnakeCase_AcronymBeforeLowercase(t *testing.T) {
	got := toSnakeCase("HTMLParser")
	if got != "html_parser" {
		t.Fatalf("toSnakeCase('HTMLParser') = %q, want 'html_parser'", got)
	}
}

func TestToSnakeCase_DigitToUpperBoundary(t *testing.T) {
	got := toSnakeCase("name2ASCII")
	if got != "name2_ascii" {
		t.Fatalf("toSnakeCase('name2ASCII') = %q, want 'name2_ascii'", got)
	}
}

func TestToSnakeCase_EmptyString(t *testing.T) {
	got := toSnakeCase("")
	if got != "" {
		t.Fatalf("toSnakeCase('') = %q, want empty", got)
	}
}

func TestToSnakeCase_SingleChar(t *testing.T) {
	got := toSnakeCase("A")
	if got != "a" {
		t.Fatalf("toSnakeCase('A') = %q, want 'a'", got)
	}
}

func TestToSnakeCase_AlreadySnakeCase(t *testing.T) {
	got := toSnakeCase("already_snake")
	if got != "already_snake" {
		t.Fatalf("toSnakeCase('already_snake') = %q, want 'already_snake'", got)
	}
}

func TestToSnakeCase_MixedWithNumbers(t *testing.T) {
	got := toSnakeCase("getHTTP2Response")
	if got != "get_http2_response" {
		t.Fatalf("toSnakeCase('getHTTP2Response') = %q, want 'get_http2_response'", got)
	}
}

func TestPgIdent_EmbeddedDoubleQuotes(t *testing.T) {
	got := pgIdent(`col"name`)
	if got != `"col""name"` {
		t.Fatalf("pgIdent with embedded quote = %q, want %q", got, `"col""name"`)
	}
}

func TestPgIdent_MultipleQuotes(t *testing.T) {
	got := pgIdent(`a""b`)
	if got != `"a""""b"` {
		t.Fatalf("pgIdent('a\"\"b') = %q, want %q", got, `"a""""b"`)
	}
}

func TestPgIdent_Whitespace(t *testing.T) {
	got := pgIdent("has space")
	if got != `"has space"` {
		t.Fatalf("pgIdent('has space') = %q, want %q", got, `"has space"`)
	}
}

func TestPgIdent_SpecialCharacters(t *testing.T) {
	got := pgIdent("col-name.with@special")
	if got != `"col-name.with@special"` {
		t.Fatalf("pgIdent with special chars = %q", got)
	}
}

func TestPgIdent_Tab(t *testing.T) {
	got := pgIdent("has\ttab")
	if got != "\"has\ttab\"" {
		t.Fatalf("pgIdent with tab = %q", got)
	}
}
