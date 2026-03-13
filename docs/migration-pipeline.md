# Migration pipeline

pgferry runs a fixed sequence of steps. Some steps are skipped depending on the
migration mode (`schema_only` or `data_only`).

## Pipeline steps

| # | Step | `full` | `schema_only` | `data_only` |
|---|---|---|---|---|
| 1 | **Introspect** &mdash; query source database for tables, columns, indexes, FKs. Report views, routines, triggers that need manual migration. Detect unsupported index types and generated columns. Abort if unsupported column types are found. | Yes | Yes | Yes |
| 2 | **Extension validation** &mdash; verify extension-backed features (for example `citext` or opt-in PostGIS) before table creation. Create missing extensions only when the feature policy allows it. | Yes | Yes | Yes |
| 3 | **Create tables** &mdash; columns only, no constraints. Optionally `UNLOGGED` for faster writes. Column defaults included by default; set `preserve_defaults = false` to omit. | Yes | Yes | &mdash; |
| 4 | **`before_data` hooks** | Yes | &mdash; | Yes |
| 5 | **Stream data** &mdash; tables with a single-column numeric PK are split into range-based chunks; other tables use full-table COPY. Chunks/tables run in parallel (or sequentially with `source_snapshot_mode = "single_tx"`). SQLite always uses 1 worker. Checkpoint state is saved after each chunk for resumability. In `data_only` mode, triggers are disabled before COPY and re-enabled after. Opt-in PostGIS spatial columns stay on the COPY path and are converted to EWKB during streaming. | Yes | &mdash; | Yes |
| 6 | **`after_data` hooks** | Yes | &mdash; | Yes |
| 6b | **Validation** &mdash; compare source and target row counts per table (when `validation = "row_count"`). Fails the migration if any mismatch is found. | Yes | &mdash; | Yes |
| 7 | **SET LOGGED** &mdash; convert `UNLOGGED` tables back to `LOGGED` | Yes | &mdash; | &mdash; |
| 8 | **Primary keys** | Yes | Yes | &mdash; |
| 9 | **Indexes** &mdash; unsupported index types (MySQL FULLTEXT, prefix, expression; SQLite partial, expression) are reported and skipped. MySQL `SPATIAL` indexes are recreated as `USING GIST` when `[postgis].enabled = true`; otherwise they remain skipped. | Yes | Yes | &mdash; |
| 10 | **`before_fk` hooks** | Yes | Yes | &mdash; |
| 11 | **Orphan cleanup** &mdash; auto-detect and remove/nullify rows that would violate FK constraints (when `clean_orphans = true`) | Yes | &mdash; | &mdash; |
| 12 | **Foreign keys** | Yes | Yes | &mdash; |
| 13 | **Sequences** &mdash; create auto-increment sequences and set to `max(col) + 1` | Yes | Yes | Yes |
| 14 | **Unsigned checks** &mdash; add CHECK constraints for unsigned ranges (when `add_unsigned_checks = true`) | Yes | Yes | &mdash; |
| 15 | **Triggers** &mdash; `ON UPDATE CURRENT_TIMESTAMP` emulation (when `replicate_on_update_current_timestamp = true`) | Yes | Yes | &mdash; |
| 16 | **`after_all` hooks** | Yes | Yes | Yes |

## Modes

### Full (default)

Runs the entire pipeline: DDL, data, constraints, sequences.

```toml
# Both default to false — no need to set them
schema_only = false
data_only = false
```

### Schema only

Creates the PostgreSQL schema (tables, PKs, indexes, FKs, sequences, triggers)
without streaming any data. Useful for inspecting or modifying the schema before
loading data.

```toml
schema_only = true
```

Skips: `before_data` hooks, data COPY, `after_data` hooks, SET LOGGED, orphan cleanup.

### Data only

Streams data into an existing schema and resets sequences. Assumes tables and
constraints already exist from a prior `schema_only` run.

```toml
data_only = true
```

Skips: table creation, PKs, indexes, `before_fk` hooks, orphan cleanup, FKs,
unsigned checks, triggers. Triggers are disabled during COPY and re-enabled after.

## Two-phase workflow

You can split a migration into two phases for more control:

```bash
# Phase 1: create the full schema (tables + constraints + indexes)
pgferry schema-migration.toml   # schema_only = true

# Phase 2: stream data into the existing schema
pgferry data-migration.toml     # data_only = true
```

Note that in a two-phase workflow, data is streamed with indexes and foreign keys
already in place, which is slower than the default full pipeline (where constraints
are deferred until after the bulk load). Use the split workflow when you need to
inspect or modify the schema before loading data.

## Snapshot modes

The `source_snapshot_mode` setting controls read consistency when streaming data
from the source database.

### `none` (default)

