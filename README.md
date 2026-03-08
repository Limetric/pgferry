# pgferry

Move MySQL or SQLite databases into PostgreSQL with one config file and one binary.

`pgferry` introspects your source schema, creates matching PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and optional triggers after the load. It is built for migrations that need to be boring in production: resumable, inspectable, and easy to reason about.

- Single binary, zero runtime dependencies
- MySQL and SQLite source support
- Parallel loading for MySQL, chunking for large tables
- Resume interrupted runs from checkpoints
- Preflight planning for unsupported objects and manual follow-up
- Optional SQL hooks and post-load validation

| Source | Driver                         | Workers                 | Snapshot mode       |
| ------ | ------------------------------ | ----------------------- | ------------------- |
| MySQL  | `go-sql-driver/mysql`          | Parallel (configurable) | `none`, `single_tx` |
| SQLite | `modernc.org/sqlite` (pure Go) | Sequential (1 worker)   | `none` only         |

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

Run the migration:

```bash
pgferry migration.toml
```

## Check First, Migrate Second

Inspect the source before touching PostgreSQL:

```bash
pgferry plan migration.toml
pgferry plan migration.toml --output-dir hooks --format json
```

`plan` reports objects that need manual attention, including views, routines, triggers, generated columns, unsupported indexes, and collation warnings. With `--output-dir`, it also generates SQL hook skeletons you can fill in.

## Why pgferry

[`pgloader`](https://github.com/dimitri/pgloader) is the OG. It earned that reputation, and it is still an important tool in the ecosystem. `pgferry` is the better default when you want a migration tool that is easier to operate and easier to trust day to day: one static binary, one TOML config, native MySQL 8.4+ auth support, flexible [type mapping](docs/type-mapping.md), charset and collation awareness, [SQL hooks](docs/hooks.md), configurable orphan cleanup, and resumable checkpoints.

If `pgloader` is the classic, `pgferry` is the more practical choice for teams that want migrations to be predictable, inspectable, and easy to rerun.

We also maintain a [pgloader fork](https://github.com/Limetric/pgloader) with fixes for common upstream issues, so that comparison comes from hands-on experience.

## Examples

The [`examples/`](examples/) directory is split by source type.

**MySQL:** [`minimal-safe`](examples/mysql/minimal-safe/), [`recreate-fast`](examples/mysql/recreate-fast/), [`hooks`](examples/mysql/hooks/), [`sakila`](examples/mysql/sakila/), [`schema-only`](examples/mysql/schema-only/), [`data-only`](examples/mysql/data-only/), [`chunked-resume`](examples/mysql/chunked-resume/)

**SQLite:** [`minimal-safe`](examples/sqlite/minimal-safe/), [`recreate-fast`](examples/sqlite/recreate-fast/), [`hooks`](examples/sqlite/hooks/), [`schema-only`](examples/sqlite/schema-only/), [`data-only`](examples/sqlite/data-only/), [`chunked-resume`](examples/sqlite/chunked-resume/)

## Documentation

- [Configuration](docs/configuration.md): all TOML settings, defaults, and validation
- [Type mapping](docs/type-mapping.md): source-to-PostgreSQL type mapping and coercion options
- [Migration pipeline](docs/migration-pipeline.md): pipeline stages, snapshot modes, chunking, resume, and validation
- [Hooks](docs/hooks.md): the four hook phases and template substitution
- [Conventions and limitations](docs/conventions.md): naming rules, cleanup behavior, and unsupported features

## License

Apache 2.0. See [LICENSE](LICENSE).
