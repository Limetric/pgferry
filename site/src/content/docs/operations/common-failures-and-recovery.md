---
title: Common Failures And Recovery
description: Recover from common pgferry failures — unsupported types, FK errors, interrupted runs, and data_only trigger-control preflight aborts.
---

## Common failure modes

| Failure | First response |
| --- | --- |
| unsupported type | stop, fix type mapping, rerun |
| `data_only` trigger-control preflight fails | stop before cutover, grant the required trigger-control privilege or switch away from `data_only` |
| `data_only` reports no sequence found for an auto-increment column | give the column a `DEFAULT nextval(...)` on the target schema's sequence or define it as identity, then rerun |
| FK creation fails | inspect orphan data or use `before_fk` hooks |
| missing extension | install the extension or change the mapping |
| interrupted long run | rerun with the same config if `resume = true` |
| dropped connection mid-copy | pgferry retries the affected chunk automatically; if it still fails, rerun with `resume = true` |
| broken semantic object after migration | recreate it with hooks or separate DDL |

## Interrupting a run

Press `Ctrl+C` (or send `SIGTERM`) to stop a migration cleanly. pgferry cancels its workers, saves the checkpoint, and exits. With `resume = true`, the next run picks up from the completed chunks instead of redoing them.

Press `Ctrl+C` a second time to abort immediately. That skips the checkpoint save, so progress since the last flush is lost and the resumed run repeats it.

## Transient connection failures

Connection-level failures during the data copy — a dropped socket, a managed-database failover, a target briefly refusing connections — are retried automatically with exponential backoff before the run is failed. Each chunk is copied in its own transaction and rolls back cleanly on error, so a retried chunk cannot duplicate rows.

Retries apply to the parallel copy path. `source_snapshot_mode = "single_tx"` holds one long-lived source transaction, and reconnecting would silently replace its snapshot, so a lost connection there fails the run instead — rerun with `resume = true`.

Errors that will not fix themselves (a missing relation, a type or constraint violation) fail immediately rather than burning retries.

## Recovery rules

- If you plan to resume, do not change the config shape casually between runs.
- If the target is disposable, `recreate-fast` may be simpler than cleaning up partial state.
- If the target is not disposable, prefer durable tables and checkpoints over pure load speed.
- If pgferry reports something instead of migrating it, do not assume it is safe to ignore for production.

## `data_only` trigger-control failures

- `data_only` preflights `ALTER TABLE ... DISABLE TRIGGER ALL` in rollback-only transactions before COPY starts. If that check fails, no data was copied and the probe transaction was rolled back.
- If a later `data_only` failure says pgferry attempted to restore trigger state, inspect the named target table and verify triggers are enabled before retrying.
- On restricted or managed PostgreSQL roles, prefer a rehearsed `schema_only` plus alternate load workflow if the role cannot control triggers on the existing tables.
