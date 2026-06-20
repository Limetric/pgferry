---
title: Schema-only
description: SQLite schema-only config that recreates PostgreSQL tables and DDL from a .db file without copying any rows — load data later separately.
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
