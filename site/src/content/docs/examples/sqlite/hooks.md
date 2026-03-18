---
title: Hooks
description: SQLite example showing all four PostgreSQL hook phases.
---

## `migration.toml`

```toml
schema = "app"
on_schema_exists = "error"
source_snapshot_mode = "none"
unlogged_tables = false
clean_orphans = false
preserve_defaults = true

[source]
type = "sqlite"
dsn = "./source.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"

[hooks]
before_data = ["before_data.sql"]
after_data = ["after_data.sql"]
before_fk = ["before_fk.sql"]
after_all = ["after_all.sql"]
```

## Hook SQL

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
```

```sql
ANALYZE {{schema}};
```

```sql
-- Put orphan cleanup statements here if needed.
```

```sql
-- Put views/materialized views/validation queries here.
```

Raw files:
[migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/hooks/migration.toml),
[before_data.sql](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/hooks/before_data.sql),
[after_data.sql](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/hooks/after_data.sql),
[before_fk.sql](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/hooks/before_fk.sql),
[after_all.sql](https://github.com/Limetric/pgferry/blob/main/examples/sqlite/hooks/after_all.sql)
