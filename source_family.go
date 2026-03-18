package main

func isMySQLFamilySourceType(sourceType string) bool {
	switch sourceType {
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

	// Keep a small Name()-based fallback for fake SourceDB implementations used
	// in tests, where the concrete type switch above cannot match.
	switch src.Name() {
	case "mysql":
		return "mysql"
	case "MySQL":
		return "mysql"
	case "mariadb":
		return "mariadb"
	case "MariaDB":
		return "mariadb"
	case "sqlite":
		return "sqlite"
	case "SQLite":
		return "sqlite"
	case "mssql":
		return "mssql"
	case "MSSQL":
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
