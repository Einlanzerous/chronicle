-- 0003_memos — CHRN-18. The memo, and the rule that makes it one row.
--
-- Decided before any code, in Mode B: docs/decisions/chrn-18-memo-model-and-idempotency.md.
-- Read that for the argument; this file carries only what the argument concluded.
--
-- Tier 2, both tables. A memo is what a person said. Nothing regenerates it,
-- and the audio behind it is pruned by policy at 30 days despite not being
-- regenerable — which is why deletion is gated on a durable transcript rather
-- than on the calendar, and why CHRN-22 reads captured_at from here and the
-- transcript from E3.
--
-- Identity is the SHA-256 of the bytes as they arrived, scoped to the author.
-- The client's idempotency key names an ATTEMPT, not a memo: it is the only
-- handle that exists before the last byte lands, and the hash is the only one
-- that catches the same recording arriving by both paths. Both are needed, and
-- they are not the same thing.

CREATE TABLE IF NOT EXISTS tier2.memos (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- RESTRICT, deliberately. Deleting an author would orphan or cascade
    -- authored, irreplaceable rows; CHRN-71's DeleteUser maps the resulting
    -- 23503 to a 409, which is the conversation this should be.
    author_id         UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,

    -- Lowercase hex, over the bytes exactly as they arrived: before
    -- normalisation, before anything. NEVER recomputed, or every prior arrival
    -- stops matching and the same recording becomes a second memo.
    --
    -- This used to name CHRN-21 as the hazard, on the assumption that it would
    -- rewrite the audio. It does not: the decode moved to E3 on 2026-08-27, so
    -- nothing in Chronicle rewrites a recording today. The rule stands for
    -- whatever tries next — and note that a rewrite would break more than this
    -- column, since byte_size is immutable too and the layout gives a memo one
    -- path, so every rewritten memo would read as CHRN-23's `mismatched`.
    content_hash      TEXT NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
    byte_size         BIGINT NOT NULL CHECK (byte_size > 0),

    -- First arrival, assigned here and never by a caller. Immutable once set,
    -- and the only clock CHRN-22 may run from: a prune deadline a client can
    -- move is a prune deadline that can be moved onto today.
    captured_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    state             TEXT NOT NULL DEFAULT 'captured'
                        CHECK (state IN ('captured', 'queued', 'transcribing',
                                         'transcribed', 'triaged', 'held',
                                         'discarded')),
    state_reason      TEXT,                 -- why it is held, why a decode failed

    retention         TEXT NOT NULL DEFAULT 'days_30'
                        CHECK (retention IN ('discard_now', 'days_30', 'forever')),
    audio_pruned_at   TIMESTAMPTZ,          -- set by CHRN-22; the transcript never goes

    -- CHRN-21 fills these; NULL until it has run, which is why 21 needs no
    -- migration of its own. It reads them from the Ogg/OpusHead headers rather
    -- than from a decode — the decode moved to E3 by decision on 2026-08-27 —
    -- so they stay NULL for any file whose headers cannot be read, which is a
    -- memo that is undescribed rather than one that is broken.
    duration_ms       INTEGER CHECK (duration_ms IS NULL OR duration_ms > 0),
    codec             TEXT,
    sample_rate_hz    INTEGER,

    -- Authored, display-only. Never used to derive a path.
    original_filename TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Identity. Author-scoped so that forwarding someone else's recording records
-- a second memo under the second author rather than silently re-attributing
-- the first.
CREATE UNIQUE INDEX IF NOT EXISTS memos_author_content
    ON tier2.memos (author_id, content_hash);

-- There is no audio_path column. CHRN-23 requires the path be derivable from
-- the row alone, and content_hash is immutable, so the path is a pure function
-- of it — storing it too would be a second source of truth for one fact.
-- Pruned is audio_pruned_at IS NOT NULL, never a nulled path.

-- Deliveries. Split out so that "four arrivals, one memo" is a thing the
-- database can state rather than a thing an operator has to infer from logs.
CREATE TABLE IF NOT EXISTS tier2.memo_arrivals (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    memo_id           UUID NOT NULL REFERENCES tier2.memos(id) ON DELETE CASCADE,
    author_id         UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,
    source            TEXT NOT NULL CHECK (source IN ('copyparty', 'upload')),
    idempotency_key   TEXT CHECK (idempotency_key IS NULL
                                  OR length(idempotency_key) BETWEEN 16 AND 200),
    source_ref        TEXT,                 -- watched path, or upload session id
    arrived_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Every arrival carries at least one handle, or a rescan cannot recognise
    -- it as one already recorded.
    CONSTRAINT memo_arrivals_has_handle
        CHECK (idempotency_key IS NOT NULL OR source_ref IS NOT NULL),
    -- The keyless path's handle is its path, and a partial unique index over a
    -- NULL column would not dedupe anything.
    CONSTRAINT memo_arrivals_watched_path
        CHECK (source <> 'copyparty' OR source_ref IS NOT NULL)
);

-- Author-scoped, not global: a global unique on a client-chosen string lets one
-- account's key collide with another's and deny it an upload, through a
-- namespace the two share for no reason.
CREATE UNIQUE INDEX IF NOT EXISTS memo_arrivals_key
    ON tier2.memo_arrivals (author_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- A repeated sighting of the same file is not a second delivery. Without this,
-- the watcher writes one arrival row per scan, forever, for every file it can
-- still see — and the watcher must keep seeing them, because it observes and
-- never consumes.
CREATE UNIQUE INDEX IF NOT EXISTS memo_arrivals_sighting
    ON tier2.memo_arrivals (memo_id, source, source_ref)
    WHERE idempotency_key IS NULL;

CREATE INDEX IF NOT EXISTS memo_arrivals_memo ON tier2.memo_arrivals (memo_id);

-- Retention is ordered, and the order is the point: an arrival may only ever
-- raise it. A phone that re-uploads with the default must not quietly undo a
-- FOREVER pin the person set afterwards in the UI.
CREATE OR REPLACE FUNCTION tier2.retention_rank(r TEXT) RETURNS INT
    LANGUAGE sql IMMUTABLE STRICT AS $fn$
    SELECT CASE r WHEN 'discard_now' THEN 0
                  WHEN 'days_30'     THEN 1
                  WHEN 'forever'     THEN 2 END
$fn$;

-- The state machine lives here rather than in Go because Go is not the only
-- thing that will ever hold a connection to this database. A psql session, a
-- migration, a future worker in another language: the edges hold for all of
-- them, or they are not edges.
CREATE OR REPLACE FUNCTION tier2.memos_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    -- One entry point. A memo exists only once its audio is complete and
    -- durable, so there is no state meaning "maybe there are bytes".
    IF TG_OP = 'INSERT' THEN
        IF NEW.state <> 'captured' THEN
            RAISE EXCEPTION 'memo must be created in state captured, got %', NEW.state
                USING ERRCODE = 'CH003';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.author_id    IS DISTINCT FROM OLD.author_id
    OR NEW.content_hash IS DISTINCT FROM OLD.content_hash
    OR NEW.byte_size    IS DISTINCT FROM OLD.byte_size
    OR NEW.captured_at  IS DISTINCT FROM OLD.captured_at THEN
        RAISE EXCEPTION 'memo identity and captured_at are immutable'
            USING ERRCODE = 'CH002';
    END IF;

    -- 'discarded' appears only as a target and never as a source: that is how
    -- terminal is written down, where it can be read. 'held' keeps an exit to
    -- 'queued' even after its audio prunes, which is why a worker claiming a
    -- memo that already has a durable transcript must skip ASR (E3) rather
    -- than reach for bytes that are gone.
    IF NEW.state IS DISTINCT FROM OLD.state
       AND (OLD.state || '>' || NEW.state) <> ALL (ARRAY[
             'captured>queued',          'captured>held',      'captured>discarded',
             'queued>transcribing',      'queued>held',        'queued>discarded',
             'transcribing>transcribed', 'transcribing>queued',
             'transcribing>held',        'transcribing>discarded',
             'transcribed>triaged',      'transcribed>held',   'transcribed>discarded',
             'triaged>held',             'triaged>discarded',
             'held>queued',              'held>discarded'
       ]) THEN
        RAISE EXCEPTION 'illegal memo state transition % -> %', OLD.state, NEW.state
            USING ERRCODE = 'CH001';
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER memos_guard BEFORE INSERT OR UPDATE ON tier2.memos
    FOR EACH ROW EXECUTE FUNCTION tier2.memos_guard();

-- Redundant when written, and stated anyway as documentation of intent, per
-- the pattern 0002 established.
--
-- WHAT MADE IT REDUNDANT IS NARROWER THAN "0001 REVOKED THE SCHEMA", and 0007
-- is why the difference matters: 0007:52 re-granted USAGE on schema tier2, so
-- the schema is not a wall and has not been one since E4. What holds instead
-- is table privileges — 0007 deliberately added no ALTER DEFAULT PRIVILEGES on
-- schema tier2, so a tier-2 table is unreachable by chronicle_tier1 until some
-- migration grants it BY NAME.
--
-- AND FOR tier2.memos, ONE LATER DID. 0007:53 grants chronicle_tier1 SELECT on
-- tier2.memos (and tier2.transcripts) so Scribe can read its input, and that
-- grant supersedes this line. tier2.memo_arrivals is still ungranted. Do not
-- read this REVOKE as evidence that the role cannot see memos today: it can,
-- by name and on purpose. schema.sql is the current state; this file is when.
--
-- Note also that it does NOT make a later loosened grant appear in schema.sql:
-- pg_dump emits only non-default ACLs, so revoking a privilege the role never
-- held leaves nothing to emit. A loosened GRANT shows up in the diff on its
-- own. The statement is worth keeping; that justification is not.
REVOKE ALL ON tier2.memos, tier2.memo_arrivals FROM chronicle_tier1;
