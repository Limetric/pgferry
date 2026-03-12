package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMSSQLMapType(t *testing.T) {
	tests := []struct {
		name    string
		col     Column
		typeMap TypeMappingConfig
		want    string
		err     bool
	}{
		// Integer types
		{"int→integer", Column{DataType: "int"}, defaultTypeMappingConfig(), "integer", false},
		{"bigint→bigint", Column{DataType: "bigint"}, defaultTypeMappingConfig(), "bigint", false},
		{"smallint→smallint", Column{DataType: "smallint"}, defaultTypeMappingConfig(), "smallint", false},
		{"tinyint→smallint", Column{DataType: "tinyint"}, defaultTypeMappingConfig(), "smallint", false},
		{"bit→boolean", Column{DataType: "bit"}, defaultTypeMappingConfig(), "boolean", false},

		// Numeric types
		{"decimal(10,2)→numeric(10,2)", Column{DataType: "decimal", Precision: 10, Scale: 2}, defaultTypeMappingConfig(), "numeric(10,2)", false},
		{"numeric(18,0)→numeric(18,0)", Column{DataType: "numeric", Precision: 18, Scale: 0}, defaultTypeMappingConfig(), "numeric(18,0)", false},
		{"float→double precision", Column{DataType: "float"}, defaultTypeMappingConfig(), "double precision", false},
		{"real→real", Column{DataType: "real"}, defaultTypeMappingConfig(), "real", false},

		// Money types
		{"money→numeric(19,4) default", Column{DataType: "money"}, defaultTypeMappingConfig(), "numeric(19,4)", false},
		{"smallmoney→numeric(10,4) default", Column{DataType: "smallmoney"}, defaultTypeMappingConfig(), "numeric(10,4)", false},
		{"money→text when disabled", Column{DataType: "money"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.MoneyAsNumeric = false; return t }(), "text", false},
		{"smallmoney→text when disabled", Column{DataType: "smallmoney"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.MoneyAsNumeric = false; return t }(), "text", false},

		// Character types
		{"char(10)→char(10)", Column{DataType: "char", CharMaxLen: 10}, defaultTypeMappingConfig(), "char(10)", false},
		{"varchar(255)→varchar(255)", Column{DataType: "varchar", CharMaxLen: 255}, defaultTypeMappingConfig(), "varchar(255)", false},
		{"varchar(max)→text", Column{DataType: "varchar", CharMaxLen: -1}, defaultTypeMappingConfig(), "text", false},
		{"nchar(10)→char(10)", Column{DataType: "nchar", CharMaxLen: 10}, defaultTypeMappingConfig(), "char(10)", false},
		{"nvarchar(100)→varchar(100)", Column{DataType: "nvarchar", CharMaxLen: 100}, defaultTypeMappingConfig(), "varchar(100)", false},
		{"nvarchar(max)→text", Column{DataType: "nvarchar", CharMaxLen: -1}, defaultTypeMappingConfig(), "text", false},
		{"nvarchar→text when nvarchar_as_text", Column{DataType: "nvarchar", CharMaxLen: 100}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.NvarcharAsText = true; return t }(), "text", false},
		{"nchar→text when nvarchar_as_text", Column{DataType: "nchar", CharMaxLen: 10}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.NvarcharAsText = true; return t }(), "text", false},
		{"text→text", Column{DataType: "text"}, defaultTypeMappingConfig(), "text", false},
		{"ntext→text", Column{DataType: "ntext"}, defaultTypeMappingConfig(), "text", false},

		// Binary types
		{"binary(16)→bytea", Column{DataType: "binary", CharMaxLen: 16}, defaultTypeMappingConfig(), "bytea", false},
		{"varbinary→bytea", Column{DataType: "varbinary", CharMaxLen: -1}, defaultTypeMappingConfig(), "bytea", false},
		{"image→bytea", Column{DataType: "image"}, defaultTypeMappingConfig(), "bytea", false},

		// Date/time types
		{"date→date", Column{DataType: "date"}, defaultTypeMappingConfig(), "date", false},
		{"time→time", Column{DataType: "time"}, defaultTypeMappingConfig(), "time", false},
		{"datetime→timestamp", Column{DataType: "datetime"}, defaultTypeMappingConfig(), "timestamp", false},
		{"datetime2→timestamp", Column{DataType: "datetime2"}, defaultTypeMappingConfig(), "timestamp", false},
		{"datetime→timestamptz", Column{DataType: "datetime"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.DatetimeAsTimestamptz = true; return t }(), "timestamptz", false},
		{"datetime2→timestamptz", Column{DataType: "datetime2"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.DatetimeAsTimestamptz = true; return t }(), "timestamptz", false},
		{"smalldatetime→timestamp", Column{DataType: "smalldatetime"}, defaultTypeMappingConfig(), "timestamp", false},
		{"smalldatetime→timestamptz", Column{DataType: "smalldatetime"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.DatetimeAsTimestamptz = true; return t }(), "timestamptz", false},
		{"datetimeoffset→timestamptz", Column{DataType: "datetimeoffset"}, defaultTypeMappingConfig(), "timestamptz", false},

		// MSSQL timestamp is NOT a datetime — it's rowversion
		{"timestamp→bytea", Column{DataType: "timestamp"}, defaultTypeMappingConfig(), "bytea", false},

		// Special types
		{"uniqueidentifier→uuid", Column{DataType: "uniqueidentifier"}, defaultTypeMappingConfig(), "uuid", false},
		{"xml→xml", Column{DataType: "xml"}, defaultTypeMappingConfig(), "xml", false},
		{"xml→text when xml_as_text", Column{DataType: "xml"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.XmlAsText = true; return t }(), "text", false},
		{"sql_variant→text", Column{DataType: "sql_variant"}, defaultTypeMappingConfig(), "text", false},
		{"hierarchyid→text", Column{DataType: "hierarchyid"}, defaultTypeMappingConfig(), "text", false},
		{"json→json", Column{DataType: "json"}, defaultTypeMappingConfig(), "json", false},
		{"json→jsonb", Column{DataType: "json"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.JSONAsJSONB = true; return t }(), "jsonb", false},

		// Spatial types
		{"geography wkt_text→text", Column{DataType: "geography"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.SpatialMode = "wkt_text"; return t }(), "text", false},
		{"geometry wkb_bytea→bytea", Column{DataType: "geometry"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.SpatialMode = "wkb_bytea"; return t }(), "bytea", false},
		{"geography off→error", Column{DataType: "geography"}, defaultTypeMappingConfig(), "", true},
		{"geography off→text when unknown_as_text", Column{DataType: "geography"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.UnknownAsText = true; return t }(), "text", false},

		// Unknown type
		{"unknown→error", Column{DataType: "cursor"}, defaultTypeMappingConfig(), "", true},
		{"unknown→text when unknown_as_text", Column{DataType: "cursor"}, func() TypeMappingConfig { t := defaultTypeMappingConfig(); t.UnknownAsText = true; return t }(), "text", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mssqlMapType(tt.col, tt.typeMap)
			if tt.err {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("mssqlMapType(%q) = %q, want %q", tt.col.DataType, got, tt.want)
			}
		})
	}
}

