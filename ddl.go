package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createTables generates and executes CREATE TABLE DDL for all tables.
// Tables are created as UNLOGGED with no PKs, FKs, or indexes for speed.
func createTables(ctx context.Context, pool *pgxpool.Pool, schema *Schema, pgSchema string) error {
	// TODO: implement in task 5
	return nil
}
