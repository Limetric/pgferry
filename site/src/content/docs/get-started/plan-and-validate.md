---
title: Plan and Validate
description: Inspect the source first, then migrate with validation instead of guesswork.
sidebar:
  order: 3
---

The safest pgferry workflow is not "point and pray." Run a plan first, fix what the report tells you, and only then move data.

## Generate a preflight report

```bash
pgferry plan migration.toml
pgferry plan migration.toml --output-dir hooks --format json
```

`plan` reports the parts of the migration that need manual attention:

- views, routines, and source triggers
- generated columns and unsupported expressions
- unsupported or skipped indexes
- collation warnings
- required PostgreSQL extensions such as `citext` or PostGIS

With `--output-dir`, pgferry also writes hook skeletons you can fill in before the main run.

## Use validation during the real run

```toml
unlogged_tables = false
validation = "row_count"
resume = true
chunk_size = 100000
```

These settings are a strong default for long-running operational migrations:

- `validation = "row_count"` checks source and target table counts after load.
- `resume = true` keeps progress in `pgferry_checkpoint.json`.
- `unlogged_tables = false` keeps checkpoints aligned with durable target data.
- `chunk_size` makes range-based retries cheaper on large tables.

## Snapshot strategy

Choose the source read mode deliberately:

- `source_snapshot_mode = "none"`: fastest and parallel, suitable when the source is static or writes are otherwise controlled.
- `source_snapshot_mode = "single_tx"`: uses one consistent read-only transaction for MySQL and MSSQL when you need a stable view of a live source database.

SQLite always uses `none`.

## Cutover checklist

Before the final cutover, verify:

1. `plan` output is understood and any hook SQL is written.
2. Row counts are acceptable for the tables you care about most.
3. Extension-backed features are installed or configured to auto-create.
4. Unsupported indexes or generated-column semantics have explicit follow-up notes.

## Next step

Use the [Reference](/reference/configuration/) pages to tune the migration for your source database and operational constraints.
