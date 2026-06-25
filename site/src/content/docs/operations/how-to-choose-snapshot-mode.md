---
title: How To Choose Snapshot Mode
description: Choose pgferry source_snapshot_mode none for speed or single_tx for one consistent snapshot when the source stays live during migration.
---

The rule of thumb is simple: use `none` unless the source is live enough that cross-table inconsistency would actually bite.

## Choose `none` when

- the source is static
- writes are already paused
- you want the fastest run
- per-table or per-chunk parallelism matters more than a single consistent snapshot

## Choose `single_tx` when

- the source stays live during the migration
- you need one point-in-time view across tables
- you accept slower sequential reads in exchange for consistency

## Source limits

- MySQL: supported
- MariaDB: supported
- MSSQL: supported; pgferry enables `ALLOW_SNAPSHOT_ISOLATION` only when it is not already on and the login may `ALTER DATABASE` (or it is already on without needing `ALTER`)
- SQLite: unsupported
