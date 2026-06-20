---
title: Advanced Options
description: Go beyond a basic pgferry migrate with hooks, resume, snapshot mode, type mapping, validation, table and column filtering, and renames.
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

If you need to debug a partial run without reading raw JSON by hand:

```bash
pgferry checkpoint status migration.toml
```

That command stays read-only. It loads the same config-relative checkpoint file pgferry would use for resume, then prints per-table chunk progress, copied-row totals, and any stored compatibility metadata. If no checkpoint exists yet, it prints a clear "no checkpoint" message instead of failing.

- [When resume is worth it](/operations/when-resume-is-worth-it/)
- [When unlogged tables are safe](/operations/when-unlogged-tables-are-safe/)

## Plan and preflight

Before you migrate anything, `pgferry plan` tells you what will need manual attention: views, routines, generated columns, skipped indexes, collation warnings, copy-risk hints (when `copy_risk_analysis` is on), and required extensions like `citext` or PostGIS. With `copy_risk_analysis` enabled, it can also print a **copy-phase-only** ETA range (low confidence) to help schedule the data-load window — it does **not** cover hooks, validation, indexes, foreign keys, or other post-copy steps; see [how to read plan output](/operations/how-to-read-plan-output/).

When `copy_risk_analysis` is enabled, COUNT/MIN/MAX probes can take a while on large schemas. Both `pgferry plan` and the advisory copy-risk pass at `pgferry migrate` startup log periodic progress (start, time-based “still probing” lines with the current table and stage, and a completion summary) so long runs do not look hung.

```bash
pgferry plan migration.toml
pgferry plan migration.toml --output-dir hooks
pgferry plan migration.toml --format json > plan.json
pgferry plan migration.toml --format markdown > plan.md
pgferry plan --input plan.json --format text
```

With `--output-dir`, pgferry writes skeleton hook files you can fill in before the real run. When the source catalog exposes object definitions, those hook files can also include commented source SQL for views, routines, or triggers. Treat that output as sensitive schema or business-logic material.

**Machine-readable and CI:** `--format json` is for diffing, dashboards, or filing tickets. `--format markdown` pastes nicely into a PR comment or a wiki page. `--fail-on errors` exits non-zero when there are unsupported column types; `--fail-on warnings` also fails on high-severity copy-risk findings. Your CI pipeline will finally care about migrations.

**Replay without the source:** `--input saved.json` renders a previous JSON report (text or JSON) without connecting to the database. You cannot combine `--input` with a TOML path or `--config` — pick one story.

If you want to start with a smaller slice, scope it to specific tables:

```toml
include_tables = ["orders", "order_items"]
exclude_tables = ["audit_log"]
```

Table filtering uses source table names, not the transformed PostgreSQL names.

If you want prefix-style or wildcard matching, opt in explicitly:

```toml
table_filter_mode = "glob"
include_tables = ["app_*", "audit_?"]
exclude_tables = ["app_tmp_*"]
```

In glob mode, matching is case-insensitive and supports only `*` and `?`. `exclude_tables` is applied after `include_tables`, so excludes still win.

To skip source columns that PostgreSQL should not receive, use `exclude_columns`:

```toml
exclude_columns = ["RowVersion"]
```

Column filtering uses source column names. A bare name applies to every table. Scope a rule to one source table with `TableName.ColumnName`; use `column_filter_mode = "glob"` for `*` and `?` patterns in either the table or column part:

```toml
column_filter_mode = "glob"
exclude_columns = ["RowVersion", "audit_*.sys_*"]
```

When a column is excluded, pgferry omits it from schema creation and data COPY. Primary keys, plain-column indexes, and foreign keys that reference excluded columns are skipped. pgferry cannot inspect arbitrary expression index definitions for excluded column references; expression indexes are already reported as unsupported and skipped separately.

To keep source objects but choose their target PostgreSQL names explicitly, use `table_renames` and `column_renames`:

```toml
[table_renames]
KP_SUMINA = "reserve_summary"

[column_renames]
"KP_SUMINA.% ставка резерва по категории качества" = "reserve_rate_quality"
"KP_SUMINA.% ставка резерва по категории качества КД" = "reserve_rate_quality_kd"
```

Rename keys use source table names and source `TableName.ColumnName` values after table and column filters. Rename values are final PostgreSQL table or column names, so pgferry does not apply `identifier_case` to them, and they must fit PostgreSQL's 63-byte identifier limit. This is useful when long source object names would otherwise collide after PostgreSQL's identifier limit.

