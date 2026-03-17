package main

import (
	"strings"
	"testing"
)

func TestValidateGeneratedIdentifiers_TableCollision(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Users", PGName: "users"},
			{SourceName: "users", PGName: "users"},
		},
	}

	err := validateGeneratedIdentifiers(schema, &MigrationConfig{}, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), `schema relation names: final name "users"`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), `table "Users"`) || !strings.Contains(err.Error(), `table "users"`) {
		t.Fatalf("error should mention both conflicting tables: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_ColumnCollisionWithinTable(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Accounts",
				PGName:     "accounts",
				Columns: []Column{
					{SourceName: "Email", PGName: "email"},
					{SourceName: "email", PGName: "email"},
				},
			},
		},
	}

	err := validateGeneratedIdentifiers(schema, &MigrationConfig{}, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), `column names on table "accounts": final name "email"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_AllowsSameColumnNameAcrossTables(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Users",
				PGName:     "users",
				Columns: []Column{
					{SourceName: "UpdatedAt", PGName: "updated_at"},
				},
			},
			{
				SourceName: "Orders",
				PGName:     "orders",
				Columns: []Column{
					{SourceName: "UpdatedAt", PGName: "updated_at"},
				},
			},
		},
	}

	if err := validateGeneratedIdentifiers(schema, &MigrationConfig{}, defaultTypeMappingConfig()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_GeneratedIndexCollisionAfterTruncation(t *testing.T) {
	longName := "very_long_index_name_that_requires_truncation_for_postgres_identifiers_and_still_collides"
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Orders",
				PGName:     "orders",
				Indexes: []Index{
					{SourceName: "IDX_A", Name: longName, Type: "BTREE", Columns: []string{"id"}},
					{SourceName: "IDX_B", Name: longName, Type: "BTREE", Columns: []string{"created_at"}},
				},
			},
		},
	}

	cfg := &MigrationConfig{}
	err := validateGeneratedIdentifiers(schema, cfg, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected collision error")
	}

	finalName := generatedIndexName(schema.Tables[0], schema.Tables[0].Indexes[0])
	if !strings.Contains(err.Error(), `schema relation names: final name "`+finalName+`"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_ReportsGeneratedSequenceAndTriggerCollisions(t *testing.T) {
	longColName := "very_long_updated_at_column_name_that_requires_truncation_for_identifier_checks"
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Events",
				PGName:     "events",
				Columns: []Column{
					{SourceName: "UpdatedAtA", PGName: longColName, Extra: "auto_increment on update CURRENT_TIMESTAMP"},
					{SourceName: "UpdatedAtB", PGName: longColName, Extra: "auto_increment on update CURRENT_TIMESTAMP"},
				},
			},
		},
	}

	cfg := &MigrationConfig{ReplicateOnUpdateCurrentTimestamp: true}
	err := validateGeneratedIdentifiers(schema, cfg, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected collision error")
	}
	seqName := generatedSequenceName(schema.Tables[0], schema.Tables[0].Columns[0])
	trigName := generatedTriggerName(schema.Tables[0], schema.Tables[0].Columns[0])
	if !strings.Contains(err.Error(), `schema relation names: final name "`+seqName+`"`) {
		t.Fatalf("sequence collision missing from error: %v", err)
	}
	if !strings.Contains(err.Error(), `trigger names on table "events": final name "`+trigName+`"`) {
		t.Fatalf("trigger collision missing from error: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_AllowsTriggerFunctionReuseForSameTargetColumn(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Users",
				PGName:     "users",
				Columns: []Column{
					{SourceName: "UpdatedAt", PGName: "updated_at", Extra: "on update CURRENT_TIMESTAMP"},
				},
			},
			{
				SourceName: "Orders",
				PGName:     "orders",
				Columns: []Column{
					{SourceName: "UpdatedAt", PGName: "updated_at", Extra: "on update CURRENT_TIMESTAMP"},
				},
			},
		},
	}

	cfg := &MigrationConfig{ReplicateOnUpdateCurrentTimestamp: true}
	if err := validateGeneratedIdentifiers(schema, cfg, defaultTypeMappingConfig()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_EnumTypeCollidesWithTableRowType(t *testing.T) {
	enumValues := []string{"draft", "published"}
	typeName := pgEnumTypeName(enumValues)
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Articles", PGName: typeName},
			{
				SourceName: "Posts",
				PGName:     "posts",
				Columns: []Column{
					{SourceName: "status", PGName: "status", DataType: "enum", ColumnType: "enum('draft','published')"},
				},
			},
		},
	}

	cfg := &MigrationConfig{}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"

	err := validateGeneratedIdentifiers(schema, cfg, tm)
	if err == nil {
		t.Fatal("expected collision error")
	}
	if !strings.Contains(err.Error(), `schema type names: final name "`+typeName+`"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateGeneratedIdentifiers_AllowsNativeEnumTypeReuseForIdenticalValues(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Articles",
				PGName:     "articles",
				Columns: []Column{
					{SourceName: "status", PGName: "status", DataType: "enum", ColumnType: "enum('draft','published')"},
				},
			},
			{
				SourceName: "Posts",
				PGName:     "posts",
				Columns: []Column{
					{SourceName: "status", PGName: "status", DataType: "enum", ColumnType: "enum('published','draft')"},
				},
			},
		},
	}

	cfg := &MigrationConfig{}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"

	if err := validateGeneratedIdentifiers(schema, cfg, tm); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
