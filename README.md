# pgferry

A MySQL/SQLite-to-PostgreSQL migration CLI. Single binary, zero runtime dependencies.

Reads your source schema, creates matching PostgreSQL tables, streams data in parallel via
COPY, then wires up constraints, indexes, sequences, and (optionally) triggers.

| Source | Driver | Workers | Snapshot mode |
|---|---|---|---|
| MySQL | `go-sql-driver/mysql` | Parallel (configurable) | `none`, `single_tx` |
| SQLite | `modernc.org/sqlite` (pure Go) | Sequential (1 worker) | `none` only |

## Install

Download the latest binary from the [GitHub Releases](https://github.com/Limetric/pgferry/releases/latest) page, or:

```bash
go install github.com/Limetric/pgferry@latest
```

## Quick start

**MySQL &rarr; PostgreSQL**

```toml
schema = "app"

[source]
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

**SQLite &rarr; PostgreSQL**

```toml
schema = "app"

[source]
type = "sqlite"
dsn = "/path/to/database.db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Run:

```bash
pgferry migration.toml
```

pgferry will introspect the source, create tables under the `app` schema, stream all data,
then add primary keys, indexes, foreign keys, and auto-increment sequences.

## Why not pgloader?

[pgloader](https://github.com/dimitri/pgloader) is a great tool. pgferry aims to be
simpler and more robust out of the box &mdash; native MySQL 8.4+ auth, a single TOML config
instead of a custom DSL, flexible [type mapping](docs/type-mapping.md) coercions,
[SQL hooks](docs/hooks.md), configurable orphan cleanup, and SQLite support.

> We also maintain a [pgloader fork](https://github.com/Limetric/pgloader) that patches
> common upstream issues.

## Examples

The [`examples/`](examples/) directory has ready-to-use configs:
[`minimal-safe`](examples/minimal-safe/),
[`recreate-fast`](examples/recreate-fast/),
[`hooks`](examples/hooks/),
[`sakila`](examples/sakila/),
[`schema-only`](examples/schema-only/),
[`data-only`](examples/data-only/).

## Documentation

- [Configuration](docs/configuration.md) &mdash; all TOML settings, defaults, validation
- [Type mapping](docs/type-mapping.md) &mdash; source&rarr;PG type tables, coercion options
- [Migration pipeline](docs/migration-pipeline.md) &mdash; step-by-step pipeline, modes, snapshots
- [Hooks](docs/hooks.md) &mdash; 4-phase SQL hook system, templating
- [Conventions & limitations](docs/conventions.md) &mdash; naming, orphan cleanup, unsupported features

## License

Apache 2.0 &mdash; see [LICENSE](LICENSE).
