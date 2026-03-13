---
title: First Production Migration Checklist
description: A practical checklist for the first real pgferry production rehearsal.
---

Use this before the first migration that matters. The goal is not speed. The goal is to avoid discovering missing assumptions during cutover.

## Config

1. Start from a `minimal-safe` example, not `recreate-fast`.
2. Keep `on_schema_exists = "error"` so you do not destroy a previous rehearsal by mistake.
3. Keep `unlogged_tables = false` unless you are explicitly running a disposable environment.
4. Turn on `validation = "row_count"` for the rehearsal that is meant to prove operational readiness.

## Source and target

1. Confirm the source DSN points at the correct database.
2. Confirm the target DSN points at the intended PostgreSQL database.
3. If the source stays live during the run, decide whether `source_snapshot_mode = "single_tx"` is needed.
4. Confirm required PostgreSQL extensions are installed or configured to auto-create.

## Plan output

1. Run `pgferry plan migration.toml`.
2. Read every warning and assign an owner.
3. Decide what happens to views, routines, and source triggers.
4. Decide how generated-column expressions will be recreated if the application depends on them.
5. Decide whether skipped indexes need PostgreSQL equivalents before the application cutover.

## Dry run discipline

1. Test the exact hook files you intend to use in production.
2. Keep the config stable once you start a resumable rehearsal.
3. Record expected runtime, largest tables, and any cleanup steps while rehearsing.
4. Confirm application connectivity and basic query behavior against the migrated target before calling the rehearsal complete.
