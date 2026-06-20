---
title: MySQL to Supabase
description: MySQL to Supabase migration playbook — pgferry moves MySQL to Supabase Postgres via the session pooler, mandatory TLS, and statement-timeout setup.
---

This is an operator's playbook for a **MySQL to Supabase** migration. It covers the Supabase-specific connection, pooler, and timeout details you need to move MySQL into [Supabase](https://supabase.com/)'s hosted PostgreSQL without the dead ends that come from generic import advice.

If you searched for how to **migrate MySQL to Supabase** or **move MySQL to Supabase Postgres**, the short version is: point `pgferry` at a session-mode (not transaction-mode) Supabase connection, raise the `postgres` role statement timeout for the load, and let the [MySQL type mapping](/reference/type-mapping/) handle enums, sets, and unsigned integers that `pgloader` mangles.

## What this guide is for

Use this guide when you have a live MySQL database (self-hosted, RDS, Aurora MySQL, Cloud SQL, PlanetScale MySQL, etc.) and you want it running on Supabase Postgres. It assumes you already have a Supabase project. For source-side behavior that is not Supabase-specific, read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

Most "mysql to supabase" walkthroughs reach for `pgloader`. On real schemas that path routinely stalls or loses fidelity:

- `pgloader` loads each table in long-running transactions and has no resume — a dropped connection (common over a hosted pooler) means starting over.
- MySQL enums, sets, unsigned integers, `tinyint(1)` booleans, and zero dates need deliberate decisions. `pgferry` exposes each as an explicit, documented knob; `pgloader` guesses.
- `pgferry` streams data with chunked, parallel `COPY`, checkpoints progress for `resume`, and runs a [`plan`](/operations/how-to-read-plan-output/) preflight so you see skipped indexes, generated columns, and required extensions *before* PostgreSQL is touched.
- `pgferry` creates every object as the connecting role, so you never hit the ownership/`SET ROLE` errors that `pg_dump`/`pg_restore` throw against a non-superuser managed role.

## Destination prerequisites

- A Supabase project (note its **project ref**, the `xxxx` in `db.xxxx.supabase.co`).
- Your database password from **Project Settings → Database** (reset it there if you never saved it).
- Decide the target schema. Supabase uses the `public` schema by default; pgferry can create and own a dedicated schema instead.
- The Supabase `postgres` role is **not a superuser** but is the most privileged role on the instance. It can create schemas, tables, indexes, foreign keys, sequences, and allow-listed extensions — everything pgferry needs.

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

