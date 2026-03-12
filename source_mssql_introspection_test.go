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
	"time"
)

type mssqlIntrospectionStub struct {
	mu      sync.Mutex
	queries []mssqlStubQueryCall
}

type mssqlStubQueryCall struct {
	query string
	args  []any
}

type mssqlStubDriver struct {
	stub *mssqlIntrospectionStub
}

type mssqlStubConn struct {
	stub *mssqlIntrospectionStub
}

type mssqlStubStmt struct {
	conn  *mssqlStubConn
	query string
}

type mssqlStubRows struct {
	columns []string
	data    [][]driver.Value
	index   int
}

func (d *mssqlStubDriver) Open(string) (driver.Conn, error) {
	return &mssqlStubConn{stub: d.stub}, nil
}

func (c *mssqlStubConn) Prepare(query string) (driver.Stmt, error) {
	return &mssqlStubStmt{conn: c, query: query}, nil
}

func (c *mssqlStubConn) Close() error { return nil }

func (c *mssqlStubConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("transactions not supported")
}

func (c *mssqlStubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.stub.query(query, args)
}

func (c *mssqlStubConn) Query(query string, args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, arg := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: arg}
	}
	return c.stub.query(query, named)
}

func (s *mssqlStubStmt) Close() error { return nil }

func (s *mssqlStubStmt) NumInput() int { return -1 }

func (s *mssqlStubStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("exec not supported")
}

func (s *mssqlStubStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.Query(s.query, args)
}

func (r *mssqlStubRows) Columns() []string { return r.columns }

func (r *mssqlStubRows) Close() error { return nil }

func (r *mssqlStubRows) Next(dest []driver.Value) error {
	if r.index >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.index])
	r.index++
	return nil
}

func (s *mssqlIntrospectionStub) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	normalized := strings.Join(strings.Fields(query), " ")
	values := make([]any, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}

	s.mu.Lock()
	s.queries = append(s.queries, mssqlStubQueryCall{query: normalized, args: values})
	s.mu.Unlock()

	switch {
	case strings.Contains(normalized, "FROM sys.tables t"):
		return &mssqlStubRows{
			columns: []string{"name"},
			data: [][]driver.Value{
				{"Accounts"},
				{"AuditTrail"},
				{"OrderVersions"},
			},
		}, nil
	case strings.Contains(normalized, "FROM sys.columns c"):
		return &mssqlStubRows{
			columns: []string{
				"table_name", "name", "base_type", "max_length", "precision", "scale",
				"is_nullable", "default_def", "is_identity", "is_computed", "computed_def", "column_id",
			},
			data: [][]driver.Value{
				{"Accounts", "AccountID", "int", int64(4), int64(10), int64(0), false, nil, true, int64(0), "", int64(1)},
				{"Accounts", "UserName", "nvarchar", int64(128), int64(0), int64(0), false, nil, false, int64(0), "", int64(2)},
				{"AuditTrail", "OrderID", "int", int64(4), int64(10), int64(0), false, nil, false, int64(0), "", int64(1)},
				{"AuditTrail", "VersionNo", "int", int64(4), int64(10), int64(0), false, nil, false, int64(0), "", int64(2)},
				{"AuditTrail", "ActorAccountID", "int", int64(4), int64(10), int64(0), false, nil, false, int64(0), "", int64(3)},
				{"OrderVersions", "OrderID", "int", int64(4), int64(10), int64(0), false, nil, false, int64(0), "", int64(1)},
				{"OrderVersions", "VersionNo", "int", int64(4), int64(10), int64(0), false, nil, false, int64(0), "", int64(2)},
				{"OrderVersions", "AccountID", "int", int64(4), int64(10), int64(0), false, nil, false, int64(0), "", int64(3)},
				{"OrderVersions", "StatusText", "nvarchar", int64(64), int64(0), int64(0), false, "('new')", false, int64(0), "", int64(4)},
				{"OrderVersions", "DisplayLabel", "nvarchar", int64(128), int64(0), int64(0), true, nil, false, int64(1), "([OrderID]+'-'+[VersionNo])", int64(5)},
			},
		}, nil
	case strings.Contains(normalized, "FROM sys.indexes i"):
		return &mssqlStubRows{
			columns: []string{
				"table_name", "index_name", "is_unique", "is_primary_key", "type_desc", "has_filter",
				"key_ordinal", "column_name", "is_descending_key", "is_included_column",
			},
			data: [][]driver.Value{
				{"Accounts", "PK_Accounts", true, true, "CLUSTERED", false, int64(1), "AccountID", false, false},
				{"OrderVersions", "PK_OrderVersions", true, true, "CLUSTERED", false, int64(1), "OrderID", false, false},
				{"OrderVersions", "PK_OrderVersions", true, true, "CLUSTERED", false, int64(2), "VersionNo", false, false},
				{"OrderVersions", "IX_OrderVersions_Filtered", false, false, "NONCLUSTERED", true, int64(1), "AccountID", false, false},
				{"OrderVersions", "IX_OrderVersions_Sort", false, false, "NONCLUSTERED", false, int64(1), "AccountID", true, false},
				{"OrderVersions", "IX_OrderVersions_Sort", false, false, "NONCLUSTERED", false, int64(2), "VersionNo", false, false},
				{"OrderVersions", "IX_OrderVersions_Sort", false, false, "NONCLUSTERED", false, int64(0), "DisplayLabel", false, true},
			},
		}, nil
	case strings.Contains(normalized, "FROM sys.foreign_keys fk"):
		return &mssqlStubRows{
			columns: []string{"table_name", "fk_name", "column_name", "ref_table", "ref_column", "update_action", "delete_action", "ref_schema"},
			data: [][]driver.Value{
				{"AuditTrail", "FK_AuditTrail_Accounts", "ActorAccountID", "Accounts", "AccountID", "NO_ACTION", "NO_ACTION", "dbo"},
				{"AuditTrail", "FK_AuditTrail_OrderVersions", "OrderID", "OrderVersions", "OrderID", "CASCADE", "CASCADE", "dbo"},
				{"AuditTrail", "FK_AuditTrail_OrderVersions", "VersionNo", "OrderVersions", "VersionNo", "CASCADE", "CASCADE", "dbo"},
				{"OrderVersions", "FK_OrderVersions_Accounts", "AccountID", "Accounts", "AccountID", "CASCADE", "NO_ACTION", "dbo"},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", normalized)
	}
}

func openMSSQLIntrospectionStubDB(t *testing.T) (*sql.DB, *mssqlIntrospectionStub) {
	t.Helper()

	stub := &mssqlIntrospectionStub{}
	driverName := fmt.Sprintf("mssql-introspection-stub-%d", time.Now().UnixNano())
	sql.Register(driverName, &mssqlStubDriver{stub: stub})

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("open stub db: %v", err)
	}
	return db, stub
}