func TestMSSQLMapDefault(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		pgType string
		want   string
	}{
		// Paren stripping (should already be done in introspection, but MapDefault strips too)
		{"stripped numeric", "0", "integer", "0"},
		{"stripped float", "3.14", "numeric(10,2)", "3.14"},

		// NULL
		{"null", "null", "text", ""},
		{"NULL", "NULL", "text", ""},

		// Function mapping
		{"getdate", "getdate()", "timestamp", "CURRENT_TIMESTAMP"},
		{"sysdatetime", "sysdatetime()", "timestamp", "CURRENT_TIMESTAMP"},
		{"sysutcdatetime", "sysutcdatetime()", "timestamptz", "CURRENT_TIMESTAMP"},
		{"newid", "newid()", "uuid", "gen_random_uuid()"},
		{"newsequentialid", "newsequentialid()", "uuid", "gen_random_uuid()"},
		{"getutcdate", "getutcdate()", "timestamp", "CURRENT_TIMESTAMP"},
		{"suser_sname", "suser_sname()", "text", "CURRENT_USER"},
		{"user_name", "user_name()", "text", "CURRENT_USER"},

		// Boolean defaults
		{"bit 0→FALSE", "0", "boolean", "FALSE"},
		{"bit 1→TRUE", "1", "boolean", "TRUE"},

		// Unicode string literal
		{"N'hello'→'hello'", "N'hello'", "text", "'hello'"},
		{"n'world'→'world'", "n'world'", "text", "'world'"},

		// Regular string literal
		{"string literal", "'hello'", "text", "'hello'"},
		{"string with quotes", "'it''s'", "text", "'it''s'"},

		// Bytea/JSON defaults not supported
		{"bytea default", "0x00", "bytea", ""},
		{"json default", "{}", "json", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := Column{Default: &tt.raw, SourceName: "test_col"}
			got, err := mssqlMapDefault(col, tt.pgType, defaultTypeMappingConfig())
			if err != nil {
				t.Fatalf("mssqlMapDefault() error: %v", err)
			}
			if got != tt.want {
				t.Errorf("mssqlMapDefault(%q, %q) = %q, want %q", tt.raw, tt.pgType, got, tt.want)
			}
		})
	}
}

