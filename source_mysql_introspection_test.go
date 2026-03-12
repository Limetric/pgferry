package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

type mysqlIntrospectionStub struct {
	mu      sync.Mutex
	queries []mysqlStubQueryCall
}

type mysqlStubQueryCall struct {
	query string
	args  []any
}

type mysqlStubDriver struct {
	stub *mysqlIntrospectionStub
}

type mysqlStubConn struct {
	stub *mysqlIntrospectionStub
}

type mysqlStubStmt struct {
	conn  *mysqlStubConn
	query string
}

type mysqlStubRows struct {
	columns []string
	data    [][]driver.Value
	index   int
}

func (d *mysqlStubDriver) Open(string) (driver.Conn, error) {
	return &mysqlStubConn{stub: d.stub}, nil
}

func (c *mysqlStubConn) Prepare(query string) (driver.Stmt, error) {
	return &mysqlStubStmt{conn: c, query: query}, nil
}

func (c *mysqlStubConn) Close() error { return nil }

func (c *mysqlStubConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("transactions not supported")
}

func (c *mysqlStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.stub.query(query, args)
}

func (c *mysqlStubConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return c.stub.query(query, named)
}

func (s *mysqlStubStmt) Close() error { return nil }

func (s *mysqlStubStmt) NumInput() int { return -1 }

func (s *mysqlStubStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("exec not supported")
}

func (s *mysqlStubStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.Query(s.query, args)
}

func (r *mysqlStubRows) Columns() []string { return r.columns }

func (r *mysqlStubRows) Close() error { return nil }

func (r *mysqlStubRows) Next(dest []driver.Value) error {
	if r.index >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.index])
	r.index++
	return nil
}

func (s *mysqlIntrospectionStub) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	normalized := strings.Join(strings.Fields(query), " ")
	values := make([]any, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}

	s.mu.Lock()
	s.queries = append(s.queries, mysqlStubQueryCall{query: normalized, args: values})
	s.mu.Unlock()

	switch {
	case strings.Contains(normalized, "FROM INFORMATION_SCHEMA.TABLES"):
		return &mysqlStubRows{
			columns: []string{"TABLE_NAME"},
			data: [][]driver.Value{
				{"Accounts"},
				{"AuditTrail"},
				{"OrderVersions"},
			},
		}, nil
	case strings.Contains(normalized, "FROM INFORMATION_SCHEMA.COLUMNS"):
		return &mysqlStubRows{
			columns: []string{
				"TABLE_NAME", "COLUMN_NAME", "DATA_TYPE", "COLUMN_TYPE",
				"CHARACTER_MAXIMUM_LENGTH", "NUMERIC_PRECISION", "NUMERIC_SCALE",
				"IS_NULLABLE", "COLUMN_DEFAULT", "EXTRA", "ORDINAL_POSITION",
				"CHARACTER_SET_NAME", "COLLATION_NAME", "GENERATION_EXPRESSION",
			},
			data: [][]driver.Value{
				{"Accounts", "AccountID", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "auto_increment", int64(1), "", "", ""},
				{"Accounts", "UserName", "varchar", "varchar(64)", int64(64), int64(0), int64(0), "NO", nil, "", int64(2), "utf8mb4", "utf8mb4_unicode_ci", ""},
				{"AuditTrail", "OrderID", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "", int64(1), "", "", ""},
				{"AuditTrail", "VersionNo", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "", int64(2), "", "", ""},
				{"AuditTrail", "ActorAccountID", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "", int64(3), "", "", ""},
				{"OrderVersions", "OrderID", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "", int64(1), "", "", ""},
				{"OrderVersions", "VersionNo", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "", int64(2), "", "", ""},
				{"OrderVersions", "AccountID", "int", "int(11)", int64(0), int64(10), int64(0), "NO", nil, "", int64(3), "", "", ""},
				{"OrderVersions", "StatusCode", "varchar", "varchar(16)", int64(16), int64(0), int64(0), "NO", "new", "", int64(4), "utf8mb4", "utf8mb4_bin", ""},
				{"OrderVersions", "DisplayLabel", "varchar", "varchar(64)", int64(64), int64(0), int64(0), "YES", nil, "VIRTUAL GENERATED", int64(5), "utf8mb4", "utf8mb4_unicode_ci", "concat(`OrderID`,'-',`VersionNo`)"},
			},
		}, nil
	case strings.Contains(normalized, "FROM INFORMATION_SCHEMA.STATISTICS"):
		return &mysqlStubRows{
			columns: []string{"TABLE_NAME", "INDEX_NAME", "COLUMN_NAME", "NON_UNIQUE", "SEQ_IN_INDEX", "INDEX_TYPE", "COLLATION", "SUB_PART"},
			data: [][]driver.Value{
				{"Accounts", "PRIMARY", "AccountID", int64(0), int64(1), "BTREE", "A", nil},
				{"OrderVersions", "PRIMARY", "OrderID", int64(0), int64(1), "BTREE", "A", nil},
				{"OrderVersions", "PRIMARY", "VersionNo", int64(0), int64(2), "BTREE", "A", nil},
				{"OrderVersions", "ft_status", "StatusCode", int64(1), int64(1), "FULLTEXT", "A", nil},
				{"OrderVersions", "idx_account_status", "AccountID", int64(1), int64(1), "BTREE", "D", nil},
				{"OrderVersions", "idx_account_status", "StatusCode", int64(1), int64(2), "BTREE", "A", int64(4)},
				{"OrderVersions", "idx_label_expr", nil, int64(1), int64(1), "BTREE", nil, nil},
			},
		}, nil
	case strings.Contains(normalized, "FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE"):
		return &mysqlStubRows{
			columns: []string{"TABLE_NAME", "CONSTRAINT_NAME", "COLUMN_NAME", "REFERENCED_TABLE_NAME", "REFERENCED_COLUMN_NAME", "UPDATE_RULE", "DELETE_RULE"},
			data: [][]driver.Value{
				{"AuditTrail", "fk_audit_actor", "ActorAccountID", "Accounts", "AccountID", "RESTRICT", "RESTRICT"},
				{"AuditTrail", "fk_audit_order", "OrderID", "OrderVersions", "OrderID", "CASCADE", "CASCADE"},
				{"AuditTrail", "fk_audit_order", "VersionNo", "OrderVersions", "VersionNo", "CASCADE", "CASCADE"},
				{"OrderVersions", "fk_order_versions_account", "AccountID", "Accounts", "AccountID", "CASCADE", "RESTRICT"},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", normalized)
	}
}

func openMySQLIntrospectionStubDB(t *testing.T) (*sql.DB, *mysqlIntrospectionStub) {
	t.Helper()

	stub := &mysqlIntrospectionStub{}
	driverName := nextIntrospectionStubDriverName("mysql-introspection-stub")
	sql.Register(driverName, &mysqlStubDriver{stub: stub})

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open stub db: %v", err)
	}
	return db, stub
}

