---
title: AWS RDS MySQL to Neon
description: An AWS RDS MySQL to Neon migration playbook — move RDS (or Aurora) MySQL into Neon serverless Postgres with pgferry, covering security groups, RDS CA TLS, read replicas, the direct endpoint, and scale-to-zero.
---

This is an operator's playbook for an **AWS RDS MySQL to Neon** migration. It covers the RDS side — security-group access, the Amazon RDS certificate authority, and reading from a replica — together with the Neon endpoint and scale-to-zero details you need to move [Amazon RDS for MySQL](https://aws.amazon.com/rds/mysql/) (or Aurora MySQL) into [Neon](https://neon.com/)'s serverless PostgreSQL.

If you searched for how to **migrate RDS MySQL to Neon** or move **AWS RDS MySQL to Neon Postgres**, the short version is: open the RDS security group to your migration host, connect with TLS, optionally read from an RDS read replica, point `pgferry` at Neon's **unpooled (direct)** endpoint, and disable scale-to-zero for the load.

## What this guide is for

Use this guide when your source is **Amazon RDS for MySQL** or **Aurora MySQL** and your destination is Neon Postgres. RDS MySQL is standard MySQL with AWS-managed access and TLS, so the type behavior is identical to self-hosted MySQL — read the generic [MySQL to PostgreSQL guide](/guides/mysql/) for that. This page focuses on the AWS access and TLS specifics. It assumes you have a Neon project and branch.

## Why use pgferry instead of generic pgloader advice

Most "rds mysql to postgres" walkthroughs reach for `pgloader`, which stalls or loses fidelity on real schemas:

- `pgloader` loads in long transactions with no resume — over a Neon connection that auto-suspends, an interrupted load restarts from zero. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)` booleans, and zero dates need deliberate decisions; `pgferry` exposes each as an explicit, documented knob.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight surfacing skipped indexes, generated columns, and required extensions first.
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

## Neon DSN, TLS, pooling, and firewall notes

Neon endpoints differ only by a `-pooler` suffix:

| Endpoint | Host shape | Use for |
| --- | --- | --- |
| Direct (unpooled) | `ep-<id>.<region>.aws.neon.tech` | **Migrations, DDL, bulk load** |
| Pooled | `ep-<id>-pooler.<region>.aws.neon.tech` | App runtime |

- **Use the direct (unpooled) endpoint.** The pooled endpoint is PgBouncer in transaction mode and breaks session-scoped DDL and the session features pgferry relies on.
- **TLS is mandatory.** Neon rejects non-TLS connections. Use `?sslmode=require` at minimum; `verify-full` works against the system trust store. Neon's console strings also include `channel_binding=require`, supported by the `pgx` driver pgferry uses.
- **IP Allow** is a paid-plan feature, default open. If enabled, add your migration host's egress IP/CIDR first. (If you run pgferry on EC2, that is the egress IP to allow.)

Example direct-endpoint target DSN:

```bash
export PGFERRY_TARGET_DSN='postgresql://neondb_owner:<password>@ep-<id>.<region>.aws.neon.tech/neondb?sslmode=require'
```

### Scale-to-zero — the Neon-specific gotcha

Neon computes auto-suspend after inactivity (5 minutes by default; fixed on Free). **Disable scale-to-zero (or raise the timeout) for the migration window** in **Branches → compute → Edit**, then re-enable it after. For large datasets, raise the compute size for more `max_connections` and index-build headroom. Keep transactions moving so the 5-minute `idle_in_transaction_session_timeout` does not terminate one mid-load. (Co-locating the Neon project in the same AWS region as the RDS source keeps transfer fast and cheap.)

## Source-specific caveats (MySQL family)

RDS MySQL is standard MySQL — decide these deliberately (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM` and `SET` columns land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` truly means boolean in your data.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — convert `0000-00-00` to `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension — Neon supports it via `CREATE EXTENSION` (or let `pgferry` surface it in `plan`).

## Step-by-step AWS RDS MySQL to Neon migration flow

1. Add your migration host to the RDS security group inbound rules and confirm a TLS connection works.
2. (Optional) Create a read replica and point the source DSN at it.
3. Create the Neon project/branch, copy the **direct** connection string, and disable scale-to-zero (raise the compute size for large data).
4. Generate a config with `pgferry wizard` or start from the snippet above; export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning (skipped indexes, generated columns, required extensions).
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions exist (`CREATE EXTENSION` for anything `plan` flagged).
- Spot-check enum/set columns and any `tinyint(1)` columns for the mapping you chose.
- Re-enable scale-to-zero and restore the compute size if you changed it.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Connection times out to the RDS endpoint | Security group / public accessibility blocks your IP | Allow inbound `3306` from your host, or run inside the VPC |
| TLS handshake / cert verification error | RDS CA not in the system trust store | Use `tls=skip-verify` (or register the RDS CA bundle) |
| Session/DDL errors, temp-table failures | Connected via the Neon `-pooler` endpoint | Use the direct (unpooled) endpoint |
| Compute suspended mid-load | Scale-to-zero fired during a quiet gap | Disable scale-to-zero for the load |
| Stale rows on the target | Read replica lag during the snapshot | Quiesce writes or migrate from the primary |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic MySQL source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [AWS RDS MySQL to Supabase](/guides/aws-rds-mysql-to-supabase/) · [MySQL to Neon](/guides/mysql-to-neon/) · [Cloud SQL for MySQL to Neon](/guides/cloud-sql-mysql-to-neon/) · [PlanetScale to Neon](/guides/planetscale-to-neon/)
</content>
