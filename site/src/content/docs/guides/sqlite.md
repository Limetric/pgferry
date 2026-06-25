---
title: SQLite To PostgreSQL
description: Migrate SQLite to PostgreSQL with pgferry — single-worker read-only loads, real-file-only sources, and the few constraints to plan around.
---

SQLite is the easygoing one of the bunch — no live server to wrestle, no replication to babysit. Still a few constraints worth knowing before you dive in.

## Start here

- [minimal-safe example](/examples/sqlite/minimal-safe/)
- [recreate-fast example](/examples/sqlite/recreate-fast/)
- [chunked-resume example](/examples/sqlite/chunked-resume/)

## SQLite-specific realities

- worker count is always effectively 1 — SQLite is single-threaded here, and that's perfectly fine
- `source_snapshot_mode = "single_tx"` is unsupported
- the source must be a real SQLite file, not an in-memory database
- the source is opened read-only, so your original file is never touched
- MySQL-family-only and MSSQL-only type-mapping flags are rejected during config validation

## Where SQLite is usually easier

- fewer source-specific type decisions
- no source schema selection
- fewer unsupported index shapes in typical workloads
