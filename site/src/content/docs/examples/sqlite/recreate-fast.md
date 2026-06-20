---
title: Recreate-fast
description: Fast disposable SQLite-to-PostgreSQL config that recreates the schema each run with unlogged tables for a quick reload loop from a .db file.
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

[type_mapping]
json_as_jsonb = true
sanitize_json_null_bytes = true
unknown_as_text = false

[hooks]
before_data = []
after_data = []
before_fk = []
after_all = []
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/recreate-fast/migration.toml)
