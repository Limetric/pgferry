# MariaDB Minimal Safe

Conservative MariaDB example for the first production-style pass.

## Why this shape

| Setting | Value | Why |
| --- | --- | --- |
| `source.type` | `mariadb` | Uses the first-class MariaDB source route |
| `source_snapshot_mode` | `single_tx` | Reads from one consistent snapshot |
| `unlogged_tables` | `false` | Prefer crash safety over peak load speed |
| `validation` | `row_count` | Adds a cheap post-load sanity check |
| `spatial_mode` | `off` | MariaDB uses fallback spatial modes only; `[postgis]` is not supported yet |

## When to use it

- You want an explicit MariaDB config instead of adapting a MySQL example.
- The source is live enough that `single_tx` is worth the read overhead.
- You want a safe baseline before adding chunking, resume, or hooks.
