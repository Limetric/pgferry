# AGENTS.md

Source database to PostgreSQL migration CLI. Supports MySQL and SQLite sources. Reads source schema via INFORMATION_SCHEMA (MySQL) or PRAGMAs (SQLite), creates tables in PG (optionally UNLOGGED), streams data via the COPY protocol with parallel workers, then adds constraints/indexes/sequences/triggers in post-migration.

## Commands

```bash
go build -o build/pgferry .          # Build binary
go vet ./...                   # Lint
go test ./... -count=1         # Unit tests (no DB required)
go test -run TestFoo ./...     # Run a single test

# Integration tests — MySQL (requires MySQL on :3306 and PostgreSQL on :5432)
MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -count=1 -v ./...

# Integration tests — SQLite (requires only PostgreSQL on :5432)
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -run TestIntegration_SQLite -count=1 -v ./...
```

## Architecture

All source is in `package main` at the repo root. Single-binary CLI using Cobra.

**Source abstraction** (`source.go`): The `SourceDB` interface abstracts source-specific logic (introspection, type mapping, value transformation, identifier quoting). Implementations:
- `source_mysql.go` — MySQL via `INFORMATION_SCHEMA`, backtick quoting, parallel workers
- `source_sqlite.go` — SQLite via PRAGMAs, double-quote quoting, single worker (`MaxWorkers=1`)

Factory: `newSourceDB(sourceType string)` returns the correct implementation based on `[source].type`.

**Migration pipeline** (orchestrated in `main.go:runMigration`):

1. `loadConfig` — TOML config (`schema` required; defaults: `on_schema_exists=error`, `source_snapshot_mode=none`, `workers=min(runtime.NumCPU(), 8)`, `unlogged_tables=false`, `preserve_defaults=true`, `add_unsigned_checks=false`, `clean_orphans=true`, `snake_case_identifiers=true`, `replicate_on_update_current_timestamp=false`)
2. `src.IntrospectSchema` — source-specific schema introspection (tables, columns, indexes, FKs). Also reports source views/routines/triggers that require manual migration.
3. `createTables` — columns only, no constraints (UNLOGGED only when enabled, defaults included by default; omitted when `preserve_defaults=false`)
4. `loadAndExecSQLFiles` — before_data hooks
5. `migrateData` — either parallel goroutines per table (`source_snapshot_mode=none`) or a single read-only transaction for a consistent snapshot across tables (`source_snapshot_mode=single_tx`, MySQL only)
6. `loadAndExecSQLFiles` — after_data hooks
7. `postMigrate` — SET LOGGED → PKs → indexes → before_fk hooks → optional orphan cleanup (`clean_orphans=true`) → FKs → sequences → optional unsigned checks → optional triggers → after_all hooks

**Hooks system:** SQL files run at 4 phases (before_data, after_data, before_fk, after_all). All occurrences of `{{schema}}` are replaced with the configured schema name. Paths are resolved relative to the TOML config file directory.

## Conventions

- Source names are converted to snake_case by default via `toSnakeCase`; when `snake_case_identifiers=false`, only lowercased; PostgreSQL reserved words are quoted via `pgIdent`
- Tables are created as regular logged tables by default; set `unlogged_tables=true` to use UNLOGGED during bulk load
- `auto_increment` columns get PG sequences; `ON UPDATE CURRENT_TIMESTAMP` columns get trigger emulation only when `replicate_on_update_current_timestamp=true`
- `type_mapping.enum_mode` controls enum handling (`text` or `check`); `type_mapping.set_mode` controls set handling (`text` or `text_array`) — MySQL only
- Some type mapping options are MySQL-only (`tinyint1_as_boolean`, `binary16_as_uuid`, `datetime_as_timestamptz`, `widen_unsigned_integers`, `enum_mode`, `set_mode`); SQLite sources reject these at config validation
- Unsupported index features (e.g. MySQL FULLTEXT/SPATIAL/prefix/expression indexes, SQLite partial/expression indexes) are reported and skipped so migration can proceed safely
- Unsupported column types are detected up front with a complete error list before table creation starts
- Generated columns are migrated as materialized values; expression semantics are reported for manual follow-up
- SQLite sources use `modernc.org/sqlite` (pure Go, no CGO); DSNs are normalized to read-only URI mode; in-memory databases are rejected
- Integration tests use build tag `//go:build integration`
