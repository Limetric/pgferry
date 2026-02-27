package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteMapType(t *testing.T) {
	tests := []struct {
		name string
		col  Column
		want string
		err  bool
	}{
		{"INTEGER→bigint", Column{DataType: "integer", ColumnType: "INTEGER"}, "bigint", false},
		{"INT→bigint", Column{DataType: "int", ColumnType: "INT"}, "bigint", false},
		{"SMALLINT→bigint", Column{DataType: "smallint", ColumnType: "SMALLINT"}, "bigint", false},
		{"BIGINT→bigint", Column{DataType: "bigint", ColumnType: "BIGINT"}, "bigint", false},
		{"REAL→double precision", Column{DataType: "real", ColumnType: "REAL"}, "double precision", false},
		{"DOUBLE→double precision", Column{DataType: "double", ColumnType: "DOUBLE"}, "double precision", false},
		{"TEXT→text", Column{DataType: "text", ColumnType: "TEXT"}, "text", false},
		{"VARCHAR→text", Column{DataType: "varchar", ColumnType: "VARCHAR(255)"}, "text", false},
		{"BLOB→bytea", Column{DataType: "blob", ColumnType: "BLOB"}, "bytea", false},
		{"NUMERIC→numeric", Column{DataType: "numeric", ColumnType: "NUMERIC"}, "numeric", false},
		{"NUMERIC(10,2)", Column{DataType: "numeric", ColumnType: "NUMERIC(10,2)", Precision: 10, Scale: 2}, "numeric(10,2)", false},
		{"BOOLEAN→boolean", Column{DataType: "boolean", ColumnType: "BOOLEAN"}, "boolean", false},
		{"DATETIME→timestamp", Column{DataType: "datetime", ColumnType: "DATETIME"}, "timestamp", false},
		{"DATE→date", Column{DataType: "date", ColumnType: "DATE"}, "date", false},
		{"JSON→json", Column{DataType: "json", ColumnType: "JSON"}, "json", false},
		{"JSON→jsonb opt-in", Column{DataType: "json", ColumnType: "JSON"}, "jsonb", false},
		{"unknown→error", Column{DataType: "foobar", ColumnType: "FOOBAR"}, "", true},
		{"unknown→text opt-in", Column{DataType: "foobar", ColumnType: "FOOBAR"}, "text", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := defaultTypeMappingConfig()
			if tt.name == "JSON→jsonb opt-in" {
				tm.JSONAsJSONB = true
			}
			if tt.name == "unknown→text opt-in" {
				tm.UnknownAsText = true
			}
			got, err := sqliteMapType(tt.col, tm)
			if tt.err {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("sqliteMapType(%q) = %q, want %q", tt.col.ColumnType, got, tt.want)
			}
		})
	}
}

func TestSQLiteTransformValue(t *testing.T) {
	src := &sqliteSourceDB{}
	tm := defaultTypeMappingConfig()
	col := Column{DataType: "text"}

	// nil → nil
	got, err := src.TransformValue(nil, col, tm)
	if err != nil || got != nil {
		t.Errorf("TransformValue(nil) = %v, want nil", got)
	}

	// string passthrough
	got, err = src.TransformValue("hello", col, tm)
	if err != nil || got != "hello" {
		t.Errorf("TransformValue(string) = %v, want hello", got)
	}

	// int64 passthrough
	got, err = src.TransformValue(int64(42), col, tm)
	if err != nil || got != int64(42) {
		t.Errorf("TransformValue(int64) = %v, want 42", got)
	}
}

