package main

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingStatementExecutor struct {
	calls      []string
	errByQuery map[string]error
}

func (r *recordingStatementExecutor) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	r.calls = append(r.calls, sql)
	if err := r.errByQuery[sql]; err != nil {
		return pgconn.CommandTag{}, err
	}
	return pgconn.NewCommandTag("EXECUTE 1"), nil
}

type fakeDDLSource struct {
	fakeNamedSource
	pgTypeByColumn  map[string]string
	defaultByColumn map[string]string
}

func (f fakeDDLSource) OpenDB(string) (*sql.DB, error) { return nil, nil }

func (f fakeDDLSource) MapType(col Column, _ TypeMappingConfig) (string, error) {
	if pgType, ok := f.pgTypeByColumn[col.PGName]; ok {
		return pgType, nil
	}
	return "text", nil
}

func (f fakeDDLSource) MapDefault(col Column, _ string, _ TypeMappingConfig) (string, error) {
	return f.defaultByColumn[col.PGName], nil
}
