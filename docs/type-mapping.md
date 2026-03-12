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
| `enum(...)` | `text` | `text` + CHECK, native enum | `enum_mode` |
| `set(...)` | `text` | `text[]`, `text[]` + CHECK | `set_mode` |
| `timestamp` | `timestamptz` | | |
| `datetime` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` |
| `year` | `integer` | | |
| `date` | `date` | | |
| `time` | `time` | `text`, `interval` | `time_mode` |
| `bit(n)` | `bytea` | `bit(n)`, `varbit` | `bit_mode` |
| `binary(16)` | `bytea` | `uuid` | `binary16_as_uuid` |
| `char(36)` | `varchar(36)` | `uuid` | `string_uuid_as_uuid` |
| `varchar(36)` | `varchar(36)` | `uuid` | `string_uuid_as_uuid` |
| `geometry` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `point` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `linestring` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `polygon` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `multipoint` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `multilinestring` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `multipolygon` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
| `geometrycollection` | unsupported | `geometry`, `bytea`, `text` | `[postgis].enabled`, `spatial_mode` |
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
- **MySQL/MSSQL-only options rejected**: `tinyint1_as_boolean`, `binary16_as_uuid`, `datetime_as_timestamptz`, `varchar_as_text`, `enum_mode = "check"/"native"`, `set_mode = "text_array"/"text_array_check"`, `bit_mode` (non-default), `string_uuid_as_uuid`, `binary16_uuid_mode` (non-default), `time_mode` (non-default), `zero_date_mode` (non-default), `spatial_mode` (non-default), `nvarchar_as_text`, `money_as_numeric = false`, and `xml_as_text` produce a config error when used with a SQLite source.

## MSSQL &rarr; PostgreSQL type table

| MSSQL type | Default PG type | Opt-in PG type | Config flag |
|---|---|---|---|
| `int` | `integer` | | |
| `bigint` | `bigint` | | |
| `smallint` | `smallint` | | |
| `tinyint` | `smallint` | | |
| `bit` | `boolean` | | |
| `decimal(p,s)` / `numeric(p,s)` | `numeric(p,s)` | | |
| `float` | `double precision` | | |
| `real` | `real` | | |
| `money` | `numeric(19,4)` | `text` | `money_as_numeric` |
| `smallmoney` | `numeric(10,4)` | `text` | `money_as_numeric` |
| `char(n)` | `char(n)` | | |
| `varchar(n)` | `varchar(n)` | | |
| `varchar(max)` | `text` | | |
| `nchar(n)` | `char(n)` | `text` | `nvarchar_as_text` |
| `nvarchar(n)` | `varchar(n)` | `text` | `nvarchar_as_text` |
| `nvarchar(max)` | `text` | | |
| `text` / `ntext` | `text` | | |
| `binary(n)` | `bytea` | | |
| `varbinary(n)` / `varbinary(max)` | `bytea` | | |
| `image` | `bytea` | | |
| `date` | `date` | | |
| `time` | `time` | | |
| `datetime` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` |
| `datetime2` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` |
| `smalldatetime` | `timestamp` | `timestamptz` | `datetime_as_timestamptz` (shared with MySQL) |
| `datetimeoffset` | `timestamptz` | | always timestamptz |
| `uniqueidentifier` | `uuid` | | |
| `xml` | `xml` | `text` | `xml_as_text` |
| `json` | `json` | `jsonb` | `json_as_jsonb` |
| `sql_variant` | `text` | | |
| `hierarchyid` | `text` | | |
| `geography` | unsupported | `bytea`, `text` | `spatial_mode` |
| `geometry` | unsupported | `bytea`, `text` | `spatial_mode` |
| `rowversion` / `timestamp` | `bytea` | | MSSQL `timestamp` is NOT a datetime |

Any MSSQL type not in this table is unsupported by default. Set
`type_mapping.unknown_as_text = true` to coerce unknown types to `text`
instead of aborting.

### MSSQL type mapping notes

- **`timestamp` is NOT a datetime**: MSSQL's `timestamp` type (alias for `rowversion`) is an 8-byte auto-generated binary value. It maps to `bytea`, not to a PostgreSQL timestamp type.
- **`nvarchar`/`nchar` length**: MSSQL stores `max_length` in bytes (UCS-2 encoding). pgferry divides by 2 to get the character count used in PostgreSQL `varchar(n)`/`char(n)`.
- **`(max)` types**: `varchar(max)`, `nvarchar(max)`, and `varbinary(max)` report `max_length = -1` in MSSQL system catalogs. These always map to `text` or `bytea` respectively.
- **Default expression double-parens**: MSSQL wraps default constraints in extra parentheses (e.g. `((0))`, `(getdate())`). pgferry strips the outer parentheses automatically.
- **User-defined types**: Resolved to their base system type via `sys.types`. The PostgreSQL mapping uses the underlying system type.
- **Identity columns**: MSSQL `IDENTITY` columns are mapped to PostgreSQL sequences (same as MySQL `auto_increment`).
- **Computed columns**: Introspected and reported for manual migration. Values are materialized during data copy.
- **Snapshot isolation**: `source_snapshot_mode = "single_tx"` uses `SNAPSHOT` isolation level, which requires `ALTER DATABASE ... SET ALLOW_SNAPSHOT_ISOLATION ON` on the source.
- **Spatial types**: `geography` and `geometry` use method syntax (`.STAsText()`, `.STAsBinary()`) for data extraction, controlled by `spatial_mode`.
- **`uniqueidentifier`**: SQL Server stores UUIDs with mixed-endian byte ordering (first 3 groups little-endian). pgferry handles byte reordering to produce standard UUID strings.
- **`money` precision**: Mapped directly to `numeric(19,4)` / `numeric(10,4)` to avoid precision loss through float intermediaries. When `money_as_numeric = false`, maps to `text`.
- **`sql_variant`**: Mapped to `text`. Values are cast to `nvarchar(max)` server-side during data extraction, so type information (e.g. integers, dates stored in the variant) is converted to their string representation.
- **Cross-schema foreign keys**: pgferry migrates a single MSSQL schema at a time. Foreign keys referencing tables in a different schema will produce a warning and may fail during post-migration FK creation if the referenced table is not in the target PostgreSQL schema.
- **MySQL-only options rejected**: `tinyint1_as_boolean`, `binary16_as_uuid`, `varchar_as_text`, `enum_mode = "check"/"native"`, `set_mode = "text_array"/"text_array_check"`, `bit_mode` (non-default), `string_uuid_as_uuid`, `binary16_uuid_mode` (non-default), `time_mode` (non-default), `zero_date_mode` (non-default), `widen_unsigned_integers = false`, `collation_mode = "auto"`, `collation_map`, and `ci_as_citext` produce a config error when used with an MSSQL source.

## Type mapping options

All options live under `[type_mapping]` in your TOML config:

```toml
[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean (MySQL only)
binary16_as_uuid = false          # binary(16) → uuid (MySQL only)
datetime_as_timestamptz = false   # datetime → timestamptz (MySQL/MSSQL)
varchar_as_text = false           # varchar(n)/char(n) → text (MySQL only)
json_as_jsonb = false             # json → jsonb
sanitize_json_null_bytes = true   # strip \x00 from JSON values
unknown_as_text = false           # unknown types → text (instead of error)
enum_mode = "text"                # "text", "check", or "native" (MySQL only)
set_mode = "text"                 # "text", "text_array", or "text_array_check" (MySQL only)
bit_mode = "bytea"                # "bytea", "bit", or "varbit" (MySQL only)
string_uuid_as_uuid = false       # char(36)/varchar(36) → uuid (MySQL only)
binary16_uuid_mode = "rfc4122"    # "rfc4122" or "mysql_uuid_to_bin_swap" (MySQL only)
time_mode = "time"                # "text", "time", or "interval" (MySQL only)
zero_date_mode = "null"           # "null" or "error" (MySQL only)
spatial_mode = "off"              # "off", "wkb_bytea", or "wkt_text" (MySQL/MSSQL)
nvarchar_as_text = false          # nvarchar(n)/nchar(n) → text (MSSQL only)
money_as_numeric = true           # money/smallmoney → numeric (MSSQL only)
xml_as_text = false               # xml → text instead of xml (MSSQL only)
collation_mode = "none"           # "none" or "auto" (MySQL only)
ci_as_citext = false              # _ci text columns → citext (MySQL only)

# Map MySQL collations to PG collations (used when collation_mode = "auto")
# [type_mapping.collation_map]
# utf8mb4_general_ci = "und-x-icu"
```

### Enum mode

- **`text`** (default) &mdash; stores enum values as plain `text`. No constraint enforcement.
- **`check`** &mdash; stores as `text` with a `CHECK` constraint restricting values to the
  MySQL enum's allowed set.
- **`native`** &mdash; creates a native PostgreSQL enum type per distinct set of values.
  Type names are content-addressable (`pgferry_enum_XXXXXXXXXXXXXXXX` using FNV64a hash
  of sorted values), so columns with identical enum definitions share the same type.
  Enum types are created before table creation.

  **Ordering caveat:** PostgreSQL native enums have a declaration order that affects
  `ORDER BY`. Because pgferry sorts values before hashing (for deduplication), two
  MySQL columns with the same values but different declaration order (e.g.
  `enum('new','old')` vs `enum('old','new')`) will share the same PG type, and
  `ORDER BY` will use alphabetical order for both. If MySQL-side enum ordering
  carries business semantics, use `enum_mode = "check"` instead.

### Set mode

- **`text`** (default) &mdash; stores the comma-separated set value as a single `text` column.
- **`text_array`** &mdash; splits the set into a PostgreSQL `text[]` array.
- **`text_array_check`** &mdash; like `text_array`, but adds a `CHECK` constraint restricting
  array elements to the MySQL set's allowed values (e.g. `CHECK (col <@ ARRAY['a','b','c']::text[])`).

### BIT mode

Controls how MySQL `BIT(n)` columns are mapped (MySQL only):

- **`bytea`** (default) &mdash; stores as `bytea` (raw bytes).
- **`bit`** &mdash; stores as PostgreSQL `bit(n)` with a fixed width matching the source.
  Values are converted to binary string representation during COPY.
- **`varbit`** &mdash; stores as PostgreSQL `varbit` (variable-length bit string).
  Values are converted to binary string representation during COPY.

### String UUID mapping

When `string_uuid_as_uuid = true`, MySQL `char(36)` and `varchar(36)` columns are
mapped to PostgreSQL `uuid`. During data streaming, values are validated as UUIDs
(must match the `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx` pattern) and lowercased.
Invalid values cause an error.

### Binary UUID mode

When `binary16_as_uuid = true`, the `binary16_uuid_mode` setting controls byte
interpretation:

- **`rfc4122`** (default) &mdash; bytes are in standard RFC 4122 order. This is
  the correct mode for applications that store UUIDs as raw 16-byte values.
- **`mysql_uuid_to_bin_swap`** &mdash; bytes follow MySQL's `UUID_TO_BIN(uuid, 1)`
  layout where the time-high and time-low fields are swapped for better index
  locality. pgferry reverses the swap during data streaming to produce standard
  UUID strings.

`binary16_uuid_mode` requires `binary16_as_uuid = true`; setting a non-default
mode without it is a config error.

### TIME mode

Controls how MySQL `TIME` columns are mapped (MySQL only):

- **`time`** (default) &mdash; stores as PostgreSQL `time`. Values outside the
  `00:00:00`&ndash;`23:59:59` range (MySQL TIME supports &minus;838:59:59 to 838:59:59)
  will cause a PostgreSQL error.
- **`text`** &mdash; stores as `text`, preserving the original string representation.
- **`interval`** &mdash; stores as PostgreSQL `interval`. Values are converted from
  MySQL's `HH:MM:SS` format to `HH hours MM mins SS secs` format, preserving
  negative durations.

### Spatial mode

Controls how spatial types are mapped (MySQL and MSSQL).

MySQL spatial types: `geometry`, `point`, `linestring`, `polygon`,
`multipoint`, `multilinestring`, `multipolygon`, `geometrycollection`.
MSSQL spatial types: `geography`, `geometry`.

- **`off`** (default) &mdash; spatial types are unsupported. Columns with spatial
  types cause an error (or map to `text` if `unknown_as_text = true`).
- **`wkb_bytea`** &mdash; stores spatial data as `bytea` using MySQL's internal
  binary representation (4-byte SRID prefix + WKB).
  **Warning:** MySQL's internal binary format prepends a 4-byte SRID before the
  standard WKB payload. This is **not** standard OGC WKB and is **not** directly
  compatible with PostGIS `geometry` columns (which expect pure WKB or EWKB).
  Use `wkb_bytea` only for raw archival; if you plan to use PostGIS, prefer
  `wkt_text` or post-process the `bytea` values to strip the SRID prefix.
- **`wkt_text`** &mdash; stores spatial data as `text` using Well-Known Text (WKT)
  representation via MySQL's `ST_AsText()` function.

### PostGIS

For MySQL sources, `[postgis].enabled = true` switches spatial columns from the
fallback `spatial_mode` behavior to native PostgreSQL/PostGIS `geometry`
columns.

- Spatial payloads stay on the COPY path and are converted from MySQL's
  internal format (4-byte SRID prefix + WKB) into PostGIS-compatible EWKB.
- pgferry currently maps spatial columns to generic `geometry` rather than
  subtype-constrained declarations.
- Supported MySQL `SPATIAL` indexes are recreated as PostgreSQL `USING GIST`
  indexes.
- `[postgis].create_extension = false` requires the `postgis` extension to
  already exist; set it to `true` to let pgferry run
  `CREATE EXTENSION IF NOT EXISTS postgis`.

## Edge cases

### Zero dates

MySQL allows `0000-00-00` and `0000-00-00 00:00:00` as valid date/datetime values.
PostgreSQL does not. The `zero_date_mode` setting controls handling:

- **`null`** (default) &mdash; converts zero dates to `NULL` during data streaming.
- **`error`** &mdash; aborts the migration with an error when a zero date is encountered.

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

### Case-insensitive columns (citext)

When `ci_as_citext = true`, MySQL text-like columns (`text`, `varchar(n)`,
`char(n)`) that use a `_ci` (case-insensitive) collation are mapped to
PostgreSQL's `citext` type instead of their default mapping. The `citext`
extension (included in PostgreSQL contrib) provides true case-insensitive
comparisons, `UNIQUE`, `GROUP BY`, and `ORDER BY` &mdash; a closer semantic
match to MySQL's `_ci` collation behavior.

pgferry validates the required `citext` extension before table creation and
creates it automatically when needed.

If a `_ci` collation also has an entry in `collation_map`, the map entry takes
precedence (the column keeps its original type with a `COLLATE` clause instead
of becoming `citext`).

Non-text columns (e.g. integers) are never affected, even if they carry a `_ci`
collation in the MySQL schema.

### Set splitting

When `set_mode = "text_array"` or `"text_array_check"`, MySQL set values like
`"a,b,c"` are split on commas and stored as `{"a","b","c"}` in a PostgreSQL
`text[]` array. With `"text_array_check"`, a CHECK constraint additionally
restricts elements to the original MySQL set's allowed values.

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

### MSSQL default values

MSSQL wraps default constraint expressions in extra parentheses. pgferry strips
outer parentheses and maps common functions:

| MSSQL default | PostgreSQL default |
|---|---|
| `((0))` | `0` |
| `((1))` on `bit` column | `TRUE` |
| `((0))` on `bit` column | `FALSE` |
| `(getdate())` | `CURRENT_TIMESTAMP` |
| `(sysdatetime())` | `CURRENT_TIMESTAMP` |
| `(newid())` | `gen_random_uuid()` |
| `(newsequentialid())` | `gen_random_uuid()` |
| `(N'string')` | `'string'` |
| `(suser_sname())` | `CURRENT_USER` |
| `(user_name())` | `CURRENT_USER` |
| Numeric literal (`((42))`) | `42` |
| String literal (`('hello')`) | `'hello'` |
