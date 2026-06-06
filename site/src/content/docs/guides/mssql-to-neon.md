---
title: MSSQL to Neon
description: An MSSQL to Neon migration playbook — migrate SQL Server to Neon serverless Postgres with pgferry, covering the direct endpoint, scale-to-zero, TLS, and SQL Server type caveats.
---

This is an operator's playbook for an **MSSQL to Neon** migration. It covers the Neon-specific endpoint, scale-to-zero, and TLS details plus the SQL Server type caveats you need to move Microsoft SQL Server into [Neon](https://neon.com/)'s serverless PostgreSQL.

If you searched for how to **migrate SQL Server to Neon** or move **MSSQL to Neon Postgres**, the short version is: point `pgferry` at Neon's **unpooled (direct)** endpoint, disable scale-to-zero for the load, and let pgferry's `sys.*` catalog introspection handle the SQL Server types that generic tools botch.

## What this guide is for

Use this guide when you have a Microsoft SQL Server database (on-prem, Azure SQL, AWS RDS for SQL Server, etc.) and want it on Neon Postgres. It assumes you have a Neon project and branch. For source-side behavior that is not Neon-specific, read the generic [MSSQL to PostgreSQL guide](/guides/mssql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

For SQL Server, generic advice is thin: `pgloader`'s MSSQL path is largely unmaintained and most tutorials assume MySQL. `pgferry` is built for this pair:

- It introspects SQL Server through `sys.*` catalog views and applies SQL Server-specific conversions (UUID byte reordering, `datetime2`/`time` scale clamping, `money` → `numeric`).
- It streams with chunked, parallel `COPY` and **resumes** from a checkpoint — important over a Neon connection that can auto-suspend.
- `pgferry plan` reports computed columns, skipped non-B-tree/filtered indexes, temporal tables, and `NEXT VALUE FOR` defaults before PostgreSQL is touched.
- It creates objects as the connecting role, avoiding the ownership/`SET ROLE` errors a `pg_dump`-style restore hits against Neon's non-superuser role.

## Destination prerequisites

- A Neon project and branch. Note the default database (`neondb`) and owner role (`neondb_owner`), or create your own.
- The connection string from the Neon console (**Connect**), which gives both pooled and direct host forms.
- Neon's owner role is a member of `neon_superuser` — not a true superuser, but it can create schemas, tables, indexes, FKs, sequences, and allow-listed extensions. That covers pgferry's needs.

## Recommended pgferry config

```toml
schema = "app"
on_schema_exists = "error"
unlogged_tables = false
resume = true
validation = "row_count"
chunk_size = 100000
source_snapshot_mode = "single_tx"

[source]
type = "mssql"
source_schema = "dbo"
# dsn supplied via PGFERRY_SOURCE_DSN

[target]
# dsn supplied via PGFERRY_TARGET_DSN

[type_mapping]
datetime_as_timestamptz = false
money_as_numeric = true
```

`resume = true` requires `unlogged_tables = false`. On MSSQL, `source_snapshot_mode = "single_tx"` uses `SNAPSHOT` isolation — see the [MSSQL guide](/guides/mssql/) for the `ALLOW_SNAPSHOT_ISOLATION` details.

## Neon DSN, TLS, pooling, and firewall notes

Neon endpoints differ only by a `-pooler` suffix:

| Endpoint | Host shape | Use for |
| --- | --- | --- |
| Direct (unpooled) | `ep-<id>.<region>.aws.neon.tech` | **Migrations, DDL, bulk load** |
| Pooled | `ep-<id>-pooler.<region>.aws.neon.tech` | App runtime |

- **Use the direct (unpooled) endpoint.** The pooled endpoint is PgBouncer in transaction mode and breaks session-scoped DDL and the session features pgferry relies on.
- **TLS is mandatory.** Neon rejects non-TLS connections. Use `?sslmode=require` at minimum; `verify-full` works against the system trust store. Neon's console strings also include `channel_binding=require`, supported by the `pgx` driver pgferry uses.
- **IP Allow** is a paid-plan feature, default open. If enabled, add your migration host's egress IP/CIDR first.

Example direct-endpoint target DSN:

```bash
export PGFERRY_TARGET_DSN='postgresql://neondb_owner:<password>@ep-<id>.<region>.aws.neon.tech/neondb?sslmode=require'
export PGFERRY_SOURCE_DSN='sqlserver://user:pass@mssql-host:1433?database=source_db'
```

### Scale-to-zero — the Neon-specific gotcha

Neon computes auto-suspend after inactivity (5 minutes by default; fixed on Free). **Disable scale-to-zero (or raise the timeout) for the migration window** in **Branches → compute → Edit**, then re-enable it after. For large datasets, raise the compute size for more `max_connections` and index-build headroom. Keep transactions moving so the 5-minute `idle_in_transaction_session_timeout` does not terminate one mid-load.

## Source-specific caveats (SQL Server)

These come from the MSSQL side (full detail in the [MSSQL guide](/guides/mssql/)):

- Choose the right `source_schema` instead of defaulting to `dbo` blindly.
- Decide `datetime_as_timestamptz`; keep `money_as_numeric = true` unless you want text.
- `datetime2`/`time` fractional precision is clamped to PostgreSQL's max scale of **6** (SQL Server allows 7).
- `uniqueidentifier` values are byte-reordered into standard UUID order.
- Computed columns are materialized as values and reported for manual recreation.
- Non-B-tree indexes (columnstore, hash, XML, spatial) and filtered indexes are skipped with warnings.
- `NEXT VALUE FOR` defaults, system-versioned temporal tables, and `sql_variant` columns produce semantic warnings — handle via [hooks](/reference/hooks/) or manual DDL.
- External tables are excluded by default.

## Step-by-step MSSQL to Neon migration flow

1. Create the Neon project/branch and copy the **direct** connection string (no `-pooler`).
2. Disable scale-to-zero and, for large data, raise the compute size.
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning.
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, triggers, and `NEXT VALUE FOR` defaults via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Verify computed columns and sequence-backed columns on the target.
- Confirm required extensions exist.
- Re-enable scale-to-zero and restore the compute size if you changed it.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Session/DDL errors, temp-table failures | Connected via the `-pooler` endpoint | Use the direct (unpooled) endpoint |
| Connection refused / SSL required | Missing TLS | Append `?sslmode=require` |
| Compute suspended mid-load | Scale-to-zero fired during a quiet gap | Disable scale-to-zero for the load |
| `single_tx` fails on a read-only login | Snapshot isolation not enabled | Enable `ALLOW_SNAPSHOT_ISOLATION` or grant `ALTER` (see [MSSQL guide](/guides/mssql/)) |
| Missing column defaults | `NEXT VALUE FOR` not translated | Recreate sequences/defaults via hooks |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MSSQL to PostgreSQL](/guides/mssql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MSSQL to Supabase](/guides/mssql-to-supabase/) · [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/) · [MySQL to Neon](/guides/mysql-to-neon/)
