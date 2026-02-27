# Conventions & limitations

## Naming

By default, source database identifiers are converted to `snake_case` for idiomatic PostgreSQL naming.
For example, `parentUserId` becomes `parent_user_id`. This may require application query updates.

When `snake_case_identifiers = false`, identifiers are lowercased instead (matching PostgreSQL's default case folding).
For example, `UserName` becomes `username`.

PostgreSQL reserved words are automatically double-quoted in all generated DDL.
For example, a source column named `user` becomes `"user"` in PostgreSQL.

Identifiers containing special characters (dots, spaces, etc.) are also quoted.

## Auto-increment &rarr; sequences

MySQL `auto_increment` and SQLite `AUTOINCREMENT` / `INTEGER PRIMARY KEY` columns are
migrated as plain integer columns during table creation. After data is loaded, pgferry:

1. Creates a PostgreSQL sequence (`schema.table_column_seq`)
2. Sets the sequence value to `max(column) + 1`
3. Attaches the sequence as the column's `DEFAULT`

This defers sequence creation to avoid conflicts during parallel COPY.

## Zero dates

MySQL allows `0000-00-00` and `0000-00-00 00:00:00` as valid date/datetime
values. PostgreSQL does not accept these. pgferry converts zero dates to `NULL`
during data streaming.

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
| SPATIAL indexes | `SPATIAL INDEX (col)` | Requires PostGIS |
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

## Source-specific notes

### MySQL

- `enum_mode` and `set_mode` control enum/set handling
- `tinyint1_as_boolean`, `binary16_as_uuid`, `datetime_as_timestamptz`, `varchar_as_text` enable semantic coercions
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

The `citext` extension is created automatically (`CREATE EXTENSION IF NOT EXISTS citext`)
before table creation.

If a `_ci` collation has an explicit `collation_map` entry, the map takes precedence
and the column retains its original type with a `COLLATE` clause.

## Enum handling

Enum behavior is controlled by `type_mapping.enum_mode` (MySQL only):

- **`text`** (default) &mdash; the column is created as `text` with no constraint. Any string value is accepted.
- **`check`** &mdash; the column is created as `text` with a `CHECK` constraint restricting values to the original MySQL enum's allowed set.

## Set handling

Set behavior is controlled by `type_mapping.set_mode` (MySQL only):

- **`text`** (default) &mdash; the comma-separated set value is stored as a single `text` string (e.g. `"a,b,c"`).
- **`text_array`** &mdash; the value is split on commas and stored as a PostgreSQL `text[]` array (e.g. `{"a","b","c"}`).

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
tables, any data in those tables is lost.
