# MSSQL Recreate Fast Migration

Optimized for repeatable dev/staging loads from MSSQL. Drops and recreates the
PostgreSQL schema each run and uses UNLOGGED tables to skip WAL overhead during
bulk copy.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `source.type` | `mssql` | Uses MSSQL introspection via `sys.*` catalog views |
| `source.source_schema` | `dbo` | Limits introspection to the source schema you want to migrate |
| `on_schema_exists` | `recreate` | Drops the schema and starts fresh every run |
| `unlogged_tables` | `true` | Skips WAL writes during COPY for faster loads |
| `clean_orphans` | `true` | Automatically deletes orphaned child rows before FK creation |
| `workers` | `8` | Increases parallelism for data copy |

## When to use

- Repeatable loads into a dev or staging PostgreSQL database.
- You want the convenience of recreating the target schema on each run.
- You want the fastest MSSQL-to-PostgreSQL path and can re-run if something goes wrong.

## Usage

```bash
pgferry examples/mssql/recreate-fast/migration.toml
```

Edit the `[source]` and `[target]` DSNs, plus `source_schema` if needed, before running.
