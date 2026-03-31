package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CDCCheckpointRow represents a row from the pgferry_cdc_checkpoint table.
type CDCCheckpointRow struct {
	BinlogFile    string
	BinlogPos     int64
	GTIDSet       string
	LastApplied   time.Time
	EventsApplied int64
	EventsSkipped int64
}

// Position returns the CDCPosition from the checkpoint row.
func (r *CDCCheckpointRow) Position() CDCPosition {
	return CDCPosition{
		File: r.BinlogFile,
		Pos:  uint32(r.BinlogPos),
		GTID: r.GTIDSet,
	}
}

// createCDCCheckpointTable creates the pgferry_cdc_checkpoint table.
func createCDCCheckpointTable(ctx context.Context, pool *pgxpool.Pool, pgSchema string) error {
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.pgferry_cdc_checkpoint (
		id              INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
		binlog_file     TEXT NOT NULL,
		binlog_pos      BIGINT NOT NULL,
		gtid_set        TEXT,
		last_applied    TIMESTAMPTZ NOT NULL DEFAULT now(),
		events_applied  BIGINT NOT NULL DEFAULT 0,
		events_skipped  BIGINT NOT NULL DEFAULT 0
	)`, pgIdent(pgSchema))
	_, err := pool.Exec(ctx, q)
	if err != nil {
		return fmt.Errorf("create cdc checkpoint table: %w", err)
	}
	return nil
}

// seedCDCCheckpoint inserts the initial checkpoint row.
func seedCDCCheckpoint(ctx context.Context, pool *pgxpool.Pool, pgSchema string, pos CDCPosition) error {
	q := fmt.Sprintf(
		`INSERT INTO %s.pgferry_cdc_checkpoint (id, binlog_file, binlog_pos, gtid_set) VALUES (1, $1, $2, $3)
		 ON CONFLICT (id) DO UPDATE SET binlog_file = $1, binlog_pos = $2, gtid_set = $3, last_applied = now(), events_applied = 0, events_skipped = 0`,
		pgIdent(pgSchema),
	)
	_, err := pool.Exec(ctx, q, pos.File, int64(pos.Pos), pos.GTID)
	if err != nil {
		return fmt.Errorf("seed cdc checkpoint: %w", err)
	}
	return nil
}

// readCDCCheckpoint reads the current CDC checkpoint.
func readCDCCheckpoint(ctx context.Context, pool *pgxpool.Pool, pgSchema string) (*CDCCheckpointRow, error) {
	q := fmt.Sprintf(
		`SELECT binlog_file, binlog_pos, COALESCE(gtid_set, ''), last_applied, events_applied, events_skipped
		 FROM %s.pgferry_cdc_checkpoint WHERE id = 1`,
		pgIdent(pgSchema),
	)
	row := pool.QueryRow(ctx, q)
	var cp CDCCheckpointRow
	err := row.Scan(&cp.BinlogFile, &cp.BinlogPos, &cp.GTIDSet, &cp.LastApplied, &cp.EventsApplied, &cp.EventsSkipped)
	if err != nil {
		return nil, fmt.Errorf("read cdc checkpoint: %w", err)
	}
	return &cp, nil
}

// updateCDCCheckpointTx updates the CDC checkpoint within an existing transaction.
func updateCDCCheckpointTx(ctx context.Context, tx pgx.Tx, pgSchema string, pos CDCPosition, applied, skipped int64) error {
	q := fmt.Sprintf(
		`UPDATE %s.pgferry_cdc_checkpoint SET binlog_file = $1, binlog_pos = $2, gtid_set = $3, last_applied = now(), events_applied = $4, events_skipped = $5 WHERE id = 1`,
		pgIdent(pgSchema),
	)
	_, err := tx.Exec(ctx, q, pos.File, int64(pos.Pos), pos.GTID, applied, skipped)
	if err != nil {
		return fmt.Errorf("update cdc checkpoint: %w", err)
	}
	return nil
}
