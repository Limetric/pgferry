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
#   "single_tx" — all tables read inside one read-only MySQL transaction (sequential)
source_snapshot_mode = "none"

# Use UNLOGGED tables during bulk load, then SET LOGGED after
# Faster writes because WAL is skipped, but data is lost on crash during migration
# Ignored when schema_only or data_only is true
# Default: false
unlogged_tables = false

# Preserve MySQL column DEFAULT values in the PostgreSQL schema
# When false (default), defaults are omitted from CREATE TABLE
# Default: false
preserve_defaults = false

# Add CHECK constraints that enforce MySQL unsigned integer ranges
# e.g. CHECK (col >= 0 AND col <= 4294967295) for unsigned int
# Default: false
add_unsigned_checks = false

# Emulate MySQL ON UPDATE CURRENT_TIMESTAMP via PG triggers
# Default: false
replicate_on_update_current_timestamp = false

# Parallel worker count for data streaming
# Default: min(runtime.NumCPU, 8)
workers = 4

[mysql]
dsn = "user:pass@tcp(host:port)/dbname"

[postgres]
dsn = "postgres://user:pass@host:port/dbname?sslmode=disable"

[type_mapping]
tinyint1_as_boolean = false       # tinyint(1) → boolean instead of smallint
binary16_as_uuid = false          # binary(16) → uuid instead of bytea
datetime_as_timestamptz = false   # datetime → timestamptz instead of timestamp
json_as_jsonb = false             # json → jsonb instead of json
sanitize_json_null_bytes = true   # strip \x00 from JSON values (PG rejects them)
unknown_as_text = false           # map unrecognized MySQL types to text instead of erroring

# Enum handling: "text" (default) stores as plain text;
#                "check" stores as text with a CHECK constraint on allowed values
enum_mode = "text"

# Set handling: "text" (default) stores as comma-separated text;
#               "text_array" stores as text[] (PostgreSQL array)
set_mode = "text"

[hooks]
before_data = []   # after table creation, before COPY
after_data = []    # after COPY, before constraints
before_fk = []     # after PKs/indexes, before FK creation
after_all = []     # after everything (views, ANALYZE, etc.)
```

See [Hooks](hooks.md) for details on the hook system.

## Validation rules

pgferry validates the config at load time and reports errors before connecting to either database:

| Field | Rule |
|---|---|
| `schema` | Required, must be non-empty after trimming whitespace |
| `on_schema_exists` | Must be `"error"` or `"recreate"` |
| `source_snapshot_mode` | Must be `"none"` or `"single_tx"` |
| `type_mapping.enum_mode` | Must be `"text"` or `"check"` |
| `type_mapping.set_mode` | Must be `"text"` or `"text_array"` |
| `schema_only` + `data_only` | Mutually exclusive &mdash; cannot both be `true` |
| `mysql.dsn` | Required |
| `postgres.dsn` | Required |
| `workers` | Defaults to `min(NumCPU, 8)` if &le; 0 |

## Defaults

Fields omitted from the TOML file use these defaults:

| Field | Default |
|---|---|
| `on_schema_exists` | `"error"` |
| `schema_only` | `false` |
| `data_only` | `false` |
| `source_snapshot_mode` | `"none"` |
| `unlogged_tables` | `false` |
| `preserve_defaults` | `false` |
| `add_unsigned_checks` | `false` |
| `replicate_on_update_current_timestamp` | `false` |
| `workers` | `min(NumCPU, 8)` |
| `tinyint1_as_boolean` | `false` |
| `binary16_as_uuid` | `false` |
| `datetime_as_timestamptz` | `false` |
| `json_as_jsonb` | `false` |
| `sanitize_json_null_bytes` | `true` |
| `unknown_as_text` | `false` |
| `enum_mode` | `"text"` |
| `set_mode` | `"text"` |
