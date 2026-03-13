---
title: Chunked-resume
description: Resumable SQLite-to-PostgreSQL example for larger files and restart safety.
---

## When to use it

- one or two large SQLite tables dominate the run
- you want checkpoints and row-count validation
- restart safety matters more than raw speed

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "error"
unlogged_tables = false
chunk_size = 100000
resume = true
validation = "row_count"

[source]
type = "sqlite"
dsn = "/path/to/database.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

## SQLite note

SQLite still runs with one effective worker, but chunking keeps resume and progress tracking useful.

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/chunked-resume/migration.toml)
