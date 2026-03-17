# Recreate Fast Migration

Optimized for speed on repeatable dev/staging loads. Drops and recreates the
schema each run and uses UNLOGGED tables to skip WAL overhead during bulk copy.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `on_schema_exists` | `recreate` | Drops the schema and starts fresh every run |
| `unlogged_tables` | `true` | Skips WAL writes during COPY for faster loads |
| `clean_orphans` | `true` | Enables orphan cleanup before FK creation |
| `clean_orphans_mode` | `apply` | Applies DELETE / SET NULL cleanup after logging exact row counts |
| `workers` | `8` | Maximizes parallelism for data copy |

## When to use

- Repeatable loads into a dev or staging database.
- You don't mind losing the data if PostgreSQL crashes mid-migration (UNLOGGED tables are truncated on crash recovery).
- You want the fastest possible migration and can re-run if something goes wrong.

## Usage

```bash
pgferry examples/mysql/recreate-fast/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.

To inspect orphan cleanup impact before a production cutover, change:

```toml
clean_orphans_mode = "report"
clean_orphans_max_rows = 10000
```

That run will log the exact `DELETE` / `SET NULL` actions and affected row counts,
then stop before mutating data or creating foreign keys.
