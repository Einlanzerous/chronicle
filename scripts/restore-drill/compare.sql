-- CHRN-132 / CHRN-68: the content comparison. Both manifests are loaded into
-- `res` (the restored database) and `live` (production) in a scratch database
-- that holds nothing else; :dump is the archive's own creation time.
--
--   FAIL  the drill fails on this row
--   LIST  a person reads it; nothing is enforced
--   LOSS  one row per table: the live-only rows after the dump, the measured
--         loss of restoring from this snapshot
--
-- The 60 s tolerance is one transaction's `now()`: a row stamped a minute
-- before the dump may simply not have been committed when pg_dump took its
-- snapshot, so it is listed rather than failed.
SELECT * FROM (
  SELECT 'FAIL' AS k, 'frozen-row-changed-or-missing' AS why, r.t, r.pk
    FROM res r LEFT JOIN live l ON l.t = r.t AND l.pk = r.pk
   WHERE r.fz <> '' AND (l.pk IS NULL OR l.fz IS DISTINCT FROM r.fz)
  UNION ALL
  SELECT 'FAIL', 'mutable-row-diverged-without-a-later-update', r.t, r.pk
    FROM res r JOIN live l ON l.t = r.t AND l.pk = r.pk
   WHERE r.cls = 'M' AND r.md <> '' AND l.md IS DISTINCT FROM r.md
     AND NOT (l.upd > :'dump'::timestamptz)
  UNION ALL
  SELECT 'LIST', 'differs-and-has-no-updated_at', r.t, r.pk
    FROM res r JOIN live l ON l.t = r.t AND l.pk = r.pk
   WHERE r.cls = 'N' AND l.md IS DISTINCT FROM r.md
  UNION ALL
  SELECT 'LIST', 'restored-only-not-frozen', r.t, r.pk
    FROM res r LEFT JOIN live l ON l.t = r.t AND l.pk = r.pk
   WHERE l.pk IS NULL AND r.fz = ''
  UNION ALL
  SELECT 'FAIL', 'live-only-row-older-than-the-dump', l.t, l.pk
    FROM live l LEFT JOIN res r ON r.t = l.t AND r.pk = l.pk
   WHERE r.pk IS NULL AND l.cre < :'dump'::timestamptz - interval '60 seconds'
  UNION ALL
  SELECT 'LIST', 'live-only-row-within-60s-of-the-dump', l.t, l.pk
    FROM live l LEFT JOIN res r ON r.t = l.t AND r.pk = l.pk
   WHERE r.pk IS NULL
     AND l.cre >= :'dump'::timestamptz - interval '60 seconds'
     AND l.cre <= :'dump'::timestamptz
  UNION ALL
  SELECT 'LOSS', 'live-only-rows-after-the-dump', l.t, count(*)::text
    FROM live l LEFT JOIN res r ON r.t = l.t AND r.pk = l.pk
   WHERE r.pk IS NULL AND l.cre > :'dump'::timestamptz
   GROUP BY l.t
) q ORDER BY k, t, why, pk;
