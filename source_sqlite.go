package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"slices"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// SQLite's default SQLITE_LIMIT_COMPOUND_SELECT is 500; stay well below it.
const sqliteMaxCompoundSelectTerms = 400

type sqliteSourceDB struct {
	snakeCaseIDs bool
}

func (s *sqliteSourceDB) SetSnakeCaseIdentifiers(enabled bool) { s.snakeCaseIDs = enabled }
func (s *sqliteSourceDB) SetCharset(_ string)                  {}
func (s *sqliteSourceDB) SetSourceSchema(_ string)             {}

// identName converts a source identifier to its PostgreSQL name.
// When snakeCaseIDs is true, applies toSnakeCase; otherwise lowercases.
func (s *sqliteSourceDB) identName(name string) string {
	if s.snakeCaseIDs {
		return toSnakeCase(name)
	}
	return strings.ToLower(name)
}

func (s *sqliteSourceDB) Name() string { return "SQLite" }

func (s *sqliteSourceDB) OpenDB(dsn string) (*sql.DB, error) {
	uri, err := sqliteReadOnlyURI(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", uri)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func (s *sqliteSourceDB) ExtractDBName(dsn string) (string, error) {
	path := dsn
	// Strip file: URI prefix
	if strings.HasPrefix(dsn, "file:") {
		u, err := url.Parse(dsn)
		if err == nil {
			path = u.Path
			if path == "" {
				path = u.Opaque
			}
		} else {
			path = strings.TrimPrefix(dsn, "file:")
			if idx := strings.IndexByte(path, '?'); idx >= 0 {
				path = path[:idx]
			}
		}
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		base = base[:len(base)-len(ext)]
	}
	if base == "" {
		return "sqlite", nil
	}
	return base, nil
}

func (s *sqliteSourceDB) SourceTableRef(table Table) string {
	return s.QuoteIdentifier(table.SourceName)
}

func (s *sqliteSourceDB) IntrospectSchema(db *sql.DB, _ string) (*Schema, error) {
	tables, err := introspectSQLiteTables(db, s.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect tables: %w", err)
	}

	tableNames := sqliteTableNames(tables)
	columnsByTable, primaryKeysByTable, err := introspectSQLiteColumnsByTable(db, tableNames, s.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect columns across %d tables: %w", len(tableNames), err)
	}

	indexesByTable, err := introspectSQLiteIndexesByTable(db, tableNames, s.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect indexes across %d tables: %w", len(tableNames), err)
	}

	foreignKeysByTable, err := introspectSQLiteForeignKeysByTable(db, tableNames, s.identName)
	if err != nil {
		return nil, fmt.Errorf("introspect foreign keys across %d tables: %w", len(tableNames), err)
	}

	for i := range tables {
		t := &tables[i]
		t.Columns = columnsByTable[t.SourceName]
		t.PrimaryKey = primaryKeysByTable[t.SourceName]
		t.Indexes = indexesByTable[t.SourceName]
		t.ForeignKeys = foreignKeysByTable[t.SourceName]
	}

	return &Schema{Tables: tables}, nil
}

func (s *sqliteSourceDB) IntrospectSourceObjects(db *sql.DB, _ string) (*SourceObjects, error) {
	objs := &SourceObjects{}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='view' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("introspect views: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		objs.Views = append(objs.Views, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows2, err := db.Query("SELECT name FROM sqlite_master WHERE type='trigger' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("introspect triggers: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var name string
		if err := rows2.Scan(&name); err != nil {
			return nil, err
		}
		objs.Triggers = append(objs.Triggers, name)
	}
	if err := rows2.Err(); err != nil {
		return nil, err
	}

	return objs, nil
}

func (s *sqliteSourceDB) MapType(col Column, typeMap TypeMappingConfig) (string, error) {
	return sqliteMapType(col, typeMap)
}

func (s *sqliteSourceDB) MapDefault(col Column, pgType string, _ TypeMappingConfig) (string, error) {
	return sqliteMapDefault(col, pgType)
}

func (s *sqliteSourceDB) TransformValue(val any, _ Column, _ TypeMappingConfig) (any, error) {
	if val == nil {
		return nil, nil
	}
	return val, nil
}

func (s *sqliteSourceDB) QuoteIdentifier(name string) string {
	return fmt.Sprintf("\"%s\"", strings.ReplaceAll(name, "\"", "\"\""))
}

func (s *sqliteSourceDB) SupportsSnapshotMode() bool { return false }
func (s *sqliteSourceDB) MaxWorkers() int            { return 1 }

func (s *sqliteSourceDB) ValidateTypeMapping(typeMap TypeMappingConfig) error {
	var errs []string
	if typeMap.TinyInt1AsBoolean {
		errs = append(errs, "tinyint1_as_boolean is a MySQL-only option")
	}
	if typeMap.Binary16AsUUID {
		errs = append(errs, "binary16_as_uuid is a MySQL-only option")
	}
	if typeMap.DatetimeAsTimestamptz {
		errs = append(errs, "datetime_as_timestamptz is a MySQL-only option")
	}
	if typeMap.VarcharAsText {
		errs = append(errs, "varchar_as_text is a MySQL-only option")
	}
	if !typeMap.WidenUnsignedIntegers {
		errs = append(errs, "widen_unsigned_integers is a MySQL-only option")
	}
	if typeMap.EnumMode != "text" {
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
	if typeMap.SpatialMode != "off" {
		errs = append(errs, fmt.Sprintf("spatial_mode=%q is a MySQL/MSSQL-only option", typeMap.SpatialMode))
	}
	if typeMap.CIAsCitext {
		errs = append(errs, "ci_as_citext is a MySQL-only option")
	}
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
		return fmt.Errorf("invalid type_mapping for SQLite source: %s", strings.Join(errs, "; "))
	}
	return nil
}

// --- DSN handling ---

func sqliteReadOnlyURI(dsn string) (string, error) {
	// Reject in-memory databases
	if dsn == ":memory:" || dsn == "file::memory:" ||
		strings.Contains(dsn, "mode=memory") {
		return "", fmt.Errorf("in-memory SQLite databases are not supported (each sql.Open gets a separate DB)")
	}

	if !strings.HasPrefix(dsn, "file:") {
		// Plain file path → file URI with read-only mode
		return "file:" + dsn + "?mode=ro", nil
	}

	// URI form — add or override mode=ro
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse sqlite URI: %w", err)
	}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// --- Schema introspection ---

func introspectSQLiteTables(db *sql.DB, identName func(string) string) ([]Table, error) {
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
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

func sqliteTableNames(tables []Table) []string {
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.SourceName)
	}
	return names
}

func sqliteUnionQuery(parts []string, orderBy string) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteString(" UNION ALL ")
		}
		b.WriteString(part)
	}
	if orderBy != "" {
		b.WriteString(" ORDER BY ")
		b.WriteString(orderBy)
	}
	return b.String()
}

