---
title: Minimal-safe
description: Conservative SQLite-to-PostgreSQL example for first real migrations.
---

## When to use it

- first migration from a real SQLite file
- you want durable target writes
- you want the simplest safe starting point

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "error"
source_snapshot_mode = "none"
unlogged_tables = false
clean_orphans = false
preserve_defaults = true

[source]
type = "sqlite"
dsn = "./source.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"

[type_mapping]
json_as_jsonb = true
sanitize_json_null_bytes = true
unknown_as_text = false
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/minimal-safe/migration.toml)
