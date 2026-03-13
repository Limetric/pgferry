---
title: Minimal-safe
description: Conservative MySQL-to-PostgreSQL example for first production rehearsals.
---

## When to use it

- first real MySQL rehearsal
- production target should not be dropped or rebuilt
- you want orphan cleanup to stay explicit

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "error"
source_snapshot_mode = "none"
unlogged_tables = false
clean_orphans = false
preserve_defaults = true
add_unsigned_checks = false
replicate_on_update_current_timestamp = false

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

[hooks]
before_data = []
after_data = []
before_fk = []
after_all = []
```

## Run it

```bash
pgferry plan migration.toml
pgferry migrate migration.toml
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/minimal-safe/migration.toml)