// sqliteLiteral reuses standard SQL single-quoted string escaping, which is
// valid for SQLite table-valued PRAGMA arguments.
func sqliteLiteral(v string) string {
	return pgLiteral(v)
}

func chunkSlice[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	if size <= 0 {
		size = len(items)
	}
	chunks := make([][]T, 0, (len(items)+size-1)/size)
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[start:end])
	}
	return chunks
}

type sqlitePKCol struct {
	name  string
	pkPos int
}

func buildSQLitePKIndex(pkCols []sqlitePKCol, identName func(string) string) *Index {
	if len(pkCols) == 0 {
		return nil
	}

	slices.SortFunc(pkCols, func(a, b sqlitePKCol) int { return a.pkPos - b.pkPos })

	idx := &Index{
		Name:       "PRIMARY",
		SourceName: "PRIMARY",
		Unique:     true,
		IsPrimary:  true,
		Type:       "BTREE",
	}
	for _, pkCol := range pkCols {
		idx.Columns = append(idx.Columns, identName(pkCol.name))
		idx.ColumnOrders = append(idx.ColumnOrders, "ASC")
	}
	return idx
}

func introspectSQLiteColumnsByTable(db *sql.DB, tableNames []string, identName func(string) string) (map[string][]Column, map[string]*Index, error) {
	colsByTable := make(map[string][]Column)
	pksByTable := make(map[string]*Index)
	if len(tableNames) == 0 {
		return colsByTable, pksByTable, nil
	}

	pkColsByTable := make(map[string][]sqlitePKCol)
	for _, batch := range chunkSlice(tableNames, sqliteMaxCompoundSelectTerms) {
		var parts []string
		for _, tableName := range batch {
			parts = append(parts,
				fmt.Sprintf(
					`SELECT %s AS table_name, cid, name, type, "notnull", dflt_value, pk, hidden FROM pragma_table_xinfo(%s)`,
					sqliteLiteral(tableName),
					sqliteLiteral(tableName),
				),
			)
		}

		rows, err := db.Query(sqliteUnionQuery(parts, "table_name, cid"))
		if err != nil {
			return nil, nil, err
		}

		for rows.Next() {
			var (
				tableName string
				cid       int
				pk        int
				notnull   int
				hidden    int
				name      string
				colType   string
				dflt      sql.NullString
			)
			if err := rows.Scan(&tableName, &cid, &name, &colType, &notnull, &dflt, &pk, &hidden); err != nil {
				rows.Close()
				return nil, nil, err
			}

			col := Column{
				SourceName: name,
				PGName:     identName(name),
				DataType:   strings.ToLower(normalizeAffinity(colType)),
				ColumnType: strings.ToLower(colType),
				Nullable:   notnull == 0,
				OrdinalPos: cid + 1,
			}
			if dflt.Valid {
				col.Default = &dflt.String
			}
			switch hidden {
			case 2:
				col.Extra = "STORED GENERATED"
			case 3:
				col.Extra = "VIRTUAL GENERATED"
			}

			parseSQLiteTypeParams(&col, colType)
			colsByTable[tableName] = append(colsByTable[tableName], col)
			if pk > 0 {
				pkColsByTable[tableName] = append(pkColsByTable[tableName], sqlitePKCol{name: name, pkPos: pk})
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, nil, err
		}
		rows.Close()
	}

	autoIncrColsByTable := make(map[string]map[string]bool)
	createSQLRows, err := db.Query("SELECT name, COALESCE(sql, '') FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return nil, nil, err
	}
	defer createSQLRows.Close()
	for createSQLRows.Next() {
		var tableName, createSQL string
		if err := createSQLRows.Scan(&tableName, &createSQL); err != nil {
			return nil, nil, err
		}
		if _, ok := colsByTable[tableName]; !ok {
			continue
		}
		if colName := detectSQLiteAutoIncrementColumnFromSQL(createSQL); colName != "" {
			autoIncrColsByTable[tableName] = map[string]bool{colName: true}
		}
	}
	if err := createSQLRows.Err(); err != nil {
		return nil, nil, err
	}

	for tableName, pkCols := range pkColsByTable {
		pksByTable[tableName] = buildSQLitePKIndex(pkCols, identName)
		if len(pkCols) != 1 {
			continue
		}
		pkColName := pkCols[0].name
		for _, col := range colsByTable[tableName] {
			if col.SourceName == pkColName && strings.EqualFold(col.ColumnType, "integer") {
				if autoIncrColsByTable[tableName] == nil {
					autoIncrColsByTable[tableName] = make(map[string]bool)
				}
				autoIncrColsByTable[tableName][pkColName] = true
				break
			}
		}
	}

	for tableName, cols := range colsByTable {
		autoIncrCols := autoIncrColsByTable[tableName]
		if len(autoIncrCols) == 0 {
			continue
		}
		for i := range cols {
			if autoIncrCols[cols[i].SourceName] {
				cols[i].Extra = "auto_increment"
			}
		}
		colsByTable[tableName] = cols
	}

	return colsByTable, pksByTable, nil
}

type sqliteIndexesForTable struct {
	indexMap map[string]*Index
	order    []string
}

func introspectSQLiteIndexesByTable(db *sql.DB, tableNames []string, identName func(string) string) (map[string][]Index, error) {
	indexesByTable := make(map[string][]Index)
	if len(tableNames) == 0 {
		return indexesByTable, nil
	}

	groups := make(map[string]*sqliteIndexesForTable)
	type sqliteIndexSpec struct {
		tableName string
		indexName string
	}
	var indexSpecs []sqliteIndexSpec

	for _, batch := range chunkSlice(tableNames, sqliteMaxCompoundSelectTerms) {
		var listParts []string
		for _, tableName := range batch {
			listParts = append(listParts,
				fmt.Sprintf(
					`SELECT %s AS table_name, seq, name, "unique", origin, partial FROM pragma_index_list(%s)`,
					sqliteLiteral(tableName),
					sqliteLiteral(tableName),
				),
			)
		}

		rows, err := db.Query(sqliteUnionQuery(listParts, "table_name, seq"))
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var (
				tableName string
				seq       int
				name      string
				origin    string
				unique    int
				partial   int
			)
			if err := rows.Scan(&tableName, &seq, &name, &unique, &origin, &partial); err != nil {
				rows.Close()
				return nil, err
			}
			_ = seq
			if origin == "pk" {
				continue
			}

			group := groups[tableName]
			if group == nil {
				group = &sqliteIndexesForTable{indexMap: make(map[string]*Index)}
				groups[tableName] = group
			}

			idx := &Index{
				Name:       identName(name),
				SourceName: name,
				Unique:     unique == 1,
				IsPrimary:  false,
				Type:       "BTREE",
			}
			if partial == 1 {
				idx.HasExpression = true
				log.Printf("    WARN: partial index %q on %s will be skipped (WHERE clause not migrated)", name, tableName)
			}

			group.indexMap[name] = idx
			group.order = append(group.order, name)
			indexSpecs = append(indexSpecs, sqliteIndexSpec{tableName: tableName, indexName: name})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	for _, batch := range chunkSlice(indexSpecs, sqliteMaxCompoundSelectTerms) {
		var infoParts []string
		for _, spec := range batch {
			infoParts = append(infoParts,
				fmt.Sprintf(
					"SELECT %s AS table_name, %s AS index_name, seqno, cid, name FROM pragma_index_info(%s)",
					sqliteLiteral(spec.tableName),
					sqliteLiteral(spec.indexName),
					sqliteLiteral(spec.indexName),
				),
			)
		}

		infoRows, err := db.Query(sqliteUnionQuery(infoParts, "table_name, index_name, seqno"))
		if err != nil {
			return nil, err
		}

		for infoRows.Next() {
			var (
				tableName string
				indexName string
				seqno     int
				cid       int
				colName   sql.NullString
			)
			if err := infoRows.Scan(&tableName, &indexName, &seqno, &cid, &colName); err != nil {
				infoRows.Close()
				return nil, err
			}
			_ = seqno
			_ = cid
			idx := groups[tableName].indexMap[indexName]
			if !colName.Valid {
				idx.HasExpression = true
				continue
			}
			idx.Columns = append(idx.Columns, identName(colName.String))
			idx.ColumnOrders = append(idx.ColumnOrders, "ASC")
		}
		if err := infoRows.Err(); err != nil {
			infoRows.Close()
			return nil, err
		}
		infoRows.Close()
	}

	for tableName, group := range groups {
		indexes := make([]Index, 0, len(group.order))
		for _, name := range group.order {
			indexes = append(indexes, *group.indexMap[name])
		}
		indexesByTable[tableName] = indexes
	}
	return indexesByTable, nil
}

type sqliteForeignKeysForTable struct {
	fkMap map[int]*ForeignKey
	order []int
}

func introspectSQLiteForeignKeysByTable(db *sql.DB, tableNames []string, identName func(string) string) (map[string][]ForeignKey, error) {
	fksByTable := make(map[string][]ForeignKey)
	if len(tableNames) == 0 {
		return fksByTable, nil
	}

	groups := make(map[string]*sqliteForeignKeysForTable)
	for _, batch := range chunkSlice(tableNames, sqliteMaxCompoundSelectTerms) {
		var parts []string
		for _, tableName := range batch {
			parts = append(parts,
				fmt.Sprintf(
					`SELECT %s AS table_name, id, seq, "table" AS ref_table, "from", "to", on_update, on_delete, match FROM pragma_foreign_key_list(%s)`,
					sqliteLiteral(tableName),
					sqliteLiteral(tableName),
				),
			)
		}

		rows, err := db.Query(sqliteUnionQuery(parts, "table_name, id, seq"))
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var (
				tableName string
				id        int
				seq       int
				refTable  string
				from      string
				to        string
				onUpdate  string
				onDelete  string
				match     string
			)
			if err := rows.Scan(&tableName, &id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				rows.Close()
				return nil, err
			}
			_ = seq
			_ = match

			group := groups[tableName]
			if group == nil {
				group = &sqliteForeignKeysForTable{fkMap: make(map[int]*ForeignKey)}
				groups[tableName] = group
			}

			fk := group.fkMap[id]
			if fk == nil {
				fk = &ForeignKey{
					Name:       fmt.Sprintf("fk_%s_%d", identName(tableName), id),
					RefTable:   refTable,
					RefPGTable: identName(refTable),
					UpdateRule: strings.ToUpper(onUpdate),
					DeleteRule: strings.ToUpper(onDelete),
				}
				group.fkMap[id] = fk
				group.order = append(group.order, id)
			}
			fk.Columns = append(fk.Columns, identName(from))
			fk.RefColumns = append(fk.RefColumns, identName(to))
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}

	for tableName, group := range groups {
		fks := make([]ForeignKey, 0, len(group.order))
		for _, id := range group.order {
			fk := *group.fkMap[id]
			if fk.UpdateRule == "NO ACTION" || fk.UpdateRule == "" {
				fk.UpdateRule = "NO ACTION"
			}
			if fk.DeleteRule == "NO ACTION" || fk.DeleteRule == "" {
				fk.DeleteRule = "NO ACTION"
			}
			fks = append(fks, fk)
		}
		fksByTable[tableName] = fks
	}
	return fksByTable, nil
}

// normalizeAffinity extracts the base type name for SQLite's flexible type system.
func normalizeAffinity(declaredType string) string {
	dt := strings.TrimSpace(declaredType)
	if dt == "" {
		return "blob" // no declared type = BLOB affinity
	}

	// Extract base name before '('
	if idx := strings.IndexByte(dt, '('); idx >= 0 {
		dt = dt[:idx]
	}
	return strings.TrimSpace(dt)
}

func parseSQLiteTypeParams(col *Column, declaredType string) {
	open := strings.IndexByte(declaredType, '(')
	close := strings.LastIndexByte(declaredType, ')')
	if open < 0 || close <= open {
		return
	}
	params := declaredType[open+1 : close]

	parts := strings.Split(params, ",")
	if len(parts) >= 1 {
		if n, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &col.Precision); n == 1 && err == nil {
			col.CharMaxLen = col.Precision
		}
	}
	if len(parts) >= 2 {
		fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &col.Scale)
	}
}

