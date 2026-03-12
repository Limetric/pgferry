package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type mysqlSourceDB struct {
	snakeCaseIDs bool
	charset      string
}

func (m *mysqlSourceDB) SetSnakeCaseIdentifiers(enabled bool) { m.snakeCaseIDs = enabled }
func (m *mysqlSourceDB) SetCharset(charset string)            { m.charset = charset }
func (m *mysqlSourceDB) SetSourceSchema(_ string)             {}

// identName converts a source identifier to its PostgreSQL name.
// When snakeCaseIDs is true, applies toSnakeCase; otherwise lowercases.
func (m *mysqlSourceDB) identName(s string) string {
	if m.snakeCaseIDs {
		return toSnakeCase(s)
	}
	return strings.ToLower(s)
}

func (m *mysqlSourceDB) Name() string { return "MySQL" }

func (m *mysqlSourceDB) OpenDB(dsn string) (*sql.DB, error) {
	// Inject charset into DSN if not already present.
	// The go-sql-driver/mysql driver natively parses charset= and sends SET NAMES on connect.
	if m.charset != "" && !strings.Contains(dsn, "charset=") {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn = dsn + sep + "charset=" + m.charset
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse mysql dsn: %w", err)
	}
	cfg.ParseTime = true
	cfg.InterpolateParams = true
	cfg.Loc = time.UTC
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	return db, nil
}

func (m *mysqlSourceDB) ExtractDBName(dsn string) (string, error) {
	return extractMySQLDBName(dsn)
}

func (m *mysqlSourceDB) IntrospectSchema(db *sql.DB, dbName string) (*Schema, error) {
	return introspectMySQLSchema(db, dbName, m.identName)
}

func (m *mysqlSourceDB) IntrospectSourceObjects(db *sql.DB, dbName string) (*SourceObjects, error) {
	return introspectMySQLSourceObjects(db, dbName)
}

func (m *mysqlSourceDB) MapType(col Column, typeMap TypeMappingConfig) (string, error) {
	return mysqlMapType(col, typeMap)
}

func (m *mysqlSourceDB) MapDefault(col Column, pgType string, typeMap TypeMappingConfig) (string, error) {
	return mysqlMapDefault(col, pgType, typeMap)
}

func (m *mysqlSourceDB) TransformValue(val any, col Column, typeMap TypeMappingConfig) (any, error) {
	return mysqlTransformValue(val, col, typeMap)
}

func (m *mysqlSourceDB) QuoteIdentifier(name string) string {
	return fmt.Sprintf("`%s`", strings.ReplaceAll(name, "`", "``"))
}

func (m *mysqlSourceDB) SourceTableRef(table Table) string {
	return m.QuoteIdentifier(table.SourceName)
}

func (m *mysqlSourceDB) SupportsSnapshotMode() bool { return true }
func (m *mysqlSourceDB) MaxWorkers() int            { return 0 }

