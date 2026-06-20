---
title: Data-only
description: Minimal SQLite data-only config that copies rows from a .db file into pre-existing PostgreSQL tables when DDL is managed elsewhere.
---

## `migration.toml`

```toml
schema = "app"
data_only = true

[source]
type = "sqlite"
dsn = "./source.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/data-only/migration.toml)
