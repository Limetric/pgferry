---
title: MSSQL Examples
description: MSSQL-to-PostgreSQL examples covering safe and fast-rebuild templates, dbo schema defaults, money-to-numeric, and uniqueidentifier handling.
---

MSSQL comes with two go-to templates: the safe default and the disposable fast path.

## Start here

- [minimal-safe](/examples/mssql/minimal-safe/) for first production rehearsals
- [recreate-fast](/examples/mssql/recreate-fast/) for repeatable dev or staging rebuilds

## Notes before you choose

- `source_schema` defaults to `dbo`
- `single_tx` enables snapshot isolation: pgferry may `ALTER` the database only when `ALLOW_SNAPSHOT_ISOLATION` is not already on; that change persists if pgferry makes it
- `money` and `smallmoney` map to `numeric` by default
- `uniqueidentifier` values are reordered into standard UUID byte order during copy

## Migrating to a specific provider?

Provider-specific playbooks with connection, TLS, pooling, and firewall setup:

- [MSSQL to Supabase](/guides/mssql-to-supabase/)
- [MSSQL to Neon](/guides/mssql-to-neon/)
- [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/)
