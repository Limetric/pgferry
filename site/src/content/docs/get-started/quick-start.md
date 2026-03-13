---
title: Quick Start
description: Start with the wizard, then run plan and migrate.
sidebar:
  order: 2
---

pgferry is meant to be easy to use for the first run. You should not need to memorize every config option before you can get moving.

The fastest path is simple: let the wizard generate the config, run `plan` to see what needs attention, then run `migrate`.

## 1. Run the wizard

```bash
pgferry wizard
```

In an interactive terminal, plain `pgferry` also opens the wizard. It asks the useful questions, writes the config, and gets you out of the “blank TOML file staring contest” phase quickly.

Use the wizard to generate `migration.toml`.

If you already know your source type and want a fuller starter instead, jump to:

- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [SQLite minimal-safe example](/examples/sqlite/minimal-safe/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)

## 2. Run plan

```bash
pgferry plan migration.toml
```

`plan` is the part where pgferry tells you the truth before PostgreSQL gets involved. If there are views, routines, generated columns, skipped indexes, or required extensions, this is where you find out.

## 3. Run migrate

```bash
pgferry migrate migration.toml
```

That runs the actual migration:

1. introspect the source
2. create PostgreSQL tables
3. stream data with `COPY`
4. add keys, indexes, sequences, and other post-load objects

## Next step

After the first run, move to:

- [Choose Your Path](/get-started/choose-your-path/) if you need more operational control
- [Plan and Validate](/get-started/plan-and-validate/) before pointing the tool at a real production schema
