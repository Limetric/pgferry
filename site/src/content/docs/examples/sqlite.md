---
title: SQLite Examples
description: SQLite examples focused on simple cutovers and controlled sequencing.
---

SQLite migrations are operationally simpler, but the example set still covers the choices that matter.

## Available examples

- [`minimal-safe`](https://github.com/Limetric/pgferry/tree/main/examples/sqlite/minimal-safe)
- [`recreate-fast`](https://github.com/Limetric/pgferry/tree/main/examples/sqlite/recreate-fast)
- [`hooks`](https://github.com/Limetric/pgferry/tree/main/examples/sqlite/hooks)
- [`schema-only`](https://github.com/Limetric/pgferry/tree/main/examples/sqlite/schema-only)
- [`data-only`](https://github.com/Limetric/pgferry/tree/main/examples/sqlite/data-only)
- [`chunked-resume`](https://github.com/Limetric/pgferry/tree/main/examples/sqlite/chunked-resume)

## SQLite constraints worth remembering

- one worker only
- no `single_tx` snapshot mode
- in-memory SQLite sources are rejected
- source-specific type mapping knobs for MySQL and MSSQL are invalid

## Recommended baseline

Use `minimal-safe` first, then add hooks or schema-only/data-only modes as needed for your cutover procedure.