func detectSQLiteAutoIncrementColumnFromSQL(createSQL string) string {
	if !strings.Contains(strings.ToUpper(createSQL), "AUTOINCREMENT") {
		return ""
	}

	upper := strings.ToUpper(createSQL)
	idx := strings.Index(upper, "AUTOINCREMENT")
	if idx <= 0 {
		return ""
	}

	prefix := strings.TrimRight(createSQL[:idx], " \t\n\r")
	tokens := strings.Fields(prefix)
	for i := len(tokens) - 1; i >= 0; i-- {
		tok := strings.ToUpper(tokens[i])
		if tok == "INTEGER" || tok == "PRIMARY" || tok == "KEY" {
			continue
		}
		return strings.Trim(tokens[i], ",(\n\r\t ")
	}
	return ""
}

// --- Type mapping ---

func sqliteMapType(col Column, typeMap TypeMappingConfig) (string, error) {
	dt := strings.ToUpper(normalizeAffinity(col.ColumnType))

	switch dt {
	case "INTEGER", "INT", "SMALLINT", "TINYINT", "MEDIUMINT", "BIGINT":
		return "bigint", nil
	case "REAL", "DOUBLE", "FLOAT":
		return "double precision", nil
	case "TEXT", "VARCHAR", "CHAR", "CLOB":
		return "text", nil
	case "BLOB":
		return "bytea", nil
	case "NUMERIC", "DECIMAL":
		if col.Precision > 0 {
			if col.Scale > 0 {
				return fmt.Sprintf("numeric(%d,%d)", col.Precision, col.Scale), nil
			}
			return fmt.Sprintf("numeric(%d)", col.Precision), nil
		}
		return "numeric", nil
	case "BOOLEAN", "BOOL":
		return "boolean", nil
	case "DATETIME", "TIMESTAMP":
		return "timestamp", nil
	case "DATE":
		return "date", nil
	case "TIME":
		return "time", nil
	case "JSON":
		if typeMap.JSONAsJSONB {
			return "jsonb", nil
		}
		return "json", nil
	default:
		if typeMap.UnknownAsText {
			return "text", nil
		}
		return "", fmt.Errorf("unsupported SQLite type %q", col.ColumnType)
	}
}

