---
title: How To Read Plan Output
description: Treat pgferry plan output as a worklist, not a warning dump.
---

`pgferry plan` is the safest first command because it tells you what pgferry will not guess about automatically.

## Pay attention to these sections first

- unsupported source column types
- generated columns
- skipped or unsupported indexes
- views, routines, and source triggers
- required PostgreSQL extensions
- collation warnings

## How to respond

| Plan output | Usual response |
| --- | --- |
| unsupported type | decide on a type-mapping override or stop and redesign |
| generated column warning | recreate the expression later with hooks or application DDL |
| unsupported index warning | decide whether PostgreSQL needs an equivalent or a different design |
| view/routine/trigger warning | write `after_all` hook SQL or separate DDL |
| extension requirement | install it up front or let pgferry create it when supported |
