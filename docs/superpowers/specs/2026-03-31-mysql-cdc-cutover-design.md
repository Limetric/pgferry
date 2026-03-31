# MySQL CDC Cutover Mode — Design Spec

**Issue:** [#20 — Add low-downtime MySQL cutover mode with binlog catch-up](https://github.com/Limetric/pgferry/issues/20)
**Date:** 2026-03-31

## Overview

Add a MySQL-only low-downtime migration mode to pgferry. After the existing bulk load completes, a binlog reader applies ongoing INSERT/UPDATE/DELETE changes to PostgreSQL until the target converges with the source, enabling a near-zero-downtime cutover.

### Scope constraints (intentionally narrow)

- MySQL source only (not MariaDB, SQLite, or MSSQL)
- Tables with primary keys only
- Row-based binlog events only (requires `binlog_format=ROW` on the source)
- Best-effort table include/exclude (reuses existing `include_tables` / `exclude_tables` config)
- No DDL replication — schema changes during replication are out of scope

## UX: Three Commands

The feature adds two new Cobra subcommands alongside the existing `migrate`:

| Command | Behavior | Lifetime |
|---|---|---|
| `pgferry migrate` | Existing bulk load. When `mode = "cdc"`, also captures binlog position and creates the CDC checkpoint table. | Batch — runs to completion |
| `pgferry replicate` | Tails MySQL binlog from the captured position, applies changes to PostgreSQL. | Long-running daemon — graceful shutdown on SIGINT/SIGTERM |
| `pgferry cutover` | Checks replication lag. With `--wait`, blocks until lag reaches zero. Reports readiness. | One-shot — exits with status |

These are separate commands because they have fundamentally different lifetimes and failure modes. The initial load is a batch job, replication is a daemon, and cutover is a critical one-shot operation.

## Section 1: Binlog Position Capture During Initial Load

When `mode = "cdc"`, the `migrate` command captures the binlog coordinates before starting the snapshot transaction.

**Capture method:** Execute `SHOW MASTER STATUS` (or `SHOW BINARY LOG STATUS` on MySQL 8.2+) to get the current binlog file and position. For GTID-enabled servers, also capture `@@gtid_executed`.

**Snapshot consistency:** `mode = "cdc"` requires `source_snapshot_mode = "single_tx"`. The `REPEATABLE READ` transaction gives a consistent point-in-time snapshot; the binlog coordinates captured just before that transaction begins mark where ongoing changes start diverging.

**Persisted state:** The binlog position is written to the existing `pgferry_checkpoint.json` file:

```go
type CDCCheckpoint struct {
    BinlogFile     string    `json:"binlog_file"`
    BinlogPosition uint32    `json:"binlog_position"`
    GTIDSet        string    `json:"gtid_set,omitempty"`
    ServerID       uint32    `json:"server_id"`
    CapturedAt     time.Time `json:"captured_at"`
}
```

**Additionally**, `migrate` creates the `pgferry_cdc_checkpoint` table in the target PostgreSQL database and seeds it with the captured position (see Section 4).

**Config validation rules for `mode = "cdc"`:**

- `source_type` must be `"mysql"`
- `source_snapshot_mode` is forced to `"single_tx"` (set automatically if unspecified; error if explicitly set to `"none"`)
- `schema_only` must be `false`
- `data_only` must be `false`
- MySQL user must have `REPLICATION SLAVE` and `REPLICATION CLIENT` privileges

## Section 2: Binlog Reader and Event Parsing

The `replicate` command connects to MySQL as a replication client using `go-mysql-org/go-mysql`'s `replication.BinlogSyncer`.

**Connection:** Connects with a configurable `server_id` (default: auto-generated from a hash of the DSN for stability across restarts). Starts streaming from the position saved in `pgferry_cdc_checkpoint`, preferring GTID mode when a GTID set is available.

**Events we process:**

| Binlog Event | Extraction |
|---|---|
| `WRITE_ROWS` | Table ID + row data -> upsert |
| `UPDATE_ROWS` | Table ID + after-image row data -> upsert |
| `DELETE_ROWS` | Table ID + row data -> delete |

**Events we ignore:** DDL (QUERY_EVENT with ALTER/CREATE), XID (transaction commits), heartbeats. Rotate events are processed internally to track the current binlog filename but produce no apply operations.

**Table filtering:** Only events for tables included in the initial migration are processed. Tables without primary keys are skipped with a warning. Table map events in the binlog provide schema+table name for matching.

**Schema introspection at startup:** `replicate` re-introspects the source schema on startup to get column names and primary key information. Binlog table map events carry column types but not names — the introspected schema maps column positions to names and identifies PK columns.

**Type mapping:** Binlog row values are converted through the same `TransformValue()` code path used during the initial load, ensuring type consistency between the snapshot and CDC paths.

## Section 3: Apply Pipeline (Idempotent Upsert Batching)

Parsed binlog events are buffered into micro-batches and applied to PostgreSQL.

**Batch thresholds (flush when either is reached):**

- Size: 500 events (configurable via `cdc_batch_size`)
- Time: 200ms since batch started (configurable via `cdc_flush_interval`)

**Apply logic per event type:**

- **INSERT / UPDATE -> upsert:** `INSERT INTO table (cols...) VALUES ($1...) ON CONFLICT (pk_cols...) DO UPDATE SET col1=EXCLUDED.col1, col2=EXCLUDED.col2, ...`
- **DELETE:** `DELETE FROM table WHERE pk_col1=$1 AND pk_col2=$2 ...`

Events within a batch are applied in binlog order to preserve causality.

**Prepared statement cache:** Upsert and delete statements are prepared once per table on first use. Keyed by `(table_name, operation)` — at most 2 entries per table.

**Batch transaction flow:**

```sql
BEGIN;
  EXECUTE upsert_users ($1, $2, ...);
  EXECUTE upsert_users ($1, $2, ...);
  EXECUTE delete_posts ($1);
  EXECUTE upsert_posts ($1, $2, ...);
  UPDATE pgferry_cdc_checkpoint SET binlog_file=$1, binlog_position=$2, ... WHERE id=1;
COMMIT;
```

The binlog position is updated in the same PostgreSQL transaction as the data changes. This gives exactly-once semantics: if the transaction commits, the checkpoint advances; if it crashes, both roll back and replay from the last committed position is safe because upserts are idempotent.

**Error handling:**

- Transient PostgreSQL errors (connection reset, deadlock): retry the batch up to 3 times with exponential backoff
- Permanent errors (constraint violation, type mismatch): log the failing event with full context, skip it, increment skipped counter, continue replication

## Section 4: Checkpoint and State Management

**Two checkpoint stores:**

| Store | Format | Purpose |
|---|---|---|
| `pgferry_checkpoint.json` | JSON file on disk | Initial load progress + CDC start position. Written by `migrate`, read by `replicate` to bootstrap. |
| `pgferry_cdc_checkpoint` | PostgreSQL table in target DB | Ongoing replication position. Updated transactionally with data changes. |

**Why a PG table for CDC:** The JSON checkpoint works for batch migration (flush periodically, redo a few chunks on crash). For CDC, the checkpoint must advance atomically with the data. Co-locating both in the same PostgreSQL transaction gives exactly-once semantics without two-phase commit.

**`pgferry_cdc_checkpoint` table:**

```sql
CREATE TABLE IF NOT EXISTS pgferry_cdc_checkpoint (
    id              INTEGER PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    binlog_file     TEXT NOT NULL,
    binlog_pos      BIGINT NOT NULL,
    gtid_set        TEXT,
    last_applied    TIMESTAMPTZ NOT NULL DEFAULT now(),
    events_applied  BIGINT NOT NULL DEFAULT 0,
    events_skipped  BIGINT NOT NULL DEFAULT 0
);
```

Single-row table enforced by the `CHECK (id = 1)` constraint.

**State transitions across commands:**

1. `migrate` (with `mode = "cdc"` in config) writes binlog position to `pgferry_checkpoint.json`, creates `pgferry_cdc_checkpoint` table, seeds it with the same position.
2. `replicate` reads current position from `pgferry_cdc_checkpoint`, streams from there, advances checkpoint with every committed batch.
3. `cutover` reads `pgferry_cdc_checkpoint` to determine lag, waits for convergence, reports readiness.

**Restart safety:** If `replicate` crashes, it restarts from the last committed position in `pgferry_cdc_checkpoint`. Replayed events are safe due to idempotent upserts.

## Section 5: CLI Commands and Configuration

**New config fields** (TOML):

```toml
# Enables CDC mode
mode = "cdc"  # "default" | "cdc". Default: "default"

# CDC tuning (only relevant when mode = "cdc")
cdc_batch_size = 500           # Max events per apply batch. Default: 500
cdc_flush_interval = "200ms"   # Max time before flushing. Default: "200ms"
cdc_server_id = 0              # MySQL replication server ID. Default: auto-generated
```

**`pgferry migrate`:** Unchanged when `mode = "default"`. When `mode = "cdc"`: captures binlog position, creates checkpoint table, seeds it. Rest of pipeline identical.

**`pgferry replicate`:**

- Requires config file (same TOML as `migrate`)
- Validates `mode = "cdc"` and that a CDC checkpoint exists
- Connects to MySQL as replication client and to PostgreSQL
- Tails binlog, applies batches, advances checkpoint
- Logs status every 10 seconds
- Graceful shutdown on SIGINT/SIGTERM (finishes current batch, commits, exits)
- Exit code 0 on clean shutdown, 1 on error

**`pgferry cutover`:**

- Reads `pgferry_cdc_checkpoint` for current position
- Compares against MySQL's current binlog position (`SHOW MASTER STATUS`)
- Default: prints lag status and exits (code 0 if lag=0, code 1 if lag>0)
- `--wait` flag: blocks until lag reaches zero, polling every second
- `--timeout` flag: maximum wait time (default: 5 minutes)

**What cutover does NOT do:** stop `replicate`, modify the source, modify the target schema, or fence writes. It is a pure read-and-report operation, safe to run repeatedly.

## Section 6: Lag Monitoring and Cutover

**Lag measurement:**

- **Byte lag:** Difference between MySQL's current binlog position and the last committed position in `pgferry_cdc_checkpoint`. Always available, cheap to compute.
- **Time since last apply:** `now() - last_applied` from the checkpoint table. Indicates how recently data was applied.

True event-time lag (comparing binlog event timestamps to wall clock) is a future enhancement.

**Status output during `replicate` (every 10 seconds):**

```
[replicate] binlog=mysql-bin.000042:83927104 | applied=142,857 | skipped=0 | lag=~2.3MB | last_applied=2s ago
```

**Cutover output (with `--wait`):**

```
[cutover] Waiting for replication lag to reach zero...
[cutover] lag=~48KB, last_applied=1s ago
[cutover] lag=~12KB, last_applied=0s ago
[cutover] lag=0, all events applied
[cutover] Cutover ready. Source and target are in sync.
[cutover]   Binlog position: mysql-bin.000042:83929472
[cutover]   Events applied: 143,012
[cutover]   Events skipped: 0
[cutover]
[cutover] Next steps:
[cutover]   1. Stop writes to MySQL
[cutover]   2. Run 'pgferry cutover' again to confirm zero lag
[cutover]   3. Point your application to PostgreSQL
```

## Section 7: Testing Strategy

**Unit tests (no DB required):**

- Event parsing: raw binlog event fixtures -> assert correct table name, operation, row data, position extraction
- Upsert SQL generation: given schema + row data -> assert correct SQL and parameter binding (single PK, composite PK, various types)
- Batch flushing: assert flush on size threshold, time threshold, and explicit flush with mocked apply
- Type transformation: verify binlog values go through same `TransformValue()` path as initial load
- Config validation: assert `mode = "cdc"` rejects non-MySQL sources, rejects `schema_only`, forces `single_tx`

**Integration tests (`TestIntegration_MySQLCDC`, build tag `integration`):**

Uses existing `MYSQL_DSN` + `POSTGRES_DSN` env vars.

1. Seed MySQL with test schema (users, posts, comments)
2. Run `migrate` with `mode = "cdc"`
3. Assert `pgferry_cdc_checkpoint` table exists with valid binlog position
4. Make changes in MySQL: insert, update, delete rows
5. Start replicate pipeline programmatically (cancel context after catching up)
6. Assert all changes reflected in PostgreSQL
7. Assert checkpoint position advanced and event counters match

**What's deferred:** stress/performance testing, long-running soak tests, network failure/reconnection tests, DDL handling.

## New Dependency

- `github.com/go-mysql-org/go-mysql` — MySQL replication protocol / binlog parser. Well-maintained, widely used (used by TiDB's data migration tooling, among others).

## Documentation Updates

The following docs pages need updates:

- `site/src/content/docs/reference/configuration.md` — add `mode`, `cdc_batch_size`, `cdc_flush_interval`, `cdc_server_id` fields
- New guide: `site/src/content/docs/guides/mysql-cdc-cutover.md` — walkthrough of the three-command workflow with prerequisites (binlog_format=ROW, required privileges, GTID recommendation)
- `site/src/content/docs/reference/type-mapping.md` — note that CDC applies the same type mapping as the initial load
