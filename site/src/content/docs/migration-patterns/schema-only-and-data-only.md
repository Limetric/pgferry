---
title: Schema-only And Data-only
description: Split the pgferry workflow when you need DDL review or tighter cutover sequencing.
---

Use the split-phase workflow when you need schema creation and data loading to happen at different times.

## `schema_only`

Use this when you want PostgreSQL tables, keys, and indexes created before any data moves.

## `data_only`

Use this when the target schema already exists and you only need to stream data plus reset sequences.

`data_only` is not a pure insert-only mode. pgferry preflights and then uses `ALTER TABLE ... DISABLE TRIGGER ALL` before COPY, followed by `ENABLE TRIGGER ALL` afterward. That means the PostgreSQL role must be allowed to control triggers on the target tables.

If that preflight fails, pgferry aborts before COPY starts so operators are not left guessing whether any data moved or whether trigger state changed permanently.

If your own schema migrator creates the target tables and inserts seed/reference data, use `truncate_before_copy = true` in the data-only config when those rows should be replaced by the source data. pgferry runs `TRUNCATE TABLE ... CASCADE` on the selected target tables after `before_data` hooks and before COPY.

## Tradeoff

This is slower than the default full pipeline because data loads into a schema that already has more objects in place.

It is also more privilege-sensitive than the full pipeline, especially on managed PostgreSQL services or environments with restricted application roles.

`truncate_before_copy = true` is incompatible with `resume = true`, because a resumed retry would truncate rows that the checkpoint may skip.

## Start from these examples

- [MySQL schema-only](/examples/mysql/schema-only/)
- [MySQL data-only](/examples/mysql/data-only/)
- [SQLite schema-only](/examples/sqlite/schema-only/)
- [SQLite data-only](/examples/sqlite/data-only/)
