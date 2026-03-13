---
title: Minimal-safe
description: Conservative MSSQL-to-PostgreSQL example for first production rehearsals.
---

## When to use it

- first MSSQL rehearsal
- you want logged target writes
- you want to inspect integrity problems manually

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "error"
source_snapshot_mode = "none"
unlogged_tables = false
clean_orphans = false
preserve_defaults = true
add_unsigned_checks = false

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

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mssql/minimal-safe/migration.toml)
