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

type sqliteSourceDB struct{}

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

func (s *sqliteSourceDB) IntrospectSchema(db *sql.DB, _ string) (*Schema, error) {
	tables, err := introspectSQLiteTables(db)
	if err != nil {
		return nil, fmt.Errorf("introspect tables: %w", err)
	}

	for i := range tables {
		t := &tables[i]

		cols, autoIncrCols, err := introspectSQLiteColumns(db, t.SourceName)
		if err != nil {
			return nil, fmt.Errorf("introspect columns for %s: %w", t.SourceName, err)
		}
		t.Columns = cols

		indexes, err := introspectSQLiteIndexes(db, t.SourceName)
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

		// If no PK from indexes, buildPKFromTableInfo already handles it

		// Mark autoincrement columns
		for i := range t.Columns {
			if autoIncrCols[t.Columns[i].SourceName] {
				t.Columns[i].Extra = "auto_increment"
			}
		}

		fks, err := introspectSQLiteForeignKeys(db, t.SourceName)
		if err != nil {
			return nil, fmt.Errorf("introspect foreign keys for %s: %w", t.SourceName, err)
		}
		t.ForeignKeys = fks
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
func (s *sqliteSourceDB) MaxWorkers() int             { return 1 }

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
	if typeMap.EnumMode != "text" {
		errs = append(errs, fmt.Sprintf("enum_mode=%q is a MySQL-only option", typeMap.EnumMode))
	}
	if typeMap.SetMode != "text" {
		errs = append(errs, fmt.Sprintf("set_mode=%q is a MySQL-only option", typeMap.SetMode))
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

func introspectSQLiteTables(db *sql.DB) ([]Table, error) {
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
			PGName:     toSnakeCase(name),
		})
	}
	return tables, rows.Err()
}

func introspectSQLiteColumns(db *sql.DB, tableName string) ([]Column, map[string]bool, error) {
	quotedTable := strings.ReplaceAll(tableName, "\"", "\"\"")
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_xinfo(\"%s\")", quotedTable))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	type colInfo struct {
		col    Column
		pk     int
		hidden int // 0=normal, 1=hidden, 2=generated stored, 3=generated virtual
	}
	var infos []colInfo

	for rows.Next() {
		var cid, pk, notnull, hidden int
		var name, colType string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk, &hidden); err != nil {
			return nil, nil, err
		}

		col := Column{
			SourceName: name,
			PGName:     toSnakeCase(name),
			DataType:   strings.ToLower(normalizeAffinity(colType)),
			ColumnType: strings.ToLower(colType),
			Nullable:   notnull == 0,
			OrdinalPos: cid + 1,
		}
		if dflt.Valid {
			col.Default = &dflt.String
		}

		// Mark generated columns so they get materialized during migration
		switch hidden {
		case 2:
			col.Extra = "STORED GENERATED"
		case 3:
			col.Extra = "VIRTUAL GENERATED"
		}

		parseSQLiteTypeParams(&col, colType)

		infos = append(infos, colInfo{col: col, pk: pk, hidden: hidden})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var cols []Column
	for _, ci := range infos {
		cols = append(cols, ci.col)
	}

	// Detect autoincrement columns from CREATE TABLE SQL
	autoIncrCols := detectSQLiteAutoIncrement(db, tableName)

	// Also mark INTEGER PRIMARY KEY as auto_increment (it's a rowid alias)
	// Use pk info already collected — no need to re-query
	pkCount := 0
	for _, ci := range infos {
		if ci.pk > 0 {
			pkCount++
		}
	}
	if pkCount == 1 {
		for _, ci := range infos {
			if ci.pk > 0 && strings.EqualFold(ci.col.ColumnType, "integer") {
				autoIncrCols[ci.col.SourceName] = true
			}
		}
	}

	return cols, autoIncrCols, nil
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

func detectSQLiteAutoIncrement(db *sql.DB, tableName string) map[string]bool {
	result := make(map[string]bool)
	var createSQL sql.NullString
	err := db.QueryRow(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name=?",
		tableName,
	).Scan(&createSQL)
	if err != nil || !createSQL.Valid {
		return result
	}
	if strings.Contains(strings.ToUpper(createSQL.String), "AUTOINCREMENT") {
		// Find the column name before AUTOINCREMENT
		// Simple heuristic: look for INTEGER PRIMARY KEY AUTOINCREMENT
		upper := strings.ToUpper(createSQL.String)
		idx := strings.Index(upper, "AUTOINCREMENT")
		if idx > 0 {
			// Walk backwards to find column name
			prefix := createSQL.String[:idx]
			// Remove "INTEGER PRIMARY KEY" before AUTOINCREMENT
			prefix = strings.TrimRight(prefix, " \t\n\r")
			tokens := strings.Fields(prefix)
			// Find the column name: it precedes "INTEGER PRIMARY KEY"
			for i := len(tokens) - 1; i >= 0; i-- {
				tok := strings.ToUpper(tokens[i])
				if tok == "INTEGER" || tok == "PRIMARY" || tok == "KEY" {
					continue
				}
				// This is the column name (possibly with trailing comma or paren)
				colName := strings.Trim(tokens[i], ",(\n\r\t ")
				if colName != "" {
					result[colName] = true
				}
				break
			}
		}
	}
	return result
}

