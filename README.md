# pgferry

A MySQL-to-PostgreSQL migration CLI. Single binary, zero runtime dependencies.

pgferry reads your MySQL schema, creates matching PostgreSQL tables, streams data
in parallel via the COPY protocol, then wires up constraints, indexes, sequences,
and (optionally) triggers.

## Why not pgloader?

[pgloader](https://github.com/dimitri/pgloader) is the established tool for this
job, but it has real pain points that pgferry sidesteps:

| | pgferry | pgloader |
|---|---|---|
| **MySQL 8+ auth** | Works out of the box (`caching_sha2_password`, etc.) | Requires `mysql_native_password` fallback or SSL workarounds due to its dated MySQL client library |
| **Configuration** | Single TOML file with sane defaults | Custom DSL (`LOAD DATABASE FROM ... WITH ...`) that is powerful but hard to get right |
| **Type mapping control** | Opt-in type coercions (`tinyint1_as_boolean`, `binary16_as_uuid`, `json_as_jsonb`, etc.) &mdash; defaults are conservative and lossless | Built-in casting rules that can silently change semantics |
| **Orphan cleanup** | Automatically detects and cleans rows that would violate FK constraints before creating them | Manual; FK failures abort the migration |
| **Hooks** | 4-phase SQL hook system (`before_data`, `after_data`, `before_fk`, `after_all`) with `{{schema}}` templating | Custom Lisp-based scripting |
| **Build** | `go build` &mdash; single static binary | Common Lisp toolchain (SBCL + Quicklisp); notoriously difficult to package |
| **Data streaming** | Parallel workers per table, each using PostgreSQL's COPY protocol | Also uses COPY, but worker model is less configurable |

In short: pgferry is easier to install, easier to configure, and works with modern
MySQL servers without auth workarounds.

> **Note:** We also maintain a [pgloader fork](https://github.com/Limetric/pgloader)
> that patches some of the most common upstream issues. It may be useful if you need
> pgloader-specific features, but pgferry is the more reliable option for
> MySQL-to-PostgreSQL migrations.

## Install

```bash
go install pgferry@latest
```

Or build from source:

```bash
git clone https://github.com/your-org/pgferry.git
cd pgferry
go build -o pgferry .
```

## Quick start

1. Create a config file (or copy one from [`examples/`](examples/)):

```toml
schema = "app"

[mysql]
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[postgres]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

2. Run the migration:

```bash
pgferry migration.toml
```

That's it. pgferry will introspect your MySQL database, create tables in
PostgreSQL under the `app` schema, stream all data, then add primary keys,
indexes, foreign keys, and auto-increment sequences.

## Examples

The [`examples/`](examples/) directory contains ready-to-use configurations for
common scenarios:

| Example | Description |
|---|---|
| [`minimal-safe`](examples/minimal-safe/) | Conservative defaults &mdash; error if schema already exists, all type mappings lossless |
| [`recreate-fast`](examples/recreate-fast/) | Drops and recreates the target schema, uses `UNLOGGED` tables during bulk load for maximum throughput, 8 parallel workers |
| [`hooks`](examples/hooks/) | Demonstrates all 4 hook phases with example SQL files for extensions, ANALYZE, orphan cleanup, and post-migration views |
| [`sakila`](examples/sakila/) | Full migration of the [Sakila sample database](https://dev.mysql.com/doc/sakila/en/) with orphan cleanup and post-migration views |

## Configuration reference

```toml
# Target PostgreSQL schema name (required)
schema = "app"

# What to do if the schema already exists: "error" (default) or "recreate"
on_schema_exists = "error"

# Source read consistency mode: "none" (default, parallel) or "single_tx" (single-transaction snapshot, sequential)
source_snapshot_mode = "none"

# Use UNLOGGED tables during bulk load, then SET LOGGED after (default: false)
unlogged_tables = false

# Emulate MySQL ON UPDATE CURRENT_TIMESTAMP via PG triggers (default: false)
replicate_on_update_current_timestamp = false

# Parallel worker count (default: min(NumCPU, 8))
workers = 4

[mysql]
dsn = "user:pass@tcp(host:port)/dbname"

[postgres]
dsn = "postgres://user:pass@host:port/dbname?sslmode=disable"

[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean instead of smallint
binary16_as_uuid = false          # binary(16) → uuid instead of bytea
datetime_as_timestamptz = false   # datetime → timestamptz instead of timestamp
json_as_jsonb = false             # json → jsonb instead of json
sanitize_json_null_bytes = true   # strip \x00 from JSON (PG rejects them)
unknown_as_text = false           # map unrecognized MySQL types to text

[hooks]
before_data = []   # after table creation, before COPY
after_data = []    # after COPY, before constraints
before_fk = []     # after PKs/indexes, before FK creation
after_all = []     # after everything (views, ANALYZE, etc.)
```

Hook SQL file paths are resolved relative to the config file directory. All
occurrences of `{{schema}}` in hook files are replaced with the configured schema
name at runtime.

## Migration pipeline

pgferry runs the following steps in order:

1. **Introspect** &mdash; query MySQL `INFORMATION_SCHEMA` for tables, columns, indexes, and foreign keys
2. **Create tables** &mdash; columns only, no constraints (optionally `UNLOGGED` for speed)
3. **`before_data` hooks**
4. **Stream data** &mdash; either parallel workers (`source_snapshot_mode = "none"`) or a single read-only source transaction (`"single_tx"`) to keep all table reads in one MySQL snapshot
5. **`after_data` hooks**
6. **Post-migration:**
   - Convert `UNLOGGED` tables to `LOGGED` (if applicable)
   - Add primary keys
   - Create indexes
   - **`before_fk` hooks**
   - Auto-clean orphaned rows that would violate FK constraints
   - Add foreign keys
   - Reset auto-increment sequences
   - Create `ON UPDATE CURRENT_TIMESTAMP` triggers (if enabled)
   - **`after_all` hooks**

## Conventions

- MySQL names are converted to `snake_case` (e.g. `parentUserId` becomes `parent_user_id`)
- PostgreSQL reserved words are automatically quoted (e.g. `user` becomes `"user"`)
- `auto_increment` columns get PostgreSQL sequences
- Zero dates (`0000-00-00`) are converted to `NULL`
- All type mappings default to conservative, lossless conversions &mdash; opt in to semantic mappings like `boolean` or `uuid` explicitly
- Generated columns are copied as materialized values; generation expressions are not recreated automatically (pgferry reports these columns before migration)

## Type mapping

| MySQL | Default PG type | Opt-in PG type |
|---|---|---|
| `tinyint(1)` | `smallint` | `boolean` |
| `tinyint` | `smallint` | |
| `smallint` | `smallint` (`integer` if unsigned) | |
| `mediumint` | `integer` | |
| `int` | `integer` (`bigint` if unsigned) | |
| `bigint` | `bigint` (`numeric(20)` if unsigned) | |
| `float` | `real` | |
| `double` | `double precision` | |
| `decimal(p,s)` | `numeric(p,s)` | |
| `varchar(n)` | `varchar(n)` | |
| `char(n)` | `varchar(n)` | |
| `text` / `mediumtext` / `longtext` | `text` | |
| `json` | `json` | `jsonb` |
| `enum` | `text` | |
| `timestamp` | `timestamptz` | |
| `datetime` | `timestamp` | `timestamptz` |
| `year` | `integer` | |
| `date` | `date` | |
| `binary(16)` | `bytea` | `uuid` |
| `blob` / `mediumblob` / `longblob` | `bytea` | |

## Development

```bash
go build -o pgferry .          # build
go vet ./...                   # lint
go test ./... -count=1         # unit tests (no DB required)

# integration tests (requires MySQL on :3306 and PostgreSQL on :5432)
MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -count=1 -v ./...
```

## License

Apache 2.0 &mdash; see [LICENSE](LICENSE).
