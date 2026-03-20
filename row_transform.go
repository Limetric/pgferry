package main

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

type valueTransformer func(any) (any, error)

func buildRowValueTransformers(table Table, src SourceDB, typeMap TypeMappingConfig) []valueTransformer {
	transformers := make([]valueTransformer, len(table.Columns))

	switch src.(type) {
	case *mysqlSourceDB:
		for i, col := range table.Columns {
			transformers[i] = buildMySQLValueTransformer(col, typeMap)
		}
	case *mariadbSourceDB:
		for i, col := range table.Columns {
			transformers[i] = buildMariaDBValueTransformer(col, typeMap)
		}
	case *mssqlSourceDB:
		for i, col := range table.Columns {
			transformers[i] = buildMSSQLValueTransformer(col)
		}
	case *sqliteSourceDB:
		for i := range table.Columns {
			transformers[i] = identityValueTransformer
		}
	default:
		for i, col := range table.Columns {
			transformers[i] = func(col Column) valueTransformer {
				return func(val any) (any, error) {
					return src.TransformValue(val, col, typeMap)
				}
			}(col)
		}
	}

	return transformers
}

func identityValueTransformer(val any) (any, error) {
	return val, nil
}

func withNilPassthrough(fn func(any) (any, error)) valueTransformer {
	return func(val any) (any, error) {
		if val == nil {
			return nil, nil
		}
		return fn(val)
	}
}

func buildMySQLValueTransformer(col Column, typeMap TypeMappingConfig) valueTransformer {
	switch {
	case isBinary16Column(col) && typeMap.Binary16AsUUID:
		return withNilPassthrough(func(val any) (any, error) {
			b, ok := val.([]byte)
			if !ok || len(b) != 16 {
				return nil, fmt.Errorf("expected 16-byte binary UUID payload, got %T", val)
			}
			return formatMySQLBinaryUUID(b, typeMap.Binary16UUIDMode == "mysql_uuid_to_bin_swap"), nil
		})

	case col.DataType == "json" && typeMap.SanitizeJSONNullBytes:
		return withNilPassthrough(stripNULStringLikeValue)

	case isStringUUIDColumn(col) && typeMap.StringUUIDAsUUID:
		return withNilPassthrough(func(val any) (any, error) {
			return normalizeUUIDStringValue(val, "string_uuid_as_uuid")
		})

	case isTinyInt1Column(col) && typeMap.TinyInt1AsBoolean:
		return withNilPassthrough(mysqlTinyIntBooleanTransformer)

	case col.DataType == "set" && (typeMap.SetMode == "text_array" || typeMap.SetMode == "text_array_check"):
		return withNilPassthrough(mysqlSetTextArrayTransformer)

	case col.DataType == "bit" && (typeMap.BitMode == "bit" || typeMap.BitMode == "varbit"):
		bitWidth := mysqlBitWidth(col)
		return withNilPassthrough(func(val any) (any, error) {
			b, ok := val.([]byte)
			if !ok {
				return nil, fmt.Errorf("expected []byte for BIT value, got %T", val)
			}
			return mysqlBitString(b, bitWidth), nil
		})

	case col.DataType == "year":
		return withNilPassthrough(mysqlYearTransformer)

	case col.DataType == "time":
		if typeMap.TimeMode == "interval" {
			return withNilPassthrough(func(val any) (any, error) {
				raw, ok := stringLikeValue(val)
				if !ok {
					return val, nil
				}
				return mysqlTimeToInterval(strings.TrimSpace(raw))
			})
		}
		return withNilPassthrough(func(val any) (any, error) {
			raw, ok := stringLikeValue(val)
			if !ok {
				return val, nil
			}
			return strings.TrimSpace(raw), nil
		})

	case col.DataType == "date":
		zeroDateMode := typeMap.ZeroDateMode
		sourceName := col.SourceName
		return withNilPassthrough(func(val any) (any, error) {
			t, ok := val.(time.Time)
			if ok && t.IsZero() {
				if zeroDateMode == "error" {
					return nil, fmt.Errorf("zero date value in column %s (zero_date_mode=error)", sourceName)
				}
				return nil, nil
			}
			return val, nil
		})

	case col.DataType == "timestamp" || col.DataType == "datetime":
		zeroDateMode := typeMap.ZeroDateMode
		sourceName := col.SourceName
		return withNilPassthrough(func(val any) (any, error) {
			t, ok := val.(time.Time)
			if ok && t.IsZero() {
				if zeroDateMode == "error" {
					return nil, fmt.Errorf("zero date/time value in column %s (zero_date_mode=error)", sourceName)
				}
				return nil, nil
			}
			return val, nil
		})

	case isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkb_bytea":
		return identityValueTransformer

	case isMySQLSpatialType(col.DataType) && typeMap.UsePostGIS:
		return withNilPassthrough(func(val any) (any, error) {
			b, ok := val.([]byte)
			if !ok {
				return nil, fmt.Errorf("expected []byte for MySQL spatial value, got %T", val)
			}
			return mysqlSpatialToEWKB(b)
		})

	case isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkt_text":
		return withNilPassthrough(func(val any) (any, error) {
			switch v := val.(type) {
			case []byte:
				return string(v), nil
			case string:
				return v, nil
			default:
				return nil, fmt.Errorf("expected string/[]byte for spatial WKT value, got %T", val)
			}
		})

	case isMySQLStringLikeColumn(col):
		return withNilPassthrough(stripNULStringLikeValue)

	default:
		return identityValueTransformer
	}
}

