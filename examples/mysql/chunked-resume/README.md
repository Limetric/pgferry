# Chunked Resume Migration

Splits large tables into range-based chunks for faster parallel loading with
crash-resilient checkpointing. If the migration is interrupted, rerunning
resumes from the last completed chunk. Post-load row count validation confirms
all data was copied.

## Key settings

| Setting | Value | Why |
|---|---|---|
| `chunk_size` | `100000` | Split tables into 100k-row chunks by PK range |
| `resume` | `true` | Skip completed chunks on rerun |
| `validation` | `row_count` | Compare source/target row counts after data load |
| `unlogged_tables` | `false` | Resume requires logged tables so checkpoints match durable target data |
| `workers` | `8` | Parallel chunk/table workers |

## When to use

- Large tables with millions of rows that dominate migration runtime.
- You want crash-resilient migrations that can resume without recopying data.
- You want verification that all rows were copied.
- You are willing to trade some bulk-load speed for resumability.

## How chunking works

Tables with a single-column numeric primary key are split into range-based
chunks using `WHERE pk >= lower AND pk < upper`. Each chunk runs as an
independent `COPY` operation. Tables without a chunkable PK (composite keys,
non-numeric keys, no PK) fall back to full-table copy.

## Checkpoint file

Progress is saved to `pgferry_checkpoint.json` in the same directory as the
TOML config file. The checkpoint is atomically updated in batches during the
run and deleted on successful completion.

## Usage

```bash
pgferry examples/mysql/chunked-resume/migration.toml
```

If interrupted, simply rerun the same command to resume.

Edit the `[source]` and `[target]` DSNs to match your environment before running.