func TestMSSQLIntrospectSchemaBatchesSchemaQueries(t *testing.T) {
	db, stub := openMSSQLIntrospectionStubDB(t)
	defer db.Close()

	src := &mssqlSourceDB{snakeCaseIDs: true, sourceSchema: "dbo"}
	schema, err := src.IntrospectSchema(db, "")
	if err != nil {
		t.Fatalf("IntrospectSchema: %v", err)
	}

	if len(stub.queries) != 4 {
		t.Fatalf("query count = %d, want 4", len(stub.queries))
	}
	for i, call := range stub.queries {
		if len(call.args) != 1 || call.args[0] != "dbo" {
			t.Fatalf("query %d args = %#v, want [dbo]", i, call.args)
		}
		if strings.Contains(call.query, "t.name = @p2") {
			t.Fatalf("query %d still filters per table: %s", i, call.query)
		}
	}

	if len(schema.Tables) != 3 {
		t.Fatalf("table count = %d, want 3", len(schema.Tables))
	}

	accounts := findSchemaTable(t, schema, "Accounts")
	if accounts.PrimaryKey == nil {
		t.Fatal("Accounts primary key = nil")
	}
	if got := strings.Join(accounts.PrimaryKey.Columns, ","); got != "account_id" {
		t.Fatalf("Accounts PK columns = %v, want [account_id]", accounts.PrimaryKey.Columns)
	}
	if accounts.Columns[0].Extra != "auto_increment" {
		t.Fatalf("Accounts identity Extra = %q, want auto_increment", accounts.Columns[0].Extra)
	}
	if accounts.Columns[1].CharMaxLen != 64 {
		t.Fatalf("Accounts.UserName CharMaxLen = %d, want 64", accounts.Columns[1].CharMaxLen)
	}

	orderVersions := findSchemaTable(t, schema, "OrderVersions")
	if orderVersions.PrimaryKey == nil {
		t.Fatal("OrderVersions primary key = nil")
	}
	if got := strings.Join(orderVersions.PrimaryKey.Columns, ","); got != "order_id,version_no" {
		t.Fatalf("OrderVersions PK columns = %v, want [order_id version_no]", orderVersions.PrimaryKey.Columns)
	}
	if len(orderVersions.Indexes) != 2 {
		t.Fatalf("OrderVersions indexes = %d, want 2", len(orderVersions.Indexes))
	}
	if !orderVersions.Indexes[0].HasExpression {
		t.Fatal("filtered index should be marked unsupported")
	}
	if got := strings.Join(orderVersions.Indexes[1].ColumnOrders, ","); got != "DESC,ASC" {
		t.Fatalf("IX_OrderVersions_Sort orders = %v, want [DESC ASC]", orderVersions.Indexes[1].ColumnOrders)
	}
	if got := strings.Join(orderVersions.Indexes[1].Columns, ","); got != "account_id,version_no" {
		t.Fatalf("IX_OrderVersions_Sort columns = %v, want [account_id version_no]", orderVersions.Indexes[1].Columns)
	}
	if orderVersions.Columns[3].Default == nil || *orderVersions.Columns[3].Default != "'new'" {
		t.Fatalf("StatusText default = %v, want 'new'", orderVersions.Columns[3].Default)
	}
	if orderVersions.Columns[4].Extra != "COMPUTED" {
		t.Fatalf("DisplayLabel Extra = %q, want COMPUTED", orderVersions.Columns[4].Extra)
	}
	if orderVersions.Columns[4].GenerationExpression != "([OrderID]+'-'+[VersionNo])" {
		t.Fatalf("DisplayLabel expression = %q", orderVersions.Columns[4].GenerationExpression)
	}

	auditTrail := findSchemaTable(t, schema, "AuditTrail")
	if len(auditTrail.ForeignKeys) != 2 {
		t.Fatalf("AuditTrail foreign keys = %d, want 2", len(auditTrail.ForeignKeys))
	}
	if got := strings.Join(auditTrail.ForeignKeys[1].Columns, ","); got != "order_id,version_no" {
		t.Fatalf("FK_AuditTrail_OrderVersions columns = %v, want [order_id version_no]", auditTrail.ForeignKeys[1].Columns)
	}
	if got := auditTrail.ForeignKeys[1].UpdateRule + "/" + auditTrail.ForeignKeys[1].DeleteRule; got != "CASCADE/CASCADE" {
		t.Fatalf("FK_AuditTrail_OrderVersions rules = %s, want CASCADE/CASCADE", got)
	}
}
