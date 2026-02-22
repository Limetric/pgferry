package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrateData streams data from MySQL to PostgreSQL for all tables using parallel workers.
func migrateData(ctx context.Context, mysqlDSN string, pool *pgxpool.Pool, schema *Schema, pgSchema string, workers, batchSize int) error {
	// TODO: implement in task 6
	return nil
}
