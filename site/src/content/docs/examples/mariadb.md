---
title: MariaDB Examples
description: MariaDB migration configs that reuse the MySQL-family path without pretending PostGIS support exists yet.
---

MariaDB is a first-class source type in `pgferry`, but the example set is intentionally small for now.

## Available examples

- [`minimal-safe`](https://github.com/Limetric/pgferry/tree/main/examples/mariadb/minimal-safe)

## What is different from MySQL

- Use `source.type = "mariadb"` explicitly.
- MySQL-family type mapping options such as enum/set handling, UUID coercions, and zero-date controls still apply.
- `[postgis]` is intentionally unsupported for MariaDB in this release. Use `type_mapping.spatial_mode` fallback modes for spatial columns instead.

## Start here

Use `minimal-safe` first, then layer on `validation`, `resume`, and hooks the same way you would for a MySQL migration.