func (m *mysqlSourceDB) ValidateTypeMapping(typeMap TypeMappingConfig) error {
	var errs []string
	if typeMap.NvarcharAsText {
		errs = append(errs, "nvarchar_as_text is a MSSQL-only option")
	}
	if !typeMap.MoneyAsNumeric {
		errs = append(errs, "money_as_numeric is a MSSQL-only option")
	}
	if typeMap.XmlAsText {
		errs = append(errs, "xml_as_text is a MSSQL-only option")
	}
	if len(errs) > 0 {
		return fmt.Errorf("invalid type_mapping for MySQL source: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- Schema introspection (moved from schema.go) ---

func introspectMySQLSchema(db *sql.DB, dbName string, identName func(string) string) (*Schema, error) {
	tables, err := introspectMySQLTables(db, dbName, identName)
	if err != nil {
		return nil, fmt.Errorf("introspect tables: %w", err)
	}

	for i := range tables {
		t := &tables[i]

		cols, err := introspectMySQLColumns(db, dbName, t.SourceName, identName)
		if err != nil {
			return nil, fmt.Errorf("introspect columns for %s: %w", t.SourceName, err)
		}
		t.Columns = cols

		indexes, err := introspectMySQLIndexes(db, dbName, t.SourceName, identName)
		if err != nil {
			return nil, fmt.Errorf("introspect indexes for %s: %w", t.SourceName, err)
		}
		for _, idx := range indexes {
			if idx.IsPrimary {
				pk := idx
				t.PrimaryKey = &pk
			} else {
				t.Indexes = append(t.Indexes, idx)
			}
		}

		fks, err := introspectMySQLForeignKeys(db, dbName, t.SourceName, identName)
		if err != nil {
			return nil, fmt.Errorf("introspect foreign keys for %s: %w", t.SourceName, err)
		}
		t.ForeignKeys = fks
	}

	return &Schema{Tables: tables}, nil
}

func introspectMySQLTables(db *sql.DB, dbName string, identName func(string) string) ([]Table, error) {
	rows, err := db.Query(
		`SELECT TABLE_NAME FROM INFORMATION_SCHEMA.TABLES
		 WHERE TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE'
		 ORDER BY TABLE_NAME`,
		dbName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []Table
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, Table{
			SourceName: name,
			PGName:     identName(name),
		})
	}
	return tables, rows.Err()
}

func introspectMySQLColumns(db *sql.DB, dbName, tableName string, identName func(string) string) ([]Column, error) {
	rows, err := db.Query(
		`SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE,
		        COALESCE(CHARACTER_MAXIMUM_LENGTH, 0),
		        COALESCE(NUMERIC_PRECISION, 0),
		        COALESCE(NUMERIC_SCALE, 0),
		        IS_NULLABLE, COLUMN_DEFAULT, EXTRA, ORDINAL_POSITION,
		        COALESCE(CHARACTER_SET_NAME, ''),
		        COALESCE(COLLATION_NAME, ''),
		        COALESCE(GENERATION_EXPRESSION, '')
		 FROM INFORMATION_SCHEMA.COLUMNS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY ORDINAL_POSITION`,
		dbName, tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []Column
	for rows.Next() {
		var c Column
		var nullable string
		var dflt sql.NullString
		if err := rows.Scan(
			&c.SourceName, &c.DataType, &c.ColumnType,
			&c.CharMaxLen, &c.Precision, &c.Scale,
			&nullable, &dflt, &c.Extra, &c.OrdinalPos,
			&c.Charset, &c.Collation,
			&c.GenerationExpression,
		); err != nil {
			return nil, err
		}
		c.PGName = identName(c.SourceName)
		c.Nullable = nullable == "YES"
		if dflt.Valid {
			c.Default = &dflt.String
		}
		c.DataType = strings.ToLower(c.DataType)
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func isMySQLGeneratedColumn(col Column) bool {
	extra := strings.ToLower(col.Extra)
	return strings.Contains(extra, "virtual generated") || strings.Contains(extra, "stored generated")
}

func introspectMySQLIndexes(db *sql.DB, dbName, tableName string, identName func(string) string) ([]Index, error) {
	rows, err := db.Query(
		`SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE, SEQ_IN_INDEX, INDEX_TYPE, COLLATION, SUB_PART
		 FROM INFORMATION_SCHEMA.STATISTICS
		 WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		 ORDER BY INDEX_NAME, SEQ_IN_INDEX`,
		dbName, tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexMap := make(map[string]*Index)
	var indexOrder []string

	for rows.Next() {
		var idxName, indexType string
		var colName, collation sql.NullString
		var subPart sql.NullInt64
		var nonUnique, seqInIndex int
		if err := rows.Scan(&idxName, &colName, &nonUnique, &seqInIndex, &indexType, &collation, &subPart); err != nil {
			return nil, err
		}

		idx, ok := indexMap[idxName]
		if !ok {
			idx = &Index{
				Name:       identName(idxName),
				SourceName: idxName,
				Unique:     nonUnique == 0,
				IsPrimary:  idxName == "PRIMARY",
				Type:       strings.ToUpper(indexType),
			}
			indexMap[idxName] = idx
			indexOrder = append(indexOrder, idxName)
		}

		if subPart.Valid {
			idx.HasPrefix = true
		}
		if !colName.Valid {
			idx.HasExpression = true
			continue
		}

		idx.Columns = append(idx.Columns, identName(colName.String))
		if collation.Valid && strings.EqualFold(collation.String, "D") {
			idx.ColumnOrders = append(idx.ColumnOrders, "DESC")
		} else {
			idx.ColumnOrders = append(idx.ColumnOrders, "ASC")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var indexes []Index
	for _, name := range indexOrder {
		indexes = append(indexes, *indexMap[name])
	}
	return indexes, nil
}

func introspectMySQLForeignKeys(db *sql.DB, dbName, tableName string, identName func(string) string) ([]ForeignKey, error) {
	rows, err := db.Query(
		`SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME,
		        kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME,
		        rc.UPDATE_RULE, rc.DELETE_RULE
		 FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
		 JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc
		   ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		   AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
		 WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
		   AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		 ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`,
		dbName, tableName,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fkMap := make(map[string]*ForeignKey)
	var fkOrder []string

	for rows.Next() {
		var fkName, colName, refTable, refCol, updateRule, deleteRule string
		if err := rows.Scan(&fkName, &colName, &refTable, &refCol, &updateRule, &deleteRule); err != nil {
			return nil, err
		}

		fk, ok := fkMap[fkName]
		if !ok {
			fk = &ForeignKey{
				Name:       identName(fkName),
				RefTable:   refTable,
				RefPGTable: identName(refTable),
				UpdateRule: updateRule,
				DeleteRule: deleteRule,
			}
			fkMap[fkName] = fk
			fkOrder = append(fkOrder, fkName)
		}
		fk.Columns = append(fk.Columns, identName(colName))
		fk.RefColumns = append(fk.RefColumns, identName(refCol))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var fks []ForeignKey
	for _, name := range fkOrder {
		fks = append(fks, *fkMap[name])
	}
	return fks, nil
}

// --- Source objects introspection (moved from source_objects.go) ---

func introspectMySQLSourceObjects(db *sql.DB, dbName string) (*SourceObjects, error) {
	objs := &SourceObjects{}

	if err := collectStringRows(db, `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.VIEWS
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME
	`, dbName, &objs.Views); err != nil {
		return nil, fmt.Errorf("introspect views: %w", err)
	}

	rows, err := db.Query(`
		SELECT ROUTINE_TYPE, ROUTINE_NAME
		FROM INFORMATION_SCHEMA.ROUTINES
		WHERE ROUTINE_SCHEMA = ?
		ORDER BY ROUTINE_TYPE, ROUTINE_NAME
	`, dbName)
	if err != nil {
		return nil, fmt.Errorf("introspect routines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var routineType, routineName string
		if err := rows.Scan(&routineType, &routineName); err != nil {
			return nil, fmt.Errorf("scan routines: %w", err)
		}
		objs.Routines = append(objs.Routines, fmt.Sprintf("%s %s", strings.ToUpper(routineType), routineName))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routines: %w", err)
	}

	if err := collectStringRows(db, `
		SELECT TRIGGER_NAME
		FROM INFORMATION_SCHEMA.TRIGGERS
		WHERE TRIGGER_SCHEMA = ?
		ORDER BY TRIGGER_NAME
	`, dbName, &objs.Triggers); err != nil {
		return nil, fmt.Errorf("introspect triggers: %w", err)
	}

	return objs, nil
}

// --- Type mapping (moved from transform.go / type_compat.go) ---

func isBinary16Column(col Column) bool {
	return isMySQLTypeWithLength(col, "binary", 16)
}

// isStringUUIDColumn returns true for CHAR(36) and VARCHAR(36) columns,
// which are commonly used for storing UUID strings.
func isStringUUIDColumn(col Column) bool {
	return (col.DataType == "char" || col.DataType == "varchar") && col.CharMaxLen == 36
}

// isMySQLSpatialType returns true for MySQL spatial/geometry types.
func isMySQLSpatialType(dataType string) bool {
	switch dataType {
	case "geometry", "point", "linestring", "polygon",
		"multipoint", "multilinestring", "multipolygon", "geometrycollection":
		return true
	}
	return false
}

func isTinyInt1Column(col Column) bool {
	return isMySQLTypeWithLength(col, "tinyint", 1)
}

func isMySQLTypeWithLength(col Column, baseType string, wantLength int64) bool {
	if col.DataType != baseType {
		return false
	}
	if n, ok := mysqlColumnTypeLength(col.ColumnType, baseType); ok {
		return n == wantLength
	}
	return strings.TrimSpace(col.ColumnType) == "" && col.Precision == wantLength
}

func mysqlColumnTypeLength(columnType, baseType string) (int64, bool) {
	ct := strings.ToLower(strings.TrimSpace(columnType))
	prefix := baseType + "("
	if !strings.HasPrefix(ct, prefix) {
		return 0, false
	}
	rest := ct[len(prefix):]
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(rest[:end]), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func mysqlMapType(col Column, typeMap TypeMappingConfig) (string, error) {
	isUnsigned := strings.Contains(strings.ToLower(col.ColumnType), "unsigned")

	switch {
	case isBinary16Column(col) && typeMap.Binary16AsUUID:
		return "uuid", nil
	case isTinyInt1Column(col) && typeMap.TinyInt1AsBoolean:
		return "boolean", nil
	case col.DataType == "tinyint":
		return "smallint", nil
	case col.DataType == "smallint":
		if isUnsigned && typeMap.WidenUnsignedIntegers {
			return "integer", nil
		}
		return "smallint", nil
	case col.DataType == "mediumint":
		return "integer", nil
	case col.DataType == "int":
		if isUnsigned && typeMap.WidenUnsignedIntegers {
			return "bigint", nil
		}
		return "integer", nil
	case col.DataType == "bigint":
		if isUnsigned && typeMap.WidenUnsignedIntegers {
			return "numeric(20)", nil
		}
		return "bigint", nil
	case col.DataType == "float":
		return "real", nil
	case col.DataType == "double":
		return "double precision", nil
	case col.DataType == "decimal":
		return fmt.Sprintf("numeric(%d,%d)", col.Precision, col.Scale), nil
	case isStringUUIDColumn(col) && typeMap.StringUUIDAsUUID:
		return "uuid", nil
	case col.DataType == "varchar":
		if typeMap.VarcharAsText {
			return "text", nil
		}
		return fmt.Sprintf("varchar(%d)", col.CharMaxLen), nil
	case col.DataType == "char":
		if typeMap.VarcharAsText {
			return "text", nil
		}
		return fmt.Sprintf("varchar(%d)", col.CharMaxLen), nil
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
		case "native":
			values, err := parseMySQLEnumSetValues(col.ColumnType)
			if err != nil {
				return "", err
			}
			return pgEnumTypeName(values), nil
		default:
			return "", fmt.Errorf("unsupported enum_mode %q", typeMap.EnumMode)
		}
	case col.DataType == "set":
		switch typeMap.SetMode {
		case "text":
			return "text", nil
		case "text_array", "text_array_check":
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
	case col.DataType == "year":
		return "integer", nil
	case col.DataType == "date":
		return "date", nil
	case col.DataType == "time":
		switch typeMap.TimeMode {
		case "text":
			return "text", nil
		case "time":
			return "time", nil
		case "interval":
			return "interval", nil
		default:
			return "", fmt.Errorf("unsupported time_mode %q", typeMap.TimeMode)
		}
	case col.DataType == "bit":
		switch typeMap.BitMode {
		case "bit":
			n, ok := mysqlColumnTypeLength(col.ColumnType, "bit")
			if !ok {
				n = col.Precision
			}
			if n <= 0 {
				n = 1
			}
			return fmt.Sprintf("bit(%d)", n), nil
		case "varbit":
			return "varbit", nil
		default:
			return "bytea", nil
		}
	case col.DataType == "binary", col.DataType == "varbinary", col.DataType == "blob",
		col.DataType == "mediumblob", col.DataType == "longblob", col.DataType == "tinyblob":
		return "bytea", nil
	case isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkb_bytea":
		return "bytea", nil
	case isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkt_text":
		return "text", nil
	default:
		if typeMap.UnknownAsText {
			return "text", nil
		}
		return "", fmt.Errorf("unsupported MySQL type %q (column_type=%q)", col.DataType, col.ColumnType)
	}
}

func mysqlTransformValue(val any, col Column, typeMap TypeMappingConfig) (any, error) {
	if val == nil {
		return nil, nil
	}

	switch {
	case isBinary16Column(col) && typeMap.Binary16AsUUID:
		b, ok := val.([]byte)
		if !ok || len(b) != 16 {
			return nil, fmt.Errorf("expected 16-byte binary UUID payload, got %T", val)
		}
		if typeMap.Binary16UUIDMode == "mysql_uuid_to_bin_swap" {
			// MySQL UUID_TO_BIN(uuid, 1) swaps time_low and time_hi_and_version:
			// Storage:  [time_hi(2)][time_mid(2)][time_low(4)][clock_seq(2)][node(6)]
			// RFC 4122: [time_low(4)][time_mid(2)][time_hi(2)][clock_seq(2)][node(6)]
			var unswapped [16]byte
			copy(unswapped[0:4], b[4:8])   // time_low
			copy(unswapped[4:6], b[2:4])   // time_mid
			copy(unswapped[6:8], b[0:2])   // time_hi_and_version
			copy(unswapped[8:16], b[8:16]) // clock_seq + node
			b = unswapped[:]
		}
		return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil

	case col.DataType == "json" && typeMap.SanitizeJSONNullBytes:
		switch v := val.(type) {
		case []byte:
			return strings.ReplaceAll(string(v), "\x00", ""), nil
		case string:
			return strings.ReplaceAll(v, "\x00", ""), nil
		}
		return val, nil

	case isStringUUIDColumn(col) && typeMap.StringUUIDAsUUID:
		var s string
		switch v := val.(type) {
		case []byte:
			s = string(v)
		case string:
			s = v
		default:
			return nil, fmt.Errorf("expected string UUID value, got %T", val)
		}
		s = strings.TrimSpace(s)
		if !uuidRegexp.MatchString(s) {
			return nil, fmt.Errorf("invalid UUID value %q for string_uuid_as_uuid", s)
		}
		return strings.ToLower(s), nil

	case isTinyInt1Column(col) && typeMap.TinyInt1AsBoolean:
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

	case col.DataType == "set" && (typeMap.SetMode == "text_array" || typeMap.SetMode == "text_array_check"):
		var raw string
		switch v := val.(type) {
		case []byte:
			raw = string(v)
		case string:
			raw = v
		default:
			return nil, fmt.Errorf("cannot coerce set value of type %T to text[]", val)
		}
		raw = strings.ReplaceAll(raw, "\x00", "")
		if raw == "" {
			return []string{}, nil
		}
		parts := strings.Split(raw, ",")
		return parts, nil

	case col.DataType == "bit" && (typeMap.BitMode == "bit" || typeMap.BitMode == "varbit"):
		b, ok := val.([]byte)
		if !ok {
			return nil, fmt.Errorf("expected []byte for BIT value, got %T", val)
		}
		// Determine bit width from column type
		bitWidth, wOk := mysqlColumnTypeLength(col.ColumnType, "bit")
		if !wOk {
			bitWidth = col.Precision
		}
		if bitWidth <= 0 {
			bitWidth = int64(len(b)) * 8
		}
		// Convert bytes to binary string, then truncate to the actual bit width
		var sb strings.Builder
		for _, byt := range b {
			fmt.Fprintf(&sb, "%08b", byt)
		}
		bits := sb.String()
		// MySQL may send more bytes than needed; take the rightmost bitWidth bits
		if int64(len(bits)) > bitWidth {
			bits = bits[len(bits)-int(bitWidth):]
		}
		return bits, nil

	case col.DataType == "year":
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
		}
		return nil, fmt.Errorf("cannot coerce year value of type %T to integer", val)

	case col.DataType == "time":
		var raw string
		switch v := val.(type) {
		case []byte:
			raw = string(v)
		case string:
			raw = v
		default:
			return val, nil
		}
		raw = strings.TrimSpace(raw)
		if typeMap.TimeMode == "interval" {
			return mysqlTimeToInterval(raw)
		}
		// For time and text modes, pass through as-is
		return raw, nil

	case col.DataType == "date":
		t, ok := val.(time.Time)
		if ok && t.IsZero() {
			if typeMap.ZeroDateMode == "error" {
				return nil, fmt.Errorf("zero date value in column %s (zero_date_mode=error)", col.SourceName)
			}
			return nil, nil
		}
		return val, nil

	case col.DataType == "timestamp" || col.DataType == "datetime":
		t, ok := val.(time.Time)
		if ok && t.IsZero() {
			if typeMap.ZeroDateMode == "error" {
				return nil, fmt.Errorf("zero date/time value in column %s (zero_date_mode=error)", col.SourceName)
			}
			return nil, nil
		}
		return val, nil

	case isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkb_bytea":
		// Raw MySQL spatial bytes (4-byte SRID prefix + WKB) pass through as
		// bytea. Explicit case to avoid relying on default passthrough.
		return val, nil

	case isMySQLSpatialType(col.DataType) && typeMap.SpatialMode == "wkt_text":
		// ST_AsText() is used in the SELECT query, so the value arrives as a
		// string (WKT). Just pass it through.
		switch v := val.(type) {
		case []byte:
			return string(v), nil
		case string:
			return v, nil
		default:
			return nil, fmt.Errorf("expected string/[]byte for spatial WKT value, got %T", val)
		}

	case col.DataType == "varchar" || col.DataType == "char" ||
		col.DataType == "text" || col.DataType == "mediumtext" ||
		col.DataType == "longtext" || col.DataType == "tinytext" ||
		col.DataType == "enum" || col.DataType == "set":
		switch v := val.(type) {
		case []byte:
			return strings.ReplaceAll(string(v), "\x00", ""), nil
		case string:
			return strings.ReplaceAll(v, "\x00", ""), nil
		}
		return val, nil

	default:
		return val, nil
	}
}

// --- Default mapping (moved from ddl.go) ---

func mysqlMapDefault(col Column, pgType string, typeMap TypeMappingConfig) (string, error) {
	if col.Default == nil {
		return "", nil
	}

	raw := strings.TrimSpace(*col.Default)
	if strings.EqualFold(raw, "null") {
		return "", nil
	}

	lower := strings.ToLower(raw)
	switch lower {
	case "current_timestamp", "current_timestamp()", "now()", "localtimestamp", "localtimestamp()":
		return "CURRENT_TIMESTAMP", nil
	}

	if strings.HasPrefix(lower, "current_timestamp(") && strings.HasSuffix(lower, ")") {
		return strings.ToUpper(raw), nil
	}

	unquoted := mysqlDefaultUnquote(raw)

	switch {
	case pgType == "boolean":
		switch unquoted {
		case "0":
			return "FALSE", nil
		case "1":
			return "TRUE", nil
		default:
			return "", fmt.Errorf("unsupported boolean default %q", raw)
		}

	case isNumericType(pgType):
		if _, err := strconv.ParseFloat(unquoted, 64); err != nil {
			return "", fmt.Errorf("unsupported numeric default %q", raw)
		}
		return unquoted, nil

	case pgType == "json" || pgType == "jsonb":
		return fmt.Sprintf("%s::%s", pgLiteral(unquoted), pgType), nil

	case pgType == "bytea":
		return "", fmt.Errorf("bytea defaults are not supported (value %q)", raw)

	case strings.HasPrefix(pgType, "bit(") || pgType == "varbit":
		// MySQL BIT defaults are typically binary literals like b'0' or b'101'
		if strings.HasPrefix(unquoted, "b'") && strings.HasSuffix(unquoted, "'") {
			bits := unquoted[2 : len(unquoted)-1]
			return fmt.Sprintf("B'%s'", bits), nil
		}
		return fmt.Sprintf("B'%s'", unquoted), nil

	case pgType == "text[]":
		vals := parseMySQLSetDefault(unquoted)
		if len(vals) == 0 {
			return "ARRAY[]::text[]", nil
		}
		items := make([]string, len(vals))
		for i, v := range vals {
			items[i] = pgLiteral(v)
		}
		return fmt.Sprintf("ARRAY[%s]::text[]", strings.Join(items, ", ")), nil

	case strings.HasPrefix(pgType, "timestamp"), pgType == "date", strings.HasPrefix(pgType, "time"):
		return pgLiteral(unquoted), nil

	case strings.HasPrefix(pgType, "char"), strings.HasPrefix(pgType, "varchar"), pgType == "text", pgType == "uuid":
		if pgType == "uuid" && typeMap.Binary16AsUUID {
			return "", fmt.Errorf("uuid defaults are not supported for binary16_as_uuid (value %q)", raw)
		}
		return pgLiteral(unquoted), nil

	default:
		return pgLiteral(unquoted), nil
	}
}

func mysqlDefaultUnquote(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		inner := v[1 : len(v)-1]
		return strings.ReplaceAll(inner, "''", "'")
	}
	return v
}

// mysqlTimeToInterval converts a MySQL TIME string (e.g. "838:59:59", "-12:30:00")
// to a PostgreSQL interval literal (e.g. "838 hours 59 mins 59 secs").
func mysqlTimeToInterval(t string) (string, error) {
	negative := false
	if strings.HasPrefix(t, "-") {
		negative = true
		t = t[1:]
	}

	parts := strings.Split(t, ":")
	var hours, mins, secs string
	switch len(parts) {
	case 3:
		hours, mins, secs = parts[0], parts[1], parts[2]
	case 2:
		hours, mins = parts[0], parts[1]
		secs = "0"
	default:
		return "", fmt.Errorf("cannot parse MySQL TIME value %q as interval", t)
	}

	var b strings.Builder
	if negative {
		// PostgreSQL interval parsing applies the sign only to the immediately
		// following component, so we must negate each part individually.
		// Skip the minus for zero components to avoid cosmetic "-00" output.
		negateComponent := func(s string) string {
			for _, c := range s {
				if c != '0' && c != '.' {
					return "-" + s
				}
			}
			return s
		}
		fmt.Fprintf(&b, "%s hours %s mins %s secs", negateComponent(hours), negateComponent(mins), negateComponent(secs))
	} else {
		fmt.Fprintf(&b, "%s hours %s mins %s secs", hours, mins, secs)
	}
	return b.String(), nil
}
