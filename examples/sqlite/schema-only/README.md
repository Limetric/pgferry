# SQLite Schema-Only Migration

Creates the PostgreSQL schema from a SQLite file without copying any row data.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `source.type` | `sqlite` | Uses SQLite introspection and read-only file access |
| `schema_only` | `true` | Creates tables and post-migration objects but skips data COPY |
| `on_schema_exists` | `recreate` | Drops and recreates the schema for a clean start |

## SQLite notes

- `source_snapshot_mode` must stay `none`.
- SQLite sources always run with a single worker.

## When to use

- You want to validate the generated PostgreSQL schema before committing to a full data migration.
- You plan to load data separately later.
- You are iterating on type mappings or hooks and do not want to wait for data transfer.

## Usage

```bash
pgferry -config examples/sqlite/schema-only/migration.toml
```

Edit the `[source]` SQLite file path and `[target]` PostgreSQL DSN before running.
