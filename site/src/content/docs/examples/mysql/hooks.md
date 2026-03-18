---
title: Hooks
description: MySQL example showing all four PostgreSQL hook phases.
---

## When to use it

- `plan` reports views, routines, or data fixes you need to own
- you want extension creation, `ANALYZE`, cleanup SQL, or final view creation in-band with the migration

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
before_data = ["before_data.sql"]
after_data = ["after_data.sql"]
before_fk = ["before_fk.sql"]
after_all = ["after_all.sql"]
```

## Hook SQL

```sql
-- before_data.sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

```sql
-- after_data.sql
ANALYZE {{schema}};
```

```sql
-- before_fk.sql
-- Put orphan cleanup statements here if needed.
```

```sql
-- after_all.sql
-- Put views/materialized views/validation queries here.
```

Raw files:
[migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/hooks/migration.toml),
[before_data.sql](https://github.com/Limetric/pgferry/blob/main/examples/mysql/hooks/before_data.sql),
[after_data.sql](https://github.com/Limetric/pgferry/blob/main/examples/mysql/hooks/after_data.sql),
[before_fk.sql](https://github.com/Limetric/pgferry/blob/main/examples/mysql/hooks/before_fk.sql),
[after_all.sql](https://github.com/Limetric/pgferry/blob/main/examples/mysql/hooks/after_all.sql)
