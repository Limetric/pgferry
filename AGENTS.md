# AGENTS.md

MySQL-to-PostgreSQL migration CLI. Reads MySQL schema via INFORMATION_SCHEMA, creates tables in PG (optionally UNLOGGED), streams data via the COPY protocol with parallel workers, then adds constraints/indexes/sequences/triggers in post-migration.

## Commands

```bash
go build -o build/pgferry .          # Build binary
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

1. `loadConfig` — TOML config (`schema` required; defaults: `on_schema_exists=error`, `source_snapshot_mode=none`, `workers=min(runtime.NumCPU(), 8)`, `unlogged_tables=false`, `preserve_defaults=false`, `add_unsigned_checks=false`, `clean_orphans=true`, `replicate_on_update_current_timestamp=false`)
2. `introspectSchema` — MySQL INFORMATION_SCHEMA queries for tables, columns, indexes, FKs
   Also reports source views/routines/triggers that require manual migration.
3. `createTables` — columns only, no constraints (UNLOGGED only when enabled, defaults only when `preserve_defaults=true`)
4. `loadAndExecSQLFiles` — before_data hooks
5. `migrateData` — either parallel goroutines per table (`source_snapshot_mode=none`) or a single read-only MySQL transaction for a consistent snapshot across tables (`source_snapshot_mode=single_tx`)
6. `loadAndExecSQLFiles` — after_data hooks
7. `postMigrate` — SET LOGGED → PKs → indexes → before_fk hooks → optional orphan cleanup (`clean_orphans=true`) → FKs → sequences → optional unsigned checks → optional triggers → after_all hooks

**Hooks system:** SQL files run at 4 phases (before_data, after_data, before_fk, after_all). All occurrences of `{{schema}}` are replaced with the configured schema name. Paths are resolved relative to the TOML config file directory.

## Conventions

- MySQL names are converted to snake_case via `toSnakeCase`; PostgreSQL reserved words are quoted via `pgIdent`
- Tables are created as regular logged tables by default; set `unlogged_tables=true` to use UNLOGGED during bulk load
- `auto_increment` columns get PG sequences; `ON UPDATE CURRENT_TIMESTAMP` columns get trigger emulation only when `replicate_on_update_current_timestamp=true`
- `type_mapping.enum_mode` controls enum handling (`text` or `check`); `type_mapping.set_mode` controls set handling (`text` or `text_array`)
- Unsupported MySQL index features (e.g. FULLTEXT/SPATIAL/prefix/expression indexes) are reported and skipped so migration can proceed safely
- Unsupported MySQL column types are detected up front with a complete error list before table creation starts
- Generated columns are migrated as materialized values; expression semantics are reported for manual follow-up
- Integration tests use build tag `//go:build integration`
