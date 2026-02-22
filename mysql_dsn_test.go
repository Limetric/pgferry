package main

import (
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestMySQLDSNWithReadOptions_NoExistingParams(t *testing.T) {
	in := "root:root@tcp(127.0.0.1:3306)/example_db"
	out, err := mysqlDSNWithReadOptions(in)
	if err != nil {
		t.Fatalf("mysqlDSNWithReadOptions() error: %v", err)
	}

	cfg, err := mysql.ParseDSN(out)
	if err != nil {
		t.Fatalf("ParseDSN(out) error: %v", err)
	}
	if cfg.DBName != "example_db" {
		t.Fatalf("DBName = %q, want %q", cfg.DBName, "example_db")
	}
	if !cfg.ParseTime {
		t.Fatalf("ParseTime = false, want true")
	}
	if !cfg.InterpolateParams {
		t.Fatalf("InterpolateParams = false, want true")
	}
	if cfg.Loc == nil || cfg.Loc.String() != time.UTC.String() {
		t.Fatalf("Loc = %v, want %v", cfg.Loc, time.UTC)
	}
}

func TestMySQLDSNWithReadOptions_PreservesExistingParams(t *testing.T) {
	in := "root:root@tcp(127.0.0.1:3306)/example_db?charset=utf8mb4&parseTime=false"
	out, err := mysqlDSNWithReadOptions(in)
	if err != nil {
		t.Fatalf("mysqlDSNWithReadOptions() error: %v", err)
	}

	if strings.Count(out, "?") != 1 {
		t.Fatalf("expected a single '?' in output DSN, got: %s", out)
	}
	if !strings.Contains(out, "charset=utf8mb4") {
		t.Fatalf("expected output DSN to preserve charset param, got: %s", out)
	}

	cfg, err := mysql.ParseDSN(out)
	if err != nil {
		t.Fatalf("ParseDSN(out) error: %v", err)
	}
	if !cfg.ParseTime {
		t.Fatalf("ParseTime = false, want true")
	}
}

func TestMySQLDSNWithReadOptions_InvalidDSN(t *testing.T) {
	_, err := mysqlDSNWithReadOptions("://bad-dsn")
	if err == nil {
		t.Fatal("expected parse error for invalid DSN")
	}
}
