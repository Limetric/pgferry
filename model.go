package main

// Column represents a single column from MySQL INFORMATION_SCHEMA.
type Column struct {
	MySQLName  string
	PGName     string
	DataType   string // e.g. "binary", "int", "varchar"
	ColumnType string // full type e.g. "tinyint(1)", "enum('a','b')"
	CharMaxLen int64
	Precision  int64
	Scale      int64
	Nullable   bool
	Default    *string
	Extra      string // e.g. "auto_increment", "on update CURRENT_TIMESTAMP"
	OrdinalPos int
}

// Index represents a MySQL index (may span multiple columns).
type Index struct {
	Name      string
	Columns   []string // PG column names, ordered by SEQ_IN_INDEX
	Unique    bool
	IsPrimary bool
}

// ForeignKey represents a MySQL foreign key constraint.
type ForeignKey struct {
	Name             string
	Columns          []string // local PG column names
	RefTable         string   // referenced MySQL table name
	RefPGTable       string   // referenced PG table name (snake_case)
	RefColumns       []string // referenced PG column names
	UpdateRule       string   // CASCADE, SET NULL, etc.
	DeleteRule       string   // CASCADE, SET NULL, etc.
}

// Table holds the full introspected definition of a MySQL table.
type Table struct {
	MySQLName   string
	PGName      string
	Columns     []Column
	PrimaryKey  *Index
	Indexes     []Index // non-primary indexes
	ForeignKeys []ForeignKey
}

// Schema holds all introspected tables for a MySQL database.
type Schema struct {
	Tables []Table
}
