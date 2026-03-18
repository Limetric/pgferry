---
title: MariaDB To PostgreSQL
description: MariaDB-specific pgferry behavior, MySQL-family knobs, and the places where MariaDB differs from MySQL.
---

MariaDB is a first-class pgferry source type. It shares most MySQL-family behavior, but pgferry treats it separately so config validation, examples, and type handling can be explicit about MariaDB-specific behavior.

## Start here

- [minimal-safe example](/examples/mariadb/minimal-safe/)
- [configuration reference](/reference/configuration/)
- [type mapping reference](/reference/type-mapping/)

## MariaDB-specific decisions

- set `source.type = "mariadb"` explicitly instead of reusing `mysql`
- use the MySQL-family mapping knobs deliberately: `enum_mode`, `set_mode`, `binary16_as_uuid`, `string_uuid_as_uuid`, `zero_date_mode`, `collation_mode`, and `ci_as_citext`
- choose `type_mapping.spatial_mode` deliberately for spatial columns because `[postgis]` is intentionally unsupported for MariaDB
- keep `source_snapshot_mode = "single_tx"` in mind when the source stays live during the migration

## What pgferry handles for MariaDB

- native `uuid` columns map directly to PostgreSQL `uuid`
- MariaDB JSON aliases detected through `JSON_VALID(...)` checks preserve `json` / `jsonb` semantics
- MySQL-family enum, set, unsigned integer, collation, and UUID conversion behaviors still apply
- generated columns are copied as values and reported for manual semantic follow-up

## Common caveats

- PostGIS migration is MySQL-only in this release; MariaDB should use spatial fallback modes instead
- unsupported indexes such as `FULLTEXT`, prefix indexes, and expression indexes are still reported and skipped rather than guessed
- if you are coming from existing MySQL configs or automation, make sure they do not leave `source.type = "mysql"` behind by habit
