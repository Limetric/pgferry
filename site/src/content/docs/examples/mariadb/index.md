---
title: MariaDB Examples
description: MariaDB-to-PostgreSQL example with explicit source.type = mariadb, MySQL-family type mapping, and spatial fallback since PostGIS is unsupported.
---

MariaDB is a first-class source in `pgferry` — the example set here is just smaller than MySQL's on purpose.

## Start here

- [minimal-safe](/examples/mariadb/minimal-safe/) for first production-style rehearsals

## Why there is only one example today

- MariaDB shares most operational decisions with MySQL
- the main thing that should stay explicit is `source.type = "mariadb"`
- `[postgis]` remains unsupported for MariaDB, so spatial columns should use `type_mapping.spatial_mode` fallback modes

## Related docs

- [MariaDB guide](/guides/mariadb/)
- [Minimal-safe migration pattern](/migration-patterns/minimal-safe/)
