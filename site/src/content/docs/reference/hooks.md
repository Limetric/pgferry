---
title: Hooks
description: Hook phases, path resolution, schema templating, and practical examples for custom migration SQL.
---

Hooks let you run your own PostgreSQL SQL at four controlled points in the migration.

## Phases

| Phase | When it runs | Typical use |
| --- | --- | --- |
| `before_data` | After table creation, before data COPY | Create extensions, helper functions, or temporary settings |
| `after_data` | After data COPY, before validation and constraints | `ANALYZE`, data cleanup, normalization |
| `before_fk` | After PKs and indexes, before FK creation | Orphan cleanup, data fixes that must happen before foreign keys |
| `after_all` | After FKs, sequences, and optional triggers | Views, materialized views, validation queries, application-specific finishing work |

## Configuration

```toml
[hooks]
before_data = ["sql/extensions.sql"]
after_data = ["sql/analyze.sql"]
before_fk = ["sql/cleanup.sql"]
after_all = ["sql/views.sql", "sql/validate.sql"]
```

Files run in the order listed within each phase.

## Path resolution

Hook file paths are resolved relative to the directory containing `migration.toml`, not the current shell working directory.

Example:

```text
project/
  config/
    migration.toml
    sql/
      before_data.sql
```

If `migration.toml` references `sql/before_data.sql`, pgferry will find that file correctly no matter where you run the command from.

## `{{schema}}` templating

All occurrences of `{{schema}}` are replaced with the configured PostgreSQL schema name before execution.

```sql
ANALYZE {{schema}};
```

If `schema = "app"`, pgferry executes `ANALYZE app;`.

This is a plain string replacement, not a SQL parser, so keep quoted-schema edge cases in mind if you use unusual schema names.

## Statement splitting

Each hook file is split on `;` and executed statement by statement. pgferry handles:

- line comments with `--`
- block comments with `/* ... */`
- nested block comments
- quoted strings and quoted identifiers
- PostgreSQL dollar-quoted blocks such as `$$...$$` and `$tag$...$tag$`

## Example hooks

### `before_data.sql`

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

### `after_data.sql`

```sql
ANALYZE {{schema}};
```

### `before_fk.sql`

```sql
DELETE FROM {{schema}}.child c
WHERE c.parent_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1
    FROM {{schema}}.parent p
    WHERE p.id = c.parent_id
  );
```

### `after_all.sql`

```sql
CREATE OR REPLACE VIEW {{schema}}.active_users AS
SELECT id, email
FROM {{schema}}.users
WHERE deleted_at IS NULL;
```

## Phase availability by mode

| Phase | Full | `schema_only` | `data_only` |
| --- | --- | --- | --- |
| `before_data` | Yes | No | Yes |
| `after_data` | Yes | No | Yes |
| `before_fk` | Yes | Yes | No |
| `after_all` | Yes | Yes | Yes |

## When to prefer hooks

Use hooks when pgferry intentionally reports but does not recreate something automatically:

- views
- functions or procedures
- source triggers
- custom validation SQL
- data repairs specific to your application

Use [Migration Pipeline](/reference/migration-pipeline/) to see exactly where each phase lands in the full run.
