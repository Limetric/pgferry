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

Download the latest binary from the [GitHub Releases](https://github.com/Limetric/pgferry/releases/latest) page.

Or install with Go:

```bash
go install github.com/Limetric/pgferry@latest
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

Check the binary version at any time with:

```bash
pgferry --version
# or
pgferry version
```

## Examples

The [`examples/`](examples/) directory contains ready-to-use configurations for
common scenarios:

| Example | Description |
|---|---|
| [`minimal-safe`](examples/minimal-safe/) | Conservative defaults &mdash; error if schema already exists, all type mappings lossless |
| [`recreate-fast`](examples/recreate-fast/) | Drops and recreates the target schema, uses `UNLOGGED` tables during bulk load for maximum throughput, 8 parallel workers |
| [`hooks`](examples/hooks/) | Demonstrates all 4 hook phases with example SQL files for extensions, ANALYZE, orphan cleanup, and post-migration views |
| [`sakila`](examples/sakila/) | Full migration of the [Sakila sample database](https://dev.mysql.com/doc/sakila/en/) with orphan cleanup and post-migration views |
| [`schema-only`](examples/schema-only/) | Create the PostgreSQL schema (tables, PKs, indexes, FKs, sequences, triggers) without migrating any data |
| [`data-only`](examples/data-only/) | Stream data into an existing schema and reset sequences &mdash; use after a `schema_only` run |

## Documentation

| Topic | Description |
|---|---|
| [Configuration](docs/configuration.md) | All TOML settings, defaults, validation |
| [Type mapping](docs/type-mapping.md) | MySQL&rarr;PG type table, coercion options, edge cases |
| [Migration pipeline](docs/migration-pipeline.md) | Step-by-step pipeline, modes, snapshots |
| [Hooks](docs/hooks.md) | 4-phase SQL hook system, templating |
| [Conventions & limitations](docs/conventions.md) | Naming, orphan cleanup, unsupported features |

## Development

```bash
go build -o build/pgferry .          # build
go vet ./...                         # lint
go test ./... -count=1               # unit tests (no DB required)

# integration tests (requires MySQL on :3306 and PostgreSQL on :5432)
MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -count=1 -v ./...
```

## License

Apache 2.0 &mdash; see [LICENSE](LICENSE).
