# Operator Tuning Guide Design

## Summary

Issue `#116` asks for a single operator-facing guide that explains how to tune the migration environment around pgferry, then makes that guide easy to find from the repository root docs surfaces.

The canonical content should live in the Astro docs site under `site/`, with short pointers from the root `README.md` and root `AGENTS.md`. This keeps end-user documentation in one maintained location and avoids drift between the repo and the site.

## Problem

pgferry already exposes the relevant controls for migration throughput and safety, but the operational guidance is scattered:

- some semantics are documented in the site reference pages
- some caveats are only obvious from code or test coverage
- some operator guidance is implied in examples and checklists

That makes it too easy for users to miss high-impact tuning decisions such as PostgreSQL bulk-load settings, `unlogged_tables` versus `resume`, or what `chunk_size` actually means.

## Goals

- Add one operator-facing guide that consolidates safe, actionable tuning advice.
- Use the exact pgferry config terminology already implemented in code and shown in TOML examples.
- Explain tradeoffs, especially where a faster setting also changes durability or consistency behavior.
- Improve discoverability from the root repo docs surfaces and the site operations section.

## Non-Goals

- No new CLI flags, config keys, or runtime behavior changes.
- No benchmark promises or vendor-specific throughput claims without evidence.
- No duplication of the full guide in root repo docs.

## Canonical Content Location

Create a new operations page in the site docs:

- `site/src/content/docs/operations/operator-tuning.md`

This page should be treated as the canonical long-form guide. Root docs should link to it briefly instead of re-explaining the material.

## Intended Audience

The guide is for operators preparing or rehearsing a real migration, not for contributors reading the codebase internals. The tone should be practical and operational: what to change, why it matters, what tradeoff it introduces, and when to leave the default alone.

## Content Design

### 1. Why Migrations Are Usually Environment-Bound

Open with a short framing section explaining that end-to-end migration time is often limited more by PostgreSQL ingest behavior, index build cost, source read pressure, and network distance than by Go-level overhead. The point is not to dismiss pgferry settings, but to place them in the right operational context.

### 2. Target PostgreSQL Tuning

Cover the PostgreSQL-side settings most relevant to pgferry runs:

- session-level `synchronous_commit = off` during bulk load, with a clear explanation of the durability tradeoff
- `maintenance_work_mem` and `max_parallel_maintenance_workers` for post-migrate index creation
- `work_mem` as a situational setting rather than a default recommendation
- WAL and checkpoint pressure at a high level, including why sustained bulk writes can stall on poor defaults

Where the guide discusses PostgreSQL server behavior rather than pgferry behavior, it should link to the official PostgreSQL documentation.

### 3. Source Database Guidance

Cover source-side guidance by source family:

- MySQL and MariaDB:
  - prefer replicas for rehearsals when possible
  - avoid causing primary contention during production-like runs
  - keep pgferry close to the source to reduce round-trip penalties
- MSSQL:
  - explain that `source_snapshot_mode = "single_tx"` depends on snapshot-isolation support at the source
  - point users to the existing MSSQL guide/reference material where appropriate
- SQLite:
  - set expectations clearly that effective copy concurrency stays single-connection
  - explain that environment and storage speed still matter, but worker scaling does not apply the same way

### 4. pgferry Configuration Semantics

Explain the pgferry knobs that operators are most likely to tune:

- `workers`
- `index_workers`
- `chunk_size`
- `unlogged_tables`
- `resume`
- `source_snapshot_mode`
- `validation`

This section must align with current implementation, including these clarifications:

- `chunk_size` is operationally about primary-key range width and resume granularity, not a guaranteed row count per chunk
- `index_workers` helps only in the post-migration index phase
- `source_snapshot_mode = "single_tx"` trades throughput flexibility for a consistent source snapshot during the copy phase
- validation runs after the data load and re-reads the source, so it is not the same snapshot as the earlier copy phase
- `resume = true` requires `unlogged_tables = false`

### 5. Maintenance-Window Example

Add a small example showing the shape of a migration-window tuning approach:

- a PostgreSQL session `SET` snippet for bulk-load-oriented settings
- a matching pgferry TOML snippet that pairs those PostgreSQL choices with realistic pgferry flags

This should be framed as an example to adapt, not a universal preset.

### 6. Pre-Run Checklist

End with a concise checklist that operators can use before a production rehearsal or cutover:

- confirm source and target placement and network path
- decide consistency mode before the run
- choose whether restart safety or absolute speed matters more
- set validation expectations explicitly
- document any temporary PostgreSQL settings that must be reverted after the migration window

## Repository Surface Changes

### Root README

Add a short pointer in `README.md` near the documentation section to direct users to the operator tuning guide on the site. Keep this to a few lines only.

### Root AGENTS

Add a short contributor/operator-facing note in `AGENTS.md` pointing to the same canonical guide. This should reinforce the existing migration-operability guidance already described there, not duplicate the full page.

### Site Navigation

Ensure the new page is discoverable from the existing operations docs. The most natural update is the operations index page, plus any nearby page where a direct cross-link makes sense.

## Files Expected To Change

- Create: `site/src/content/docs/operations/operator-tuning.md`
- Modify: `site/src/content/docs/operations/index.md`
- Modify: `README.md`
- Modify: `AGENTS.md`

Potential additional site edits if needed for discoverability:

- one or more related pages under `site/src/content/docs/operations/`
- one or more related guides/reference pages if a short cross-link materially improves navigation

## Implementation Constraints

- Follow the current site voice and formatting conventions used by the Starlight docs.
- Keep claims aligned with current behavior in:
  - `config.go`
  - `validate.go`
  - existing site reference/configuration pages
- Do not silently change existing wording in unrelated docs unless needed for correctness or cross-linking.
- Prefer linking to official PostgreSQL documentation for PostgreSQL server settings rather than restating low-level database internals in detail.

## Verification

Implementation should be considered complete only if:

- the new guide accurately matches current pgferry config semantics
- root pointers link to the canonical site page
- the site navigation exposes the page clearly enough to find it from the operations section
- the site builds successfully
- any route/link verification used by the repo still passes

Expected verification commands:

- `bun install --frozen-lockfile` in `site/` if dependencies are needed
- `bun run build`
- `bun run check-routes`

No Go behavior changes are expected, so Go tests are not required for this issue unless an implementation detail unexpectedly touches generated content or shared build wiring.

## Risks

- The biggest risk is docs drift: saying something more precise than the code actually guarantees.
- A second risk is overstating PostgreSQL tuning advice in a way that sounds universal rather than situational.
- A third risk is scattering the content again through too many cross-links instead of keeping one canonical page.

## Recommended Implementation Approach

1. Write the canonical site guide first.
2. Add navigation links from the operations index and any clearly-related page.
3. Add short pointers from the root `README.md` and `AGENTS.md`.
4. Build and verify the site.

## Approval Status

This design reflects the approved direction from the user:

- canonical long-form guide in the site
- short pointers from `README.md` and `AGENTS.md`
- no product behavior changes as part of issue `#116`
