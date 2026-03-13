---
title: Examples
description: Copy-pasteable example migrations grouped by source database and pattern.
---

Use the examples section when you want a working starting point instead of an abstract reference page.

## Pick by situation

- First production-style rehearsal:
  [MySQL minimal-safe](/examples/mysql/minimal-safe/),
  [SQLite minimal-safe](/examples/sqlite/minimal-safe/),
  [MSSQL minimal-safe](/examples/mssql/minimal-safe/)
- Fast disposable reloads:
  [MySQL recreate-fast](/examples/mysql/recreate-fast/),
  [SQLite recreate-fast](/examples/sqlite/recreate-fast/),
  [MSSQL recreate-fast](/examples/mssql/recreate-fast/)
- Large or interruption-prone runs:
  [MySQL chunked-resume](/examples/mysql/chunked-resume/),
  [SQLite chunked-resume](/examples/sqlite/chunked-resume/)
- Controlled sequencing:
  [schema-only and data-only patterns](/migration-patterns/schema-only-and-data-only/)
- Hook-driven follow-up SQL:
  [MySQL hooks](/examples/mysql/hooks/),
  [SQLite hooks](/examples/sqlite/hooks/)

## Browse by source

- [MySQL examples](/examples/mysql/)
- [SQLite examples](/examples/sqlite/)
- [MSSQL examples](/examples/mssql/)

## What each example page includes

- when to use it
- tradeoffs
- a copy-pasteable `migration.toml`
- SQL hook snippets where relevant
- the exact command to run
- a link back to the raw files in GitHub

## Before you run an example

1. Replace the source and target DSNs with your real endpoints.
2. Run `pgferry plan migration.toml` first.
3. Read the matching [source guide](/source-guides/) for source-specific constraints.
4. Decide whether you want the safe path, the fast disposable path, or the resumable path before you start editing flags.
