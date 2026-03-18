---
title: Sakila
description: Real MySQL Sakila sample database migration with cleanup and post-migration SQL hooks.
---

## Why this example matters

This is the most complete MySQL playbook in the repo: a real sample schema, a `before_fk` cleanup hook, and an `after_all` view plus `ANALYZE` pass.

## `migration.toml`

```toml
schema = "sakila"
on_schema_exists = "error"
source_snapshot_mode = "none"
unlogged_tables = false
clean_orphans = true
preserve_defaults = true
workers = 4

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/sakila"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/sakila"

[hooks]
before_data = []
after_data = []
before_fk = ["cleanup.sql"]
after_all = ["post.sql"]
```

## Cleanup hook excerpt

```sql
UPDATE {{schema}}.address
SET city_id = NULL
WHERE city_id IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM {{schema}}.city c
    WHERE c.city_id = {{schema}}.address.city_id
  );
```

## Post hook excerpt

```sql
CREATE OR REPLACE VIEW {{schema}}.film_list AS
SELECT f.film_id, f.title, l.name AS language
FROM {{schema}}.film f
LEFT JOIN {{schema}}.language l ON l.language_id = f.language_id;
```

Raw files:
[migration.toml](https://github.com/Limetric/pgferry/blob/main/examples/mysql/sakila/migration.toml),
[cleanup.sql](https://github.com/Limetric/pgferry/blob/main/examples/mysql/sakila/cleanup.sql),
[post.sql](https://github.com/Limetric/pgferry/blob/main/examples/mysql/sakila/post.sql)
