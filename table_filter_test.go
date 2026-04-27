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
	if got := strings.Join(report.OverlappingTables, ","); got != "accounts (excluded by \"Accounts\")" {
		t.Fatalf("overlapping tables = %q, want accounts (excluded by \"Accounts\")", got)
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

func TestFilterSchemaTables_RejectsUnknownExcludeTableName(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "OrderItems", PGName: "order_items"},
		},
	}
	cfg := &MigrationConfig{
		ExcludeTables: []string{"audit_log"},
	}

	_, _, err := filterSchemaTables(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exclude_tables entries did not match any source table") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterSchemaTables_GlobModeMatchesPatternsAndExcludeWins(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "App_Orders", PGName: "app_orders"},
			{SourceName: "App_AuditLog", PGName: "app_audit_log"},
			{SourceName: "audit_1", PGName: "audit_1"},
			{SourceName: "LegacyUsers", PGName: "legacy_users"},
		},
	}
	cfg := &MigrationConfig{
		TableFilterMode: "glob",
		IncludeTables:   []string{"app_*", "AUDIT_?"},
		ExcludeTables:   []string{"app_audit*"},
	}

	filtered, report, err := filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}

	if got := len(filtered.Tables); got != 2 {
		t.Fatalf("filtered table count = %d, want 2", got)
	}
	if got := strings.Join(report.SelectedTables, ","); got != "App_Orders,audit_1" {
		t.Fatalf("selected tables = %q, want App_Orders,audit_1", got)
	}
	if got := strings.Join(report.SkippedTables, ","); got != "App_AuditLog,LegacyUsers" {
		t.Fatalf("skipped tables = %q, want App_AuditLog,LegacyUsers", got)
	}
	if got := strings.Join(report.OverlappingTables, ","); got != "App_AuditLog (excluded by \"app_audit*\")" {
		t.Fatalf("overlapping tables = %q, want App_AuditLog (excluded by \"app_audit*\")", got)
	}
}

func TestFilterSchemaTables_GlobModeRejectsUnmatchedPattern(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Orders", PGName: "orders"},
		},
	}
	cfg := &MigrationConfig{
		TableFilterMode: "glob",
		IncludeTables:   []string{"app_*"},
	}

	_, _, err := filterSchemaTables(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "include_tables entries did not match any source table") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFilterSchemaTables_GlobModeExcludeOnly(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "App_Orders", PGName: "app_orders"},
			{SourceName: "App_AuditLog", PGName: "app_audit_log"},
			{SourceName: "Audit_1", PGName: "audit_1"},
		},
	}
	cfg := &MigrationConfig{
		TableFilterMode: "glob",
		ExcludeTables:   []string{"app_*"},
	}

	filtered, report, err := filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}

	if got := len(filtered.Tables); got != 1 {
		t.Fatalf("filtered table count = %d, want 1", got)
	}
	if got := filtered.Tables[0].SourceName; got != "Audit_1" {
		t.Fatalf("filtered table = %q, want Audit_1", got)
	}
	if got := strings.Join(report.SelectedTables, ","); got != "Audit_1" {
		t.Fatalf("selected tables = %q, want Audit_1", got)
	}
	if got := strings.Join(report.SkippedTables, ","); got != "App_Orders,App_AuditLog" {
		t.Fatalf("skipped tables = %q, want App_Orders,App_AuditLog", got)
	}
	if got := len(report.OverlappingTables); got != 0 {
		t.Fatalf("overlapping tables = %v, want none", report.OverlappingTables)
	}
}

