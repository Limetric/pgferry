---
title: Configuration
description: Full TOML reference for pgferry migration configs, including defaults, safe starting points, and source-specific constraints.
---

Every migration run is driven by a single TOML file:

```bash
pgferry migrate migration.toml
pgferry validate migration.toml
```

If you want the fastest safe starting point, use the wizard first:

```bash
pgferry wizard
```

Shorthand: `pgferry run` is the same as `migrate`, and `generate` / `init` are aliases for `wizard` — use whichever muscle memory you have.

The wizard keeps the default path short, then offers an optional advanced section for `validation`, `resume`, `chunk_size`, and `index_workers`. If you skip that section, the generated TOML stays on the minimal safe/default path.

## Minimal config

```toml
schema = "app"

[source]
type = "mysql" # or "mariadb" / "sqlite" / "mssql"
dsn = "root:root@tcp(127.0.0.1:3306)/source_db"

[target]
dsn = "postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable"
```

Any PostgreSQL `sslmode` is supported. `sslmode=disable` is just a local example.

## Environment overrides for secrets

If you do not want credentials committed into the TOML, pgferry can override the DSNs from environment variables after loading the file:

```bash
export PGFERRY_SOURCE_DSN='root:root@tcp(127.0.0.1:3306)/source_db'
export PGFERRY_TARGET_DSN='postgres://postgres:postgres@127.0.0.1:5432/target_db?sslmode=disable'
pgferry migrate migration.toml
```

Rules:

- `PGFERRY_SOURCE_DSN` overrides `source.dsn` when set to a non-empty value.
- `PGFERRY_TARGET_DSN` overrides `target.dsn` when set to a non-empty value.
- Empty strings do not override the TOML.
- Validation still runs on the effective DSNs after the override is applied.

That means a committed config can omit the secret values entirely:

```toml
schema = "app"

[source]
type = "mysql"

[target]
```

`[source].type` is still required so pgferry knows which source backend and validation rules to apply.

## Recommended starting points

Use one of these before you start tuning smaller details.

### First production-style migration

```toml
schema = "app"
on_schema_exists = "error"
unlogged_tables = false
resume = true
validation = "row_count"
chunk_size = 100000
```

Why: this keeps target data durable, enables checkpoint reuse, and gives you a row-count sanity check before cutover.

### Fast disposable rehearsal

```toml
schema = "app"
on_schema_exists = "recreate"
unlogged_tables = true
clean_orphans = true
```

Why: this is the fastest full-load path when the target schema can be dropped and rebuilt.

## Full reference

### Top-level settings

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `schema` | string | required | Target PostgreSQL schema name. |
| `table_filter_mode` | string | `"exact"` | Table-filter matching mode. `"exact"` preserves current exact-name behavior. `"glob"` enables case-insensitive `*` and `?` patterns in `include_tables` and `exclude_tables`. Character classes like `[]` are intentionally not supported. |
| `column_filter_mode` | string | `"exact"` | Column-filter matching mode. `"exact"` matches source column names exactly, case-insensitively. `"glob"` enables case-insensitive `*` and `?` patterns in `exclude_columns`. |
| `include_tables` | array of strings | `[]` | If non-empty, only migrate these source tables. Entries are exact names by default, or glob patterns when `table_filter_mode = "glob"`. |
| `exclude_tables` | array of strings | `[]` | Source tables to skip. Applied after `include_tables`, so excludes win when both match the same table. |
| `exclude_columns` | array of strings | `[]` | Source columns to skip. Use `ColumnName` to skip matching columns in any table, or `TableName.ColumnName` to scope a rule to one source table. Entries are exact by default, or glob patterns in either the table or column part when `column_filter_mode = "glob"`. |
| `on_schema_exists` | string | `"error"` | `"error"` aborts if the schema exists. `"recreate"` drops and recreates it. `"use"` requires the schema to already exist and be empty, then pgferry creates objects inside it. |
| `schema_only` | bool | `false` | Create schema objects only. Skip data COPY. |
| `data_only` | bool | `false` | Load data into an existing schema, then reset sequences. Requires the target role to disable and re-enable triggers on the selected target tables during the load. |
| `truncate_before_copy` | bool | `false` | Run `TRUNCATE TABLE ... CASCADE` on the selected target tables after `before_data` hooks and before COPY. Useful with `data_only` when an external schema migrator pre-seeds reference rows that should be replaced by source data. In full migrations, target tables are normally freshly created, so this is usually a no-op. |
| `source_snapshot_mode` | string | `"none"` | `"none"` is fastest. `"single_tx"` gives one consistent source snapshot on MySQL, MariaDB, and MSSQL. |
| `identifier_case` | string | `"snake"` | How source identifiers map to PostgreSQL names. `"snake"` converts `OrderItems` → `order_items`. `"lower"` lowercases only (`OrderItems` → `orderitems`). `"preserve"` keeps the source casing unchanged (`OrderItems` → `OrderItems`); PostgreSQL DDL is always quoted so mixed-case names are safe. |
| `unlogged_tables` | bool | `true` | Use `UNLOGGED` tables during full loads, then `SET LOGGED` later. |
| `preserve_defaults` | bool | `true` | Keep source column defaults in the created PostgreSQL schema. |
| `add_unsigned_checks` | bool | `false` | Add `CHECK` constraints for MySQL-family unsigned ranges. |
| `clean_orphans` | bool | `true` | Automatically delete or null invalid child rows before FK creation. |
| `clean_orphans_mode` | string | `"apply"` | `"apply"` fixes rows before FKs. `"report"` logs what would happen and stops before FK creation (dry-run style). |
| `clean_orphans_max_rows` | int | `0` | Safety cap: abort if orphan rows to fix would exceed this total. `0` means no cap. |
| `replicate_on_update_current_timestamp` | bool | `false` | Create PostgreSQL trigger emulation for MySQL-family `ON UPDATE CURRENT_TIMESTAMP`. |
| `workers` | int | `min(runtime.NumCPU(), 8)` | Parallel worker count for data loading. SQLite is internally capped at 1. |
| `index_workers` | int | `workers` | Concurrent index builds during post-migration. |
| `chunk_size` | int | `100000` | Key-range width for chunkable single-column numeric PK tables. Actual rows per chunk vary with key density. |
| `copy_risk_analysis` | bool | `true` | Lightweight COUNT/MIN/MAX probes to flag awkward COPY chunking (huge tables, sparse keys, etc.). Feeds `plan` copy-risk output and optional migrate warnings. On long runs, emits periodic progress logs (current table and probe stage). Turn off for quieter runs when you already know the shape. |
| `resume` | bool | `false` | Reuse `pgferry_checkpoint.json` after interruptions. |
| `validation` | string | `"none"` | `"row_count"` compares per-table counts after load. `"sampled_hash"` adds bounded content fingerprints for deterministic primary-key-addressable rows. `pgferry validate` reuses this setting without rerunning migration stages. |

