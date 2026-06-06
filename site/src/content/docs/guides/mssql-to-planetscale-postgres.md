---
title: MSSQL to PlanetScale Postgres
description: An MSSQL to PlanetScale Postgres migration playbook — migrate SQL Server into PlanetScale for Postgres with pgferry, covering the direct port, verify-full TLS, and SQL Server type caveats.
---

This is an operator's playbook for an **MSSQL to PlanetScale Postgres** migration — moving a Microsoft SQL Server database into **PlanetScale for Postgres**, PlanetScale's PostgreSQL product (GA since September 2025), **not** PlanetScale's legacy MySQL/Vitess platform.

If you searched for how to **migrate SQL Server to PlanetScale Postgres** or move **MSSQL to PlanetScale for Postgres**, the short version is: point `pgferry` at PlanetScale's **direct port (5432)** with `sslmode=verify-full sslrootcert=system`, not the PSBouncer pooler, and let pgferry's `sys.*` catalog introspection handle the SQL Server types that generic tools botch.

## What this guide is for

Use this guide when your source is **Microsoft SQL Server** and your destination is **PlanetScale for Postgres**. Note that this is a real cross-engine MSSQL-to-PostgreSQL migration: PlanetScale for Postgres is genuine PostgreSQL on PlanetScale Metal, unrelated to PlanetScale's MySQL/Vitess product. For source-side behavior that is not PlanetScale-specific, read the generic [MSSQL to PostgreSQL guide](/guides/mssql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

PlanetScale's documented import path (`pg_dump`/`pg_restore`) is Postgres-to-Postgres only — it cannot read a SQL Server source. The generic cross-engine option, `pgloader`, has weak and largely unmaintained MSSQL support. `pgferry` is purpose-built:

- It introspects SQL Server via `sys.*` catalog views and applies SQL Server-specific conversions (UUID byte reordering, `datetime2`/`time` scale clamping, `money` → `numeric`).
- It streams with chunked, parallel `COPY`, checkpoints for `resume`, and runs a [`plan`](/operations/how-to-read-plan-output/) preflight.
- It creates objects as the connecting role, so PlanetScale's `--no-owner --no-privileges` ownership caveats (a `pg_dump` concern) simply do not arise.

## Destination prerequisites

- A PlanetScale for Postgres database and branch.
- A role's connection string from **Settings → Roles → View connection strings** (passwords are prefixed `pscale_pw_`). PlanetScale recommends a dedicated role over the default `postgres` role for ongoing app traffic.
- The `postgres` role is `NOSUPERUSER` but has `CREATEDB`/`CREATEROLE` and can create schemas, tables, indexes, FKs, sequences, and allow-listed extensions — everything pgferry needs.

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

`resume = true` requires `unlogged_tables = false`. On MSSQL, `source_snapshot_mode = "single_tx"` uses `SNAPSHOT` isolation — see the [MSSQL guide](/guides/mssql/).

## PlanetScale Postgres DSN, TLS, pooling, and firewall notes

| Endpoint | Port | Use for |
| --- | --- | --- |
| Direct Postgres | `5432` | **Migrations, DDL, bulk load** |
| PSBouncer (pooler) | `6432` | App runtime |

- **Use the direct port (5432).** PSBouncer runs transaction mode and lacks persistent prepared statements; PlanetScale's docs require the direct connection for schema changes, ETL/data streaming, and long or multi-statement transactions — what pgferry does.
- **TLS is required.** PlanetScale documents `sslmode=verify-full` with `sslrootcert=system` (certs chain to a public CA, so the OS trust store works) and `sslnegotiation=direct`.
- **Host shape:** `{identifier}-{region}-{instance}.horizon.psdb.cloud`. Read the exact host/port/user/dbname from the generated connection string.
- **IP restrictions** are opt-in per branch (default open); add your migration host's egress IP if enabled. Private connectivity via AWS PrivateLink / GCP Private Service Connect is available.
- **Capacity:** provision around **150% of the source size** on the target.

Example direct target DSN:

```bash
export PGFERRY_TARGET_DSN='postgres://postgres.<branch_id>:pscale_pw_<...>@<identifier>-<region>-1.horizon.psdb.cloud:5432/<dbname>?sslmode=verify-full&sslrootcert=system&sslnegotiation=direct'
export PGFERRY_SOURCE_DSN='sqlserver://user:pass@mssql-host:1433?database=source_db'
```

## Source-specific caveats (SQL Server)

These come from the MSSQL side (full detail in the [MSSQL guide](/guides/mssql/)):

- Choose the right `source_schema` instead of defaulting to `dbo` blindly.
- Decide `datetime_as_timestamptz`; keep `money_as_numeric = true` unless you want text.
- `datetime2`/`time` precision is clamped to PostgreSQL's max scale of **6** (SQL Server allows 7).
- `uniqueidentifier` values are byte-reordered into standard UUID order.
- Computed columns are materialized as values and reported for manual recreation.
- Non-B-tree indexes (columnstore, hash, XML, spatial) and filtered indexes are skipped with warnings.
- `NEXT VALUE FOR` defaults, system-versioned temporal tables, and `sql_variant` columns produce semantic warnings — handle via [hooks](/reference/hooks/) or manual DDL.
- External tables are excluded by default.

## Step-by-step MSSQL to PlanetScale Postgres migration flow

1. Create the PlanetScale Postgres database/branch and copy a role's **direct** (5432) connection string.
2. Pre-create any required extensions on the target (within PlanetScale's supported set).
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning.
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, triggers, and `NEXT VALUE FOR` defaults via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Verify computed columns and sequence-backed columns on the target.
- Confirm you connected on 5432, not 6432, throughout, and that required extensions installed.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Prepared-statement / session errors | Connected via PSBouncer (6432) | Use the direct port (5432) |
| TLS handshake / cert error | Missing verify-full params | Use `sslmode=verify-full&sslrootcert=system` |
| Connection refused after enabling IP restrictions | Migration host not allow-listed | Add your egress IP to the branch allowlist |
| `single_tx` fails on a read-only login | Snapshot isolation not enabled | Enable `ALLOW_SNAPSHOT_ISOLATION` or grant `ALTER` |
| Missing column defaults | `NEXT VALUE FOR` not translated | Recreate sequences/defaults via hooks |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MSSQL to PostgreSQL](/guides/mssql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MySQL to PlanetScale Postgres](/guides/mysql-to-planetscale-postgres/) · [MSSQL to Neon](/guides/mssql-to-neon/)
