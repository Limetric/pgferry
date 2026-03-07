# Hooks Example

Demonstrates all four hook phases with example SQL files.

## Hook phases

Hooks are SQL files executed at specific points during migration. All occurrences
of `{{schema}}` are replaced with the configured schema name at runtime.

| Phase | File | Runs when |
|---|---|---|
| `before_data` | `before_data.sql` | After table creation, before data COPY. Good for installing extensions or creating helper functions. |
| `after_data` | `after_data.sql` | After data COPY, before post-migration constraints. Good for ANALYZE or data transforms. |
| `before_fk` | `before_fk.sql` | After PKs and indexes, before FK creation. Good for orphan cleanup. |
| `after_all` | `after_all.sql` | After FKs, sequences, and triggers. Good for views, materialized views, or validation queries. |

## Files

- **`before_data.sql`** — Installs the `pgcrypto` extension.
- **`after_data.sql`** — Runs `ANALYZE` on the schema for fresh planner stats.
- **`before_fk.sql`** — Placeholder for orphan cleanup statements.
- **`after_all.sql`** — Placeholder for views and validation queries.

## Usage

```bash
pgferry examples/mysql/hooks/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.