Table filters always match source table names, not the transformed PostgreSQL names. In `glob` mode the matching is case-insensitive, mirroring exact-mode normalization. If a filter entry matches no source table, pgferry fails early instead of silently migrating the wrong scope.

Column filters also match source names, not transformed PostgreSQL names. When a column is excluded, pgferry omits it from `CREATE TABLE`, source `SELECT`, and target `COPY`. Primary keys, plain-column indexes, and foreign keys that reference excluded columns are skipped. pgferry cannot inspect arbitrary expression index definitions for excluded column references; expression indexes are already reported as unsupported and skipped separately. If a filter entry matches no source column in the migrated schema, pgferry fails early.

Changing `table_filter_mode`, `include_tables`, `exclude_tables`, `column_filter_mode`, or `exclude_columns` can change the selected migration scope. When `resume = true`, that can invalidate the checkpoint fingerprint and force a fresh run.

`truncate_before_copy` follows the selected table scope after filters. Excluded tables are not named in pgferry's generated `TRUNCATE TABLE ... CASCADE` statement, though PostgreSQL can still truncate dependent tables through `CASCADE`.

### `[source]`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `type` | string | required | `"mysql"`, `"mariadb"`, `"sqlite"`, or `"mssql"`. |
| `dsn` | string | required unless `PGFERRY_SOURCE_DSN` is set | Source connection string or SQLite file path/URI. |
| `charset` | string | `"utf8mb4"` | MySQL and MariaDB only. Injected into the DSN unless already present. |
| `source_schema` | string | `"dbo"` | MSSQL only. Limits introspection to one source schema. |

### `[target]`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `dsn` | string | required unless `PGFERRY_TARGET_DSN` is set | PostgreSQL connection string. If it does not set `pool_max_conns`, pgferry raises target pool `MaxConns` to at least `max(workers, index_workers)`. If `pool_max_conns` is set explicitly, pgferry preserves it and warns when it is lower than that concurrency. |

### `[type_mapping]`

Use [Type Mapping](/reference/type-mapping/) for the full behavior tables. These are the knobs you can set directly:

```toml
[type_mapping]
tinyint1_as_boolean = false
binary16_as_uuid = false
datetime_as_timestamptz = false
varchar_as_text = false
json_as_jsonb = true
widen_unsigned_integers = true
sanitize_json_null_bytes = true
unknown_as_text = false
enum_mode = "check"
set_mode = "text"
bit_mode = "bytea"
string_uuid_as_uuid = false
binary16_uuid_mode = "rfc4122"
time_mode = "time"
zero_date_mode = "null"
spatial_mode = "off"
nvarchar_as_text = false
money_as_numeric = true
xml_as_text = false
collation_mode = "none"
ci_as_citext = false
```

