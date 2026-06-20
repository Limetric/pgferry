---
title: Hooks-driven Migrations
description: Use pgferry SQL hook phases to create extensions, run ANALYZE, clean orphans, and recreate views and routines around the built-in pipeline.
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
