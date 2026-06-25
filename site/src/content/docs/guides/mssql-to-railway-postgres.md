---
title: MSSQL to Railway Postgres
description: MSSQL to Railway Postgres playbook — pgferry moves SQL Server to a Railway PostgreSQL service via the public TCP proxy, self-signed TLS, and sys.* conversions.
---

This is an operator's playbook for an **MSSQL to Railway Postgres** migration. It covers the Railway-specific connection details — the public TCP proxy, self-signed TLS, and private networking — plus the SQL Server type caveats that get Microsoft SQL Server into a [Railway](https://railway.com/) PostgreSQL service without surprises.

If you searched for how to **migrate SQL Server to Railway Postgres** or move **MSSQL to Railway**, the short version is: use Railway's **public proxy URL** (`DATABASE_PUBLIC_URL`) with `sslmode=require` from an external host, run the migration **inside Railway** to avoid egress charges when you can, and let pgferry's `sys.*` catalog introspection handle the SQL Server types generic tools botch.

## What this guide is for

Use this guide when you have a Microsoft SQL Server database (on-prem, Azure SQL, AWS RDS for SQL Server, etc.) and want it on a Railway-hosted PostgreSQL service. It assumes you have added a Postgres database to a Railway project. For source-side behavior that is not Railway-specific, read the generic [MSSQL to PostgreSQL guide](/guides/mssql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

For SQL Server, the generic advice is especially weak: `pgloader`'s MSSQL support is thin and largely unmaintained, and most tutorials assume MySQL. `pgferry` is built for this pair:

- It introspects SQL Server through `sys.*` catalog views and applies SQL Server-specific conversions (UUID byte reordering, `datetime2`/`time` scale clamping, `money` → `numeric`).
- It streams with chunked, parallel `COPY` and **resumes** from a checkpoint — important over Railway's TCP proxy.
- `pgferry plan` reports computed columns, skipped non-B-tree/filtered indexes, temporal tables, and `NEXT VALUE FOR` defaults before PostgreSQL is touched. See [how to read plan output](/operations/how-to-read-plan-output/).
- It creates objects as the connecting role, sidestepping ownership/`SET ROLE` errors that `pg_dump`-style restores hit.

## Destination prerequisites

- A Railway project with a Postgres service. Default database is `railway`, default user is `postgres`.
- The connection variables from the service's **Variables** tab. Railway's Postgres image runs as a single-tenant container, so the `postgres` role has broad privileges — `CREATE SCHEMA`, `CREATE EXTENSION` (for extensions whose binaries ship in the image), etc.
- For pgvector or other non-default extensions, deploy the matching Railway template/image variant — the base image only carries the standard contrib set.

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

`resume = true` requires `unlogged_tables = false`. On MSSQL, `source_snapshot_mode = "single_tx"` uses `SNAPSHOT` isolation — see the [MSSQL guide](/guides/mssql/) for the `ALLOW_SNAPSHOT_ISOLATION` details (and the [Azure SQL to Neon guide](/guides/azure-sql-to-neon/) if your source is Azure SQL).

## Railway DSN, TLS, proxy, and networking notes

Railway exposes two connection URLs on the Postgres service:

| Variable | Host shape | Use from |
| --- | --- | --- |
| `DATABASE_URL` (private) | `postgres.railway.internal:5432` | Another service **inside** the same Railway project |
| `DATABASE_PUBLIC_URL` (public) | `<name>.proxy.rlwy.net:<random-port>` | An **external** migration host |

- **From your laptop or any external host, use `DATABASE_PUBLIC_URL`.** The `.railway.internal` host is not routable outside Railway. The public proxy port is **randomly assigned** — never hardcode `5432` for the external endpoint; read it from the variable.
- **TLS:** Railway's Postgres image uses a **self-signed** certificate. Use `?sslmode=require` (encrypts without certificate-chain verification). `sslmode=verify-full` fails because Railway does not publish a CA for the auto-generated cert.
- **Egress:** traffic through the public proxy leaves Railway's network and **counts toward egress/network billing**. A multi-GB migration over the proxy can add up.
- **Private networking:** if you run pgferry as a service **inside the same Railway project**, use `DATABASE_URL` (the internal host) — no egress charges, lower latency. The private network is IPv6-based; the `pgx` driver pgferry uses handles this as long as the host resolves.

Example external (public proxy) target DSN, with an MSSQL source DSN:

```bash
export PGFERRY_TARGET_DSN='postgres://postgres:<password>@<name>.proxy.rlwy.net:<proxy-port>/railway?sslmode=require'
export PGFERRY_SOURCE_DSN='sqlserver://user:pass@mssql-host:1433?database=source_db'
```

Inside Railway (same project, no egress):

```bash
export PGFERRY_TARGET_DSN='postgres://postgres:<password>@postgres.railway.internal:5432/railway?sslmode=require'
```

### Capacity gotcha

The Railway Postgres service is backed by a volume with a plan-dependent size cap, and WAL plus indexes built during the load consume extra space transiently. Pre-size or upgrade the volume before a large import so you do not run out mid-load.

## Source-specific caveats (SQL Server)

These come from the MSSQL side (full detail in the [MSSQL guide](/guides/mssql/)):

- Choose the right `source_schema` instead of relying on `dbo` blindly.
- Decide `datetime_as_timestamptz`; keep `money_as_numeric = true` unless you want text preservation.
- `datetime2`/`time` fractional precision is clamped to PostgreSQL's max scale of **6** (SQL Server allows 7).
- `uniqueidentifier` values are byte-reordered into standard UUID order.
- Computed columns are materialized as values and reported for manual recreation.
- Non-B-tree indexes (columnstore, hash, XML, spatial) and filtered indexes are skipped with warnings.
- `NEXT VALUE FOR` defaults, system-versioned temporal tables, and `sql_variant` columns produce semantic warnings — handle via [hooks](/reference/hooks/) or manual DDL.

## Step-by-step MSSQL to Railway Postgres migration flow

1. Add Postgres to your Railway project and copy `DATABASE_PUBLIC_URL` (or use the internal URL if running inside Railway).
2. Confirm the volume is large enough for the dataset plus index/WAL overhead.
3. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
4. Run `pgferry plan migration.toml` and resolve every warning (computed columns, skipped indexes, sequence defaults, temporal tables).
5. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
6. Recreate views, routines, triggers, and `NEXT VALUE FOR` defaults via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions exist (and that you used the right image variant for pgvector etc.).
- Verify computed columns and sequence-backed columns on the target.
- Verify the volume has headroom after the load.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| `could not translate host name "postgres.railway.internal"` | Used the private host from outside Railway | Use `DATABASE_PUBLIC_URL` |
| TLS / certificate verification error | `verify-full` against a self-signed cert | Use `sslmode=require` |
| Connection refused on `:5432` externally | Hardcoded port instead of proxy port | Use the random proxy port from `DATABASE_PUBLIC_URL` |
| `single_tx` fails on a read-only login | Snapshot isolation not enabled on the source | Enable `ALLOW_SNAPSHOT_ISOLATION` (see [MSSQL guide](/guides/mssql/)) |
| `No space left on device` mid-load | Volume too small for data + indexes | Pre-size/upgrade the volume |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MSSQL to PostgreSQL](/guides/mssql/) — generic SQL Server source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MSSQL to Render Postgres](/guides/mssql-to-render-postgres/) · [MSSQL to Supabase](/guides/mssql-to-supabase/) · [MSSQL to Neon](/guides/mssql-to-neon/) · [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/) · [Azure SQL to Neon](/guides/azure-sql-to-neon/)