`resume = true` requires `unlogged_tables = false` (see the [configuration reference](/reference/configuration/#important-incompatibilities)). `source_snapshot_mode = "single_tx"` gives one consistent read view while the MySQL source stays live — see [how to choose snapshot mode](/operations/how-to-choose-snapshot-mode/).

## Supabase DSN, TLS, pooling, and firewall notes

Supabase exposes three connection types. Database name is always `postgres`.

| Type | Host | Port | Username |
| --- | --- | --- | --- |
| Direct | `db.<ref>.supabase.co` | `5432` | `postgres` |
| Session pooler (Supavisor) | `aws-0-<region>.pooler.supabase.com` | `5432` | `postgres.<ref>` |
| Transaction pooler (Supavisor) | `aws-0-<region>.pooler.supabase.com` | `6543` | `postgres.<ref>` |

- **Use the session pooler (port 5432) or the direct connection for migrations.** Both preserve session state and prepared statements, which pgferry's DDL and `COPY` pipeline depend on.
- **Do not use the transaction pooler (port 6543).** Transaction mode multiplexes one backend per transaction, disables prepared statements, and drops session-scoped settings — it will break a migration.
- **IPv4-only migration host?** The direct connection (`db.<ref>.supabase.co`) is IPv6-only unless you enable the paid IPv4 add-on. The **session pooler is IPv4-native with no add-on**, so it is the safest default. Copy the exact host string from the dashboard's **Connect** dialog rather than hand-building it.
- **TLS** is expected — use at least `?sslmode=require`. For full verification, download the project CA cert from **Project Settings → Database → SSL Configuration** and use `sslmode=verify-full&sslrootcert=/path/to/ca.crt`.

Example session-pooler target DSN:

```bash
export PGFERRY_TARGET_DSN='postgresql://postgres.<ref>:<password>@aws-0-<region>.pooler.supabase.com:5432/postgres?sslmode=require'
export PGFERRY_SOURCE_DSN='user:pass@tcp(mysql-host:3306)/source_db'
```

### Statement timeout — the most common Supabase migration failure

Supabase caps the `postgres` role at a **default 2-minute statement timeout**. A large `COPY` chunk or index build will be killed mid-load with `canceling statement due to statement timeout`. Disable it for the migration window, then revert:

```sql
-- before the migration
alter role postgres set statement_timeout = '0';
-- after cutover, restore a sane default
alter role postgres reset statement_timeout;
```

Reconnect for the change to take effect — `ALTER ROLE ... SET` applies to new sessions only.

## Source-specific caveats (MySQL)

These come from the MySQL side and are not Supabase-specific — decide them deliberately:

- `enum_mode` / `set_mode` — how MySQL `ENUM` and `SET` columns land in PostgreSQL.
- `tinyint1_as_boolean` — only enable if `tinyint(1)` truly means boolean in your data.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — convert `0000-00-00` to `NULL` or error.
- Generated columns are copied as values, not recreated as expressions.
- `FULLTEXT`, prefix, and expression indexes are reported and skipped, not guessed.
- For `_ci` collations, consider `ci_as_citext = true` (Supabase ships the `citext` extension; enable it from **Database → Extensions** if pgferry reports it as required).

See the [MySQL guide](/guides/mysql/) and [type mapping](/reference/type-mapping/) for the full list.

## Step-by-step MySQL to Supabase migration flow

1. Create the Supabase project and grab the session-pooler connection string from the **Connect** dialog.
2. `alter role postgres set statement_timeout = '0';` (see above).
3. Generate a config with `pgferry wizard`, or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every reported warning (skipped indexes, generated columns, required extensions).
6. Run `pgferry migrate migration.toml`. If interrupted, rerun the same command — `resume = true` continues from the checkpoint.
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/) — pgferry migrates tables, data, indexes, FKs, and sequences, not application logic.

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs the `row_count` (or `sampled_hash`) check without redoing DDL or `COPY`.
- Confirm Supabase **Database → Extensions** has every extension your schema needs enabled.
- Spot-check enum/set columns and any `tinyint(1)` columns for the mapping you chose.
- Restore the `postgres` role `statement_timeout`.
- Walk the full [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/) before pointing traffic at Supabase.

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| `canceling statement due to statement timeout` | Supabase 2-min role timeout | `alter role postgres set statement_timeout = '0'` for the load |
| `prepared statement ... does not exist` / odd session errors | Connected via transaction pooler (6543) | Use the session pooler (5432) or direct connection |
| `could not translate host name` / no route | Direct host is IPv6-only on an IPv4 host | Use the session pooler, or enable the IPv4 add-on |
| `extension "citext" does not exist` (or similar) | Extension not enabled | Enable it in **Database → Extensions** |
| SSL/connection refused | Missing TLS | Append `?sslmode=require` |

See [common failures and recovery](/operations/common-failures-and-recovery/) for the general catalog.

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MySQL to Neon](/guides/mysql-to-neon/) · [MySQL to Railway Postgres](/guides/mysql-to-railway-postgres/) · [MySQL to Render Postgres](/guides/mysql-to-render-postgres/) · [MySQL to PlanetScale Postgres](/guides/mysql-to-planetscale-postgres/) · [MSSQL to Supabase](/guides/mssql-to-supabase/)
