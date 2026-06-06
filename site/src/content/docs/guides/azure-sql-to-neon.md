---
title: Azure SQL to Neon
description: An Azure SQL to Neon migration playbook — move Azure SQL Database into Neon serverless Postgres with pgferry, covering the Azure firewall, encrypt=true TLS, snapshot isolation, the direct endpoint, and scale-to-zero.
---

This is an operator's playbook for an **Azure SQL to Neon** migration. It covers the Azure SQL Database side — server-level firewall rules, mandatory encryption, and the snapshot-isolation setting `single_tx` relies on — together with the Neon endpoint and scale-to-zero details you need to land [Azure SQL Database](https://learn.microsoft.com/en-us/azure/azure-sql/database/) on [Neon](https://neon.com/)'s serverless PostgreSQL.

If you searched for how to **migrate Azure SQL to Neon** or move **Azure SQL Database to Neon Postgres**, the short version is: open the Azure server firewall to your migration host, connect with `encrypt=true`, enable `ALLOW_SNAPSHOT_ISOLATION` for a consistent read, point `pgferry` at Neon's **unpooled (direct)** endpoint, and disable scale-to-zero for the load.

## What this guide is for

Use this guide when your source is **Azure SQL Database** (the managed PaaS offering on `*.database.windows.net`) and your destination is Neon Postgres. Azure SQL Database is SQL Server's T-SQL engine, so this is a real cross-engine SQL Server → PostgreSQL migration. For source-side type behavior that is not Azure-specific, read the generic [MSSQL to PostgreSQL guide](/guides/mssql/) alongside this page. It assumes you have a Neon project and branch.

## Why use pgferry instead of generic pgloader advice

For SQL Server sources, generic advice is thin: `pgloader`'s MSSQL path is largely unmaintained and most tutorials assume MySQL. `pgferry` is built for this pair:

- It introspects Azure SQL through `sys.*` catalog views and applies SQL Server-specific conversions (UUID byte reordering, `datetime2`/`time` scale clamping, `money` → `numeric`).
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

`resume = true` requires `unlogged_tables = false`. `source_snapshot_mode = "single_tx"` uses SQL Server `SNAPSHOT` isolation — see the Azure snapshot note below.

## Azure SQL source connection, TLS, and firewall notes

Azure SQL Database is reached on `<server>.database.windows.net:1433`. pgferry uses the `go-mssqldb` driver, so the source DSN is a `sqlserver://` URL:

```bash
export PGFERRY_SOURCE_DSN='sqlserver://<user>:<password>@<server>.database.windows.net:1433?database=<db>&encrypt=true'
```

- **Encryption is mandatory.** Azure SQL refuses unencrypted connections — keep `encrypt=true`. Azure presents a certificate that chains to a public CA, so you do **not** need `TrustServerCertificate=true`; leaving it off gives you real verification.
- **Server-level firewall.** Azure SQL blocks all client IPs by default. In the portal under **Networking → Firewall rules** (or via `sp_set_firewall_rule`), add a server-level rule for your migration host's **public** IP before starting. The "Allow Azure services and resources to access this server" toggle (the `0.0.0.0` rule) only helps if you run pgferry from inside Azure.
- **Login form.** The `sqlserver://` URL takes the bare login as the username. Some SQL Server tools want the `<login>@<server>` form instead — that is a tool quirk, not something the `go-mssqldb` URL needs.
- **Read scale-out.** If your tier offers a read-only replica (`ApplicationIntent=ReadOnly`), point the migration there to keep load off the primary.

### Snapshot isolation — the Azure-specific gotcha

`single_tx` reads everything in one `SNAPSHOT`-isolation transaction. On Azure SQL Database, `READ_COMMITTED_SNAPSHOT` is on by default, but **`ALLOW_SNAPSHOT_ISOLATION` is not** — and `SNAPSHOT` isolation requires it. Enable it once ahead of the migration with a login that can `ALTER DATABASE`:

```sql
ALTER DATABASE [<db>] SET ALLOW_SNAPSHOT_ISOLATION ON;
```

pgferry will try to enable it automatically if the login has permission, but Azure SQL logins are often least-privilege, so set it yourself. Once on, a read-only login can run the snapshot. (See the [MSSQL guide](/guides/mssql/) for the full behavior.)

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
```

### Scale-to-zero — the Neon-specific gotcha

Neon computes auto-suspend after inactivity (5 minutes by default; fixed on Free). **Disable scale-to-zero (or raise the timeout) for the migration window** in **Branches → compute → Edit**, then re-enable it after. For large datasets, raise the compute size for more `max_connections` and index-build headroom. Keep transactions moving so the 5-minute `idle_in_transaction_session_timeout` does not terminate one mid-load.

## Source-specific caveats (Azure SQL / SQL Server)

These come from the SQL Server side (full detail in the [MSSQL guide](/guides/mssql/)):

- Choose the right `source_schema` instead of defaulting to `dbo` blindly.
- Decide `datetime_as_timestamptz`; `datetime` and `datetime2` carry no zone, so keeping it `false` (→ `timestamp`) is usually correct.
- `datetime2`/`time` fractional precision is clamped to PostgreSQL's max scale of **6** (SQL Server allows 7).
- `uniqueidentifier` values are byte-reordered into standard UUID order.
- Computed (and persisted computed) columns are materialized as values and reported for manual recreation — Azure schemas lean on these heavily.
- Non-B-tree indexes (columnstore, hash, XML, spatial) and filtered indexes are skipped with warnings.
- `NEXT VALUE FOR` defaults, system-versioned temporal tables, and `sql_variant` columns produce semantic warnings — handle via [hooks](/reference/hooks/) or manual DDL.

## Step-by-step Azure SQL to Neon migration flow

1. Add your migration host's IP to the Azure server firewall and confirm `encrypt=true` connects.
2. `ALTER DATABASE [<db>] SET ALLOW_SNAPSHOT_ISOLATION ON;` so `single_tx` works.
3. Create the Neon project/branch, copy the **direct** connection string, and disable scale-to-zero (raise the compute size for large data).
4. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning (computed columns, skipped indexes, sequence defaults, temporal tables).
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
| `Cannot open server ... requested by the login` | Migration host IP not in the Azure firewall | Add a server-level firewall rule for your IP |
| TLS / login fails on connect | `encrypt` not set | Use `encrypt=true` in the `sqlserver://` DSN |
| `single_tx` fails / snapshot isolation error | `ALLOW_SNAPSHOT_ISOLATION` is off | `ALTER DATABASE [<db>] SET ALLOW_SNAPSHOT_ISOLATION ON` |
| Session/DDL errors, temp-table failures | Connected via the Neon `-pooler` endpoint | Use the direct (unpooled) endpoint |
| Compute suspended mid-load | Scale-to-zero fired during a quiet gap | Disable scale-to-zero for the load |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MSSQL to PostgreSQL](/guides/mssql/) — generic SQL Server source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MSSQL minimal-safe example](/examples/mssql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [Azure SQL to Supabase](/guides/azure-sql-to-supabase/) · [MSSQL to Neon](/guides/mssql-to-neon/) · [MSSQL to Supabase](/guides/mssql-to-supabase/) · [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/)
</content>
