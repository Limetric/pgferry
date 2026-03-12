# MSSQL Minimal Safe Migration

A conservative starting template for first-time MSSQL to PostgreSQL migrations.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `source.type` | `mssql` | Uses MSSQL introspection via `sys.*` catalog views |
| `source.source_schema` | `dbo` | Limits introspection to the source schema you want to migrate |
| `on_schema_exists` | `error` | Prevents accidentally overwriting an existing schema |
| `clean_orphans` | `false` | No automatic deletion of orphaned rows |
| `unlogged_tables` | `false` | Tables are fully WAL-logged for crash safety |
| `preserve_defaults` | `true` | Column defaults are carried over to PostgreSQL |

## When to use

- You are migrating from MSSQL for the first time and want the safest defaults.
- The target PostgreSQL database does not yet contain the schema.
- You want to review data issues manually instead of cleaning orphaned rows automatically.

## Usage

```bash
pgferry examples/mssql/minimal-safe/migration.toml
```

Edit the `[source]` and `[target]` DSNs, plus `source_schema` if needed, before running.
