---
title: Data-only
description: MySQL example for loading rows into an existing PostgreSQL schema.
---

## When to use it

- target tables already exist
- you only need to backfill or refresh data
- schema creation is handled separately

## `migration.toml`

```toml
schema = "app"
data_only = true
workers = 8

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/data-only/migration.toml)
