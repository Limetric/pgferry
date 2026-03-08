# Chunked Resume Migration (SQLite)

Splits large tables into range-based chunks with crash-resilient checkpointing.
If the migration is interrupted, rerunning resumes from the last completed
chunk. Post-load row count validation confirms all data was copied.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `chunk_size` | `100000` | Split tables into 100k-row chunks by PK range |
| `resume` | `true` | Skip completed chunks on rerun |
| `validation` | `row_count` | Compare source/target row counts after data load |
| `unlogged_tables` | `true` | Skip WAL during bulk load for speed |

## SQLite-specific notes

SQLite sources always use 1 worker (sequential processing). Chunks run one at a
time, but chunking still provides resume capability and progress tracking.

## When to use

- Large SQLite databases where a single table dominates migration time.
- You want crash-resilient migrations that can resume without recopying data.
- You want verification that all rows were copied.

## Usage

```bash
pgferry examples/sqlite/chunked-resume/migration.toml
```

If interrupted, simply rerun the same command to resume.

Edit the `[source]` DSN and `[target]` DSN to match your environment before running.
