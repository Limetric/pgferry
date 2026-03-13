---
title: MySQL Examples
description: Practical MySQL migration configurations for common cutover styles.
---

MySQL has the broadest example set in the repository because it exercises the most pgferry features.

## Available examples

- [`minimal-safe`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/minimal-safe)
- [`recreate-fast`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/recreate-fast)
- [`hooks`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/hooks)
- [`sakila`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/sakila)
- [`schema-only`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/schema-only)
- [`data-only`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/data-only)
- [`chunked-resume`](https://github.com/Limetric/pgferry/tree/main/examples/mysql/chunked-resume)

## Start here

If the source is production and you want the least drama:

1. Begin with `minimal-safe`.
2. Run `pgferry plan`.
3. Add `validation = "row_count"`, `resume = true`, and `unlogged_tables = false`.
4. Only switch to `recreate-fast` after you understand the operational tradeoff.

## MySQL-only features to look for

- enum and set handling
- unsigned integer widening and optional checks
- `binary(16)` UUID conversion
- `citext` mapping for `_ci` collations
- native PostGIS migration for spatial columns