func TestFilterSchemaTables_ExactModeExcludeOnly(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Accounts", PGName: "accounts"},
			{SourceName: "AuditLog", PGName: "audit_log"},
		},
	}
	cfg := &MigrationConfig{
		ExcludeTables: []string{"auditlog"},
	}

	filtered, report, err := filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}

	if got := len(filtered.Tables); got != 1 {
		t.Fatalf("filtered table count = %d, want 1", got)
	}
	if got := filtered.Tables[0].SourceName; got != "Accounts" {
		t.Fatalf("filtered table = %q, want Accounts", got)
	}
	if got := strings.Join(report.SelectedTables, ","); got != "Accounts" {
		t.Fatalf("selected tables = %q, want Accounts", got)
	}
	if got := strings.Join(report.SkippedTables, ","); got != "AuditLog" {
		t.Fatalf("skipped tables = %q, want AuditLog", got)
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

func TestFilterSchemaTables_PrunesCrossSchemaMSSQLForeignKeys(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "Users", PGName: "users"},
			{
				SourceName: "AuditLog",
				PGName:     "audit_log",
				ForeignKeys: []ForeignKey{
					{
						Name:       "fk_audit_users",
						RefSchema:  "archive",
						RefTable:   "Users",
						RefPGTable: "users",
					},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		Source: SourceConfig{
			Type:         "mssql",
			SourceSchema: "dbo",
		},
		IncludeTables: []string{"Users", "AuditLog"},
	}

	filtered, report, err := filterSchemaTables(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}

	if got := len(filtered.Tables[1].ForeignKeys); got != 0 {
		t.Fatalf("cross-schema foreign key count = %d, want 0", got)
	}
	if got := len(report.SkippedForeignKeys); got != 1 {
		t.Fatalf("skipped foreign key count = %d, want 1", got)
	}
	if got := report.SkippedForeignKeys[0].RefTable; got != "Users" {
		t.Fatalf("skipped foreign key ref table = %q, want Users", got)
	}
	if got := report.SkippedForeignKeys[0].Reason; !strings.Contains(got, `outside the migrated schema "dbo"`) {
		t.Fatalf("skipped foreign key reason = %q, want migrated schema detail", got)
	}
}

func TestFilterSchemaTables_NilSchemaReturnsEmptySchema(t *testing.T) {
	filtered, report, err := filterSchemaTables(nil, &MigrationConfig{})
	if err != nil {
		t.Fatalf("filterSchemaTables() error: %v", err)
	}
	if filtered == nil {
		t.Fatal("filtered schema = nil, want empty schema")
	}
	if got := len(filtered.Tables); got != 0 {
		t.Fatalf("filtered table count = %d, want 0", got)
	}
	if report.TotalTables != 0 {
		t.Fatalf("report.TotalTables = %d, want 0", report.TotalTables)
	}
}

func TestFilterSchemaColumns_ExcludesColumnsBySourceNameAndPrunesDependentObjects(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Orders",
				PGName:     "orders",
				Columns: []Column{
					{SourceName: "ID", PGName: "id"},
					{SourceName: "RowVersion", PGName: "row_version"},
					{SourceName: "CustomerID", PGName: "customer_id"},
				},
				PrimaryKey: &Index{Name: "pk_orders", Columns: []string{"id"}},
				Indexes: []Index{
					{Name: "idx_customer", Columns: []string{"customer_id"}},
					{Name: "idx_row_version", Columns: []string{"row_version"}},
				},
				ForeignKeys: []ForeignKey{
					{Name: "fk_orders_customer", Columns: []string{"customer_id"}, RefTable: "Customers", RefPGTable: "customers", RefColumns: []string{"id"}},
				},
			},
			{
				SourceName: "Customers",
				PGName:     "customers",
				Columns: []Column{
					{SourceName: "ID", PGName: "id"},
					{SourceName: "RowVersion", PGName: "row_version"},
				},
				PrimaryKey: &Index{Name: "pk_customers", Columns: []string{"id"}},
			},
		},
	}
	cfg := &MigrationConfig{
		ExcludeColumns: []string{"rowversion"},
	}

	filtered, report, err := filterSchemaColumns(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaColumns() error: %v", err)
	}

	orders := filtered.Tables[0]
	if got := sourceColumnNames(orders.Columns); strings.Join(got, ",") != "ID,CustomerID" {
		t.Fatalf("orders columns = %v, want [ID CustomerID]", got)
	}
	if got := len(orders.Indexes); got != 1 {
		t.Fatalf("orders indexes = %d, want 1", got)
	}
	if got := orders.Indexes[0].Name; got != "idx_customer" {
		t.Fatalf("kept index = %q, want idx_customer", got)
	}
	if got := len(orders.ForeignKeys); got != 1 {
		t.Fatalf("orders foreign keys = %d, want 1", got)
	}
	if got := sourceColumnNames(filtered.Tables[1].Columns); strings.Join(got, ",") != "ID" {
		t.Fatalf("customers columns = %v, want [ID]", got)
	}
	if got := strings.Join(report.ExcludedColumns, ","); got != "Orders.RowVersion,Customers.RowVersion" {
		t.Fatalf("excluded columns = %q, want Orders.RowVersion,Customers.RowVersion", got)
	}
	if got := len(report.SkippedIndexes); got != 1 || report.SkippedIndexes[0].Name != "idx_row_version" {
		t.Fatalf("skipped indexes = %#v, want idx_row_version", report.SkippedIndexes)
	}
}

