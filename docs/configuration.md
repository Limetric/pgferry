# Configuration

pgferry is configured with a single TOML file passed as the first argument:

```bash
pgferry migration.toml
```

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

# Source database configuration (required)
[source]
type = "mysql"                                       # "mysql" or "sqlite"
dsn = "user:pass@tcp(host:port)/dbname"              # MySQL DSN
# dsn = "/path/to/database.db"                       # SQLite file path
# dsn = "file:/path/to/database.db?cache=shared"     # SQLite file URI

[postgres]
dsn = "postgres://user:pass@host:port/dbname?sslmode=disable"

[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean instead of smallint (MySQL only)
binary16_as_uuid = false          # binary(16) → uuid instead of bytea (MySQL only)
datetime_as_timestamptz = false   # datetime → timestamptz instead of timestamp (MySQL only)
json_as_jsonb = false             # json → jsonb instead of json
widen_unsigned_integers = true    # unsigned int → bigint; set false to keep as integer (MySQL only)
sanitize_json_null_bytes = true   # strip \x00 from JSON values (PG rejects them)
unknown_as_text = false           # map unrecognized source types to text instead of erroring

# Enum handling (MySQL only): "text" (default) stores as plain text;
#                              "check" stores as text with a CHECK constraint on allowed values
enum_mode = "text"

# Set handling (MySQL only): "text" (default) stores as comma-separated text;
#                             "text_array" stores as text[] (PostgreSQL array)
set_mode = "text"

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
| `tinyint1_as_boolean` | Supported | Config error |
| `binary16_as_uuid` | Supported | Config error |
| `datetime_as_timestamptz` | Supported | Config error |
| `enum_mode = "check"` | Supported | Config error |
| `set_mode = "text_array"` | Supported | Config error |
| `widen_unsigned_integers = false` | Supported | Config error |

## Validation rules

pgferry validates the config at load time and reports errors before connecting to either database:

| Field | Rule |
|---|---|
| `schema` | Required, must be non-empty after trimming whitespace |
| `on_schema_exists` | Must be `"error"` or `"recreate"` |
| `source_snapshot_mode` | Must be `"none"` or `"single_tx"` |
| `source.type` | Required, must be `"mysql"` or `"sqlite"` |
| `source.dsn` | Required |
| `type_mapping.enum_mode` | Must be `"text"` or `"check"` |
| `type_mapping.set_mode` | Must be `"text"` or `"text_array"` |
| `schema_only` + `data_only` | Mutually exclusive &mdash; cannot both be `true` |
| `postgres.dsn` | Required |
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
| `tinyint1_as_boolean` | `false` |
| `binary16_as_uuid` | `false` |
| `datetime_as_timestamptz` | `false` |
| `json_as_jsonb` | `false` |
| `widen_unsigned_integers` | `true` |
| `sanitize_json_null_bytes` | `true` |
| `unknown_as_text` | `false` |
| `enum_mode` | `"text"` |
| `set_mode` | `"text"` |
