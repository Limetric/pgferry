---
title: Chunked-resume
description: The pgferry pattern for large or interruption-prone migrations — chunking, resumable checkpoints, and row-count validation cut restart cost.
---

Go with `chunked-resume` when big tables or the risk of interruption make starting over too costly to stomach.

## What defines this pattern

- `resume = true`
- `unlogged_tables = false`
- `chunk_size = 100000` or tuned to your data shape
- `validation = "row_count"`

## Best fit

- very large tables
- unstable networks or long-running maintenance windows
- rehearsals where recovery procedure matters as much as raw throughput

## Start from these examples

- [MySQL chunked-resume](/examples/mysql/chunked-resume/)
- [SQLite chunked-resume](/examples/sqlite/chunked-resume/)
