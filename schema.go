package main

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

// pgReservedWords are PostgreSQL reserved words that must be quoted as identifiers.
var pgReservedWords = map[string]bool{
	"all": true, "analyse": true, "analyze": true, "and": true, "any": true,
	"array": true, "as": true, "asc": true, "authorization": true, "between": true,
	"binary": true, "both": true, "case": true, "cast": true, "check": true,
	"collate": true, "column": true, "constraint": true, "create": true, "cross": true,
	"current_date": true, "current_role": true, "current_time": true,
	"current_timestamp": true, "current_user": true, "default": true, "deferrable": true,
	"desc": true, "distinct": true, "do": true, "else": true, "end": true, "except": true,
	"false": true, "fetch": true, "for": true, "foreign": true, "freeze": true,
	"from": true, "full": true, "grant": true, "group": true, "having": true,
	"ilike": true, "in": true, "initially": true, "inner": true, "intersect": true,
	"into": true, "is": true, "isnull": true, "join": true, "lateral": true,
	"leading": true, "left": true, "like": true, "limit": true, "localtime": true,
	"localtimestamp": true, "natural": true, "not": true, "notnull": true, "null": true,
	"offset": true, "on": true, "only": true, "or": true, "order": true, "outer": true,
	"overlaps": true, "placing": true, "primary": true, "references": true,
	"returning": true, "right": true, "select": true, "session_user": true,
	"similar": true, "some": true, "symmetric": true, "table": true, "then": true,
	"to": true, "trailing": true, "true": true, "union": true, "unique": true,
	"user": true, "using": true, "variadic": true, "verbose": true, "when": true,
	"where": true, "window": true, "with": true,
}

// toSnakeCase converts camelCase to snake_case.
func toSnakeCase(s string) string {
	var result []byte
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, byte(unicode.ToLower(r)))
		} else {
			result = append(result, byte(r))
		}
	}
	return string(result)
}

// pgIdent returns a PG-safe identifier, quoting reserved words.
func pgIdent(name string) string {
	if pgReservedWords[name] {
		return `"` + name + `"`
	}
	return name
}

// introspectSchema reads all tables, columns, indexes, and foreign keys from MySQL.
func introspectSchema(db *sql.DB, dbName string) (*Schema, error) {
	tables, err := introspectTables(db, dbName)
	if err != nil {
		return nil, fmt.Errorf("introspect tables: %w", err)
	}

	for i := range tables {
		t := &tables[i]

		cols, err := introspectColumns(db, dbName, t.MySQLName)
		if err != nil {
			return nil, fmt.Errorf("introspect columns for %s: %w", t.MySQLName, err)
		}
		t.Columns = cols

		indexes, err := introspectIndexes(db, dbName, t.MySQLName)
		if err != nil {
			return nil, fmt.Errorf("introspect indexes for %s: %w", t.MySQLName, err)
		}
		for _, idx := range indexes {
			if idx.IsPrimary {
				pk := idx
				t.PrimaryKey = &pk
			} else {
				t.Indexes = append(t.Indexes, idx)
			}
		}

		fks, err := introspectForeignKeys(db, dbName, t.MySQLName)
		if err != nil {
			return nil, fmt.Errorf("introspect foreign keys for %s: %w", t.MySQLName, err)
		}
		t.ForeignKeys = fks
	}

	return &Schema{Tables: tables}, nil
}

func introspectTables(db *sql.DB, dbName string) ([]Table, error) {
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
			MySQLName: name,
			PGName:    toSnakeCase(name),
		})
	}
	return tables, rows.Err()
}

func introspectColumns(db *sql.DB, dbName, tableName string) ([]Column, error) {
	rows, err := db.Query(
		`SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE,
		        COALESCE(CHARACTER_MAXIMUM_LENGTH, 0),
		        COALESCE(NUMERIC_PRECISION, 0),
		        COALESCE(NUMERIC_SCALE, 0),
		        IS_NULLABLE, COLUMN_DEFAULT, EXTRA, ORDINAL_POSITION
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
			&c.MySQLName, &c.DataType, &c.ColumnType,
			&c.CharMaxLen, &c.Precision, &c.Scale,
			&nullable, &dflt, &c.Extra, &c.OrdinalPos,
		); err != nil {
			return nil, err
		}
		c.PGName = toSnakeCase(c.MySQLName)
		c.Nullable = nullable == "YES"
		if dflt.Valid {
			c.Default = &dflt.String
		}
		// Normalize data type to lowercase
		c.DataType = strings.ToLower(c.DataType)
		c.ColumnType = strings.ToLower(c.ColumnType)
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func isGeneratedColumn(col Column) bool {
	extra := strings.ToLower(col.Extra)
	return strings.Contains(extra, "virtual generated") || strings.Contains(extra, "stored generated")
}

func introspectIndexes(db *sql.DB, dbName, tableName string) ([]Index, error) {
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
				Name:      toSnakeCase(idxName),
				MySQLName: idxName,
				Unique:    nonUnique == 0,
				IsPrimary: idxName == "PRIMARY",
				Type:      strings.ToUpper(indexType),
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

		idx.Columns = append(idx.Columns, toSnakeCase(colName.String))
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

func introspectForeignKeys(db *sql.DB, dbName, tableName string) ([]ForeignKey, error) {
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
				Name:       toSnakeCase(fkName),
				RefTable:   refTable,
				RefPGTable: toSnakeCase(refTable),
				UpdateRule: updateRule,
				DeleteRule: deleteRule,
			}
			fkMap[fkName] = fk
			fkOrder = append(fkOrder, fkName)
		}
		fk.Columns = append(fk.Columns, toSnakeCase(colName))
		fk.RefColumns = append(fk.RefColumns, toSnakeCase(refCol))
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
