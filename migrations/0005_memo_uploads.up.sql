-- 0005_memo_uploads — CHRN-20. The bookkeeping behind a resumable upload.
--
-- One row per upload in flight. It records what the client DECLARED it is about
-- to send — the content hash, the byte count, the idempotency key, the
-- capture-time retention choice — so that the bytes arriving over the next
-- minutes can be checked against a promise made before the first one landed.
--
-- TIER 1, and the argument is worth making here rather than only in the
-- decision document, because it is the one call in this ticket that is not
-- obvious.
--
-- The tier test is not "are these bytes precious" — a partial upload is
-- absolutely somebody's recording. It is "is this regenerable from a source of
-- truth that lives outside Chronicle", and here it plainly is: THE PHONE STILL
-- HOLDS THE FILE. A client does not delete its local copy until the server
-- acknowledges a memo, so dropping this table costs the bytes already
-- transferred and nothing else — the client re-opens a session and sends them
-- again. Losing it costs bandwidth; losing tier 2 costs the corpus. That
-- asymmetry is the whole test, and 0004 applied it the same way to
-- tier1.watch_seen, where the cost was a re-hash.
--
-- The moment that stops being true is the moment tier 2 takes over: finalise
-- renames the staging file into the audio store and writes tier2.memos, and
-- from then on this row is deleted and irrelevant. There is deliberately no
-- state in which a row here is the only account of an authored memo.
--
-- What it is NOT: a queue, a job table, or a record of memos. It holds no
-- transcript, no filename-derived path, and nothing anyone would miss.

CREATE TABLE IF NOT EXISTS tier1.memo_uploads (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- The account the upload will be attributed to, resolved from the session
    -- that opened it.
    --
    -- NO FOREIGN KEY into tier2.users, and that is not an oversight: 0004
    -- established that a tier-1 table referencing tier 2 would be the
    -- cross-schema path the doctrine exists to forbid. The consequence is that
    -- deleting an account leaves its in-flight uploads behind rather than
    -- cascading them — which is correct, because tier 2 must not be able to
    -- reach in here either. The sweep collects them; see the expiry index.
    author_id         UUID NOT NULL,

    -- The client's handle for this attempt. Required here, unlike on
    -- tier2.memo_arrivals where the watcher has none to mint: an upload always
    -- has a client, and without a key there is no way to answer "is this a
    -- resume of something I already have" before any bytes arrive. Bounds
    -- mirror memo_arrivals.idempotency_key so a key that opens a session can
    -- always be recorded on the arrival it becomes.
    idempotency_key   TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 200),

    -- What the client says these bytes will hash to, and how many there will
    -- be. DECLARED, never trusted: finalise hashes what actually arrived and
    -- refuses to commit a memo if the two disagree. The declaration earns its
    -- place by making re-delivery free — an author who already holds these
    -- bytes is told so before a single one is transferred.
    content_hash      TEXT NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    byte_size         BIGINT NOT NULL CHECK (byte_size > 0),

    -- The capture-time choice, carried so that DISCARD NOW / 30 DAYS / FOREVER
    -- survives the trip rather than being re-asked on the server. NULL means
    -- the client had no opinion, which is NOT days_30 — CHRN-18's ratchet
    -- treats the two differently and a default must never outrank a choice.
    retention         TEXT CHECK (retention IS NULL OR
                                  retention IN ('discard_now', 'days_30', 'forever')),

    -- Display-only, passed through to the memo. Never used to derive a path:
    -- the audio layout is a pure function of (author_id, content_hash).
    original_filename TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Moved by every accepted chunk. This is what expiry is measured from, so
    -- a slow upload that is still making progress is never swept out from
    -- under itself — the thing being bounded is abandonment, not duration.
    last_activity_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One session per key per author, which is what makes re-POSTing the same key
-- a RESUME rather than a second session. A client that lost its upload id — a
-- process restart, a crashed app — recovers by presenting the key it already
-- has, and this index is the reason that returns the same half-written file
-- instead of starting a parallel one.
--
-- Author-scoped for the same reason memo_arrivals_key is: a global unique on a
-- client-chosen string lets one account's key collide with another's and deny
-- it an upload, through a namespace the two share for no reason.
CREATE UNIQUE INDEX IF NOT EXISTS memo_uploads_key
    ON tier1.memo_uploads (author_id, idempotency_key);

-- The sweep reads this. Abandoned sessions are the only way staging files
-- accumulate, and an index keeps the sweep from being a sequential scan of
-- everything in flight.
CREATE INDEX IF NOT EXISTS memo_uploads_activity
    ON tier1.memo_uploads (last_activity_at);

COMMENT ON TABLE tier1.memo_uploads IS
  'CHRN-20. Uploads in flight: what the client declared it is sending, so the '
  'bytes can be checked against it. Tier 1 — regenerable from the client, '
  'which still holds the recording until the memo is acknowledged. Holds no '
  'reference into tier 2.';

-- No REVOKE, and 0004 explains why: this is a TIER 1 table, so chronicle_tier1
-- is supposed to reach it, and 0001's ALTER DEFAULT PRIVILEGES on schema tier1
-- grants it automatically. The explicit REVOKE pattern belongs to tier-2
-- tables, where it documents intent at the boundary (REVIEW.md 1).
