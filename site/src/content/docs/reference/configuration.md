---
title: Configuration
description: Full TOML reference for pgferry migration configs, including defaults, safe starting points, and source-specific constraints.
---

Every migration is driven by a single TOML file:

```bash
pgferry migrate migration.toml
```

If you want the fastest safe starting point, use the wizard first:

```bash
pgferry wizard
```

## Minimal config

```toml
schema = "app"

[source]
type = "mysql" # or "sqlite" / "mssql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

## Recommended starting points

Use one of these before you start tuning smaller details.

### First production-style migration

```toml
schema = "app"
on_schema_exists = "error"
unlogged_tables = false
resume = true
validation = "row_count"
chunk_size = 100000
```

Why: this keeps target data durable, enables checkpoint reuse, and gives you a row-count sanity check before cutover.

### Fast disposable rehearsal

```toml
schema = "app"
on_schema_exists = "recreate"
unlogged_tables = true
clean_orphans = true
```

Why: this is the fastest full-load path when the target schema can be dropped and rebuilt.

## Full reference

### Top-level settings

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `schema` | string | required | Target PostgreSQL schema name. |
| `on_schema_exists` | string | `"error"` | `"error"` aborts if the schema exists. `"recreate"` drops and recreates it. |
| `schema_only` | bool | `false` | Create schema objects only. Skip data COPY. |
| `data_only` | bool | `false` | Load data into an existing schema, then reset sequences. |
| `source_snapshot_mode` | string | `"none"` | `"none"` is fastest. `"single_tx"` gives one consistent source snapshot on MySQL and MSSQL. |
| `snake_case_identifiers` | bool | `true` | Convert source names to `snake_case`. When false, pgferry lowercases only. |
| `unlogged_tables` | bool | `true` | Use `UNLOGGED` tables during full loads, then `SET LOGGED` later. |
| `preserve_defaults` | bool | `true` | Keep source column defaults in the created PostgreSQL schema. |
| `add_unsigned_checks` | bool | `false` | Add `CHECK` constraints for MySQL unsigned ranges. |
| `clean_orphans` | bool | `true` | Automatically delete or null invalid child rows before FK creation. |
| `replicate_on_update_current_timestamp` | bool | `false` | Create PostgreSQL trigger emulation for MySQL `ON UPDATE CURRENT_TIMESTAMP`. |
| `workers` | int | `min(runtime.NumCPU(), 8)` | Parallel worker count for data loading. SQLite is internally capped at 1. |
| `index_workers` | int | `workers` | Concurrent index builds during post-migration. |
| `chunk_size` | int | `100000` | Target rows per chunk for single-column numeric PK tables. |
| `resume` | bool | `false` | Reuse `pgferry_checkpoint.json` after interruptions. |
| `validation` | string | `"none"` | `"row_count"` compares source and target row counts after data load. |

### `[source]`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `type` | string | required | `"mysql"`, `"sqlite"`, or `"mssql"`. |
| `dsn` | string | required | Source connection string or SQLite file path/URI. |
| `charset` | string | `"utf8mb4"` | MySQL only. Injected into the DSN unless already present. |
| `source_schema` | string | `"dbo"` | MSSQL only. Limits introspection to one source schema. |

### `[target]`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `dsn` | string | required | PostgreSQL connection string. |

### `[type_mapping]`

Use [Type Mapping](/reference/type-mapping/) for the full behavior tables. These are the knobs you can set directly:

```toml
[type_mapping]
tinyint1_as_boolean = false
binary16_as_uuid = false
datetime_as_timestamptz = false
varchar_as_text = false
json_as_jsonb = true
widen_unsigned_integers = true
sanitize_json_null_bytes = true
unknown_as_text = false
enum_mode = "check"
set_mode = "text"
bit_mode = "bytea"
string_uuid_as_uuid = false
binary16_uuid_mode = "rfc4122"
time_mode = "time"
zero_date_mode = "null"
spatial_mode = "off"
nvarchar_as_text = false
money_as_numeric = true
xml_as_text = false
collation_mode = "none"
ci_as_citext = false
```

Optional MySQL collation remapping:

```toml
[type_mapping.collation_map]
utf8mb4_general_ci = "und-x-icu"
utf8mb4_unicode_ci = "und-x-icu"
```

### `[postgis]`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | MySQL only. Maps spatial columns to PostgreSQL `geometry`. |
| `create_extension` | bool | `false` | When true, pgferry runs `CREATE EXTENSION IF NOT EXISTS postgis`. |

### `[hooks]`

```toml
[hooks]
before_data = []
after_data = []
before_fk = []
after_all = []
```

Hook file paths are resolved relative to the config file directory. See [Hooks](/reference/hooks/) for phase details and templating behavior.

## DSN formats

### SQLite

pgferry opens SQLite in read-only mode and requires a real file.

| Format | Example | Result |
| --- | --- | --- |
| Plain path | `/data/app.db` | Normalized to `file:/data/app.db?mode=ro` |
| Relative path | `./relative.db` | Normalized to `file:./relative.db?mode=ro` |
| File URI | `file:/data/app.db?cache=shared` | Existing params kept, `mode=ro` appended |

Not supported: `:memory:`, `file::memory:`, or `mode=memory`.

### MSSQL

| Format | Example | Notes |
| --- | --- | --- |
| URL | `sqlserver://user:pass@host:1433?database=mydb` | Recommended. |
| URL with instance | `sqlserver://user:pass@host/instance?database=mydb` | Named instances. |
| ADO | `server=host;user id=user;password=pass;database=mydb` | Legacy format. |

The `database` parameter is required because pgferry extracts the DB name for introspection queries.

## Source-specific constraints

| Setting | MySQL | SQLite | MSSQL |
| --- | --- | --- | --- |
| `source_snapshot_mode = "single_tx"` | Yes | No | Yes |
| Worker parallelism | Yes | Forced to 1 | Yes |
| `source.charset` | Yes | Error | Error |
| `source.source_schema` | N/A | N/A | Yes |
| MySQL-only type options | Yes | Error | Error |
| MSSQL-only type options | Error | Error | Yes |
| `[postgis]` | Yes | Error | Error |
| `collation_mode` / `collation_map` / `ci_as_citext` | Yes | Error | Error |

## Important incompatibilities

| Combination | Result |
| --- | --- |
| `resume = true` + `on_schema_exists = "recreate"` | Invalid because the target schema would be dropped. |
| `resume = true` + `schema_only = true` | Invalid because there is no data stage to resume. |
| `resume = true` + `unlogged_tables = true` | Invalid because checkpointed progress could outlive crash-truncated tables. |
| `schema_only = true` + `data_only = true` | Invalid. Choose one or neither. |
| SQLite + `source_snapshot_mode = "single_tx"` | Invalid. SQLite only supports `none`. |

## Practical guidance

- Run `pgferry plan migration.toml` before the first real migration.
- Use `unlogged_tables = false` whenever you also need `resume = true`.
- Use `source_snapshot_mode = "single_tx"` when the source stays live during the migration and you need one consistent read view.
- Keep `on_schema_exists = "error"` for the first production dry runs so you do not destroy previous target state by mistake.
- Prefer hooks for views, routines, cleanup SQL, or post-load validation queries that are specific to your application.
