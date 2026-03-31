package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	mysqldriver "github.com/go-sql-driver/mysql"
)

// BinlogReaderConfig holds configuration for creating a BinlogReader.
type BinlogReaderConfig struct {
	DSN      string
	ServerID uint32
	StartPos CDCPosition
	Tables   map[string]Table // source table name -> Table
	Src      SourceDB
	TypeMap  TypeMappingConfig
	DBName   string
}

// BinlogReader reads MySQL binlog events and emits CDCEvents for tracked tables.
type BinlogReader struct {
	syncer   *replication.BinlogSyncer
	streamer *replication.BinlogStreamer
	tables   map[string]Table
	tableMap map[uint64]*tableMap
	src      SourceDB
	typeMap  TypeMappingConfig
	dbName   string
	pos      CDCPosition
}

type tableMap struct {
	schema  string
	table   string
	columns []Column
}

func NewBinlogReader(cfg BinlogReaderConfig) (*BinlogReader, error) {
	dsnCfg, err := mysqldriver.ParseDSN(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse MySQL DSN for replication: %w", err)
	}

	host, portStr, err := net.SplitHostPort(dsnCfg.Addr)
	if err != nil {
		host = dsnCfg.Addr
		portStr = "3306"
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("parse MySQL port %q: %w", portStr, err)
	}

	serverID := cfg.ServerID
	if serverID == 0 {
		serverID = stableServerID(cfg.DSN)
	}

	syncerCfg := replication.BinlogSyncerConfig{
		ServerID: serverID,
		Flavor:   "mysql",
		Host:     host,
		Port:     uint16(port),
		User:     dsnCfg.User,
		Password: dsnCfg.Passwd,
	}

	syncer := replication.NewBinlogSyncer(syncerCfg)

	var streamer *replication.BinlogStreamer
	if cfg.StartPos.GTID != "" {
		gtidSet, parseErr := mysql.ParseGTIDSet("mysql", cfg.StartPos.GTID)
		if parseErr != nil {
			syncer.Close()
			return nil, fmt.Errorf("parse GTID set %q: %w", cfg.StartPos.GTID, parseErr)
		}
		streamer, err = syncer.StartSyncGTID(gtidSet)
	} else {
		streamer, err = syncer.StartSync(mysql.Position{
			Name: cfg.StartPos.File,
			Pos:  cfg.StartPos.Pos,
		})
	}
	if err != nil {
		syncer.Close()
		return nil, fmt.Errorf("start binlog sync: %w", err)
	}

	return &BinlogReader{
		syncer:   syncer,
		streamer: streamer,
		tables:   cfg.Tables,
		tableMap: make(map[uint64]*tableMap),
		src:      cfg.Src,
		typeMap:  cfg.TypeMap,
		dbName:   cfg.DBName,
		pos:      cfg.StartPos,
	}, nil
}

func (r *BinlogReader) ReadEvent(ctx context.Context) (*CDCEvent, error) {
	ev, err := r.streamer.GetEvent(ctx)
	if err != nil {
		return nil, err
	}

	switch e := ev.Event.(type) {
	case *replication.RotateEvent:
		r.pos.File = string(e.NextLogName)
		r.pos.Pos = uint32(e.Position)
		return nil, nil

	case *replication.TableMapEvent:
		schema := string(e.Schema)
		table := string(e.Table)
		if schema != r.dbName {
			return nil, nil
		}
		if t, ok := r.tables[table]; ok {
			r.tableMap[e.TableID] = &tableMap{
				schema:  schema,
				table:   table,
				columns: t.Columns,
			}
		}
		return nil, nil

	case *replication.RowsEvent:
		r.pos.Pos = ev.Header.LogPos

		tm, ok := r.tableMap[e.TableID]
		if !ok {
			return nil, nil
		}

		var op CDCOperation
		switch ev.Header.EventType {
		case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
			op = CDCInsert
		case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
			op = CDCUpdate
		case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
			op = CDCDelete
		default:
			return nil, nil
		}

		rows, transformErr := r.transformRows(e, tm, op)
		if transformErr != nil {
			return nil, fmt.Errorf("transform rows for %s.%s: %w", tm.schema, tm.table, transformErr)
		}

		return &CDCEvent{
			Schema:    tm.schema,
			Table:     tm.table,
			Operation: op,
			Rows:      rows,
			Position:  r.pos,
		}, nil

	default:
		if ev.Header.LogPos > 0 {
			r.pos.Pos = ev.Header.LogPos
		}
		return nil, nil
	}
}

func (r *BinlogReader) transformRows(e *replication.RowsEvent, tm *tableMap, op CDCOperation) ([][]any, error) {
	rawRows := e.Rows
	if op == CDCUpdate {
		var afterRows [][]any
		for i := 1; i < len(rawRows); i += 2 {
			afterRows = append(afterRows, rawRows[i])
		}
		rawRows = afterRows
	}

	var result [][]any
	for _, rawRow := range rawRows {
		transformed := make([]any, len(rawRow))
		for i, val := range rawRow {
			if i < len(tm.columns) {
				tv, tvErr := r.src.TransformValue(val, tm.columns[i], r.typeMap)
				if tvErr != nil {
					log.Printf("[replicate] WARN: transform error table=%s col=%s: %v", tm.table, tm.columns[i].SourceName, tvErr)
					transformed[i] = val
				} else {
					transformed[i] = tv
				}
			} else {
				transformed[i] = val
			}
		}
		result = append(result, transformed)
	}
	return result, nil
}

func (r *BinlogReader) Close() {
	r.syncer.Close()
}

func stableServerID(dsn string) uint32 {
	var h uint32 = 2166136261
	for _, b := range []byte(dsn) {
		h ^= uint32(b)
		h *= 16777619
	}
	id := (h % 4294967284) + 11
	return id
}
