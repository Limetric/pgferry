# pgferry

A MySQL/SQLite-to-PostgreSQL migration CLI. Single binary, zero runtime dependencies.

Reads your source schema, creates matching PostgreSQL tables, streams data in parallel via
COPY with optional per-table chunking for large tables, then wires up constraints, indexes,
sequences, and (optionally) triggers. Supports resumable migrations with checkpoints and
post-load validation.

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

Or launch the interactive config wizard:

```bash
pgferry generate
```

The wizard can save a reusable `migration.toml`, run the migration immediately, or do both.

pgferry will introspect the source, create tables under the `app` schema, stream all data,
then add primary keys, indexes, foreign keys, and auto-increment sequences.

## Why not pgloader?

[pgloader](https://github.com/dimitri/pgloader) is a great tool. pgferry aims to be
simpler and more robust out of the box &mdash; native MySQL 8.4+ auth, a single TOML config
instead of a custom DSL, flexible [type mapping](docs/type-mapping.md) coercions,
charset/collation awareness, [SQL hooks](docs/hooks.md), configurable orphan cleanup,
and SQLite support.

> We also maintain a [pgloader fork](https://github.com/Limetric/pgloader) that patches
> common upstream issues.

## Examples

The [`examples/`](examples/) directory is organized by source type.

**MySQL**

[`minimal-safe`](examples/mysql/minimal-safe/),
[`recreate-fast`](examples/mysql/recreate-fast/),
[`hooks`](examples/mysql/hooks/),
[`sakila`](examples/mysql/sakila/),
[`schema-only`](examples/mysql/schema-only/),
[`data-only`](examples/mysql/data-only/),
[`chunked-resume`](examples/mysql/chunked-resume/).

**SQLite**

[`minimal-safe`](examples/sqlite/minimal-safe/),
[`recreate-fast`](examples/sqlite/recreate-fast/),
[`hooks`](examples/sqlite/hooks/),
[`schema-only`](examples/sqlite/schema-only/),
[`data-only`](examples/sqlite/data-only/),
[`chunked-resume`](examples/sqlite/chunked-resume/).

## Documentation

- [Configuration](docs/configuration.md) &mdash; all TOML settings, defaults, validation
- [Type mapping](docs/type-mapping.md) &mdash; source&rarr;PG type tables, coercion options
- [Migration pipeline](docs/migration-pipeline.md) &mdash; step-by-step pipeline, modes, snapshots, chunking, resume, validation
- [Hooks](docs/hooks.md) &mdash; 4-phase SQL hook system, templating
- [Conventions & limitations](docs/conventions.md) &mdash; naming, orphan cleanup, unsupported features

## License

Apache 2.0 &mdash; see [LICENSE](LICENSE).
