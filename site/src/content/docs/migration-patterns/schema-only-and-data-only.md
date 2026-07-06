---
title: Schema-only And Data-only
description: Split pgferry into schema_only and data_only runs for DDL review, tighter cutover sequencing, and truncate-before-copy into existing schemas.
---

Reach for the split-phase workflow when schema creation and data loading need to happen at different moments.

## `schema_only`

Use this when you want PostgreSQL tables, keys, and indexes created before any data moves.

## `data_only`

Use this when the target schema already exists and you only need to stream data plus advance existing sequences.

`data_only` is not a pure insert-only mode. pgferry preflights and then uses `ALTER TABLE ... DISABLE TRIGGER ALL` before COPY, followed by `ENABLE TRIGGER ALL` afterward. That means the PostgreSQL role must be allowed to control triggers on the target tables.

If that preflight fails, pgferry aborts before COPY starts so operators are not left guessing whether any data moved or whether trigger state changed permanently.

If your own schema migrator creates the target tables and inserts seed/reference data, use `truncate_before_copy = true` in the data-only config when those rows should be replaced by the source data. pgferry runs `TRUNCATE TABLE ...` on the selected target tables after `before_data` hooks and before COPY. It does not add `CASCADE`, so PostgreSQL will fail rather than silently truncating dependent tables outside the selected scope.

For a multi-schema cutover with cross-schema foreign keys, use `truncate_before_copy = "once"` only on the first schema config and set `truncate_before_copy_schemas` to the full batch, for example `["schema_a", "schema_b", "schema_c"]`. That makes pgferry run one pre-copy `TRUNCATE TABLE ... CASCADE` across all listed target schemas, then later per-schema configs should leave `truncate_before_copy = false`. pgferry fails before truncating if a listed schema is missing or has no ordinary target tables, which helps catch typos in the batch list.

After COPY, pgferry uses `setval` for auto-increment columns. It resolves the sequence from the target catalog rather than assuming a name: first the sequence actually attached to the column via `pg_get_serial_sequence` (identity columns and `OWNED BY` sequences, whatever an external migration tool named them), then a sequence named `{table}_{col}_seq` as created by a pgferry `schema_only` run. If neither exists, the run fails with an error naming the column rather than leaving the sequence behind the copied rows. pgferry does not create sequences or change column defaults in `data_only`; keep that DDL in the external schema migrator or the earlier `schema_only` run.

## Tradeoff

This is slower than the default full pipeline because data loads into a schema that already has more objects in place.

It is also more privilege-sensitive than the full pipeline, especially on managed PostgreSQL services or environments with restricted application roles.

`truncate_before_copy = true` and `truncate_before_copy = "once"` are incompatible with `resume = true`, because a resumed retry would truncate rows that the checkpoint may skip.

## Start from these examples

- [MySQL schema-only](/examples/mysql/schema-only/)
- [MySQL data-only](/examples/mysql/data-only/)
- [SQLite schema-only](/examples/sqlite/schema-only/)
- [SQLite data-only](/examples/sqlite/data-only/)
