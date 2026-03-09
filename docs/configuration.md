# Configuration

pgferry is configured with a single TOML file passed as the first argument:

```bash
pgferry migration.toml
```

If you want help creating that file, start the interactive wizard instead:

```bash
pgferry generate
```

The wizard walks through the main migration settings and lets you either save the generated
TOML for later reuse, execute it immediately, or do both.

## Full reference

```toml
# Target PostgreSQL schema name (required)
schema = "app"

# What to do if the schema already exists: "error" (default) or "recreate"
# "error"    — abort if the schema exists
# "recreate" — DROP CASCADE then re-create
on_schema_exists = "error"

# DDL only — create tables, PKs, indexes, FKs, sequences, triggers; skip data
# Default: false
schema_only = false

# Data only — assumes schema exists; stream data + reset sequences
# Mutually exclusive with schema_only
# Default: false
data_only = false

# Source read consistency mode:
#   "none"      — each table is read in its own connection (parallel, default)
#   "single_tx" — all tables read inside one read-only transaction (sequential, MySQL only)
source_snapshot_mode = "none"

# Convert source identifiers to snake_case (e.g. userName → user_name)
# snake_case is the de facto standard for PostgreSQL identifiers.
# Enable this for idiomatic PG naming. When disabled, source names are
# lowercased to match PostgreSQL's default case folding.
# Default: true
snake_case_identifiers = true

# Use UNLOGGED tables during bulk load, then SET LOGGED after
# Faster writes because WAL is skipped, but data is lost on crash during migration
# Ignored when schema_only or data_only is true
# Default: false
unlogged_tables = false

# Preserve source column DEFAULT values in the PostgreSQL schema
# When false, defaults are omitted from CREATE TABLE
# Default: true
preserve_defaults = true

# Add CHECK constraints that enforce MySQL unsigned integer ranges
# e.g. CHECK (col >= 0 AND col <= 4294967295) for unsigned int
# Default: false
add_unsigned_checks = false

# Clean orphaned rows (child rows referencing non-existent parents) before FK creation
# When false, FK creation fails naturally if orphans exist — use before_fk hooks to handle manually
# Default: true
clean_orphans = true

# Emulate MySQL ON UPDATE CURRENT_TIMESTAMP via PG triggers
# Default: false
replicate_on_update_current_timestamp = false

# Parallel worker count for data streaming
# Default: min(runtime.NumCPU, 8)
# SQLite sources are capped at 1 worker regardless of this setting
workers = 4

# Target number of rows per chunk for range-based table splitting
# Tables with a single-column numeric primary key are split into chunks of this size
# Tables without a chunkable PK fall back to full-table copy
# Default: 100000
chunk_size = 100000

# Resume from a previous incomplete migration using the checkpoint file
# When true, completed chunks/tables are skipped on rerun
# Incompatible with on_schema_exists=recreate and schema_only
# Default: false
resume = false

# Post-load validation mode:
#   "none"      — no validation (default)
#   "row_count" — compare source and target row counts per table after data load
validation = "none"

# Source database configuration (required)
[source]
type = "mysql"                                       # "mysql" or "sqlite"
dsn = "user:pass@tcp(host:port)/dbname"              # MySQL DSN
# dsn = "/path/to/database.db"                       # SQLite file path
# dsn = "file:/path/to/database.db?cache=shared"     # SQLite file URI
charset = "utf8mb4"                                  # MySQL connection charset (default: "utf8mb4")
                                                     # Injected into DSN as charset= param unless already present
                                                     # MySQL only — config error for SQLite if not "utf8mb4"

[target]
dsn = "postgres://user:pass@host:port/dbname?sslmode=disable"

[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean instead of smallint (MySQL only)
binary16_as_uuid = false          # binary(16) → uuid instead of bytea (MySQL only)
datetime_as_timestamptz = false   # datetime → timestamptz instead of timestamp (MySQL only)
varchar_as_text = false           # varchar(n)/char(n) → text instead of varchar(n) (MySQL only)
json_as_jsonb = false             # json → jsonb instead of json
widen_unsigned_integers = true    # unsigned int → bigint; set false to keep as integer (MySQL only)
sanitize_json_null_bytes = true   # strip \x00 from JSON values (PG rejects them)
unknown_as_text = false           # map unrecognized source types to text instead of erroring

# Enum handling (MySQL only): "text" (default) stores as plain text;
#                              "check" stores as text with a CHECK constraint on allowed values;
#                              "native" creates native PostgreSQL enum types
enum_mode = "text"

# Set handling (MySQL only): "text" (default) stores as comma-separated text;
#                             "text_array" stores as text[] (PostgreSQL array);
#                             "text_array_check" stores as text[] with CHECK on allowed values
set_mode = "text"

# BIT(n) handling (MySQL only): "bytea" (default), "bit" (fixed-width), or "varbit" (variable)
bit_mode = "bytea"

# Map CHAR(36)/VARCHAR(36) to uuid (MySQL only). Values are validated during COPY.
# Default: false
string_uuid_as_uuid = false

# Binary UUID byte order (MySQL only, requires binary16_as_uuid = true):
#   "rfc4122" (default) — standard byte order
#   "mysql_uuid_to_bin_swap" — reverses MySQL UUID_TO_BIN(uuid, 1) swap
binary16_uuid_mode = "rfc4122"

# TIME column handling (MySQL only): "time" (default), "text", or "interval"
time_mode = "time"

# Zero-date handling (MySQL only): "null" (default) converts to NULL;
#                                   "error" aborts on zero dates
zero_date_mode = "null"

# Spatial type handling (MySQL only): "off" (default, unsupported);
#   "wkb_bytea" stores as bytea; "wkt_text" stores as text via ST_AsText()
spatial_mode = "off"

# Collation handling (MySQL only):
#   "none"  (default) — no COLLATE clauses added; warnings are still reported
#   "auto"  — emit COLLATE clauses for text columns based on source collation
collation_mode = "none"

# Map _ci (case-insensitive) text columns to PostgreSQL citext type (MySQL only).
# citext provides true case-insensitive comparisons, UNIQUE, ORDER BY, etc.
# Requires the citext extension (included in PostgreSQL contrib).
# Default: false
ci_as_citext = false

# Map specific MySQL collations to PostgreSQL collations (MySQL only).
# Only used when collation_mode = "auto". Keys are MySQL collation names,
# values are PG collation names.
# When both ci_as_citext and collation_map are set for the same collation,
# collation_map takes precedence (user chose COLLATE over citext).
# [type_mapping.collation_map]
# utf8mb4_general_ci = "und-x-icu"
# utf8mb4_unicode_ci = "und-x-icu"

[hooks]
before_data = []   # after table creation, before COPY
after_data = []    # after COPY, before constraints
before_fk = []     # after PKs/indexes, before FK creation
after_all = []     # after everything (views, ANALYZE, etc.)
```

