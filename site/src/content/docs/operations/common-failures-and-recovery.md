---
title: Common Failures And Recovery
description: Recovering from the most common pgferry migration failures without making the situation worse.
---

## Common failure modes

| Failure | First response |
| --- | --- |
| unsupported type | stop, fix type mapping, rerun |
| FK creation fails | inspect orphan data or use `before_fk` hooks |
| missing extension | install the extension or change the mapping |
| interrupted long run | rerun with the same config if `resume = true` |
| broken semantic object after migration | recreate it with hooks or separate DDL |

## Recovery rules

- If you plan to resume, do not change the config shape casually between runs.
- If the target is disposable, `recreate-fast` may be simpler than cleaning up partial state.
- If the target is not disposable, prefer durable tables and checkpoints over pure load speed.
- If pgferry reports something instead of migrating it, do not assume it is safe to ignore for production.
