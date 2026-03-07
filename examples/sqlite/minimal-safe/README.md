# SQLite Minimal Safe Migration

A conservative starting template for migrating a SQLite file into PostgreSQL.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `source.type` | `sqlite` | Uses SQLite introspection and read-only file access |
| `on_schema_exists` | `error` | Prevents accidentally overwriting an existing schema |
| `clean_orphans` | `false` | No automatic deletion of orphaned rows |
| `unlogged_tables` | `false` | Tables are fully WAL-logged for crash safety |
| `preserve_defaults` | `true` | SQLite column defaults are carried over when possible |

## SQLite notes

- `source_snapshot_mode` must stay `none`.
- SQLite sources always run with a single worker.
- The source DSN must point to a real SQLite file; in-memory databases are not supported.

## When to use

- You want a ready-to-edit example for SQLite instead of adapting a MySQL template.
- You are migrating from a local `.db` or `.sqlite` file into PostgreSQL.
- You prefer the safest defaults for a first migration run.

## Usage

```bash
pgferry -config examples/sqlite/minimal-safe/migration.toml
```
Edit the `[source]` SQLite file path and `[target]` PostgreSQL DSN before running.