See [Hooks](hooks.md) for details on the hook system.

## SQLite DSN formats

SQLite accepts file paths or file URIs. pgferry opens the database in **read-only mode**.

| Format | Example | Notes |
|---|---|---|
| Plain path | `/data/app.db` | Normalized to `file:/data/app.db?mode=ro` |
| Relative path | `./relative.db` | Normalized to `file:./relative.db?mode=ro` |
| File URI | `file:/data/app.db?cache=shared` | `mode=ro` appended to existing params |

**Not supported:** `:memory:`, `file::memory:`, `mode=memory` — pgferry requires a real file.

## Source-specific constraints

| Constraint | MySQL | SQLite |
|---|---|---|
| `source_snapshot_mode = "single_tx"` | Supported | Not supported (config error) |
| Workers | Configurable (`workers` setting) | Always 1 (capped internally) |
| `source.charset` | Supported (default `"utf8mb4"`) | Config error if not default |
| `tinyint1_as_boolean` | Supported | Config error |
| `binary16_as_uuid` | Supported | Config error |
| `datetime_as_timestamptz` | Supported | Config error |
| `varchar_as_text` | Supported | Config error |
| `enum_mode = "check"` or `"native"` | Supported | Config error |
| `set_mode = "text_array"` or `"text_array_check"` | Supported | Config error |
| `bit_mode` (non-default) | Supported | Config error |
| `string_uuid_as_uuid` | Supported | Config error |
| `binary16_uuid_mode` (non-default) | Supported | Config error |
| `time_mode` (non-default) | Supported | Config error |
| `zero_date_mode` (non-default) | Supported | Config error |
| `spatial_mode` (non-default) | Supported | Config error |
| `widen_unsigned_integers = false` | Supported | Config error |
| `collation_mode = "auto"` | Supported | Config error |
| `collation_map` | Supported | Config error if non-empty |
| `ci_as_citext` | Supported | Config error |

