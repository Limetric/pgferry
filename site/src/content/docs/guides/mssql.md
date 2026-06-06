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
- `single_tx` uses snapshot isolation: pgferry enables `ALLOW_SNAPSHOT_ISOLATION` only when `sys.databases` shows it is not already on and the login may `ALTER DATABASE`; if it is already on, read-only logins work. Any `ALTER` pgferry runs leaves that option enabled (it is not reverted).

## Common caveats

- `timestamp` and `rowversion` are binary values, not datetimes
- `datetime2` and `time` fractional precision from `sys.columns.scale` is mapped to PostgreSQL `timestamp(n)` / `timestamptz(n)` / `time(n)` with **n clamped to 6** (SQL Server’s maximum scale 7 becomes 6 on PostgreSQL)
- `uniqueidentifier` values are reordered to standard UUID byte order during copy
- computed columns are materialized as values and reported for manual semantic follow-up
- **Non–B-tree indexes** (columnstore, hash, XML, spatial) and **filtered** (predicate) indexes are skipped with warnings; only clustered/nonclustered B-tree shapes are recreated on PostgreSQL
- **External tables** (`sys.tables.is_external`) are excluded from migration by default; semantic warnings list each skipped name
- **System-versioned temporal tables** produce semantic warnings: current rows copy as a flat table; `SYSTEM_TIME`, history-table semantics, and retention are not recreated
- **Sequence-style defaults** (`NEXT VALUE FOR …`) are not translated to PostgreSQL; defaults are omitted and semantic warnings point you at sequences + hooks or manual DDL
- **`sql_variant`** columns are selected with `TRY_CAST(… AS nvarchar(max))` to avoid hard failures on some bases; values may become NULL or lose fidelity—see semantic warnings

## Operational caveats

- **Always Encrypted / column encryption**: pgferry does not configure keys or `column encryption setting`. Depending on the driver and server policy, encrypted columns may error or return unusable values. Use a DSN and client settings that match your SQL Server deployment (see Microsoft’s driver documentation).
- **Synonyms**: Introspection follows real objects in `source_schema`. `sys.synonyms` targets are not expanded; point the DSN/schema at underlying tables or use hooks.
- **Cross-schema foreign keys**: When a FK references another schema, pgferry logs a warning at introspection. The FK may fail in PostgreSQL if the referenced table is not in the target schema; adjust migration order, use hooks, or align schemas.
- **Azure SQL / connectivity**: Firewalls, TLS, and `encrypt` / trust settings are environment-specific; the config wizard validates DSN shape via `msdsn` but does not cover every hosting matrix.

## Migrating to a managed Postgres provider?

These destination-specific guides add the provider connection, TLS, pooling, and firewall setup on top of the MSSQL behavior above:

- [MSSQL to Supabase](/guides/mssql-to-supabase/)
- [MSSQL to Neon](/guides/mssql-to-neon/)
- [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/)
