---
title: Minimal-safe
description: Conservative MariaDB-to-PostgreSQL rehearsal config using single_tx snapshots, row-count validation, and spatial fallback as a safe baseline.
---

## When to use it

- you want an explicit MariaDB config instead of adapting a MySQL example
- the source is live enough that `single_tx` is worth the read overhead
- you want a safe baseline before adding chunking, resume, or hooks

## `migration.toml`

```toml
schema = "app"
source_snapshot_mode = "single_tx"
unlogged_tables = false
clean_orphans = true
validation = "row_count"

[source]
type = "mariadb"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"
charset = "utf8mb4"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"

[type_mapping]
json_as_jsonb = true
enum_mode = "check"
set_mode = "text"
time_mode = "time"
zero_date_mode = "null"
spatial_mode = "off"
```

## Notes

- `single_tx` gives you one consistent source snapshot for the run
- `validation = "row_count"` adds a cheap post-load sanity check
- `spatial_mode = "off"` keeps MariaDB spatial columns on fallback handling because `[postgis]` is not supported here

Raw files: [migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mariadb/minimal-safe/migration.toml)
