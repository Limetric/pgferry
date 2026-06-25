---
title: PlanetScale to Neon
description: PlanetScale to Neon playbook — pgferry moves a PlanetScale MySQL/Vitess DB to Neon serverless Postgres via tls=true, the direct endpoint, and scale-to-zero.
---

This is an operator's playbook for a **PlanetScale to Neon** migration — moving a **PlanetScale (MySQL/Vitess) database** into [Neon](https://neon.com/)'s serverless PostgreSQL with `pgferry`. PlanetScale's classic product is MySQL on Vitess, so this is a cross-engine MySQL → PostgreSQL migration, with one twist: Vitess imposes connection and transaction constraints that plain self-hosted MySQL does not.

If you searched for how to **migrate PlanetScale to Neon** or move **PlanetScale MySQL to Neon Postgres**, the short version is: connect to PlanetScale over TLS (`tls=true`), be deliberate about how you take a consistent snapshot through Vitess, point `pgferry` at Neon's **unpooled (direct)** endpoint, and disable scale-to-zero for the load.

## What this guide is for

Use this guide when your source is a **PlanetScale MySQL/Vitess** database and your destination is Neon Postgres. This page is **not** about [PlanetScale for Postgres](/guides/mysql-to-planetscale-postgres/) (their newer PostgreSQL product) — here PlanetScale is the *source* and Neon is the *destination*. For source-side type behavior that is not PlanetScale-specific (enums, sets, unsigned integers, zero dates), read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page. It assumes you have a Neon project and branch.

## Why use pgferry instead of generic pgloader advice

Most "planetscale to postgres" advice points at `pgloader`, which tends to struggle on real schemas — and PlanetScale's own exporter targets MySQL/PlanetScale, not PostgreSQL:

- `pgloader` has no resume and loads in long transactions; over a Neon connection that auto-suspends, an interrupted load restarts from zero. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)`, and zero dates are explicit, documented knobs in `pgferry`; `pgloader` guesses and frequently picks `text`.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight that surfaces skipped indexes, generated columns, and required extensions before PostgreSQL is touched.
- `pgferry` creates objects as the connecting role, avoiding the ownership/`SET ROLE` errors `pg_dump`/`pg_restore` hit against Neon's non-superuser role.

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

`resume = true` requires `unlogged_tables = false` (see the [configuration reference](/reference/configuration/#important-incompatibilities)). On a Vitess source, read the snapshot caveat below before relying on `single_tx`.

## PlanetScale source connection, TLS, and Vitess constraints

PlanetScale connection credentials come from the database's **Connect** dialog; passwords are prefixed `pscale_pw_`, and the direct host is typically `aws.connect.psdb.cloud`. pgferry uses the `go-sql-driver/mysql` driver, so the source DSN is:

```bash
export PGFERRY_SOURCE_DSN='<user>:pscale_pw_<...>@tcp(aws.connect.psdb.cloud:3306)/<db>?tls=true'
```

- **TLS is mandatory.** PlanetScale rejects unencrypted connections (`client must use SSL/TLS`). Use `?tls=true` — PlanetScale's certificate chains to a public CA, so the driver verifies against the system trust store with no CA file to download.
- **Vitess is not plain MySQL.** PlanetScale runs MySQL behind Vitess (VTGate). Two consequences for a migration source:
  - Stored procedures are unsupported, and **foreign keys** were unsupported for most of PlanetScale's history (they require opt-in on recent versions). Your schema may have *no* FKs to migrate — that is expected, not a pgferry omission.
  - Connections are pooled and proxied through VTGate, which enforces query and transaction timeouts. A long-running `single_tx` snapshot over a very large keyspace can be cut off mid-read.
- **Snapshot strategy.** For small-to-medium databases, `source_snapshot_mode = "single_tx"` is fine. For large ones, run the migration during a quiet window, or migrate from a **branch** created off production so the read load and any timeout pressure stay off your live keyspace.
- **IP restrictions** are an opt-in PlanetScale feature (default open). If you have enabled them, add your migration host's egress IP.

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

## Source-specific caveats (MySQL family)

PlanetScale is MySQL, so the MySQL decisions apply — decide them deliberately (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM` and `SET` columns land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` truly means boolean in your data.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — convert `0000-00-00` to `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension — Neon supports it via `CREATE EXTENSION` (or let `pgferry` surface it in `plan`).

## Step-by-step PlanetScale to Neon migration flow

1. Copy a PlanetScale connection string (`pscale_pw_` password) and confirm `tls=true` connects.
2. Decide your snapshot strategy — `single_tx` for small/medium data, or migrate from a branch for large keyspaces.
3. Create the Neon project/branch, copy the **direct** connection string, and disable scale-to-zero (raise the compute size for large data).
4. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning (skipped indexes, generated columns, required extensions).
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views and triggers via [hooks](/reference/hooks/) (PlanetScale has no stored procedures to port).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions exist (`CREATE EXTENSION` for anything `plan` flagged).
- Spot-check enum/set columns and any `tinyint(1)` columns for the mapping you chose.
- Re-enable scale-to-zero and restore the compute size if you changed it.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| `client must use SSL/TLS` on the source | Missing TLS on the PlanetScale DSN | Append `?tls=true` |
| Source read cut off on a huge keyspace | Vitess query/transaction timeout during `single_tx` | Migrate from a branch or during a quiet window |
| No foreign keys appear on the target | PlanetScale schema had none (Vitess default) | Expected; add FKs on PostgreSQL via hooks if desired |
| Session/DDL errors, temp-table failures | Connected via the Neon `-pooler` endpoint | Use the direct (unpooled) endpoint |
| Compute suspended mid-load | Scale-to-zero fired during a quiet gap | Disable scale-to-zero for the load |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic MySQL source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [PlanetScale to Supabase](/guides/planetscale-to-supabase/) · [MySQL to Neon](/guides/mysql-to-neon/) · [AWS RDS MySQL to Neon](/guides/aws-rds-mysql-to-neon/) · [Cloud SQL for MySQL to Neon](/guides/cloud-sql-mysql-to-neon/)
