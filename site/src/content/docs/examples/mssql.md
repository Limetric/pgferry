---
title: MSSQL Examples
description: MSSQL examples for conservative and faster rebuild-oriented migrations.
---

MSSQL support is newer than MySQL in pgferry, but the examples already cover the two operational modes most teams need.

## Available examples

- [`minimal-safe`](https://github.com/Limetric/pgferry/tree/main/examples/mssql/minimal-safe)
- [`recreate-fast`](https://github.com/Limetric/pgferry/tree/main/examples/mssql/recreate-fast)

## MSSQL-specific considerations

- `source_schema` defaults to `dbo`
- `money` and `smallmoney` map to `numeric` by default
- `uniqueidentifier` values are reordered to standard UUID byte order
- `timestamp` and `rowversion` are treated as binary values, not datetimes

## Recommended baseline

Start with `minimal-safe` unless the target schema lifecycle is fully disposable and you have already validated object recreation end to end.
