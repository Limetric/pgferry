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

Every migration starts with a TOML file. If you do not want to write that file by hand, use the wizard first and edit the generated config afterward.

## The first-run flow

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

If you want a fuller starting point instead of this minimal config, jump to:

- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [SQLite minimal-safe example](/examples/sqlite/minimal-safe/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)

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

## Next step

After the first run, move to:

- [Choose Your Path](/get-started/choose-your-path/) if you need more operational control
- [Plan and Validate](/get-started/plan-and-validate/) before pointing the tool at a real production schema