Each table is read in its own connection using parallel workers. This is
the fastest option but does not guarantee a consistent snapshot across tables &mdash;
if the source database is being written to during migration, different tables may
reflect different points in time.

```toml
source_snapshot_mode = "none"
workers = 4
```

For SQLite sources, workers are automatically capped at 1 regardless of the setting,
since SQLite does not support concurrent readers across multiple connections in the
same way as MySQL.

### `single_tx` (MySQL only)

All table reads happen inside a single read-only MySQL transaction with
`REPEATABLE READ` isolation. This guarantees a consistent snapshot across all
tables, but reads are sequential (one table at a time) regardless of the
`workers` setting.

```toml
source_snapshot_mode = "single_tx"
```

Use `single_tx` when your source database has active writes and you need
referential consistency in the migrated data.

**Note:** `single_tx` is not supported for SQLite sources and produces a config
validation error.

## Chunked migration

pgferry automatically splits large tables into smaller range-based chunks for
improved performance and crash resilience. This happens transparently &mdash;
no special configuration is needed beyond the defaults.

### How it works

1. During the data migration phase, pgferry checks each table for a
   **single-column numeric primary key** (integer types).
2. If found, it queries `MIN(pk)` and `MAX(pk)` to determine the key range.
3. The range is divided into chunks of approximately `chunk_size` rows (default:
   100,000). Each chunk becomes a bounded `SELECT ... WHERE pk >= lower AND pk < upper`.
4. Chunks can run in parallel across multiple workers, just like full-table copies.

Tables without a chunkable primary key (composite PKs, non-numeric PKs like
UUID/VARCHAR, or no PK at all) fall back to the existing full-table `SELECT` +
`COPY` approach.

### Benefits

- **Large tables no longer dominate runtime** &mdash; multiple chunks of the same
  table can run in parallel.
- **Failures are cheaper** &mdash; only the failed chunk needs to be retried
  instead of the entire table.
- **Resume support** &mdash; completed chunks are checkpointed and skipped on rerun.

### Configuration

```toml
chunk_size = 100000   # rows per chunk (default)
```

### Interaction with snapshot mode

When `source_snapshot_mode = "single_tx"`, chunks run sequentially within the
snapshot transaction (no intra-table parallelism), but chunking still provides
resume capability and progress tracking.

## Resume

When `resume = true`, pgferry persists a checkpoint file
(`pgferry_checkpoint.json` in the config file directory) that tracks which
chunks and tables have been successfully copied.

If the migration is interrupted (crash, Ctrl+C, error), rerunning with
`resume = true` will skip completed work and continue from where it left off.

```toml
resume = true
```

### Checkpoint lifecycle

1. **On start:** if a checkpoint file exists, load it and skip completed chunks.
   Before any work is skipped, pgferry verifies that the checkpoint matches the
   current migration shape (chunking, identifier rules, relevant type mapping,
   hooks, and introspected table layout). If the checkpoint is incompatible,
   pgferry aborts and tells you to delete the checkpoint or restore the original
   config/schema. If no checkpoint exists, create a fresh one.
2. **During migration:** checkpoint state is tracked in memory and flushed to
   disk after every 10 completed items, or within 5 seconds of the next
   completed item (there is no background timer — flushes are evaluated when
   a chunk or table finishes). Each flush writes atomically (temp file +
   rename). On error, pending state is flushed to preserve partial progress
   for the next resume.
3. **On success:** the checkpoint file is deleted.

**Durability trade-off:** batched flushing means a crash can lose up to 10
chunks of progress (or up to 5 seconds worth), compared to at most 1 chunk
with per-item flushing. This is an acceptable trade-off for the significant
reduction in I/O overhead, especially on heavily chunked migrations.

When `resume = false` (the default), no checkpoint file is created or updated,
eliminating all checkpoint-related I/O from the data copy hot path.

### Constraints

- `resume = true` is incompatible with `on_schema_exists = "recreate"` (which
  would drop the schema containing data to resume into).
- `resume = true` is incompatible with `schema_only = true` (no data to resume).
- If the source data changes between runs, the resumed migration may produce
  inconsistent results. Ensure source stability during resumed migrations.
- Checkpoints from older pgferry versions are rejected for safe resume. Delete
  the checkpoint and rerun the migration from scratch if compatibility cannot be
  established.

## Post-load validation

pgferry can optionally verify the migration by comparing source and target row
counts after data streaming completes.

```toml
validation = "row_count"
```

### `row_count` mode

For each table, pgferry runs `SELECT COUNT(*)` on both the source and target
databases and compares the results. If any table has a mismatch, the migration
fails with a clear error listing the affected tables.

Validation runs after the `after_data` hooks and before post-migration steps
(SET LOGGED, PKs, indexes, FKs, etc.).

### `none` (default)

No validation is performed.
