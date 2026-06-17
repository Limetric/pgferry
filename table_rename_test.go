package main

import (
	"strings"
	"testing"
)

func TestApplyTableRenames_UpdatesTableNamesAndForeignKeyReferences(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Parents",
				PGName:     "parents",
				Columns:    []Column{{SourceName: "ID", PGName: "id"}},
			},
			{
				SourceName: "Children",
				PGName:     "children",
				Columns:    []Column{{SourceName: "ParentID", PGName: "parent_id"}},
				ForeignKeys: []ForeignKey{
					{
						Name:       "fk_children_parents",
						Columns:    []string{"parent_id"},
						RefTable:   "Parents",
						RefPGTable: "parents",
						RefColumns: []string{"id"},
					},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{
			"Parents":  "parent_records",
			"Children": "child_records",
		},
	}

	renamed, err := applyTableRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}

	if got := renamed.Tables[0].PGName; got != "parent_records" {
		t.Fatalf("parent table PGName = %q, want parent_records", got)
	}
	if got := renamed.Tables[1].PGName; got != "child_records" {
		t.Fatalf("child table PGName = %q, want child_records", got)
	}
	if got := renamed.Tables[1].ForeignKeys[0].RefPGTable; got != "parent_records" {
		t.Fatalf("child FK RefPGTable = %q, want parent_records", got)
	}
	if got := renamed.Tables[1].ForeignKeys[0].RefTable; got != "Parents" {
		t.Fatalf("child FK RefTable = %q, want source name Parents", got)
	}
	if got := schema.Tables[0].PGName; got != "parents" {
		t.Fatalf("original schema mutated: parent PGName = %q", got)
	}
	if got := schema.Tables[1].ForeignKeys[0].RefPGTable; got != "parents" {
		t.Fatalf("original schema mutated: child FK RefPGTable = %q", got)
	}
}

func TestApplyTableRenames_AutoResolvesPostgresTruncationCollision(t *testing.T) {
	first := "orders_with_a_source_name_that_is_long_enough_to_collide_after_postgresql_truncation_a"
	second := "orders_with_a_source_name_that_is_long_enough_to_collide_after_postgresql_truncation_b"
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "OrdersA",
				PGName:     first,
				Columns:    []Column{{SourceName: "ID", PGName: "id"}},
			},
			{
				SourceName: "OrdersB",
				PGName:     second,
				Columns:    []Column{{SourceName: "ID", PGName: "id"}},
				ForeignKeys: []ForeignKey{
					{
						Name:       "fk_orders_b_orders_a",
						Columns:    []string{"id"},
						RefTable:   "OrdersA",
						RefPGTable: first,
						RefColumns: []string{"id"},
					},
				},
			},
		},
	}
	cfg := &MigrationConfig{TableCollisionMode: "auto"}

	renamed, err := applyTableRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}

	firstTarget := renamed.Tables[0].PGName
	secondTarget := renamed.Tables[1].PGName
	if firstTarget == first {
		t.Fatalf("first colliding table was not renamed")
	}
	if secondTarget == second {
		t.Fatalf("second colliding table was not renamed")
	}
	if len(firstTarget) > postgresMaxIdentifierBytes {
		t.Fatalf("first auto-renamed table length = %d, want <= %d", len(firstTarget), postgresMaxIdentifierBytes)
	}
	if len(secondTarget) > postgresMaxIdentifierBytes {
		t.Fatalf("second auto-renamed table length = %d, want <= %d", len(secondTarget), postgresMaxIdentifierBytes)
	}
	if postgresIdentifierKey(firstTarget) == postgresIdentifierKey(secondTarget) {
		t.Fatalf("auto-renamed tables still collide: %q and %q", firstTarget, secondTarget)
	}
	if got := renamed.Tables[1].ForeignKeys[0].RefPGTable; got != firstTarget {
		t.Fatalf("child FK RefPGTable = %q, want %q", got, firstTarget)
	}
	if got := schema.Tables[0].PGName; got != first {
		t.Fatalf("original schema mutated: first PGName = %q", got)
	}
	if err := validateGeneratedIdentifiers(renamed, &MigrationConfig{}, defaultTypeMappingConfig()); err != nil {
		t.Fatalf("validateGeneratedIdentifiers() error: %v", err)
	}
}

