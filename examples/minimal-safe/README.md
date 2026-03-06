# Minimal Safe Migration

A conservative starting template for first-time migrations.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `on_schema_exists` | `error` | Prevents accidentally overwriting an existing schema |
| `clean_orphans` | `false` | No automatic deletion of orphaned rows |
| `unlogged_tables` | `false` | Tables are fully WAL-logged for crash safety |
| `preserve_defaults` | `true` | Column defaults are carried over to PostgreSQL |

## When to use

- You are migrating for the first time and want the safest defaults.
- The target PostgreSQL database does not yet contain the schema.
- You prefer to handle orphan cleanup manually (or via hooks) rather than automatically.

## Usage

```bash
pgferry -config examples/minimal-safe/migration.toml
```

Edit the `[source]` and `[target]` DSNs to match your environment before running.
