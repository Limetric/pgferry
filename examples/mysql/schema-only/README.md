# Schema-Only Migration

Creates the PostgreSQL schema (tables, constraints, indexes, sequences) without
copying any row data.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `schema_only` | `true` | Creates tables and post-migration objects but skips data COPY |
| `on_schema_exists` | `recreate` | Drops and recreates the schema for a clean start |

## When to use

- You want to validate the generated PostgreSQL schema before committing to a full data migration.
- You plan to load data separately (e.g., via `data_only` mode or a custom ETL pipeline).
- You need a quick way to iterate on type mappings and hooks without waiting for data transfer.

## Usage

```bash
pgferry -config examples/mysql/schema-only/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.
