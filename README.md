# pgferry

**[pgferry.com](https://www.pgferry.com)** — Migrate MySQL, MariaDB, SQLite, or MSSQL databases to PostgreSQL with one config file and one binary.

Introspects your source schema, creates matching PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and triggers after the load. When things get messy, you still get hooks, type mapping, checkpoints, validation, and post-load cleanup.

- No runtime dependencies or extra tooling to install
- Interactive `pgferry wizard` that can generate, plan, and start a migration in one flow
- Fast parallel `COPY` loads with range-based chunking for large tables
- Clear stage and row-copy progress logs, so long runs do not look frozen
- Preflight `plan` command reports views, routines, triggers, generated columns, skipped indexes, semantic-drift warnings (defaults, CHECK constraints, comments, partitioning), orphan-cleanup candidates, required extensions, collation warnings, and (with copy-risk analysis) a low-confidence copy-phase ETA range before PostgreSQL is touched
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
type = "mysql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"
# type = "mariadb"
# dsn = "root:root@tcp(127.0.0.1:3306)/source_db"
# type = "sqlite"
# dsn = "/path/to/source.db"
# type = "mssql"
# dsn = "sqlserver://sa:pass@127.0.0.1:1433?database=source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Any PostgreSQL `sslmode` is supported. `sslmode=disable` is just a local example.

If you want to keep secrets out of the committed TOML, set them at runtime instead:

```bash
export PGFERRY_SOURCE_DSN='root:root@tcp(127.0.0.1:3306)/source_db'
export PGFERRY_TARGET_DSN='postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable'
pgferry migrate migration.toml
```

Non-empty `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN` override `source.dsn` and `target.dsn`.

`pgferry migration.toml` remains supported as a shorthand for `pgferry migrate migration.toml`.

## Examples

Use the docs site for copy-pasteable example configs and walkthroughs:

- [All examples](https://www.pgferry.com/examples/)
- [MySQL examples](https://www.pgferry.com/examples/mysql/)
- [MariaDB examples](https://www.pgferry.com/examples/mariadb/)
- [SQLite examples](https://www.pgferry.com/examples/sqlite/)
- [MSSQL examples](https://www.pgferry.com/examples/mssql/)

The raw example files still live in [`examples/`](examples/).

## Documentation

The website is the primary end-user docs surface:

- [Install](https://www.pgferry.com/get-started/install/)
- [Quick Start](https://www.pgferry.com/get-started/quick-start/)
- [Advanced Options](https://www.pgferry.com/get-started/advanced-options/)
- [Migration Patterns](https://www.pgferry.com/migration-patterns/)
- [Guides](https://www.pgferry.com/guides/)
- [Examples](https://www.pgferry.com/examples/)
- [Operations](https://www.pgferry.com/operations/)
- [Operator tuning](https://www.pgferry.com/operations/operator-tuning/)
- [Reference](https://www.pgferry.com/reference/)

## How it's built

Most of this codebase was written with LLM agents. The architecture, edge case handling, and test coverage reflect that. It runs in production and the integration test matrix catches regressions, but you should know how it was made.

## License

Apache 2.0. See [LICENSE](LICENSE).
