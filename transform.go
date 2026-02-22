package main

import (
	"fmt"
	"strings"
	"time"
)

// mapType returns the PostgreSQL type for a given MySQL column.
func mapType(col Column) string {
	switch {
	case col.DataType == "binary" && col.Precision == 16:
		return "uuid"
	case col.DataType == "tinyint" && col.Precision == 1:
		return "boolean"
	case col.DataType == "tinyint":
		return "smallint"
	case col.DataType == "smallint":
		return "smallint"
	case col.DataType == "mediumint":
		return "integer"
	case col.DataType == "int":
		return "integer"
	case col.DataType == "bigint":
		return "bigint"
	case col.DataType == "float":
		return "real"
	case col.DataType == "double":
		return "double precision"
	case col.DataType == "decimal":
		return fmt.Sprintf("numeric(%d,%d)", col.Precision, col.Scale)
	case col.DataType == "varchar":
		return fmt.Sprintf("varchar(%d)", col.CharMaxLen)
	case col.DataType == "char":
		return fmt.Sprintf("varchar(%d)", col.CharMaxLen) // char→varchar per pgloader convention
	case col.DataType == "text", col.DataType == "mediumtext", col.DataType == "longtext", col.DataType == "tinytext":
		return "text"
	case col.DataType == "json":
		return "jsonb"
	case col.DataType == "enum":
		return "text"
	case col.DataType == "timestamp", col.DataType == "datetime":
		return "timestamptz"
	case col.DataType == "date":
		return "date"
	case col.DataType == "binary", col.DataType == "varbinary", col.DataType == "blob",
		col.DataType == "mediumblob", col.DataType == "longblob", col.DataType == "tinyblob":
		return "bytea"
	default:
		return "text" // safe fallback
	}
}

// transformValue converts a MySQL row value to its PostgreSQL equivalent.
func transformValue(val any, col Column) any {
	if val == nil {
		return nil
	}

	switch {
	// binary(16) → uuid string
	case col.DataType == "binary" && col.Precision == 16:
		b, ok := val.([]byte)
		if !ok || len(b) != 16 {
			return nil
		}
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])

	// json → strip null bytes (MySQL allows \x00, PG doesn't)
	case col.DataType == "json":
		switch v := val.(type) {
		case []byte:
			return strings.ReplaceAll(string(v), "\x00", "")
		case string:
			return strings.ReplaceAll(v, "\x00", "")
		}
		return val

	// tinyint(1) → bool
	case col.DataType == "tinyint" && col.Precision == 1:
		switch v := val.(type) {
		case int64:
			return v != 0
		case []byte:
			return string(v) != "0"
		case bool:
			return v
		}
		return val

	// date → zero dates to null
	case col.DataType == "date":
		t, ok := val.(time.Time)
		if ok && t.IsZero() {
			return nil
		}
		return val

	// timestamp/datetime → zero dates to null
	case col.DataType == "timestamp" || col.DataType == "datetime":
		t, ok := val.(time.Time)
		if ok && t.IsZero() {
			return nil
		}
		return val

	default:
		return val
	}
}
