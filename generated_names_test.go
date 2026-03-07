package main

import "testing"

func TestGeneratedIndexNameFitsPostgresLimit(t *testing.T) {
	name := generatedIndexName(
		Table{PGName: "very_long_table_name_that_needs_truncation_for_postgres_identifiers"},
		Index{Name: "very_long_index_name_that_also_needs_truncation_for_postgres_identifiers"},
	)
	if len(name) > 63 {
		t.Fatalf("index name length = %d, want <= 63", len(name))
	}
}

func TestGeneratedForeignKeyNameFitsPostgresLimit(t *testing.T) {
	name := generatedForeignKeyName(ForeignKey{
		Name: "fk_very_long_child_table_name_to_very_long_parent_table_name_with_extra_suffixes_and_columns",
	})
	if len(name) > 63 {
		t.Fatalf("foreign key name length = %d, want <= 63", len(name))
	}
}

func TestGeneratedSequenceNameFitsPostgresLimit(t *testing.T) {
	name := generatedSequenceName(
		Table{PGName: "very_long_table_name_that_needs_truncation_for_postgres_identifiers"},
		Column{PGName: "very_long_column_name_that_needs_truncation"},
	)
	if len(name) > 63 {
		t.Fatalf("sequence name length = %d, want <= 63", len(name))
	}
}

func TestGeneratedTriggerFunctionNameFitsPostgresLimit(t *testing.T) {
	name := generatedTriggerFunctionName(Column{
		PGName: "very_long_timestamp_column_name_that_needs_truncation_for_trigger_function",
	})
	if len(name) > 63 {
		t.Fatalf("trigger function name length = %d, want <= 63", len(name))
	}
}

func TestGeneratedTriggerNameFitsPostgresLimit(t *testing.T) {
	name := generatedTriggerName(
		Table{PGName: "very_long_table_name_that_needs_truncation_for_postgres_identifiers"},
		Column{PGName: "very_long_timestamp_column_name_that_needs_truncation_for_trigger_name"},
	)
	if len(name) > 63 {
		t.Fatalf("trigger name length = %d, want <= 63", len(name))
	}
}

func TestGeneratedIdentifierTruncationIsDeterministic(t *testing.T) {
	input := "very_long_generated_identifier_name_that_needs_deterministic_truncation_with_hash_suffix"
	first := truncateGeneratedIdentifier(input)
	second := truncateGeneratedIdentifier(input)
	if first != second {
		t.Fatalf("expected deterministic truncation, got %q and %q", first, second)
	}
}
