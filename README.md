# pgferry

Migrate MySQL, SQLite, or MSSQL databases to PostgreSQL with one config file and one binary.

Introspects your source schema, creates matching PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and triggers after the load. When things get messy — and real migrations always do — you get hooks, type mapping, checkpoints, validation, and post-load cleanup.

- No runtime dependencies or extra tooling to install
- MySQL, SQLite, and MSSQL support that holds up in production
- Fast parallel `COPY` loads with range-based chunking for large tables
- `schema_only` and `data_only` runs when you need tighter control
- Preflight `plan` command, resumable checkpoints, and SQL hooks for messy migrations
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

## Check First, Migrate Second

Inspect the source before touching PostgreSQL:

```bash
pgferry plan migration.toml
pgferry plan migration.toml --output-dir hooks --format json
```

`plan` reports objects that need manual attention, including views, routines, triggers, generated columns, unsupported indexes, required extensions, and collation warnings. With `--output-dir`, it also generates SQL hook skeletons you can fill in.

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
