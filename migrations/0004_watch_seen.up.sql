-- 0004_watch_seen — the watcher's memory of what it has already read.
--
-- TIER 1, deliberately, and it is worth saying why in the file rather than only
-- in a decision document. This table is genuinely derived and genuinely
-- disposable: delete it and the next scan rebuilds it by re-hashing the inbox.
-- It records observed file identity -> content hash and NOTHING ELSE, and it
-- holds no foreign key into tier2.memos — a tier-1 table with a reference into
-- tier 2 would be the cross-schema write path CHRN-71 ruled out.
--
-- Note the direction of the dependency: losing this table costs time, not data.
-- Losing tier 2 costs the corpus. That asymmetry is the whole test for which
-- side a table belongs on, and this one passes it cleanly.
--
-- What it is FOR: CHRN-18 §3 requires that "an arrival row means a delivery, not
-- a scan". Without a durable ledger the watcher re-hashes the whole corpus every
-- poll and re-delivers every file it has ever seen, so re-delivery stops being a
-- no-op and E2's exit criterion is false in the ordinary case.

CREATE TABLE IF NOT EXISTS tier1.watch_seen (
    -- The absolute path as the watcher observed it. Primary key because a path
    -- names at most one file at a time, and the whole question this table
    -- answers is "have I already read the file at this path".
    path          TEXT PRIMARY KEY,

    -- Observed identity. A file is "the same file" when path, size and mtime
    -- all still agree; any of the three moving means re-read it. mtime alone is
    -- too weak (a truncating rewrite can preserve it on some filesystems) and
    -- a hash is too expensive to take on every poll, which is the cost this
    -- table exists to avoid.
    size_bytes    BIGINT      NOT NULL CHECK (size_bytes > 0),
    mtime         TIMESTAMPTZ NOT NULL,

    -- What those bytes hashed to. Kept so an operator can answer "which memo
    -- did this file become" without re-hashing, and so a future consistency
    -- check has something to compare against. Same shape as tier2.memos.
    content_hash  TEXT        NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),

    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- "Which paths delivered these bytes" — the diagnostic direction. Not unique:
-- the same recording can legitimately sit at two paths, and both are sightings
-- of one memo.
CREATE INDEX IF NOT EXISTS watch_seen_content_hash ON tier1.watch_seen (content_hash);

COMMENT ON TABLE tier1.watch_seen IS
  'CHRN-19. What the Copyparty watcher has already read, so a rescan is not a '
  're-delivery. Derived from the inbox and rebuilt by re-hashing it; holds no '
  'reference into tier 2.';

-- No REVOKE here, and that is not an omission. This is a TIER 1 table, so
-- chronicle_tier1 is SUPPOSED to reach it — 0001's ALTER DEFAULT PRIVILEGES on
-- schema tier1 grants it automatically. The explicit REVOKE pattern belongs to
-- tier-2 tables, where it documents intent at the boundary (REVIEW.md 1, and
-- see 0002 for what that statement does and does not do).
