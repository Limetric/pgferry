# pgferry

A MySQL-to-PostgreSQL migration CLI. Single binary, zero runtime dependencies.

pgferry reads your MySQL schema, creates matching PostgreSQL tables, streams data
in parallel via the COPY protocol, then wires up constraints, indexes, sequences,
and (optionally) triggers.

## How does this compare to pgloader?

[pgloader](https://github.com/dimitri/pgloader) is a great tool and the
go-to choice for many migrations. pgferry aims to be simpler to set up and
more robust out of the box:

- **Just works with MySQL 8.4+** &mdash; native support for `caching_sha2_password` auth
- **Simple configuration** &mdash; a single TOML file with safe defaults instead of a custom DSL
- **Flexible type mapping** &mdash; defaults are lossless, but you can opt into
  coercions like `tinyint1_as_boolean`, `binary16_as_uuid`, `json_as_jsonb`,
  enum-to-check-constraint, and more &mdash; useful for making the PostgreSQL
  schema feel native rather than a 1:1 MySQL clone
- **Configurable orphan cleanup** &mdash; detects and removes rows that would
  violate FK constraints (on by default, disable with `clean_orphans = false`)
- **SQL hooks** &mdash; run your own SQL at 4 phases (`before_data`, `after_data`,
  `before_fk`, `after_all`) with `{{schema}}` templating
- **CI-tested against real databases** &mdash; every change is verified with
  integration tests against MySQL and PostgreSQL

> We also maintain a [pgloader fork](https://github.com/Limetric/pgloader)
> that patches some common upstream issues &mdash; worth a look if you need
> pgloader-specific features.

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

| Example                                    | Description                                                                                                                       |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------- |
| [`minimal-safe`](examples/minimal-safe/)   | Conservative defaults &mdash; error if schema already exists, all type mappings lossless                                          |
| [`recreate-fast`](examples/recreate-fast/) | Drops and recreates the target schema, uses `UNLOGGED` tables during bulk load for maximum throughput, 8 parallel workers         |
| [`hooks`](examples/hooks/)                 | Demonstrates all 4 hook phases with example SQL files for extensions, ANALYZE, orphan cleanup, and post-migration views           |
| [`sakila`](examples/sakila/)               | Full migration of the [Sakila sample database](https://dev.mysql.com/doc/sakila/en/) with orphan cleanup and post-migration views |
| [`schema-only`](examples/schema-only/)     | Create the PostgreSQL schema (tables, PKs, indexes, FKs, sequences, triggers) without migrating any data                          |
| [`data-only`](examples/data-only/)         | Stream data into an existing schema and reset sequences &mdash; use after a `schema_only` run                                     |

## Documentation

| Topic                                            | Description                                            |
| ------------------------------------------------ | ------------------------------------------------------ |
| [Configuration](docs/configuration.md)           | All TOML settings, defaults, validation                |
| [Type mapping](docs/type-mapping.md)             | MySQL&rarr;PG type table, coercion options, edge cases |
| [Migration pipeline](docs/migration-pipeline.md) | Step-by-step pipeline, modes, snapshots                |
| [Hooks](docs/hooks.md)                           | 4-phase SQL hook system, templating                    |
| [Conventions & limitations](docs/conventions.md) | Naming, orphan cleanup, unsupported features           |

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
