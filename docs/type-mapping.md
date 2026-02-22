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
| `varchar(n)` | `varchar(n)` | | |
| `char(n)` | `varchar(n)` | | |
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

## Type mapping options

All options live under `[type_mapping]` in your TOML config:

```toml
[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean
binary16_as_uuid = false          # binary(16) → uuid
datetime_as_timestamptz = false   # datetime → timestamptz
json_as_jsonb = false             # json → jsonb
sanitize_json_null_bytes = true   # strip \x00 from JSON values
unknown_as_text = false           # unknown types → text (instead of error)
enum_mode = "text"                # "text" or "check"
set_mode = "text"                 # "text" or "text_array"
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

### Char &rarr; varchar

MySQL `char(n)` columns are mapped to `varchar(n)` rather than `char(n)` in
PostgreSQL. This follows the pgloader convention and avoids padding issues.

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
