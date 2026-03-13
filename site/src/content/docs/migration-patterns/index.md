---
title: Migration Patterns
description: Choose the right pgferry run style before you start editing flags.
---

Most operators only need a handful of migration patterns. Choose one first, then refine it for your schema.

## Patterns

- [Minimal-safe](/migration-patterns/minimal-safe/) for first production rehearsals and cautious cutovers
- [Recreate-fast](/migration-patterns/recreate-fast/) for disposable environments and quick repeat runs
- [Chunked-resume](/migration-patterns/chunked-resume/) for large tables and interruption-prone jobs
- [Schema-only and data-only](/migration-patterns/schema-only-and-data-only/) for split-phase execution
- [Hooks-driven migrations](/migration-patterns/hooks-driven/) when you need follow-up SQL around the built-in pipeline

## How to decide quickly

| Situation | Start with |
| --- | --- |
| First real migration, production data matters | [Minimal-safe](/migration-patterns/minimal-safe/) |
| Dev or staging database can be dropped and rebuilt | [Recreate-fast](/migration-patterns/recreate-fast/) |
| Load is long enough that restart cost matters | [Chunked-resume](/migration-patterns/chunked-resume/) |
| You need to inspect DDL before loading data | [Schema-only and data-only](/migration-patterns/schema-only-and-data-only/) |
| You must recreate views, routines, or cleanup SQL | [Hooks-driven migrations](/migration-patterns/hooks-driven/) |
