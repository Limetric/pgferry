package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type CDCOperation int

const (
	CDCInsert CDCOperation = iota
	CDCUpdate
	CDCDelete
)

func (o CDCOperation) String() string {
	switch o {
	case CDCInsert:
		return "INSERT"
	case CDCUpdate:
		return "UPDATE"
	case CDCDelete:
		return "DELETE"
	default:
		return "UNKNOWN"
	}
}

type CDCPosition struct {
	File string `json:"binlog_file"`
	Pos  uint32 `json:"binlog_position"`
	GTID string `json:"gtid_set,omitempty"`
}

type CDCEvent struct {
	Schema    string
	Table     string
	Operation CDCOperation
	Rows      [][]any
	Position  CDCPosition
}

// CDCCheckpointFile is the CDC position data saved to pgferry_checkpoint.json.
type CDCCheckpointFile struct {
	CDCPosition
	ServerID   uint32    `json:"server_id"`
	CapturedAt time.Time `json:"captured_at"`
}

// captureBinlogPosition queries the MySQL server for its current binlog coordinates.
func captureBinlogPosition(ctx context.Context, db *sql.DB) (CDCPosition, error) {
	var pos CDCPosition
	// Try MySQL 8.2+ syntax first, fall back to legacy.
	row := db.QueryRowContext(ctx, "SHOW BINARY LOG STATUS")
	var binlogDoDB, binlogIgnoreDB, executedGTIDSet string
	err := row.Scan(&pos.File, &pos.Pos, &binlogDoDB, &binlogIgnoreDB, &executedGTIDSet)
	if err != nil {
		row = db.QueryRowContext(ctx, "SHOW MASTER STATUS")
		err = row.Scan(&pos.File, &pos.Pos, &binlogDoDB, &binlogIgnoreDB, &executedGTIDSet)
		if err != nil {
			return pos, fmt.Errorf("capture binlog position: %w", err)
		}
	}
	if executedGTIDSet != "" {
		pos.GTID = executedGTIDSet
	}
	return pos, nil
}
