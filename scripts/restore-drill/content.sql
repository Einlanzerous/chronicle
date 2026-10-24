-- CHRN-132 / CHRN-68: the CONTENT manifest, one row per tier-2 row, tab-
-- separated, digests only. No column of authored text leaves the container:
-- every field after the key is an MD5 or a timestamp.
--
--   t    table            pk   primary key, tabs and newlines flattened
--   fz   digest of the columns a trigger FREEZES against UPDATE ('' if none)
--   cls  what the rest of the row is: M = mutable with updated_at,
--        N = mutable with none, '' = nothing beyond fz
--   md   digest of the mutable remainder ('' if cls is '')
--   upd  updated_at (M only)
--   cre  the row's own creation timestamp, for the live-only rule
--
-- The classes are the plan's table (CHRN-68 rev 2, "Content manifest"). The
-- session timezone is pinned to UTC by the caller so a digest of a timestamp
-- is the same on both servers.
SELECT concat_ws(E'\t', t, pk, fz, cls, md, upd, cre) FROM (
  SELECT 'memos' AS t, id::text AS pk,
         md5(concat_ws('|', id, author_id, content_hash, byte_size, captured_at, recorded_at)) AS fz,
         'M' AS cls, md5((to_jsonb(x) - 'updated_at')::text) AS md,
         to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS upd,
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"') AS cre
    FROM tier2.memos x
  UNION ALL
  SELECT 'transcripts', id::text,
         CASE WHEN NOT partial THEN md5(concat_ws('|', memo_id, model, text)) ELSE '' END,
         'M',
         CASE WHEN partial THEN md5((to_jsonb(x) - 'updated_at')::text) ELSE '' END,
         to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.transcripts x
  UNION ALL
  SELECT 'note_revisions', id::text, md5(to_jsonb(x)::text), '', '', '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.note_revisions x
  UNION ALL
  SELECT 'discussion_turns', id::text, md5(to_jsonb(x)::text), '', '', '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.discussion_turns x
  UNION ALL
  SELECT 'notes', id::text, '', 'M', md5((to_jsonb(x) - 'updated_at')::text),
         to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.notes x
  UNION ALL
  SELECT 'pages', id::text, '', 'M', md5((to_jsonb(x) - 'updated_at')::text),
         to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.pages x
  UNION ALL
  SELECT 'memo_links', id::text, '', 'M', md5((to_jsonb(x) - 'updated_at')::text),
         to_char(updated_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.memo_links x
  UNION ALL
  SELECT 'users', id::text, '', 'N', md5(to_jsonb(x)::text), '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.users x
  UNION ALL
  SELECT 'memo_arrivals', id::text, '', 'N', md5(to_jsonb(x)::text), '',
         to_char(arrived_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.memo_arrivals x
  UNION ALL
  SELECT 'discussions', id::text, '', 'N', md5(to_jsonb(x)::text), '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.discussions x
  UNION ALL
  SELECT 'note_deletions', id::text, '', 'N', md5(to_jsonb(x)::text), '',
         to_char(deleted_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.note_deletions x
  UNION ALL
  SELECT 'note_tags', note_id::text || ':' || translate(tag, E'\t\n', '  '), '', 'N',
         md5(to_jsonb(x)::text), '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.note_tags x
  UNION ALL
  SELECT 'page_redirects', translate(from_path, E'\t\n', '  '), '', 'N',
         md5(to_jsonb(x)::text), '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.page_redirects x
  UNION ALL
  SELECT 'user_tokens', id::text, '', 'N',
         md5((to_jsonb(x) - 'last_used_at' - 'used_at')::text), '',
         to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.user_tokens x
  UNION ALL
  SELECT 'discussion_participants', discussion_id::text || ':' || user_id::text, '', 'N',
         md5((to_jsonb(x) - 'last_read_seq' - 'last_read_at')::text), '',
         to_char(added_at, 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')
    FROM tier2.discussion_participants x
) q
ORDER BY t, pk;
