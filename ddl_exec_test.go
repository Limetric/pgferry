package main

import (
	"context"
	"strings"
	"testing"
)

func TestCreateEnumTypes_DeduplicatesIdenticalValueSets(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "orders",
				Columns: []Column{
					{PGName: "status", DataType: "enum", ColumnType: "enum('pending','paid')"},
				},
			},
			{
				PGName: "invoices",
				Columns: []Column{
					{PGName: "status", DataType: "enum", ColumnType: "enum('paid','pending')"},
					{PGName: "kind", DataType: "enum", ColumnType: "enum('draft','final')"},
				},
			},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"
	exec := &recordingStatementExecutor{}

	if err := createEnumTypes(context.Background(), exec, schema, "app", tm, "mysql"); err != nil {
		t.Fatalf("createEnumTypes() error: %v", err)
	}

	if len(exec.calls) != 2 {
		t.Fatalf("exec calls = %d, want 2", len(exec.calls))
	}
	// createEnumTypes iterates schema.Tables and Columns in slice order, so the
	// first distinct enum encountered is orders.status and the second is invoices.kind.
	if !strings.Contains(exec.calls[0], `AS ENUM ('paid', 'pending')`) {
		t.Fatalf("first enum SQL = %q, want sorted identical-value enum", exec.calls[0])
	}
	if !strings.Contains(exec.calls[1], `AS ENUM ('draft', 'final')`) {
		t.Fatalf("second enum SQL = %q, want sorted distinct enum", exec.calls[1])
	}
}

func TestCreateEnumTypes_InvalidEnumDefinitionReturnsError(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "orders",
				Columns: []Column{
					{PGName: "status", DataType: "enum", ColumnType: "enum("},
				},
			},
		},
	}
	tm := defaultTypeMappingConfig()
	tm.EnumMode = "native"
	exec := &recordingStatementExecutor{}

	err := createEnumTypes(context.Background(), exec, schema, "app", tm, "mysql")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse enum values") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateTables_ExecutesGeneratedDDL(t *testing.T) {
	schema := &Schema{
		Tables: []Table{
			{
				PGName: "users",
				Columns: []Column{
					{PGName: "id", Nullable: false},
					{PGName: "name", Nullable: false},
				},
			},
		},
	}
	src := fakeDDLSource{
		fakeNamedSource: fakeNamedSource{name: "MySQL"},
		pgTypeByColumn: map[string]string{
			"id":   "bigint",
			"name": "text",
		},
	}
	exec := &recordingStatementExecutor{}

	if err := createTables(context.Background(), exec, schema, "app", false, true, defaultTypeMappingConfig(), src); err != nil {
		t.Fatalf("createTables() error: %v", err)
	}

	if len(exec.calls) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(exec.calls))
	}
	if !strings.Contains(exec.calls[0], `CREATE TABLE "app"."users"`) {
		t.Fatalf("create table SQL = %q, want schema-qualified users table", exec.calls[0])
	}
	if !strings.Contains(exec.calls[0], `"id" bigint NOT NULL`) {
		t.Fatalf("create table SQL = %q, want id column", exec.calls[0])
	}
	if !strings.Contains(exec.calls[0], `"name" text NOT NULL`) {
		t.Fatalf("create table SQL = %q, want name column", exec.calls[0])
	}
}
