---
title: Schema-only
description: MySQL example for creating PostgreSQL schema objects without loading data.
---

## When to use it

- you want to inspect generated PostgreSQL DDL first
- you plan to load data later with `data_only`
- you are iterating on type mapping and hooks

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "recreate"
schema_only = true

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/schema-only/migration.toml)
