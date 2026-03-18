---
title: Type Mapping
description: Source-to-PostgreSQL type mappings, alternate modes, and source-specific coercion flags.
---

pgferry defaults to conservative, mostly lossless mappings. The main default exception is JSON, which becomes PostgreSQL `jsonb` because that is usually the more useful target type.

## MySQL to PostgreSQL

| MySQL type | Default PG type | Alternate mapping | Flag |
| --- | --- | --- | --- |
| `tinyint(1)` | `smallint` | `boolean` | `tinyint1_as_boolean` |
| `tinyint` | `smallint` | | |
| `smallint` | `smallint` or `integer` if unsigned | | |
| `mediumint` | `integer` | | |
| `int` | `integer` or `bigint` if unsigned | | |
| `bigint` | `bigint` or `numeric(20)` if unsigned | | |
| `float` | `real` | | |
| `double` | `double precision` | | |
| `decimal(p,s)` | `numeric(p,s)` | | |
| `varchar(n)` / `char(n)` | `varchar(n)` | `text` | `varchar_as_text` |
| `text` family | `text` | | |
| `json` | `jsonb` | `json` | `json_as_jsonb = false` |
| `enum(...)` | `text` with `CHECK` | plain `text`, native enum | `enum_mode` |
| `set(...)` | `text` | `text[]`, `text[]` + `CHECK` | `set_mode` |
| `timestamp` | `timestamptz` | | |
| `datetime` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` |
| `date` | `date` | | |
| `time` | `time` | `text`, `interval` | `time_mode` |
| `bit(n)` | `bytea` | `bit(n)`, `varbit` | `bit_mode` |
| `binary(16)` | `bytea` | `uuid` | `binary16_as_uuid` |
| `char(36)` / `varchar(36)` | `varchar(36)` | `uuid` | `string_uuid_as_uuid` |
| `binary` / `varbinary` / blob family | `bytea` | | |
| Spatial types | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |

Unknown MySQL types error by default. Set `unknown_as_text = true` to coerce them to `text` instead.

### MySQL-specific mapping decisions

- `widen_unsigned_integers = true` preserves MySQL unsigned ranges by widening the PostgreSQL target type.
- `add_unsigned_checks = true` adds PostgreSQL `CHECK` constraints after load.
- `binary16_uuid_mode = "mysql_uuid_to_bin_swap"` reverses MySQL `UUID_TO_BIN(uuid, 1)` byte swaps.
- `zero_date_mode = "null"` converts MySQL zero dates to `NULL`. `error` aborts instead.
- `collation_mode = "auto"` emits PostgreSQL `COLLATE` clauses when pgferry can map the source collation.
- `ci_as_citext = true` maps `_ci` text columns to PostgreSQL `citext` unless a `collation_map` entry overrides that choice.

## SQLite to PostgreSQL

SQLite uses type affinity, so pgferry keeps the mapping conservative.

| SQLite type | PG type | Notes |
| --- | --- | --- |
| `INTEGER`, `INT`, `SMALLINT`, `TINYINT`, `MEDIUMINT`, `BIGINT` | `bigint` | SQLite integers are up to 64-bit. |
| `REAL`, `DOUBLE`, `FLOAT` | `double precision` | |
| `TEXT`, `VARCHAR(N)`, `CHAR(N)`, `CLOB` | `text` | SQLite does not enforce declared length. |
| `BLOB` | `bytea` | |
| `NUMERIC` | `numeric` | |
| `NUMERIC(P,S)` / `DECIMAL(P,S)` | `numeric(P,S)` | Precision and scale are preserved when declared. |
| `BOOLEAN` | `boolean` | |
| `DATETIME`, `TIMESTAMP` | `timestamp` | |
| `DATE` | `date` | |
| `JSON` | `jsonb` | `json` when `json_as_jsonb = false` |

SQLite rejects MySQL-only and MSSQL-only type-mapping options during config validation.

## MSSQL to PostgreSQL

| MSSQL type | Default PG type | Alternate mapping | Flag |
| --- | --- | --- | --- |
| `int` | `integer` | | |
| `bigint` | `bigint` | | |
| `smallint` | `smallint` | | |
| `tinyint` | `smallint` | | |
| `bit` | `boolean` | | |
| `decimal(p,s)` / `numeric(p,s)` | `numeric(p,s)` | | |
| `float` | `double precision` | | |
| `real` | `real` | | |
| `money` | `numeric(19,4)` | `text` | `money_as_numeric = false` |
| `smallmoney` | `numeric(10,4)` | `text` | `money_as_numeric = false` |
| `char(n)` | `char(n)` | | |
| `varchar(n)` | `varchar(n)` | | |
| `varchar(max)` | `text` | | |
| `nchar(n)` / `nvarchar(n)` | `char(n)` / `varchar(n)` | `text` | `nvarchar_as_text` |
| `nvarchar(max)` | `text` | | |
| `text` / `ntext` | `text` | | |
| `binary` / `varbinary` / `image` | `bytea` | | |
| `date` | `date` | | |
| `time` | `time` | | |
| `datetime`, `datetime2`, `smalldatetime` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` |
| `datetimeoffset` | `timestamptz` | | |
| `uniqueidentifier` | `uuid` | | |
| `xml` | `xml` | `text` | `xml_as_text` |
| `json` | `jsonb` | `json` | `json_as_jsonb = false` |
| `sql_variant`, `hierarchyid` | `text` | | |
| `geography`, `geometry` | unsupported | `bytea`, `text` | `spatial_mode` |
| `rowversion` / `timestamp` | `bytea` | | MSSQL `timestamp` is not a datetime type. |

