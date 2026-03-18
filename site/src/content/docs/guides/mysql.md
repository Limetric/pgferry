---
title: MySQL To PostgreSQL
description: MySQL-specific pgferry behavior, type-mapping knobs, and recommended starting paths.
---

MySQL is the richest pgferry source because it includes enums, sets, unsigned types, generated columns, optional PostGIS migration, and collation handling.

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
