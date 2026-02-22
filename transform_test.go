package main

import (
	"testing"
	"time"
)

func TestMapType(t *testing.T) {
	tests := []struct {
		name string
		col  Column
		want string
	}{
		{"binary16→uuid", Column{DataType: "binary", Precision: 16}, "uuid"},
		{"tinyint1→bool", Column{DataType: "tinyint", Precision: 1}, "boolean"},
		{"tinyint→smallint", Column{DataType: "tinyint", Precision: 3}, "smallint"},
		{"int→integer", Column{DataType: "int"}, "integer"},
		{"mediumint→integer", Column{DataType: "mediumint"}, "integer"},
		{"bigint", Column{DataType: "bigint"}, "bigint"},
		{"float→real", Column{DataType: "float"}, "real"},
		{"double", Column{DataType: "double"}, "double precision"},
		{"decimal", Column{DataType: "decimal", Precision: 10, Scale: 7}, "numeric(10,7)"},
		{"varchar", Column{DataType: "varchar", CharMaxLen: 200}, "varchar(200)"},
		{"char→varchar", Column{DataType: "char", CharMaxLen: 64}, "varchar(64)"},
		{"text", Column{DataType: "text"}, "text"},
		{"mediumtext", Column{DataType: "mediumtext"}, "text"},
		{"json→jsonb", Column{DataType: "json"}, "jsonb"},
		{"enum→text", Column{DataType: "enum"}, "text"},
		{"timestamp→timestamptz", Column{DataType: "timestamp"}, "timestamptz"},
		{"datetime→timestamptz", Column{DataType: "datetime"}, "timestamptz"},
		{"date", Column{DataType: "date"}, "date"},
		{"binary→bytea", Column{DataType: "binary", Precision: 32}, "bytea"},
		{"varbinary→bytea", Column{DataType: "varbinary"}, "bytea"},
		{"unknown→text", Column{DataType: "geometry"}, "text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapType(tt.col)
			if got != tt.want {
				t.Errorf("mapType(%+v) = %q, want %q", tt.col, got, tt.want)
			}
		})
	}
}

func TestTransformValue_UUID(t *testing.T) {
	col := Column{DataType: "binary", Precision: 16}

	// Valid 16-byte UUID
	input := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	got := transformValue(input, col)
	want := "01020304-0506-0708-090a-0b0c0d0e0f10"
	if got != want {
		t.Errorf("transformValue(uuid) = %q, want %q", got, want)
	}

	// Nil input
	if got := transformValue(nil, col); got != nil {
		t.Errorf("transformValue(nil, uuid) = %v, want nil", got)
	}

	// Wrong length
	if got := transformValue([]byte{0x01, 0x02}, col); got != nil {
		t.Errorf("transformValue(short bytes, uuid) = %v, want nil", got)
	}
}

func TestTransformValue_Bool(t *testing.T) {
	col := Column{DataType: "tinyint", Precision: 1}

	if got := transformValue(int64(1), col); got != true {
		t.Errorf("transformValue(1, bool) = %v, want true", got)
	}
	if got := transformValue(int64(0), col); got != false {
		t.Errorf("transformValue(0, bool) = %v, want false", got)
	}
	if got := transformValue(nil, col); got != nil {
		t.Errorf("transformValue(nil, bool) = %v, want nil", got)
	}
}

func TestTransformValue_JSON(t *testing.T) {
	col := Column{DataType: "json"}

	input := []byte("hello\x00world")
	got := transformValue(input, col)
	if got != "helloworld" {
		t.Errorf("transformValue(json with null byte) = %q, want %q", got, "helloworld")
	}

	if got := transformValue(nil, col); got != nil {
		t.Errorf("transformValue(nil, json) = %v, want nil", got)
	}
}

func TestTransformValue_ZeroDates(t *testing.T) {
	for _, dt := range []string{"date", "timestamp", "datetime"} {
		col := Column{DataType: dt}

		// Zero time → nil
		if got := transformValue(time.Time{}, col); got != nil {
			t.Errorf("transformValue(zero %s) = %v, want nil", dt, got)
		}

		// Valid time → pass through
		valid := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		if got := transformValue(valid, col); got != valid {
			t.Errorf("transformValue(valid %s) = %v, want %v", dt, got, valid)
		}

		// Nil → nil
		if got := transformValue(nil, col); got != nil {
			t.Errorf("transformValue(nil, %s) = %v, want nil", dt, got)
		}
	}
}

func TestTransformValue_Passthrough(t *testing.T) {
	col := Column{DataType: "varchar"}
	if got := transformValue("hello", col); got != "hello" {
		t.Errorf("transformValue(varchar) = %v, want %q", got, "hello")
	}
}
