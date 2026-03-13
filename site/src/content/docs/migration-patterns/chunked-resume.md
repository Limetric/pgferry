---
title: Chunked-resume
description: Use chunking, checkpoints, and row-count validation for long or interruption-prone migrations.
---

Choose `chunked-resume` when large tables or operational interruption risk make restart cost unacceptable.

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
