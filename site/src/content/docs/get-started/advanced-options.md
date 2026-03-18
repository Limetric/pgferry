---
title: Advanced Options
description: A tour of the features you'll reach for once the basic migrate works.
sidebar:
  order: 3
---

The quick start gets you from zero to migrated. This page is for when you come back and think "okay, but what if I need…"

Good news: pgferry has a knob for most of those situations. Here's the quick tour.

## Migration patterns

Not every migration looks the same. Pick the pattern that matches your situation — they're all one config file.

| Situation | Pattern | What it does |
| --- | --- | --- |
| First production rehearsal | [Minimal-safe](/migration-patterns/minimal-safe/) | Logged tables, no schema destruction, biased toward caution |
| Disposable dev/staging target | [Recreate-fast](/migration-patterns/recreate-fast/) | Drops and recreates the schema, unlogged tables, maximum speed |
| Large tables or unreliable connections | [Chunked-resume](/migration-patterns/chunked-resume/) | Checkpoint-based chunking so you don't restart from zero |
| Need schema review before data load | [Schema-only / data-only](/migration-patterns/schema-only-and-data-only/) | Split the DDL and data phases into separate runs |
| Views, routines, or custom follow-up SQL | [Hooks-driven](/migration-patterns/hooks-driven/) | SQL files that run at 4 phases of the pipeline |

## Snapshot mode

If the source database is live during the migration, you probably want a consistent read. Set `source_snapshot_mode = "single_tx"` and pgferry reads all tables inside a single read-only transaction — no phantom rows, no torn state.

The trade-off: `single_tx` runs sequentially instead of in parallel, so it's slower. If your source is quiet or you're migrating a dump, `none` (the default) is fine.

- [How to choose snapshot mode](/operations/how-to-choose-snapshot-mode/)

## Resume and checkpointing

Long migrations fail. Networks drop, disks fill, someone trips over a cable. When `resume = true`, pgferry writes a checkpoint after every chunk, and picks up where it left off on the next run.

One thing to keep in mind: pair `resume = true` with `unlogged_tables = false`. Unlogged tables don't survive a PostgreSQL crash, which would defeat the purpose of resuming.

- [When resume is worth it](/operations/when-resume-is-worth-it/)
- [When unlogged tables are safe](/operations/when-unlogged-tables-are-safe/)

## Plan and preflight

Before you migrate anything, `pgferry plan` tells you what will need manual attention: views, routines, generated columns, skipped indexes, collation warnings, and required extensions like `citext` or PostGIS.

```bash
pgferry plan migration.toml
pgferry plan migration.toml --output-dir hooks
```

With `--output-dir`, pgferry writes skeleton hook files you can fill in before the real run. If you want to start with a smaller slice, scope it to specific tables:

```toml
include_tables = ["orders", "order_items"]
exclude_tables = ["audit_log"]
```

Table filtering uses source table names, not the transformed PostgreSQL names.

- [How to read plan output](/operations/how-to-read-plan-output/)

## Hooks

pgferry intentionally doesn't try to recreate views, routines, or source triggers — those need human judgment. But it does give you four hook phases to run your own SQL at the right time: `before_data`, `after_data`, `before_fk`, and `after_all`.

`pgferry plan --output-dir` can generate the skeleton files for you. Fill in the blanks, drop them in, and they'll run automatically on the next migrate.

- [Handling unsupported objects with hooks](/operations/handling-unsupported-objects-with-hooks/)
- [Hooks reference](/reference/hooks/)

## Type mapping

pgferry maps source types to PostgreSQL types automatically, but sometimes you want to override the defaults. Want `tinyint(1)` as `boolean`? `binary(16)` as `uuid`? `nvarchar` as plain `text`? There's a config option for each.

- [Type mapping reference](/reference/type-mapping/)
- [MySQL guide](/guides/mysql/)
- [MSSQL guide](/guides/mssql/)

## Validation

Trust, but verify. pgferry can compare source and target after the load to make sure everything arrived.

- `validation = "row_count"` — fast per-table count comparison. Good enough for most runs.
- `validation = "sampled_hash"` — checks counts plus a bounded deterministic content sample on primary-key-addressable rows. Stronger, but still not a full proof of correctness.

Note that validation runs after `after_data` hooks and re-reads the current source state, not the earlier COPY snapshot. If the source is live, keep that in mind.

## Guides

Every source engine has its own quirks. These guides cover what pgferry handles automatically and what you should watch out for.

- [MySQL to PostgreSQL](/guides/mysql/)
- [SQLite to PostgreSQL](/guides/sqlite/)
- [MSSQL to PostgreSQL](/guides/mssql/)

## Going to production

When it's time for the real thing:

- [First production migration checklist](/operations/first-production-migration-checklist/)
- [Cutover checklist](/operations/cutover-checklist/)
- [Common failures and recovery](/operations/common-failures-and-recovery/)

## Full reference

When you want every config key, every type mapping rule, and every pipeline stage documented:

- [Configuration reference](/reference/configuration/)
- [Type mapping reference](/reference/type-mapping/)
- [Migration pipeline](/reference/migration-pipeline/)
- [Conventions and limitations](/reference/conventions-and-limitations/)