func TestFilterSchemaColumns_QualifiedGlobPattern(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "App_Orders",
				PGName:     "app_orders",
				Columns: []Column{
					{SourceName: "ID", PGName: "id"},
					{SourceName: "Sys_RowVersion", PGName: "sys_row_version"},
				},
			},
			{
				SourceName: "AuditLog",
				PGName:     "audit_log",
				Columns: []Column{
					{SourceName: "ID", PGName: "id"},
					{SourceName: "Sys_RowVersion", PGName: "sys_row_version"},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		ColumnFilterMode: "glob",
		ExcludeColumns:   []string{"app_*.sys_*"},
	}

	filtered, report, err := filterSchemaColumns(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaColumns() error: %v", err)
	}

	if got := sourceColumnNames(filtered.Tables[0].Columns); strings.Join(got, ",") != "ID" {
		t.Fatalf("app orders columns = %v, want [ID]", got)
	}
	if got := sourceColumnNames(filtered.Tables[1].Columns); strings.Join(got, ",") != "ID,Sys_RowVersion" {
		t.Fatalf("audit columns = %v, want [ID Sys_RowVersion]", got)
	}
	if got := strings.Join(report.ExcludedColumns, ","); got != "App_Orders.Sys_RowVersion" {
		t.Fatalf("excluded columns = %q, want App_Orders.Sys_RowVersion", got)
	}
}

func TestFilterSchemaColumns_PrunesForeignKeysReferencingExcludedColumns(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Parents",
				PGName:     "parents",
				Columns: []Column{
					{SourceName: "ID", PGName: "id"},
					{SourceName: "RowVersion", PGName: "row_version"},
				},
			},
			{
				SourceName: "Children",
				PGName:     "children",
				Columns: []Column{
					{SourceName: "ID", PGName: "id"},
					{SourceName: "ParentID", PGName: "parent_id"},
					{SourceName: "ParentRowVersion", PGName: "parent_row_version"},
				},
				ForeignKeys: []ForeignKey{
					{Name: "fk_child_parent_id", Columns: []string{"parent_id"}, RefTable: "Parents", RefPGTable: "parents", RefColumns: []string{"id"}},
					{Name: "fk_child_parent_version", Columns: []string{"parent_row_version"}, RefTable: "Parents", RefPGTable: "parents", RefColumns: []string{"row_version"}},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		ExcludeColumns: []string{"RowVersion"},
	}

	filtered, report, err := filterSchemaColumns(schema, cfg)
	if err != nil {
		t.Fatalf("filterSchemaColumns() error: %v", err)
	}

	children := filtered.Tables[1]
	if got := len(children.ForeignKeys); got != 1 {
		t.Fatalf("children foreign keys = %d, want 1", got)
	}
	if got := children.ForeignKeys[0].Name; got != "fk_child_parent_id" {
		t.Fatalf("kept foreign key = %q, want fk_child_parent_id", got)
	}
	if got := len(report.SkippedForeignKeys); got != 1 {
		t.Fatalf("skipped foreign keys = %d, want 1", got)
	}
	if got := report.SkippedForeignKeys[0].Name; got != "fk_child_parent_version" {
		t.Fatalf("skipped foreign key = %q, want fk_child_parent_version", got)
	}
	if got := report.SkippedForeignKeys[0].Reason; !strings.Contains(got, "referenced column row_version is excluded") {
		t.Fatalf("skipped foreign key reason = %q, want referenced column detail", got)
	}
}

func TestFilterSchemaColumns_RejectsUnknownExcludeColumn(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Orders",
				PGName:     "orders",
				Columns:    []Column{{SourceName: "ID", PGName: "id"}},
			},
		},
	}
	cfg := &MigrationConfig{
		ExcludeColumns: []string{"RowVersion"},
	}

	_, _, err := filterSchemaColumns(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exclude_columns entries did not match any source column") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func sourceColumnNames(cols []Column) []string {
	names := make([]string, 0, len(cols))
	for _, col := range cols {
		names = append(names, col.SourceName)
	}
	return names
}

func TestFilterTriggersBySelectedTables(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{SourceName: "orders", PGName: "orders"},
			{SourceName: "Products", PGName: "products"},
		},
	}
	triggers := []SourceTrigger{
		{Name: "t_orders", Table: "orders"},
		{Name: "t_audit", Table: "audit_log"},
		{Name: "t_unknown", Table: ""},
	}
	got := filterTriggersBySelectedTables(triggers, schema)
	if len(got) != 2 {
		t.Fatalf("got len=%d %+v, want t_orders and t_unknown (empty table preserved)", len(got), got)
	}
	if got[0].Name != "t_orders" || got[1].Name != "t_unknown" || got[1].Table != "" {
		t.Fatalf("got %+v, want [t_orders, t_unknown]", got)
	}
	got2 := filterTriggersBySelectedTables([]SourceTrigger{{Name: "x", Table: "PRODUCTS"}}, schema)
	if len(got2) != 1 || got2[0].Name != "x" {
		t.Fatalf("case-insensitive table: got %+v", got2)
	}
	if len(filterTriggersBySelectedTables(nil, schema)) != 0 {
		t.Fatal("nil triggers slice should yield empty slice")
	}
	preserved := filterTriggersBySelectedTables(triggers, nil)
	if len(preserved) != len(triggers) {
		t.Fatalf("nil schema should preserve all triggers: got %d want %d", len(preserved), len(triggers))
	}
}
