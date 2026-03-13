---
title: When Resume Is Worth It
description: Use resume when restart cost matters more than absolute bulk-load speed.
---

`resume = true` is valuable when redoing work is expensive. It is not the default fastest path.

## Use resume when

- the migration is long enough that a restart would be painful
- large chunkable tables dominate runtime
- network or maintenance-window reliability is uncertain
- you need a documented recovery story for operations

## Do not reach for resume automatically when

- the target schema is disposable and cheap to recreate
- the migration is short enough that a full rerun is acceptable
- you are optimizing for the fastest possible dev or staging loop

## Required pairing

```toml
resume = true
unlogged_tables = false
```

Why: checkpoints only make sense when the target data survives a crash.

## Practical tradeoff

You give up the fastest `UNLOGGED` full-load path, but you gain:

- chunk-level restartability
- lower cost after interruptions
- a safer operational story for large runs

If you are deciding between `recreate-fast` and `chunked-resume`, ask one question first: is the real risk load time, or is the real risk having to start over?
