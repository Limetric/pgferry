package main

import (
	"testing"
	"time"
)

func noWidenTypeMappingConfig() TypeMappingConfig {
	cfg := defaultTypeMappingConfig()
	cfg.WidenUnsignedIntegers = false
	return cfg
}

func TestMapType(t *testing.T) {
	tests := []struct {
		name string
		col  Column
		tm   TypeMappingConfig
		want string
		err  bool
	}{
		{"binary16→bytea default", Column{DataType: "binary", Precision: 16, ColumnType: "binary(16)"}, defaultTypeMappingConfig(), "bytea", false},
		{"binary16→uuid opt-in", Column{DataType: "binary", Precision: 16, ColumnType: "binary(16)"}, TypeMappingConfig{Binary16AsUUID: true, SanitizeJSONNullBytes: true}, "uuid", false},
		{"binary16→uuid opt-in when precision missing", Column{DataType: "binary", Precision: 0, ColumnType: "binary(16)"}, TypeMappingConfig{Binary16AsUUID: true, SanitizeJSONNullBytes: true}, "uuid", false},
		{"binary shape uses column_type over precision", Column{DataType: "binary", Precision: 16, ColumnType: "binary(8)"}, TypeMappingConfig{Binary16AsUUID: true, SanitizeJSONNullBytes: true}, "bytea", false},
		{"tinyint1→smallint default", Column{DataType: "tinyint", Precision: 1, ColumnType: "tinyint(1)"}, defaultTypeMappingConfig(), "smallint", false},
		{"tinyint1→bool opt-in", Column{DataType: "tinyint", Precision: 1, ColumnType: "tinyint(1)"}, TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}, "boolean", false},
		{"tinyint1→bool opt-in when precision misleading", Column{DataType: "tinyint", Precision: 3, ColumnType: "tinyint(1)"}, TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}, "boolean", false},
		{"tinyint shape uses column_type over precision", Column{DataType: "tinyint", Precision: 1, ColumnType: "tinyint(2)"}, TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}, "smallint", false},
		{"tinyint→smallint", Column{DataType: "tinyint", Precision: 3, ColumnType: "tinyint(3)"}, defaultTypeMappingConfig(), "smallint", false},
		{"smallint unsigned→integer", Column{DataType: "smallint", ColumnType: "smallint unsigned"}, defaultTypeMappingConfig(), "integer", false},
		{"smallint unsigned→smallint no-widen", Column{DataType: "smallint", ColumnType: "smallint unsigned"}, noWidenTypeMappingConfig(), "smallint", false},
		{"int unsigned→bigint", Column{DataType: "int", ColumnType: "int unsigned"}, defaultTypeMappingConfig(), "bigint", false},
		{"int unsigned→integer no-widen", Column{DataType: "int", ColumnType: "int unsigned"}, noWidenTypeMappingConfig(), "integer", false},
		{"bigint unsigned→numeric20", Column{DataType: "bigint", ColumnType: "bigint unsigned"}, defaultTypeMappingConfig(), "numeric(20)", false},
		{"bigint unsigned→bigint no-widen", Column{DataType: "bigint", ColumnType: "bigint unsigned"}, noWidenTypeMappingConfig(), "bigint", false},
		{"mediumint→integer", Column{DataType: "mediumint", ColumnType: "mediumint"}, defaultTypeMappingConfig(), "integer", false},
		{"bigint", Column{DataType: "bigint", ColumnType: "bigint"}, defaultTypeMappingConfig(), "bigint", false},
		{"float→real", Column{DataType: "float", ColumnType: "float"}, defaultTypeMappingConfig(), "real", false},
		{"double", Column{DataType: "double", ColumnType: "double"}, defaultTypeMappingConfig(), "double precision", false},
		{"decimal", Column{DataType: "decimal", ColumnType: "decimal(10,7)", Precision: 10, Scale: 7}, defaultTypeMappingConfig(), "numeric(10,7)", false},
		{"char36→uuid opt-in", Column{DataType: "char", ColumnType: "char(36)", CharMaxLen: 36}, TypeMappingConfig{StringUUIDAaUUID: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea"}, "uuid", false},
		{"varchar36→uuid opt-in", Column{DataType: "varchar", ColumnType: "varchar(36)", CharMaxLen: 36}, TypeMappingConfig{StringUUIDAaUUID: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea"}, "uuid", false},
		{"varchar35→varchar no match", Column{DataType: "varchar", ColumnType: "varchar(35)", CharMaxLen: 35}, TypeMappingConfig{StringUUIDAaUUID: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea"}, "varchar(35)", false},
		{"varchar36→varchar when disabled", Column{DataType: "varchar", ColumnType: "varchar(36)", CharMaxLen: 36}, defaultTypeMappingConfig(), "varchar(36)", false},
		{"varchar", Column{DataType: "varchar", ColumnType: "varchar(200)", CharMaxLen: 200}, defaultTypeMappingConfig(), "varchar(200)", false},
		{"varchar→text opt-in", Column{DataType: "varchar", ColumnType: "varchar(200)", CharMaxLen: 200}, TypeMappingConfig{VarcharAsText: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "text", false},
		{"char→varchar", Column{DataType: "char", ColumnType: "char(64)", CharMaxLen: 64}, defaultTypeMappingConfig(), "varchar(64)", false},
		{"char→text opt-in", Column{DataType: "char", ColumnType: "char(64)", CharMaxLen: 64}, TypeMappingConfig{VarcharAsText: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "text", false},
		{"text", Column{DataType: "text", ColumnType: "text"}, defaultTypeMappingConfig(), "text", false},
		{"mediumtext", Column{DataType: "mediumtext", ColumnType: "mediumtext"}, defaultTypeMappingConfig(), "text", false},
		{"json→json default", Column{DataType: "json", ColumnType: "json"}, defaultTypeMappingConfig(), "json", false},
		{"json→jsonb opt-in", Column{DataType: "json", ColumnType: "json"}, TypeMappingConfig{JSONAsJSONB: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "jsonb", false},
		{"enum→text", Column{DataType: "enum", ColumnType: "enum('a','b')"}, defaultTypeMappingConfig(), "text", false},
		{"enum→text check mode", Column{DataType: "enum", ColumnType: "enum('a','b')"}, TypeMappingConfig{EnumMode: "check", SetMode: "text", SanitizeJSONNullBytes: true}, "text", false},
		{"enum→native", Column{DataType: "enum", ColumnType: "enum('new','used')"}, TypeMappingConfig{EnumMode: "native", SetMode: "text", SanitizeJSONNullBytes: true}, pgEnumTypeName([]string{"new", "used"}), false},
		{"set→text default", Column{DataType: "set", ColumnType: "set('a','b')"}, defaultTypeMappingConfig(), "text", false},
		{"set→text[] opt-in", Column{DataType: "set", ColumnType: "set('a','b')"}, TypeMappingConfig{EnumMode: "text", SetMode: "text_array", SanitizeJSONNullBytes: true}, "text[]", false},
		{"set→text[] check", Column{DataType: "set", ColumnType: "set('a','b')"}, TypeMappingConfig{EnumMode: "text", SetMode: "text_array_check", SanitizeJSONNullBytes: true}, "text[]", false},
		{"timestamp→timestamptz", Column{DataType: "timestamp", ColumnType: "timestamp"}, defaultTypeMappingConfig(), "timestamptz", false},
		{"datetime→timestamp default", Column{DataType: "datetime", ColumnType: "datetime"}, defaultTypeMappingConfig(), "timestamp", false},
		{"datetime→timestamptz opt-in", Column{DataType: "datetime", ColumnType: "datetime"}, TypeMappingConfig{DatetimeAsTimestamptz: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "timestamptz", false},
		{"year→integer", Column{DataType: "year", ColumnType: "year"}, defaultTypeMappingConfig(), "integer", false},
		{"time→time default", Column{DataType: "time", ColumnType: "time"}, defaultTypeMappingConfig(), "time", false},
		{"time→text", Column{DataType: "time", ColumnType: "time"}, TypeMappingConfig{TimeMode: "text", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122"}, "text", false},
		{"time→interval", Column{DataType: "time", ColumnType: "time"}, TypeMappingConfig{TimeMode: "interval", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122"}, "interval", false},
		{"date", Column{DataType: "date", ColumnType: "date"}, defaultTypeMappingConfig(), "date", false},
		{"bit→bytea default", Column{DataType: "bit", ColumnType: "bit(8)", Precision: 8}, defaultTypeMappingConfig(), "bytea", false},
		{"bit→bit(8)", Column{DataType: "bit", ColumnType: "bit(8)", Precision: 8}, TypeMappingConfig{BitMode: "bit", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "bit(8)", false},
		{"bit→bit(1)", Column{DataType: "bit", ColumnType: "bit(1)", Precision: 1}, TypeMappingConfig{BitMode: "bit", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "bit(1)", false},
		{"bit→varbit", Column{DataType: "bit", ColumnType: "bit(16)", Precision: 16}, TypeMappingConfig{BitMode: "varbit", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "varbit", false},
		{"binary→bytea", Column{DataType: "binary", ColumnType: "binary(32)", Precision: 32}, defaultTypeMappingConfig(), "bytea", false},
		{"varbinary→bytea", Column{DataType: "varbinary", ColumnType: "varbinary(32)"}, defaultTypeMappingConfig(), "bytea", false},
		{"geometry→error when off", Column{DataType: "geometry", ColumnType: "geometry"}, defaultTypeMappingConfig(), "", true},
		{"geometry→bytea wkb", Column{DataType: "geometry", ColumnType: "geometry"}, TypeMappingConfig{SpatialMode: "wkb_bytea", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}, "bytea", false},
		{"point→bytea wkb", Column{DataType: "point", ColumnType: "point"}, TypeMappingConfig{SpatialMode: "wkb_bytea", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}, "bytea", false},
		{"geometry→text wkt", Column{DataType: "geometry", ColumnType: "geometry"}, TypeMappingConfig{SpatialMode: "wkt_text", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}, "text", false},
		{"polygon→text wkt", Column{DataType: "polygon", ColumnType: "polygon"}, TypeMappingConfig{SpatialMode: "wkt_text", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}, "text", false},
		{"multipoint→bytea wkb", Column{DataType: "multipoint", ColumnType: "multipoint"}, TypeMappingConfig{SpatialMode: "wkb_bytea", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}, "bytea", false},
		{"unknown→text opt-in", Column{DataType: "sometype", ColumnType: "sometype"}, TypeMappingConfig{UnknownAsText: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}, "text", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysqlMapType(tt.col, tt.tm)
			if tt.err {
				if err == nil {
					t.Fatalf("mysqlMapType(%+v) expected error", tt.col)
				}
				return
			}
			if err != nil {
				t.Fatalf("mysqlMapType(%+v) unexpected error: %v", tt.col, err)
			}
			if got != tt.want {
				t.Errorf("mysqlMapType(%+v) = %q, want %q", tt.col, got, tt.want)
			}
		})
	}
}

func TestTransformValue_UUID(t *testing.T) {
	col := Column{DataType: "binary", Precision: 0, ColumnType: "binary(16)"}
	optIn := TypeMappingConfig{Binary16AsUUID: true, SanitizeJSONNullBytes: true}

	// Valid 16-byte UUID
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	got, err := mysqlTransformValue(input, col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(uuid) error: %v", err)
	}
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != want {
		t.Errorf("mysqlTransformValue(uuid) = %q, want %q", got, want)
	}

	// Nil input
	if got, err := mysqlTransformValue(nil, col, optIn); err != nil || got != nil {
		t.Errorf("mysqlTransformValue(nil, uuid) = %v, want nil", got)
	}

	// Wrong length
	if _, err := mysqlTransformValue([]byte{0x01, 0x02}, col, optIn); err == nil {
		t.Fatal("mysqlTransformValue(short bytes, uuid) expected error")
	}
}

func TestTransformValue_UUIDSwapMode(t *testing.T) {
	col := Column{DataType: "binary", Precision: 0, ColumnType: "binary(16)"}
	swapTm := TypeMappingConfig{Binary16AsUUID: true, Binary16UUIDMode: "mysql_uuid_to_bin_swap", SanitizeJSONNullBytes: true, EnumMode: "text", SetMode: "text", BitMode: "bytea"}

	// UUID_TO_BIN('11223344-5566-7788-99aa-bbccddeeff00', 1) produces:
	// [77 88] [55 66] [11 22 33 44] [99 aa] [bb cc dd ee ff 00]
	// which should decode to: 11223344-5566-7788-99aa-bbccddeeff00
	input := []byte{0x77, 0x88, 0x55, 0x66, 0x11, 0x22, 0x33, 0x44, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}
	got, err := mysqlTransformValue(input, col, swapTm)
	if err != nil {
		t.Fatalf("mysqlTransformValue(uuid swap) error: %v", err)
	}
	want := "11223344-5566-7788-99aa-bbccddeeff00"
	if got != want {
		t.Errorf("mysqlTransformValue(uuid swap) = %q, want %q", got, want)
	}
}

func TestTransformValue_UUIDRfc4122Default(t *testing.T) {
	col := Column{DataType: "binary", Precision: 0, ColumnType: "binary(16)"}
	rfc := TypeMappingConfig{Binary16AsUUID: true, Binary16UUIDMode: "rfc4122", SanitizeJSONNullBytes: true, EnumMode: "text", SetMode: "text", BitMode: "bytea"}

	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	got, err := mysqlTransformValue(input, col, rfc)
	if err != nil {
		t.Fatalf("mysqlTransformValue(uuid rfc4122) error: %v", err)
	}
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != want {
		t.Errorf("mysqlTransformValue(uuid rfc4122) = %q, want %q", got, want)
	}
}

func TestTransformValue_UUIDOptInNonBinary16Passthrough(t *testing.T) {
	col := Column{DataType: "binary", Precision: 16, ColumnType: "binary(8)"}
	optIn := TypeMappingConfig{Binary16AsUUID: true, SanitizeJSONNullBytes: true}

	in := []byte{0x01, 0x02, 0x03}
	got, err := mysqlTransformValue(in, col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(non-binary16) unexpected error: %v", err)
	}
	out, ok := got.([]byte)
	if !ok {
		t.Fatalf("mysqlTransformValue(non-binary16) type = %T, want []byte", got)
	}
	if len(out) != len(in) || out[0] != in[0] || out[1] != in[1] || out[2] != in[2] {
		t.Fatalf("mysqlTransformValue(non-binary16) = %#v, want %#v", out, in)
	}
}

func TestTransformValue_Bool(t *testing.T) {
	col := Column{DataType: "tinyint", Precision: 3, ColumnType: "tinyint(1)"}
	optIn := TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}

	if got, err := mysqlTransformValue(int64(1), col, optIn); err != nil || got != true {
		t.Errorf("mysqlTransformValue(1, bool) = %v, want true", got)
	}
	if got, err := mysqlTransformValue(int64(0), col, optIn); err != nil || got != false {
		t.Errorf("mysqlTransformValue(0, bool) = %v, want false", got)
	}
	if got, err := mysqlTransformValue(nil, col, optIn); err != nil || got != nil {
		t.Errorf("mysqlTransformValue(nil, bool) = %v, want nil", got)
	}
	if _, err := mysqlTransformValue(int64(2), col, optIn); err == nil {
		t.Fatal("mysqlTransformValue(2, bool) expected error")
	}
}

func TestTransformValue_BoolNoOptInPassthrough(t *testing.T) {
	col := Column{DataType: "tinyint", Precision: 3, ColumnType: "tinyint(1)"}
	got, err := mysqlTransformValue(int64(2), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("mysqlTransformValue default tinyint(1) unexpected error: %v", err)
	}
	if got != int64(2) {
		t.Fatalf("mysqlTransformValue default tinyint(1) = %v, want %v", got, int64(2))
	}
}

func TestTransformValue_BoolOptInNonTinyInt1Passthrough(t *testing.T) {
	col := Column{DataType: "tinyint", Precision: 1, ColumnType: "tinyint(2)"}
	optIn := TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}
	got, err := mysqlTransformValue(int64(2), col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(non-tinyint1) unexpected error: %v", err)
	}
	if got != int64(2) {
		t.Fatalf("mysqlTransformValue(non-tinyint1) = %v, want %v", got, int64(2))
	}
}

func TestTransformValue_SetTextArray(t *testing.T) {
	col := Column{DataType: "set"}
	optIn := TypeMappingConfig{EnumMode: "text", SetMode: "text_array", SanitizeJSONNullBytes: true}

	got, err := mysqlTransformValue([]byte("a,b"), col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(set) error: %v", err)
	}
	vals, ok := got.([]string)
	if !ok {
		t.Fatalf("mysqlTransformValue(set) type = %T, want []string", got)
	}
	if len(vals) != 2 || vals[0] != "a" || vals[1] != "b" {
		t.Fatalf("mysqlTransformValue(set) = %#v, want [a b]", vals)
	}

	got, err = mysqlTransformValue([]byte(""), col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(empty set) error: %v", err)
	}
	vals, ok = got.([]string)
	if !ok {
		t.Fatalf("mysqlTransformValue(empty set) type = %T, want []string", got)
	}
	if len(vals) != 0 {
		t.Fatalf("mysqlTransformValue(empty set) = %#v, want empty slice", vals)
	}
}

func TestTransformValue_JSON(t *testing.T) {
	col := Column{DataType: "json"}

	input := []byte("hello\x00world")
	got, err := mysqlTransformValue(input, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("mysqlTransformValue(json) error: %v", err)
	}
	if got != "helloworld" {
		t.Errorf("mysqlTransformValue(json with null byte) = %q, want %q", got, "helloworld")
	}

	if got, err := mysqlTransformValue(nil, col, defaultTypeMappingConfig()); err != nil || got != nil {
		t.Errorf("mysqlTransformValue(nil, json) = %v, want nil", got)
	}
}

func TestTransformValue_ZeroDates(t *testing.T) {
	for _, dt := range []string{"date", "timestamp", "datetime"} {
		col := Column{DataType: dt}

		// Zero time → nil
		if got, err := mysqlTransformValue(time.Time{}, col, defaultTypeMappingConfig()); err != nil || got != nil {
			t.Errorf("mysqlTransformValue(zero %s) = %v, want nil", dt, got)
		}

		// Valid time → pass through
		valid := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		if got, err := mysqlTransformValue(valid, col, defaultTypeMappingConfig()); err != nil || got != valid {
			t.Errorf("mysqlTransformValue(valid %s) = %v, want %v", dt, got, valid)
		}

		// Nil → nil
		if got, err := mysqlTransformValue(nil, col, defaultTypeMappingConfig()); err != nil || got != nil {
			t.Errorf("mysqlTransformValue(nil, %s) = %v, want nil", dt, got)
		}
	}
}

func TestTransformValue_ZeroDatesErrorMode(t *testing.T) {
	errTm := defaultTypeMappingConfig()
	errTm.ZeroDateMode = "error"

	for _, dt := range []string{"date", "timestamp", "datetime"} {
		col := Column{DataType: dt, SourceName: "test_col"}

		// Zero time → error
		_, err := mysqlTransformValue(time.Time{}, col, errTm)
		if err == nil {
			t.Errorf("zero %s with error mode: expected error", dt)
		}

		// Valid time → pass through
		valid := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		got, err := mysqlTransformValue(valid, col, errTm)
		if err != nil || got != valid {
			t.Errorf("valid %s with error mode: got %v, want %v", dt, got, valid)
		}

		// Nil → nil (even in error mode)
		got, err = mysqlTransformValue(nil, col, errTm)
		if err != nil || got != nil {
			t.Errorf("nil %s with error mode: got %v, want nil", dt, got)
		}
	}
}

func TestTransformValue_ZeroDatesNullMode(t *testing.T) {
	// Default behavior (null mode) should still work
	for _, dt := range []string{"date", "timestamp", "datetime"} {
		col := Column{DataType: dt}
		got, err := mysqlTransformValue(time.Time{}, col, defaultTypeMappingConfig())
		if err != nil || got != nil {
			t.Errorf("zero %s with null mode: got %v, want nil", dt, got)
		}
	}
}

func TestTransformValue_Year(t *testing.T) {
	col := Column{DataType: "year"}

	if got, err := mysqlTransformValue([]byte("2024"), col, defaultTypeMappingConfig()); err != nil || got != int64(2024) {
		t.Fatalf("mysqlTransformValue([]byte(\"2024\"), year) = %v, err=%v; want 2024", got, err)
	}

	if got, err := mysqlTransformValue("1999", col, defaultTypeMappingConfig()); err != nil || got != int64(1999) {
		t.Fatalf("mysqlTransformValue(\"1999\", year) = %v, err=%v; want 1999", got, err)
	}

	if got, err := mysqlTransformValue(int64(2001), col, defaultTypeMappingConfig()); err != nil || got != int64(2001) {
		t.Fatalf("mysqlTransformValue(int64(2001), year) = %v, err=%v; want 2001", got, err)
	}

	if _, err := mysqlTransformValue("not-a-year", col, defaultTypeMappingConfig()); err == nil {
		t.Fatal("mysqlTransformValue(invalid year) expected error")
	}
}

func TestTransformValue_Passthrough(t *testing.T) {
	col := Column{DataType: "varchar"}
	if got, err := mysqlTransformValue("hello", col, defaultTypeMappingConfig()); err != nil || got != "hello" {
		t.Errorf("mysqlTransformValue(varchar) = %v, want %q", got, "hello")
	}
}

func TestTransformValue_TextNullByteStripping(t *testing.T) {
	for _, dt := range []string{"varchar", "char", "text", "mediumtext", "longtext", "tinytext", "enum", "set"} {
		col := Column{DataType: dt}
		// string input
		got, err := mysqlTransformValue("hello\x00world", col, defaultTypeMappingConfig())
		if err != nil {
			t.Fatalf("mysqlTransformValue(%s string) error: %v", dt, err)
		}
		if got != "helloworld" {
			t.Errorf("mysqlTransformValue(%s string with null byte) = %q, want %q", dt, got, "helloworld")
		}
		// []byte input
		got, err = mysqlTransformValue([]byte("foo\x00bar"), col, defaultTypeMappingConfig())
		if err != nil {
			t.Fatalf("mysqlTransformValue(%s []byte) error: %v", dt, err)
		}
		if got != "foobar" {
			t.Errorf("mysqlTransformValue(%s []byte with null byte) = %q, want %q", dt, got, "foobar")
		}
	}
}

func TestTransformValue_SetTextArrayNullByteStripping(t *testing.T) {
	col := Column{DataType: "set"}
	optIn := TypeMappingConfig{EnumMode: "text", SetMode: "text_array", SanitizeJSONNullBytes: true}
	got, err := mysqlTransformValue([]byte("a\x00b,c"), col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(set text_array) error: %v", err)
	}
	arr, ok := got.([]string)
	if !ok {
		t.Fatalf("mysqlTransformValue(set text_array) type = %T, want []string", got)
	}
	if len(arr) != 2 || arr[0] != "ab" || arr[1] != "c" {
		t.Errorf("mysqlTransformValue(set text_array with null byte) = %v, want [ab c]", arr)
	}
}

func TestTransformValue_SetTextArrayCheck(t *testing.T) {
	col := Column{DataType: "set"}
	optIn := TypeMappingConfig{EnumMode: "text", SetMode: "text_array_check", SanitizeJSONNullBytes: true}

	got, err := mysqlTransformValue([]byte("a,b"), col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(set text_array_check) error: %v", err)
	}
	vals, ok := got.([]string)
	if !ok {
		t.Fatalf("mysqlTransformValue(set text_array_check) type = %T, want []string", got)
	}
	if len(vals) != 2 || vals[0] != "a" || vals[1] != "b" {
		t.Fatalf("mysqlTransformValue(set text_array_check) = %#v, want [a b]", vals)
	}

	got, err = mysqlTransformValue([]byte(""), col, optIn)
	if err != nil {
		t.Fatalf("mysqlTransformValue(empty set text_array_check) error: %v", err)
	}
	vals, ok = got.([]string)
	if !ok {
		t.Fatalf("mysqlTransformValue(empty set text_array_check) type = %T, want []string", got)
	}
	if len(vals) != 0 {
		t.Fatalf("mysqlTransformValue(empty set text_array_check) = %#v, want empty slice", vals)
	}
}

func TestTransformValue_BitPassthrough(t *testing.T) {
	col := Column{DataType: "bit", Precision: 8, ColumnType: "bit(8)"}
	in := []byte{0x01}
	got, err := mysqlTransformValue(in, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("mysqlTransformValue(bit) unexpected error: %v", err)
	}
	out, ok := got.([]byte)
	if !ok {
		t.Fatalf("mysqlTransformValue(bit) type = %T, want []byte", got)
	}
	if len(out) != 1 || out[0] != 0x01 {
		t.Fatalf("mysqlTransformValue(bit) = %#v, want %#v", out, in)
	}
}

func TestTransformValue_BitToBitString(t *testing.T) {
	tm := TypeMappingConfig{BitMode: "bit", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}

	tests := []struct {
		name     string
		col      Column
		input    []byte
		wantBits string
	}{
		{"bit(8) 0xFF", Column{DataType: "bit", ColumnType: "bit(8)", Precision: 8}, []byte{0xFF}, "11111111"},
		{"bit(8) 0x01", Column{DataType: "bit", ColumnType: "bit(8)", Precision: 8}, []byte{0x01}, "00000001"},
		{"bit(1) true", Column{DataType: "bit", ColumnType: "bit(1)", Precision: 1}, []byte{0x01}, "1"},
		{"bit(1) false", Column{DataType: "bit", ColumnType: "bit(1)", Precision: 1}, []byte{0x00}, "0"},
		{"bit(16)", Column{DataType: "bit", ColumnType: "bit(16)", Precision: 16}, []byte{0xAB, 0xCD}, "1010101111001101"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysqlTransformValue(tt.input, tt.col, tm)
			if err != nil {
				t.Fatalf("mysqlTransformValue error: %v", err)
			}
			s, ok := got.(string)
			if !ok {
				t.Fatalf("mysqlTransformValue type = %T, want string", got)
			}
			if s != tt.wantBits {
				t.Errorf("mysqlTransformValue = %q, want %q", s, tt.wantBits)
			}
		})
	}
}

func TestTransformValue_BitToVarbit(t *testing.T) {
	col := Column{DataType: "bit", ColumnType: "bit(8)", Precision: 8}
	tm := TypeMappingConfig{BitMode: "varbit", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}

	got, err := mysqlTransformValue([]byte{0xAB}, col, tm)
	if err != nil {
		t.Fatalf("mysqlTransformValue(varbit) error: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("mysqlTransformValue(varbit) type = %T, want string", got)
	}
	if s != "10101011" {
		t.Errorf("mysqlTransformValue(varbit) = %q, want %q", s, "10101011")
	}
}

func TestTransformValue_StringUUID(t *testing.T) {
	col := Column{DataType: "varchar", ColumnType: "varchar(36)", CharMaxLen: 36}
	tm := TypeMappingConfig{StringUUIDAaUUID: true, EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea"}

	// Valid UUID string
	got, err := mysqlTransformValue("550e8400-e29b-41d4-a716-446655440000", col, tm)
	if err != nil {
		t.Fatalf("mysqlTransformValue(string uuid) error: %v", err)
	}
	if got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("mysqlTransformValue(string uuid) = %q, want canonical UUID", got)
	}

	// Upper case → lowered
	got, err = mysqlTransformValue("550E8400-E29B-41D4-A716-446655440000", col, tm)
	if err != nil {
		t.Fatalf("mysqlTransformValue(upper uuid) error: %v", err)
	}
	if got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("mysqlTransformValue(upper uuid) = %q, want lowered", got)
	}

	// []byte input
	got, err = mysqlTransformValue([]byte("550e8400-e29b-41d4-a716-446655440000"), col, tm)
	if err != nil {
		t.Fatalf("mysqlTransformValue([]byte uuid) error: %v", err)
	}
	if got != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("mysqlTransformValue([]byte uuid) = %q", got)
	}

	// Invalid UUID → error
	if _, err := mysqlTransformValue("not-a-uuid", col, tm); err == nil {
		t.Fatal("expected error for invalid UUID string")
	}

	// Nil → nil
	got, err = mysqlTransformValue(nil, col, tm)
	if err != nil || got != nil {
		t.Errorf("mysqlTransformValue(nil uuid) = %v, want nil", got)
	}
}

func TestTransformValue_TimeAsInterval(t *testing.T) {
	col := Column{DataType: "time", ColumnType: "time"}
	tm := TypeMappingConfig{TimeMode: "interval", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122"}

	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"normal", "12:30:45", "12 hours 30 mins 45 secs"},
		{"negative", "-05:15:00", "-05 hours 15 mins 00 secs"},
		{"long duration", "838:59:59", "838 hours 59 mins 59 secs"},
		{"zero", "00:00:00", "00 hours 00 mins 00 secs"},
		{"bytes", []byte("10:20:30"), "10 hours 20 mins 30 secs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mysqlTransformValue(tt.input, col, tm)
			if err != nil {
				t.Fatalf("error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTransformValue_TimeAsTime(t *testing.T) {
	col := Column{DataType: "time", ColumnType: "time"}
	tm := defaultTypeMappingConfig()

	got, err := mysqlTransformValue("12:30:45", col, tm)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != "12:30:45" {
		t.Errorf("got %q, want %q", got, "12:30:45")
	}
}

func TestTransformValue_TimeAsText(t *testing.T) {
	col := Column{DataType: "time", ColumnType: "time"}
	tm := TypeMappingConfig{TimeMode: "text", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122"}

	got, err := mysqlTransformValue("838:59:59", col, tm)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != "838:59:59" {
		t.Errorf("got %q, want %q", got, "838:59:59")
	}
}

func TestTransformValue_SpatialWKBBytea(t *testing.T) {
	col := Column{DataType: "geometry", ColumnType: "geometry"}
	tm := TypeMappingConfig{SpatialMode: "wkb_bytea", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}

	// wkb_bytea mode: raw bytes pass through as-is (bytea)
	in := []byte{0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00}
	got, err := mysqlTransformValue(in, col, tm)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	out, ok := got.([]byte)
	if !ok {
		t.Fatalf("type = %T, want []byte", got)
	}
	if len(out) != len(in) {
		t.Errorf("len = %d, want %d", len(out), len(in))
	}
}

func TestTransformValue_SpatialWKTText(t *testing.T) {
	col := Column{DataType: "point", ColumnType: "point"}
	tm := TypeMappingConfig{SpatialMode: "wkt_text", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}

	in := []byte{0xAB, 0xCD}
	got, err := mysqlTransformValue(in, col, tm)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("type = %T, want string", got)
	}
	if s != "abcd" {
		t.Errorf("got %q, want %q", s, "abcd")
	}
}

func TestTransformValue_SpatialNil(t *testing.T) {
	col := Column{DataType: "geometry", ColumnType: "geometry"}
	tm := TypeMappingConfig{SpatialMode: "wkb_bytea", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true, BitMode: "bytea", Binary16UUIDMode: "rfc4122", TimeMode: "time", ZeroDateMode: "null"}

	got, err := mysqlTransformValue(nil, col, tm)
	if err != nil || got != nil {
		t.Errorf("nil spatial: got %v, want nil", got)
	}
}

func TestTransformValue_TimeNil(t *testing.T) {
	col := Column{DataType: "time", ColumnType: "time"}
	got, err := mysqlTransformValue(nil, col, defaultTypeMappingConfig())
	if err != nil || got != nil {
		t.Errorf("nil TIME: got %v, want nil", got)
	}
}

func TestTransformValue_StringUUIDDisabledPassthrough(t *testing.T) {
	col := Column{DataType: "varchar", ColumnType: "varchar(36)", CharMaxLen: 36}
	got, err := mysqlTransformValue("hello", col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want passthrough", got)
	}
}

func TestTransformValue_BitNilPassthrough(t *testing.T) {
	col := Column{DataType: "bit", ColumnType: "bit(8)", Precision: 8}
	tm := TypeMappingConfig{BitMode: "bit", EnumMode: "text", SetMode: "text", SanitizeJSONNullBytes: true}

	got, err := mysqlTransformValue(nil, col, tm)
	if err != nil {
		t.Fatalf("mysqlTransformValue(nil bit) error: %v", err)
	}
	if got != nil {
		t.Errorf("mysqlTransformValue(nil bit) = %v, want nil", got)
	}
}
