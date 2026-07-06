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
| broken semantic object after migration | recreate it with hooks or separate DDL |

## Recovery rules

- If you plan to resume, do not change the config shape casually between runs.
- If the target is disposable, `recreate-fast` may be simpler than cleaning up partial state.
- If the target is not disposable, prefer durable tables and checkpoints over pure load speed.
- If pgferry reports something instead of migrating it, do not assume it is safe to ignore for production.

## `data_only` trigger-control failures

- `data_only` preflights `ALTER TABLE ... DISABLE TRIGGER ALL` in rollback-only transactions before COPY starts. If that check fails, no data was copied and the probe transaction was rolled back.
- If a later `data_only` failure says pgferry attempted to restore trigger state, inspect the named target table and verify triggers are enabled before retrying.
- On restricted or managed PostgreSQL roles, prefer a rehearsed `schema_only` plus alternate load workflow if the role cannot control triggers on the existing tables.
