---
title: Minimal-safe
description: The safest default pgferry path for first production rehearsals and cautious cutovers.
---

Choose `minimal-safe` when you want the least surprising behavior, even if it costs some speed.

## What defines this pattern

- `on_schema_exists = "error"`
- `unlogged_tables = false`
- `clean_orphans = false`
- `preserve_defaults = true`

## Why it is the default recommendation

- it does not destroy existing target state
- it keeps target writes durable during the run
- it forces orphan cleanup to be explicit instead of silent
- it gives you a cleaner first rehearsal signal

## Start from these examples

- [MySQL minimal-safe](/examples/mysql/minimal-safe/)
- [SQLite minimal-safe](/examples/sqlite/minimal-safe/)
- [MSSQL minimal-safe](/examples/mssql/minimal-safe/)
