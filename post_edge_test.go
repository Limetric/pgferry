package main

import (
	"strings"
	"testing"
)

func TestTruncateGeneratedIdentifier_ShortName(t *testing.T) {
	got := truncateGeneratedIdentifier("short")
	if got != "short" {
		t.Fatalf("got %q, want %q", got, "short")
	}
}

func TestTruncateGeneratedIdentifier_ExactlyMaxLen(t *testing.T) {
	name := strings.Repeat("a", 63)
	got := truncateGeneratedIdentifier(name)
	if got != name {
		t.Fatalf("63-char name should not be truncated")
	}
}

func TestTruncateGeneratedIdentifier_Over63Chars(t *testing.T) {
	name := strings.Repeat("a", 70)
	got := truncateGeneratedIdentifier(name)
	if len(got) > 63 {
		t.Fatalf("truncated name length = %d, want <= 63", len(got))
	}
	if !strings.Contains(got, "_") {
		t.Fatal("truncated name should contain hash suffix")
	}
}

func TestTruncateGeneratedIdentifier_Deterministic(t *testing.T) {
	name := strings.Repeat("x", 100)
	got1 := truncateGeneratedIdentifier(name)
	got2 := truncateGeneratedIdentifier(name)
	if got1 != got2 {
		t.Fatalf("not deterministic: %q vs %q", got1, got2)
	}
}

func TestTruncateGeneratedIdentifier_DifferentNamesProduceDifferentResults(t *testing.T) {
	name1 := strings.Repeat("a", 80)
	name2 := strings.Repeat("b", 80)
	got1 := truncateGeneratedIdentifier(name1)
	got2 := truncateGeneratedIdentifier(name2)
	if got1 == got2 {
		t.Fatal("different long names should produce different truncated results")
	}
}

func TestTruncateGeneratedIdentifierWithSuffix_PreservesSuffix(t *testing.T) {
	base := strings.Repeat("long_table_name_", 5) // >63 chars total
	suffix := "_pkey"
	got := truncateGeneratedIdentifierWithSuffix(base, suffix)
	if len(got) > 63 {
		t.Fatalf("length = %d, want <= 63", len(got))
	}
	if !strings.Contains(got, suffix) {
		t.Fatalf("result %q should contain suffix %q", got, suffix)
	}
}

func TestTruncateGeneratedIdentifierWithSuffix_EmptySuffix(t *testing.T) {
	base := strings.Repeat("x", 100)
	got := truncateGeneratedIdentifierWithSuffix(base, "")
	if len(got) > 63 {
		t.Fatalf("length = %d, want <= 63", len(got))
	}
}

func TestTruncateGeneratedIdentifierWithSuffix_ShortNoTruncation(t *testing.T) {
	got := truncateGeneratedIdentifierWithSuffix("orders", "_pkey")
	if got != "orders_pkey" {
		t.Fatalf("got %q, want 'orders_pkey'", got)
	}
}

func TestUnsignedCheckExpr_AllIntTypes(t *testing.T) {
	tests := []struct {
		dataType string
		wantMin  string
		wantMax  string
	}{
		{"tinyint", ">= 0", "<= 255"},
		{"smallint", ">= 0", "<= 65535"},
		{"mediumint", ">= 0", "<= 16777215"},
		{"int", ">= 0", "<= 4294967295"},
		{"bigint", ">= 0", "<= 18446744073709551615"},
	}

	for _, tt := range tests {
		col := Column{DataType: tt.dataType, ColumnType: tt.dataType + " unsigned", PGName: "col"}
		got, ok := unsignedCheckExpr(col, defaultTypeMappingConfig())
		if !ok {
			t.Fatalf("unsignedCheckExpr(%s unsigned) = false", tt.dataType)
		}
		if !strings.Contains(got, tt.wantMin) || !strings.Contains(got, tt.wantMax) {
			t.Errorf("unsignedCheckExpr(%s unsigned) = %q, want min=%s max=%s", tt.dataType, got, tt.wantMin, tt.wantMax)
		}
	}
}

func TestUnsignedCheckExpr_FloatingPoint(t *testing.T) {
	for _, dt := range []string{"decimal", "float", "double"} {
		col := Column{DataType: dt, ColumnType: dt + " unsigned", PGName: "col"}
		got, ok := unsignedCheckExpr(col, defaultTypeMappingConfig())
		if !ok {
			t.Fatalf("unsignedCheckExpr(%s unsigned) = false", dt)
		}
		if !strings.Contains(got, ">= 0") {
			t.Errorf("unsignedCheckExpr(%s unsigned) = %q, want '>= 0'", dt, got)
		}
		// Floating point types should not have upper bound
		if strings.Contains(got, "<=") {
			t.Errorf("unsignedCheckExpr(%s unsigned) = %q, should not have upper bound", dt, got)
		}
	}
}