func TestMSSQLStripParens(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"((0))", "0"},
		{"(getdate())", "getdate()"},
		{"(N'hello')", "N'hello'"},
		{"((0.00))", "0.00"},
		{"plain", "plain"},
		{"(single)", "single"},
		{"((double))", "double"},
		// Compound expressions: outer parens are balanced but inner aren't a wrapping pair
		{"((1)+(2))", "(1)+(2)"},
		{"((1)*(3))", "(1)*(3)"},
		{"(a()+(b()))", "a()+(b())"},
	}
	for _, tt := range tests {
		got := mssqlStripParens(tt.in)
		if got != tt.want {
			t.Errorf("mssqlStripParens(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMSSQLTransformValue(t *testing.T) {
	tm := defaultTypeMappingConfig()

	// nil → nil
	got, err := mssqlTransformValue(nil, Column{DataType: "int"}, tm)
	if err != nil || got != nil {
		t.Errorf("TransformValue(nil) = %v, want nil", got)
	}

	// uniqueidentifier from raw bytes (mixed-endian)
	// UUID: 01020304-0506-0708-090a-0b0c0d0e0f10
	// SQL Server LE storage: [04 03 02 01] [06 05] [08 07] [09 0a] [0b 0c 0d 0e 0f 10]
	uuidBytes := []byte{0x04, 0x03, 0x02, 0x01, 0x06, 0x05, 0x08, 0x07, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	got, err = mssqlTransformValue(uuidBytes, Column{DataType: "uniqueidentifier"}, tm)
	if err != nil {
		t.Fatalf("uuid transform error: %v", err)
	}
	wantUUID := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != wantUUID {
		t.Errorf("uuid transform = %q, want %q", got, wantUUID)
	}

	// uniqueidentifier from string passthrough
	got, err = mssqlTransformValue("ABCDEF01-2345-6789-ABCD-EF0123456789", Column{DataType: "uniqueidentifier"}, tm)
	if err != nil {
		t.Fatalf("uuid string transform error: %v", err)
	}
	if got != "abcdef01-2345-6789-abcd-ef0123456789" {
		t.Errorf("uuid string transform = %q", got)
	}

	// money string passthrough
	got, err = mssqlTransformValue("19.9900", Column{DataType: "money"}, tm)
	if err != nil {
		t.Fatalf("money transform error: %v", err)
	}
	if got != "19.9900" {
		t.Errorf("money transform = %q, want %q", got, "19.9900")
	}

	// money float64
	got, err = mssqlTransformValue(float64(19.99), Column{DataType: "money"}, tm)
	if err != nil {
		t.Fatalf("money float64 transform error: %v", err)
	}
	if got != "19.9900" {
		t.Errorf("money float64 transform = %q, want %q", got, "19.9900")
	}

	// bit passthrough
	got, err = mssqlTransformValue(true, Column{DataType: "bit"}, tm)
	if err != nil || got != true {
		t.Errorf("bit transform = %v, want true", got)
	}

	// text null byte stripping
	got, err = mssqlTransformValue("hello\x00world", Column{DataType: "nvarchar"}, tm)
	if err != nil {
		t.Fatalf("nvarchar transform error: %v", err)
	}
	if got != "helloworld" {
		t.Errorf("nvarchar transform = %q, want %q", got, "helloworld")
	}

	// int64 passthrough
	got, err = mssqlTransformValue(int64(42), Column{DataType: "int"}, tm)
	if err != nil || got != int64(42) {
		t.Errorf("int transform = %v, want 42", got)
	}
}

func TestMSSQLQuoteIdentifier(t *testing.T) {
	src := &mssqlSourceDB{}

	tests := []struct {
		in, want string
	}{
		{"users", "[users]"},
		{"my]table", "[my]]table]"},
		{"simple", "[simple]"},
		{"My Table", "[My Table]"},
	}
	for _, tt := range tests {
		got := src.QuoteIdentifier(tt.in)
		if got != tt.want {
			t.Errorf("QuoteIdentifier(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestMSSQLSourceTableRef(t *testing.T) {
	src := &mssqlSourceDB{sourceSchema: "sales"}
	table := Table{SourceName: "orders"}

	got := src.SourceTableRef(table)
	want := "[sales].[orders]"
	if got != want {
		t.Fatalf("SourceTableRef() = %q, want %q", got, want)
	}
}

func TestMSSQLExtractDBName(t *testing.T) {
	src := &mssqlSourceDB{}

	tests := []struct {
		dsn, want string
		err       bool
	}{
		// URL format
		{"sqlserver://sa:pass@localhost:1433?database=mydb", "mydb", false},
		{"sqlserver://sa:pass@localhost/instance?database=mydb", "mydb", false},

		// ADO format
		{"server=localhost;user id=sa;password=pass;database=mydb", "mydb", false},
		{"Server=localhost;Database=MyDB;User Id=sa;Password=pass", "MyDB", false},

		// Missing database
		{"sqlserver://sa:pass@localhost:1433", "", true},
		{"server=localhost;user id=sa", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.dsn, func(t *testing.T) {
			got, err := src.ExtractDBName(tt.dsn)
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
				t.Errorf("ExtractDBName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMSSQLValidateTypeMapping(t *testing.T) {
	src := &mssqlSourceDB{}

	// Default config should be valid
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

	tm = defaultTypeMappingConfig()
	tm.VarcharAsText = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for varchar_as_text")
	}

	tm = defaultTypeMappingConfig()
	tm.EnumMode = "check"
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for enum_mode=check")
	}

	tm = defaultTypeMappingConfig()
	tm.SetMode = "text_array"
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for set_mode=text_array")
	}

	// MSSQL-acceptable options should pass
	tm = defaultTypeMappingConfig()
	tm.DatetimeAsTimestamptz = true
	if err := src.ValidateTypeMapping(tm); err != nil {
		t.Fatalf("datetime_as_timestamptz should be valid for MSSQL: %v", err)
	}

	tm = defaultTypeMappingConfig()
	tm.JSONAsJSONB = true
	if err := src.ValidateTypeMapping(tm); err != nil {
		t.Fatalf("json_as_jsonb should be valid for MSSQL: %v", err)
	}

	tm = defaultTypeMappingConfig()
	tm.SpatialMode = "wkt_text"
	if err := src.ValidateTypeMapping(tm); err != nil {
		t.Fatalf("spatial_mode=wkt_text should be valid for MSSQL: %v", err)
	}

	tm = defaultTypeMappingConfig()
	tm.NvarcharAsText = true
	if err := src.ValidateTypeMapping(tm); err != nil {
		t.Fatalf("nvarchar_as_text should be valid for MSSQL: %v", err)
	}

	tm = defaultTypeMappingConfig()
	tm.MoneyAsNumeric = false
	if err := src.ValidateTypeMapping(tm); err != nil {
		t.Fatalf("money_as_numeric=false should be valid for MSSQL: %v", err)
	}

	tm = defaultTypeMappingConfig()
	tm.XmlAsText = true
	if err := src.ValidateTypeMapping(tm); err != nil {
		t.Fatalf("xml_as_text should be valid for MSSQL: %v", err)
	}
}

func TestMSSQLSupportsSnapshotMode(t *testing.T) {
	src := &mssqlSourceDB{}
	if !src.SupportsSnapshotMode() {
		t.Error("MSSQL should support snapshot mode")
	}
}

func TestMSSQLMaxWorkers(t *testing.T) {
	src := &mssqlSourceDB{}
	if src.MaxWorkers() != 0 {
		t.Errorf("MaxWorkers() = %d, want 0 (no limit)", src.MaxWorkers())
	}
}

func TestMSSQLName(t *testing.T) {
	src := &mssqlSourceDB{}
	if src.Name() != "MSSQL" {
		t.Errorf("Name() = %q, want MSSQL", src.Name())
	}
}

func TestMSSQLIdentName(t *testing.T) {
	src := &mssqlSourceDB{snakeCaseIDs: true}
	if got := src.identName("MyColumn"); got != "my_column" {
		t.Errorf("identName(MyColumn) = %q, want my_column", got)
	}

	src.snakeCaseIDs = false
	if got := src.identName("MyColumn"); got != "mycolumn" {
		t.Errorf("identName(MyColumn) = %q, want mycolumn", got)
	}
}

func TestMSSQLIsSpatialType(t *testing.T) {
	if !isMSSQLSpatialType("geography") {
		t.Error("geography should be spatial")
	}
	if !isMSSQLSpatialType("geometry") {
		t.Error("geometry should be spatial")
	}
	if isMSSQLSpatialType("int") {
		t.Error("int should not be spatial")
	}
}

// --- Config tests for MSSQL ---

func TestLoadConfig_MSSQLBasic(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql.toml")

	content := `
schema = "target"

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Source.Type != "mssql" {
		t.Errorf("Source.Type = %q, want mssql", cfg.Source.Type)
	}
	if cfg.Source.SourceSchema != "dbo" {
		t.Errorf("Source.SourceSchema = %q, want dbo (default)", cfg.Source.SourceSchema)
	}
	if !cfg.TypeMapping.MoneyAsNumeric {
		t.Errorf("MoneyAsNumeric = %t, want true (default)", cfg.TypeMapping.MoneyAsNumeric)
	}
}

func TestLoadConfig_MSSQLSourceSchema(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql_schema.toml")

	content := `
schema = "target"

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"
source_schema = "sales"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Source.SourceSchema != "sales" {
		t.Errorf("Source.SourceSchema = %q, want sales", cfg.Source.SourceSchema)
	}
}

func TestLoadConfig_MSSQLTypeMappingOverrides(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql_type_mapping.toml")

	content := `
schema = "target"

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
nvarchar_as_text = true
money_as_numeric = false
xml_as_text = true
datetime_as_timestamptz = true
json_as_jsonb = true
spatial_mode = "wkt_text"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if !cfg.TypeMapping.NvarcharAsText {
		t.Errorf("NvarcharAsText = %t, want true", cfg.TypeMapping.NvarcharAsText)
	}
	if cfg.TypeMapping.MoneyAsNumeric {
		t.Errorf("MoneyAsNumeric = %t, want false", cfg.TypeMapping.MoneyAsNumeric)
	}
	if !cfg.TypeMapping.XmlAsText {
		t.Errorf("XmlAsText = %t, want true", cfg.TypeMapping.XmlAsText)
	}
	if !cfg.TypeMapping.DatetimeAsTimestamptz {
		t.Errorf("DatetimeAsTimestamptz = %t, want true", cfg.TypeMapping.DatetimeAsTimestamptz)
	}
	if !cfg.TypeMapping.JSONAsJSONB {
		t.Errorf("JSONAsJSONB = %t, want true", cfg.TypeMapping.JSONAsJSONB)
	}
	if cfg.TypeMapping.SpatialMode != "wkt_text" {
		t.Errorf("SpatialMode = %q, want wkt_text", cfg.TypeMapping.SpatialMode)
	}
}

func TestLoadConfig_MSSQLWithMySQLOnlyOptionRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql_bad.toml")

	content := `
schema = "target"

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"

[target]
dsn = "postgres://u:p@h:5432/db"

[type_mapping]
tinyint1_as_boolean = true
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for MySQL-only option with MSSQL source")
	}
	if !strings.Contains(err.Error(), "MySQL-only") {
		t.Errorf("error should mention MySQL-only, got: %v", err)
	}
}

func TestLoadConfig_MSSQLCharsetRejected(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql_charset.toml")

	content := `
schema = "target"

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"
charset = "latin1"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig(cfgFile)
	if err == nil {
		t.Fatal("expected error for MSSQL + charset override")
	}
	if !strings.Contains(err.Error(), "MySQL-only") {
		t.Errorf("error should mention MySQL-only, got: %v", err)
	}
}

func TestLoadConfig_MSSQLSingleTxAllowed(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql_single_tx.toml")

	content := `
schema = "target"
source_snapshot_mode = "single_tx"

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("MSSQL should support single_tx: %v", err)
	}
	if cfg.SourceSnapshotMode != "single_tx" {
		t.Errorf("SourceSnapshotMode = %q, want single_tx", cfg.SourceSnapshotMode)
	}
}

func TestLoadConfig_MSSQLWorkersNotCapped(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "mssql_workers.toml")

	content := `
schema = "target"
workers = 8

[source]
type = "mssql"
dsn = "sqlserver://sa:pass@localhost:1433?database=testdb"

[target]
dsn = "postgres://u:p@h:5432/db"
`
	if err := os.WriteFile(cfgFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(cfgFile)
	if err != nil {
		t.Fatalf("loadConfig() error: %v", err)
	}

	if cfg.Workers != 8 {
		t.Errorf("Workers = %d, want 8 (MSSQL should not cap workers)", cfg.Workers)
	}
}

// --- MySQL validator tests for MSSQL-only options ---

func TestMySQLValidateTypeMapping_MSSQLOnlyRejected(t *testing.T) {
	src := &mysqlSourceDB{}

	tm := defaultTypeMappingConfig()
	tm.NvarcharAsText = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for nvarchar_as_text with MySQL source")
	}

	tm = defaultTypeMappingConfig()
	tm.MoneyAsNumeric = false
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for money_as_numeric=false with MySQL source")
	}

	tm = defaultTypeMappingConfig()
	tm.XmlAsText = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for xml_as_text with MySQL source")
	}

	// Default config should still be valid
	if err := src.ValidateTypeMapping(defaultTypeMappingConfig()); err != nil {
		t.Fatalf("default config should be valid for MySQL: %v", err)
	}
}

// --- Chunk test for MSSQL types ---

func TestChunkKeyForTable_MSSQLInt(t *testing.T) {
	src := &mssqlSourceDB{}
	table := Table{
		SourceName: "orders",
		PGName:     "orders",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "int"},
			{SourceName: "total", PGName: "total", DataType: "money"},
		},
		PrimaryKey: &Index{
			Columns: []string{"id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key == nil {
		t.Fatal("expected non-nil ChunkKey for MSSQL int PK")
	}
	if key.SourceColumn != "id" {
		t.Errorf("key.SourceColumn = %q, want id", key.SourceColumn)
	}
}

func TestChunkKeyForTable_MSSQLBigint(t *testing.T) {
	src := &mssqlSourceDB{}
	table := Table{
		SourceName: "events",
		PGName:     "events",
		Columns: []Column{
			{SourceName: "id", PGName: "id", DataType: "bigint"},
		},
		PrimaryKey: &Index{
			Columns: []string{"id"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key == nil {
		t.Fatal("expected non-nil ChunkKey for MSSQL bigint PK")
	}
}

func TestChunkKeyForTable_MSSQLVarcharNotChunkable(t *testing.T) {
	src := &mssqlSourceDB{}
	table := Table{
		SourceName: "slugs",
		PGName:     "slugs",
		Columns: []Column{
			{SourceName: "slug", PGName: "slug", DataType: "nvarchar"},
		},
		PrimaryKey: &Index{
			Columns: []string{"slug"},
		},
	}

	key := chunkKeyForTable(table, src)
	if key != nil {
		t.Fatal("expected nil ChunkKey for MSSQL nvarchar PK")
	}
}

func TestBuildChunkedSelectQuery_MSSQL(t *testing.T) {
	src := &mssqlSourceDB{}
	table := Table{
		SourceName: "users",
		Columns: []Column{
			{SourceName: "id"},
			{SourceName: "name"},
		},
	}
	key := ChunkKey{SourceColumn: "id", PGColumn: "id"}

	chunk := Chunk{Index: 0, LowerBound: 1, UpperBound: 100, IsLast: false}
	got := buildChunkedSelectQuery(src, table, key, chunk, defaultTypeMappingConfig())
	want := "SELECT [id], [name] FROM [users] WHERE [id] >= 1 AND [id] < 100 ORDER BY [id]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	chunk = Chunk{Index: 1, LowerBound: 100, UpperBound: 150, IsLast: true}
	got = buildChunkedSelectQuery(src, table, key, chunk, defaultTypeMappingConfig())
	want = "SELECT [id], [name] FROM [users] WHERE [id] >= 100 AND [id] <= 150 ORDER BY [id]"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// --- Generated column detection ---

func TestIsGeneratedColumn_MSSQLComputed(t *testing.T) {
	col := Column{Extra: "COMPUTED"}
	if !isGeneratedColumn(col) {
		t.Fatal("MSSQL COMPUTED column should be detected as generated")
	}

	col = Column{Extra: "computed"}
	if !isGeneratedColumn(col) {
		t.Fatal("lowercase COMPUTED should also be detected")
	}
}

// --- Column select expression tests ---

func TestColumnSelectExpr_MSSQLHierarchyid(t *testing.T) {
	src := &mssqlSourceDB{}
	col := Column{SourceName: "path", DataType: "hierarchyid"}
	got := columnSelectExpr(src, col, defaultTypeMappingConfig())
	want := "[path].ToString() AS [path]"
	if got != want {
		t.Errorf("columnSelectExpr(hierarchyid) = %q, want %q", got, want)
	}
}

func TestColumnSelectExpr_MSSQLSpatialWKT(t *testing.T) {
	src := &mssqlSourceDB{}
	tm := defaultTypeMappingConfig()
	tm.SpatialMode = "wkt_text"
	col := Column{SourceName: "geom", DataType: "geography"}
	got := columnSelectExpr(src, col, tm)
	want := "[geom].STAsText() AS [geom]"
	if got != want {
		t.Errorf("columnSelectExpr(geography wkt) = %q, want %q", got, want)
	}
}

func TestColumnSelectExpr_MSSQLSpatialWKB(t *testing.T) {
	src := &mssqlSourceDB{}
	tm := defaultTypeMappingConfig()
	tm.SpatialMode = "wkb_bytea"
	col := Column{SourceName: "geom", DataType: "geometry"}
	got := columnSelectExpr(src, col, tm)
	want := "[geom].STAsBinary() AS [geom]"
	if got != want {
		t.Errorf("columnSelectExpr(geometry wkb) = %q, want %q", got, want)
	}
}

func TestColumnSelectExpr_MSSQLSqlVariant(t *testing.T) {
	src := &mssqlSourceDB{}
	col := Column{SourceName: "val", DataType: "sql_variant"}
	got := columnSelectExpr(src, col, defaultTypeMappingConfig())
	want := "CAST([val] AS nvarchar(max)) AS [val]"
	if got != want {
		t.Errorf("columnSelectExpr(sql_variant) = %q, want %q", got, want)
	}
}

func TestColumnSelectExpr_MSSQLRegularColumn(t *testing.T) {
	src := &mssqlSourceDB{}
	col := Column{SourceName: "name", DataType: "nvarchar"}
	got := columnSelectExpr(src, col, defaultTypeMappingConfig())
	want := "[name]"
	if got != want {
		t.Errorf("columnSelectExpr(regular) = %q, want %q", got, want)
	}
}

// --- SQLite validator: MSSQL-only options ---

func TestSQLiteValidateTypeMapping_MSSQLOnlyRejected(t *testing.T) {
	src := &sqliteSourceDB{}

	tm := defaultTypeMappingConfig()
	tm.NvarcharAsText = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for nvarchar_as_text with SQLite source")
	}

	tm = defaultTypeMappingConfig()
	tm.MoneyAsNumeric = false
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for money_as_numeric=false with SQLite source")
	}

	tm = defaultTypeMappingConfig()
	tm.XmlAsText = true
	if err := src.ValidateTypeMapping(tm); err == nil {
		t.Fatal("expected error for xml_as_text with SQLite source")
	}
}
