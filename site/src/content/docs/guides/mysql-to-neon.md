---
title: MySQL to Neon
description: A MySQL to Neon migration playbook — move MySQL to Neon serverless Postgres with pgferry, including direct vs pooled endpoints, scale-to-zero, and TLS details generic pgloader guides skip.
---

This is an operator's playbook for a **MySQL to Neon** migration. It covers the Neon-specific endpoint, scale-to-zero, and TLS details you need to move MySQL into [Neon](https://neon.com/)'s serverless PostgreSQL — the parts generic import tutorials leave out.

If you searched for how to **migrate MySQL to Neon** or **move a MySQL database to Neon Postgres**, the short version is: point `pgferry` at the **unpooled (direct)** Neon endpoint, disable scale-to-zero for the load, and let the [MySQL type mapping](/reference/type-mapping/) handle enums, sets, and unsigned integers that `pgloader` mishandles.

## What this guide is for

Use this guide when you have a live MySQL database (self-hosted, RDS, Aurora MySQL, Cloud SQL, PlanetScale MySQL, etc.) and want it on Neon Postgres. It assumes you have a Neon project and branch. For source-side behavior that is not Neon-specific, read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

The usual "mysql to neon" advice is `pgloader`, which struggles on real migrations:

- `pgloader` has no resume. Over a Neon connection that auto-suspends or hiccups, an interrupted load restarts from zero. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)`, and zero dates are explicit, documented knobs in `pgferry`; `pgloader` guesses and frequently picks `text`.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight that surfaces skipped indexes, generated columns, and required extensions before PostgreSQL is touched.
- `pgferry` creates objects as the connecting role, avoiding the ownership/`SET ROLE` errors that `pg_dump`/`pg_restore` hit against Neon's non-superuser role.

## Destination prerequisites

- A Neon project and branch. Note the default database (`neondb`) and the owner role (`neondb_owner` by default), or create your own.
- The connection string from the Neon console (**Connect** button), which gives you both pooled and direct host forms.
- Neon's owner role is a member of `neon_superuser` — **not a true superuser**, but it can create databases, schemas, tables, indexes, FKs, sequences, and allow-listed extensions. That is everything pgferry needs.

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

`resume = true` requires `unlogged_tables = false` (see the [configuration reference](/reference/configuration/#important-incompatibilities)). Use `source_snapshot_mode = "single_tx"` for one consistent read view on a live MySQL source.

## Neon DSN, TLS, pooling, and firewall notes

Neon endpoints differ only by a `-pooler` suffix on the endpoint ID:

| Endpoint | Host shape | Use for |
| --- | --- | --- |
| Direct (unpooled) | `ep-<id>.<region>.aws.neon.tech` | **Migrations, DDL, bulk load** |
| Pooled | `ep-<id>-pooler.<region>.aws.neon.tech` | App runtime / many short connections |

- **Use the direct (unpooled) endpoint for the migration.** The pooled endpoint is PgBouncer in transaction mode and breaks session-scoped DDL, temp tables, and the session features pgferry relies on. Neon's own import guidance says to avoid the pooled string for bulk load. The direct endpoint's connection count scales with compute size, which is far more than pgferry needs.
- **TLS is mandatory.** Neon rejects non-TLS connections. Use `?sslmode=require` at minimum; `sslmode=verify-full` is better and works against the system trust store (Neon uses a public CA). Neon's console strings also add `channel_binding=require`, which the Go/`pgx` driver pgferry uses supports.
- **IP Allow** (allowlisting) is a paid-plan feature and defaults to open. If you have it enabled, add your migration host's egress IP/CIDR before starting.

Example direct-endpoint target DSN:

```bash
export PGFERRY_TARGET_DSN='postgresql://neondb_owner:<password>@ep-<id>.<region>.aws.neon.tech/neondb?sslmode=require'
export PGFERRY_SOURCE_DSN='user:pass@tcp(mysql-host:3306)/source_db'
```

### Scale-to-zero — the Neon-specific gotcha

Neon computes auto-suspend after a period of inactivity (5 minutes by default; fixed on the Free plan). An active query keeps the compute busy, but to remove any risk of a suspend during quiet gaps in a long load, **disable scale-to-zero (or raise the timeout) for the migration window**, then re-enable it. Set this per branch in **Branches → compute → Edit**. For large datasets, also bump the compute size — more RAM means more `max_connections` and more local disk for index builds.

Neon's `idle_in_transaction_session_timeout` defaults to 5 minutes; keep transactions moving so an idle gap does not terminate one mid-migration.

## Source-specific caveats (MySQL)

Decide these on the MySQL side; they are not Neon-specific:

- `enum_mode` / `set_mode` — how `ENUM` and `SET` land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` truly means boolean.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — `0000-00-00` → `NULL` or error.
- Generated columns copy as values, not expressions.
- `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- For `_ci` collations, `ci_as_citext = true` needs the `citext` extension — Neon supports it via `CREATE EXTENSION` (or let `pgferry` surface it in `plan`).

See the [MySQL guide](/guides/mysql/) and [type mapping](/reference/type-mapping/) for the complete picture.

## Step-by-step MySQL to Neon migration flow

1. Create the Neon project/branch and copy the **direct** connection string (no `-pooler`).
2. Disable scale-to-zero for the target branch and, for large data, raise the compute size.
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning.
6. Run `pgferry migrate migration.toml`. Rerun on interruption — `resume = true` continues from the checkpoint.
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions exist (`CREATE EXTENSION` for anything `plan` flagged).
- Re-enable scale-to-zero and restore the compute size if you changed it.
- Spot-check enum/set and `tinyint(1)` columns.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| Session/DDL errors, temp-table failures | Connected via the `-pooler` endpoint | Use the direct (unpooled) endpoint |
| Connection refused / SSL required | Missing TLS | Append `?sslmode=require` |
| Compute suspended mid-load | Scale-to-zero fired during a quiet gap | Disable scale-to-zero for the migration window |
| `extension "..." is not available` | Extension not in Neon's supported set | Map the type differently or recreate manually |
| Slow index builds / OOM | Compute too small | Raise the compute size for the load |

See [common failures and recovery](/operations/common-failures-and-recovery/) for the general catalog.

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MySQL to Supabase](/guides/mysql-to-supabase/) · [MSSQL to Neon](/guides/mssql-to-neon/)
