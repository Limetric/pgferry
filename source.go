package main

import (
	"database/sql"
	"fmt"
)

// SourceDB abstracts source database operations so pgferry can support
// multiple source engines (MySQL, SQLite, etc.).
type SourceDB interface {
	// Name returns a human-readable name for the source ("MySQL", "SQLite").
	Name() string

	// OpenDB opens a database connection with driver-specific options.
	OpenDB(dsn string) (*sql.DB, error)

	// ExtractDBName extracts a logical database name from the DSN (for logging).
	ExtractDBName(dsn string) (string, error)

	// IntrospectSchema reads all tables, columns, indexes, and foreign keys.
	IntrospectSchema(db *sql.DB, dbName string) (*Schema, error)

	// IntrospectSourceObjects discovers views, routines, triggers that need manual migration.
	IntrospectSourceObjects(db *sql.DB, dbName string) (*SourceObjects, error)

	// MapType returns the PostgreSQL type for a source column.
	MapType(col Column, typeMap TypeMappingConfig) (string, error)

	// MapDefault returns the PostgreSQL DEFAULT expression for a source column.
	MapDefault(col Column, pgType string, typeMap TypeMappingConfig) (string, error)

	// TransformValue converts a source row value to its PostgreSQL equivalent.
	TransformValue(val any, col Column, typeMap TypeMappingConfig) (any, error)

	// QuoteIdentifier quotes a source identifier for use in queries.
	QuoteIdentifier(name string) string

	// SupportsSnapshotMode reports whether single_tx snapshot mode is supported.
	SupportsSnapshotMode() bool

	// MaxWorkers returns the maximum number of parallel workers.
	// 0 means use the config value; >0 caps workers to this value.
	MaxWorkers() int

	// ValidateTypeMapping checks for source-specific type mapping options that are invalid.
	ValidateTypeMapping(typeMap TypeMappingConfig) error

	// SetSnakeCaseIdentifiers enables or disables snake_case conversion for source identifiers.
	// When false (default), identifiers are lowercased to match PostgreSQL's default case folding.
	SetSnakeCaseIdentifiers(enabled bool)

	// SetCharset sets the character set for the source connection.
	// For MySQL, this is injected into the DSN. For SQLite, this is a no-op.
	SetCharset(charset string)
}

// newSourceDB returns a SourceDB implementation for the given source type.
func newSourceDB(sourceType string) (SourceDB, error) {
	switch sourceType {
	case "mysql":
		return &mysqlSourceDB{}, nil
	case "sqlite":
		return &sqliteSourceDB{}, nil
	default:
		return nil, fmt.Errorf("unsupported source type %q (must be mysql or sqlite)", sourceType)
	}
}
