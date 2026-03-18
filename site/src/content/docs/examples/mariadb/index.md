---
title: MariaDB Examples
description: MariaDB-to-PostgreSQL example playbooks with explicit MariaDB config and MySQL-family type mapping choices.
---

MariaDB is a first-class source type in `pgferry`, even though the example surface is intentionally smaller than MySQL.

## Start here

- [minimal-safe](/examples/mariadb/minimal-safe/) for first production-style rehearsals

## Why there is only one example today

- MariaDB shares most operational decisions with MySQL
- the main thing that should stay explicit is `source.type = "mariadb"`
- `[postgis]` remains unsupported for MariaDB, so spatial columns should use `type_mapping.spatial_mode` fallback modes

## Related docs

- [MariaDB guide](/guides/mariadb/)
- [Minimal-safe migration pattern](/migration-patterns/minimal-safe/)
