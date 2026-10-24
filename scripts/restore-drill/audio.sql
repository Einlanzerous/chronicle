-- CHRN-132: the recordings a database expects, for the storage prediction and
-- for explaining it from live state. Keys and hashes only.
SELECT concat_ws(E'\t', id, author_id, content_hash, (audio_pruned_at IS NOT NULL)::text)
  FROM tier2.memos ORDER BY id;