If many long tables or columns collide only because PostgreSQL truncates identifiers to 63 bytes, opt into deterministic automatic names:

```toml
table_collision_mode = "auto"
column_collision_mode = "auto"
```

Explicit `table_renames` and `column_renames` still take precedence. Automatic renames are limited to truncation-only table collisions within the target schema and truncation-only column collisions within a table; exact generated-name collisions and other object collisions still stop with an error so you can choose the target names deliberately.

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
- [MariaDB guide](/guides/mariadb/)
- [MSSQL guide](/guides/mssql/)

## Validation

Trust, but verify. pgferry can compare source and target after the load to make sure everything arrived.

- `validation = "row_count"` — fast per-table count comparison. Good enough for most runs.
- `validation = "sampled_hash"` — checks counts plus a bounded deterministic content sample on primary-key-addressable rows. Stronger, but still not a full proof of correctness.

During `pgferry migrate`, validation runs after `after_data` hooks and re-reads the current source state, not the earlier COPY snapshot. If the source is live, keep that in mind.

If you want to rerun the same built-in validation later without touching schema or data load again:

```bash
pgferry validate migration.toml
```

That command uses the existing TOML config, connects to the source and target, introspects the selected tables again, and runs the configured validation mode only. It does not rerun hooks, COPY, checkpoints, or post-load DDL.

## Scripting migrate runs

If something is consuming pgferry's output — a deploy script, a CI step, a status dashboard — `--log-format json` gives you one JSON object on stdout when the run finishes. Human progress and error messages stay on stderr, out of the way.

```bash
pgferry migrate migration.toml --log-format json
pgferry migrate migration.toml --log-format json | jq '.success'
```

The JSON fields: `version`, `duration_ms`, `mode`, `validation`, `success`, `error` and `stage` (on failure), `tables_migrated` (omitted when zero).

For large chunked tables, default progress includes each chunk start and finish. Use table-level logging when you want a readable operator stream without external filters:

```bash
pgferry migrate migration.toml --quiet
pgferry migrate migration.toml --log-level table
```

`--log-level verbose` keeps the default per-chunk progress. `--log-level table` logs one row-copy start/done pair per table. `--log-level schema` suppresses row-copy detail while leaving warnings, errors, and stage-level messages visible. Do not combine `--quiet` with an explicit `--log-level`; choose one form.

## Config path inspection

Hooks and checkpoints live relative to your config file, which can get confusing when scripts move things around. `pgferry config paths` shows you exactly where pgferry will look — including whether each hook file actually exists on disk.

```bash
pgferry config paths migration.toml
pgferry config paths migration.toml --json
```

No database connection required. It loads and validates the config, then prints the resolved absolute paths for the config file, config directory, checkpoint file, and every hook SQL path.

## Secret injection via environment

For CI/CD or shared configs, you can keep DSNs out of the TOML and inject them at runtime:

```bash
export PGFERRY_SOURCE_DSN='root:root@tcp(127.0.0.1:3306)/source_db'
export PGFERRY_TARGET_DSN='postgres://user:pass@host:5432/dbname?sslmode=disable'
pgferry plan migration.toml
```

Non-empty `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN` override `source.dsn` and `target.dsn` after the TOML is loaded. Empty values are ignored.

## Guides

Every source engine has its own quirks. These guides cover what pgferry handles automatically and what you should watch out for.

- [MySQL to PostgreSQL](/guides/mysql/)
- [MariaDB to PostgreSQL](/guides/mariadb/)
- [SQLite to PostgreSQL](/guides/sqlite/)
- [MSSQL to PostgreSQL](/guides/mssql/)

## Going to production

When it's time for the real thing:

- [First production migration checklist](/operations/first-production-migration-checklist/)
- [Cutover checklist](/operations/cutover-checklist/)
- [Common failures and recovery](/operations/common-failures-and-recovery/)

## Build info

`pgferry version` prints the version. `--verbose` adds the full commit hash, build date (if embedded at link time), and Go runtime version — handy for bug reports and support tickets.

```bash
pgferry version
pgferry version --verbose
```

## Full reference

When you want every config key, every type mapping rule, and every pipeline stage documented:

- [Configuration reference](/reference/configuration/)
- [Type mapping reference](/reference/type-mapping/)
- [Migration pipeline](/reference/migration-pipeline/)
- [Conventions and limitations](/reference/conventions-and-limitations/)
