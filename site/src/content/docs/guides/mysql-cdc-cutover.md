---
title: Low-Downtime MySQL Cutover
description: Use pgferry's CDC mode to migrate from MySQL to PostgreSQL with near-zero downtime.
---

pgferry's CDC mode captures ongoing MySQL changes via binlog replication after the initial bulk load, allowing you to cut over to PostgreSQL with minimal downtime.

## Prerequisites

- MySQL 5.7+ or 8.0+ with `binlog_format = ROW`
- GTID mode recommended (not required)
- MySQL user with `SELECT`, `REPLICATION SLAVE`, and `REPLICATION CLIENT` privileges
- All tables to replicate must have primary keys

## Step 1: Configure

```toml
mode = "cdc"
schema = "myapp"

[source]
type = "mysql"
dsn = "repl_user:password@tcp(mysql-host:3306)/mydb"

[target]
dsn = "postgres://user:password@pg-host:5432/mydb?sslmode=require"
```

Setting `mode = "cdc"` automatically enables `source_snapshot_mode = "single_tx"` for a consistent snapshot.

## Step 2: Initial Load

```bash
pgferry migrate migration.toml
```

This runs the standard migration pipeline and additionally:
- Captures the MySQL binlog position at snapshot time
- Creates a `pgferry_cdc_checkpoint` table in the target schema

## Step 3: Start Replication

```bash
pgferry replicate migration.toml
```

This connects to MySQL as a replication client and applies ongoing changes (INSERT, UPDATE, DELETE) to PostgreSQL. It runs continuously until you stop it with Ctrl+C.

Status is logged every 10 seconds:
```
[replicate] binlog=mysql-bin.000042:83927104 | applied=142,857 | skipped=0 | last_applied=2s ago
```

## Step 4: Cutover

When you're ready to switch:

```bash
# Check current lag
pgferry cutover migration.toml

# Or wait for zero lag
pgferry cutover --wait --timeout 2m migration.toml
```

Once lag reaches zero:
1. Stop writes to MySQL (e.g., put your application in read-only mode)
2. Run `pgferry cutover` one more time to confirm zero lag
3. Point your application to PostgreSQL
4. Stop the `replicate` process (Ctrl+C)

## Limitations

- MySQL source only (MariaDB, SQLite, MSSQL are not supported for CDC)
- Tables without primary keys are skipped
- DDL changes (ALTER TABLE, etc.) during replication are not supported
- Row-based replication only (`binlog_format = ROW`)
