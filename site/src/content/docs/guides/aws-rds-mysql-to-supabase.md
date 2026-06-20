---
title: AWS RDS MySQL to Supabase
description: AWS RDS MySQL to Supabase migration playbook — pgferry moves RDS or Aurora MySQL to Supabase Postgres via security groups, RDS CA TLS, and the session pooler.
---

This is an operator's playbook for an **AWS RDS MySQL to Supabase** migration. It covers the RDS side — security-group access, the Amazon RDS certificate authority, and reading from a replica — together with the Supabase connection and timeout setup you need to move [Amazon RDS for MySQL](https://aws.amazon.com/rds/mysql/) (or Aurora MySQL) into [Supabase](https://supabase.com/) PostgreSQL.

If you searched for how to **migrate RDS MySQL to Supabase** or move **AWS RDS MySQL to Supabase Postgres**, the short version is: open the RDS security group to your migration host, connect with TLS, optionally read from an RDS read replica, point `pgferry` at Supabase's session pooler, and raise the `postgres` role statement timeout for the load.

## What this guide is for

Use this guide when your source is **Amazon RDS for MySQL** or **Aurora MySQL** and your destination is Supabase Postgres. RDS MySQL is standard MySQL with AWS-managed access and TLS, so the type behavior is identical to self-hosted MySQL — read the generic [MySQL to PostgreSQL guide](/guides/mysql/) for that. This page focuses on the AWS access and TLS specifics. It assumes you already have a Supabase project.

## Why use pgferry instead of generic pgloader advice

Most "rds mysql to postgres" walkthroughs reach for `pgloader`, which stalls or loses fidelity on real schemas:

- `pgloader` loads in long transactions with no resume — a dropped connection over the public internet means starting over. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)` booleans, and zero dates need deliberate decisions; `pgferry` exposes each as an explicit, documented knob.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight surfacing skipped indexes, generated columns, and required extensions first.
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

`resume = true` requires `unlogged_tables = false` (see the [configuration reference](/reference/configuration/#important-incompatibilities)). `source_snapshot_mode = "single_tx"` gives one consistent read view while the RDS source stays live.

## AWS RDS source access, TLS, and replica notes

RDS MySQL is reached on `<id>.<region>.rds.amazonaws.com:3306`. pgferry uses the `go-sql-driver/mysql` driver, so the source DSN is:

```bash
export PGFERRY_SOURCE_DSN='<user>:<password>@tcp(<id>.<region>.rds.amazonaws.com:3306)/<db>?tls=skip-verify'
```

- **Network access.** To reach RDS from an external migration host, the instance must be **publicly accessible** *and* the attached **security group** must allow inbound TCP `3306` from your migration host's IP/CIDR. The cleanest path is to run pgferry on an EC2 instance in the same VPC and allow that instance's security group — no public exposure, lower latency.
- **TLS.** RDS allows both encrypted and unencrypted connections by default. The server certificate is signed by the **Amazon RDS CA** (e.g. `rds-ca-rsa2048-g1`), which is not in the OS trust store. With a plain DSN, `tls=skip-verify` encrypts the connection in transit (full chain verification would require registering the downloaded RDS CA bundle, which a bare DSN can't do). To force encryption server-side, set `require_secure_transport=ON` in the DB parameter group.
- **Read from a replica.** For a large or busy database, create an **RDS read replica** and point the source DSN at the replica's endpoint so the migration's reads don't load the primary. Quiesce writes or accept the replica's snapshot point for consistency.
- **Aurora MySQL.** Use the cluster **reader endpoint** for the same effect.

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

RDS MySQL is standard MySQL — decide these deliberately (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM` and `SET` columns land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` truly means boolean in your data.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — convert `0000-00-00` to `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension (enable it in Supabase **Database → Extensions** if pgferry reports it).

## Step-by-step AWS RDS MySQL to Supabase migration flow

1. Add your migration host to the RDS security group inbound rules and confirm a TLS connection works.
2. (Optional) Create a read replica and point the source DSN at it.
3. Create the Supabase project, copy the session-pooler string, and `alter role postgres set statement_timeout = '0';`.
4. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning (skipped indexes, generated columns, required extensions).
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm Supabase **Database → Extensions** has every extension your schema needs.
- Spot-check enum/set columns and any `tinyint(1)` columns for the mapping you chose.
- Restore the `postgres` role `statement_timeout`.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Connection times out to the RDS endpoint | Security group / public accessibility blocks your IP | Allow inbound `3306` from your host, or run inside the VPC |
| TLS handshake / cert verification error | RDS CA not in the system trust store | Use `tls=skip-verify` (or register the RDS CA bundle) |
| `canceling statement due to statement timeout` | Supabase 2-min role timeout | `alter role postgres set statement_timeout = '0'` |
| `prepared statement ... does not exist` | Connected via transaction pooler (6543) | Use session pooler (5432) or direct |
| Stale rows on the target | Read replica lag during the snapshot | Quiesce writes or migrate from the primary |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic MySQL source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [AWS RDS MySQL to Neon](/guides/aws-rds-mysql-to-neon/) · [MySQL to Supabase](/guides/mysql-to-supabase/) · [Cloud SQL for MySQL to Supabase](/guides/cloud-sql-mysql-to-supabase/) · [PlanetScale to Supabase](/guides/planetscale-to-supabase/)
</content>
