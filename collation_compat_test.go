package main

import (
	"strings"
	"testing"
)

func TestCollectCollationWarnings_EmptySchema(t *testing.T) {
	schema := &Schema{}
	warnings := collectCollationWarnings(schema, defaultTypeMappingConfig())
	if len(warnings) != 0 {
		t.Errorf("expected 0 warnings for empty schema, got %d: %v", len(warnings), warnings)
	}
}

func TestCollectCollationWarnings_CIWarnings(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "users",
				Columns: []Column{
					{PGName: "name", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
					{PGName: "email", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
					{PGName: "id", Charset: "", Collation: ""},
				},
			},
		},
	}

	warnings := collectCollationWarnings(schema, defaultTypeMappingConfig())

	// Should have: charset summary, collation summary, _ci warning
	var hasCIWarning bool
	for _, w := range warnings {
		if strings.Contains(w, "utf8mb4_general_ci") && strings.Contains(w, "case-insensitive") {
			hasCIWarning = true
			// Should report 2 columns
			if !strings.Contains(w, "2 column(s)") {
				t.Errorf("expected 2 columns in CI warning, got: %s", w)
			}
		}
	}
	if !hasCIWarning {
		t.Errorf("expected CI collation warning, got: %v", warnings)
	}
}

func TestCollectCollationWarnings_CIDeduplicated(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "t1",
				Columns: []Column{
					{PGName: "a", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
				},
			},
			{
				PGName: "t2",
				Columns: []Column{
					{PGName: "b", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
				},
			},
		},
	}

	warnings := collectCollationWarnings(schema, defaultTypeMappingConfig())

	// Should have exactly one _ci warning for utf8mb4_general_ci, not two
	ciCount := 0
	for _, w := range warnings {
		if strings.Contains(w, "case-insensitive") {
			ciCount++
		}
	}
	if ciCount != 1 {
		t.Errorf("expected 1 deduplicated CI warning, got %d: %v", ciCount, warnings)
	}
}

func TestCollectCollationWarnings_MappedCISuppressed(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "users",
				Columns: []Column{
					{PGName: "name", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
				},
			},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.CollationMap = map[string]string{
		"utf8mb4_general_ci": "und-x-icu",
	}

	warnings := collectCollationWarnings(schema, tm)

	for _, w := range warnings {
		if strings.Contains(w, "case-insensitive") {
			t.Errorf("expected CI warning to be suppressed when mapped, got: %s", w)
		}
	}
}

func TestCollectCollationWarnings_UniqueIndexCI(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "users",
				Columns: []Column{
					{PGName: "email", Charset: "utf8mb4", Collation: "utf8mb4_unicode_ci"},
				},
				Indexes: []Index{
					{Name: "idx_email", Columns: []string{"email"}, Unique: true},
				},
			},
		},
	}

	warnings := collectCollationWarnings(schema, defaultTypeMappingConfig())

	var hasUniqueWarning bool
	for _, w := range warnings {
		if strings.Contains(w, "unique index/PK") && strings.Contains(w, "users.email") {
			hasUniqueWarning = true
		}
	}
	if !hasUniqueWarning {
		t.Errorf("expected unique index CI warning, got: %v", warnings)
	}
}

func TestCollectCollationWarnings_PKColumnCI(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "tags",
				PrimaryKey: &Index{
					Columns: []string{"slug"},
					Unique:  true,
				},
				Columns: []Column{
					{PGName: "slug", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
				},
			},
		},
	}

	warnings := collectCollationWarnings(schema, defaultTypeMappingConfig())

	var hasPKWarning bool
	for _, w := range warnings {
		if strings.Contains(w, "unique index/PK") && strings.Contains(w, "tags.slug") {
			hasPKWarning = true
		}
	}
	if !hasPKWarning {
		t.Errorf("expected PK CI warning, got: %v", warnings)
	}
}

func TestPgCollationClause_ModeNone(t *testing.T) {
	col := Column{Collation: "utf8mb4_bin"}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "none"

	got := pgCollationClause(col, tm)
	if got != "" {
		t.Errorf("expected empty for mode=none, got %q", got)
	}
}

func TestPgCollationClause_AutoBin(t *testing.T) {
	col := Column{Collation: "utf8mb4_bin"}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"

	got := pgCollationClause(col, tm)
	if got != `COLLATE "C"` {
		t.Errorf("expected COLLATE \"C\" for _bin, got %q", got)
	}
}

func TestPgCollationClause_AutoCINoMap(t *testing.T) {
	col := Column{Collation: "utf8mb4_general_ci"}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"

	got := pgCollationClause(col, tm)
	if got != "" {
		t.Errorf("expected empty for _ci without map, got %q", got)
	}
}

func TestPgCollationClause_AutoMapped(t *testing.T) {
	col := Column{Collation: "utf8mb4_general_ci"}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"
	tm.CollationMap = map[string]string{
		"utf8mb4_general_ci": "und-x-icu",
	}

	got := pgCollationClause(col, tm)
	if got != `COLLATE "und-x-icu"` {
		t.Errorf("expected COLLATE \"und-x-icu\", got %q", got)
	}
}

func TestPgCollationClause_EmptyCollation(t *testing.T) {
	col := Column{Collation: ""}
	tm := defaultTypeMappingConfig()
	tm.CollationMode = "auto"

	got := pgCollationClause(col, tm)
	if got != "" {
		t.Errorf("expected empty for no collation, got %q", got)
	}
}