func buildMariaDBValueTransformer(col Column, typeMap TypeMappingConfig) valueTransformer {
	if col.DataType == "uuid" {
		return withNilPassthrough(func(val any) (any, error) {
			return normalizeUUIDStringValue(val, "MariaDB uuid")
		})
	}
	return buildMySQLValueTransformer(col, typeMap)
}

func buildMSSQLValueTransformer(col Column) valueTransformer {
	switch col.DataType {
	case "uniqueidentifier":
		return withNilPassthrough(func(val any) (any, error) {
			switch v := val.(type) {
			case []byte:
				if len(v) == 16 {
					return formatMSSQLUniqueidentifier(v), nil
				}
				return string(v), nil
			case string:
				return strings.ToLower(v), nil
			default:
				return val, nil
			}
		})

	case "money", "smallmoney":
		return withNilPassthrough(func(val any) (any, error) {
			switch v := val.(type) {
			case []byte:
				return string(v), nil
			case string:
				return v, nil
			case float64:
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("MSSQL %s value is not finite (NaN/Inf cannot be copied to PostgreSQL numeric)", col.DataType)
				}
				return strconv.FormatFloat(v, 'f', 4, 64), nil
			default:
				return val, nil
			}
		})

	case "bit":
		return identityValueTransformer

	case "varchar", "nvarchar", "char", "nchar", "text", "ntext", "xml", "json":
		return withNilPassthrough(stripNULStringLikeValue)

	default:
		return identityValueTransformer
	}
}

func isMySQLStringLikeColumn(col Column) bool {
	switch col.DataType {
	case "varchar", "char", "text", "mediumtext", "longtext", "tinytext", "enum", "set":
		return true
	default:
		return false
	}
}

func stringLikeValue(val any) (string, bool) {
	switch v := val.(type) {
	case []byte:
		return string(v), true
	case string:
		return v, true
	default:
		return "", false
	}
}

func stripNULStringLikeValue(val any) (any, error) {
	switch v := val.(type) {
	case []byte:
		return stripNULBytesToString(v), nil
	case string:
		return stripNULString(v), nil
	default:
		return val, nil
	}
}