func sqliteMapDefault(col Column, pgType string) (string, error) {
	if col.Default == nil {
		return "", nil
	}

	raw := strings.TrimSpace(*col.Default)
	upper := strings.ToUpper(raw)

	// NULL
	if strings.EqualFold(raw, "NULL") || strings.EqualFold(raw, "null") {
		return "", nil
	}

	// Special SQL functions and boolean keywords
	switch upper {
	case "CURRENT_TIMESTAMP", "CURRENT_DATE", "CURRENT_TIME":
		return upper, nil
	case "TRUE":
		return "TRUE", nil
	case "FALSE":
		return "FALSE", nil
	}

	// Numeric literals
	if isNumericLiteral(raw) {
		if pgType == "boolean" {
			switch raw {
			case "0":
				return "FALSE", nil
			case "1":
				return "TRUE", nil
			}
		}
		return raw, nil
	}

	// String literals (single-quoted)
	if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		inner := raw[1 : len(raw)-1]
		inner = strings.ReplaceAll(inner, "''", "'")
		return pgLiteral(inner), nil
	}

	// Expression defaults — skip with warning
	log.Printf("    WARN: skipping expression default %q for column %s", raw, col.SourceName)
	return "", nil
}

func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	hasDot := false
	start := 0
	if s[0] == '-' || s[0] == '+' {
		start = 1
	}
	if start >= len(s) {
		return false
	}
	for i := start; i < len(s); i++ {
		if s[i] == '.' {
			if hasDot {
				return false
			}
			hasDot = true
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
