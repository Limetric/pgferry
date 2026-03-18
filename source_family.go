package main

import "strings"

func isMySQLFamilySourceType(sourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "mysql", "mariadb":
		return true
	default:
		return false
	}
}

func sourceTypeForDB(src SourceDB) string {
	switch src.(type) {
	case *mysqlSourceDB:
		return "mysql"
	case *mariadbSourceDB:
		return "mariadb"
	case *sqliteSourceDB:
		return "sqlite"
	case *mssqlSourceDB:
		return "mssql"
	}

	if src == nil {
		return ""
	}

	switch strings.ToLower(strings.TrimSpace(src.Name())) {
	case "mysql":
		return "mysql"
	case "mariadb":
		return "mariadb"
	case "sqlite":
		return "sqlite"
	case "mssql":
		return "mssql"
	default:
		return ""
	}
}

func isMySQLFamilySource(src SourceDB) bool {
	return isMySQLFamilySourceType(sourceTypeForDB(src))
}

func mysqlFamilyBaseSource(src SourceDB) *mysqlSourceDB {
	switch s := src.(type) {
	case *mysqlSourceDB:
		return s
	case *mariadbSourceDB:
		return &s.mysqlSourceDB
	default:
		return nil
	}
}
