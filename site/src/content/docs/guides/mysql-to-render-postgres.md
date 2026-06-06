---
title: MySQL to Render Postgres
description: A MySQL to Render Postgres migration playbook — move MySQL to a Render PostgreSQL instance with pgferry, covering internal vs external URLs, required TLS, and inbound IP allowlists.
---

This is an operator's playbook for a **MySQL to Render Postgres** migration. It covers the Render-specific connection details — external vs internal URLs, required TLS, and the inbound IP allowlist — that you need to move MySQL into a [Render](https://render.com/) PostgreSQL instance.

If you searched for how to **migrate MySQL to Render Postgres** or **move a MySQL database to Render**, the short version is: connect from an external host with Render's **External Database URL** and `sslmode=require`, allow your migration host in the inbound IP rules, never load production data into a Free instance, and let pgferry's [MySQL type mapping](/reference/type-mapping/) handle enums, sets, and unsigned integers.

## What this guide is for

Use this guide when you have a live MySQL database and want it on a Render-hosted PostgreSQL instance. It assumes you have created a Render Postgres instance. For source-side behavior that is not Render-specific, read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

`pgloader` is the common "mysql to render postgres" recommendation and it struggles on real workloads:

- No resume — a drop mid-load means starting over. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)`, and zero dates are explicit knobs in `pgferry`.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight surfacing skipped indexes, generated columns, and required extensions first.
- `pgferry` creates objects as the connecting role — no ownership/`SET ROLE` errors against Render's non-superuser role.

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

## Render DSN, TLS, and firewall notes

Render gives every instance two URLs:

| URL | Host shape | Use from |
| --- | --- | --- |
| External Database URL | `dpg-<id>-a.<region>-postgres.render.com:5432` | An **external** migration host |
| Internal Database URL | `dpg-<id>-a` (private) | A Render service in the **same region** |

- **From your laptop or any external host, use the External Database URL.** Only a service running inside Render (same region) can use the internal host.
- **TLS is required for external connections.** Append `?sslmode=require`. Render manages the certificate; `sslmode=require` encrypts in transit (it does not validate the chain, which is the documented/safe setting).
- **Inbound IP allowlist:** by default a Render Postgres instance is reachable from any IP (`0.0.0.0/0`), gated only by credentials + SSL. If you have tightened the inbound rules, **add your migration host's IP** (e.g. `203.0.113.45/32`) on the instance's **Networking** section before starting. If a dynamic IP makes that impractical, temporarily widen the rule for the load and tighten it afterward. The allowlist applies only to the external URL — internal connections are always allowed.
- **Connection limits** scale with instance RAM (100 connections below 8 GB, 200 at 8–16 GB, and up). Keep pgferry's `workers`/`index_workers` within the instance's ceiling.

Example external target DSN:

```bash
export PGFERRY_TARGET_DSN='postgres://<user>:<password>@dpg-<id>-a.<region>-postgres.render.com:5432/<dbname>?sslmode=require'
export PGFERRY_SOURCE_DSN='user:pass@tcp(mysql-host:3306)/source_db'
```

## Source-specific caveats (MySQL)

Decide these on the MySQL side (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM`/`SET` land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` means boolean.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — `0000-00-00` → `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension (`CREATE EXTENSION` works for Render's supported set).

## Step-by-step MySQL to Render Postgres migration flow

1. Create a **paid** Render Postgres instance sized for the dataset; copy the External Database URL.
2. Add your migration host's IP to the inbound rules if you have restricted them; enable storage autoscaling or pre-provision storage.
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning.
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions are enabled (`CREATE EXTENSION` for anything `plan` flagged).
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
| `too many connections` | `workers` above the instance limit | Lower `workers`/`index_workers` or size up |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MySQL to Railway Postgres](/guides/mysql-to-railway-postgres/) · [MySQL to Supabase](/guides/mysql-to-supabase/)
