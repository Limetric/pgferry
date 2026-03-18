---
title: SQLite Examples
description: SQLite-to-PostgreSQL example playbooks with real configs and practical sequencing notes.
---

SQLite is operationally simpler than MySQL or MSSQL, but the examples still map to the same major migration decisions.

## Start here

- [minimal-safe](/examples/sqlite/minimal-safe/) for first real migrations
- [recreate-fast](/examples/sqlite/recreate-fast/) for disposable target schemas
- [chunked-resume](/examples/sqlite/chunked-resume/) for larger files and restart safety
- [hooks](/examples/sqlite/hooks/) for PostgreSQL-side follow-up SQL

## Full set

- [minimal-safe](/examples/sqlite/minimal-safe/)
- [recreate-fast](/examples/sqlite/recreate-fast/)
- [chunked-resume](/examples/sqlite/chunked-resume/)
- [hooks](/examples/sqlite/hooks/)
- [schema-only](/examples/sqlite/schema-only/)
- [data-only](/examples/sqlite/data-only/)
