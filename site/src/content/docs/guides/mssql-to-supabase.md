---
title: MSSQL to Supabase
description: An MSSQL to Supabase migration playbook — migrate SQL Server to Supabase Postgres with pgferry, covering the session pooler, TLS, statement timeouts, and SQL Server type caveats.
---

This is an operator's playbook for an **MSSQL to Supabase** migration. It covers the Supabase-specific connection and timeout setup plus the SQL Server type caveats you need to move Microsoft SQL Server into [Supabase](https://supabase.com/)'s hosted PostgreSQL.

If you searched for how to **migrate SQL Server to Supabase** or move **MSSQL to Supabase Postgres**, the short version is: point `pgferry` at a session-mode (not transaction-mode) Supabase connection, raise the `postgres` role statement timeout for the load, and let pgferry's `sys.*` catalog introspection handle the SQL Server types that no `pgloader` recipe covers well.

## What this guide is for

Use this guide when you have a Microsoft SQL Server database (on-prem, Azure SQL, AWS RDS for SQL Server, etc.) and want it on Supabase Postgres. It assumes you have a Supabase project. For source-side behavior that is not Supabase-specific, read the generic [MSSQL to PostgreSQL guide](/guides/mssql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

For SQL Server, the generic advice is especially weak: `pgloader`'s MSSQL support is thin and largely unmaintained, and most tutorials assume MySQL. `pgferry` is built for this pair:

- It introspects SQL Server through `sys.*` catalog views and applies SQL Server-specific conversions (UUID byte reordering, `datetime2`/`time` scale clamping, `money` → `numeric`).
- It streams data with chunked, parallel `COPY` and **resumes** from a checkpoint — important over a hosted connection.
- `pgferry plan` reports computed columns, skipped non-B-tree/filtered indexes, temporal tables, and `NEXT VALUE FOR` defaults *before* PostgreSQL is touched. See [how to read plan output](/operations/how-to-read-plan-output/).
- It creates objects as the connecting role, sidestepping the ownership/`SET ROLE` errors a `pg_dump`-style restore hits against Supabase's non-superuser role.

## Destination prerequisites

- A Supabase project (note its **project ref**) and the database password from **Project Settings → Database**.
- Decide the target schema. SQL Server commonly uses `dbo`; pgferry can create and own any PostgreSQL schema name.
- The Supabase `postgres` role is **not a superuser** but can create schemas, tables, indexes, FKs, sequences, and allow-listed extensions — everything pgferry needs.

## Recommended pgferry config

```toml
schema = "app"
on_schema_exists = "error"
unlogged_tables = false
resume = true
validation = "row_count"
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

## Supabase DSN, TLS, pooling, and firewall notes

Supabase exposes three connection types. Database name is always `postgres`.

| Type | Host | Port | Username |
| --- | --- | --- | --- |
| Direct | `db.<ref>.supabase.co` | `5432` | `postgres` |
| Session pooler (Supavisor) | `aws-0-<region>.pooler.supabase.com` | `5432` | `postgres.<ref>` |
| Transaction pooler (Supavisor) | `aws-0-<region>.pooler.supabase.com` | `6543` | `postgres.<ref>` |

- **Use the session pooler (5432) or the direct connection.** Both keep prepared statements and session state that pgferry's `COPY` and DDL pipeline need.
- **Never use the transaction pooler (6543) for a migration** — transaction mode disables prepared statements and drops session settings.
- **IPv4-only host?** The direct connection is IPv6-only without the paid IPv4 add-on; the **session pooler is IPv4-native**, so prefer it. Copy the exact host from the dashboard **Connect** dialog.
- **TLS:** use `?sslmode=require`, or `sslmode=verify-full&sslrootcert=...` with the CA cert from **Project Settings → Database → SSL Configuration**.

Example session-pooler target DSN:

```bash
export PGFERRY_TARGET_DSN='postgresql://postgres.<ref>:<password>@aws-0-<region>.pooler.supabase.com:5432/postgres?sslmode=require'
export PGFERRY_SOURCE_DSN='sqlserver://user:pass@mssql-host:1433?database=source_db'
```

### Statement timeout — the most common Supabase migration failure

Supabase caps the `postgres` role at a **2-minute statement timeout** by default. A large `COPY` chunk or index build dies with `canceling statement due to statement timeout`. Disable it for the load, then restore:

```sql
alter role postgres set statement_timeout = '0';   -- before
alter role postgres reset statement_timeout;        -- after cutover
```

Reconnect for it to take effect.

## Source-specific caveats (SQL Server)

These come from the MSSQL side (full detail in the [MSSQL guide](/guides/mssql/)):

- Choose the right `source_schema` instead of relying on `dbo` blindly.
- Decide `datetime_as_timestamptz`; keep `money_as_numeric = true` unless you want text preservation.
- `datetime2`/`time` fractional precision is clamped to PostgreSQL's max scale of **6** (SQL Server allows 7).
- `uniqueidentifier` values are byte-reordered into standard UUID order.
- Computed columns are materialized as values and reported for manual recreation.
- Non-B-tree indexes (columnstore, hash, XML, spatial) and filtered indexes are skipped with warnings.
- `NEXT VALUE FOR` sequence defaults, system-versioned temporal tables, and `sql_variant` columns produce semantic warnings — recreate via [hooks](/reference/hooks/) or manual DDL.
- External tables are excluded by default.

## Step-by-step MSSQL to Supabase migration flow

1. Create the Supabase project and copy the session-pooler connection string from **Connect**.
2. `alter role postgres set statement_timeout = '0';`.
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning (computed columns, skipped indexes, sequence defaults, temporal tables).
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, triggers, and `NEXT VALUE FOR` defaults via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Verify computed columns and sequence-backed columns behave correctly on the target.
- Confirm required extensions are enabled in **Database → Extensions**.
- Restore the `postgres` role `statement_timeout`.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| `canceling statement due to statement timeout` | Supabase 2-min role timeout | `alter role postgres set statement_timeout = '0'` |
| `prepared statement ... does not exist` | Connected via transaction pooler (6543) | Use session pooler (5432) or direct |
| `could not translate host name` | Direct host is IPv6-only | Use the session pooler or enable the IPv4 add-on |
| `single_tx` fails on a read-only login | Snapshot isolation not enabled | Enable `ALLOW_SNAPSHOT_ISOLATION` or grant `ALTER` (see [MSSQL guide](/guides/mssql/)) |
| Missing defaults on some columns | `NEXT VALUE FOR` not translated | Recreate sequences/defaults via hooks |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MSSQL to PostgreSQL](/guides/mssql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MSSQL to Neon](/guides/mssql-to-neon/) · [MySQL to Supabase](/guides/mysql-to-supabase/)
