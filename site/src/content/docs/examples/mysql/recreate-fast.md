---
title: Recreate-fast
description: Fast disposable MySQL-to-PostgreSQL example for repeatable dev or staging loads.
---

## When to use it

- target schema can be dropped every run
- you want the fastest repeatable path
- you accept rerunning from scratch after interruption

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "recreate"
source_snapshot_mode = "none"
unlogged_tables = true
clean_orphans = true
preserve_defaults = true
add_unsigned_checks = false
replicate_on_update_current_timestamp = false
workers = 8

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"

[type_mapping]
tinyint1_as_boolean = false
binary16_as_uuid = false
datetime_as_timestamptz = false
json_as_jsonb = true
enum_mode = "check"
set_mode = "text"
sanitize_json_null_bytes = true
unknown_as_text = false
```

## Run it

```bash
pgferry plan migration.toml
pgferry migrate migration.toml
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/recreate-fast/migration.toml)
