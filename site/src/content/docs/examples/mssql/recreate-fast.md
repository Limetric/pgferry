---
title: Recreate-fast
description: Fast disposable MSSQL-to-PostgreSQL example for repeatable dev or staging loads.
---

## When to use it

- target schema can be rebuilt each run
- you want a fast MSSQL reload loop
- the environment is disposable

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "recreate"
source_snapshot_mode = "none"
unlogged_tables = true
clean_orphans = true
preserve_defaults = true
add_unsigned_checks = false
workers = 8

[source]
type = "mssql"
dsn = "sqlserver://sa:YourStrong!Pass@127.0.0.1:1433?database=source_db"
source_schema = "dbo"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"

[type_mapping]
json_as_jsonb = true
nvarchar_as_text = false
money_as_numeric = true
xml_as_text = false
sanitize_json_null_bytes = true
unknown_as_text = false
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mssql/recreate-fast/migration.toml)
