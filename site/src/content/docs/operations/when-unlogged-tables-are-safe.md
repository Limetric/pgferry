---
title: When Unlogged Tables Are Safe
description: Use pgferry UNLOGGED tables for disposable dev, staging, and rehearsal loads — never when a crash forcing a restart from zero is unacceptable.
---

`unlogged_tables = true` is a performance choice, not a default safety choice.

## Safe enough cases

- repeatable dev or staging loads
- disposable targets that can be rebuilt from scratch
- rehearsals where speed matters more than crash durability
- one-off experiments where the migration can simply be rerun

## Bad fit

- production rehearsals where you need durable target state
- migrations that rely on `resume = true`
- runs where the target cannot be dropped and rebuilt cheaply

## Rule of thumb

If a PostgreSQL crash during the migration would force you to restart from zero and that would be unacceptable, keep `unlogged_tables = false`.

## Pairings

| Goal | Setting |
| --- | --- |
| fastest disposable load | `unlogged_tables = true` |
| durable long-running migration | `unlogged_tables = false` |
| resumable migration | `unlogged_tables = false` and `resume = true` |
