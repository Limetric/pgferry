# SQLite Data-Only Migration

Copies data from a SQLite file without creating schema objects. The target
tables must already exist in PostgreSQL, and the target role must be allowed to
disable and re-enable triggers on them during the load.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `source.type` | `sqlite` | Uses SQLite introspection and read-only file access |
| `data_only` | `true` | Skips schema creation and most post-migration DDL, but still resets sequences after the load |

## SQLite notes

- SQLite sources always run with a single worker.
- The target schema and tables must already exist.

## When to use

- The PostgreSQL schema was created manually or by a prior `schema_only` run.
- You only need to backfill or refresh data from the SQLite source file.
- Constraints, indexes, and sequences are already in place.

## PostgreSQL privilege note

Before COPY starts, pgferry preflights `ALTER TABLE ... DISABLE TRIGGER ALL` on
the selected target tables inside rollback-only transactions. If that preflight
fails, pgferry aborts before copying any data.

## Usage

```bash
pgferry examples/sqlite/data-only/migration.toml
```

Edit the `[source]` SQLite file path and `[target]` PostgreSQL DSN before running.
