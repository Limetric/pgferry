---
title: Quick Start
description: Start with the wizard, then run plan and migrate.
sidebar:
  order: 2
---

pgferry is meant to be easy to use for the first run. You should not need to memorize every config option before you can get moving.

The fastest path is simple: let the wizard generate the config, run `plan` to see what needs attention, then run `migrate`.

## Prerequisites

All you need are two connection strings: one for the source database you're migrating from, and one for the target PostgreSQL database you're migrating into.

**Source DSN** — depends on your source type:

| Source | Format | Example |
| --- | --- | --- |
| MySQL | `user:pass@tcp(host:port)/dbname` | `root:root@tcp(127.0.0.1:3306)/source_db` |
| MariaDB | `user:pass@tcp(host:port)/dbname` | `root:root@tcp(127.0.0.1:3306)/source_db` |
| SQLite | File path or `file:` URI | `/path/to/database.db` |
| MSSQL | `sqlserver://` URI | `sqlserver://sa:Pass@127.0.0.1:1433?database=source_db` |

**Target DSN** — always a PostgreSQL connection string:

```text
postgres://user:pass@host:port/dbname?sslmode=disable
```

Any PostgreSQL `sslmode` is supported. `sslmode=disable` is only shown here because it is a simple local-development example.

The wizard will prompt you for both of these, so you don't need to get the format perfect from memory.

If you prefer to keep secrets out of the TOML, set them at runtime instead:

```bash
export PGFERRY_SOURCE_DSN='root:root@tcp(127.0.0.1:3306)/source_db'
export PGFERRY_TARGET_DSN='postgres://user:pass@host:5432/dbname?sslmode=disable'
pgferry migrate migration.toml
```

`PGFERRY_SOURCE_DSN` overrides `source.dsn` and `PGFERRY_TARGET_DSN` overrides `target.dsn` when they are set to non-empty values.

## 1. Run the wizard

```bash
pgferry wizard
```

Same wizard, different spellings: `pgferry generate` and `pgferry init` do the same thing if your fingers prefer those words.

In an interactive terminal, plain `pgferry` also opens the wizard. It asks the useful questions, writes the config, and gets you out of the “blank TOML file staring contest” phase quickly.

Use the wizard to generate `migration.toml`.

If you want to stay on the simplest path, skip the optional advanced section and accept the defaults. If you already know you want built-in validation, resume support, or non-default `chunk_size` / `index_workers`, the wizard can now capture those too without sending you straight into manual TOML edits.

If you already know your source type and want a fuller starter instead, jump to:

- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [MariaDB minimal-safe example](/examples/mariadb/minimal-safe/)
- [SQLite minimal-safe example](/examples/sqlite/minimal-safe/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)

## 2. Run plan

```bash
pgferry plan migration.toml
```

`plan` is the part where pgferry tells you the truth before PostgreSQL gets involved. If there are views, routines, generated columns, skipped indexes, or required extensions, this is where you find out.

This step is technically optional — the same way riding your bike without checking the weather is technically optional. You can skip it, but don't be surprised when it rains.

## 3. Run migrate

```bash
pgferry migrate migration.toml
```

`pgferry run migration.toml` is identical — choose whichever verb feels right that day.

If you ran `plan` first, this step shouldn't have any surprises — you've already seen what's coming.

The migrate command runs the full pipeline:

1. introspect the source schema
2. create PostgreSQL tables
3. run before_data hooks (if configured)
4. stream data with `COPY` (chunked and parallelized where possible)
5. run after_data hooks and validation (if configured)
6. add primary keys, indexes, foreign keys, and sequences
7. run remaining hooks and finalize

## Re-run validation

If you want to re-check source vs target later without rerunning DDL or COPY:

```bash
pgferry validate migration.toml
```

That reuses the same `validation = "row_count"` or `validation = "sampled_hash"` setting from the TOML file and compares the current source state against the already-loaded PostgreSQL target.

## Next step

After the first run, move to:

- [Advanced Options](/get-started/advanced-options/) for hooks, resume, snapshot mode, type mapping, and more
- [Plan and Validate](/get-started/advanced-options/#plan-and-preflight) before pointing the tool at a real production schema
