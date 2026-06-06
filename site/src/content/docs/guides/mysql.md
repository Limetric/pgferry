---
title: MySQL To PostgreSQL
description: MySQL-specific pgferry behavior, type-mapping knobs, and recommended starting paths.
---

MySQL is still the richest pgferry source because it includes enums, sets, unsigned types, generated columns, optional PostGIS migration, and collation handling. If your source is MariaDB, use the dedicated [MariaDB guide](/guides/mariadb/) rather than assuming every MySQL-specific feature applies unchanged.

## Start here

- [minimal-safe example](/examples/mysql/minimal-safe/) for the first real rehearsal
- [chunked-resume example](/examples/mysql/chunked-resume/) when restart cost matters
- [hooks example](/examples/mysql/hooks/) if `plan` reports manual follow-up work

## MySQL-specific options to decide deliberately

- `enum_mode`
- `set_mode`
- `tinyint1_as_boolean`
- `binary16_as_uuid`
- `string_uuid_as_uuid`
- `widen_unsigned_integers`
- `add_unsigned_checks`
- `ci_as_citext`
- `[postgis]`

## Common caveats

- generated columns are copied as values, not recreated as expressions
- unsupported indexes such as `FULLTEXT`, prefix indexes, and expression indexes are reported and skipped
- `single_tx` is available when you need one consistent snapshot on a live source
- zero dates need explicit handling through `zero_date_mode`

## Migrating to a managed Postgres provider?

These destination-specific guides add the provider connection, TLS, pooling, and firewall setup on top of the MySQL behavior above:

- [MySQL to Supabase](/guides/mysql-to-supabase/)
- [MySQL to Neon](/guides/mysql-to-neon/)
- [MySQL to Railway Postgres](/guides/mysql-to-railway-postgres/)
- [MySQL to Render Postgres](/guides/mysql-to-render-postgres/)
- [MySQL to PlanetScale Postgres](/guides/mysql-to-planetscale-postgres/)