func TestSQLiteQuoteIdentifier(t *testing.T) {
	src := &sqliteSourceDB{}

	tests := []struct {
		in, want string
	}{
		{"users", `"users"`},
		{`my"table`, `"my""table"`},
		{"simple", `"simple"`},
	}
	for _, tt := range tests {
		got := src.QuoteIdentifier(tt.in)
		if got != tt.want {
			t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSQLiteReadOnlyURI(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
		err  bool
	}{
		{"plain path", "/data/app.db", "file:/data/app.db?mode=ro", false},
		{"relative path", "./relative.db", "file:./relative.db?mode=ro", false},
		{"file URI no params", "file:/data/app.db", "file:/data/app.db?mode=ro", false},
		{"file URI with params", "file:/data/app.db?cache=shared", "file:/data/app.db?cache=shared&mode=ro", false},
		{"memory rejected", ":memory:", "", true},
		{"file memory rejected", "file::memory:", "", true},
		{"mode=memory rejected", "file:test.db?mode=memory", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sqliteReadOnlyURI(tt.dsn)
			if tt.err {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("sqliteReadOnlyURI(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

func TestSQLiteExtractDBName(t *testing.T) {
	src := &sqliteSourceDB{}

	tests := []struct {
		dsn, want string
	}{
		{"/data/app.db", "app"},
		{"./mydata.sqlite", "mydata"},
		{"/tmp/test.db", "test"},
		{"file:/data/app.db", "app"},
	}
	for _, tt := range tests {
		got, err := src.ExtractDBName(tt.dsn)
		if err != nil {
			t.Fatalf("ExtractDBName(%q) error: %v", tt.dsn, err)
		}
		if got != tt.want {
			t.Errorf("ExtractDBName(%q) = %q, want %q", tt.dsn, got, tt.want)
		}
	}
}

func TestSQLiteIntrospectSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT
		)`,
		`CREATE TABLE posts (
			id INTEGER PRIMARY KEY,
			user_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX idx_posts_title ON posts(title)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt[:min(len(stmt), 40)], err)
		}
	}

	src := &sqliteSourceDB{}
	schema, err := src.IntrospectSchema(db, "")
	if err != nil {
		t.Fatalf("IntrospectSchema: %v", err)
	}

	if len(schema.Tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(schema.Tables))
	}

	// Check users table
	var users *Table
	for i := range schema.Tables {
		if schema.Tables[i].SourceName == "users" {
			users = &schema.Tables[i]
		}
	}
	if users == nil {
		t.Fatal("users table not found")
	}

	if len(users.Columns) != 3 {
		t.Fatalf("users: expected 3 columns, got %d", len(users.Columns))
	}
	if users.PrimaryKey == nil {
		t.Fatal("users: expected primary key")
	}
	if len(users.PrimaryKey.Columns) != 1 || users.PrimaryKey.Columns[0] != "id" {
		t.Errorf("users PK columns = %v, want [id]", users.PrimaryKey.Columns)
	}

	// Check auto_increment detection
	idCol := users.Columns[0]
	if !strings.Contains(idCol.Extra, "auto_increment") {
		t.Errorf("users.id Extra = %q, want to contain auto_increment", idCol.Extra)
	}

	// Check posts table
	var posts *Table
	for i := range schema.Tables {
		if schema.Tables[i].SourceName == "posts" {
			posts = &schema.Tables[i]
		}
	}
	if posts == nil {
		t.Fatal("posts table not found")
	}

	if len(posts.ForeignKeys) != 1 {
		t.Fatalf("posts: expected 1 FK, got %d", len(posts.ForeignKeys))
	}
	fk := posts.ForeignKeys[0]
	if fk.RefTable != "users" {
		t.Errorf("posts FK ref table = %q, want users", fk.RefTable)
	}

	// Check index on posts
	if len(posts.Indexes) != 1 {
		t.Fatalf("posts: expected 1 non-PK index, got %d", len(posts.Indexes))
	}
	idx := posts.Indexes[0]
	if idx.SourceName != "idx_posts_title" {
		t.Errorf("posts index name = %q, want idx_posts_title", idx.SourceName)
	}
}

func TestSQLiteIntrospectCompositePK(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE tag_map (
		tag_id INTEGER NOT NULL,
		item_id INTEGER NOT NULL,
		PRIMARY KEY (tag_id, item_id)
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	src := &sqliteSourceDB{}
	schema, err := src.IntrospectSchema(db, "")
	if err != nil {
		t.Fatalf("IntrospectSchema: %v", err)
	}

	if len(schema.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d", len(schema.Tables))
	}

	tbl := schema.Tables[0]
	if tbl.PrimaryKey == nil {
		t.Fatal("expected PK for composite key table")
	}
	if len(tbl.PrimaryKey.Columns) != 2 {
		t.Fatalf("PK columns = %v, want 2 columns", tbl.PrimaryKey.Columns)
	}
}

func TestSQLiteIntrospectSourceObjects(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	stmts := []string{
		"CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE VIEW v_users AS SELECT id, name FROM users",
		"CREATE TRIGGER trg_users AFTER INSERT ON users BEGIN SELECT 1; END",
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}

	src := &sqliteSourceDB{}
	objs, err := src.IntrospectSourceObjects(db, "")
	if err != nil {
		t.Fatalf("IntrospectSourceObjects: %v", err)
	}

	if len(objs.Views) != 1 || objs.Views[0] != "v_users" {
		t.Errorf("views = %v, want [v_users]", objs.Views)
	}
	if len(objs.Triggers) != 1 || objs.Triggers[0] != "trg_users" {
		t.Errorf("triggers = %v, want [trg_users]", objs.Triggers)
	}
	if len(objs.Routines) != 0 {
		t.Errorf("routines = %v, want empty", objs.Routines)
	}
}

func TestSQLiteValidateTypeMapping(t *testing.T) {
	src := &sqliteSourceDB{}

	// Default config should be fine
	if err := src.ValidateTypeMapping(defaultTypeMappingConfig()); err != nil {
		t.Fatalf("default type mapping should be valid: %v", err)
	}

	// MySQL-only options should fail
	tm := defaultTypeMappingConfig()
	tm.TinyInt1AsBoolean = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for tinyint1_as_boolean")
	}

	tm = defaultTypeMappingConfig()
	tm.Binary16AsUUID = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for binary16_as_uuid")
	}
}

func TestSQLiteMapDefault(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		pgType string
		want   string
	}{
		{"NULL", "NULL", "text", ""},
		{"null lowercase", "null", "text", ""},
		{"CURRENT_TIMESTAMP", "CURRENT_TIMESTAMP", "timestamp", "CURRENT_TIMESTAMP"},
		{"CURRENT_DATE", "CURRENT_DATE", "date", "CURRENT_DATE"},
		{"numeric", "42", "bigint", "42"},
		{"negative numeric", "-1", "bigint", "-1"},
		{"float numeric", "3.14", "double precision", "3.14"},
		{"boolean 0", "0", "boolean", "FALSE"},
		{"boolean 1", "1", "boolean", "TRUE"},
		{"string literal", "'hello'", "text", "'hello'"},
		{"string with quotes", "'it''s'", "text", "'it''s'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := Column{Default: &tt.raw, SourceName: "test_col"}
			got, err := sqliteMapDefault(col, tt.pgType)
			if err != nil {
				t.Fatalf("sqliteMapDefault() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("sqliteMapDefault(%q, %q) = %q, want %q", tt.raw, tt.pgType, got, tt.want)
			}
		})
	}
}

func TestSQLiteOpenDB_RejectsMemory(t *testing.T) {
	src := &sqliteSourceDB{}
	_, err := src.OpenDB(":memory:")
	if err == nil {
		t.Fatal("expected error for :memory: DSN")
	}
}

func TestSQLiteOpenDB_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Create the file first
	f, err := os.Create(dbPath)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	f.Close()

	// Write something so it's a valid SQLite DB
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open for init: %v", err)
	}
	db.Exec("CREATE TABLE t(x)")
	db.Close()

	src := &sqliteSourceDB{}
	roDB, err := src.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB() error: %v", err)
	}
	defer roDB.Close()

	if err := roDB.Ping(); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}
