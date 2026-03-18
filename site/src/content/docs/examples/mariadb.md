---
title: MariaDB Examples
description: MariaDB migration configs and guidance for first-class MariaDB-to-PostgreSQL migrations.
---

MariaDB is a first-class source type in `pgferry`. The example set is still intentionally small, but the config validation, type mapping, wizard support, and guide surface are explicit about MariaDB rather than treating it as a footnote.

## Available examples

- [`minimal-safe`](https://github.com/Limetric/pgferry/tree/main/examples/mariadb/minimal-safe)

## Related docs

- [MariaDB guide](/guides/mariadb/)

## What is different from MySQL

- Use `source.type = "mariadb"` explicitly.
- MySQL-family type mapping options such as enum/set handling, UUID coercions, and zero-date controls still apply.
- `[postgis]` is intentionally unsupported for MariaDB in this release. Use `type_mapping.spatial_mode` fallback modes for spatial columns instead.

## Start here

Use `minimal-safe` first, then layer on `validation`, `resume`, and hooks the same way you would for a MySQL migration.
