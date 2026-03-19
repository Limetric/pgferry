package main

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type statementExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type queryExecutor interface {
	statementExecutor
	QueryRow(context.Context, string, ...any) pgx.Row
}
