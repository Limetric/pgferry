package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
)

type mssqlSourceDB struct {
	snakeCaseIDs bool
	sourceSchema string // MSSQL schema (default "dbo")
}

func (m *mssqlSourceDB) Name() string                         { return "MSSQL" }
func (m *mssqlSourceDB) SetSnakeCaseIdentifiers(enabled bool) { m.snakeCaseIDs = enabled }
func (m *mssqlSourceDB) SetCharset(_ string)                  {}
func (m *mssqlSourceDB) SetSourceSchema(schema string) {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		schema = "dbo"
	}
	m.sourceSchema = schema
}
func (m *mssqlSourceDB) SupportsSnapshotMode() bool { return true }
func (m *mssqlSourceDB) MaxWorkers() int            { return 0 }

// identName converts a source identifier to its PostgreSQL name.
func (m *mssqlSourceDB) identName(s string) string {
	if m.snakeCaseIDs {
		return toSnakeCase(s)
	}
	return strings.ToLower(s)
}

func (m *mssqlSourceDB) QuoteIdentifier(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func (m *mssqlSourceDB) SourceTableRef(table Table) string {
	tableRef := m.QuoteIdentifier(table.SourceName)
	if m.sourceSchema == "" {
		return tableRef
	}
	return m.QuoteIdentifier(m.sourceSchema) + "." + tableRef
}

func (m *mssqlSourceDB) OpenDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mssql: %w", err)
	}
	return db, nil
}

func (m *mssqlSourceDB) ExtractDBName(dsn string) (string, error) {
	// Try URL format: sqlserver://user:pass@host:1433?database=mydb
	if strings.HasPrefix(dsn, "sqlserver://") || strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err == nil {
			if db := u.Query().Get("database"); db != "" {
				return db, nil
			}
		}
	}

	// Try ADO format: server=host;user id=user;password=pass;database=mydb
	for _, part := range strings.Split(dsn, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && strings.EqualFold(strings.TrimSpace(kv[0]), "database") {
			db := strings.TrimSpace(kv[1])
			if db != "" {
				return db, nil
			}
		}
	}

	return "", fmt.Errorf("cannot extract database name from MSSQL DSN: no 'database' parameter found")
}

