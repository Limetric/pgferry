# Type mapping

pgferry defaults to conservative, lossless type conversions. Semantic mappings
(e.g. `tinyint(1)` &rarr; `boolean`) are opt-in via the `[type_mapping]` config section.

## MySQL &rarr; PostgreSQL type table

| MySQL type | Default PG type | Opt-in PG type | Config flag |
|---|---|---|---|
| `tinyint(1)` | `smallint` | `boolean` | `tinyint1_as_boolean` |
| `tinyint` | `smallint` | | |
| `smallint` | `smallint` (`integer` if unsigned) | | |
| `mediumint` | `integer` | | |
| `int` | `integer` (`bigint` if unsigned) | | |
| `bigint` | `bigint` (`numeric(20)` if unsigned) | | |
| `float` | `real` | | |
| `double` | `double precision` | | |
| `decimal(p,s)` | `numeric(p,s)` | | |
| `varchar(n)` | `varchar(n)` | `text` | `varchar_as_text` |
| `char(n)` | `varchar(n)` | `text` | `varchar_as_text` |
| `tinytext` | `text` | | |
| `text` | `text` | | |
| `mediumtext` | `text` | | |
| `longtext` | `text` | | |
| `json` | `json` | `jsonb` | `json_as_jsonb` |
| `enum(...)` | `text` | `text` + CHECK | `enum_mode` |
| `set(...)` | `text` | `text[]` | `set_mode` |
| `timestamp` | `timestamptz` | | |
| `datetime` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` |
| `year` | `integer` | | |
| `date` | `date` | | |
| `bit(n)` | `bytea` | | |
| `binary(16)` | `bytea` | `uuid` | `binary16_as_uuid` |
| `binary(n)` | `bytea` | | |
| `varbinary(n)` | `bytea` | | |
| `tinyblob` | `bytea` | | |
| `blob` | `bytea` | | |
| `mediumblob` | `bytea` | | |
| `longblob` | `bytea` | | |

Any MySQL type not in this table is unsupported by default. Set
`type_mapping.unknown_as_text = true` to coerce unknown types to `text`
instead of aborting.

## SQLite &rarr; PostgreSQL type table

SQLite uses dynamic typing with [type affinity](https://www.sqlite.org/datatype3.html).
pgferry uses conservative mappings &mdash; all integer types map to `bigint` since
SQLite integers can be up to 64-bit.

| SQLite type | PG type | Notes |
|---|---|---|
| `INTEGER`, `INT`, `SMALLINT`, `TINYINT`, `MEDIUMINT`, `BIGINT` | `bigint` | All integers → bigint (SQLite stores up to 64-bit) |
| `REAL`, `DOUBLE`, `FLOAT` | `double precision` | |
| `TEXT`, `VARCHAR(N)`, `CHAR(N)`, `CLOB` | `text` | Length constraints are not enforced by SQLite |
| `BLOB` | `bytea` | |
| `NUMERIC` | `numeric` | |
| `NUMERIC(P,S)` / `DECIMAL(P,S)` | `numeric(P,S)` | Precision/scale preserved when declared |
| `BOOLEAN` | `boolean` | |
| `DATETIME`, `TIMESTAMP` | `timestamp` | |
| `DATE` | `date` | |
| `JSON` | `json` | `jsonb` with `json_as_jsonb = true` |

Any SQLite type not in this table is unsupported by default. Set
`type_mapping.unknown_as_text = true` to coerce unknown types to `text`
instead of aborting.

### SQLite type mapping notes

- **No unsigned integers**: SQLite has no unsigned concept, so `add_unsigned_checks` has no effect.
- **No enums or sets**: SQLite has no native enum or set types, so `enum_mode` and `set_mode` must remain at their defaults (`"text"`).
- **MySQL-only options rejected**: `tinyint1_as_boolean`, `binary16_as_uuid`, `datetime_as_timestamptz`, `varchar_as_text`, `enum_mode = "check"`, and `set_mode = "text_array"` produce a config error when used with a SQLite source.

## Type mapping options

All options live under `[type_mapping]` in your TOML config:

```toml
[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean (MySQL only)
binary16_as_uuid = false          # binary(16) → uuid (MySQL only)
datetime_as_timestamptz = false   # datetime → timestamptz (MySQL only)
varchar_as_text = false           # varchar(n)/char(n) → text (MySQL only)
json_as_jsonb = false             # json → jsonb
sanitize_json_null_bytes = true   # strip \x00 from JSON values
unknown_as_text = false           # unknown types → text (instead of error)
enum_mode = "text"                # "text" or "check" (MySQL only)
set_mode = "text"                 # "text" or "text_array" (MySQL only)
```

### Enum mode

- **`text`** (default) &mdash; stores enum values as plain `text`. No constraint enforcement.
- **`check`** &mdash; stores as `text` with a `CHECK` constraint restricting values to the
  MySQL enum's allowed set.

### Set mode

- **`text`** (default) &mdash; stores the comma-separated set value as a single `text` column.
- **`text_array`** &mdash; splits the set into a PostgreSQL `text[]` array.

## Edge cases

### Zero dates

MySQL allows `0000-00-00` and `0000-00-00 00:00:00` as valid date/datetime values.
PostgreSQL does not. pgferry converts these to `NULL` during data streaming.

### JSON null bytes

PostgreSQL's `json` and `jsonb` types reject `\x00` (null byte) in string values.
By default (`sanitize_json_null_bytes = true`), pgferry strips null bytes from
JSON columns during COPY. Set to `false` only if you're certain your data is clean.

### Binary(16) as UUID

When `binary16_as_uuid = true`, pgferry maps `binary(16)` columns to PostgreSQL
`uuid` and converts raw 16-byte values to UUID string format during data streaming.
Other binary column sizes are always mapped to `bytea`.

### Boolean coercion

When `tinyint1_as_boolean = true`, `tinyint(1)` columns are mapped to `boolean`.
Values `0` become `false` and non-zero values become `true`. All other `tinyint`
sizes remain `smallint`.

### Varchar/char &rarr; text

When `varchar_as_text = true`, MySQL `varchar(n)` and `char(n)` columns are
mapped to PostgreSQL `text` instead of `varchar(n)`. In PostgreSQL, `text` and
`varchar(n)` have identical storage and performance &mdash; the length constraint
is a minor overhead on every write. This is useful when the source length limits
(e.g. MySQL's common `varchar(255)` default) carry no business meaning.

When disabled (the default), `char(n)` maps to `varchar(n)` rather than
`char(n)` in PostgreSQL, following the pgloader convention to avoid padding issues.

### Set splitting

When `set_mode = "text_array"`, MySQL set values like `"a,b,c"` are split on
commas and stored as `{"a","b","c"}` in a PostgreSQL `text[]` array.

### Unsigned integers

Unsigned MySQL integers are mapped to the next-wider PostgreSQL integer type
(e.g. unsigned `int` &rarr; `bigint`) to accommodate the full unsigned range. For
`bigint unsigned`, `numeric(20)` is used since PostgreSQL has no wider integer type.

Optionally, `add_unsigned_checks = true` adds `CHECK` constraints that enforce
the original unsigned range (e.g. `CHECK (col >= 0 AND col <= 4294967295)` for
unsigned `int`).

### SQLite default values

SQLite default values are mapped to PostgreSQL equivalents:

| SQLite default | PostgreSQL default |
|---|---|
| `NULL` / `null` | Omitted (no default) |
| `CURRENT_TIMESTAMP` | `CURRENT_TIMESTAMP` |
| `CURRENT_DATE` | `CURRENT_DATE` |
| Numeric literal (`42`, `3.14`) | Passed through |
| `0`/`1` on boolean column | `FALSE`/`TRUE` |
| String literal (`'hello'`) | Passed through as-is |
