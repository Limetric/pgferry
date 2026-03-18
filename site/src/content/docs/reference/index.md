---
title: Reference
description: Complete end-user reference for configuration, type mapping, hooks, pipeline stages, and operational limits.
---

Use the reference section when you already understand the basic flow and need exact behavior, defaults, or source-specific constraints.

## Read in this order

1. [Configuration](/reference/configuration/) for every TOML option, defaults, and incompatible combinations.
2. [Type Mapping](/reference/type-mapping/) for source-to-PostgreSQL conversions and per-source knobs.
3. [Migration Pipeline](/reference/migration-pipeline/) for stage order, mode differences, resume, and validation.
4. [Hooks](/reference/hooks/) for the four SQL phases and `{{schema}}` templating.
5. [Conventions & limitations](/reference/conventions-and-limitations/) for naming, generated columns, unsupported objects, and source caveats.

## Fast answers

| Question | Start here |
| --- | --- |
| Which flags matter for a first production run? | [Configuration](/reference/configuration/#recommended-starting-points) |
| Which options only apply to MySQL or MSSQL? | [Configuration](/reference/configuration/#source-specific-constraints) and [Type Mapping](/reference/type-mapping/) |
| What happens in `schema_only` or `data_only` mode? | [Migration Pipeline](/reference/migration-pipeline/#modes) |
| When do hook files run? | [Hooks](/reference/hooks/#phases) |
| What does pgferry report but not migrate automatically? | [Conventions & limitations](/reference/conventions-and-limitations/#unsupported-objects-and-features) |
