package main

import (
	"strings"
	"testing"
)

// Regression tests for #255: values read from the source catalog (or from user
// config) must never reach generated DDL as unescaped SQL.

func TestMySQLMapDefault_BitRejectsNonBinaryLiteral(t *testing.T) {
	// A BIT default is interpolated into a B'...' literal rather than escaped, so
	// a quote in a source-controlled default would otherwise break out of the
	// literal and inject arbitrary SQL into the CREATE TABLE.
	malicious := []string{
		`b'0') /*`,
		`0') , x int default (pg_sleep(10)`,
		`b'01'; DROP TABLE users; --`,
		`'`,
		`nonsense`,
		`b'2'`,
	}
	for _, def := range malicious {
		col := Column{SourceName: "flags", DataType: "bit", ColumnType: "bit(8)", Default: strPtr(def)}
		got, err := mysqlMapDefault(col, "bit(8)", TypeMappingConfig{})
		if err == nil {
			t.Errorf("mysqlMapDefault(bit default %q) = %q, want an error", def, got)
		}
		if got != "" {
			t.Errorf("mysqlMapDefault(bit default %q) returned %q, want empty on error", def, got)
		}
	}
}

func TestMySQLMapDefault_BitAcceptsBinaryLiterals(t *testing.T) {
	tests := map[string]string{
		"b'0'":   "B'0'",
		"b'101'": "B'101'",
		"B'101'": "B'101'",
		"1":      "B'1'",
		"0":      "B'0'",
		"1010":   "B'1010'",
		// MySQL's b'' is a legal empty bit literal, and B'' is injection-safe.
		"b''": "B''",
	}
	for def, want := range tests {
		col := Column{SourceName: "flags", DataType: "bit", ColumnType: "bit(8)", Default: strPtr(def)}
		got, err := mysqlMapDefault(col, "bit(8)", TypeMappingConfig{})
		if err != nil {
			t.Errorf("mysqlMapDefault(bit default %q) error: %v", def, err)
			continue
		}
		if got != want {
			t.Errorf("mysqlMapDefault(bit default %q) = %q, want %q", def, got, want)
		}
	}
}

func TestMySQLMapDefault_CurrentTimestampPrecisionIsReRendered(t *testing.T) {
	// CURRENT_TIMESTAMP(n) is emitted as bare SQL, so the source text must never be
	// passed through verbatim.
	malicious := []string{
		`current_timestamp(6), CHECK (pg_sleep(10) IS NULL) --)`,
		`current_timestamp(6) , x int default (pg_sleep(10))`,
		`current_timestamp(abc)`,
		`current_timestamp(99)`,
		`current_timestamp(-1)`,
	}
	for _, def := range malicious {
		col := Column{SourceName: "created_at", DataType: "timestamp", ColumnType: "timestamp(6)", Default: strPtr(def)}
		got, err := mysqlMapDefault(col, "timestamp(6)", TypeMappingConfig{})
		if err == nil {
			t.Errorf("mysqlMapDefault(default %q) = %q, want an error", def, got)
		}
		if got != "" {
			t.Errorf("mysqlMapDefault(default %q) returned %q, want empty on error", def, got)
		}
	}

	valid := map[string]string{
		"current_timestamp(6)": "CURRENT_TIMESTAMP(6)",
		"CURRENT_TIMESTAMP(3)": "CURRENT_TIMESTAMP(3)",
		"current_timestamp(0)": "CURRENT_TIMESTAMP(0)",
		"current_timestamp()":  "CURRENT_TIMESTAMP",
		"current_timestamp":    "CURRENT_TIMESTAMP",
	}
	for def, want := range valid {
		col := Column{SourceName: "created_at", DataType: "timestamp", ColumnType: "timestamp(6)", Default: strPtr(def)}
		got, err := mysqlMapDefault(col, "timestamp(6)", TypeMappingConfig{})
		if err != nil {
			t.Errorf("mysqlMapDefault(default %q) error: %v", def, err)
			continue
		}
		if got != want {
			t.Errorf("mysqlMapDefault(default %q) = %q, want %q", def, got, want)
		}
	}
}

func TestReferentialAction_RejectsUnknownActions(t *testing.T) {
	valid := map[string]string{
		"CASCADE":     "CASCADE",
		"cascade":     "CASCADE",
		"SET NULL":    "SET NULL",
		"set  null":   "SET NULL",
		"NO ACTION":   "NO ACTION",
		"RESTRICT":    "RESTRICT",
		"SET DEFAULT": "SET DEFAULT",
		"":            "NO ACTION",
	}
	for in, want := range valid {
		got, err := referentialAction(in)
		if err != nil {
			t.Errorf("referentialAction(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("referentialAction(%q) = %q, want %q", in, got, want)
		}
	}

	invalid := []string{
		"CASCADE, ADD CONSTRAINT evil CHECK (pg_sleep(10) IS NULL)",
		"NO ACTION; DROP TABLE users",
		"SET NULL --",
		"BOGUS",
	}
	for _, in := range invalid {
		if got, err := referentialAction(in); err == nil {
			t.Errorf("referentialAction(%q) = %q, want an error", in, got)
		}
	}
}

func TestPGCollationClause_QuotesMappedCollation(t *testing.T) {
	typeMap := TypeMappingConfig{
		CollationMode: "auto",
		CollationMap: map[string]string{
			"utf8mb4_general_ci": `evil" , x int default pg_sleep(10)) --`,
			"utf8mb4_polish_ci":  "pl-PL-x-icu",
		},
	}

	got := pgCollationClause(Column{Collation: "utf8mb4_general_ci"}, typeMap)
	if strings.Contains(got, `evil" ,`) {
		t.Errorf("pgCollationClause did not escape the embedded quote: %s", got)
	}
	if want := `COLLATE "evil"" , x int default pg_sleep(10)) --"`; got != want {
		t.Errorf("pgCollationClause = %s, want %s", got, want)
	}

	if got, want := pgCollationClause(Column{Collation: "utf8mb4_polish_ci"}, typeMap), `COLLATE "pl-PL-x-icu"`; got != want {
		t.Errorf("pgCollationClause = %s, want %s", got, want)
	}
}