func mysqlSetTextArrayTransformer(val any) (any, error) {
	raw, ok := stringLikeValue(val)
	if !ok {
		return nil, fmt.Errorf("cannot coerce set value of type %T to text[]", val)
	}
	raw = stripNULString(raw)
	if raw == "" {
		return []string{}, nil
	}
	return strings.Split(raw, ","), nil
}

func mysqlTinyIntBooleanTransformer(val any) (any, error) {
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
		if len(v) == 1 {
			switch v[0] {
			case '0':
				return false, nil
			case '1':
				return true, nil
			}
		}
		return nil, fmt.Errorf("cannot coerce tinyint(1) value %q to boolean", string(v))
	case bool:
		return v, nil
	default:
		return nil, fmt.Errorf("cannot coerce tinyint(1) value of type %T to boolean", val)
	}
}

func mysqlYearTransformer(val any) (any, error) {
	switch v := val.(type) {
	case int64:
		return v, nil
	case []byte:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse year value %q: %w", string(v), err)
		}
		return n, nil
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse year value %q: %w", v, err)
		}
		return n, nil
	default:
		return nil, fmt.Errorf("cannot coerce year value of type %T to integer", val)
	}
}

func mysqlBitWidth(col Column) int64 {
	bitWidth := int64(col.MySQLBitWidth)
	if bitWidth > 0 {
		return bitWidth
	}
	if parsed, ok := mysqlColumnTypeLength(col.ColumnType, "bit"); ok {
		return parsed
	}
	if col.Precision > 0 {
		return col.Precision
	}
	return 0
}

func mysqlBitString(b []byte, bitWidth int64) string {
	buf := make([]byte, len(b)*8)
	pos := 0
	for _, byt := range b {
		for i := 7; i >= 0; i-- {
			if byt&(1<<uint(i)) != 0 {
				buf[pos] = '1'
			} else {
				buf[pos] = '0'
			}
			pos++
		}
	}
	if bitWidth <= 0 {
		return string(buf)
	}
	if bitWidth >= int64(len(buf)) {
		return string(buf)
	}
	return string(buf[len(buf)-int(bitWidth):])
}

func formatMySQLBinaryUUID(b []byte, swapped bool) string {
	if !swapped {
		return formatRFC4122UUID(b)
	}

	var unswapped [16]byte
	copy(unswapped[0:4], b[4:8])
	copy(unswapped[4:6], b[2:4])
	copy(unswapped[6:8], b[0:2])
	copy(unswapped[8:16], b[8:16])
	return formatRFC4122UUID(unswapped[:])
}

func formatRFC4122UUID(b []byte) string {
	var dst [36]byte
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst[:])
}

func formatMSSQLUniqueidentifier(b []byte) string {
	var dst [36]byte
	writeHexByte(dst[:], 0, b[3])
	writeHexByte(dst[:], 2, b[2])
	writeHexByte(dst[:], 4, b[1])
	writeHexByte(dst[:], 6, b[0])
	dst[8] = '-'
	writeHexByte(dst[:], 9, b[5])
	writeHexByte(dst[:], 11, b[4])
	dst[13] = '-'
	writeHexByte(dst[:], 14, b[7])
	writeHexByte(dst[:], 16, b[6])
	dst[18] = '-'
	writeHexByte(dst[:], 19, b[8])
	writeHexByte(dst[:], 21, b[9])
	dst[23] = '-'
	writeHexByte(dst[:], 24, b[10])
	writeHexByte(dst[:], 26, b[11])
	writeHexByte(dst[:], 28, b[12])
	writeHexByte(dst[:], 30, b[13])
	writeHexByte(dst[:], 32, b[14])
	writeHexByte(dst[:], 34, b[15])
	return string(dst[:])
}

func writeHexByte(dst []byte, offset int, v byte) {
	const hexDigits = "0123456789abcdef"
	dst[offset] = hexDigits[v>>4]
	dst[offset+1] = hexDigits[v&0x0f]
}
