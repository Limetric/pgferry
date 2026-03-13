---
title: Schema-only
description: SQLite example for creating PostgreSQL schema objects without copying data.
---

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "recreate"
schema_only = true

[source]
type = "sqlite"
dsn = "./source.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/schema-only/migration.toml)
