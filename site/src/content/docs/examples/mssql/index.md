---
title: MSSQL Examples
description: MSSQL-to-PostgreSQL example playbooks for conservative and fast rebuild-oriented migrations.
---

MSSQL support currently has two main operational templates: the safe default and the disposable fast path.

## Start here

- [minimal-safe](/examples/mssql/minimal-safe/) for first production rehearsals
- [recreate-fast](/examples/mssql/recreate-fast/) for repeatable dev or staging rebuilds

## Notes before you choose

- `source_schema` defaults to `dbo`
- `single_tx` requires snapshot isolation on the source database
- `money` and `smallmoney` map to `numeric` by default
- `uniqueidentifier` values are reordered into standard UUID byte order during copy
