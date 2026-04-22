package main

import (
	"database/sql"
	"testing"
)

func TestNewConfiguredSourceDB_MSSQLSourceSchema(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mssql"
	cfg.Source.SourceSchema = "sales"
	cfg.IdentifierCase = "snake"

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	mssqlSrc, ok := src.(*mssqlSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *mssqlSourceDB", src)
	}

	if mssqlSrc.sourceSchema != "sales" {
		t.Fatalf("sourceSchema = %q, want sales", mssqlSrc.sourceSchema)
	}
	if mssqlSrc.identCase != "snake" {
		t.Fatalf("identCase = %q, want snake", mssqlSrc.identCase)
	}
}

func TestNewConfiguredSourceDB_MySQLAppliesCharsetAndIdentifiers(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.Charset = "latin1"
	cfg.IdentifierCase = "lower"

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	mysqlSrc, ok := src.(*mysqlSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *mysqlSourceDB", src)
	}

	if mysqlSrc.charset != "latin1" {
		t.Fatalf("charset = %q, want latin1", mysqlSrc.charset)
	}
	if mysqlSrc.identCase != "lower" {
		t.Fatalf("identCase = %q, want lower", mysqlSrc.identCase)
	}
}

func TestNewConfiguredSourceDB_MariaDBAppliesCharsetAndIdentifiers(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mariadb"
	cfg.Source.Charset = "latin1"
	cfg.IdentifierCase = "lower"

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	mariaSrc, ok := src.(*mariadbSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *mariadbSourceDB", src)
	}

	if mariaSrc.charset != "latin1" {
		t.Fatalf("charset = %q, want latin1", mariaSrc.charset)
	}
	if mariaSrc.identCase != "lower" {
		t.Fatalf("identCase = %q, want lower", mariaSrc.identCase)
	}
	if mariaSrc.Name() != "MariaDB" {
		t.Fatalf("Name() = %q, want MariaDB", mariaSrc.Name())
	}
}

func TestNewConfiguredSourceDB_SQLiteAppliesIdentifiers(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "sqlite"
	cfg.IdentifierCase = "lower"

	src, err := newConfiguredSourceDB(&cfg)
	if err != nil {
		t.Fatalf("newConfiguredSourceDB() error: %v", err)
	}

	sqliteSrc, ok := src.(*sqliteSourceDB)
	if !ok {
		t.Fatalf("source type = %T, want *sqliteSourceDB", src)
	}

	if sqliteSrc.identCase != "lower" {
		t.Fatalf("identCase = %q, want lower", sqliteSrc.identCase)
	}
}

func TestNewConfiguredSourceDB_InvalidType(t *testing.T) {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "oracle"

	_, err := newConfiguredSourceDB(&cfg)
	if err == nil {
		t.Fatal("expected error for invalid source type")
	}
	if got, want := err.Error(), `unsupported source type "oracle" (must be mysql, mariadb, sqlite, or mssql)`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestIsMySQLFamilySourceType(t *testing.T) {
	if !isMySQLFamilySourceType("mysql") {
		t.Fatal("mysql should be MySQL-family")
	}
	if !isMySQLFamilySourceType("mariadb") {
		t.Fatal("mariadb should be MySQL-family")
	}
	if isMySQLFamilySourceType("MySQL") {
		t.Fatal("MySQL should not match non-canonical source.type values")
	}
	if isMySQLFamilySourceType("sqlite") {
		t.Fatal("sqlite should not be MySQL-family")
	}
}

type fakeNamedSource struct {
	name string
}

func (f fakeNamedSource) Name() string                                      { return f.name }
func (f fakeNamedSource) OpenDB(string) (*sql.DB, error)                    { return nil, nil }
func (f fakeNamedSource) ExtractDBName(string) (string, error)              { return "", nil }
func (f fakeNamedSource) IntrospectSchema(*sql.DB, string) (*Schema, error) { return nil, nil }
func (f fakeNamedSource) IntrospectSourceObjects(*sql.DB, string) (*SourceObjects, error) {
	return nil, nil
}
func (f fakeNamedSource) MapType(Column, TypeMappingConfig) (string, error) { return "", nil }
func (f fakeNamedSource) MapDefault(Column, string, TypeMappingConfig) (string, error) {
	return "", nil
}
func (f fakeNamedSource) TransformValue(any, Column, TypeMappingConfig) (any, error) { return nil, nil }
func (f fakeNamedSource) QuoteIdentifier(string) string                              { return "" }
func (f fakeNamedSource) SourceTableRef(Table) string                                { return "" }
func (f fakeNamedSource) SupportsSnapshotMode() bool                                 { return false }
func (f fakeNamedSource) MaxWorkers() int                                            { return 0 }
func (f fakeNamedSource) ValidateTypeMapping(TypeMappingConfig) error                { return nil }
func (f fakeNamedSource) SetIdentifierCase(string)                                   {}
func (f fakeNamedSource) SetCharset(string)                                          {}
func (f fakeNamedSource) SetSourceSchema(string)                                     {}

func TestSourceTypeForDB_FallsBackToNameForTestDoubles(t *testing.T) {
	if got := sourceTypeForDB(fakeNamedSource{name: "MariaDB"}); got != "mariadb" {
		t.Fatalf("sourceTypeForDB(fake MariaDB) = %q, want mariadb", got)
	}
}

func TestMySQLIdentName_PreserveMode(t *testing.T) {
	src := &mysqlSourceDB{}
	src.SetIdentifierCase("preserve")
	cases := []struct{ in, want string }{
		{"SomeProducts", "SomeProducts"},
		{"SomeProductId", "SomeProductId"},
		{"HTMLParser", "HTMLParser"},
		{"user_id", "user_id"},
	}
	for _, tc := range cases {
		if got := src.identName(tc.in); got != tc.want {
			t.Errorf("identName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSQLiteIdentName_PreserveMode(t *testing.T) {
	src := &sqliteSourceDB{}
	src.SetIdentifierCase("preserve")
	if got := src.identName("MixedCase"); got != "MixedCase" {
		t.Errorf("identName(\"MixedCase\") = %q, want %q", got, "MixedCase")
	}
}

func TestMSSQLIdentName_PreserveMode(t *testing.T) {
	src := &mssqlSourceDB{}
	src.SetIdentifierCase("preserve")
	if got := src.identName("SomeProducts"); got != "SomeProducts" {
		t.Errorf("identName(\"SomeProducts\") = %q, want %q", got, "SomeProducts")
	}
}
