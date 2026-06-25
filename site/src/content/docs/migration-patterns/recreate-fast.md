---
title: Recreate-fast
description: The fastest pgferry pattern for disposable dev and staging targets — UNLOGGED tables, schema recreate, and high worker parallelism.
---

Pick `recreate-fast` when the target is yours to drop and rebuild, and speed matters more than crash durability.

## What defines this pattern

- `on_schema_exists = "recreate"`
- `unlogged_tables = true`
- `clean_orphans = true`
- worker parallelism turned up for the data load

## Tradeoff

This is a great dev or staging loop, not the default first production path. `UNLOGGED` tables are truncated after crash recovery, so you must be comfortable rerunning the migration from scratch.

## Start from these examples

- [MySQL recreate-fast](/examples/mysql/recreate-fast/)
- [SQLite recreate-fast](/examples/sqlite/recreate-fast/)
- [MSSQL recreate-fast](/examples/mssql/recreate-fast/)