func TestApplyTableRenames_AutoKeepsExplicitRename(t *testing.T) {
	explicitTarget := strings.Repeat("t", postgresMaxIdentifierBytes)
	collidingGeneratedName := explicitTarget + "_source_suffix"
	schema := &Schema{
		Tables: []Table{
			{SourceName: "ExplicitOrders", PGName: "explicit_orders"},
			{SourceName: "GeneratedOrders", PGName: collidingGeneratedName},
		},
	}
	cfg := &MigrationConfig{
		TableCollisionMode: "auto",
		TableRenames: map[string]string{
			"ExplicitOrders": explicitTarget,
		},
	}

	renamed, err := applyTableRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}

	if got := renamed.Tables[0].PGName; got != explicitTarget {
		t.Fatalf("explicitly renamed table PGName = %q, want %q", got, explicitTarget)
	}
	if got := renamed.Tables[1].PGName; got == collidingGeneratedName {
		t.Fatalf("non-explicit colliding table was not auto-renamed")
	}
	if postgresIdentifierKey(renamed.Tables[0].PGName) == postgresIdentifierKey(renamed.Tables[1].PGName) {
		t.Fatalf("auto-renamed table still collides with explicit rename: %q and %q", renamed.Tables[0].PGName, renamed.Tables[1].PGName)
	}
}

func TestApplyTableRenames_ExplicitRenameCollisionIsValidated(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Orders", PGName: "orders"},
			{SourceName: "Customers", PGName: "customer_records"},
		},
	}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{"Orders": "customer_records"},
	}

	renamed, err := applyTableRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}
	err = validateGeneratedIdentifiers(renamed, &MigrationConfig{}, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected explicit rename collision to be rejected by identifier validation")
	}
	if !strings.Contains(err.Error(), `schema relation names: final name "customer_records"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_AutoDoesNotGuessExactGeneratedNameCollision(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "TableA", PGName: "same_name"},
			{SourceName: "TableB", PGName: "same_name"},
		},
	}
	cfg := &MigrationConfig{TableCollisionMode: "auto"}

	renamed, err := applyTableRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}
	err = validateGeneratedIdentifiers(renamed, &MigrationConfig{}, defaultTypeMappingConfig())
	if err == nil {
		t.Fatal("expected exact generated-name collision to remain an error in auto mode")
	}
	if !strings.Contains(err.Error(), `schema relation names: final name "same_name"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_RejectsUnknownTable(t *testing.T) {
	schema := &Schema{Tables: []Table{{SourceName: "Orders", PGName: "orders"}}}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{"Customers": "customer_records"},
	}

	_, err := applyTableRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "table_renames entries did not match any source table in the migrated schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_RejectsEmptyTargetName(t *testing.T) {
	schema := &Schema{Tables: []Table{{SourceName: "Orders", PGName: "orders"}}}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{"Orders": "   "},
	}

	_, err := applyTableRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `table_renames entry "Orders" has an empty target table name`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_RejectsTargetNameOverPostgresLimit(t *testing.T) {
	schema := &Schema{Tables: []Table{{SourceName: "Orders", PGName: "orders"}}}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{"Orders": strings.Repeat("x", postgresMaxIdentifierBytes+1)},
	}

	_, err := applyTableRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exceeds PostgreSQL's 63-byte identifier limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_RejectsDuplicateSourceTableMapping(t *testing.T) {
	schema := &Schema{Tables: []Table{{SourceName: "Orders", PGName: "orders"}}}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{
			"Orders": "order_records",
			"orders": "order_records_again",
		},
	}

	_, err := applyTableRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `both map to source table orders`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_MatchesSourceNamesCaseInsensitively(t *testing.T) {
	schema := &Schema{Tables: []Table{{SourceName: "Orders", PGName: "orders"}}}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{"orders": "order_records"},
	}

	renamed, err := applyTableRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}
	if got := renamed.Tables[0].PGName; got != "order_records" {
		t.Fatalf("table PGName = %q, want order_records", got)
	}
}

func TestApplyTableRenames_RejectsAmbiguousTableNames(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Orders", PGName: "orders"},
			{SourceName: "orders", PGName: "orders_lower"},
		},
	}
	cfg := &MigrationConfig{
		TableRenames: map[string]string{"Orders": "order_records"},
	}

	_, err := applyTableRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source schema contains ambiguous table names") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyTableRenames_NilSchemaReturnsNil(t *testing.T) {
	renamed, err := applyTableRenames(nil, &MigrationConfig{
		TableRenames: map[string]string{"Orders": "order_records"},
	})
	if err != nil {
		t.Fatalf("applyTableRenames() error: %v", err)
	}
	if renamed != nil {
		t.Fatalf("renamed schema = %#v, want nil", renamed)
	}
}
