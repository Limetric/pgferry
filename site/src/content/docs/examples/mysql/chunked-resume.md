---
title: Chunked-resume
description: Resumable MySQL-to-PostgreSQL example for large tables and restart safety.
---

## When to use it

- large numeric-PK tables dominate runtime
- you want checkpoints and restart safety
- row-count validation matters

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "error"
unlogged_tables = false
workers = 8
chunk_size = 100000
resume = true
validation = "row_count"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

## Run it

```bash
pgferry plan migration.toml
pgferry migrate migration.toml
```

## Why `unlogged_tables` is off

Resume only makes sense when the target data survives crashes, so this pattern keeps tables logged on purpose.

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/chunked-resume/migration.toml)
