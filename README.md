# pgferry

Migrate MySQL, SQLite, or MSSQL databases to PostgreSQL with one config file and one binary.

Introspects your source schema, creates matching PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and triggers after the load. When things get messy — and real migrations always do — you get hooks, type mapping, checkpoints, validation, and post-load cleanup.

- Single binary, zero runtime dependencies
- MySQL, SQLite, and MSSQL sources, PostgreSQL target
- Parallel workers and range-based chunking for large tables
- Split into `schema_only` and `data_only` phases for tighter control
- Preflight `plan` reports what needs manual attention
- Extension-backed features with validation and optional auto-create
- Resume interrupted runs from checkpoints
- SQL hooks at four pipeline stages

CI runs integration tests across MySQL 5.7, 8.0, LTS, and Innovation, MSSQL 2017 through 2025, and SQLite against the latest PostgreSQL release on every commit.

| Source | Driver                         | Workers                 | Snapshot mode       |
| ------ | ------------------------------ | ----------------------- | ------------------- |
| MySQL  | `go-sql-driver/mysql`          | Parallel (configurable) | `none`, `single_tx` |
| SQLite | `modernc.org/sqlite` (pure Go) | Sequential (1 worker)   | `none` only         |
| MSSQL  | `go-mssqldb` (pure Go)         | Parallel (configurable) | `none`, `single_tx` |

## Install

Download the latest binary from [GitHub Releases](https://github.com/Limetric/pgferry/releases/latest), or build from source:

```bash
git clone https://github.com/Limetric/pgferry.git
cd pgferry
go build -o build/pgferry .
```

## Quick Start

Create `migration.toml` manually or via `pgferry generate`.

**MySQL -> PostgreSQL**

```toml
schema = "app"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

**SQLite -> PostgreSQL**

```toml
schema = "app"

[source]
type = "sqlite"
dsn = "/path/to/database.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

**MSSQL -> PostgreSQL**

```toml
schema = "app"

[source]
type = "mssql"
dsn = "sqlserver://sa:YourStrong!Pass@127.0.0.1:1433?database=source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Run the migration:

```bash
pgferry migration.toml
```

## Extension Support

pgferry supports PostgreSQL extension-backed migrations instead of forcing every
edge case into plain text or `bytea`.

- `ci_as_citext = true` maps MySQL `_ci` text columns to PostgreSQL `citext`
- `[postgis].enabled = true` maps MySQL spatial columns to native PostGIS `geometry`
- `plan` reports required extensions before migration starts
- pgferry validates required extensions in `full`, `schema_only`, and `data_only` runs
- Missing extensions can be preinstalled manually, or auto-created when the feature allows it

Example:

```toml
[type_mapping]
ci_as_citext = true
spatial_mode = "off"

[postgis]
enabled = true
create_extension = true
```

Use `spatial_mode = "wkb_bytea"` or `spatial_mode = "wkt_text"` when you want
to preserve spatial data without requiring PostGIS.

## Check First, Migrate Second

Inspect the source before touching PostgreSQL:

```bash
pgferry plan migration.toml
pgferry plan migration.toml --output-dir hooks --format json
```

`plan` reports objects that need manual attention, including views, routines, triggers, generated columns, unsupported indexes, required extensions, and collation warnings. With `--output-dir`, it also generates SQL hook skeletons you can fill in.

## Why pgferry over pgloader

We maintain a [pgloader fork](https://github.com/Limetric/pgloader) with fixes for common upstream issues — this comparison comes from hands-on experience shipping both tools.

[`pgloader`](https://github.com/dimitri/pgloader) is the established choice, but it has real gaps that `pgferry` fills:

- **MSSQL that actually works**: pgloader's MSSQL support depends on FreeTDS (C library), which breaks `datetimeoffset`, `datetime2`, `varbinary(max)`, Unicode text, Azure SQL, and parallel reads. pgferry uses `go-mssqldb` (pure Go, native TDS) and handles all MSSQL types correctly.
- **MySQL 8.4+ auth**: pgloader's MySQL driver doesn't support `caching_sha2_password`, the default auth plugin since MySQL 8.0. pgferry works out of the box.
- **Static binary**: no Common Lisp runtime, no SBCL build issues, no dependency problems. One binary, runs anywhere.
- **TOML config**: declarative, version-controllable, no DSL to learn.
- **Resumable checkpoints**: interrupt a migration and pick up where you left off. pgloader restarts from scratch.
- **Granular type mapping**: fine-grained control over how MySQL and SQLite types map to PostgreSQL — booleans, UUIDs, timestamps, enums, sets, unsigned integers, text widening, and more, each with its own toggle.
- **Extension-aware migrations**: native `citext` and opt-in PostGIS support, with preflight validation and optional `CREATE EXTENSION` for PostGIS.
- **Charset and collation awareness**: detects mismatches and warns before data moves.
- **SQL hooks at four stages**: inject custom SQL before data, after data, before foreign keys, and after everything.
- **Orphan cleanup**: optionally delete rows that would violate foreign keys before constraints are added.
- **Preflight planning**: `pgferry plan` tells you exactly what will need manual follow-up, before you touch the target.

## Examples

The [`examples/`](examples/) directory is split by source type.

**MySQL:** [`minimal-safe`](examples/mysql/minimal-safe/), [`recreate-fast`](examples/mysql/recreate-fast/), [`hooks`](examples/mysql/hooks/), [`sakila`](examples/mysql/sakila/), [`schema-only`](examples/mysql/schema-only/), [`data-only`](examples/mysql/data-only/), [`chunked-resume`](examples/mysql/chunked-resume/)

**SQLite:** [`minimal-safe`](examples/sqlite/minimal-safe/), [`recreate-fast`](examples/sqlite/recreate-fast/), [`hooks`](examples/sqlite/hooks/), [`schema-only`](examples/sqlite/schema-only/), [`data-only`](examples/sqlite/data-only/), [`chunked-resume`](examples/sqlite/chunked-resume/)

**MSSQL:** [`minimal-safe`](examples/mssql/minimal-safe/), [`recreate-fast`](examples/mssql/recreate-fast/)

## Documentation

- [Configuration](docs/configuration.md): all TOML settings, defaults, and validation
- [Type mapping](docs/type-mapping.md): source-to-PostgreSQL type mapping and coercion options
- [Migration pipeline](docs/migration-pipeline.md): pipeline stages, snapshot modes, chunking, resume, and validation
- [Conventions and limitations](docs/conventions.md): includes extension-backed features such as `citext` and PostGIS
- [Hooks](docs/hooks.md): the four hook phases and template substitution

## How it's built

Most of this codebase was written with LLM agents. The architecture, edge case handling, and test coverage reflect that — it moved fast. It runs in production and the integration test matrix catches regressions across MySQL 5.7 to latest, MSSQL 2017 to 2025, SQLite, and PostgreSQL latest, but you should know how it was made.

## License

Apache 2.0. See [LICENSE](LICENSE).
