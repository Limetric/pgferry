# Hooks

pgferry runs user-provided SQL files at four points during migration. Use hooks
to create extensions, run ANALYZE, clean up data, create views, or anything else
that needs to happen at a specific phase.

## Phases

| Phase | When it runs | Typical use |
|---|---|---|
| `before_data` | After table creation, before COPY | Create extensions, configure settings |
| `after_data` | After COPY, before constraints | ANALYZE, data transformations |
| `before_fk` | After PKs and indexes, before FK creation | Custom orphan cleanup, data fixes |
| `after_all` | After FKs, sequences, and triggers | Views, materialized views, validation |

## Configuration

```toml
[hooks]
before_data = ["extensions.sql"]
after_data  = ["analyze.sql"]
before_fk   = ["cleanup.sql"]
after_all   = ["views.sql", "validate.sql"]
```

Each phase accepts a list of SQL file paths. Files are executed in order within
each phase. Multiple files per phase are supported.

## Path resolution

File paths are resolved **relative to the TOML config file's directory**, not
the working directory. For example, given this layout:

```
project/
  config/
    migration.toml       # hooks reference "sql/before_data.sql"
    sql/
      before_data.sql
```

Running `pgferry project/config/migration.toml` from any directory will correctly
find `project/config/sql/before_data.sql`.

Absolute paths are also supported and used as-is.

## `{{schema}}` templating

All occurrences of `{{schema}}` in hook SQL files are replaced with the
configured schema name at runtime. This lets you write schema-agnostic hooks:

```sql
-- analyze.sql
ANALYZE {{schema}};
```

If `schema = "app"`, this executes as `ANALYZE app;`.

## Statement splitting

Each SQL file is split into individual statements on `;` semicolons and
executed one at a time. The splitter correctly handles:

- `--` line comments
- `/* ... */` block comments (including nested)
- `'...'` single-quoted string literals (with `''` escape)
- `"..."` double-quoted identifiers (with `""` escape)
- `$$...$$` and `$tag$...$tag$` dollar-quoted blocks (for PL/pgSQL functions)

A trailing statement without a final semicolon is included. Empty statements
are skipped.

## Example hook files

From [`examples/hooks/`](../examples/hooks/):

**`before_data.sql`** &mdash; create extensions before data load:
```sql
-- Runs after table creation and before any data COPY.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

**`after_data.sql`** &mdash; update statistics after COPY:
```sql
-- Runs after data COPY and before post-migration constraints.
ANALYZE {{schema}};
```

**`before_fk.sql`** &mdash; custom cleanup before FK creation:
```sql
-- Runs after PK/index creation and before FK creation.
-- Put orphan cleanup statements here if needed.
```

**`after_all.sql`** &mdash; post-migration views and validation:
```sql
-- Runs after FK, sequences, and triggers are created.
-- Put views/materialized views/validation queries here.
```

With this config:

```toml
schema = "app"

[hooks]
before_data = ["before_data.sql"]
after_data  = ["after_data.sql"]
before_fk   = ["before_fk.sql"]
after_all   = ["after_all.sql"]
```

## Phase availability by mode

Not all phases run in every migration mode:

| Phase | `full` | `schema_only` | `data_only` |
|---|---|---|---|
| `before_data` | Yes | &mdash; | Yes |
| `after_data` | Yes | &mdash; | Yes |
| `before_fk` | Yes | Yes | &mdash; |
| `after_all` | Yes | Yes | Yes |
