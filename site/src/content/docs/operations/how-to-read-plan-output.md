---
title: How To Read Plan Output
description: Treat pgferry plan output as a worklist, not a warning dump.
---

`pgferry plan` is the safest first command because it tells you what pgferry will not guess about automatically.

## Pay attention to these sections first

- unsupported source column types
- schema semantic warnings for skipped or lossy defaults, CHECK constraints, comments, and partitioning
- generated columns
- skipped or unsupported indexes
- views, routines, and source triggers
- required PostgreSQL extensions
- collation warnings
- copy risk findings (when `copy_risk_analysis` is enabled — large or awkward tables worth a second look before COPY day)

## How to respond

| Plan output | Usual response |
| --- | --- |
| unsupported type | decide on a type-mapping override or stop and redesign |
| schema semantic warning | decide whether to recreate the behavior with PostgreSQL DDL or hook SQL |
| generated column warning | recreate the expression later with hooks or application DDL |
| unsupported index warning | decide whether PostgreSQL needs an equivalent or a different design |
| view/routine/trigger warning | write `after_all` hook SQL or separate DDL |
| extension requirement | install it up front or let pgferry create it when supported |
| copy risk finding | sanity-check chunking strategy, `chunk_size`, or table shape before the long run |

## JSON output and replays

`--format json` emits the full report as JSON — great for archives, diffs, or feeding something that isn't a human.

`--input previous.json` reprints or re-checks a saved report without talking to the source. Pair with `--fail-on` in CI so the pipeline fails on unsupported columns or high-severity copy risks even when the database is asleep.

`--fail-on` levels: `none` (always exit 0 if parsing works), `errors` (unsupported types), `warnings` (errors plus high-severity copy risks). Pick your own adventure.
