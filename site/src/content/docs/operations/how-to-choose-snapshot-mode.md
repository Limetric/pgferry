---
title: How To Choose Snapshot Mode
description: Choose between speed and consistency deliberately when the source stays live.
---

The rule is simple: use `none` unless the source is live enough that cross-table inconsistency matters.

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
- MSSQL: supported, but source snapshot isolation must be enabled
- SQLite: unsupported
