package main

import (
	"testing"
	"time"
)

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
		{"tinyint1→smallint default", Column{DataType: "tinyint", Precision: 1, ColumnType: "tinyint(1)"}, defaultTypeMappingConfig(), "smallint", false},
		{"tinyint1→bool opt-in", Column{DataType: "tinyint", Precision: 1, ColumnType: "tinyint(1)"}, TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}, "boolean", false},
		{"tinyint→smallint", Column{DataType: "tinyint", Precision: 3, ColumnType: "tinyint(3)"}, defaultTypeMappingConfig(), "smallint", false},
		{"smallint unsigned→integer", Column{DataType: "smallint", ColumnType: "smallint unsigned"}, defaultTypeMappingConfig(), "integer", false},
		{"int unsigned→bigint", Column{DataType: "int", ColumnType: "int unsigned"}, defaultTypeMappingConfig(), "bigint", false},
		{"bigint unsigned→numeric20", Column{DataType: "bigint", ColumnType: "bigint unsigned"}, defaultTypeMappingConfig(), "numeric(20)", false},
		{"mediumint→integer", Column{DataType: "mediumint", ColumnType: "mediumint"}, defaultTypeMappingConfig(), "integer", false},
		{"bigint", Column{DataType: "bigint", ColumnType: "bigint"}, defaultTypeMappingConfig(), "bigint", false},
		{"float→real", Column{DataType: "float", ColumnType: "float"}, defaultTypeMappingConfig(), "real", false},
		{"double", Column{DataType: "double", ColumnType: "double"}, defaultTypeMappingConfig(), "double precision", false},
		{"decimal", Column{DataType: "decimal", ColumnType: "decimal(10,7)", Precision: 10, Scale: 7}, defaultTypeMappingConfig(), "numeric(10,7)", false},
		{"varchar", Column{DataType: "varchar", ColumnType: "varchar(200)", CharMaxLen: 200}, defaultTypeMappingConfig(), "varchar(200)", false},
		{"char→varchar", Column{DataType: "char", ColumnType: "char(64)", CharMaxLen: 64}, defaultTypeMappingConfig(), "varchar(64)", false},
		{"text", Column{DataType: "text", ColumnType: "text"}, defaultTypeMappingConfig(), "text", false},
		{"mediumtext", Column{DataType: "mediumtext", ColumnType: "mediumtext"}, defaultTypeMappingConfig(), "text", false},
		{"json→json default", Column{DataType: "json", ColumnType: "json"}, defaultTypeMappingConfig(), "json", false},
		{"json→jsonb opt-in", Column{DataType: "json", ColumnType: "json"}, TypeMappingConfig{JSONAsJSONB: true, SanitizeJSONNullBytes: true}, "jsonb", false},
		{"enum→text", Column{DataType: "enum", ColumnType: "enum('a','b')"}, defaultTypeMappingConfig(), "text", false},
		{"timestamp→timestamptz", Column{DataType: "timestamp", ColumnType: "timestamp"}, defaultTypeMappingConfig(), "timestamptz", false},
		{"datetime→timestamp default", Column{DataType: "datetime", ColumnType: "datetime"}, defaultTypeMappingConfig(), "timestamp", false},
		{"datetime→timestamptz opt-in", Column{DataType: "datetime", ColumnType: "datetime"}, TypeMappingConfig{DatetimeAsTimestamptz: true, SanitizeJSONNullBytes: true}, "timestamptz", false},
		{"date", Column{DataType: "date", ColumnType: "date"}, defaultTypeMappingConfig(), "date", false},
		{"binary→bytea", Column{DataType: "binary", ColumnType: "binary(32)", Precision: 32}, defaultTypeMappingConfig(), "bytea", false},
		{"varbinary→bytea", Column{DataType: "varbinary", ColumnType: "varbinary(32)"}, defaultTypeMappingConfig(), "bytea", false},
		{"unknown→error default", Column{DataType: "geometry", ColumnType: "geometry"}, defaultTypeMappingConfig(), "", true},
		{"unknown→text opt-in", Column{DataType: "geometry", ColumnType: "geometry"}, TypeMappingConfig{UnknownAsText: true, SanitizeJSONNullBytes: true}, "text", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mapType(tt.col, tt.tm)
			if tt.err {
				if err == nil {
					t.Fatalf("mapType(%+v) expected error", tt.col)
				}
				return
			}
			if err != nil {
				t.Fatalf("mapType(%+v) unexpected error: %v", tt.col, err)
			}
			if got != tt.want {
				t.Errorf("mapType(%+v) = %q, want %q", tt.col, got, tt.want)
			}
		})
	}
}

