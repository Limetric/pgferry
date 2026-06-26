---
title: MySQL to Railway Postgres
description: MySQL to Railway Postgres playbook — pgferry moves MySQL to a Railway PostgreSQL service via the public TCP proxy, self-signed TLS, and private networking.
---

This is an operator's playbook for a **MySQL to Railway Postgres** migration. It covers the Railway-specific connection details — the public TCP proxy, self-signed TLS, and private networking — that get MySQL into a [Railway](https://railway.com/) PostgreSQL service without surprises.

If you searched for how to **migrate MySQL to Railway Postgres** or **move a MySQL database to Railway**, the short version is: use Railway's **public proxy URL** (`DATABASE_PUBLIC_URL`) with `sslmode=require` from an external host, run the migration **inside Railway** to avoid egress charges when you can, and let pgferry's [MySQL type mapping](/reference/type-mapping/) handle enums, sets, and unsigned integers.

## What this guide is for

Use this guide when you have a live MySQL database and want it on a Railway-hosted PostgreSQL service. It assumes you have added a Postgres database to a Railway project. For source-side behavior that is not Railway-specific, read the generic [MySQL to PostgreSQL guide](/guides/mysql/) alongside this page.

## Why use pgferry instead of generic pgloader advice

`pgloader` is the usual "mysql to railway postgres" suggestion, and it tends to fall down on real schemas:

- No resume — a drop over Railway's TCP proxy means restarting the whole load. `pgferry` checkpoints and resumes.
- MySQL enums, sets, unsigned integers, `tinyint(1)`, and zero dates are explicit, documented knobs in `pgferry`.
- `pgferry` streams with chunked, parallel `COPY` and runs a [`plan`](/operations/how-to-read-plan-output/) preflight that surfaces skipped indexes, generated columns, and required extensions first.

## Destination prerequisites

- A Railway project with a Postgres service. Default database is `railway`, default user is `postgres`.
- The connection variables from the service's **Variables** tab. Railway's Postgres image runs as a single-tenant container, so the `postgres` role has broad privileges — `CREATE SCHEMA`, `CREATE EXTENSION` (for extensions whose binaries ship in the image), etc.
- For pgvector or other non-default extensions, deploy the matching Railway template/image variant — the base image only carries the standard contrib set.

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

## Railway DSN, TLS, proxy, and networking notes

Railway exposes two connection URLs on the Postgres service:

| Variable | Host shape | Use from |
| --- | --- | --- |
| `DATABASE_URL` (private) | `postgres.railway.internal:5432` | Another service **inside** the same Railway project |
| `DATABASE_PUBLIC_URL` (public) | `<name>.proxy.rlwy.net:<random-port>` | An **external** migration host |

- **From your laptop or any external host, use `DATABASE_PUBLIC_URL`.** The `.railway.internal` host is not routable outside Railway. The public proxy port is **randomly assigned** — never hardcode `5432` for the external endpoint; read it from the variable.
- **TLS:** Railway's Postgres image uses a **self-signed** certificate. Use `?sslmode=require` (encrypts the connection without certificate-chain verification). `sslmode=verify-full` will fail because Railway does not publish a CA for the auto-generated cert.
- **Egress:** traffic through the public proxy leaves Railway's network and **counts toward egress/network billing**. A multi-GB migration over the proxy can add up.
- **Private networking:** if you run pgferry as a service **inside the same Railway project**, use `DATABASE_URL` (the internal host) — no egress charges, lower latency. Note the private network is IPv6-based; the Go/`pgx` driver pgferry uses handles this as long as the host resolves.

Example external (public proxy) target DSN:

```bash
export PGFERRY_TARGET_DSN='postgres://postgres:<password>@<name>.proxy.rlwy.net:<proxy-port>/railway?sslmode=require'
export PGFERRY_SOURCE_DSN='user:pass@tcp(mysql-host:3306)/source_db'
```

Inside Railway (same project, no egress):

```bash
export PGFERRY_TARGET_DSN='postgres://postgres:<password>@postgres.railway.internal:5432/railway?sslmode=require'
```

The private network is isolated to your Railway project, so `sslmode=disable` also connects internally — but Railway's image serves SSL on the same port, so keep `sslmode=require` to encrypt the wire with no practical downside.

### Capacity gotcha

The Railway Postgres service is backed by a volume with a plan-dependent size cap, and WAL plus indexes built during the load consume extra space transiently. Pre-size or upgrade the volume before a large import so you do not run out mid-load.

## Source-specific caveats (MySQL)

Decide these on the MySQL side (full detail in the [MySQL guide](/guides/mysql/)):

- `enum_mode` / `set_mode` — how `ENUM`/`SET` land in PostgreSQL.
- `tinyint1_as_boolean` — only if `tinyint(1)` means boolean.
- `widen_unsigned_integers` / `add_unsigned_checks` — preserve unsigned ranges.
- `zero_date_mode` — `0000-00-00` → `NULL` or error.
- Generated columns copy as values; `FULLTEXT`, prefix, and expression indexes are reported and skipped.
- `ci_as_citext = true` needs the `citext` extension (present in the base image's contrib set).

## Step-by-step MySQL to Railway Postgres migration flow

1. Add Postgres to your Railway project and copy `DATABASE_PUBLIC_URL` (or use the internal URL if running inside Railway).
2. Confirm the volume is large enough for the dataset plus index/WAL overhead.
3. Generate a config with `pgferry wizard` or start from the snippet above.
4. Export `PGFERRY_SOURCE_DSN` and `PGFERRY_TARGET_DSN`.
5. Run `pgferry plan migration.toml` and resolve every warning.
6. Run `pgferry migrate migration.toml`; rerun on interruption (`resume = true`).
7. Recreate views, routines, and triggers via [hooks](/reference/hooks/).

## Validation and cutover checklist

- `pgferry validate migration.toml` re-runs validation without redoing DDL or `COPY`.
- Confirm required extensions exist (and that you used the right image variant for pgvector etc.).
- Verify the volume has headroom after the load.
- Spot-check enum/set and `tinyint(1)` columns.
- Walk the [cutover checklist](/operations/cutover-checklist/) and [first production migration checklist](/operations/first-production-migration-checklist/).

## Common failures for this provider pair

| Symptom | Cause | Fix |
| --- | --- | --- |
| `could not translate host name "postgres.railway.internal"` | Used the private host from outside Railway | Use `DATABASE_PUBLIC_URL` |
| TLS / certificate verification error | `verify-full` against a self-signed cert | Use `sslmode=require` |
| Connection refused on `:5432` externally | Hardcoded port instead of proxy port | Use the random proxy port from `DATABASE_PUBLIC_URL` |
| `No space left on device` mid-load | Volume too small for data + indexes | Pre-size/upgrade the volume |
| Unexpected network bill | Bulk load over the public proxy | Run the migration inside Railway (private network) |

See [common failures and recovery](/operations/common-failures-and-recovery/).

## Related

- [MySQL to PostgreSQL](/guides/mysql/) — generic source guide
- [Configuration reference](/reference/configuration/)
- [Type mapping](/reference/type-mapping/)
- [MySQL minimal-safe example](/examples/mysql/minimal-safe/)
- [Cutover checklist](/operations/cutover-checklist/) · [First production migration checklist](/operations/first-production-migration-checklist/)
- Other destinations: [MySQL to Supabase](/guides/mysql-to-supabase/) · [MySQL to Neon](/guides/mysql-to-neon/) · [MySQL to Render Postgres](/guides/mysql-to-render-postgres/) · [MySQL to PlanetScale Postgres](/guides/mysql-to-planetscale-postgres/)
