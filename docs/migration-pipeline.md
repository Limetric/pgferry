# Migration pipeline

pgferry runs a fixed sequence of steps. Some steps are skipped depending on the
migration mode (`schema_only` or `data_only`).

## Pipeline steps

| # | Step | `full` | `schema_only` | `data_only` |
|---|---|---|---|---|
| 1 | **Introspect** &mdash; query MySQL `INFORMATION_SCHEMA` for tables, columns, indexes, FKs. Report views, routines, triggers that need manual migration. Detect unsupported index types and generated columns. Abort if unsupported column types are found. | Yes | Yes | Yes |
| 2 | **Create tables** &mdash; columns only, no constraints. Optionally `UNLOGGED` for faster writes. Column defaults included only when `preserve_defaults = true`. | Yes | Yes | &mdash; |
| 3 | **`before_data` hooks** | Yes | &mdash; | Yes |
| 4 | **Stream data** &mdash; parallel `COPY` workers per table (or sequential inside a single MySQL transaction when `source_snapshot_mode = "single_tx"`). In `data_only` mode, triggers are disabled before COPY and re-enabled after. | Yes | &mdash; | Yes |
| 5 | **`after_data` hooks** | Yes | &mdash; | Yes |
| 6 | **SET LOGGED** &mdash; convert `UNLOGGED` tables back to `LOGGED` | Yes | &mdash; | &mdash; |
| 7 | **Primary keys** | Yes | Yes | &mdash; |
| 8 | **Indexes** &mdash; unsupported index types (FULLTEXT, SPATIAL, prefix, expression) are reported and skipped | Yes | Yes | &mdash; |
| 9 | **`before_fk` hooks** | Yes | Yes | &mdash; |
| 10 | **Orphan cleanup** &mdash; auto-detect and remove/nullify rows that would violate FK constraints | Yes | &mdash; | &mdash; |
| 11 | **Foreign keys** | Yes | Yes | &mdash; |
| 12 | **Sequences** &mdash; create auto-increment sequences and set to `max(col) + 1` | Yes | Yes | Yes |
| 13 | **Unsigned checks** &mdash; add CHECK constraints for unsigned ranges (when `add_unsigned_checks = true`) | Yes | Yes | &mdash; |
| 14 | **Triggers** &mdash; `ON UPDATE CURRENT_TIMESTAMP` emulation (when `replicate_on_update_current_timestamp = true`) | Yes | Yes | &mdash; |
| 15 | **`after_all` hooks** | Yes | Yes | Yes |

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
from MySQL.

### `none` (default)

Each table is read in its own MySQL connection using parallel workers. This is
the fastest option but does not guarantee a consistent snapshot across tables &mdash;
if the source database is being written to during migration, different tables may
reflect different points in time.

```toml
source_snapshot_mode = "none"
workers = 4
```

### `single_tx`

All table reads happen inside a single read-only MySQL transaction with
`REPEATABLE READ` isolation. This guarantees a consistent snapshot across all
tables, but reads are sequential (one table at a time) regardless of the
`workers` setting.

```toml
source_snapshot_mode = "single_tx"
```

Use `single_tx` when your source database has active writes and you need
referential consistency in the migrated data.
