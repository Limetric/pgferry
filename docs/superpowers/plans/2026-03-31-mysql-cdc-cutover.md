# MySQL CDC Cutover Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add low-downtime MySQL cutover via binlog-based CDC (`replicate` + `cutover` commands) after an initial bulk `migrate`.

**Architecture:** Three-command workflow — `migrate` (with `mode = "cdc"`) captures binlog position during a consistent snapshot, `replicate` tails binlog and applies changes via idempotent upserts, `cutover` checks replication lag and reports readiness. CDC checkpoint is co-transactional with data in a PostgreSQL table for exactly-once semantics.

**Tech Stack:** Go, `go-mysql-org/go-mysql` (binlog reader), `pgx/v5` (PostgreSQL), Cobra (CLI)

**Spec:** `docs/superpowers/specs/2026-03-31-mysql-cdc-cutover-design.md`

---

## File Structure

**New files:**
| File | Responsibility |
|---|---|
| `cdc.go` | CDC types (`CDCPosition`, `CDCEvent`, `CDCOperation`), binlog position capture (`captureBinlogPosition`) |
| `cdc_reader.go` | `BinlogReader` — wraps `go-mysql` syncer, parses row events, filters tables, transforms values |
| `cdc_apply.go` | `CDCApplier` — builds upsert/delete SQL, applies batches in PG transactions, co-transactional checkpoint |
| `cdc_batcher.go` | `CDCBatcher` — buffers events, flushes on size/time threshold |
| `cdc_checkpoint.go` | CDC checkpoint table CRUD (create, seed, read, update) |
| `cmd_replicate.go` | `replicate` Cobra command — main loop, signal handling, status logging |
| `cmd_cutover.go` | `cutover` Cobra command — lag check, `--wait`, `--timeout` |
| `cdc_test.go` | Unit tests for CDC types, SQL generation, config validation, batcher |
| `cdc_integration_test.go` | Integration test for full CDC pipeline (`//go:build integration`) |

**Modified files:**
| File | Changes |
|---|---|
| `config.go` | Add `Mode`, `CDCBatchSize`, `CDCFlushInterval`, `CDCServerID` to `MigrationConfig`; add validation in `finalizeConfig` |
| `checkpoint.go` | Add `CDC *CDCCheckpoint` field to `CheckpointState` |
| `main.go` | Register `replicateCmd` and `cutoverCmd`; add CDC binlog capture + checkpoint table creation to migrate pipeline |
| `go.mod` / `go.sum` | Add `github.com/go-mysql-org/go-mysql` |

---

### Task 1: Add go-mysql dependency and CDC config fields

**Files:**
- Modify: `go.mod`
- Modify: `config.go:22-53` (MigrationConfig struct), `config.go:151-163` (defaults), `config.go:166-384` (validation)
- Test: `cdc_test.go` (new)

- [ ] **Step 1: Add the go-mysql dependency**

```bash
go get github.com/go-mysql-org/go-mysql@latest
```

- [ ] **Step 2: Write failing tests for CDC config validation**

Create `cdc_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"
)

func TestCDCConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*MigrationConfig)
		wantErr string
	}{
		{
			name: "cdc mode requires mysql source",
			modify: func(c *MigrationConfig) {
				c.Mode = "cdc"
				c.Source.Type = "sqlite"
			},
			wantErr: "mode \"cdc\" is only supported for mysql sources",
		},
		{
			name: "cdc mode rejects mariadb",
			modify: func(c *MigrationConfig) {
				c.Mode = "cdc"
				c.Source.Type = "mariadb"
			},
			wantErr: "mode \"cdc\" is only supported for mysql sources",
		},
		{
			name: "cdc mode rejects schema_only",
			modify: func(c *MigrationConfig) {
				c.Mode = "cdc"
				c.Source.Type = "mysql"
				c.SchemaOnly = true
			},
			wantErr: "mode \"cdc\" is incompatible with schema_only",
		},
		{
			name: "cdc mode rejects data_only",
			modify: func(c *MigrationConfig) {
				c.Mode = "cdc"
				c.Source.Type = "mysql"
				c.DataOnly = true
			},
			wantErr: "mode \"cdc\" is incompatible with data_only",
		},
		{
			name: "cdc mode rejects explicit snapshot_mode=none",
			modify: func(c *MigrationConfig) {
				c.Mode = "cdc"
				c.Source.Type = "mysql"
				c.SourceSnapshotMode = "none"
				c.cdcSnapshotModeExplicit = true
			},
			wantErr: "mode \"cdc\" requires source_snapshot_mode = \"single_tx\"",
		},
		{
			name: "cdc mode forces single_tx when not set",
			modify: func(c *MigrationConfig) {
				c.Mode = "cdc"
				c.Source.Type = "mysql"
			},
			wantErr: "", // should succeed
		},
		{
			name: "invalid mode value",
			modify: func(c *MigrationConfig) {
				c.Mode = "invalid"
			},
			wantErr: "mode must be one of: default, cdc",
		},
		{
			name: "default mode is valid",
			modify: func(c *MigrationConfig) {
				c.Mode = "default"
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validBaseConfig()
			tt.modify(&cfg)
			err := finalizeConfig(&cfg, t.TempDir())
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestCDCConfigDefaults(t *testing.T) {
	cfg := validBaseConfig()
	cfg.Mode = "cdc"
	cfg.Source.Type = "mysql"
	if err := finalizeConfig(&cfg, t.TempDir()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SourceSnapshotMode != "single_tx" {
		t.Errorf("expected single_tx, got %q", cfg.SourceSnapshotMode)
	}
	if cfg.CDCBatchSize != 500 {
		t.Errorf("expected batch size 500, got %d", cfg.CDCBatchSize)
	}
	if cfg.CDCFlushInterval != 200*time.Millisecond {
		t.Errorf("expected flush interval 200ms, got %v", cfg.CDCFlushInterval)
	}
}

// validBaseConfig returns a MigrationConfig with all required fields set
// so that finalizeConfig does not fail on unrelated missing fields.
func validBaseConfig() MigrationConfig {
	cfg := defaultMigrationConfig()
	cfg.Source.Type = "mysql"
	cfg.Source.DSN = "root:root@tcp(127.0.0.1:3306)/testdb"
	cfg.Target.DSN = "postgres://localhost/testdb"
	cfg.Schema = "public"
	return cfg
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test -run TestCDCConfig -count=1 -v ./...
```

Expected: compilation errors — `Mode`, `CDCBatchSize`, `CDCFlushInterval`, `CDCServerID`, `cdcSnapshotModeExplicit` fields don't exist yet.

- [ ] **Step 4: Add CDC fields to MigrationConfig and validation to finalizeConfig**

In `config.go`, add fields to `MigrationConfig` (after line 49, before `configDir`):

```go
	Mode             string        `toml:"mode"`               // "default" | "cdc"
	CDCBatchSize     int           `toml:"cdc_batch_size"`     // max events per apply batch
	CDCFlushInterval time.Duration `toml:"cdc_flush_interval"` // max time before flushing
	CDCServerID      uint32        `toml:"cdc_server_id"`      // MySQL replication server ID

	// cdcSnapshotModeExplicit tracks whether the user explicitly set source_snapshot_mode,
	// so CDC validation can distinguish "not set" (force single_tx) from "set to none" (error).
	cdcSnapshotModeExplicit bool
```

Add `"time"` to the imports in `config.go`.

In `defaultMigrationConfig()`, after the existing defaults add:

