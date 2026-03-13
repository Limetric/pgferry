---
title: Cutover Checklist
description: A short practical checklist for the final pgferry rehearsal and cutover run.
---

## Before the run

1. Confirm the exact config file and hook files you intend to use.
2. Run `pgferry plan` and make sure every warning has an owner.
3. Decide whether the run is safe-default, fast-disposable, or resumable.
4. Confirm required PostgreSQL extensions are installed or configured to auto-create.

## During the run

1. Watch for table-count progress and warnings, not just process exit status.
2. If the run is long, confirm checkpoint behavior and target durability settings match your expectations.
3. Do not change config flags mid-run if you expect to resume from the checkpoint.

## Before application cutover

1. Review validation output.
2. Run any `after_all` verification SQL that matters to your application.
3. Confirm views, routines, generated-column follow-ups, and unsupported-index replacements are in place.
4. Test the application against PostgreSQL before declaring the migration complete.
