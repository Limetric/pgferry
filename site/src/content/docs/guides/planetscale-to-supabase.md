---
title: PlanetScale to Supabase
description: A PlanetScale to Supabase migration playbook — move a PlanetScale MySQL database into Supabase Postgres with pgferry, covering Vitess connection constraints, required TLS, snapshot caveats, and MySQL type handling.
---

This is an operator's playbook for a **PlanetScale to Supabase** migration — moving a **PlanetScale (MySQL/Vitess) database** into [Supabase](https://supabase.com/) PostgreSQL with `pgferry`. PlanetScale's classic product is MySQL on Vitess, so this is a cross-engine MySQL → PostgreSQL migration, with one twist: Vitess imposes connection and transaction constraints that plain self-hosted MySQL does not.

If you searched for how to **migrate PlanetScale to Supabase** or move **PlanetScale MySQL to Supabase Postgres**, the short version is: connect to PlanetScale over TLS (`tls=true`), be deliberate about how you take a consistent snapshot through Vitess, point `pgferry` at Supabase's session pooler, and raise the `postgres` role statement timeout for the load.

## What this guide is for

Use this guide when your source is a **PlanetScale MySQL/Vitess** database and your destination is Supabase Postgres. This page is **not** about [PlanetScale for Postgres](/guides/mysql-to-planetscale-postgres/) (their newer PostgreSQL product) — here PlanetScale is the *source* and Supabase is the *destination*. For source-side type behavior that is not PlanetScale-specific (enums, sets, unsigned integers, zero dates), read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

Most "planetscale to postgres" advice points at `pgloader`, which struggles on real schemas — and PlanetScale's own exporter targets MySQL/PlanetScale, not PostgreSQL:

- `pgloader` has no resume and loads in long transactions; a drop over PlanetScale's proxied connection means starting over. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)`, and zero dates are explicit, documented knobs in `pgferry`; `pgloader` guesses and frequently picks `text`.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight that surfaces skipped indexes, generated columns, and required extensions before PostgreSQL is touched.
- `pgferry` creates objects as the connecting role, avoiding the ownership/`SET ROLE` errors `pg_dump`/`pg_restore` hit against Supabase's non-superuser role.

## Destination prerequisites

- A Supabase project (note its **project ref**) and the database password from **Project Settings → Database**.
- Decide the target schema. Supabase uses `public` by default; pgferry can create and own a dedicated schema instead.
- The Supabase `postgres` role is **not a superuser** but can create schemas, tables, indexes, FKs, sequences, and allow-listed extensions — everything pgferry needs.

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
```

### Statement timeout — the most common Supabase migration failure

Supabase caps the `postgres` role at a **2-minute statement timeout** by default. A large `COPY` chunk or index build dies with `canceling statement due to statement timeout`. Disable it for the load, then restore:

```sql
alter role postgres set statement_timeout = '0';   -- before
alter role postgres reset statement_timeout;        -- after cutover
```

Reconnect for it to take effect.

## Source-specific caveats (MySQL family)

PlanetScale is MySQL, so the MySQL decisions apply — decide them deliberately (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM` and `SET` columns land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` truly means boolean in your data.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — convert `0000-00-00` to `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension (enable it in Supabase **Database → Extensions** if pgferry reports it).

## Step-by-step PlanetScale to Supabase migration flow

1. Copy a PlanetScale connection string (`pscale_pw_` password) and confirm `tls=true` connects.
2. Decide your snapshot strategy — `single_tx` for small/medium data, or migrate from a branch for large keyspaces.
3. Create the Supabase project, copy the session-pooler string, and `alter role postgres set statement_timeout = '0';`.
4. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning (skipped indexes, generated columns, required extensions).
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views and triggers via [hooks](/reference/hooks/) (PlanetScale has no stored procedures to port).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm Supabase **Database → Extensions** has every extension your schema needs.
- Spot-check enum/set columns and any `tinyint(1)` columns for the mapping you chose.
- Restore the `postgres` role `statement_timeout`.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| `client must use SSL/TLS` on the source | Missing TLS on the PlanetScale DSN | Append `?tls=true` |
| Source read cut off on a huge keyspace | Vitess query/transaction timeout during `single_tx` | Migrate from a branch or during a quiet window |
| No foreign keys appear on the target | PlanetScale schema had none (Vitess default) | Expected; add FKs on PostgreSQL via hooks if desired |
| `canceling statement due to statement timeout` | Supabase 2-min role timeout | `alter role postgres set statement_timeout = '0'` |
| `prepared statement ... does not exist` | Connected via transaction pooler (6543) | Use session pooler (5432) or direct |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic MySQL source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [PlanetScale to Neon](/guides/planetscale-to-neon/) · [MySQL to Supabase](/guides/mysql-to-supabase/) · [AWS RDS MySQL to Supabase](/guides/aws-rds-mysql-to-supabase/) · [Cloud SQL for MySQL to Supabase](/guides/cloud-sql-mysql-to-supabase/)
</content>
