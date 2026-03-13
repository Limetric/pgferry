---
title: Recreate-fast
description: The fastest repeatable pgferry path for disposable targets.
---

Choose `recreate-fast` when the target can be dropped and rebuilt and you care more about speed than crash durability.

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
