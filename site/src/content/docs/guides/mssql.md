---
title: MSSQL To PostgreSQL
description: MSSQL-specific pgferry behavior, caveats, and example starting points.
---

MSSQL support uses `sys.*` catalog introspection and a small set of SQL Server-specific conversions that are worth understanding up front.

## Start here

- [minimal-safe example](/examples/mssql/minimal-safe/)
- [recreate-fast example](/examples/mssql/recreate-fast/)

## MSSQL-specific decisions

- choose the right `source_schema` instead of relying on the `dbo` default blindly
- decide whether `datetime_as_timestamptz` should be enabled
- keep `money_as_numeric = true` unless you explicitly want text preservation
- enable `single_tx` only if snapshot isolation is available on the source database

## Common caveats

- `timestamp` and `rowversion` are binary values, not datetimes
- `uniqueidentifier` values are reordered to standard UUID byte order during copy
- computed columns are materialized as values and reported for manual semantic follow-up
- filtered, XML, and spatial indexes are reported instead of recreated automatically
