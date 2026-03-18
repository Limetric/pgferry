---
title: Migration Pipeline
description: Stage order, mode differences, chunking, resume behavior, validation timing, and snapshot strategy.
---

The default pgferry flow is built to load data first and add expensive constraints later.

## Stage order

| Stage | What happens | Full | `schema_only` | `data_only` |
| --- | --- | --- | --- | --- |
| 1 | Load and validate config | Yes | Yes | Yes |
| 2 | Introspect the source schema | Yes | Yes | Yes |
| 3 | Create target schema and tables | Yes | Yes | No |
| 4 | Run `before_data` hooks | Yes | No | Yes |
| 5 | Stream data with `COPY` | Yes | No | Yes |
| 6 | Run `after_data` hooks | Yes | No | Yes |
| 7 | Optional validation | Yes | No | Yes |
| 8 | `SET LOGGED` for full migrations using `UNLOGGED` tables | Yes | No | No |
| 9 | Add primary keys | Yes | Yes | No |
| 10 | Add indexes with bounded parallelism | Yes | Yes | No |
| 11 | Run `before_fk` hooks | Yes | Yes | No |
| 12 | Optional orphan cleanup | Yes | No | No |
| 13 | Add foreign keys | Yes | Yes | No |
| 14 | Reset sequences | Yes | Yes | Yes |
| 15 | Optional unsigned checks | Yes | Yes | No |
| 16 | Optional trigger emulation | Yes | Yes | No |
| 17 | Run `after_all` hooks | Yes | Yes | Yes |

## Modes

### Full migration

This is the default. pgferry creates tables, loads data, then adds constraints and post-load objects.

```toml
schema_only = false
data_only = false
```

Use this unless you have a specific operational reason to split schema creation and data load.

### `schema_only`

```toml
schema_only = true
```

Use this when you want to inspect or adjust the target schema before any data is loaded. It skips:

- `before_data`
- data COPY
- `after_data`
- row-count validation
- `SET LOGGED`
- orphan cleanup

### `data_only`

```toml
data_only = true
```

Use this when the target schema already exists from a prior `schema_only` run or from your own DDL. It skips:

- table creation
- primary keys and indexes
- `before_fk`
- orphan cleanup
- foreign keys
- unsigned checks
- trigger creation

## Two-phase workflow

```bash
pgferry migrate schema-migration.toml  # schema_only = true
pgferry migrate data-migration.toml    # data_only = true
```

Tradeoff: this is slower than the default full pipeline because data is loaded with indexes and foreign keys already present.

## Snapshot strategy

### `source_snapshot_mode = "none"`

- Fastest mode.
- Tables or chunks can run in parallel.
- Best when source writes are already paused or consistency across tables does not matter.

### `source_snapshot_mode = "single_tx"`

- Supported on MySQL and MSSQL.
- Reads the source inside one read-only transaction for a stable point-in-time view.
- Gives up parallel source reads for consistency.

Use `single_tx` when the source stays live during migration and the tables must agree with each other at one point in time.

## Chunking

pgferry chunks a table only when it has a single-column numeric primary key.

### Chunkable tables

- MySQL integer PKs such as `tinyint`, `smallint`, `mediumint`, `int`, `bigint`
- SQLite integer primary keys
- MSSQL numeric integer primary keys

### Not chunkable

- composite primary keys
- non-numeric primary keys such as UUID or text
- tables with no primary key

### What chunking changes

1. pgferry finds `MIN(pk)` and `MAX(pk)`.
2. It splits the range into chunks of roughly `chunk_size`.
3. Each chunk becomes a bounded `SELECT ... WHERE pk >= lower AND pk < upper`.
4. Completed chunks can be checkpointed individually when `resume = true`.

## Resume behavior

When `resume = true`, pgferry stores progress in `pgferry_checkpoint.json` next to the config file.

### What the checkpoint protects

- completed table copies
- completed chunk copies
- migration shape compatibility, including hooks, identifier rules, type mapping, and table layout

### What you need for safe resume

```toml
resume = true
unlogged_tables = false
```

Do not combine `resume = true` with `on_schema_exists = "recreate"` or `schema_only = true`.

## Validation timing

`validation = "row_count"` runs after `after_data` hooks and before post-load DDL like indexes and foreign keys.

This is intentional:

- table data is already present
- hook-driven cleanup or transforms can run first
- expensive post-load objects are not built yet if validation finds a mismatch

## Practical operating advice

- Use the full pipeline for most rehearsals and first production attempts.
- Split into `schema_only` and `data_only` only when you need manual schema review or a more controlled cutover sequence.
- Prefer `resume = true` for large runs where restarts are expensive.
- Prefer `validation = "row_count"` for the final rehearsals before cutover.
