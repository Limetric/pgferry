# Data-Only Migration

Copies data without creating schema objects. The target tables must already
exist in PostgreSQL, and the target role must be allowed to disable and
re-enable triggers on them during the load.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `data_only` | `true` | Skips schema creation and most post-migration DDL, but still resets sequences after the load |
| `workers` | `8` | Parallel COPY workers for throughput |

## When to use

- The PostgreSQL schema was created manually or by another tool.
- You only need to backfill or refresh data from the source database.
- Schema objects (constraints, indexes, sequences) are already in place.

## PostgreSQL privilege note

Before COPY starts, pgferry preflights `ALTER TABLE ... DISABLE TRIGGER ALL` on
the selected target tables inside rollback-only transactions. If that preflight
fails, pgferry aborts before copying any data.

## Usage

```bash
pgferry examples/mysql/data-only/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.