func TestUnsignedCheckExpr_NotUnsigned(t *testing.T) {
	col := Column{DataType: "int", ColumnType: "int", PGName: "col"}
	_, ok := unsignedCheckExpr(col, defaultTypeMappingConfig())
	if ok {
		t.Fatal("non-unsigned column should not produce check expr")
	}
}

func TestUnsignedCheckExpr_TinyInt1AsBooleanSkipped(t *testing.T) {
	col := Column{DataType: "tinyint", ColumnType: "tinyint(1) unsigned", PGName: "col"}
	tm := defaultTypeMappingConfig()
	tm.TinyInt1AsBoolean = true

	_, ok := unsignedCheckExpr(col, tm)
	if ok {
		t.Fatal("tinyint(1) unsigned with TinyInt1AsBoolean should be skipped")
	}
}

func TestUnsignedCheckExpr_UnknownType(t *testing.T) {
	col := Column{DataType: "varchar", ColumnType: "varchar(255) unsigned", PGName: "col"}
	_, ok := unsignedCheckExpr(col, defaultTypeMappingConfig())
	if ok {
		t.Fatal("varchar unsigned should not produce check expr")
	}
}

func TestUnsignedConstraintName_Short(t *testing.T) {
	got := unsignedConstraintName("orders", "id")
	if got != "ck_orders_id_unsigned" {
		t.Fatalf("got %q, want 'ck_orders_id_unsigned'", got)
	}
}

func TestUnsignedConstraintName_LongTruncates(t *testing.T) {
	table := strings.Repeat("a", 40)
	col := strings.Repeat("b", 40)
	got := unsignedConstraintName(table, col)
	if len(got) > 63 {
		t.Fatalf("length = %d, want <= 63", len(got))
	}
	if !strings.Contains(got, "_unsigned") {
		t.Fatalf("result %q should contain '_unsigned'", got)
	}
}

func TestGeneratedPrimaryKeyName(t *testing.T) {
	table := Table{PGName: "users"}
	got := generatedPrimaryKeyName(table)
	if got != "users_pkey" {
		t.Fatalf("got %q, want 'users_pkey'", got)
	}
}

func TestGeneratedIndexName(t *testing.T) {
	table := Table{PGName: "users"}
	idx := Index{Name: "idx_email"}
	got := generatedIndexName(table, idx)
	if got != "users_idx_email" {
		t.Fatalf("got %q, want 'users_idx_email'", got)
	}
}

func TestGeneratedSequenceName(t *testing.T) {
	table := Table{PGName: "users"}
	col := Column{PGName: "id"}
	got := generatedSequenceName(table, col)
	if got != "users_id_seq" {
		t.Fatalf("got %q, want 'users_id_seq'", got)
	}
}

func TestGeneratedTriggerName(t *testing.T) {
	table := Table{PGName: "users"}
	col := Column{PGName: "updated_at"}
	got := generatedTriggerName(table, col)
	if got != "trg_users_updated_at" {
		t.Fatalf("got %q, want 'trg_users_updated_at'", got)
	}
}

func TestGeneratedTriggerFunctionName(t *testing.T) {
	col := Column{PGName: "updated_at"}
	got := generatedTriggerFunctionName(col)
	if got != "set_updated_at" {
		t.Fatalf("got %q, want 'set_updated_at'", got)
	}
}

func TestHasAutoIncrementExtra(t *testing.T) {
	if !hasAutoIncrementExtra(Column{Extra: "auto_increment"}) {
		t.Error("should detect auto_increment")
	}
	if hasAutoIncrementExtra(Column{Extra: "STORED GENERATED"}) {
		t.Error("should not detect auto_increment in generated column")
	}
	if hasAutoIncrementExtra(Column{Extra: ""}) {
		t.Error("should not detect auto_increment in empty extra")
	}
}

func TestHasOnUpdateCurrentTimestampExtra(t *testing.T) {
	if !hasOnUpdateCurrentTimestampExtra(Column{Extra: "on update current_timestamp"}) {
		t.Error("should detect lowercase on update current_timestamp")
	}
	if !hasOnUpdateCurrentTimestampExtra(Column{Extra: "ON UPDATE CURRENT_TIMESTAMP"}) {
		t.Error("should detect uppercase ON UPDATE CURRENT_TIMESTAMP")
	}
	if hasOnUpdateCurrentTimestampExtra(Column{Extra: "auto_increment"}) {
		t.Error("should not detect on update in auto_increment")
	}
}

func TestOrphanCleanupAction_SetNull(t *testing.T) {
	fk := ForeignKey{DeleteRule: "SET NULL"}
	if orphanCleanupAction(fk) != "set_null" {
		t.Fatalf("expected 'set_null' for SET NULL")
	}
}

func TestOrphanCleanupAction_SetNullCaseInsensitive(t *testing.T) {
	fk := ForeignKey{DeleteRule: "set null"}
	if orphanCleanupAction(fk) != "set_null" {
		t.Fatalf("expected 'set_null' for lowercase 'set null'")
	}
}

