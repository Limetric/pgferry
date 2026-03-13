---
title: Choose Your Path
description: Decide when to use the safe path, the fast path, resumable runs, hooks, or split-phase migrations.
sidebar:
  order: 3
---

After the first wizard-driven run, the next question is not "what flag exists?" It is "which migration path matches the risk profile of this run?"

## Start with one of these

<div class="route-list">
	<a href="/migration-patterns/minimal-safe/">Minimal-safe</a>
	<a href="/migration-patterns/recreate-fast/">Recreate-fast</a>
	<a href="/migration-patterns/chunked-resume/">Chunked-resume</a>
	<a href="/migration-patterns/schema-only-and-data-only/">Schema-only / data-only</a>
	<a href="/migration-patterns/hooks-driven/">Hooks-driven</a>
</div>

## Quick decisions

| Situation | Start here |
| --- | --- |
| First production rehearsal | [Minimal-safe](/migration-patterns/minimal-safe/) |
| Dev or staging target is disposable | [Recreate-fast](/migration-patterns/recreate-fast/) |
| Restarting the run would be expensive | [Chunked-resume](/migration-patterns/chunked-resume/) |
| You need schema review before data load | [Schema-only / data-only](/migration-patterns/schema-only-and-data-only/) |
| You need views, routines, or custom follow-up SQL | [Hooks-driven](/migration-patterns/hooks-driven/) |

## Key operator choices

### When to use `single_tx`

Use `source_snapshot_mode = "single_tx"` when the source stays live and you need one consistent view across tables.

Read more:

- [How to choose snapshot mode](/operations/how-to-choose-snapshot-mode/)
- [MySQL source guide](/source-guides/mysql/)
- [MSSQL source guide](/source-guides/mssql/)

### When to use `resume`

Use `resume = true` when interruption cost matters more than absolute load speed. Pair it with `unlogged_tables = false`.

Read more:

- [When resume is worth it](/operations/when-resume-is-worth-it/)
- [When unlogged tables are safe](/operations/when-unlogged-tables-are-safe/)

### When to use hooks

Use hooks when `plan` reports work that pgferry intentionally does not recreate automatically, such as views, routines, or custom cleanup SQL.

Read more:

- [Handling unsupported objects with hooks](/operations/handling-unsupported-objects-with-hooks/)
- [Hooks reference](/reference/hooks/)

## Example-driven starting points

- [MySQL examples](/examples/mysql/)
- [SQLite examples](/examples/sqlite/)
- [MSSQL examples](/examples/mssql/)

## Next step

Run [Plan and Validate](/get-started/plan-and-validate/) once you know which path you are taking.
