package main

import (
	"fmt"
	"strings"
	"time"
)

// mapType returns the PostgreSQL type for a given MySQL column.
func mapType(col Column, typeMap TypeMappingConfig) (string, error) {
	isUnsigned := strings.Contains(col.ColumnType, "unsigned")

	switch {
	case col.DataType == "binary" && col.Precision == 16 && typeMap.Binary16AsUUID:
		return "uuid", nil
	case col.DataType == "tinyint" && col.Precision == 1 && typeMap.TinyInt1AsBoolean:
		return "boolean", nil
	case col.DataType == "tinyint":
		return "smallint", nil
	case col.DataType == "smallint":
		if isUnsigned {
			return "integer", nil
		}
		return "smallint", nil
	case col.DataType == "mediumint":
		return "integer", nil
	case col.DataType == "int":
		if isUnsigned {
			return "bigint", nil
		}
		return "integer", nil
	case col.DataType == "bigint":
		if isUnsigned {
			return "numeric(20)", nil
		}
		return "bigint", nil
	case col.DataType == "float":
		return "real", nil
	case col.DataType == "double":
		return "double precision", nil
	case col.DataType == "decimal":
		return fmt.Sprintf("numeric(%d,%d)", col.Precision, col.Scale), nil
	case col.DataType == "varchar":
		return fmt.Sprintf("varchar(%d)", col.CharMaxLen), nil
	case col.DataType == "char":
		return fmt.Sprintf("varchar(%d)", col.CharMaxLen), nil // char→varchar per pgloader convention
	case col.DataType == "text", col.DataType == "mediumtext", col.DataType == "longtext", col.DataType == "tinytext":
		return "text", nil
	case col.DataType == "json":
		if typeMap.JSONAsJSONB {
			return "jsonb", nil
		}
		return "json", nil
	case col.DataType == "enum":
		switch typeMap.EnumMode {
		case "text", "check":
			return "text", nil
		default:
			return "", fmt.Errorf("unsupported enum_mode %q", typeMap.EnumMode)
		}
	case col.DataType == "set":
		switch typeMap.SetMode {
		case "text":
			return "text", nil
		case "text_array":
			return "text[]", nil
		default:
			return "", fmt.Errorf("unsupported set_mode %q", typeMap.SetMode)
		}
	case col.DataType == "timestamp":
		return "timestamptz", nil
	case col.DataType == "datetime":
		if typeMap.DatetimeAsTimestamptz {
			return "timestamptz", nil
		}
		return "timestamp", nil
	case col.DataType == "date":
		return "date", nil
	case col.DataType == "binary", col.DataType == "varbinary", col.DataType == "blob",
		col.DataType == "mediumblob", col.DataType == "longblob", col.DataType == "tinyblob":
		return "bytea", nil
	default:
		if typeMap.UnknownAsText {
			return "text", nil
		}
		return "", fmt.Errorf("unsupported MySQL type %q (column_type=%q)", col.DataType, col.ColumnType)
	}
}

// transformValue converts a MySQL row value to its PostgreSQL equivalent.
func transformValue(val any, col Column, typeMap TypeMappingConfig) (any, error) {
	if val == nil {
		return nil, nil
	}

	switch {
	// binary(16) → uuid string
	case col.DataType == "binary" && col.Precision == 16 && typeMap.Binary16AsUUID:
		b, ok := val.([]byte)
		if !ok || len(b) != 16 {
			return nil, fmt.Errorf("expected 16-byte binary UUID payload, got %T", val)
		}
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil

	// json → strip null bytes (MySQL allows \x00, PG doesn't)
	case col.DataType == "json" && typeMap.SanitizeJSONNullBytes:
		switch v := val.(type) {
		case []byte:
			return strings.ReplaceAll(string(v), "\x00", ""), nil
		case string:
			return strings.ReplaceAll(v, "\x00", ""), nil
		}
		return val, nil

	// tinyint(1) → bool
	case col.DataType == "tinyint" && col.Precision == 1 && typeMap.TinyInt1AsBoolean:
		switch v := val.(type) {
		case int64:
			if v == 0 {
				return false, nil
			}
			if v == 1 {
				return true, nil
			}
			return nil, fmt.Errorf("cannot coerce tinyint(1) value %d to boolean", v)
		case []byte:
			if string(v) == "0" {
				return false, nil
			}
			if string(v) == "1" {
				return true, nil
			}
			return nil, fmt.Errorf("cannot coerce tinyint(1) value %q to boolean", string(v))
		case bool:
			return v, nil
		}
		return nil, fmt.Errorf("cannot coerce tinyint(1) value of type %T to boolean", val)

	// set → text[] (optional)
	case col.DataType == "set" && typeMap.SetMode == "text_array":
		var raw string
		switch v := val.(type) {
		case []byte:
			raw = string(v)
		case string:
			raw = v
		default:
			return nil, fmt.Errorf("cannot coerce set value of type %T to text[]", val)
		}
		if raw == "" {
			return []string{}, nil
		}
		parts := strings.Split(raw, ",")
		return parts, nil

	// date → zero dates to null
	case col.DataType == "date":
		t, ok := val.(time.Time)
		if ok && t.IsZero() {
			return nil, nil
		}
		return val, nil

	// timestamp/datetime → zero dates to null
	case col.DataType == "timestamp" || col.DataType == "datetime":
		t, ok := val.(time.Time)
		if ok && t.IsZero() {
			return nil, nil
		}
		return val, nil

	default:
		return val, nil
	}
}
