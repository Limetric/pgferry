package main

import (
	"strings"
	"testing"
)

func TestFilterSchemaTables_UsesSourceNamesAndExcludeWins(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Accounts", PGName: "accounts"},
			{SourceName: "OrderItems", PGName: "order_items"},
			{SourceName: "AuditLog", PGName: "audit_log"},
		},
	}
	cfg := &MigrationConfig{
		IncludeTables: []string{"accounts", "OrderItems"},
		ExcludeTables: []string{"Accounts"},
	}

	filtered, report, err := filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}

	if got := len(filtered.Tables); got != 1 {
		t.Fatalf("filtered table count = %d, want 1", got)
	}
	if got := filtered.Tables[0].SourceName; got != "OrderItems" {
		t.Fatalf("filtered table = %q, want OrderItems", got)
	}
	if got := strings.Join(report.SelectedTables, ","); got != "OrderItems" {
		t.Fatalf("selected tables = %q, want OrderItems", got)
	}
	if got := strings.Join(report.SkippedTables, ","); got != "Accounts,AuditLog" {
		t.Fatalf("skipped tables = %q, want Accounts,AuditLog", got)
	}
}

func TestFilterSchemaTables_PrunesForeignKeysToExcludedTables(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Orders", PGName: "orders"},
			{SourceName: "Products", PGName: "products"},
			{
				SourceName: "OrderItems",
				PGName:     "order_items",
				ForeignKeys: []ForeignKey{
					{Name: "fk_order_items_orders", RefTable: "Orders", RefPGTable: "orders"},
					{Name: "fk_order_items_products", RefTable: "Products", RefPGTable: "products"},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		IncludeTables: []string{"Orders", "OrderItems"},
	}

	filtered, report, err := filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}

	var orderItems Table
	for _, table := range filtered.Tables {
		if table.SourceName == "OrderItems" {
			orderItems = table
			break
		}
	}
	if got := len(orderItems.ForeignKeys); got != 1 {
		t.Fatalf("filtered foreign key count = %d, want 1", got)
	}
	if got := orderItems.ForeignKeys[0].Name; got != "fk_order_items_orders" {
		t.Fatalf("kept foreign key = %q, want fk_order_items_orders", got)
	}
	if got := len(report.SkippedForeignKeys); got != 1 {
		t.Fatalf("skipped foreign key count = %d, want 1", got)
	}
	if got := report.SkippedForeignKeys[0].Name; got != "fk_order_items_products" {
		t.Fatalf("skipped foreign key = %q, want fk_order_items_products", got)
	}
	if got := len(schema.Tables[2].ForeignKeys); got != 2 {
		t.Fatalf("original schema foreign key count = %d, want 2", got)
	}
}

func TestFilterSchemaTables_RejectsUnknownTableName(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "OrderItems", PGName: "order_items"},
		},
	}
	cfg := &MigrationConfig{
		IncludeTables: []string{"order_items"},
	}

	_, _, err := filterSchemaTables(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "include_tables entries did not match any source table") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterSchemaTables_RejectsEmptyResult(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Accounts", PGName: "accounts"},
		},
	}
	cfg := &MigrationConfig{
		IncludeTables: []string{"Accounts"},
		ExcludeTables: []string{"accounts"},
	}

	_, _, err := filterSchemaTables(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "excluded every source table") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterSchemaTables_RejectsAmbiguousCaseInsensitiveSourceNames(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Users", PGName: "users"},
			{SourceName: "users", PGName: "users_2"},
		},
	}
	cfg := &MigrationConfig{
		IncludeTables: []string{"users"},
	}

	_, _, err := filterSchemaTables(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ambiguous table names") {
		t.Fatalf("unexpected error: %v", err)
	}
}