func TestOrphanCleanupAction_Cascade(t *testing.T) {
	fk := ForeignKey{DeleteRule: "CASCADE"}
	if orphanCleanupAction(fk) != "delete" {
		t.Fatalf("expected 'delete' for CASCADE")
	}
}

func TestOrphanCleanupAction_NoAction(t *testing.T) {
	fk := ForeignKey{DeleteRule: "NO ACTION"}
	if orphanCleanupAction(fk) != "delete" {
		t.Fatalf("expected 'delete' for NO ACTION")
	}
}

func TestBuildCleanOrphansSQL_SelfReferencingFK(t *testing.T) {
	table := Table{PGName: "categories"}
	fk := ForeignKey{
		Name:       "fk_parent",
		Columns:    []string{"parent_id"},
		RefPGTable: "categories",
		RefColumns: []string{"id"},
		DeleteRule: "CASCADE",
	}

	sql := buildCleanOrphansSQL("public", table, fk)
	if !strings.Contains(sql, "DELETE FROM") {
		t.Fatalf("expected DELETE, got: %s", sql)
	}
	if !strings.Contains(sql, `"categories"`) {
		t.Fatalf("expected table name, got: %s", sql)
	}
	// Self-referencing: child and parent are the same table
	if !strings.Contains(sql, "NOT EXISTS") {
		t.Fatalf("expected NOT EXISTS subquery, got: %s", sql)
	}
}

func TestBuildCleanOrphansSQL_ThreeColumnCompositeFK(t *testing.T) {
	table := Table{PGName: "events"}
	fk := ForeignKey{
		Name:       "fk_location",
		Columns:    []string{"country", "state", "city"},
		RefPGTable: "locations",
		RefColumns: []string{"country", "state", "city"},
		DeleteRule: "SET NULL",
	}

	sql := buildCleanOrphansSQL("public", table, fk)
	if !strings.Contains(sql, "UPDATE") {
		t.Fatalf("expected UPDATE for SET NULL, got: %s", sql)
	}
	// Should have 3 SET clauses and 3 NOT NULL conditions
	if strings.Count(sql, "= NULL") != 3 {
		t.Fatalf("expected 3 SET NULL clauses, got: %s", sql)
	}
	if strings.Count(sql, "IS NOT NULL") != 3 {
		t.Fatalf("expected 3 IS NOT NULL conditions, got: %s", sql)
	}
}

func TestForeignKeyAllNotNullPredicate_SingleColumn(t *testing.T) {
	got := foreignKeyAllNotNullPredicate([]string{"user_id"})
	if got != `c."user_id" IS NOT NULL` {
		t.Fatalf("got %q", got)
	}
}

func TestForeignKeyAllNotNullPredicate_MultipleColumns(t *testing.T) {
	got := foreignKeyAllNotNullPredicate([]string{"a", "b", "c"})
	if !strings.Contains(got, `c."a" IS NOT NULL`) {
		t.Fatalf("missing column a in: %s", got)
	}
	if !strings.Contains(got, `c."b" IS NOT NULL`) {
		t.Fatalf("missing column b in: %s", got)
	}
	if !strings.Contains(got, `c."c" IS NOT NULL`) {
		t.Fatalf("missing column c in: %s", got)
	}
}

func TestQuotedOrderedColumnList_WithDESC(t *testing.T) {
	cols := []string{"name", "age"}
	orders := []string{"ASC", "DESC"}
	got := quotedOrderedColumnList(cols, orders)
	if !strings.Contains(got, `"age" DESC`) {
		t.Fatalf("expected DESC on age, got: %s", got)
	}
	if strings.Contains(got, `"name" ASC`) {
		t.Fatalf("ASC should not be appended explicitly, got: %s", got)
	}
}

func TestQuotedOrderedColumnList_NoOrders(t *testing.T) {
	cols := []string{"id", "name"}
	got := quotedOrderedColumnList(cols, nil)
	if got != `"id", "name"` {
		t.Fatalf("got %q, want '\"id\", \"name\"'", got)
	}
}

func TestResetSequenceStatements_Structure(t *testing.T) {
	table := Table{PGName: "users"}
	col := Column{PGName: "id", Extra: "auto_increment"}

	stmts := resetSequenceStatements("public", table, col)
	if len(stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(stmts))
	}
	if !strings.Contains(stmts[0], "CREATE SEQUENCE") {
		t.Fatalf("first statement should be CREATE SEQUENCE, got: %s", stmts[0])
	}
	if !strings.Contains(stmts[1], "setval") {
		t.Fatalf("second statement should use setval, got: %s", stmts[1])
	}
	if !strings.Contains(stmts[2], "SET DEFAULT nextval") {
		t.Fatalf("third statement should SET DEFAULT nextval, got: %s", stmts[2])
	}
}