func (m *mssqlSourceDB) ValidateTypeMapping(typeMap TypeMappingConfig) error {
	var errs []string

	// MySQL-only options
	if typeMap.TinyInt1AsBoolean {
		errs = append(errs, "tinyint1_as_boolean is a MySQL-only option")
	}
	if typeMap.Binary16AsUUID {
		errs = append(errs, "binary16_as_uuid is a MySQL-only option")
	}
	if typeMap.VarcharAsText {
		errs = append(errs, "varchar_as_text is a MySQL-only option")
	}
	if !typeMap.WidenUnsignedIntegers {
		errs = append(errs, "widen_unsigned_integers is a MySQL-only option")
	}
	if effectiveTypeMappingForSource(typeMap, "mssql").EnumMode != "text" {
		errs = append(errs, fmt.Sprintf("enum_mode=%q is a MySQL-only option", typeMap.EnumMode))
	}
	if typeMap.SetMode != "text" {
		errs = append(errs, fmt.Sprintf("set_mode=%q is a MySQL-only option", typeMap.SetMode))
	}
	if typeMap.CollationMode != "none" {
		errs = append(errs, fmt.Sprintf("collation_mode=%q is a MySQL-only option", typeMap.CollationMode))
	}
	if len(typeMap.CollationMap) > 0 {
		errs = append(errs, "collation_map is a MySQL-only option")
	}
	if typeMap.CIAsCitext {
		errs = append(errs, "ci_as_citext is a MySQL-only option")
	}
	if typeMap.BitMode != "bytea" {
		errs = append(errs, fmt.Sprintf("bit_mode=%q is a MySQL-only option", typeMap.BitMode))
	}
	if typeMap.StringUUIDAsUUID {
		errs = append(errs, "string_uuid_as_uuid is a MySQL-only option")
	}
	if typeMap.Binary16UUIDMode != "rfc4122" {
		errs = append(errs, fmt.Sprintf("binary16_uuid_mode=%q is a MySQL-only option", typeMap.Binary16UUIDMode))
	}
	if typeMap.TimeMode != "time" {
		errs = append(errs, fmt.Sprintf("time_mode=%q is a MySQL-only option", typeMap.TimeMode))
	}
	if typeMap.ZeroDateMode != "null" {
		errs = append(errs, fmt.Sprintf("zero_date_mode=%q is a MySQL-only option", typeMap.ZeroDateMode))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid type_mapping for MSSQL source: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- Schema introspection ---

func (m *mssqlSourceDB) IntrospectSchema(db *sql.DB, _ string) (*Schema, error) {
	tables, err := introspectMSSQLTables(db, m.sourceSchema, m.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect tables: %w", err)
	}

	columnsByTable, err := introspectMSSQLColumnsByTable(db, m.sourceSchema, m.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect columns for schema %s: %w", m.sourceSchema, err)
	}

	indexesByTable, err := introspectMSSQLIndexesByTable(db, m.sourceSchema, m.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect indexes for schema %s: %w", m.sourceSchema, err)
	}

	foreignKeysByTable, err := introspectMSSQLForeignKeysByTable(db, m.sourceSchema, m.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect foreign keys for schema %s: %w", m.sourceSchema, err)
	}

	for i := range tables {
		t := &tables[i]
		t.Columns = columnsByTable[t.SourceName]
		for _, idx := range indexesByTable[t.SourceName] {
			if idx.IsPrimary {
				pk := idx
				t.PrimaryKey = &pk
			} else {
				t.Indexes = append(t.Indexes, idx)
			}
		}
		t.ForeignKeys = foreignKeysByTable[t.SourceName]
	}

	return &Schema{Tables: tables}, nil
}

func introspectMSSQLTables(db *sql.DB, schema string, identName func(string) string) ([]Table, error) {
	rows, err := db.Query(`
		SELECT t.name
		FROM sys.tables t
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE s.name = @p1
		  AND t.is_ms_shipped = 0
		ORDER BY t.name`,
		schema,
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

func introspectMSSQLColumnsByTable(db *sql.DB, schema string, identName func(string) string) (map[string][]Column, error) {
	rows, err := db.Query(`
		SELECT
			t.name,
			c.name,
			LOWER(COALESCE(st.name, ut.name)) AS base_type,
			c.max_length,
			c.precision,
			c.scale,
			c.is_nullable,
			dc.definition AS default_def,
			c.is_identity,
			CASE WHEN cc.column_id IS NOT NULL THEN 1 ELSE 0 END AS is_computed,
			COALESCE(cc.definition, '') AS computed_def,
			c.column_id
		FROM sys.columns c
		JOIN sys.tables t ON c.object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		JOIN sys.types ut ON c.user_type_id = ut.user_type_id
		LEFT JOIN sys.types st ON ut.system_type_id = st.user_type_id
			AND st.system_type_id = st.user_type_id
		LEFT JOIN sys.default_constraints dc ON c.default_object_id = dc.object_id
		LEFT JOIN sys.computed_columns cc ON c.object_id = cc.object_id
			AND c.column_id = cc.column_id
		WHERE s.name = @p1
		  AND c.is_hidden = 0
		ORDER BY t.name, c.column_id`,
		schema,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	colsByTable := make(map[string][]Column)
	for rows.Next() {
		var (
			tableName   string
			name        string
			baseType    string
			maxLength   int
			precision   int
			scale       int
			isNullable  bool
			defaultDef  sql.NullString
			isIdentity  bool
			isComputed  int
			computedDef string
			columnID    int
		)
		if err := rows.Scan(
			&tableName, &name, &baseType, &maxLength, &precision, &scale,
			&isNullable, &defaultDef, &isIdentity, &isComputed,
			&computedDef, &columnID,
		); err != nil {
			return nil, err
		}

		col := Column{
			SourceName: name,
			PGName:     identName(name),
			DataType:   baseType,
			ColumnType: baseType,
			Precision:  int64(precision),
			Scale:      int64(scale),
			Nullable:   isNullable,
			OrdinalPos: columnID,
		}

		// Handle max_length for character/binary types
		// nvarchar/nchar max_length is in bytes (÷2 for char count)
		// -1 means (max) type
		switch baseType {
		case "nvarchar", "nchar", "ntext":
			if maxLength == -1 {
				col.CharMaxLen = -1
			} else if maxLength > 0 {
				col.CharMaxLen = int64(maxLength / 2)
			}
		case "varchar", "char", "binary", "varbinary":
			col.CharMaxLen = int64(maxLength)
		}

		// Default expression — strip outer parens
		if defaultDef.Valid {
			d := mssqlStripParens(defaultDef.String)
			col.Default = &d
		}

		// IDENTITY → auto_increment (reuse convention)
		if isIdentity {
			col.Extra = "auto_increment"
		}

		// Computed columns
		if isComputed != 0 {
			col.Extra = "COMPUTED"
			col.GenerationExpression = computedDef
		}

		colsByTable[tableName] = append(colsByTable[tableName], col)
	}
	return colsByTable, rows.Err()
}

type mssqlIndexesForTable struct {
	indexMap map[string]*Index
	order    []string
}

func introspectMSSQLIndexesByTable(db *sql.DB, schema string, identName func(string) string) (map[string][]Index, error) {
	rows, err := db.Query(`
		SELECT
			t.name AS table_name,
			i.name AS index_name,
			i.is_unique,
			i.is_primary_key,
			i.type_desc,
			i.has_filter,
			ic.key_ordinal,
			c.name AS column_name,
			ic.is_descending_key,
			ic.is_included_column
		FROM sys.indexes i
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		JOIN sys.tables t ON i.object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE s.name = @p1
		  AND i.type > 0
		  AND i.name IS NOT NULL
		ORDER BY t.name, i.index_id, ic.is_included_column, ic.key_ordinal`,
		schema,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make(map[string]*mssqlIndexesForTable)

	for rows.Next() {
		var (
			tableName     string
			idxName       string
			isUnique      bool
			isPrimary     bool
			typeDesc      string
			hasFilter     bool
			keyOrdinal    int
			colName       string
			isDescending  bool
			isIncludedCol bool
		)
		if err := rows.Scan(
			&tableName, &idxName, &isUnique, &isPrimary, &typeDesc,
			&hasFilter, &keyOrdinal, &colName, &isDescending,
			&isIncludedCol,
		); err != nil {
			return nil, err
		}

		group := groups[tableName]
		if group == nil {
			group = &mssqlIndexesForTable{indexMap: make(map[string]*Index)}
			groups[tableName] = group
		}

		idx, ok := group.indexMap[idxName]
		if !ok {
			idx = &Index{
				Name:       identName(idxName),
				SourceName: idxName,
				Unique:     isUnique,
				IsPrimary:  isPrimary,
				Type:       "BTREE",
			}
			group.indexMap[idxName] = idx
			group.order = append(group.order, idxName)

			// XML, SPATIAL, and FULLTEXT indexes → skip
			switch typeDesc {
			case "XML", "SPATIAL":
				idx.HasExpression = true
				log.Printf("    WARN: %s index %q on %s will be skipped (not supported in PostgreSQL)", typeDesc, idxName, tableName)
			}

			// Filtered indexes → skip
			if hasFilter {
				idx.HasExpression = true
				log.Printf("    WARN: filtered index %q on %s will be skipped (WHERE clause not migrated)", idxName, tableName)
			}
		}

		// Skip included columns (not key columns)
		if isIncludedCol {
			continue
		}

		idx.Columns = append(idx.Columns, identName(colName))
		if isDescending {
			idx.ColumnOrders = append(idx.ColumnOrders, "DESC")
		} else {
			idx.ColumnOrders = append(idx.ColumnOrders, "ASC")
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	indexesByTable := make(map[string][]Index, len(groups))
	for tableName, group := range groups {
		indexes := make([]Index, 0, len(group.order))
		for _, name := range group.order {
			indexes = append(indexes, *group.indexMap[name])
		}
		indexesByTable[tableName] = indexes
	}
	return indexesByTable, nil
}

type mssqlForeignKeysForTable struct {
	fkMap map[string]*ForeignKey
	order []string
}

func introspectMSSQLForeignKeysByTable(db *sql.DB, schema string, identName func(string) string) (map[string][]ForeignKey, error) {
	rows, err := db.Query(`
		SELECT
			t.name AS table_name,
			fk.name AS fk_name,
			COL_NAME(fkc.parent_object_id, fkc.parent_column_id) AS column_name,
			OBJECT_NAME(fkc.referenced_object_id) AS ref_table,
			COL_NAME(fkc.referenced_object_id, fkc.referenced_column_id) AS ref_column,
			fk.update_referential_action_desc,
			fk.delete_referential_action_desc,
			SCHEMA_NAME(ref_t.schema_id) AS ref_schema
		FROM sys.foreign_keys fk
		JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id
		JOIN sys.tables t ON fk.parent_object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		JOIN sys.tables ref_t ON fk.referenced_object_id = ref_t.object_id
		WHERE s.name = @p1
		ORDER BY t.name, fk.name, fkc.constraint_column_id`,
		schema,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make(map[string]*mssqlForeignKeysForTable)

	for rows.Next() {
		var tableName, fkName, colName, refTable, refCol, updateAction, deleteAction, refSchema string
		if err := rows.Scan(&tableName, &fkName, &colName, &refTable, &refCol, &updateAction, &deleteAction, &refSchema); err != nil {
			return nil, err
		}

		group := groups[tableName]
		if group == nil {
			group = &mssqlForeignKeysForTable{fkMap: make(map[string]*ForeignKey)}
			groups[tableName] = group
		}

		fk, ok := group.fkMap[fkName]
		if !ok {
			refPGTable := identName(refTable)
			// If the referenced table is in a different schema, log a warning.
			// pgferry migrates a single schema at a time, so cross-schema FKs
			// may fail if the referenced table isn't in the target schema.
			if refSchema != schema {
				log.Printf("WARN: FK %s references table %s.%s in a different schema; the FK may fail if that table is not in the target PostgreSQL schema", fkName, refSchema, refTable)
			}
			fk = &ForeignKey{
				Name:       identName(fkName),
				RefTable:   refTable,
				RefPGTable: refPGTable,
				UpdateRule: strings.ReplaceAll(updateAction, "_", " "),
				DeleteRule: strings.ReplaceAll(deleteAction, "_", " "),
			}
			group.fkMap[fkName] = fk
			group.order = append(group.order, fkName)
		}
		fk.Columns = append(fk.Columns, identName(colName))
		fk.RefColumns = append(fk.RefColumns, identName(refCol))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	fksByTable := make(map[string][]ForeignKey, len(groups))
	for tableName, group := range groups {
		fks := make([]ForeignKey, 0, len(group.order))
		for _, name := range group.order {
			fks = append(fks, *group.fkMap[name])
		}
		fksByTable[tableName] = fks
	}
	return fksByTable, nil
}

// --- Source objects introspection ---

func (m *mssqlSourceDB) IntrospectSourceObjects(db *sql.DB, _ string) (*SourceObjects, error) {
	objs := &SourceObjects{}

	// Views
	viewRows, err := db.Query(`
		SELECT v.name
		FROM sys.views v
		JOIN sys.schemas s ON v.schema_id = s.schema_id
		WHERE s.name = @p1
		ORDER BY v.name`,
		m.sourceSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("introspect views: %w", err)
	}
	defer viewRows.Close()
	for viewRows.Next() {
		var name string
		if err := viewRows.Scan(&name); err != nil {
			return nil, err
		}
		objs.Views = append(objs.Views, name)
	}
	if err := viewRows.Err(); err != nil {
		return nil, err
	}

	// Procedures and functions
	routineRows, err := db.Query(`
		SELECT o.type_desc, o.name
		FROM sys.objects o
		JOIN sys.schemas s ON o.schema_id = s.schema_id
		WHERE s.name = @p1
		  AND o.type IN ('P', 'FN', 'IF', 'TF')
		ORDER BY o.type, o.name`,
		m.sourceSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("introspect routines: %w", err)
	}
	defer routineRows.Close()
	for routineRows.Next() {
		var typeDesc, name string
		if err := routineRows.Scan(&typeDesc, &name); err != nil {
			return nil, err
		}
		objs.Routines = append(objs.Routines, fmt.Sprintf("%s %s", typeDesc, name))
	}
	if err := routineRows.Err(); err != nil {
		return nil, err
	}

	// Triggers
	triggerRows, err := db.Query(`
		SELECT tr.name
		FROM sys.triggers tr
		JOIN sys.objects o ON tr.parent_id = o.object_id
		JOIN sys.schemas s ON o.schema_id = s.schema_id
		WHERE s.name = @p1
		ORDER BY tr.name`,
		m.sourceSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("introspect triggers: %w", err)
	}
	defer triggerRows.Close()
	for triggerRows.Next() {
		var name string
		if err := triggerRows.Scan(&name); err != nil {
			return nil, err
		}
		objs.Triggers = append(objs.Triggers, name)
	}
	if err := triggerRows.Err(); err != nil {
		return nil, err
	}

	return objs, nil
}

// --- Type mapping ---

// isMSSQLSpatialType returns true for MSSQL spatial types.
func isMSSQLSpatialType(dataType string) bool {
	return dataType == "geography" || dataType == "geometry"
}

func (m *mssqlSourceDB) MapType(col Column, typeMap TypeMappingConfig) (string, error) {
	return mssqlMapType(col, typeMap)
}

func mssqlMapType(col Column, typeMap TypeMappingConfig) (string, error) {
	switch col.DataType {
	case "int":
		return "integer", nil
	case "bigint":
		return "bigint", nil
	case "smallint":
		return "smallint", nil
	case "tinyint":
		return "smallint", nil
	case "bit":
		return "boolean", nil
	case "decimal", "numeric":
		if col.Precision > 0 {
			return fmt.Sprintf("numeric(%d,%d)", col.Precision, col.Scale), nil
		}
		return "numeric", nil
	case "float":
		return "double precision", nil
	case "real":
		return "real", nil
	case "money":
		if typeMap.MoneyAsNumeric {
			return "numeric(19,4)", nil
		}
		return "text", nil
	case "smallmoney":
		if typeMap.MoneyAsNumeric {
			return "numeric(10,4)", nil
		}
		return "text", nil

	// Character types
	case "char":
		if col.CharMaxLen > 0 {
			return fmt.Sprintf("char(%d)", col.CharMaxLen), nil
		}
		return "char(1)", nil
	case "varchar":
		if col.CharMaxLen == -1 {
			return "text", nil
		}
		if col.CharMaxLen > 0 {
			return fmt.Sprintf("varchar(%d)", col.CharMaxLen), nil
		}
		return "varchar(1)", nil
	case "nchar":
		if typeMap.NvarcharAsText {
			return "text", nil
		}
		if col.CharMaxLen > 0 {
			return fmt.Sprintf("char(%d)", col.CharMaxLen), nil
		}
		return "char(1)", nil
	case "nvarchar":
		if col.CharMaxLen == -1 || typeMap.NvarcharAsText {
			return "text", nil
		}
		if col.CharMaxLen > 0 {
			return fmt.Sprintf("varchar(%d)", col.CharMaxLen), nil
		}
		return "varchar(1)", nil
	case "text", "ntext":
		return "text", nil

	// Binary types
	case "binary", "varbinary", "image":
		return "bytea", nil

	// Date/time types
	case "date":
		return "date", nil
	case "time":
		return "time", nil
	case "datetime", "datetime2":
		if typeMap.DatetimeAsTimestamptz {
			return "timestamptz", nil
		}
		return "timestamp", nil
	case "smalldatetime":
		if typeMap.DatetimeAsTimestamptz {
			return "timestamptz", nil
		}
		return "timestamp", nil
	case "datetimeoffset":
		return "timestamptz", nil

	// MSSQL timestamp is NOT a datetime — it's rowversion (8-byte binary)
	case "timestamp":
		return "bytea", nil

	// Special types
	case "uniqueidentifier":
		return "uuid", nil
	case "xml":
		if typeMap.XmlAsText {
			return "text", nil
		}
		return "xml", nil
	case "sql_variant":
		return "text", nil
	case "hierarchyid":
		return "text", nil
	case "json":
		if typeMap.JSONAsJSONB {
			return "jsonb", nil
		}
		return "json", nil

	// Spatial types
	case "geography", "geometry":
		switch typeMap.SpatialMode {
		case "wkb_bytea":
			return "bytea", nil
		case "wkt_text":
			return "text", nil
		default:
			if typeMap.UnknownAsText {
				return "text", nil
			}
			return "", fmt.Errorf("unsupported MSSQL type %q (set spatial_mode to wkt_text or wkb_bytea)", col.DataType)
		}

	default:
		if typeMap.UnknownAsText {
			return "text", nil
		}
		return "", fmt.Errorf("unsupported MSSQL type %q", col.DataType)
	}
}

// --- Default mapping ---

func (m *mssqlSourceDB) MapDefault(col Column, pgType string, typeMap TypeMappingConfig) (string, error) {
	return mssqlMapDefault(col, pgType, typeMap)
}

func mssqlMapDefault(col Column, pgType string, _ TypeMappingConfig) (string, error) {
	if col.Default == nil {
		return "", nil
	}

	raw := strings.TrimSpace(*col.Default)
	if raw == "" {
		return "", nil
	}

	// Already stripped in introspection, but be safe
	raw = mssqlStripParens(raw)

	if strings.EqualFold(raw, "null") {
		return "", nil
	}

	lower := strings.ToLower(raw)

	// Function mapping
	switch lower {
	case "getdate()", "sysdatetime()", "sysutcdatetime()", "sysdatetimeoffset()", "getutcdate()":
		return "CURRENT_TIMESTAMP", nil
	case "newid()", "newsequentialid()":
		return "gen_random_uuid()", nil
	case "suser_sname()", "user_name()":
		return "CURRENT_USER", nil
	}

	// Boolean defaults for bit columns
	if pgType == "boolean" {
		switch raw {
		case "0":
			return "FALSE", nil
		case "1":
			return "TRUE", nil
		}
	}

	// Strip N prefix from Unicode string literals: N'text' → 'text'
	if strings.HasPrefix(raw, "N'") || strings.HasPrefix(raw, "n'") {
		raw = raw[1:]
	}

	// Numeric defaults
	if isNumericType(pgType) {
		cleaned := raw
		// Remove trailing type markers like (e.g. "0.0" is fine)
		if _, err := strconv.ParseFloat(cleaned, 64); err == nil {
			return cleaned, nil
		}
		return "", nil
	}

	// String/quoted defaults
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		inner := raw[1 : len(raw)-1]
		inner = strings.ReplaceAll(inner, "''", "'")
		return pgLiteral(inner), nil
	}

	// JSON/bytea defaults are not supported
	if pgType == "bytea" || pgType == "json" || pgType == "jsonb" {
		return "", nil
	}

	// Timestamp/date defaults that are plain values
	if strings.HasPrefix(pgType, "timestamp") || pgType == "date" || strings.HasPrefix(pgType, "time") {
		return pgLiteral(raw), nil
	}

	// For everything else, try to pass through as a literal
	return pgLiteral(raw), nil
}

// mssqlStripParens removes balanced outer parentheses from MSSQL default expressions.
// MSSQL's sys.default_constraints stores defaults wrapped in extra parens by the engine:
// ((0)), (getdate()), (N'hello'). This function strips matched outer pairs only,
// so compound expressions like ((1)+(2)) correctly reduce to (1)+(2), not 1)+(2.
func mssqlStripParens(s string) string {
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' {
		// Verify the opening paren at position 0 is balanced with the closing
		// paren at the final position (not with an inner close).
		depth := 0
		outerMatched := true
		for i := 0; i < len(s); i++ {
			if s[i] == '(' {
				depth++
			} else if s[i] == ')' {
				depth--
			}
			if depth == 0 && i < len(s)-1 {
				outerMatched = false
				break
			}
		}
		if !outerMatched {
			break
		}
		s = s[1 : len(s)-1]
	}
	return s
}

// --- Value transformation ---

func (m *mssqlSourceDB) TransformValue(val any, col Column, typeMap TypeMappingConfig) (any, error) {
	return mssqlTransformValue(val, col, typeMap)
}

func mssqlTransformValue(val any, col Column, _ TypeMappingConfig) (any, error) {
	if val == nil {
		return nil, nil
	}

	switch col.DataType {
	case "uniqueidentifier":
		switch v := val.(type) {
		case []byte:
			if len(v) == 16 {
				// SQL Server stores UUIDs in mixed-endian format:
				// bytes 0-3: data1 (little-endian uint32)
				// bytes 4-5: data2 (little-endian uint16)
				// bytes 6-7: data3 (little-endian uint16)
				// bytes 8-15: data4+data5 (big-endian)
				d1 := binary.LittleEndian.Uint32(v[0:4])
				d2 := binary.LittleEndian.Uint16(v[4:6])
				d3 := binary.LittleEndian.Uint16(v[6:8])
				return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
					d1, d2, d3,
					v[8], v[9],
					v[10], v[11], v[12], v[13], v[14], v[15],
				), nil
			}
			return string(v), nil
		case string:
			return strings.ToLower(v), nil
		}
		return val, nil

	case "money", "smallmoney":
		// go-mssqldb may return float64; format with fixed 4-decimal precision
		// to avoid floating-point representation issues in the COPY text stream
		switch v := val.(type) {
		case []byte:
			return string(v), nil
		case string:
			return v, nil
		case float64:
			return strconv.FormatFloat(v, 'f', 4, 64), nil
		}
		return val, nil

	case "bit":
		// go-mssqldb returns bool natively
		return val, nil

	case "varchar", "nvarchar", "char", "nchar", "text", "ntext", "xml":
		// Strip null bytes
		switch v := val.(type) {
		case []byte:
			return strings.ReplaceAll(string(v), "\x00", ""), nil
		case string:
			return strings.ReplaceAll(v, "\x00", ""), nil
		}
		return val, nil

	case "json":
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