```go
		Mode:             "default",
		CDCBatchSize:     500,
		CDCFlushInterval: 200 * time.Millisecond,
```

In `loadConfig()`, after `toml.Decode` and before `applyConfigEnvOverrides`, detect if snapshot mode was explicitly set:

```go
	// Track whether user explicitly set source_snapshot_mode (for CDC validation).
	for _, key := range md.Keys() {
		if key.String() == "source_snapshot_mode" {
			cfg.cdcSnapshotModeExplicit = true
			break
		}
	}
```

In `finalizeConfig()`, add mode validation after the existing `cfg.Source.Type` check (after line 192, before the `cfg.Schema` check):

```go
	// Mode validation
	switch cfg.Mode {
	case "default", "cdc":
	default:
		return fmt.Errorf("mode must be one of: default, cdc")
	}
	if cfg.Mode == "cdc" {
		if cfg.Source.Type != "mysql" {
			return fmt.Errorf("mode \"cdc\" is only supported for mysql sources")
		}
		if cfg.SchemaOnly {
			return fmt.Errorf("mode \"cdc\" is incompatible with schema_only")
		}
		if cfg.DataOnly {
			return fmt.Errorf("mode \"cdc\" is incompatible with data_only")
		}
		if cfg.cdcSnapshotModeExplicit && cfg.SourceSnapshotMode != "single_tx" {
			return fmt.Errorf("mode \"cdc\" requires source_snapshot_mode = \"single_tx\"")
		}
		cfg.SourceSnapshotMode = "single_tx"
		if cfg.CDCBatchSize <= 0 {
			cfg.CDCBatchSize = 500
		}
		if cfg.CDCFlushInterval <= 0 {
			cfg.CDCFlushInterval = 200 * time.Millisecond
		}
	}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test -run TestCDCConfig -count=1 -v ./...
```

Expected: all PASS.

- [ ] **Step 6: Format and commit**

```bash
go fmt ./...
git add cdc_test.go config.go go.mod go.sum
git commit -m "feat: add CDC config fields and validation (mode, cdc_batch_size, cdc_flush_interval, cdc_server_id)"
```

---

### Task 2: CDC types and checkpoint struct

**Files:**
- Create: `cdc.go`
- Modify: `checkpoint.go:16-21` (CheckpointState)

- [ ] **Step 1: Create CDC types**

Create `cdc.go`:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// CDCOperation identifies the type of a binlog row event.
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

// CDCPosition identifies a point in the MySQL binlog.
type CDCPosition struct {
	File string `json:"binlog_file"`
	Pos  uint32 `json:"binlog_position"`
	GTID string `json:"gtid_set,omitempty"`
}

// CDCEvent represents one or more parsed row changes from a single binlog row event.
type CDCEvent struct {
	Schema    string
	Table     string       // source table name
	Operation CDCOperation
	Rows      [][]any      // each row is column values in ordinal order
	Position  CDCPosition
}

