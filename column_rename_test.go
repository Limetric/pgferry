package main

import (
	"strings"
	"testing"
)

func TestApplyColumnRenames_UpdatesColumnsIndexesAndForeignKeys(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Parents",
				PGName:     "parents",
				Columns: []Column{
					{SourceName: "Code", PGName: "code"},
				},
				PrimaryKey: &Index{Name: "pk_parents", Columns: []string{"code"}},
				Indexes: []Index{
					{Name: "idx_parent_code", Columns: []string{"code"}},
				},
			},
			{
				SourceName: "Children",
				PGName:     "children",
				Columns: []Column{
					{SourceName: "ParentCode", PGName: "parent_code"},
				},
				ForeignKeys: []ForeignKey{
					{
						Name:       "fk_children_parents",
						Columns:    []string{"parent_code"},
						RefTable:   "Parents",
						RefPGTable: "parents",
						RefColumns: []string{"code"},
					},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		ColumnRenames: map[string]string{
			"Parents.Code":        "parent_code_target",
			"Children.ParentCode": "child_parent_code_target",
		},
	}

	renamed, err := applyColumnRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyColumnRenames() error: %v", err)
	}

	parent := renamed.Tables[0]
	if got := parent.Columns[0].PGName; got != "parent_code_target" {
		t.Fatalf("parent column PGName = %q, want parent_code_target", got)
	}
	if got := parent.PrimaryKey.Columns[0]; got != "parent_code_target" {
		t.Fatalf("parent primary key column = %q, want parent_code_target", got)
	}
	if got := parent.Indexes[0].Columns[0]; got != "parent_code_target" {
		t.Fatalf("parent index column = %q, want parent_code_target", got)
	}

	child := renamed.Tables[1]
	if got := child.Columns[0].PGName; got != "child_parent_code_target" {
		t.Fatalf("child column PGName = %q, want child_parent_code_target", got)
	}
	if got := child.ForeignKeys[0].Columns[0]; got != "child_parent_code_target" {
		t.Fatalf("child foreign key column = %q, want child_parent_code_target", got)
	}
	if got := child.ForeignKeys[0].RefColumns[0]; got != "parent_code_target" {
		t.Fatalf("child foreign key ref column = %q, want parent_code_target", got)
	}
	if got := schema.Tables[0].Columns[0].PGName; got != "code" {
		t.Fatalf("original schema mutated: parent column PGName = %q", got)
	}
}

func TestApplyColumnRenames_ResolvesPostgresTruncationCollision(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "KP_SUMINA",
				PGName:     "KP_SUMINA",
				Columns: []Column{
					{SourceName: "% ставка резерва по категории качества", PGName: "% ставка резерва по категории качества"},
					{SourceName: "% ставка резерва по категории качества КД", PGName: "% ставка резерва по категории качества КД"},
				},
			},
		},
	}
	cfg := &MigrationConfig{
		ColumnRenames: map[string]string{
			"KP_SUMINA.% ставка резерва по категории качества":    "reserve_rate_quality",
			"KP_SUMINA.% ставка резерва по категории качества КД": "reserve_rate_quality_kd",
		},
	}

	renamed, err := applyColumnRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyColumnRenames() error: %v", err)
	}
	if err := validateGeneratedIdentifiers(renamed, &MigrationConfig{}, defaultTypeMappingConfig()); err != nil {
		t.Fatalf("validateGeneratedIdentifiers() error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsUnknownColumn(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"Orders.Missing": "missing_target",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "column_renames entries did not match any source column on matched source tables: Orders.Missing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsUnknownTable(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"Customers.ID": "customer_id",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "column_renames entries did not match any source table in the migrated schema") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsUnqualifiedEntry(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"ID": "id_target",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `column_renames entry "ID" must be qualified as TableName.ColumnName`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsSchemaQualifiedEntry(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"dbo.Orders.ID": "id_target",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `column_renames entry "dbo.Orders.ID" must be qualified as TableName.ColumnName`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsEmptyTargetName(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"Orders.ID": "   ",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `column_renames entry "Orders.ID" has an empty target column name`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsTargetNameOverPostgresLimit(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"Orders.ID": strings.Repeat("x", postgresMaxIdentifierBytes+1),
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "exceeds PostgreSQL's 63-byte identifier limit") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_RejectsDuplicateSourceColumnMapping(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"Orders.ID": "id_target",
			"orders.id": "id_target_again",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `both target source column orders.id`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_MatchesSourceNamesCaseInsensitively(t *testing.T) {
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
		ColumnRenames: map[string]string{
			"orders.id": "order_id",
		},
	}

	renamed, err := applyColumnRenames(schema, cfg)
	if err != nil {
		t.Fatalf("applyColumnRenames() error: %v", err)
	}
	if got := renamed.Tables[0].Columns[0].PGName; got != "order_id" {
		t.Fatalf("column PGName = %q, want order_id", got)
	}
}

func TestApplyColumnRenames_RejectsAmbiguousTableNames(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				SourceName: "Orders",
				PGName:     "orders",
				Columns:    []Column{{SourceName: "ID", PGName: "id"}},
			},
			{
				SourceName: "orders",
				PGName:     "orders_lower",
				Columns:    []Column{{SourceName: "ID", PGName: "id"}},
			},
		},
	}
	cfg := &MigrationConfig{
		ColumnRenames: map[string]string{
			"Orders.ID": "order_id",
		},
	}

	_, err := applyColumnRenames(schema, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source schema contains ambiguous table names") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyColumnRenames_NilSchemaReturnsNil(t *testing.T) {
	renamed, err := applyColumnRenames(nil, &MigrationConfig{
		ColumnRenames: map[string]string{"Orders.ID": "order_id"},
	})
	if err != nil {
		t.Fatalf("applyColumnRenames() error: %v", err)
	}
	if renamed != nil {
		t.Fatalf("renamed schema = %#v, want nil", renamed)
	}
}