func TestIsCICollation(t *testing.T) {
	tests := []struct {
		collation string
		want      bool
	}{
		{"utf8mb4_general_ci", true},
		{"utf8mb4_unicode_ci", true},
		{"UTF8MB4_GENERAL_CI", true},
		{"utf8mb4_bin", false},
		{"latin1_swedish_ci", true},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.collation, func(t *testing.T) {
			got := isCICollation(tt.collation)
			if got != tt.want {
				t.Errorf("isCICollation(%q) = %v, want %v", tt.collation, got, tt.want)
			}
		})
	}
}

func TestPgTypeForCollation(t *testing.T) {
	tests := []struct {
		name    string
		col     Column
		pgType  string
		typeMap TypeMappingConfig
		want    string
	}{
		{
			name:   "ci+enabled→citext",
			col:    Column{Collation: "utf8mb4_general_ci"},
			pgType: "text",
			typeMap: TypeMappingConfig{CIAsCitext: true},
			want:   "citext",
		},
		{
			name:   "ci+enabled+varchar→citext",
			col:    Column{Collation: "utf8mb4_general_ci"},
			pgType: "varchar(255)",
			typeMap: TypeMappingConfig{CIAsCitext: true},
			want:   "citext",
		},
		{
			name:   "ci+disabled→unchanged",
			col:    Column{Collation: "utf8mb4_general_ci"},
			pgType: "text",
			typeMap: TypeMappingConfig{CIAsCitext: false},
			want:   "text",
		},
		{
			name:   "ci+collation_map→unchanged (map wins)",
			col:    Column{Collation: "utf8mb4_general_ci"},
			pgType: "text",
			typeMap: TypeMappingConfig{
				CIAsCitext:   true,
				CollationMap: map[string]string{"utf8mb4_general_ci": "und-x-icu"},
			},
			want: "text",
		},
		{
			name:   "non-ci→unchanged",
			col:    Column{Collation: "utf8mb4_bin"},
			pgType: "text",
			typeMap: TypeMappingConfig{CIAsCitext: true},
			want:   "text",
		},
		{
			name:   "non-text→unchanged",
			col:    Column{Collation: "utf8mb4_general_ci"},
			pgType: "integer",
			typeMap: TypeMappingConfig{CIAsCitext: true},
			want:   "integer",
		},
		{
			name:   "empty collation→unchanged",
			col:    Column{Collation: ""},
			pgType: "text",
			typeMap: TypeMappingConfig{CIAsCitext: true},
			want:   "text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pgTypeForCollation(tt.col, tt.pgType, tt.typeMap)
			if got != tt.want {
				t.Errorf("pgTypeForCollation() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollectCollationWarnings_CIAsCitextSuppresses(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "users",
				Columns: []Column{
					{PGName: "name", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
					{PGName: "email", Charset: "utf8mb4", Collation: "utf8mb4_general_ci"},
				},
				Indexes: []Index{
					{Name: "idx_email", Columns: []string{"email"}, Unique: true},
				},
			},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.CIAsCitext = true

	warnings := collectCollationWarnings(schema, tm)

	for _, w := range warnings {
		if strings.Contains(w, "case-insensitive") {
			t.Errorf("expected CI column count warning to be suppressed with ci_as_citext, got: %s", w)
		}
		if strings.Contains(w, "unique index/PK") {
			t.Errorf("expected unique index CI warning to be suppressed with ci_as_citext, got: %s", w)
		}
	}
}

func TestCollectCollationWarnings_CIAsCitextPartialSuppression(t *testing.T) {
	// When ci_as_citext is true but a specific collation has a collation_map entry,
	// the map entry takes precedence (handled), so warnings for that collation are suppressed.
	// A different _ci collation without a map entry is also suppressed by ci_as_citext.
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "t1",
				Columns: []Column{
					{PGName: "a", Collation: "utf8mb4_general_ci"},
					{PGName: "b", Collation: "utf8mb4_unicode_ci"},
				},
			},
		},
	}

	tm := defaultTypeMappingConfig()
	tm.CIAsCitext = true
	tm.CollationMap = map[string]string{
		"utf8mb4_general_ci": "und-x-icu",
	}

	warnings := collectCollationWarnings(schema, tm)

	for _, w := range warnings {
		if strings.Contains(w, "case-insensitive") {
			t.Errorf("expected all CI warnings suppressed, got: %s", w)
		}
	}
}

func TestIsTextLikePGType(t *testing.T) {
	tests := []struct {
		pgType string
		want   bool
	}{
		{"text", true},
		{"varchar(255)", true},
		{"varchar(50)", true},
		{"char(1)", true},
		{"integer", false},
		{"bigint", false},
		{"bytea", false},
		{"boolean", false},
		{"json", false},
		{"jsonb", false},
		{"numeric(10,2)", false},
		{"text[]", false},
	}
	for _, tt := range tests {
		t.Run(tt.pgType, func(t *testing.T) {
			got := isTextLikePGType(tt.pgType)
			if got != tt.want {
				t.Errorf("isTextLikePGType(%q) = %v, want %v", tt.pgType, got, tt.want)
			}
		})
	}
}
