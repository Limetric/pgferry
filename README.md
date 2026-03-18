# pgferry

Move MySQL, MariaDB, SQLite, or MSSQL into PostgreSQL with one config file and one binary.

Introspects your source schema, creates matching PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and triggers after the load. When things get messy, you still get hooks, type mapping, checkpoints, validation, and post-load cleanup.

- No runtime dependencies or extra tooling to install
- Interactive `pgferry wizard` that can generate, plan, and start a migration in one flow
- Fast parallel `COPY` loads with range-based chunking for large tables
- Clear stage and row-copy progress logs, so long runs do not look frozen
- Preflight `plan` command reports views, routines, triggers, generated columns, skipped indexes, orphan-cleanup candidates, required extensions, and collation warnings before PostgreSQL is touched
- Resumable chunked migrations, so failures do not send you back to zero
- Consistent-snapshot mode for migrating live source databases safely
- Built for messy real-world schemas with hooks, orphan cleanup, generated-column reporting, and unsupported-index warnings
- `schema_only` and `data_only` runs when you need tighter control
- Extension-backed features like `citext` and PostGIS, with validation and optional auto-create
- Post-load validation modes that range from fast `row_count` checks to stronger bounded `sampled_hash` content checks

CI runs integration tests across MySQL 5.7, 8.0, LTS, and Innovation, MariaDB 10.6 and latest, MSSQL 2017 through 2025, and SQLite against the latest PostgreSQL release on every commit.

## Install

Download the latest binary from [GitHub Releases](https://github.com/Limetric/pgferry/releases/latest), or build from source:

```bash
git clone https://github.com/Limetric/pgferry.git
cd pgferry
go build -o build/pgferry .
```

For a first run, start with the wizard and then run `plan`:

```bash
pgferry wizard
pgferry plan migration.toml
pgferry migrate migration.toml
```

In an interactive terminal, plain `pgferry` also opens the wizard. It walks you through the source and target DSNs, target schema, migration mode, and the key type-mapping options.

If you prefer to create the config yourself, the minimum shape looks like this:

```toml
schema = "app"

[source]
type = "mysql" # or "mariadb" / "sqlite" / "mssql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Any PostgreSQL `sslmode` is supported. `sslmode=disable` is just a local example.
Any valid PostgreSQL `sslmode` works here. `sslmode=disable` is only shown in the example because it is a common local-development setting.

`pgferry migration.toml` remains supported as a shorthand for `pgferry migrate migration.toml`.

## Examples

The [`examples/`](examples/) directory is split by source type.

**MySQL:** [`minimal-safe`](examples/mysql/minimal-safe/), [`recreate-fast`](examples/mysql/recreate-fast/), [`hooks`](examples/mysql/hooks/), [`sakila`](examples/mysql/sakila/), [`schema-only`](examples/mysql/schema-only/), [`data-only`](examples/mysql/data-only/), [`chunked-resume`](examples/mysql/chunked-resume/)

**MariaDB:** [`minimal-safe`](examples/mariadb/minimal-safe/)

**SQLite:** [`minimal-safe`](examples/sqlite/minimal-safe/), [`recreate-fast`](examples/sqlite/recreate-fast/), [`hooks`](examples/sqlite/hooks/), [`schema-only`](examples/sqlite/schema-only/), [`data-only`](examples/sqlite/data-only/), [`chunked-resume`](examples/sqlite/chunked-resume/)

**MSSQL:** [`minimal-safe`](examples/mssql/minimal-safe/), [`recreate-fast`](examples/mssql/recreate-fast/)

## Documentation

The website is the primary end-user docs surface:

- [Install](https://pgferry.com/get-started/install/)
- [Quick Start](https://pgferry.com/get-started/quick-start/)
- [Plan and Validate](https://pgferry.com/get-started/plan-and-validate/)
- [Migration Patterns](https://pgferry.com/migration-patterns/)
- [Source Guides](https://pgferry.com/source-guides/)
- [Examples](https://pgferry.com/examples/)
- [Reference](https://pgferry.com/reference/)

## How it's built

Most of this codebase was written with LLM agents. The architecture, edge case handling, and test coverage reflect that. It runs in production and the integration test matrix catches regressions, but you should know how it was made.

## License

Apache 2.0. See [LICENSE](LICENSE).
