package main

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// statementExecutor is the minimal write-only contract needed by DDL and hook
// helpers so they can be unit-tested without a live database connection.
type statementExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

// queryExecutor is the minimal read/write contract needed by helpers that both
// execute statements and query a single result row during setup/validation.
type queryExecutor interface {
	statementExecutor
	QueryRow(context.Context, string, ...any) pgx.Row
}

// rollbackExecutor is the minimal transactional contract needed for
// rollback-only preflight probes against PostgreSQL.
type rollbackExecutor interface {
	statementExecutor
	Rollback(context.Context) error
}