func findSchemaTable(t *testing.T, schema *Schema, sourceName string) *Table {
	t.Helper()

	for i := range schema.Tables {
		if schema.Tables[i].SourceName == sourceName {
			return &schema.Tables[i]
		}
	}
	t.Fatalf("table %q not found", sourceName)
	return nil
}

func TestMySQLIntrospectSchemaBatchesSchemaQueries(t *testing.T) {
	db, stub := openMySQLIntrospectionStubDB(t)
	defer db.Close()

	schema, err := introspectMySQLSchema(db, "appdb", toSnakeCase)
	if err != nil {
		t.Fatalf("introspectMySQLSchema: %v", err)
	}

	if len(stub.queries) != 4 {
		t.Fatalf("query count = %d, want 4", len(stub.queries))
	}
	for i, call := range stub.queries {
		if len(call.args) != 1 || call.args[0] != "appdb" {
			t.Fatalf("query %d args = %#v, want [appdb]", i, call.args)
		}
		if strings.Contains(call.query, "TABLE_NAME = ?") {
			t.Fatalf("query %d still filters per table: %s", i, call.query)
		}
	}

	if len(schema.Tables) != 3 {
		t.Fatalf("table count = %d, want 3", len(schema.Tables))
	}
	if got := []string{schema.Tables[0].SourceName, schema.Tables[1].SourceName, schema.Tables[2].SourceName}; strings.Join(got, ",") != "Accounts,AuditTrail,OrderVersions" {
		t.Fatalf("table order = %v, want [Accounts AuditTrail OrderVersions]", got)
	}

	accounts := findSchemaTable(t, schema, "Accounts")
	if accounts.PGName != "accounts" {
		t.Fatalf("Accounts PGName = %q, want accounts", accounts.PGName)
	}
	if accounts.PrimaryKey == nil {
		t.Fatal("Accounts primary key = nil")
	}
	if got := strings.Join(accounts.PrimaryKey.Columns, ","); got != "account_id" {
		t.Fatalf("Accounts PK columns = %v, want [account_id]", accounts.PrimaryKey.Columns)
	}
	if len(accounts.Indexes) != 0 {
		t.Fatalf("Accounts indexes = %v, want none", accounts.Indexes)
	}
	if len(accounts.ForeignKeys) != 0 {
		t.Fatalf("Accounts foreign keys = %v, want none", accounts.ForeignKeys)
	}

	orderVersions := findSchemaTable(t, schema, "OrderVersions")
	if len(orderVersions.Columns) != 5 {
		t.Fatalf("OrderVersions columns = %d, want 5", len(orderVersions.Columns))
	}
	if orderVersions.PrimaryKey == nil {
		t.Fatal("OrderVersions primary key = nil")
	}
	if got := strings.Join(orderVersions.PrimaryKey.Columns, ","); got != "order_id,version_no" {
		t.Fatalf("OrderVersions PK columns = %v, want [order_id version_no]", orderVersions.PrimaryKey.Columns)
	}
	if len(orderVersions.Indexes) != 3 {
		t.Fatalf("OrderVersions indexes = %d, want 3", len(orderVersions.Indexes))
	}
	if got := []string{
		orderVersions.Indexes[0].SourceName,
		orderVersions.Indexes[1].SourceName,
		orderVersions.Indexes[2].SourceName,
	}; strings.Join(got, ",") != "ft_status,idx_account_status,idx_label_expr" {
		t.Fatalf("OrderVersions index order = %v, want [ft_status idx_account_status idx_label_expr]", got)
	}
	if orderVersions.Indexes[0].Type != "FULLTEXT" {
		t.Fatalf("ft_status type = %q, want FULLTEXT", orderVersions.Indexes[0].Type)
	}
	if !orderVersions.Indexes[1].HasPrefix {
		t.Fatal("idx_account_status HasPrefix = false, want true")
	}
	if got := strings.Join(orderVersions.Indexes[1].ColumnOrders, ","); got != "DESC,ASC" {
		t.Fatalf("idx_account_status orders = %v, want [DESC ASC]", orderVersions.Indexes[1].ColumnOrders)
	}
	if !orderVersions.Indexes[2].HasExpression {
		t.Fatal("idx_label_expr HasExpression = false, want true")
	}

	statusCode := orderVersions.Columns[3]
	if statusCode.PGName != "status_code" {
		t.Fatalf("StatusCode PGName = %q, want status_code", statusCode.PGName)
	}
	if statusCode.Default == nil || *statusCode.Default != "new" {
		t.Fatalf("StatusCode default = %v, want new", statusCode.Default)
	}
	if statusCode.Charset != "utf8mb4" || statusCode.Collation != "utf8mb4_bin" {
		t.Fatalf("StatusCode charset/collation = %q/%q, want utf8mb4/utf8mb4_bin", statusCode.Charset, statusCode.Collation)
	}

	displayLabel := orderVersions.Columns[4]
	if !isMySQLGeneratedColumn(displayLabel) {
		t.Fatal("DisplayLabel should be detected as generated")
	}
	if displayLabel.GenerationExpression != "concat(`OrderID`,'-',`VersionNo`)" {
		t.Fatalf("DisplayLabel expression = %q", displayLabel.GenerationExpression)
	}

	auditTrail := findSchemaTable(t, schema, "AuditTrail")
	if len(auditTrail.ForeignKeys) != 2 {
		t.Fatalf("AuditTrail foreign keys = %d, want 2", len(auditTrail.ForeignKeys))
	}
	if got := []string{auditTrail.ForeignKeys[0].Name, auditTrail.ForeignKeys[1].Name}; strings.Join(got, ",") != "fk_audit_actor,fk_audit_order" {
		t.Fatalf("AuditTrail FK order = %v, want [fk_audit_actor fk_audit_order]", got)
	}
	if got := strings.Join(auditTrail.ForeignKeys[1].Columns, ","); got != "order_id,version_no" {
		t.Fatalf("fk_audit_order columns = %v, want [order_id version_no]", auditTrail.ForeignKeys[1].Columns)
	}
	if got := strings.Join(auditTrail.ForeignKeys[1].RefColumns, ","); got != "order_id,version_no" {
		t.Fatalf("fk_audit_order ref columns = %v, want [order_id version_no]", auditTrail.ForeignKeys[1].RefColumns)
	}
	if auditTrail.ForeignKeys[1].RefPGTable != "order_versions" {
		t.Fatalf("fk_audit_order ref pg table = %q, want order_versions", auditTrail.ForeignKeys[1].RefPGTable)
	}
	if auditTrail.ForeignKeys[1].UpdateRule != "CASCADE" || auditTrail.ForeignKeys[1].DeleteRule != "CASCADE" {
		t.Fatalf("fk_audit_order rules = %q/%q, want CASCADE/CASCADE", auditTrail.ForeignKeys[1].UpdateRule, auditTrail.ForeignKeys[1].DeleteRule)
	}
}
