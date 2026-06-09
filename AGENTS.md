# AGENTS.md

Source database to PostgreSQL migration CLI. Supports MySQL, MariaDB, SQLite, and MSSQL sources. Single-binary Go CLI (Cobra), all source in `package main` at the repo root.

## Commands

**Go code must be formatted before every commit** — CI rejects unformatted code.

```bash
go fmt ./...                   # Format all packages — run before commit
go build -o build/pgferry .   # Build binary
go vet ./...                   # Lint
go test ./... -count=1         # Unit tests (no DB required)
go test -run TestFoo ./...     # Run a single test

# Integration tests — MySQL (requires MySQL on :3306 and PostgreSQL on :5432)
MYSQL_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -count=1 -v ./...

# Integration tests — MariaDB (requires MariaDB on :3306 and PostgreSQL on :5432)
MARIADB_DSN="root:root@tcp(127.0.0.1:3306)/pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -run TestIntegration_MariaDB -count=1 -v ./...

# Integration tests — SQLite (requires only PostgreSQL on :5432)
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -run TestIntegration_SQLite -count=1 -v ./...

# Integration tests — MSSQL (requires MSSQL on :1433 and PostgreSQL on :5432)
MSSQL_DSN="sqlserver://sa:YourStrong!Pass@127.0.0.1:1433?database=pgferry_test" \
POSTGRES_DSN="postgres://postgres:postgres@127.0.0.1:5432/pgferry_test?sslmode=disable" \
go test -tags integration -run TestIntegration_MSSQL -count=1 -v ./...
```

## Documentation

**Code changes must be reflected in the site.** The site source lives in `site/src/content/docs/`. When you add, remove, or change CLI flags, config options, behavior, or supported types, update the relevant docs pages — especially `reference/configuration.md`, `reference/type-mapping.md`, and any affected guides or examples.

## Site package management

The documentation site uses Bun for package management. Work from `site/`.

```bash
cd site
bun install                  # Install dependencies
bun run dev                  # Start the docs dev server via portless
bun run build                # Build the docs site
bun run check-routes         # Verify generated routes after a build
```

## Key references

- Operator tuning guide: <https://www.pgferry.com/operations/operator-tuning/>
- Source abstraction interface: `source.go` (`SourceDB` interface, `newSourceDB` factory)
- Migration pipeline: `main.go:runMigration`
- Integration tests use build tag `//go:build integration`