### MSSQL-specific mapping notes

- `source_snapshot_mode = "single_tx"` uses `SNAPSHOT` isolation and requires `ALLOW_SNAPSHOT_ISOLATION ON` on the source database.
- `uniqueidentifier` values are byte-reordered into standard UUID order during copy.
- `nvarchar` and `nchar` lengths are divided by two because MSSQL reports byte length, not character length.
- `(max)` types map to PostgreSQL `text` or `bytea`.
- Computed columns are copied as materialized values and reported for manual semantic recreation.

## Shared mapping options

```toml
[type_mapping]
json_as_jsonb = true
sanitize_json_null_bytes = true
unknown_as_text = false
datetime_as_timestamptz = false
spatial_mode = "off"
```

## Enum and set behavior

### `enum_mode`

| Mode | Behavior |
| --- | --- |
| `check` | Store as `text` and add a `CHECK` constraint with the allowed values. |
| `text` | Store as unconstrained `text`. |
| `native` | Create a PostgreSQL enum type and reuse it for identical value sets. |

Use `check` when MySQL enum ordering matters and you do not want PostgreSQL native enum ordering semantics.

### `set_mode`

| Mode | Behavior |
| --- | --- |
| `text` | Keep the original comma-separated string. |
| `text_array` | Split the set into `text[]`. |
| `text_array_check` | Store as `text[]` and add a `CHECK` constraint limiting allowed members. |

## Spatial choices

| Choice | Use when |
| --- | --- |
| `[postgis].enabled = true` | You want real `geometry` columns and spatial index recreation for MySQL spatial data. |
| `spatial_mode = "wkb_bytea"` | You want raw binary preservation without PostGIS. |
| `spatial_mode = "wkt_text"` | You want readable text output without PostGIS. |
| `spatial_mode = "off"` | You prefer pgferry to stop and report spatial columns instead of guessing. |

## Choosing safe overrides

- Leave most type mapping at defaults for the first rehearsal.
- Turn on semantic remaps like `tinyint1_as_boolean`, `binary16_as_uuid`, or `string_uuid_as_uuid` only when you know the source data actually follows that convention.
- Prefer `unknown_as_text = false` for production rehearsals so pgferry surfaces unsupported types early instead of hiding them behind generic text columns.