Optional MySQL-family collation remapping:

```toml
[type_mapping.collation_map]
utf8mb4_general_ci = "und-x-icu"
utf8mb4_unicode_ci = "und-x-icu"
```

### `[postgis]`

| Key | Type | Default | Notes |
| --- | --- | --- | --- |
| `enabled` | bool | `false` | MySQL only. Maps spatial columns to PostgreSQL `geometry`. MariaDB should use `type_mapping.spatial_mode` fallback modes instead. |
| `create_extension` | bool | `false` | When true, pgferry runs `CREATE EXTENSION IF NOT EXISTS postgis`. |

### `[hooks]`

```toml
[hooks]
before_data = []
after_data = []
before_fk = []
after_all = []
```

Hook file paths are resolved relative to the config file directory. See [Hooks](/reference/hooks/) for phase details and templating behavior.

## DSN formats

### SQLite

pgferry opens SQLite in read-only mode and requires a real file.

| Format | Example | Result |
| --- | --- | --- |
| Plain path | `/data/app.db` | Normalized to `file:/data/app.db?mode=ro` |
| Relative path | `./relative.db` | Normalized to `file:./relative.db?mode=ro` |
| File URI | `file:/data/app.db?cache=shared` | Existing params kept, `mode=ro` appended |

Not supported: `:memory:`, `file::memory:`, or `mode=memory`.

### MSSQL

| Format | Example | Notes |
| --- | --- | --- |
| URL | `sqlserver://user:pass@host:1433?database=mydb` | Recommended. |
| URL with instance | `sqlserver://user:pass@host/instance?database=mydb` | Named instances. |
| ADO | `server=host;user id=user;password=pass;database=mydb` | Legacy format. |

The `database` parameter is required because pgferry extracts the DB name for introspection queries.

## Source-specific constraints

| Setting | MySQL | MariaDB | SQLite | MSSQL |
| --- | --- | --- | --- | --- |
| `source_snapshot_mode = "single_tx"` | Yes | Yes | No | Yes |
| Worker parallelism | Yes | Yes | Forced to 1 | Yes |
| `source.charset` | Yes | Yes | Error | Error |
| `source.source_schema` | N/A | N/A | N/A | Yes |
| MySQL-family type options | Yes | Yes | Error | Error |
| `datetime_as_timestamptz` for date/time types | Yes | Yes | Yes (`DATETIME` / `TIMESTAMP` → `timestamptz`) | Yes |
| MSSQL-only type options | Error | Error | Error | Yes |
| `[postgis]` | Yes | Error | Error | Error |
| `collation_mode` / `collation_map` / `ci_as_citext` | Yes | Yes | Error | Error |

## Important incompatibilities

| Combination | Result |
| --- | --- |
| `resume = true` + `on_schema_exists = "recreate"` | Invalid because the target schema would be dropped. |
| `resume = true` + `on_schema_exists = "use"` | Invalid because a resumed run would re-enter a now non-empty schema. |
| `resume = true` + `schema_only = true` | Invalid because there is no data stage to resume. |
| `resume = true` + `truncate_before_copy = true` | Invalid because a resumed run would truncate rows that the checkpoint may skip. |
| `resume = true` + `unlogged_tables = true` | Invalid because checkpointed progress could outlive crash-truncated tables. |
| `schema_only = true` + `data_only = true` | Invalid. Choose one or neither. |
| `schema_only = true` + `truncate_before_copy = true` | Invalid because no data phase runs. |
| SQLite + `source_snapshot_mode = "single_tx"` | Invalid. SQLite only supports `none`. |
| MariaDB + `[postgis]` | Invalid. Use `type_mapping.spatial_mode` fallback modes instead. |

## Practical guidance

- Run `pgferry plan migration.toml` before the first real migration.
- Use `pgferry validate migration.toml` when you need to rerun built-in validation later without rerunning schema creation or data copy.
- Use [Operator tuning](/operations/operator-tuning/) when runtime is dominated by PostgreSQL settings, source pressure, or network distance rather than config shape alone.
- Use `unlogged_tables = false` whenever you also need `resume = true`.
- Use `source_snapshot_mode = "single_tx"` when the source stays live during the migration and you need one consistent read view.
- Keep `on_schema_exists = "error"` for the first production dry runs so you do not destroy previous target state by mistake.
- Use `on_schema_exists = "use"` only when infrastructure or policy pre-creates the schema shell and you want pgferry to own everything inside it from empty.
- Use `truncate_before_copy = true` with `data_only` when the target schema was created by another migrator and contains seed rows that should be replaced before COPY.
- Expect `data_only` to fail early on PostgreSQL roles that cannot run `ALTER TABLE ... DISABLE/ENABLE TRIGGER ALL`; rehearse that path with the real target role before cutover.
- Prefer hooks for views, routines, cleanup SQL, or post-load validation queries that are specific to your application.
