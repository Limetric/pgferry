---
title: SQLite To PostgreSQL
description: SQLite-specific pgferry behavior, constraints, and example starting points.
---

SQLite is simpler operationally, but there are still a few important constraints to account for before you start.

## Start here

- [minimal-safe example](/examples/sqlite/minimal-safe/)
- [recreate-fast example](/examples/sqlite/recreate-fast/)
- [chunked-resume example](/examples/sqlite/chunked-resume/)

## SQLite-specific realities

- worker count is always effectively 1
- `source_snapshot_mode = "single_tx"` is unsupported
- the source must be a real SQLite file, not an in-memory database
- the source is opened read-only
- MySQL-only and MSSQL-only type-mapping flags are rejected during config validation

## Where SQLite is usually easier

- fewer source-specific type decisions
- no source schema selection
- fewer unsupported index shapes in typical workloads