func introspectSQLiteIndexes(db *sql.DB, tableName string) ([]Index, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA index_list(\"%s\")", strings.ReplaceAll(tableName, "\"", "\"\"")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []Index
	for rows.Next() {
		var seq int
		var name, origin string
		var unique, partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}

		// Skip auto-generated PK indexes — we handle PKs from table_info
		if origin == "pk" {
			continue
		}

		idx := Index{
			Name:       toSnakeCase(name),
			SourceName: name,
			Unique:     unique == 1,
			IsPrimary:  false,
			Type:       "BTREE",
		}

		if partial == 1 {
			idx.HasExpression = true
			log.Printf("    WARN: partial index %q on %s will be skipped (WHERE clause not migrated)", name, tableName)
		}

		// Get columns for this index
		colRows, err := db.Query(fmt.Sprintf("PRAGMA index_info(\"%s\")", strings.ReplaceAll(name, "\"", "\"\"")))
		if err != nil {
			return nil, err
		}

		for colRows.Next() {
			var seqno, cid int
			var colName sql.NullString
			if err := colRows.Scan(&seqno, &cid, &colName); err != nil {
				colRows.Close()
				return nil, err
			}
			if !colName.Valid {
				// Expression index
				idx.HasExpression = true
				continue
			}
			idx.Columns = append(idx.Columns, toSnakeCase(colName.String))
			idx.ColumnOrders = append(idx.ColumnOrders, "ASC")
		}
		colRows.Close()

		indexes = append(indexes, idx)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Build PK from PRAGMA table_info pk column
	pk, err := buildPKFromTableInfo(db, tableName)
	if err != nil {
		return nil, err
	}
	if pk != nil {
		indexes = append(indexes, *pk)
	}

	return indexes, nil
}

func buildPKFromTableInfo(db *sql.DB, tableName string) (*Index, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(\"%s\")", strings.ReplaceAll(tableName, "\"", "\"\"")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type pkCol struct {
		name  string
		pkPos int
	}
	var pkCols []pkCol

	for rows.Next() {
		var cid, pk int
		var name, colType string
		var notnull int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		if pk > 0 {
			pkCols = append(pkCols, pkCol{name: name, pkPos: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(pkCols) == 0 {
		return nil, nil
	}

	slices.SortFunc(pkCols, func(a, b pkCol) int { return a.pkPos - b.pkPos })

	idx := &Index{
		Name:       "PRIMARY",
		SourceName: "PRIMARY",
		Unique:     true,
		IsPrimary:  true,
		Type:       "BTREE",
	}
	for _, pc := range pkCols {
		idx.Columns = append(idx.Columns, toSnakeCase(pc.name))
		idx.ColumnOrders = append(idx.ColumnOrders, "ASC")
	}
	return idx, nil
}

func introspectSQLiteForeignKeys(db *sql.DB, tableName string) ([]ForeignKey, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA foreign_key_list(\"%s\")", strings.ReplaceAll(tableName, "\"", "\"\"")))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fkMap := make(map[int]*ForeignKey)
	var fkOrder []int

	for rows.Next() {
		var id, seq int
		var refTable, from, to, onUpdate, onDelete, match string
		if err := rows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			return nil, err
		}

		fk, ok := fkMap[id]
		if !ok {
			fk = &ForeignKey{
				Name:       fmt.Sprintf("fk_%s_%d", toSnakeCase(tableName), id),
				RefTable:   refTable,
				RefPGTable: toSnakeCase(refTable),
				UpdateRule: strings.ToUpper(onUpdate),
				DeleteRule: strings.ToUpper(onDelete),
			}
			fkMap[id] = fk
			fkOrder = append(fkOrder, id)
		}
		fk.Columns = append(fk.Columns, toSnakeCase(from))
		fk.RefColumns = append(fk.RefColumns, toSnakeCase(to))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var fks []ForeignKey
	for _, id := range fkOrder {
		fk := *fkMap[id]
		// Normalize rules
		if fk.UpdateRule == "NO ACTION" || fk.UpdateRule == "" {
			fk.UpdateRule = "NO ACTION"
		}
		if fk.DeleteRule == "NO ACTION" || fk.DeleteRule == "" {
			fk.DeleteRule = "NO ACTION"
		}
		fks = append(fks, fk)
	}
	return fks, nil
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
