# pgferry

Move MySQL, SQLite, or MSSQL into PostgreSQL with one config file and one binary.

pgferry introspects the source schema, creates PostgreSQL tables, streams data with `COPY`, then adds keys, indexes, foreign keys, sequences, and optional trigger emulation after the load. It also gives you `plan`, hook phases, chunked resume, validation, and source-specific type mapping for the parts that should not be guessed blindly.

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

Most of this codebase was written with LLM agents. The architecture, edge case handling, and test coverage reflect that — it moved fast. It runs in production and the integration test matrix catches regressions, but you should know how it was made.

## License

Apache 2.0. See [LICENSE](LICENSE).