func TestTransformValue_UUID(t *testing.T) {
	col := Column{DataType: "binary", Precision: 16}
	optIn := TypeMappingConfig{Binary16AsUUID: true, SanitizeJSONNullBytes: true}

	// Valid 16-byte UUID
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	got, err := transformValue(input, col, optIn)
	if err != nil {
		t.Fatalf("transformValue(uuid) error: %v", err)
	}
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != want {
		t.Errorf("transformValue(uuid) = %q, want %q", got, want)
	}

	// Nil input
	if got, err := transformValue(nil, col, optIn); err != nil || got != nil {
		t.Errorf("transformValue(nil, uuid) = %v, want nil", got)
	}

	// Wrong length
	if _, err := transformValue([]byte{0x01, 0x02}, col, optIn); err == nil {
		t.Fatal("transformValue(short bytes, uuid) expected error")
	}
}

func TestTransformValue_Bool(t *testing.T) {
	col := Column{DataType: "tinyint", Precision: 1}
	optIn := TypeMappingConfig{TinyInt1AsBoolean: true, SanitizeJSONNullBytes: true}

	if got, err := transformValue(int64(1), col, optIn); err != nil || got != true {
		t.Errorf("transformValue(1, bool) = %v, want true", got)
	}
	if got, err := transformValue(int64(0), col, optIn); err != nil || got != false {
		t.Errorf("transformValue(0, bool) = %v, want false", got)
	}
	if got, err := transformValue(nil, col, optIn); err != nil || got != nil {
		t.Errorf("transformValue(nil, bool) = %v, want nil", got)
	}
	if _, err := transformValue(int64(2), col, optIn); err == nil {
		t.Fatal("transformValue(2, bool) expected error")
	}
}

func TestTransformValue_BoolNoOptInPassthrough(t *testing.T) {
	col := Column{DataType: "tinyint", Precision: 1}
	got, err := transformValue(int64(2), col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("transformValue default tinyint(1) unexpected error: %v", err)
	}
	if got != int64(2) {
		t.Fatalf("transformValue default tinyint(1) = %v, want %v", got, int64(2))
	}
}

func TestTransformValue_JSON(t *testing.T) {
	col := Column{DataType: "json"}

	input := []byte("hello\x00world")
	got, err := transformValue(input, col, defaultTypeMappingConfig())
	if err != nil {
		t.Fatalf("transformValue(json) error: %v", err)
	}
	if got != "helloworld" {
		t.Errorf("transformValue(json with null byte) = %q, want %q", got, "helloworld")
	}

	if got, err := transformValue(nil, col, defaultTypeMappingConfig()); err != nil || got != nil {
		t.Errorf("transformValue(nil, json) = %v, want nil", got)
	}
}

func TestTransformValue_ZeroDates(t *testing.T) {
	for _, dt := range []string{"date", "timestamp", "datetime"} {
		col := Column{DataType: dt}

		// Zero time → nil
		if got, err := transformValue(time.Time{}, col, defaultTypeMappingConfig()); err != nil || got != nil {
			t.Errorf("transformValue(zero %s) = %v, want nil", dt, got)
		}

		// Valid time → pass through
		valid := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		if got, err := transformValue(valid, col, defaultTypeMappingConfig()); err != nil || got != valid {
			t.Errorf("transformValue(valid %s) = %v, want %v", dt, got, valid)
		}

		// Nil → nil
		if got, err := transformValue(nil, col, defaultTypeMappingConfig()); err != nil || got != nil {
			t.Errorf("transformValue(nil, %s) = %v, want nil", dt, got)
		}
	}
}

func TestTransformValue_Passthrough(t *testing.T) {
	col := Column{DataType: "varchar"}
	if got, err := transformValue("hello", col, defaultTypeMappingConfig()); err != nil || got != "hello" {
		t.Errorf("transformValue(varchar) = %v, want %q", got, "hello")
	}
}
