---
title: Recreate-fast
description: Fast disposable SQLite-to-PostgreSQL example for repeatable reloads.
---

## When to use it

- target schema is disposable
- you want a quick reload loop from a SQLite file
- rerunning from scratch is acceptable

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "recreate"
source_snapshot_mode = "none"
unlogged_tables = true
clean_orphans = true
preserve_defaults = true

[source]
type = "sqlite"
dsn = "./source.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/recreate-fast/migration.toml)
