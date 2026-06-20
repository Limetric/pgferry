---
title: Cloud SQL for MySQL to Supabase
description: Cloud SQL for MySQL to Supabase playbook — pgferry moves Cloud SQL MySQL into Supabase Postgres via the Auth Proxy, session pooler, and statement-timeout setup.
---

This is an operator's playbook for a **Cloud SQL for MySQL to Supabase** migration. It covers the Google Cloud SQL side — the Cloud SQL Auth Proxy versus public-IP authorized networks, and SSL/TLS — together with the Supabase connection and timeout setup you need to move [Cloud SQL for MySQL](https://cloud.google.com/sql/docs/mysql) into [Supabase](https://supabase.com/) PostgreSQL.

If you searched for how to **migrate Cloud SQL MySQL to Supabase** or move **Google Cloud SQL for MySQL to Supabase Postgres**, the short version is: connect to Cloud SQL through the **Auth Proxy** (or add your IP to authorized networks and enforce SSL), point `pgferry` at Supabase's session pooler, and raise the `postgres` role statement timeout for the load.

## What this guide is for

Use this guide when your source is **Google Cloud SQL for MySQL** and your destination is Supabase Postgres. Cloud SQL MySQL is standard MySQL with Google-managed access and TLS, so the type behavior is identical to self-hosted MySQL — read the generic [MySQL to PostgreSQL guide](/guides/mysql/) for that. This page focuses on the GCP access and TLS specifics. It assumes you already have a Supabase project.

## Why use pgferry instead of generic pgloader advice

Most "cloud sql mysql to postgres" walkthroughs reach for `pgloader`, which stalls or loses fidelity on real schemas:

- `pgloader` loads in long transactions with no resume — a dropped connection means starting over. `pgferry` checkpoints and resumes.
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

`resume = true` requires `unlogged_tables = false` (see the [configuration reference](/reference/configuration/#important-incompatibilities)). `source_snapshot_mode = "single_tx"` gives one consistent read view while the Cloud SQL source stays live.

## Cloud SQL source access, TLS, and replica notes

pgferry uses the `go-sql-driver/mysql` driver. There are two practical ways to reach Cloud SQL — the Auth Proxy is the cleaner default:

**1. Cloud SQL Auth Proxy (recommended).** Run the proxy on your migration host; it listens locally, authenticates with IAM, and encrypts automatically — no authorized networks, no certificate download. Point pgferry at the proxy's local TCP port (`127.0.0.1:3306`) with TLS off — the proxy already encrypts:

```bash
# cloud-sql-proxy <project>:<region>:<instance> --port 3306 &
export PGFERRY_SOURCE_DSN='<user>:<password>@tcp(127.0.0.1:3306)/<db>?tls=false'
```

**2. Direct public IP.** Add your migration host's IP to the instance's **Authorized networks**, connect to the instance's **public IP**, and enforce SSL. Cloud SQL's server certificate is a Google-managed self-signed CA not in the OS trust store, so use `tls=skip-verify` in the DSN (full verification would require registering the downloaded `server-ca.pem`, which a bare DSN can't do):

```bash
export PGFERRY_SOURCE_DSN='<user>:<password>@tcp(<public-ip>:3306)/<db>?tls=skip-verify'
```

- **Enforce SSL** on the instance (**Connections → Security**) so unencrypted connections are rejected — then keep `tls=skip-verify` (or use the Auth Proxy, which is encrypted regardless).
- **Read from a replica.** For a large or busy database, create a Cloud SQL **read replica** and point the source at it so reads don't load the primary. Quiesce writes or accept the replica's snapshot point for consistency.

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

Cloud SQL MySQL is standard MySQL — decide these deliberately (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM` and `SET` columns land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` truly means boolean in your data.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — convert `0000-00-00` to `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension (enable it in Supabase **Database → Extensions** if pgferry reports it).

## Step-by-step Cloud SQL for MySQL to Supabase migration flow

1. Start the Cloud SQL Auth Proxy (or add your IP to authorized networks and enforce SSL); confirm the source connects.
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
| Connection times out to the public IP | Migration host IP not in authorized networks | Add your IP, or use the Cloud SQL Auth Proxy |
| `connections using insecure transport are prohibited` | Instance enforces SSL, DSN has no TLS | Use `tls=skip-verify` (or the Auth Proxy) |
| TLS handshake / cert verification error | Google CA not in the system trust store | Use `tls=skip-verify`, or the Auth Proxy (`tls=false`) |
| `canceling statement due to statement timeout` | Supabase 2-min role timeout | `alter role postgres set statement_timeout = '0'` |
| `prepared statement ... does not exist` | Connected via transaction pooler (6543) | Use session pooler (5432) or direct |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic MySQL source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [Cloud SQL for MySQL to Neon](/guides/cloud-sql-mysql-to-neon/) · [MySQL to Supabase](/guides/mysql-to-supabase/) · [AWS RDS MySQL to Supabase](/guides/aws-rds-mysql-to-supabase/) · [PlanetScale to Supabase](/guides/planetscale-to-supabase/)
</content>
