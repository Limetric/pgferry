---
title: Quick Start
description: Create a migration config and run your first pgferry migration.
sidebar:
  order: 2
---

The fastest correct first run is:

```bash
pgferry wizard
```

In an interactive terminal, plain `pgferry` also opens the wizard. It walks you through the source DSN, target DSN, target schema, migration mode, and the most important type-mapping options. At the end, you can save the generated `migration.toml`, run `plan`, start the migration immediately, or do both.

## Pick your source path

<div class="route-list">
	<a href="#mysql-to-postgresql">MySQL minimal config</a>
	<a href="#sqlite-to-postgresql">SQLite minimal config</a>
	<a href="#mssql-to-postgresql">MSSQL minimal config</a>
	<a href="/examples/mysql/minimal-safe/">MySQL example playbook</a>
	<a href="/examples/sqlite/minimal-safe/">SQLite example playbook</a>
	<a href="/examples/mssql/minimal-safe/">MSSQL example playbook</a>
</div>

Every migration starts with a TOML file. If you do not want to write that file by hand, use the wizard first and edit the generated config afterward.

## MySQL to PostgreSQL

```toml
schema = "app"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

## SQLite to PostgreSQL

```toml
schema = "app"

[source]
type = "sqlite"
dsn = "/path/to/database.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

## MSSQL to PostgreSQL

```toml
schema = "app"

[source]
type = "mssql"
dsn = "sqlserver://sa:YourStrong!Pass@127.0.0.1:1433?database=source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

## Run the migration

```bash
pgferry plan migration.toml
pgferry migrate migration.toml
```

The default pipeline is:

1. Load and validate config.
2. Introspect the source schema.
3. Create PostgreSQL tables.
4. Stream table data with `COPY`.
5. Add indexes, foreign keys, sequences, and optional triggers afterward.

## High-value defaults

- `snake_case_identifiers = true`
- `unlogged_tables = true`
- `preserve_defaults = true`
- `clean_orphans = true`
- `validation = "none"`
- `workers = min(runtime.NumCPU(), 8)`

These defaults bias toward fast full-load migrations while keeping the resulting PostgreSQL schema usable without much extra tuning.

## When to stop using the minimal config

Add more configuration when you need:

- `source_snapshot_mode = "single_tx"` for a consistent source snapshot on MySQL or MSSQL.
- `resume = true` plus `unlogged_tables = false` for chunk checkpoint reuse.
- `validation = "row_count"` for a post-load sanity check.
- Hook files for views, routines, cleanup SQL, or foreign-key sequencing.
- Source-specific type mapping, including PostGIS-backed MySQL spatial migration.

## Next step

Run [Plan and Validate](/get-started/plan-and-validate/) before pointing the tool at a real production schema.
