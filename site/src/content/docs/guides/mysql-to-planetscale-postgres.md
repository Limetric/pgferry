---
title: MySQL to PlanetScale Postgres
description: A MySQL to PlanetScale Postgres migration playbook — move MySQL into PlanetScale for Postgres with pgferry, covering the direct port, verify-full TLS, PSBouncer, and the postgres-vs-Vitess distinction.
---

This is an operator's playbook for a **MySQL to PlanetScale Postgres** migration — moving a MySQL database into **PlanetScale for Postgres**, PlanetScale's PostgreSQL product (generally available since September 2025), **not** PlanetScale's legacy MySQL/Vitess platform.

If you searched for how to **migrate MySQL to PlanetScale Postgres** or **move MySQL to PlanetScale for Postgres**, the short version is: point `pgferry` at PlanetScale's **direct port (5432)** with `sslmode=verify-full sslrootcert=system`, not the PSBouncer pooler, and let pgferry's [MySQL type mapping](/reference/type-mapping/) handle enums, sets, and unsigned integers.

## What this guide is for

Use this guide when your source is **MySQL** and your destination is **PlanetScale for Postgres**. Two common points of confusion:

- This is **not** about PlanetScale's MySQL/Vitess product. If you are staying on MySQL, no cross-engine migration is needed. This guide is for teams deliberately moving onto PlanetScale's PostgreSQL offering.
- PlanetScale for Postgres is real PostgreSQL running on PlanetScale Metal — so a MySQL → PlanetScale Postgres move is a genuine MySQL-to-PostgreSQL migration, with all the type and DDL translation that implies.

For source-side behavior that is not PlanetScale-specific, read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

PlanetScale recommends `pg_dump`/`pg_restore` for Postgres-to-Postgres imports, but those are useless for a MySQL **source**. The generic cross-engine tool is `pgloader`, which struggles on real schemas:

- No resume, single long transactions, and weak type fidelity for MySQL enums/sets/unsigned integers.
- `pgferry` streams with chunked, parallel `COPY`, checkpoints for `resume`, and runs a [`plan`](/operations/how-to-read-plan-output/) preflight.
- `pgferry` creates objects as the connecting role, so you avoid the ownership/`SET ROLE` errors PlanetScale's docs warn about for non-superuser restores (their guidance to use `--no-owner --no-privileges` is a `pg_dump` concern that simply does not arise with pgferry).

## Destination prerequisites

- A PlanetScale for Postgres database and branch.
- A role's connection string from **Settings → Roles → View connection strings**. PlanetScale passwords are prefixed `pscale_pw_`. PlanetScale recommends a dedicated role rather than the default `postgres` role for ongoing app traffic.
- The default `postgres` role is `NOSUPERUSER` but has `CREATEDB`/`CREATEROLE` and can create schemas, tables, indexes, FKs, sequences, and allow-listed extensions — everything pgferry needs.

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
type = "mysql"
# dsn supplied via PGFERRY_SOURCE_DSN

[target]
# dsn supplied via PGFERRY_TARGET_DSN

[type_mapping]
tinyint1_as_boolean = false
json_as_jsonb = true
enum_mode = "check"
set_mode = "text"
sanitize_json_null_bytes = true
```

`resume = true` requires `unlogged_tables = false` (see the [configuration reference](/reference/configuration/#important-incompatibilities)).

## PlanetScale Postgres DSN, TLS, pooling, and firewall notes

| Endpoint | Port | Use for |
| --- | --- | --- |
| Direct Postgres | `5432` | **Migrations, DDL, bulk load** |
| PSBouncer (pooler) | `6432` | App runtime |

- **Use the direct port (5432) for the migration.** PSBouncer runs in transaction mode and does not keep persistent prepared statements across transactions; PlanetScale's docs require the direct connection for schema changes, ETL/data streaming, and long or multi-statement transactions — exactly what pgferry does.
- **TLS is required.** PlanetScale documents `sslmode=verify-full` with `sslrootcert=system` (their certs chain to a public CA, so the OS trust store works — no CA file download). They also document `sslnegotiation=direct`.
- **Host shape:** `{identifier}-{region}-{instance}.horizon.psdb.cloud`. Read the exact host, port, user, and dbname from the generated connection string.
- **IP restrictions** are an opt-in, per-branch allowlist (default open). If you enable them, add your migration host's egress IP. Private connectivity is available via AWS PrivateLink / GCP Private Service Connect.
- **Capacity:** PlanetScale advises the target have disk capacity around **150% of the source size**.

Example direct target DSN:

```bash
export PGFERRY_TARGET_DSN='postgres://postgres.<branch_id>:pscale_pw_<...>@<identifier>-<region>-1.horizon.psdb.cloud:5432/<dbname>?sslmode=verify-full&sslrootcert=system&sslnegotiation=direct'
export PGFERRY_SOURCE_DSN='user:pass@tcp(mysql-host:3306)/source_db'
```

## Source-specific caveats (MySQL)

Decide these on the MySQL side (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM`/`SET` land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` means boolean.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — `0000-00-00` → `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs `citext` — PlanetScale supports it in its extension set; pre-create extensions your schema requires.

## Step-by-step MySQL to PlanetScale Postgres migration flow

1. Create the PlanetScale Postgres database/branch and copy a role's **direct** (5432) connection string.
2. Pre-create any required extensions on the target (within PlanetScale's supported set).
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning.
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm every required extension installed (some PlanetScale extensions need elevated handling).
- Verify you connected on 5432, not 6432, throughout.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Prepared-statement / session errors | Connected via PSBouncer (6432) | Use the direct port (5432) |
| TLS handshake / cert error | Missing verify-full params | Use `sslmode=verify-full&sslrootcert=system` |
| Connection refused after enabling IP restrictions | Migration host not allow-listed | Add your egress IP to the branch allowlist |
| `extension ... must be installed` | Extension not pre-created/unsupported | Create supported extensions first; map type differently otherwise |
| Out of disk near the end | Target undersized | Provision ~150% of source size |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MySQL to Supabase](/guides/mysql-to-supabase/) · [MySQL to Neon](/guides/mysql-to-neon/) · [MySQL to Railway Postgres](/guides/mysql-to-railway-postgres/) · [MySQL to Render Postgres](/guides/mysql-to-render-postgres/) · [MSSQL to PlanetScale Postgres](/guides/mssql-to-planetscale-postgres/)
