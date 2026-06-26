---
title: MSSQL to Render Postgres
description: MSSQL to Render Postgres playbook — pgferry moves SQL Server to a Render PostgreSQL instance via the External URL, required TLS, and IP allowlists.
---

This is an operator's playbook for an **MSSQL to Render Postgres** migration. It covers the Render-specific connection details — external vs internal URLs, required TLS, and the inbound IP allowlist — plus the SQL Server type caveats that get Microsoft SQL Server onto a [Render](https://render.com/) PostgreSQL instance in one piece.

If you searched for how to **migrate SQL Server to Render Postgres** or move **MSSQL to Render**, the short version is: connect from an external host with Render's **External Database URL** and `sslmode=require`, allow your migration host in the inbound IP rules, never load production data into a Free instance, and let pgferry's `sys.*` catalog introspection handle the SQL Server types generic tools botch.

## What this guide is for

Use this guide when you have a Microsoft SQL Server database (on-prem, Azure SQL, AWS RDS for SQL Server, etc.) and want it on a Render-hosted PostgreSQL instance. It assumes you have created a Render Postgres instance. For source-side behavior that is not Render-specific, read the generic [MSSQL to PostgreSQL guide](/guides/mssql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

For SQL Server, the generic advice is especially weak: `pgloader`'s MSSQL support is thin and largely unmaintained, and most tutorials assume MySQL. `pgferry` is built for this pair:

- It introspects SQL Server through `sys.*` catalog views and applies SQL Server-specific conversions (UUID byte reordering, `datetime2`/`time` scale clamping, `money` → `numeric`).
- It streams with chunked, parallel `COPY` and **resumes** from a checkpoint.
- `pgferry plan` reports computed columns, skipped non-B-tree/filtered indexes, temporal tables, and `NEXT VALUE FOR` defaults before PostgreSQL is touched. See [how to read plan output](/operations/how-to-read-plan-output/).
- It creates objects as the connecting role — no ownership/`SET ROLE` errors against Render's non-superuser role.

## Destination prerequisites

- A Render Postgres instance. Render auto-generates the database name and user at creation, and these are **immutable** afterward — read them from the instance's **Connect** menu.
- **Do not use a Free instance for production data.** Free Postgres instances expire 30 days after creation and are capped at 1 GB with no backups. Use a paid instance type sized for your dataset.
- Render's role is **not a superuser** but owns its database — it can `CREATE SCHEMA`, create tables/indexes/FKs/sequences, and `CREATE EXTENSION` for Render's allow-listed extensions. That covers everything pgferry needs.

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

`resume = true` requires `unlogged_tables = false`. On MSSQL, `source_snapshot_mode = "single_tx"` uses `SNAPSHOT` isolation — see the [MSSQL guide](/guides/mssql/) for the `ALLOW_SNAPSHOT_ISOLATION` details (and the [Azure SQL to Supabase guide](/guides/azure-sql-to-supabase/) if your source is Azure SQL).

## Render DSN, TLS, and firewall notes

Render gives every instance two URLs:

| URL | Host shape | Use from |
| --- | --- | --- |
| External Database URL | `dpg-<id>-a.<region>-postgres.render.com:5432` | An **external** migration host |
| Internal Database URL | `dpg-<id>-a` (private) | A Render service in the **same region** |

- **From your laptop or any external host, use the External Database URL.** Only a service running inside Render (same region) can use the internal host.
- **TLS is required for external connections.** Append `?sslmode=require`. Render manages the certificate; `sslmode=require` encrypts in transit (the documented/safe setting — it does not validate the chain).
- **Inbound IP allowlist:** by default a Render Postgres instance is reachable from any IP (`0.0.0.0/0`), gated only by credentials + SSL. If you have tightened the inbound rules, **add your migration host's IP** (e.g. `203.0.113.45/32`) under the instance's **Networking** section before starting. If a dynamic IP makes that impractical, temporarily widen the rule for the load and tighten it afterward. The allowlist applies only to the external URL — internal connections are always allowed.
- **Connection limits** scale with instance RAM (100 connections below 8 GB, 200 at 8–16 GB, and up). Keep pgferry's `workers`/`index_workers` within the instance's ceiling (see the [configuration reference](/reference/configuration/)).

Example external target DSN, with an MSSQL source DSN:

```bash
export PGFERRY_TARGET_DSN='postgres://<user>:<password>@dpg-<id>-a.<region>-postgres.render.com:5432/<dbname>?sslmode=require'
export PGFERRY_SOURCE_DSN='sqlserver://user:pass@mssql-host:1433?database=source_db'
```

## Source-specific caveats (SQL Server)

These come from the MSSQL side (full detail in the [MSSQL guide](/guides/mssql/)):

- Choose the right `source_schema` instead of relying on `dbo` blindly.
- Decide `datetime_as_timestamptz`; keep `money_as_numeric = true` unless you want text preservation.
- `datetime2`/`time` fractional precision is clamped to PostgreSQL's max scale of **6** (SQL Server allows 7).
- `uniqueidentifier` values are byte-reordered into standard UUID order.
- Computed columns are materialized as values and reported for manual recreation.
- Non-B-tree indexes (columnstore, hash, XML, spatial) and filtered indexes are skipped with warnings.
- `NEXT VALUE FOR` defaults, system-versioned temporal tables, and `sql_variant` columns produce semantic warnings — handle via [hooks](/reference/hooks/) or manual DDL.

## Step-by-step MSSQL to Render Postgres migration flow

1. Create a **paid** Render Postgres instance sized for the dataset; copy the External Database URL.
2. Add your migration host's IP to the inbound rules if you have restricted them; choose an instance plan with enough disk for your dataset plus index/WAL overhead (increase the disk before the load if needed).
3. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
4. Run `pgferry plan migration.toml` and resolve every warning (computed columns, skipped indexes, sequence defaults, temporal tables).
5. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
6. Recreate views, routines, triggers, and `NEXT VALUE FOR` defaults via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions are enabled (`CREATE EXTENSION` for anything `plan` flagged).
- Verify computed columns and sequence-backed columns on the target.
- Always load into the **primary**, never a read replica.
- Tighten the inbound IP allowlist after the load if you widened it.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Connection times out from your host | Inbound IP not allow-listed | Add your IP to the instance's inbound rules |
| `no encryption` / SSL required | Missing TLS | Append `?sslmode=require` |
| Can't resolve the `dpg-...-a` host | Used the internal URL externally | Use the External Database URL |
| Data gone after a month | Loaded into a Free instance | Use a paid instance for production |
| `single_tx` fails on a read-only login | Snapshot isolation not enabled on the source | Enable `ALLOW_SNAPSHOT_ISOLATION` (see [MSSQL guide](/guides/mssql/)) |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MSSQL to PostgreSQL](/guides/mssql/) — generic SQL Server source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MSSQL to Railway Postgres](/guides/mssql-to-railway-postgres/) · [MSSQL to Supabase](/guides/mssql-to-supabase/) · [MSSQL to Neon](/guides/mssql-to-neon/) · [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/) · [Azure SQL to Supabase](/guides/azure-sql-to-supabase/)