## Validation rules

pgferry validates the config at load time and reports errors before connecting to either database:

| Field | Rule |
|---|---|
| `schema` | Required, must be non-empty after trimming whitespace |
| `on_schema_exists` | Must be `"error"` or `"recreate"` |
| `source_snapshot_mode` | Must be `"none"` or `"single_tx"` |
| `source.type` | Required, must be `"mysql"` or `"sqlite"` |
| `source.dsn` | Required |
| `type_mapping.enum_mode` | Must be `"text"`, `"check"`, or `"native"` |
| `type_mapping.set_mode` | Must be `"text"`, `"text_array"`, or `"text_array_check"` |
| `type_mapping.bit_mode` | Must be `"bytea"`, `"bit"`, or `"varbit"` |
| `type_mapping.binary16_uuid_mode` | Must be `"rfc4122"` or `"mysql_uuid_to_bin_swap"`; requires `binary16_as_uuid = true` |
| `type_mapping.time_mode` | Must be `"text"`, `"time"`, or `"interval"` |
| `type_mapping.zero_date_mode` | Must be `"null"` or `"error"` |
| `type_mapping.spatial_mode` | Must be `"off"`, `"wkb_bytea"`, or `"wkt_text"` |
| `type_mapping.collation_mode` | Must be `"none"` or `"auto"` |
| `source.charset` | MySQL-only; config error for SQLite if not `"utf8mb4"` |
| `validation` | Must be `"none"` or `"row_count"` |
| `chunk_size` | Defaults to `100000` if &le; 0 |
| `resume` + `on_schema_exists=recreate` | Incompatible &mdash; recreate would destroy data to resume into |
| `resume` + `schema_only` | Incompatible &mdash; no data to resume |
| `schema_only` + `data_only` | Mutually exclusive &mdash; cannot both be `true` |
| `target.dsn` | Required |
| `workers` | Defaults to `min(NumCPU, 8)` if &le; 0; capped at 1 for SQLite |
| Source-specific type mappings | MySQL-only options rejected for SQLite sources |

## Defaults

Fields omitted from the TOML file use these defaults:

| Field | Default |
|---|---|
| `on_schema_exists` | `"error"` |
| `snake_case_identifiers` | `true` |
| `schema_only` | `false` |
| `data_only` | `false` |
| `source_snapshot_mode` | `"none"` |
| `unlogged_tables` | `false` |
| `preserve_defaults` | `true` |
| `add_unsigned_checks` | `false` |
| `clean_orphans` | `true` |
| `replicate_on_update_current_timestamp` | `false` |
| `workers` | `min(NumCPU, 8)` |
| `chunk_size` | `100000` |
| `resume` | `false` |
| `validation` | `"none"` |
| `tinyint1_as_boolean` | `false` |
| `binary16_as_uuid` | `false` |
| `datetime_as_timestamptz` | `false` |
| `varchar_as_text` | `false` |
| `json_as_jsonb` | `false` |
| `widen_unsigned_integers` | `true` |
| `sanitize_json_null_bytes` | `true` |
| `unknown_as_text` | `false` |
| `enum_mode` | `"text"` |
| `set_mode` | `"text"` |
| `bit_mode` | `"bytea"` |
| `string_uuid_as_uuid` | `false` |
| `binary16_uuid_mode` | `"rfc4122"` |
| `time_mode` | `"time"` |
| `zero_date_mode` | `"null"` |
| `spatial_mode` | `"off"` |
| `collation_mode` | `"none"` |
| `collation_map` | `nil` (empty) |
| `ci_as_citext` | `false` |
| `source.charset` | `"utf8mb4"` |
