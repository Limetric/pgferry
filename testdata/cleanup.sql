DELETE FROM {{schema}}.comments
WHERE NOT EXISTS (
  SELECT 1 FROM {{schema}}.posts p WHERE p.id = {{schema}}.comments.post_id
);
