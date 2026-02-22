package main

// Column represents a single column from the source database.
type Column struct {
	SourceName string
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

// Index represents a source database index (may span multiple columns).
type Index struct {
	Name          string
	SourceName    string
	Columns       []string // PG column names, ordered by SEQ_IN_INDEX
	ColumnOrders  []string // ASC/DESC order per column
	Unique        bool
	IsPrimary     bool
	Type          string // BTREE, FULLTEXT, SPATIAL, HASH
	HasPrefix     bool   // MySQL prefix index (SUB_PART)
	HasExpression bool   // expression/key-part index not representable as plain column list
}

// ForeignKey represents a source database foreign key constraint.
type ForeignKey struct {
	Name       string
	Columns    []string // local PG column names
	RefTable   string   // referenced MySQL table name
	RefPGTable string   // referenced PG table name (snake_case)
	RefColumns []string // referenced PG column names
	UpdateRule string   // CASCADE, SET NULL, etc.
	DeleteRule string   // CASCADE, SET NULL, etc.
}

// Table holds the full introspected definition of a source database table.
type Table struct {
	SourceName  string
	PGName      string
	Columns     []Column
	PrimaryKey  *Index
	Indexes     []Index // non-primary indexes
	ForeignKeys []ForeignKey
}

// Schema holds all introspected tables for a source database.
type Schema struct {
	Tables []Table
}
