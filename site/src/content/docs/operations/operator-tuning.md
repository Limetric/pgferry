---
title: Operator Tuning
description: Tune PostgreSQL, source pressure, and pgferry settings deliberately before large migrations.
---

Most slow migrations are bottlenecked by target PostgreSQL write behavior, post-load index builds, source read pressure, or network RTT long before Go code becomes the limiting factor.

Treat pgferry tuning as an operator problem first:

- decide whether you are optimizing for absolute speed, restartability, or a consistent live-source snapshot
- keep pgferry close to both the source and PostgreSQL when possible
- rehearse on data and infrastructure that resemble the real run

## Target PostgreSQL

### `synchronous_commit`

For a disposable or replayable maintenance window, session-level `synchronous_commit = off` can reduce WAL flush pressure during bulk load and post-load DDL.

Use it only if a PostgreSQL crash during the migration can cost you some recently acknowledged work and you are prepared to replay it. If exact crash recovery and restartability matter more than peak throughput, leave it on.

Official docs:

- [`synchronous_commit`](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-SYNCHRONOUS-COMMIT)

### Index build memory and parallelism

pgferry builds primary keys and secondary indexes after the data load. `index_workers` controls how many index builds pgferry allows at once. PostgreSQL still decides how much memory and parallel worker capacity each build gets.

The main PostgreSQL knobs are:

- [`maintenance_work_mem`](https://www.postgresql.org/docs/current/runtime-config-resource.html#GUC-MAINTENANCE-WORK-MEM) for index build memory
- [`max_parallel_maintenance_workers`](https://www.postgresql.org/docs/current/runtime-config-resource.html#GUC-MAX-PARALLEL-MAINTENANCE-WORKERS) for parallel index build workers

Raise them cautiously. Higher per-index memory and more concurrent index builds multiply together quickly.

### `work_mem`

`work_mem` is not usually the first knob for pgferry itself. Reach for it when your own hook SQL, validation queries, or follow-up application checks run large sorts or hashes.

Official docs:

- [`work_mem`](https://www.postgresql.org/docs/current/runtime-config-resource.html#GUC-WORK-MEM)

### WAL and checkpoints

Long COPY phases can be limited by WAL churn and aggressive checkpoints. If the target spends the whole migration checkpointing, look at PostgreSQL's WAL tuning rather than only lowering `workers`.

Start with:

- [`max_wal_size`](https://www.postgresql.org/docs/current/runtime-config-wal.html#GUC-MAX-WAL-SIZE)
- [WAL configuration](https://www.postgresql.org/docs/current/wal-configuration.html)

These are usually maintenance-window or server-level decisions, not pgferry config flags.

## Source Database Guidance

### MySQL and MariaDB

- Prefer a read replica for rehearsals when the production primary is sensitive to read pressure.
- Keep pgferry physically close to the source and target. Extra RTT hurts many small round trips more than operators expect.
- Watch `workers` on busy primaries. More parallel readers are not free if they churn the buffer pool or compete with application traffic.
- If the source stays live, decide whether you need `source_snapshot_mode = "single_tx"` for consistency before you start tuning throughput.

### MSSQL

- `source_snapshot_mode = "single_tx"` depends on snapshot isolation being available on the source database.
- If snapshot isolation is not enabled, keep `source_snapshot_mode = "none"` and accept that the source may change during the run.
- See [MSSQL to PostgreSQL](/guides/mssql/) for source-specific behavior and [How to choose snapshot mode](/operations/how-to-choose-snapshot-mode/) for the consistency tradeoff.

### SQLite

- SQLite sources stay effectively single-connection in pgferry, so `workers` does not increase source-side read parallelism.
- Put the source database on fast storage when possible.
- Tuning the target PostgreSQL server and avoiding unnecessary network distance still matter, but SQLite will not scale like MySQL, MariaDB, or MSSQL sources.

## pgferry Config Semantics

### `workers` and `index_workers`

`workers` affects the COPY phase. Increase it when the source, network, and target PostgreSQL server all have headroom. If one of those is saturated, a higher value just increases contention.

`index_workers` affects only the post-migration index creation phase. It is worth raising only when PostgreSQL still has spare CPU, I/O, and memory after the data load.

### `chunk_size`

`chunk_size` is key-range width for chunkable tables with a single-column numeric primary key. It is not a promise of rows per chunk.

Sparse IDs, deleted ranges, and skewed write patterns can make equal key ranges contain very different row counts.

Use smaller values when:

- restart granularity matters
- you want more frequent progress and checkpoint updates

Use larger values when:

- planning overhead is starting to matter
- the source key space is dense and you want fewer, larger units of work

### `unlogged_tables` and `resume`

`unlogged_tables = true` is the fastest disposable full-load path. It is not the durable path.

`resume = true` requires `unlogged_tables = false` because checkpointed progress only makes sense when target data survives crashes.

If the real risk is having to start over, prefer the slower durable path:

```toml
unlogged_tables = false
resume = true
```

For more on that tradeoff, see [When resume is worth it](/operations/when-resume-is-worth-it/) and [When unlogged tables are safe](/operations/when-unlogged-tables-are-safe/).

### `source_snapshot_mode`

`source_snapshot_mode = "none"` is the fastest path and preserves parallel copy behavior.

`source_snapshot_mode = "single_tx"` trades some throughput flexibility for one consistent source snapshot during the COPY phase. That is the right choice when the source stays live and cross-table consistency matters more than maximum parallelism.

Use [How to choose snapshot mode](/operations/how-to-choose-snapshot-mode/) for the short decision rule.

### `validation`

Both validation modes re-read the source after COPY:

- `validation = "row_count"` adds a cheap per-table cardinality check
- `validation = "sampled_hash"` adds row-count checks plus bounded content fingerprints for deterministic primary-key-addressable rows

That has two implications:

- validation adds extra source reads after the data load
- if the source keeps changing, validation can compare the target to a newer source state than the one COPY saw

Even with `source_snapshot_mode = "single_tx"`, validation does not read from the earlier COPY snapshot.

## Maintenance-Window Example

Treat this as a starting point to adapt, not a universal preset.

Apply equivalent PostgreSQL settings through the role, connection options, or environment that pgferry itself uses for the migration window. The session-level SQL shape looks like this:

```sql
SET synchronous_commit = off;
SET maintenance_work_mem = '1GB';
SET max_parallel_maintenance_workers = 4;
```

Match that with a pgferry config that makes the operational goal explicit:

```toml
schema = "app"
source_snapshot_mode = "single_tx"
unlogged_tables = false
resume = true
validation = "row_count"
workers = 8
index_workers = 4
chunk_size = 100000
```

This example chooses durable target tables, resumability, and a consistent COPY snapshot over the fastest disposable path.

## Pre-Run Checklist

- Decide whether the run is optimizing for speed, restart safety, or live-source consistency.
- Confirm pgferry is running close enough to the source and target that network RTT is not the hidden bottleneck.
- Make PostgreSQL tuning changes deliberately and document how to revert them after the migration window.
- Keep `resume = true` paired with `unlogged_tables = false`.
- Rehearse validation expectations before cutover so extra post-load source reads do not surprise you.
- Run `pgferry plan migration.toml` and assign every warning an owner before the real run.

If this is the first rehearsal that matters, continue with the [First production migration checklist](/operations/first-production-migration-checklist/).
