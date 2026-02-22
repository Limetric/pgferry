# AGENTS.md

MySQL-to-PostgreSQL migration CLI. Reads MySQL schema via INFORMATION_SCHEMA, creates tables in PG (optionally UNLOGGED), streams data via the COPY protocol with parallel workers, then adds constraints/indexes/sequences/triggers in post-migration.

## Commands

```bash
go build -o pgferry .          # Build binary
go vet ./...                   # Lint
go test ./... -count=1         # Unit tests (no DB required)
go test -run TestFoo ./...     # Run a single test

# Integration tests (requires MySQL on :3306 and PostgreSQL on :5432)
MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -count=1 -v ./...
```

## Architecture

All source is in `package main` at the repo root. Single-binary CLI using Cobra.

**Migration pipeline** (orchestrated in `main.go:runMigration`):

1. `loadConfig` — TOML config (`schema` required; defaults: `on_schema_exists=error`, `workers=min(runtime.NumCPU(), 8)`, `unlogged_tables=false`, `preserve_defaults=false`, `add_unsigned_checks=false`, `replicate_on_update_current_timestamp=false`)
2. `introspectSchema` — MySQL INFORMATION_SCHEMA queries for tables, columns, indexes, FKs
3. `createTables` — columns only, no constraints (UNLOGGED only when enabled, defaults only when `preserve_defaults=true`)
4. `loadAndExecSQLFiles` — before_data hooks
5. `migrateData` — parallel goroutines (semaphore pattern), each table gets own MySQL conn, streams rows through `pgx.CopyFrom`
6. `loadAndExecSQLFiles` — after_data hooks
7. `postMigrate` — SET LOGGED → PKs → indexes → before_fk hooks → orphan cleanup → FKs → sequences → optional unsigned checks → optional triggers → after_all hooks

**Hooks system:** SQL files run at 4 phases (before_data, after_data, before_fk, after_all). All occurrences of `{{schema}}` are replaced with the configured schema name. Paths are resolved relative to the TOML config file directory.

## Conventions

- MySQL names are converted to snake_case via `toSnakeCase`; PostgreSQL reserved words are quoted via `pgIdent`
- Tables are created as regular logged tables by default; set `unlogged_tables=true` to use UNLOGGED during bulk load
- `auto_increment` columns get PG sequences; `ON UPDATE CURRENT_TIMESTAMP` columns get trigger emulation only when `replicate_on_update_current_timestamp=true`
- Unsupported MySQL index features (e.g. FULLTEXT/SPATIAL/prefix/expression indexes) are reported and skipped so migration can proceed safely
- Integration tests use build tag `//go:build integration`
