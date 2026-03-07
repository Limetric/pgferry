# Sakila Sample Database Migration

A real-world example migrating the [MySQL Sakila sample database](https://dev.mysql.com/doc/sakila/en/)
to PostgreSQL.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `on_schema_exists` | `error` | Fail if the schema already exists |
| `clean_orphans` | `true` | Automatically remove orphaned rows before FK creation |
| `workers` | `4` | Parallel COPY workers |

## Hook files

- **`cleanup.sql`** (`before_fk`) — NULLs out dangling optional references and
  deletes orphaned child rows so FK constraints can be created cleanly.
- **`post.sql`** (`after_all`) — Creates a `film_list` convenience view and runs
  `ANALYZE` on all tables for fresh planner statistics.

## Prerequisites

- MySQL with the Sakila sample database loaded ([installation guide](https://dev.mysql.com/doc/sakila/en/sakila-installation.html)).
- A target PostgreSQL database.

## Usage

```bash
pgferry examples/mysql/sakila/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.
