package main

import (
	"testing"
)

func TestMySQLSourceOpenDB_InvalidDSN(t *testing.T) {
	src := &mysqlSourceDB{}
	_, err := src.OpenDB("://bad-dsn")
	if err == nil {
		t.Fatal("expected error for invalid DSN")
	}
}

func TestMySQLSourceExtractDBName(t *testing.T) {
	src := &mysqlSourceDB{}
	name, err := src.ExtractDBName("root:root@tcp(127.0.0.1:3306)/example_db")
	if err != nil {
		t.Fatalf("ExtractDBName() error: %v", err)
	}
	if name != "example_db" {
		t.Errorf("ExtractDBName() = %q, want %q", name, "example_db")
	}
}

func TestMySQLSourceQuoteIdentifier(t *testing.T) {
	src := &mysqlSourceDB{}
	got := src.QuoteIdentifier("my`table")
	want := "`my``table`"
	if got != want {
		t.Errorf("QuoteIdentifier() = %q, want %q", got, want)
	}
}
