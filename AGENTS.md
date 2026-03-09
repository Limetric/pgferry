# AGENTS.md

Source database to PostgreSQL migration CLI. Supports MySQL, SQLite, and MSSQL sources. Reads source schema via INFORMATION_SCHEMA (MySQL), PRAGMAs (SQLite), or `sys.*` catalog views (MSSQL), creates tables in PG (optionally UNLOGGED), streams data via the COPY protocol with parallel workers, then adds constraints/indexes/sequences/triggers in post-migration.

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

# Integration tests — MSSQL (requires MSSQL on :1433 and PostgreSQL on :5432)
MSSQL_DSN="sqlserver://sa:YourStrong!Pass@127.0.0.1:1433?database=pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -run TestIntegration_MSSQL -count=1 -v ./...
```

## Architecture

All source is in `package main` at the repo root. Single-binary CLI using Cobra.

**Source abstraction** (`source.go`): The `SourceDB` interface abstracts source-specific logic (introspection, type mapping, value transformation, identifier quoting). Implementations:
- `source_mysql.go` — MySQL via `INFORMATION_SCHEMA`, backtick quoting, parallel workers
- `source_sqlite.go` — SQLite via PRAGMAs, double-quote quoting, single worker (`MaxWorkers=1`)
- `source_mssql.go` — MSSQL via `sys.*` catalog views, bracket `[name]` quoting, parallel workers, configurable `sourceSchema` (default "dbo")

Factory: `newSourceDB(sourceType string)` returns the correct implementation based on `[source].type`.

**Migration pipeline** (orchestrated in `main.go:runMigration`):

1. `loadConfig` — TOML config (`schema` required; defaults: `on_schema_exists=error`, `source_snapshot_mode=none`, `workers=min(runtime.NumCPU(), 8)`, `index_workers=workers`, `unlogged_tables=false`, `preserve_defaults=true`, `add_unsigned_checks=false`, `clean_orphans=true`, `snake_case_identifiers=true`, `replicate_on_update_current_timestamp=false`, `chunk_size=100000`, `resume=false`, `validation=none`)
2. `src.IntrospectSchema` — source-specific schema introspection (tables, columns, indexes, FKs). Also reports source views/routines/triggers that require manual migration.
3. `createTables` — columns only, no constraints (UNLOGGED only when enabled, defaults included by default; omitted when `preserve_defaults=false`)
4. `loadAndExecSQLFiles` — before_data hooks
5. `migrateData` — tables with a single-column numeric PK are split into range-based chunks; other tables use full-table copy. Either parallel goroutines per chunk/table (`source_snapshot_mode=none`) or sequential within a single read-only transaction (`source_snapshot_mode=single_tx`, MySQL/MSSQL). Checkpoint state is persisted to `pgferry_checkpoint.json` after each chunk; when `resume=true`, completed chunks are skipped.
6. `loadAndExecSQLFiles` — after_data hooks
7. `validateMigration` — optional post-load validation (`validation=row_count` compares source and target row counts per table)
8. `postMigrate` — SET LOGGED → PKs → indexes (parallel with bounded `index_workers` concurrency) → before_fk hooks → optional orphan cleanup (`clean_orphans=true`) → FKs → sequences → optional unsigned checks → optional triggers → after_all hooks

**Hooks system:** SQL files run at 4 phases (before_data, after_data, before_fk, after_all). All occurrences of `{{schema}}` are replaced with the configured schema name. Paths are resolved relative to the TOML config file directory.

## Conventions

- Source names are converted to snake_case by default via `toSnakeCase`; when `snake_case_identifiers=false`, only lowercased; PostgreSQL reserved words are quoted via `pgIdent`
- Tables are created as regular logged tables by default; set `unlogged_tables=true` to use UNLOGGED during bulk load
- `auto_increment` columns get PG sequences; `ON UPDATE CURRENT_TIMESTAMP` columns get trigger emulation only when `replicate_on_update_current_timestamp=true`
- `type_mapping.enum_mode` controls enum handling (`text` or `check`); `type_mapping.set_mode` controls set handling (`text` or `text_array`) — MySQL only
- Some type mapping options are MySQL-only (`tinyint1_as_boolean`, `binary16_as_uuid`, `varchar_as_text`, `widen_unsigned_integers`, `enum_mode`, `set_mode`); some are MSSQL-only (`nvarchar_as_text`, `money_as_numeric`, `xml_as_text`); some are shared (`datetime_as_timestamptz`, `spatial_mode`). Incompatible sources reject these at config validation
- Unsupported index features (e.g. MySQL FULLTEXT/SPATIAL/prefix/expression indexes, SQLite partial/expression indexes, MSSQL XML/SPATIAL/filtered indexes) are reported and skipped so migration can proceed safely
- Unsupported column types are detected up front with a complete error list before table creation starts
- Generated columns are migrated as materialized values; expression semantics are reported for manual follow-up
- SQLite sources use `modernc.org/sqlite` (pure Go, no CGO); DSNs are normalized to read-only URI mode; in-memory databases are rejected
- MSSQL sources use `go-mssqldb` (pure Go, native TDS); introspection via `sys.tables`, `sys.columns`, `sys.types`, `sys.indexes`, `sys.foreign_keys` filtered by `sourceSchema`; `nvarchar`/`nchar` `max_length` divided by 2 (UCS-2 bytes → chars); user-defined types resolved to base system types; default expressions have outer parentheses stripped; `IDENTITY` columns mapped to `auto_increment` convention; computed columns detected via `is_computed`; `timestamp`/`rowversion` mapped to `bytea` (not datetime); `uniqueidentifier` byte reordering for mixed-endian UUIDs; `money`/`smallmoney` → `numeric` to avoid float precision loss
- Chunking logic is in `chunk.go` (chunk key eligibility, range planning, chunked SELECT queries); checkpoint persistence is in `checkpoint.go` (JSON state, atomic writes); validation is in `validate.go` (row count comparison)
- Tables with composite or non-numeric primary keys fall back to full-table copy (not chunkable)
- Integration tests use build tag `//go:build integration`
