---
title: Install
description: Install pgferry from releases or build it from source.
---

`pgferry` is a single Go binary. There are no sidecar services, no runtime agents, and no extra database middleware to deploy.

## Download a release

Grab the latest binary from [GitHub Releases](https://github.com/Limetric/pgferry/releases/latest).

After downloading, verify the binary is reachable:

```bash
pgferry version
```

## Build from source

```bash
git clone https://github.com/Limetric/pgferry.git
cd pgferry
go build -o build/pgferry .
./build/pgferry version
```

## What the binary expects

- A source DSN for MySQL, SQLite, or MSSQL.
- A target PostgreSQL DSN.
- A TOML config file describing schema, type mapping, and migration behavior.

## Test locally

Unit tests do not require a database:

```bash
go test ./... -count=1
```

Integration coverage is split by source type. The repository README includes the exact environment variables and commands for MySQL, SQLite, and MSSQL runs.

## Next step

Move to [Quick Start](/get-started/quick-start/) to create a minimal config and run your first migration.
