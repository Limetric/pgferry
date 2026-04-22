---
title: Conventions & Limitations
description: Naming rules, generated-column behavior, unsupported objects, chunking limits, checkpoints, and source-specific caveats.
---

pgferry tries to be explicit about where it is opinionated and where it deliberately stops short of guessing.

## Naming

The `identifier_case` option controls how source identifiers map to PostgreSQL names:

- `identifier_case = "snake"` (default) converts to PostgreSQL-style `snake_case`.
- `identifier_case = "lower"` lowercases only — no word-boundary insertion.
- `identifier_case = "preserve"` keeps the original source casing unchanged.

Generated PostgreSQL DDL always uses quoted identifiers, so mixed-case names produced by `"preserve"` are safe to use in downstream SQL and tooling (referenced as `"SomeProducts"` rather than `someproducts`).

Examples:

| Source identifier | `"snake"` | `"lower"` | `"preserve"` |
| --- | --- | --- | --- |
| `parentUserId` | `parent_user_id` | `parentuserid` | `parentUserId` |
| `UserName` | `user_name` | `username` | `UserName` |
| `SomeProducts` | `some_products` | `someproducts` | `SomeProducts` |
| `HTMLParser` | `html_parser` | `htmlparser` | `HTMLParser` |

PostgreSQL SQL is emitted as `"app"."users"` rather than `app.users` under all three modes.

## Auto-increment and sequences

MySQL and MariaDB `auto_increment`, SQLite integer primary key auto-increment behavior, and MSSQL `IDENTITY` columns are recreated as PostgreSQL sequences after data load.

The sequence flow is:

1. Create the sequence after the bulk load.
2. Set it to `max(column) + 1`.
3. Attach it as the column default.

## Orphan cleanup

When `clean_orphans = true`, pgferry checks foreign-key relationships before adding PostgreSQL FKs.

Cleanup behavior:

- `ON DELETE SET NULL` foreign keys are repaired by setting the child columns to `NULL`.
- Other delete rules cause the orphaned child rows to be deleted.

`clean_orphans_mode = "apply"` (the default) actually does the work. `"report"` only inspects and logs — handy when you want a preview without touching rows, but the migration stops before FK creation so you can react first.

Set `clean_orphans_max_rows` to a positive number when you want a fuse: if the total orphan rows across all FKs would exceed that cap, pgferry aborts instead of deleting half the internet. `0` means no fuse.

Disable orphan cleanup entirely when you want FK creation to fail naturally so you can inspect the data problem yourself or fix it with `before_fk` hooks.

## Generated columns

Generated columns are copied as their materialized values. pgferry does not recreate the source expression semantics automatically.

What you get:

- the copied data values
- warnings in `plan` output
- a clear signal that you may need an `after_data` or `after_all` follow-up step

## Reported semantic drift

`pgferry plan` also reports schema semantics that do not necessarily block the migration but still need review because pgferry skips or leaves them behind.

Current examples include:

- skipped source defaults that cannot be recreated automatically
- source `CHECK` constraints that are not recreated
- table and column comments / extended properties that are not migrated
- source partitioning metadata that is detected but not rebuilt in PostgreSQL

Treat these as cutover work items, not as noise. They are usually the difference between a load that completed and a schema that still behaves the way the source system did.

## Unsupported objects and features

pgferry reports these rather than silently faking them:

### Source objects

- views
- routines or procedures
- source triggers

These are not migrated automatically. Recreate them with hooks or separate DDL.

### Unsupported or skipped indexes

MySQL and MariaDB:

- `FULLTEXT`
- `SPATIAL` unless `[postgis].enabled = true`
- prefix indexes
- expression indexes

SQLite:

- partial indexes
- expression indexes

MSSQL:

- XML indexes
- spatial indexes
- filtered indexes

Unsupported indexes are logged as warnings so the migration can continue safely.

## Unsupported column types

Unsupported source column types are collected up front and the migration aborts before table creation starts.

If you intentionally want a softer landing, set:

```toml
[type_mapping]
unknown_as_text = true
```

## Chunking limits

Chunking only applies to tables with a single-column numeric primary key. Tables with composite, non-numeric, or missing primary keys fall back to full-table COPY.

Gaps in the numeric primary key range are fine. The chunk simply returns fewer rows.

## Checkpoint behavior

When `resume = true`, pgferry writes `pgferry_checkpoint.json` next to the config file.

Important details:

- writes are atomic
- progress flushes are batched
- a checkpoint is deleted automatically after a successful migration
- old or incompatible checkpoints are rejected instead of reused unsafely
- `pgferry checkpoint status migration.toml` prints the stored table/chunk progress and compatibility summary without connecting to any database

## Source-specific caveats

### MySQL and MariaDB

- `enum_mode` and `set_mode` control semantic handling of enums and sets.
- `zero_date_mode` controls how `0000-00-00` values are handled.
- MariaDB native `uuid` columns and JSON aliases are normalized onto PostgreSQL `uuid` and `json` / `jsonb` semantics during COPY.
- `[postgis]` enables native spatial migration only for MySQL. MariaDB should use `spatial_mode` fallback storage modes instead.
- `_ci` collations can be mapped to `citext` with `ci_as_citext = true`.

### SQLite

- SQLite always runs with one worker.
- `source_snapshot_mode = "single_tx"` is unsupported.
- In-memory SQLite databases are rejected.
- The source database is opened read-only.

### MSSQL

- `source_schema` defaults to `dbo`.
- `timestamp` and `rowversion` map to `bytea`, not PostgreSQL datetime types.
- `money` and `smallmoney` map to `numeric` by default.
- `single_tx` requires snapshot isolation on the source database.

## What pgferry does automatically vs what you still own

| pgferry handles | You still need to handle |
| --- | --- |
| schema introspection | application-specific cutover sequencing |
| table creation | views, routines, and source-trigger replacements |
| data COPY with chunking | unsupported index redesign when needed |
| PKs, indexes, FKs, sequences, optional trigger emulation | semantic recreation of generated-column expressions |
| extension checks for supported features | any validation beyond what you choose to script or configure |
