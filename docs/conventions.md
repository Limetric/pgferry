# Conventions & limitations

## Naming

By default, source database identifiers are converted to `snake_case` for idiomatic PostgreSQL naming.
For example, `parentUserId` becomes `parent_user_id`. This may require application query updates.

When `snake_case_identifiers = false`, identifiers are lowercased instead (matching PostgreSQL's default case folding).
For example, `UserName` becomes `username`.

PostgreSQL identifiers are always emitted as double-quoted identifiers in generated SQL.
For example, a source column named `user` becomes `"user"` and a plain identifier like
`users` becomes `"users"` in PostgreSQL.

This changes the exact SQL text compared with older pgferry releases that only quoted
some identifiers. If you compare emitted SQL strings in external tooling or scripts,
expect more quoted output such as `"app"."users"` instead of `app.users`.

## Auto-increment &rarr; sequences

MySQL `auto_increment` and SQLite `AUTOINCREMENT` / `INTEGER PRIMARY KEY` columns are
migrated as plain integer columns during table creation. After data is loaded, pgferry:

1. Creates a PostgreSQL sequence (`schema.table_column_seq`)
2. Sets the sequence value to `max(column) + 1`
3. Attaches the sequence as the column's `DEFAULT`

This defers sequence creation to avoid conflicts during parallel COPY.

## Zero dates

MySQL allows `0000-00-00` and `0000-00-00 00:00:00` as valid date/datetime
values. PostgreSQL does not accept these. Behavior is controlled by
`type_mapping.zero_date_mode`:

- **`null`** (default) &mdash; converts zero dates to `NULL` during data streaming.
- **`error`** &mdash; aborts the migration with an error when a zero date is encountered.

## Orphan cleanup

MySQL allows orphaned rows (child rows referencing non-existent parent rows) when
`FOREIGN_KEY_CHECKS = 0` is used. PostgreSQL rejects these when FK constraints
are added.

When `clean_orphans = true`, pgferry automatically detects and cleans orphaned
rows before creating foreign keys. The cleanup action depends on the FK's
`ON DELETE` rule:

- **`SET NULL`** &rarr; the FK columns on orphaned rows are set to `NULL`
- **All other rules** (`CASCADE`, `RESTRICT`, `NO ACTION`) &rarr; orphaned rows are deleted

For each FK, pgferry checks for child rows where at least one FK column is
non-null and the referenced parent row does not exist. Affected row counts are
logged.

Orphan cleanup is **enabled by default** (`clean_orphans = true`). Set
`clean_orphans = false` to disable it &mdash; FK creation will fail if orphaned
rows exist, which is useful when you want to investigate data integrity issues
or handle cleanup manually via `before_fk` hooks.

Orphan cleanup runs only in `full` mode (skipped in `schema_only` and `data_only`).

## Generated columns

MySQL `VIRTUAL GENERATED` and `STORED GENERATED` columns are detected during
introspection. pgferry:

- Copies the **materialized value** as plain data (the generation expression is not recreated)
- Reports each generated column before migration so you can manually recreate expressions in PostgreSQL if needed

## Unsupported features

### Column types

Unsupported source column types are detected before any DDL runs. pgferry reports
the full list of unsupported columns and aborts. Set `type_mapping.unknown_as_text = true`
to coerce unknown types to `text` instead.

### Index types

The following index features are reported and skipped (migration continues):

**MySQL:**

| Feature | Example | Reason |
|---|---|---|
| FULLTEXT indexes | `FULLTEXT INDEX (col)` | No PostgreSQL equivalent without extensions |
| SPATIAL indexes | `SPATIAL INDEX (col)` | Skipped by default; recreated as `USING GIST` when `[postgis].enabled = true` |
| Prefix indexes | `INDEX (col(10))` | PostgreSQL does not support `SUB_PART` |
| Expression indexes | `INDEX ((col + 1))` | Expression key-parts not currently translated |

**SQLite:**

| Feature | Example | Reason |
|---|---|---|
| Partial indexes | `CREATE INDEX ... WHERE condition` | WHERE clause not translated |
| Expression indexes | `CREATE INDEX ... (expr)` | Expression not translated |

Unsupported indexes are logged as warnings but do not block the migration.

### Source objects

pgferry detects views, routines (functions/procedures, MySQL only), and triggers in the
source database and reports them as warnings. These are **not migrated
automatically** and require manual recreation in PostgreSQL.

## Chunking eligibility

pgferry automatically chunks tables that have a **single-column numeric primary key**
(integer types: `int`, `bigint`, `smallint`, `mediumint`, `tinyint` for MySQL;
`INTEGER` for SQLite). Chunking is range-based using `WHERE pk >= lower AND pk < upper`.

Tables that are **not chunkable** fall back to full-table `SELECT` + `COPY`:

- Tables with composite primary keys (multiple columns)
- Tables with non-numeric primary keys (UUID, VARCHAR, etc.)
- Tables with no primary key

Gaps in primary key sequences are handled naturally &mdash; a chunk spanning a
gap simply returns fewer rows than the target chunk size.

## Checkpoint files

When `resume = true`, pgferry writes a checkpoint file
(`pgferry_checkpoint.json`) in the same directory as the TOML config file.
The checkpoint records which chunks and tables have been completed.

- **Format:** Compact JSON
- **Writes:** Batched and atomic (temp file + rename) to prevent corruption
  from crashes. Flushes occur after every 10 completed items, or within 5
  seconds of the next completed item. A crash can lose up to 10 chunks of
  progress.
- **No overhead when disabled:** when `resume = false` (the default), no
  checkpoint file is created or updated.
- **Logged target required for resume:** `resume = true` requires
  `unlogged_tables = false` so checkpointed progress matches durable target
  data after a crash.
- **Cleanup:** Automatically deleted when migration completes successfully
- **Stale checkpoints:** If the source data changes between runs, resuming from
  a stale checkpoint may produce inconsistent results. pgferry logs the
  checkpoint's `started_at` timestamp when resuming.

## Source-specific notes

### MySQL

- `enum_mode` and `set_mode` control enum/set handling (including `native` enums and `text_array_check` sets)
- `tinyint1_as_boolean`, `binary16_as_uuid`, `datetime_as_timestamptz`, `varchar_as_text` enable semantic coercions
- `string_uuid_as_uuid` maps `char(36)`/`varchar(36)` to `uuid`
- `binary16_uuid_mode` controls byte order for `binary16_as_uuid` (RFC 4122 vs MySQL swap)
- `bit_mode` controls BIT(n) mapping (`bytea`, `bit`, or `varbit`)
- `time_mode` controls TIME mapping (`time`, `text`, or `interval`)
- `zero_date_mode` controls zero-date handling (`null` or `error`)
- `spatial_mode` controls raw/text spatial fallback mapping (`off`, `wkb_bytea`, or `wkt_text`)
- `[postgis]` enables native MySQL spatial migration to PostGIS `geometry`
- `source_snapshot_mode = "single_tx"` enables consistent snapshots
- Unsigned integers are widened by default (`widen_unsigned_integers = true`) to preserve the full range; set `false` to keep the original type size
- Unsigned integer ranges can be enforced via `add_unsigned_checks`

### SQLite

- All integer types map to `bigint` (SQLite stores up to 64-bit integers)
- Always runs with 1 worker (sequential data streaming)
- `source_snapshot_mode = "single_tx"` is not supported
- MySQL-only type mapping options are rejected at config validation
- In-memory databases (`:memory:`) are not supported
- The database is opened in read-only mode

## Case-insensitive columns (citext)

When `ci_as_citext = true` (MySQL only), text-like columns with `_ci` collations
are mapped to PostgreSQL's `citext` type. This preserves case-insensitive
semantics for comparisons, `UNIQUE` constraints, `GROUP BY`, and `ORDER BY`.

The required `citext` extension is validated through pgferry's shared extension
setup path and created automatically when needed.

If a `_ci` collation has an explicit `collation_map` entry, the map takes precedence
and the column retains its original type with a `COLLATE` clause.

## Enum handling

Enum behavior is controlled by `type_mapping.enum_mode` (MySQL only):

- **`text`** (default) &mdash; the column is created as `text` with no constraint. Any string value is accepted.
- **`check`** &mdash; the column is created as `text` with a `CHECK` constraint restricting values to the original MySQL enum's allowed set.
- **`native`** &mdash; creates a native PostgreSQL enum type. Type names are content-addressable
  (`pgferry_enum_XXXXXXXXXXXXXXXX` via FNV64a hash of sorted values), so columns with identical
  value sets share the same type. Enum types are created before table creation.
  **Note:** Values are sorted before hashing for deduplication, which means declaration order
  from MySQL is not preserved. PostgreSQL `ORDER BY` on native enums uses declaration order,
  so this may change sort semantics. Use `check` mode if MySQL enum ordering matters.

## Set handling

Set behavior is controlled by `type_mapping.set_mode` (MySQL only):

- **`text`** (default) &mdash; the comma-separated set value is stored as a single `text` string (e.g. `"a,b,c"`).
- **`text_array`** &mdash; the value is split on commas and stored as a PostgreSQL `text[]` array (e.g. `{"a","b","c"}`).
- **`text_array_check`** &mdash; like `text_array`, but adds a `CHECK` constraint restricting array
  elements to the original MySQL set's allowed values.

## BIT handling

BIT column behavior is controlled by `type_mapping.bit_mode` (MySQL only):

- **`bytea`** (default) &mdash; stores as raw bytes.
- **`bit`** &mdash; stores as PostgreSQL `bit(n)` with the source width. Values are converted to binary strings during COPY.
- **`varbit`** &mdash; stores as PostgreSQL `varbit`. Values are converted to binary strings during COPY.

## TIME handling

TIME column behavior is controlled by `type_mapping.time_mode` (MySQL only):

- **`time`** (default) &mdash; stores as PostgreSQL `time`. Values outside `00:00:00`&ndash;`23:59:59` will error.
- **`text`** &mdash; stores the original string representation as `text`.
- **`interval`** &mdash; stores as PostgreSQL `interval`, converting `HH:MM:SS` to `HH hours MM mins SS secs`.

## Spatial types

pgferry supports two MySQL spatial paths:

- **Native PostGIS mode** via `[postgis].enabled = true`
- **Fallback storage modes** via `type_mapping.spatial_mode`

With `[postgis].enabled = true`, MySQL spatial columns are created as PostgreSQL
`geometry` columns, MySQL's internal spatial payloads are converted to
PostGIS-compatible EWKB during COPY, and supported MySQL `SPATIAL` indexes are
recreated as `USING GIST` indexes. PostgreSQL must already have the `postgis`
extension installed unless `[postgis].create_extension = true`.

With `type_mapping.spatial_mode`, spatial values are preserved without requiring
PostGIS:

- **`off`** (default) &mdash; spatial columns are unsupported (error or `text` with `unknown_as_text`).
- **`wkb_bytea`** &mdash; stores as `bytea` using MySQL's internal binary representation
  (4-byte SRID prefix + WKB). This is not standard WKB and is not directly
  compatible with PostGIS geometry columns.
- **`wkt_text`** &mdash; stores as `text` using Well-Known Text via `ST_AsText()`.

## String UUID mapping

When `type_mapping.string_uuid_as_uuid = true` (MySQL only), `char(36)` and
`varchar(36)` columns are mapped to PostgreSQL `uuid`. Values are validated
and lowercased during COPY; invalid UUIDs cause an error.

## Binary UUID byte order

When `binary16_as_uuid = true`, the `binary16_uuid_mode` controls byte interpretation:

- **`rfc4122`** (default) &mdash; standard byte order.
- **`mysql_uuid_to_bin_swap`** &mdash; reverses MySQL's `UUID_TO_BIN(uuid, 1)` time-field swap.

## Unsigned checks

When `add_unsigned_checks = true`, pgferry adds `NOT VALID` CHECK constraints
that enforce MySQL's unsigned ranges, then validates them. This ensures the
PostgreSQL schema rejects negative values that MySQL would not have allowed.

Constraint names follow the pattern `ck_<table>_<column>_unsigned` (truncated
with a hash suffix if the name exceeds PostgreSQL's 63-character identifier limit).

Example constraints:

| MySQL type | CHECK expression |
|---|---|
| `tinyint unsigned` | `col >= 0 AND col <= 255` |
| `smallint unsigned` | `col >= 0 AND col <= 65535` |
| `mediumint unsigned` | `col >= 0 AND col <= 16777215` |
| `int unsigned` | `col >= 0 AND col <= 4294967295` |
| `bigint unsigned` | `col >= 0 AND col <= 18446744073709551615` |
| `decimal/float/double unsigned` | `col >= 0` |

## Column defaults

By default, pgferry preserves source column defaults in the PostgreSQL schema.
Set `preserve_defaults = false` to omit them from the `CREATE TABLE` DDL.

## UNLOGGED tables

When `unlogged_tables = true`, tables are created as `UNLOGGED` during the bulk
data load phase. UNLOGGED tables skip WAL writes, which significantly speeds up
COPY inserts. After data loading completes, pgferry runs `SET LOGGED` on each
table to convert them back to regular logged tables.

This option is ignored in `schema_only` and `data_only` modes.

**Warning:** If pgferry crashes or is killed during migration with UNLOGGED
tables, any data in those tables is lost. Because of that, `resume = true`
requires `unlogged_tables = false`.
