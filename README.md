# pgferry

Migrate MySQL, SQLite, or MSSQL databases to PostgreSQL with one config file and one binary.

Introspects your source schema, creates matching PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and triggers after the load. When things get messy — and real migrations always do — you get hooks, type mapping, checkpoints, validation, and post-load cleanup.

- No runtime dependencies or extra tooling to install
- Interactive `pgferry wizard` that can generate, plan, and start a migration in one flow
- Fast parallel `COPY` loads with range-based chunking for large tables
- Clear stage and row-copy progress logs, so long runs don’t look frozen
- Preflight `plan` command reports views, routines, triggers, generated columns, skipped indexes, required extensions, and collation warnings before PostgreSQL is touched
- Resumable chunked migrations, so failures don’t send you back to zero
- Consistent-snapshot mode for migrating live source databases safely
- Built for messy real-world schemas with hooks, orphan cleanup, generated-column reporting, and unsupported-index warnings
- `schema_only` and `data_only` runs when you need tighter control
- Extension-backed features like `citext` and PostGIS, with validation and optional auto-create

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

For a first run, start with the wizard:

```bash
pgferry wizard
```

In an interactive terminal start, plain `pgferry` also opens the wizard. It walks you through source and target DSNs, the target schema, migration mode, and the most important type-mapping options. At the end, you can save the generated `migration.toml`, run `plan`, start the migration immediately, or do both.

If you prefer to create the config yourself, the minimum shape looks like this:

```toml
schema = "app"

[source]
type = "mysql" # or "sqlite" / "mssql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Then run:

```bash
pgferry migrate migration.toml
```

`pgferry migration.toml` remains supported as a shorthand.

Need source-specific DSN examples? See [Configuration](docs/configuration.md) or the source-specific configs in [examples/](examples/).

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

Most of this codebase was written with LLM agents. The architecture, edge case handling, and test coverage reflect that — it moved fast. It runs in production and the integration test matrix catches regressions, but you should know how it was made.

## License

Apache 2.0. See [LICENSE](LICENSE).
