# SQLite Recreate Fast Migration

Optimized for repeatable dev/staging loads from a SQLite file. Drops and
recreates the PostgreSQL schema each run and uses UNLOGGED tables to skip WAL
overhead during bulk copy.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `source.type` | `sqlite` | Uses SQLite introspection and read-only file access |
| `on_schema_exists` | `recreate` | Drops the schema and starts fresh every run |
| `unlogged_tables` | `true` | Skips WAL writes during COPY for faster loads |
| `clean_orphans` | `true` | Automatically deletes orphaned child rows before FK creation |

## SQLite notes

- `source_snapshot_mode` must stay `none`.
- SQLite sources always run with a single worker.
- The source DSN must point to a real SQLite file; in-memory databases are not supported.

## When to use

- Repeatable loads into a dev or staging PostgreSQL database.
- You want the convenience of recreating the target schema on each run.
- You want the fastest SQLite-to-PostgreSQL path and can re-run if something goes wrong.

## Usage

```bash
pgferry -config examples/sqlite/recreate-fast/migration.toml
```

Edit the `[source]` SQLite file path and `[target]` PostgreSQL DSN before running.
