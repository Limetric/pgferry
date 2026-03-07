# Data-Only Migration

Copies data without creating or modifying schema objects. The target tables must
already exist in PostgreSQL.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `data_only` | `true` | Skips schema introspection, table creation, and post-migration (PKs, FKs, indexes, sequences) |
| `workers` | `8` | Parallel COPY workers for throughput |

## When to use

- The PostgreSQL schema was created manually or by another tool.
- You only need to backfill or refresh data from the source database.
- Schema objects (constraints, indexes, sequences) are already in place.

## Usage

```bash
pgferry examples/mysql/data-only/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.