// captureBinlogPosition queries the MySQL server for its current binlog coordinates.
func captureBinlogPosition(ctx context.Context, db *sql.DB) (CDCPosition, error) {
	var pos CDCPosition

	// Try MySQL 8.2+ syntax first, fall back to legacy.
	row := db.QueryRowContext(ctx, "SHOW BINARY LOG STATUS")
	var binlogDoDB, binlogIgnoreDB, executedGTIDSet string
	err := row.Scan(&pos.File, &pos.Pos, &binlogDoDB, &binlogIgnoreDB, &executedGTIDSet)
	if err != nil {
		// Fall back to legacy SHOW MASTER STATUS.
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
```

- [ ] **Step 2: Add CDCCheckpoint to CheckpointState**

In `checkpoint.go`, add a CDC field to `CheckpointState` (after `Tables` field):

```go
type CheckpointState struct {
	Version       int                         `json:"version"`
	StartedAt     time.Time                   `json:"started_at"`
	Compatibility *checkpointCompatibility    `json:"compatibility,omitempty"`
	Tables        map[string]*TableCheckpoint `json:"tables"`
	CDC           *CDCCheckpointFile          `json:"cdc,omitempty"`
}
```

Add the file checkpoint type in `cdc.go` (after the `CDCPosition` type):

```go
// CDCCheckpointFile is the CDC position data saved to pgferry_checkpoint.json.
type CDCCheckpointFile struct {
	CDCPosition
	ServerID   uint32    `json:"server_id"`
	CapturedAt time.Time `json:"captured_at"`
}
```

- [ ] **Step 3: Run existing tests to verify no regressions**

```bash
go build ./... && go test -count=1 ./...
```

Expected: all pass (the new `CDC` field is `omitempty` so existing checkpoints unmarshal fine).

- [ ] **Step 4: Format and commit**

```bash
go fmt ./...
git add cdc.go checkpoint.go
git commit -m "feat: add CDC types (CDCPosition, CDCEvent, CDCOperation) and binlog position capture"
```

---

### Task 3: CDC checkpoint table operations

**Files:**
- Create: `cdc_checkpoint.go`

- [ ] **Step 1: Create CDC checkpoint table operations**

Create `cdc_checkpoint.go`:

```go
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

// createCDCCheckpointTable creates the pgferry_cdc_checkpoint table in the target schema.
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

// readCDCCheckpoint reads the current CDC checkpoint from the target database.
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
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 3: Format and commit**

```bash
go fmt ./...
git add cdc_checkpoint.go
git commit -m "feat: add CDC checkpoint table operations (create, seed, read, update)"
```

---

### Task 4: Upsert and delete SQL generation

**Files:**
- Create: `cdc_apply.go`
- Modify: `cdc_test.go`

- [ ] **Step 1: Write failing tests for SQL generation**

Append to `cdc_test.go`:

```go
func TestBuildUpsertSQL(t *testing.T) {
	table := Table{
		PGName: "users",
		Columns: []Column{
			{PGName: "id"},
			{PGName: "name"},
			{PGName: "email"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	got := buildUpsertSQL("myschema", table)
	want := `INSERT INTO "myschema"."users" ("id", "name", "email") VALUES ($1, $2, $3) ON CONFLICT ("id") DO UPDATE SET "name" = EXCLUDED."name", "email" = EXCLUDED."email"`
	if got != want {
		t.Errorf("buildUpsertSQL:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestBuildUpsertSQL_CompositePK(t *testing.T) {
	table := Table{
		PGName: "order_items",
		Columns: []Column{
			{PGName: "order_id"},
			{PGName: "item_id"},
			{PGName: "quantity"},
			{PGName: "price"},
		},
		PrimaryKey: &Index{Columns: []string{"order_id", "item_id"}},
	}

	got := buildUpsertSQL("s", table)
	want := `INSERT INTO "s"."order_items" ("order_id", "item_id", "quantity", "price") VALUES ($1, $2, $3, $4) ON CONFLICT ("order_id", "item_id") DO UPDATE SET "quantity" = EXCLUDED."quantity", "price" = EXCLUDED."price"`
	if got != want {
		t.Errorf("buildUpsertSQL composite PK:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestBuildDeleteSQL(t *testing.T) {
	table := Table{
		PGName:     "users",
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	got := buildDeleteSQL("myschema", table)
	want := `DELETE FROM "myschema"."users" WHERE "id" = $1`
	if got != want {
		t.Errorf("buildDeleteSQL:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestBuildDeleteSQL_CompositePK(t *testing.T) {
	table := Table{
		PGName:     "order_items",
		PrimaryKey: &Index{Columns: []string{"order_id", "item_id"}},
	}

	got := buildDeleteSQL("s", table)
	want := `DELETE FROM "s"."order_items" WHERE "order_id" = $1 AND "item_id" = $2`
	if got != want {
		t.Errorf("buildDeleteSQL composite PK:\n  got:  %s\n  want: %s", got, want)
	}
}

func TestPKColumnPositions(t *testing.T) {
	table := Table{
		Columns: []Column{
			{PGName: "id"},
			{PGName: "name"},
			{PGName: "email"},
		},
		PrimaryKey: &Index{Columns: []string{"id"}},
	}

	positions := pkColumnPositions(table)
	if len(positions) != 1 || positions[0] != 0 {
		t.Errorf("expected [0], got %v", positions)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run "TestBuild(Upsert|Delete)SQL|TestPKColumnPositions" -count=1 -v ./...
```

Expected: compile errors — `buildUpsertSQL`, `buildDeleteSQL`, `pkColumnPositions` not defined.

- [ ] **Step 3: Implement SQL generation functions**

Create `cdc_apply.go`:

```go
package main

import (
	"fmt"
	"strings"
)

// buildUpsertSQL builds an INSERT ... ON CONFLICT DO UPDATE for the given table.
// All columns are included in the INSERT; non-PK columns are updated on conflict.
func buildUpsertSQL(pgSchema string, table Table) string {
	cols := make([]string, len(table.Columns))
	params := make([]string, len(table.Columns))
	for i, col := range table.Columns {
		cols[i] = pgIdent(col.PGName)
		params[i] = fmt.Sprintf("$%d", i+1)
	}

	pkSet := make(map[string]bool, len(table.PrimaryKey.Columns))
	for _, pk := range table.PrimaryKey.Columns {
		pkSet[pk] = true
	}

	pkCols := make([]string, len(table.PrimaryKey.Columns))
	for i, pk := range table.PrimaryKey.Columns {
		pkCols[i] = pgIdent(pk)
	}

	var setClauses []string
	for _, col := range table.Columns {
		if pkSet[col.PGName] {
			continue
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = EXCLUDED.%s", pgIdent(col.PGName), pgIdent(col.PGName)))
	}

	return fmt.Sprintf(
		"INSERT INTO %s.%s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		pgIdent(pgSchema),
		pgIdent(table.PGName),
		strings.Join(cols, ", "),
		strings.Join(params, ", "),
		strings.Join(pkCols, ", "),
		strings.Join(setClauses, ", "),
	)
}

// buildDeleteSQL builds a DELETE WHERE pk = $1 [AND pk2 = $2 ...] for the given table.
func buildDeleteSQL(pgSchema string, table Table) string {
	var whereClauses []string
	for i, pk := range table.PrimaryKey.Columns {
		whereClauses = append(whereClauses, fmt.Sprintf("%s = $%d", pgIdent(pk), i+1))
	}
	return fmt.Sprintf(
		"DELETE FROM %s.%s WHERE %s",
		pgIdent(pgSchema),
		pgIdent(table.PGName),
		strings.Join(whereClauses, " AND "),
	)
}

// pkColumnPositions returns the ordinal positions (0-based) of PK columns within table.Columns.
func pkColumnPositions(table Table) []int {
	pkSet := make(map[string]bool, len(table.PrimaryKey.Columns))
	for _, pk := range table.PrimaryKey.Columns {
		pkSet[pk] = true
	}
	var positions []int
	for i, col := range table.Columns {
		if pkSet[col.PGName] {
			positions = append(positions, i)
		}
	}
	return positions
}

// extractPKValues pulls the PK column values from a full row based on PK column positions.
func extractPKValues(row []any, pkPositions []int) []any {
	vals := make([]any, len(pkPositions))
	for i, pos := range pkPositions {
		vals[i] = row[pos]
	}
	return vals
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run "TestBuild(Upsert|Delete)SQL|TestPKColumnPositions" -count=1 -v ./...
```

Expected: all PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add cdc_apply.go cdc_test.go
git commit -m "feat: add upsert and delete SQL generation for CDC apply pipeline"
```

---

### Task 5: Event batcher

**Files:**
- Create: `cdc_batcher.go`
- Modify: `cdc_test.go`

- [ ] **Step 1: Write failing tests for the batcher**

Append to `cdc_test.go`:

```go
func TestCDCBatcher_FlushOnSize(t *testing.T) {
	b := newCDCBatcher(3)
	ev := func(pos uint32) *CDCEvent {
		return &CDCEvent{Position: CDCPosition{File: "bin.000001", Pos: pos}}
	}

	if batch := b.Add(ev(100)); batch != nil {
		t.Fatal("expected nil on first add")
	}
	if batch := b.Add(ev(200)); batch != nil {
		t.Fatal("expected nil on second add")
	}
	batch := b.Add(ev(300))
	if batch == nil {
		t.Fatal("expected batch on third add (size threshold)")
	}
	if len(batch) != 3 {
		t.Errorf("expected batch of 3, got %d", len(batch))
	}
	if b.Len() != 0 {
		t.Errorf("expected empty batcher after flush, got %d", b.Len())
	}
}

func TestCDCBatcher_ManualFlush(t *testing.T) {
	b := newCDCBatcher(10)
	ev := &CDCEvent{Position: CDCPosition{File: "bin.000001", Pos: 100}}
	b.Add(ev)

	batch := b.Flush()
	if batch == nil || len(batch) != 1 {
		t.Fatal("expected batch of 1 on manual flush")
	}

	batch = b.Flush()
	if batch != nil {
		t.Fatal("expected nil on flush of empty batcher")
	}
}

func TestCDCBatcher_Position(t *testing.T) {
	b := newCDCBatcher(100)
	b.Add(&CDCEvent{Position: CDCPosition{File: "bin.000001", Pos: 100}})
	b.Add(&CDCEvent{Position: CDCPosition{File: "bin.000001", Pos: 200}})

	pos := b.Position()
	if pos.Pos != 200 {
		t.Errorf("expected position 200, got %d", pos.Pos)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test -run TestCDCBatcher -count=1 -v ./...
```

Expected: compile errors — `newCDCBatcher`, `CDCBatcher` not defined.

- [ ] **Step 3: Implement the batcher**

Create `cdc_batcher.go`:

```go
package main

// CDCBatcher accumulates CDC events and flushes them as batches
// when the size threshold is reached or on manual flush.
type CDCBatcher struct {
	batch   []*CDCEvent
	maxSize int
	lastPos CDCPosition
}

func newCDCBatcher(maxSize int) *CDCBatcher {
	return &CDCBatcher{
		batch:   make([]*CDCEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add appends an event. Returns the full batch if the size threshold is reached, else nil.
func (b *CDCBatcher) Add(ev *CDCEvent) []*CDCEvent {
	b.batch = append(b.batch, ev)
	b.lastPos = ev.Position
	if len(b.batch) >= b.maxSize {
		return b.Flush()
	}
	return nil
}

// Flush returns the current batch and resets. Returns nil if empty.
func (b *CDCBatcher) Flush() []*CDCEvent {
	if len(b.batch) == 0 {
		return nil
	}
	batch := b.batch
	b.batch = make([]*CDCEvent, 0, b.maxSize)
	return batch
}

// Position returns the binlog position of the last event added.
func (b *CDCBatcher) Position() CDCPosition {
	return b.lastPos
}

// Len returns the number of buffered events.
func (b *CDCBatcher) Len() int {
	return len(b.batch)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test -run TestCDCBatcher -count=1 -v ./...
```

Expected: all PASS.

- [ ] **Step 5: Format and commit**

```bash
go fmt ./...
git add cdc_batcher.go cdc_test.go
git commit -m "feat: add CDCBatcher for micro-batching binlog events"
```

---

### Task 6: Binlog reader

**Files:**
- Create: `cdc_reader.go`

- [ ] **Step 1: Implement the BinlogReader**

Create `cdc_reader.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strconv"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
)

// BinlogReaderConfig holds configuration for creating a BinlogReader.
type BinlogReaderConfig struct {
	DSN      string
	ServerID uint32
	StartPos CDCPosition
	Tables   map[string]Table // source table name -> Table (only tables we care about)
	Src      SourceDB
	TypeMap  TypeMappingConfig
	DBName   string // source database name
}

// BinlogReader reads MySQL binlog events and emits CDCEvents for tracked tables.
type BinlogReader struct {
	syncer   *replication.BinlogSyncer
	streamer *replication.BinlogStreamer
	tables   map[string]Table     // source table name -> Table
	tableMap map[uint64]*tableMap // binlog table ID -> table metadata
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

// NewBinlogReader creates a new reader that streams from the given position.
func NewBinlogReader(cfg BinlogReaderConfig) (*BinlogReader, error) {
	dsnCfg, err := mysqldriver.ParseDSN(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse MySQL DSN for replication: %w", err)
	}

	host, portStr, err := net.SplitHostPort(dsnCfg.Addr)
	if err != nil {
		// Addr might be just a host without port; default to 3306
		host = dsnCfg.Addr
		portStr = "3306"
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, fmt.Errorf("parse MySQL port %q: %w", portStr, err)
	}

	serverID := cfg.ServerID
	if serverID == 0 {
		// Auto-generate a stable server ID from the DSN hash.
		serverID = stableServerID(cfg.DSN)
	}

	syncerCfg := replication.BinlogSyncerConfig{
		ServerID: serverID,
		Flavor:   "mysql",
		Host:     host,
		Port:     uint16(port),
		User:     dsnCfg.User,
		Passwd:   dsnCfg.Passwd,
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

// ReadEvent blocks until the next relevant CDCEvent is available.
// Returns nil event (no error) for events we skip (DDL, non-tracked tables).
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
			return nil, nil // not a tracked table
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

		rows, err := r.transformRows(e, tm, op)
		if err != nil {
			return nil, fmt.Errorf("transform rows for %s.%s: %w", tm.schema, tm.table, err)
		}

		return &CDCEvent{
			Schema:    tm.schema,
			Table:     tm.table,
			Operation: op,
			Rows:      rows,
			Position:  r.pos,
		}, nil

	default:
		// Update position from header for all events.
		if ev.Header.LogPos > 0 {
			r.pos.Pos = ev.Header.LogPos
		}
		return nil, nil
	}
}

// transformRows converts raw binlog row data through the same TransformValue path
// as the initial load. For UPDATE events, only the after-image rows are used.
func (r *BinlogReader) transformRows(e *replication.RowsEvent, tm *tableMap, op CDCOperation) ([][]any, error) {
	rawRows := e.Rows
	if op == CDCUpdate {
		// UPDATE events contain [before, after, before, after, ...] pairs.
		// We only need the after-image (odd-indexed rows).
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
				tv, err := r.src.TransformValue(val, tm.columns[i], r.typeMap)
				if err != nil {
					log.Printf("[replicate] WARN: transform error table=%s col=%s: %v", tm.table, tm.columns[i].SourceName, err)
					transformed[i] = val // use raw value as fallback
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

// Close shuts down the binlog syncer.
func (r *BinlogReader) Close() {
	r.syncer.Close()
}

// stableServerID generates a deterministic server ID from a DSN string.
func stableServerID(dsn string) uint32 {
	var h uint32 = 2166136261 // FNV-1a offset basis
	for _, b := range []byte(dsn) {
		h ^= uint32(b)
		h *= 16777619
	}
	// Ensure non-zero and avoid common server IDs (1-10).
	id := (h % 4294967284) + 11
	return id
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 3: Format and commit**

```bash
go fmt ./...
git add cdc_reader.go
git commit -m "feat: add BinlogReader wrapping go-mysql for CDC event streaming"
```

---

### Task 7: Apply pipeline

**Files:**
- Modify: `cdc_apply.go`

- [ ] **Step 1: Add the CDCApplier to cdc_apply.go**

Append to `cdc_apply.go`:

```go
import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CDCApplier applies batches of CDC events to PostgreSQL with co-transactional checkpoint updates.
type CDCApplier struct {
	pool     *pgxpool.Pool
	pgSchema string
	tables   map[string]Table // source table name -> Table
	src      SourceDB
	typeMap  TypeMappingConfig

	// Cached SQL per table.
	upsertCache map[string]string // table name -> upsert SQL
	deleteCache map[string]string // table name -> delete SQL
	pkPosCache  map[string][]int  // table name -> PK column positions

	// Cumulative stats (read via atomic for status logging).
	applied  atomic.Int64
	skipped  atomic.Int64
}

// NewCDCApplier creates an applier targeting the given PG pool and schema.
func NewCDCApplier(pool *pgxpool.Pool, pgSchema string, tables map[string]Table, src SourceDB, typeMap TypeMappingConfig) *CDCApplier {
	upsertCache := make(map[string]string, len(tables))
	deleteCache := make(map[string]string, len(tables))
	pkPosCache := make(map[string][]int, len(tables))
	for name, table := range tables {
		if table.PrimaryKey == nil || len(table.PrimaryKey.Columns) == 0 {
			continue
		}
		upsertCache[name] = buildUpsertSQL(pgSchema, table)
		deleteCache[name] = buildDeleteSQL(pgSchema, table)
		pkPosCache[name] = pkColumnPositions(table)
	}
	return &CDCApplier{
		pool:        pool,
		pgSchema:    pgSchema,
		tables:      tables,
		src:         src,
		typeMap:     typeMap,
		upsertCache: upsertCache,
		deleteCache: deleteCache,
		pkPosCache:  pkPosCache,
	}
}

// ApplyBatch applies a batch of events in a single PG transaction,
// updating the CDC checkpoint atomically with the data.
func (a *CDCApplier) ApplyBatch(ctx context.Context, events []*CDCEvent, pos CDCPosition) error {
	const maxRetries = 3

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			sleep := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
			time.Sleep(sleep)
		}
		lastErr = a.applyBatchOnce(ctx, events, pos)
		if lastErr == nil {
			return nil
		}
		log.Printf("[replicate] WARN: batch apply attempt %d failed: %v", attempt+1, lastErr)
	}
	return fmt.Errorf("batch apply failed after %d retries: %w", maxRetries, lastErr)
}

func (a *CDCApplier) applyBatchOnce(ctx context.Context, events []*CDCEvent, pos CDCPosition) error {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	batchApplied := int64(0)
	batchSkipped := int64(0)

	for _, ev := range events {
		upsertSQL, hasUpsert := a.upsertCache[ev.Table]
		deleteSQL, hasDelete := a.deleteCache[ev.Table]
		pkPositions := a.pkPosCache[ev.Table]

		if !hasUpsert || !hasDelete {
			continue // table not tracked or has no PK
		}

		for _, row := range ev.Rows {
			var execErr error
			switch ev.Operation {
			case CDCInsert, CDCUpdate:
				_, execErr = tx.Exec(ctx, upsertSQL, row...)
			case CDCDelete:
				pkVals := extractPKValues(row, pkPositions)
				_, execErr = tx.Exec(ctx, deleteSQL, pkVals...)
			}

			if execErr != nil {
				log.Printf("[replicate] WARN: skip event table=%s op=%s err=%v", ev.Table, ev.Operation, execErr)
				batchSkipped++
				continue
			}
			batchApplied++
		}
	}

	// Update checkpoint in same transaction.
	totalApplied := a.applied.Load() + batchApplied
	totalSkipped := a.skipped.Load() + batchSkipped
	if err := updateCDCCheckpointTx(ctx, tx, a.pgSchema, pos, totalApplied, totalSkipped); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit batch: %w", err)
	}

	a.applied.Add(batchApplied)
	a.skipped.Add(batchSkipped)
	return nil
}

// Stats returns the cumulative applied and skipped event counts.
func (a *CDCApplier) Stats() (applied, skipped int64) {
	return a.applied.Load(), a.skipped.Load()
}
```

Note: Update the imports at the top of `cdc_apply.go` to include `context`, `log`, `sync/atomic`, `time`, `fmt`, `strings`, and the pgxpool import.

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 3: Format and commit**

```bash
go fmt ./...
git add cdc_apply.go
git commit -m "feat: add CDCApplier for transactional batch apply with co-transactional checkpoint"
```

---

### Task 8: Replicate command

**Files:**
- Create: `cmd_replicate.go`
- Modify: `main.go:74-85` (init function)

- [ ] **Step 1: Implement the replicate command**

Create `cmd_replicate.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var replicateCmd = &cobra.Command{
	Use:   "replicate [migration.toml]",
	Short: "Tail MySQL binlog and apply changes to PostgreSQL",
	Long:  "Start CDC replication from the binlog position captured during migrate. Runs until interrupted (Ctrl+C).",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runReplicate,
}

func init() {
	replicateCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
}

func runReplicate(cmd *cobra.Command, args []string) error {
	cfgPath, err := resolveOptionalConfigPath(configPath, args)
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return missingMigrationConfigError()
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	if cfg.Mode != "cdc" {
		return fmt.Errorf("replicate requires mode = \"cdc\" in config")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("[replicate] received %s, shutting down gracefully...", sig)
		cancel()
	}()

	// Connect to PostgreSQL.
	pgPool, err := pgxpool.New(ctx, cfg.Target.DSN)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pgPool.Close()

	// Read CDC checkpoint.
	checkpoint, err := readCDCCheckpoint(ctx, pgPool, cfg.Schema)
	if err != nil {
		return fmt.Errorf("read CDC checkpoint (has migrate been run with mode = \"cdc\"?): %w", err)
	}

	startPos := checkpoint.Position()
	log.Printf("[replicate] starting from binlog=%s:%d applied=%d skipped=%d",
		startPos.File, startPos.Pos, checkpoint.EventsApplied, checkpoint.EventsSkipped)

	// Introspect source schema to get table/column metadata.
	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}
	srcDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}
	dbName, err := src.ExtractDBName(cfg.Source.DSN)
	if err != nil {
		return fmt.Errorf("extract source db name: %w", err)
	}
	schema, err := src.IntrospectSchema(srcDB, dbName)
	if err != nil {
		return fmt.Errorf("introspect source schema: %w", err)
	}
	srcDB.Close()

	// Filter schema using the same include/exclude logic as migrate.
	filteredSchema, _, filterErr := filterSchemaTables(schema, cfg)
	if filterErr != nil {
		return fmt.Errorf("filter tables: %w", filterErr)
	}

	// Build table map: source name -> Table (only tables with PKs).
	tables := make(map[string]Table)
	for _, t := range filteredSchema.Tables {
		if t.PrimaryKey == nil || len(t.PrimaryKey.Columns) == 0 {
			log.Printf("[replicate] WARN: skipping table %s (no primary key)", t.SourceName)
			continue
		}
		tables[t.SourceName] = t
	}

	if len(tables) == 0 {
		return fmt.Errorf("no tables with primary keys found for replication")
	}
	log.Printf("[replicate] tracking %d tables", len(tables))

	// Create components.
	reader, err := NewBinlogReader(BinlogReaderConfig{
		DSN:      cfg.Source.DSN,
		ServerID: cfg.CDCServerID,
		StartPos: startPos,
		Tables:   tables,
		Src:      src,
		TypeMap:  cfg.TypeMapping,
		DBName:   dbName,
	})
	if err != nil {
		return fmt.Errorf("start binlog reader: %w", err)
	}
	defer reader.Close()

	applier := NewCDCApplier(pgPool, cfg.Schema, tables, src, cfg.TypeMapping)
	batcher := newCDCBatcher(cfg.CDCBatchSize)

	// Pre-load stats from checkpoint.
	applier.applied.Store(checkpoint.EventsApplied)
	applier.skipped.Store(checkpoint.EventsSkipped)

	log.Printf("[replicate] replication started")

	// Main loop.
	return runReplicateLoop(ctx, reader, batcher, applier, cfg.CDCFlushInterval)
}

func runReplicateLoop(ctx context.Context, reader *BinlogReader, batcher *CDCBatcher, applier *CDCApplier, flushInterval time.Duration) error {
	const statusInterval = 10 * time.Second

	lastFlush := time.Now()
	lastStatus := time.Now()

	for {
		// Check for shutdown.
		select {
		case <-ctx.Done():
			if batch := batcher.Flush(); batch != nil {
				bgCtx := context.Background()
				if err := applier.ApplyBatch(bgCtx, batch, batcher.Position()); err != nil {
					log.Printf("[replicate] WARN: final flush failed: %v", err)
				}
			}
			applied, skipped := applier.Stats()
			log.Printf("[replicate] shutdown complete. applied=%d skipped=%d", applied, skipped)
			return nil
		default:
		}

		// Read next event with timeout equal to flush interval.
		readCtx, readCancel := context.WithTimeout(ctx, flushInterval)
		ev, err := reader.ReadEvent(readCtx)
		readCancel()

		if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			return fmt.Errorf("read binlog event: %w", err)
		}

		if ev != nil {
			if batch := batcher.Add(ev); batch != nil {
				if err := applier.ApplyBatch(ctx, batch, batcher.Position()); err != nil {
					return err
				}
				lastFlush = time.Now()
			}
		}

		// Time-based flush.
		if batcher.Len() > 0 && time.Since(lastFlush) >= flushInterval {
			if batch := batcher.Flush(); batch != nil {
				if err := applier.ApplyBatch(ctx, batch, batcher.Position()); err != nil {
					return err
				}
				lastFlush = time.Now()
			}
		}

		// Periodic status.
		if time.Since(lastStatus) >= statusInterval {
			applied, skipped := applier.Stats()
			pos := batcher.Position()
			if pos.File == "" {
				pos = reader.pos
			}
			log.Printf("[replicate] binlog=%s:%d | applied=%s | skipped=%d | last_applied=%s ago",
				pos.File, pos.Pos,
				humanize.Comma(applied),
				skipped,
				time.Since(lastStatus).Round(time.Second),
			)
			lastStatus = time.Now()
		}
	}
}

```

- [ ] **Step 2: Register the replicate command in main.go**

In `main.go`, in the `init()` function, add after `rootCmd.AddCommand(planCmd)`:

```go
	rootCmd.AddCommand(replicateCmd)
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 4: Format and commit**

```bash
go fmt ./...
git add cmd_replicate.go main.go
git commit -m "feat: add replicate command for MySQL CDC binlog tailing"
```

---

### Task 9: Cutover command

**Files:**
- Create: `cmd_cutover.go`
- Modify: `main.go:74-85` (init function)

- [ ] **Step 1: Implement the cutover command**

Create `cmd_cutover.go`:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

var cutoverWait bool
var cutoverTimeout time.Duration

var cutoverCmd = &cobra.Command{
	Use:   "cutover [migration.toml]",
	Short: "Check replication lag and report cutover readiness",
	Long:  "Compare the CDC checkpoint against the MySQL binlog head. With --wait, block until lag reaches zero.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCutover,
}

func init() {
	cutoverCmd.Flags().StringVar(&configPath, "config", "", "path to migration TOML config file")
	cutoverCmd.Flags().BoolVar(&cutoverWait, "wait", false, "block until replication lag reaches zero")
	cutoverCmd.Flags().DurationVar(&cutoverTimeout, "timeout", 5*time.Minute, "maximum time to wait (only with --wait)")
}

func runCutover(cmd *cobra.Command, args []string) error {
	cfgPath, err := resolveOptionalConfigPath(configPath, args)
	if err != nil {
		return err
	}
	if cfgPath == "" {
		return missingMigrationConfigError()
	}
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		return err
	}

	if cfg.Mode != "cdc" {
		return fmt.Errorf("cutover requires mode = \"cdc\" in config")
	}

	ctx := context.Background()

	// Connect to PostgreSQL.
	pgPool, err := pgxpool.New(ctx, cfg.Target.DSN)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pgPool.Close()

	// Connect to MySQL.
	src, err := newConfiguredSourceDB(cfg)
	if err != nil {
		return err
	}
	srcDB, err := src.OpenDB(cfg.Source.DSN)
	if err != nil {
		return fmt.Errorf("connect to MySQL: %w", err)
	}
	defer srcDB.Close()

	if cutoverWait {
		return runCutoverWait(ctx, pgPool, srcDB, cfg.Schema, cutoverTimeout)
	}
	return runCutoverCheck(ctx, pgPool, srcDB, cfg.Schema)
}

func runCutoverCheck(ctx context.Context, pgPool *pgxpool.Pool, srcDB *sql.DB, pgSchema string) error {
	checkpoint, err := readCDCCheckpoint(ctx, pgPool, pgSchema)
	if err != nil {
		return fmt.Errorf("read CDC checkpoint: %w", err)
	}

	mysqlPos, err := captureBinlogPosition(ctx, srcDB)
	if err != nil {
		return err
	}

	lag := computeByteLag(mysqlPos, checkpoint)
	if lag > 0 {
		log.Printf("[cutover] lag=~%s, last_applied=%s ago",
			humanize.IBytes(uint64(lag)),
			time.Since(checkpoint.LastApplied).Round(time.Second),
		)
		return fmt.Errorf("replication lag is not zero (lag=~%s)", humanize.IBytes(uint64(lag)))
	}

	printCutoverReady(checkpoint)
	return nil
}

func runCutoverWait(ctx context.Context, pgPool *pgxpool.Pool, srcDB *sql.DB, pgSchema string, timeout time.Duration) error {
	log.Printf("[cutover] Waiting for replication lag to reach zero...")

	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout: replication lag did not reach zero within %s", timeout)
		}

		checkpoint, err := readCDCCheckpoint(ctx, pgPool, pgSchema)
		if err != nil {
			return fmt.Errorf("read CDC checkpoint: %w", err)
		}

		mysqlPos, err := captureBinlogPosition(ctx, srcDB)
		if err != nil {
			return err
		}

		lag := computeByteLag(mysqlPos, checkpoint)
		if lag == 0 {
			printCutoverReady(checkpoint)
			return nil
		}

		log.Printf("[cutover] lag=~%s, last_applied=%s ago",
			humanize.IBytes(uint64(lag)),
			time.Since(checkpoint.LastApplied).Round(time.Second),
		)

		time.Sleep(1 * time.Second)
	}
}

func computeByteLag(mysqlPos CDCPosition, checkpoint *CDCCheckpointRow) int64 {
	if mysqlPos.File != checkpoint.BinlogFile {
		// Different binlog files — can't compute exact byte lag,
		// but it's definitely > 0.
		return 1
	}
	return int64(mysqlPos.Pos) - checkpoint.BinlogPos
}

func printCutoverReady(checkpoint *CDCCheckpointRow) {
	log.Printf("[cutover] lag=0, all events applied")
	log.Printf("[cutover] Cutover ready. Source and target are in sync.")
	log.Printf("[cutover]   Binlog position: %s:%d", checkpoint.BinlogFile, checkpoint.BinlogPos)
	log.Printf("[cutover]   Events applied: %s", humanize.Comma(checkpoint.EventsApplied))
	log.Printf("[cutover]   Events skipped: %d", checkpoint.EventsSkipped)
	log.Printf("[cutover]")
	log.Printf("[cutover] Next steps:")
	log.Printf("[cutover]   1. Stop writes to MySQL")
	log.Printf("[cutover]   2. Run 'pgferry cutover' again to confirm zero lag")
	log.Printf("[cutover]   3. Point your application to PostgreSQL")
}
```

- [ ] **Step 2: Register the cutover command in main.go**

In `main.go`, in the `init()` function, add after the `replicateCmd` line:

```go
	rootCmd.AddCommand(cutoverCmd)
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 4: Format and commit**

```bash
go fmt ./...
git add cmd_cutover.go main.go
git commit -m "feat: add cutover command for replication lag check and readiness reporting"
```

---

### Task 10: Wire CDC into the migrate pipeline

**Files:**
- Modify: `main.go:159-461` (runMigrationWithConfig)

- [ ] **Step 1: Add binlog capture and checkpoint table creation to migrate**

In `main.go`, in `runMigrationWithConfig`, add CDC binlog capture after the source connection is established and before data migration begins. Specifically, after the `prepareTargetSchema` / `createTables` block (around line 388) and before the `if !cfg.SchemaOnly` block (line 390):

```go
	// CDC: capture binlog position and create checkpoint table.
	var cdcPos CDCPosition
	if cfg.Mode == "cdc" {
		log.Printf("CDC mode: capturing binlog position...")
		srcDBForBinlog, openErr := src.OpenDB(cfg.Source.DSN)
		if openErr != nil {
			return fmt.Errorf("open source for binlog capture: %w", openErr)
		}
		cdcPos, err = captureBinlogPosition(ctx, srcDBForBinlog)
		srcDBForBinlog.Close()
		if err != nil {
			return err
		}
		log.Printf("CDC: binlog position captured: %s:%d (GTID: %s)", cdcPos.File, cdcPos.Pos, cdcPos.GTID)

		log.Printf("CDC: creating checkpoint table...")
		if err := createCDCCheckpointTable(ctx, pgPool, cfg.Schema); err != nil {
			return err
		}
		if err := seedCDCCheckpoint(ctx, pgPool, cfg.Schema, cdcPos); err != nil {
			return err
		}
		log.Printf("CDC: checkpoint table seeded")
	}
```

Also, after the migration completes successfully (after the `postMigrate` call, around line 457), save the CDC position to the file checkpoint:

```go
	// CDC: persist binlog position to checkpoint file for reference.
	if cfg.Mode == "cdc" {
		cpPath := checkpointPath(cfg.configDir)
		cpState := &CheckpointState{
			Version:   2,
			StartedAt: start,
			CDC: &CDCCheckpointFile{
				CDCPosition: cdcPos,
				ServerID:    cfg.CDCServerID,
				CapturedAt:  time.Now(),
			},
		}
		if err := saveCheckpoint(cpPath, cpState); err != nil {
			log.Printf("WARN: failed to save CDC checkpoint file: %v", err)
		} else {
			log.Printf("CDC: checkpoint saved to %s", cpPath)
		}
	}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./...
```

Expected: compiles cleanly.

- [ ] **Step 3: Run unit tests to check for regressions**

```bash
go test -count=1 ./...
```

Expected: all pass.

- [ ] **Step 4: Format and commit**

```bash
go fmt ./...
git add main.go
git commit -m "feat: wire CDC binlog capture and checkpoint table creation into migrate pipeline"
```

---

### Task 11: Integration test

**Files:**
- Create: `cdc_integration_test.go`

- [ ] **Step 1: Write the CDC integration test**

Create `cdc_integration_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegration_MySQLCDC(t *testing.T) {
	mysqlDSN := os.Getenv("MYSQL_DSN")
	pgDSN := os.Getenv("POSTGRES_DSN")
	if mysqlDSN == "" || pgDSN == "" {
		t.Skip("MYSQL_DSN and POSTGRES_DSN env vars required")
	}

	ctx := context.Background()

	// --- Seed source ---
	sourceDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql: %v", err)
	}
	defer sourceDB.Close()

	seedMySQLNoOrphans(t, sourceDB)

	// Ensure binlog_format is ROW.
	var binlogFormat string
	if err := sourceDB.QueryRow("SELECT @@binlog_format").Scan(&binlogFormat); err != nil {
		t.Fatalf("check binlog_format: %v", err)
	}
	if binlogFormat != "ROW" {
		t.Skipf("skipping CDC test: binlog_format=%s (need ROW)", binlogFormat)
	}

	sourceDB.Close()

	// --- Run migrate with CDC mode ---
	src := &mysqlSourceDB{}
	srcDB2, err := src.OpenDB(mysqlDSN)
	if err != nil {
		t.Fatalf("open mysql for introspection: %v", err)
	}
	dbName, err := src.ExtractDBName(mysqlDSN)
	if err != nil {
		t.Fatalf("extract db name: %v", err)
	}
	schema, err := src.IntrospectSchema(srcDB2, dbName)
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	srcDB2.Close()

	pgPool, err := pgxpool.New(ctx, pgDSN)
	if err != nil {
		t.Fatalf("connect pg: %v", err)
	}
	defer pgPool.Close()

	const pgSchema = "inttest_cdc"
	_, _ = pgPool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	if _, err := pgPool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgIdent(pgSchema))); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		pgPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgIdent(pgSchema)))
	})

	typeMap := defaultTypeMappingConfig()

	// Create tables.
	if err := createTables(ctx, pgPool, schema, pgSchema, false, true, typeMap, src); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	// Capture binlog position.
	capDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true")
	if err != nil {
		t.Fatalf("open mysql for binlog capture: %v", err)
	}
	cdcPos, err := captureBinlogPosition(ctx, capDB)
	capDB.Close()
	if err != nil {
		t.Fatalf("capture binlog position: %v", err)
	}
	t.Logf("captured binlog position: %s:%d", cdcPos.File, cdcPos.Pos)

	// Create and seed CDC checkpoint.
	if err := createCDCCheckpointTable(ctx, pgPool, pgSchema); err != nil {
		t.Fatalf("create cdc checkpoint table: %v", err)
	}
	if err := seedCDCCheckpoint(ctx, pgPool, pgSchema, cdcPos); err != nil {
		t.Fatalf("seed cdc checkpoint: %v", err)
	}

	// Migrate data (initial load).
	if err := migrateData(ctx, migrateDataConfig{
		Src: src, SrcDSN: mysqlDSN, Pool: pgPool, Schema: schema, PGSchema: pgSchema,
		Workers: 2, TypeMap: typeMap, SourceSnapshotMode: "single_tx",
		ChunkSize: 100000, ConfigDir: t.TempDir(),
	}); err != nil {
		t.Fatalf("migrateData: %v", err)
	}

	// Post-migrate: add PKs so upserts work.
	cfg := &MigrationConfig{
		Schema:         pgSchema,
		OnSchemaExists: "error",
		TypeMapping:    typeMap,
	}
	if err := postMigrate(ctx, pgPool, schema, cfg); err != nil {
		t.Fatalf("postMigrate: %v", err)
	}

	// Verify initial counts.
	assertRowCount(t, pgPool, pgSchema, "users", 5)
	assertRowCount(t, pgPool, pgSchema, "posts", 5)
	assertRowCount(t, pgPool, pgSchema, "comments", 10)

	// --- Make changes in MySQL ---
	changeDB, err := sql.Open("mysql", mysqlDSN+"?parseTime=true&loc=UTC&interpolateParams=true&multiStatements=true")
	if err != nil {
		t.Fatalf("open mysql for changes: %v", err)
	}
	defer changeDB.Close()

	// INSERT a new user.
	if _, err := changeDB.Exec("INSERT INTO users (name, email) VALUES ('Frank', 'frank@example.com')"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// UPDATE an existing user.
	if _, err := changeDB.Exec("UPDATE users SET email = 'alice_updated@example.com' WHERE name = 'Alice'"); err != nil {
		t.Fatalf("update user: %v", err)
	}
	// DELETE a user (Eve, id=5 — also delete her comments and posts first).
	if _, err := changeDB.Exec("DELETE FROM comments WHERE user_id = 5"); err != nil {
		t.Fatalf("delete comments: %v", err)
	}
	if _, err := changeDB.Exec("DELETE FROM posts WHERE user_id = 5"); err != nil {
		t.Fatalf("delete posts: %v", err)
	}
	if _, err := changeDB.Exec("DELETE FROM users WHERE name = 'Eve'"); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	// --- Run replicate ---
	tables := make(map[string]Table)
	for _, tbl := range schema.Tables {
		if tbl.PrimaryKey != nil {
			tables[tbl.SourceName] = tbl
		}
	}

	readerCtx, readerCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readerCancel()

	reader, err := NewBinlogReader(BinlogReaderConfig{
		DSN:      mysqlDSN,
		ServerID: 99999,
		StartPos: cdcPos,
		Tables:   tables,
		Src:      src,
		TypeMap:  typeMap,
		DBName:   dbName,
	})
	if err != nil {
		t.Fatalf("create binlog reader: %v", err)
	}
	defer reader.Close()

	applier := NewCDCApplier(pgPool, pgSchema, tables, src, typeMap)
	batcher := newCDCBatcher(100)

	// Read until we catch up (context timeout is our safety net).
	for {
		ev, readErr := reader.ReadEvent(readerCtx)
		if readErr != nil {
			// Timeout means we've likely caught up.
			break
		}
		if ev == nil {
			continue
		}
		if batch := batcher.Add(ev); batch != nil {
			if applyErr := applier.ApplyBatch(ctx, batch, batcher.Position()); applyErr != nil {
				t.Fatalf("apply batch: %v", applyErr)
			}
		}
	}
	// Flush remaining.
	if batch := batcher.Flush(); batch != nil {
		if err := applier.ApplyBatch(ctx, batch, batcher.Position()); err != nil {
			t.Fatalf("apply final batch: %v", err)
		}
	}

	// --- Assert CDC changes applied ---

	// Users: 5 original - 1 deleted (Eve) + 1 inserted (Frank) = 5
	assertRowCount(t, pgPool, pgSchema, "users", 5)

	// Check Frank was inserted.
	var frankEmail string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT email FROM %s.users WHERE name = 'Frank'", pgIdent(pgSchema)),
	).Scan(&frankEmail)
	if err != nil {
		t.Fatalf("query Frank: %v", err)
	}
	if frankEmail != "frank@example.com" {
		t.Errorf("expected Frank's email 'frank@example.com', got %q", frankEmail)
	}

	// Check Alice was updated.
	var aliceEmail string
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT email FROM %s.users WHERE name = 'Alice'", pgIdent(pgSchema)),
	).Scan(&aliceEmail)
	if err != nil {
		t.Fatalf("query Alice: %v", err)
	}
	if aliceEmail != "alice_updated@example.com" {
		t.Errorf("expected Alice's email 'alice_updated@example.com', got %q", aliceEmail)
	}

	// Check Eve was deleted.
	var eveCount int
	err = pgPool.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s.users WHERE name = 'Eve'", pgIdent(pgSchema)),
	).Scan(&eveCount)
	if err != nil {
		t.Fatalf("query Eve count: %v", err)
	}
	if eveCount != 0 {
		t.Errorf("expected Eve to be deleted, got count=%d", eveCount)
	}

	// Check CDC checkpoint was advanced.
	checkpoint, err := readCDCCheckpoint(ctx, pgPool, pgSchema)
	if err != nil {
		t.Fatalf("read cdc checkpoint: %v", err)
	}
	if checkpoint.EventsApplied == 0 {
		t.Error("expected events_applied > 0")
	}
	t.Logf("CDC checkpoint: file=%s pos=%d applied=%d skipped=%d",
		checkpoint.BinlogFile, checkpoint.BinlogPos, checkpoint.EventsApplied, checkpoint.EventsSkipped)
}
```

- [ ] **Step 2: Run the integration test (requires MySQL and PostgreSQL)**

```bash
MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -run TestIntegration_MySQLCDC -count=1 -v ./...
```

Expected: PASS — inserts, updates, and deletes are all reflected in PostgreSQL.

- [ ] **Step 3: Format and commit**

```bash
go fmt ./...
git add cdc_integration_test.go
git commit -m "test: add CDC integration test proving insert/update/delete replication"
```

---

### Task 12: Documentation updates

**Files:**
- Modify: `site/src/content/docs/reference/configuration.md`
- Create: `site/src/content/docs/guides/mysql-cdc-cutover.md`
- Modify: `site/src/content/docs/reference/type-mapping.md`

- [ ] **Step 1: Add CDC config fields to configuration.md**

Add a new section to `site/src/content/docs/reference/configuration.md` for CDC configuration fields:

```markdown
### CDC Mode

| Key | Type | Default | Description |
|---|---|---|---|
| `mode` | `string` | `"default"` | Migration mode. Set to `"cdc"` to enable binlog-based change capture for low-downtime MySQL cutover. |
| `cdc_batch_size` | `int` | `500` | Maximum number of binlog events per apply batch. Only used when `mode = "cdc"`. |
| `cdc_flush_interval` | `duration` | `"200ms"` | Maximum time to buffer events before flushing a batch. Only used when `mode = "cdc"`. |
| `cdc_server_id` | `int` | auto | MySQL replication server ID. Auto-generated from DSN if not set. Only used when `mode = "cdc"`. |

**Constraints when `mode = "cdc"`:**
- `source.type` must be `"mysql"`
- `source_snapshot_mode` is forced to `"single_tx"`
- `schema_only` and `data_only` must be `false`
- MySQL user must have `REPLICATION SLAVE` and `REPLICATION CLIENT` privileges
- MySQL must have `binlog_format = ROW`
```

- [ ] **Step 2: Create the CDC cutover guide**

Create `site/src/content/docs/guides/mysql-cdc-cutover.md`:

```markdown
---
title: Low-Downtime MySQL Cutover
description: Use pgferry's CDC mode to migrate from MySQL to PostgreSQL with near-zero downtime.
---

pgferry's CDC mode captures ongoing MySQL changes via binlog replication after the initial bulk load, allowing you to cut over to PostgreSQL with minimal downtime.

## Prerequisites

- MySQL 5.7+ or 8.0+ with `binlog_format = ROW`
- GTID mode recommended (not required)
- MySQL user with `SELECT`, `REPLICATION SLAVE`, and `REPLICATION CLIENT` privileges
- All tables to replicate must have primary keys

## Step 1: Configure

```toml
mode = "cdc"
schema = "myapp"

[source]
type = "mysql"
dsn = "repl_user:password@tcp(mysql-host:3306)/mydb"

[target]
dsn = "postgres://user:password@pg-host:5432/mydb?sslmode=require"
```

Setting `mode = "cdc"` automatically enables `source_snapshot_mode = "single_tx"` for a consistent snapshot.

## Step 2: Initial Load

```bash
pgferry migrate migration.toml
```

This runs the standard migration pipeline and additionally:
- Captures the MySQL binlog position at snapshot time
- Creates a `pgferry_cdc_checkpoint` table in the target schema

## Step 3: Start Replication

```bash
pgferry replicate migration.toml
```

This connects to MySQL as a replication client and applies ongoing changes (INSERT, UPDATE, DELETE) to PostgreSQL. It runs continuously until you stop it with Ctrl+C.

Status is logged every 10 seconds:
```
[replicate] binlog=mysql-bin.000042:83927104 | applied=142,857 | skipped=0 | last_applied=2s ago
```

## Step 4: Cutover

When you're ready to switch:

```bash
# Check current lag
pgferry cutover migration.toml

# Or wait for zero lag
pgferry cutover --wait --timeout 2m migration.toml
```

Once lag reaches zero:
1. Stop writes to MySQL (e.g., put your application in read-only mode)
2. Run `pgferry cutover` one more time to confirm zero lag
3. Point your application to PostgreSQL
4. Stop the `replicate` process (Ctrl+C)

## Limitations

- MySQL source only (MariaDB, SQLite, MSSQL are not supported for CDC)
- Tables without primary keys are skipped
- DDL changes (ALTER TABLE, etc.) during replication are not supported
- Row-based replication only (`binlog_format = ROW`)
```

- [ ] **Step 3: Add CDC note to type-mapping.md**

Add a note to `site/src/content/docs/reference/type-mapping.md`:

```markdown
:::note
When using CDC mode (`mode = "cdc"`), the same type mapping configuration applies to both the initial bulk load and ongoing binlog replication. Values from the binlog are transformed through the same `TransformValue` path, ensuring consistency between the snapshot and CDC-replicated data.
:::
```

- [ ] **Step 4: Commit documentation**

```bash
git add site/src/content/docs/reference/configuration.md site/src/content/docs/guides/mysql-cdc-cutover.md site/src/content/docs/reference/type-mapping.md
git commit -m "docs: add CDC mode configuration reference and cutover guide"
```

---

## Dependency Summary

| New Dependency | Purpose |
|---|---|
| `github.com/go-mysql-org/go-mysql` | MySQL binlog replication protocol and event parsing |

## Verification Checklist

After all tasks are complete:

- [ ] `go build ./...` compiles cleanly
- [ ] `go vet ./...` reports no issues
- [ ] `go fmt ./...` produces no changes
- [ ] `go test -count=1 ./...` — all unit tests pass
- [ ] Integration test passes with MySQL + PostgreSQL (see Task 11)
- [ ] `pgferry --help` shows `replicate` and `cutover` commands
- [ ] Documentation site builds without errors
