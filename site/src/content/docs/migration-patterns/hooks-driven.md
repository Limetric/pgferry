---
title: Hooks-driven Migrations
description: Use SQL hook phases when pgferry reports work you need to finish manually.
---

Hooks are the normal answer when pgferry correctly tells you that something exists but should not be recreated automatically.

## Typical use cases

- create extensions before data load
- run `ANALYZE` after bulk COPY
- clean orphaned data before foreign keys
- recreate views, materialized views, routines, and validation SQL after the built-in pipeline

## Start from these examples

- [MySQL hooks](/examples/mysql/hooks/)
- [SQLite hooks](/examples/sqlite/hooks/)
- [Hooks reference](/reference/hooks/)
