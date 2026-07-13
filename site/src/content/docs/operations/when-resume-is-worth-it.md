---
title: When Resume Is Worth It
description: Decide when pgferry resume earns its cost — long runs and large chunkable tables where restart pain outweighs raw bulk-load speed.
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

If you use table or column filters, treat `table_filter_mode`, `include_tables`, `exclude_tables`, `column_filter_mode`, and `exclude_columns` as part of the checkpoint contract. Changing them between runs can change the selected migration scope, and pgferry will reject the old checkpoint when the resolved migration scope no longer matches.

## Tables without a primary key

A table with no single-column integer primary key is copied in one pass rather than in chunks, and that `COPY` commits before the checkpoint can record it. If the machine dies in that window, the rows are in the target but the checkpoint does not know — and with no primary key, nothing would reject the duplicates a second copy would create.

pgferry writes a durable marker before starting such a copy. A resume that finds the marker refuses to continue, because it cannot tell whether zero rows or every row was committed. Delete the checkpoint and rerun, or empty the target table first.

A clean interrupt (`Ctrl+C`, or a failed run) clears the marker, since an unfinished `COPY` is rolled back. Only a hard crash leaves it set.

## Resume assumes a stable source

Chunks are planned over the primary key's `MIN`..`MAX` range, and progress is recorded per chunk. If rows are inserted or deleted between the interrupted run and the resumed one, that range moves and the recorded chunks no longer describe the same rows.

pgferry records the key range it planned over and refuses to resume when it has changed, rather than silently skipping or duplicating rows. If that happens, delete the checkpoint and rerun, or resume against a source that is not being written to.

For a source that is still taking writes, use `source_snapshot_mode = "single_tx"` for a consistent snapshot within a run.

## Practical tradeoff

You give up the fastest `UNLOGGED` full-load path, but you gain:

- chunk-level restartability
- lower cost after interruptions
- a safer operational story for large runs

If you are deciding between `recreate-fast` and `chunked-resume`, ask one question first: is the real risk load time, or is the real risk having to start over?
