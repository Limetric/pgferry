---
title: Handling Unsupported Objects With Hooks
description: Use hook phases to recreate the source objects pgferry intentionally reports instead of migrating automatically.
---

pgferry reports certain objects instead of guessing how to recreate them. Hooks are the normal way to finish that work.

## Objects you should expect to handle yourself

- views
- materialized views
- routines or procedures
- source triggers
- generated-column follow-up DDL
- custom validation SQL

## Which hook phase to use

| Need | Hook phase |
| --- | --- |
| create extensions or helper functions before data load | `before_data` |
| run `ANALYZE` or post-load data cleanup before validation/constraints | `after_data` |
| clean invalid child rows before foreign keys | `before_fk` |
| recreate views, functions, or final validation queries | `after_all` |

## Practical workflow

1. Run `pgferry plan migration.toml --output-dir hooks`.
2. Use the generated hook skeletons as the starting point.
3. Keep application-specific SQL in hook files rather than scattering it through runbooks.
4. Rehearse the exact hook set before the final cutover run.

## Example

```sql
CREATE OR REPLACE VIEW {{schema}}.active_customers AS
SELECT customer_id, email
FROM {{schema}}.customer
WHERE active = true;
```

Use this in an `after_all` hook because the view depends on the finished schema and post-load objects.
