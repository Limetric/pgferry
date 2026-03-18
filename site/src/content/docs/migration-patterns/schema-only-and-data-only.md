---
title: Schema-only And Data-only
description: Split the pgferry workflow when you need DDL review or tighter cutover sequencing.
---

Use the split-phase workflow when you need schema creation and data loading to happen at different times.

## `schema_only`

Use this when you want PostgreSQL tables, keys, and indexes created before any data moves.

## `data_only`

Use this when the target schema already exists and you only need to stream data plus reset sequences.

## Tradeoff

This is slower than the default full pipeline because data loads into a schema that already has more objects in place.

## Start from these examples

- [MySQL schema-only](/examples/mysql/schema-only/)
- [MySQL data-only](/examples/mysql/data-only/)
- [SQLite schema-only](/examples/sqlite/schema-only/)
- [SQLite data-only](/examples/sqlite/data-only/)
