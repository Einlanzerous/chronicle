-- 0006_transcripts — CHRN-27. The transcript, and the bookkeeping behind
-- getting one.
--
-- Two tables on OPPOSITE SIDES OF THE TIER LINE, which is the whole reason
-- this migration is worth reading rather than skimming.
--
-- The transcript is TIER 2. It is what survives the audio being pruned, what
-- search will index, and what Scribe reads. Once CHRN-22 has deleted a memo's
-- audio at thirty days, the transcript is the only remaining account of what
-- was said — and nothing regenerates it, because the recording it came from is
-- gone. That is the definition of tier 2 exactly.
--
-- The job correlation is TIER 1. It records which ASR job a memo was submitted
-- to and under which idempotency key. Lose it and Chronicle re-submits: a
-- second GPU run of audio it still holds, which costs a few seconds and
-- nothing else. Losing it costs compute; losing the transcript costs the
-- corpus. That asymmetry is the test, and 0004 and 0005 applied it the same
-- way to tier1.watch_seen and tier1.memo_uploads.
--
-- The predicate this table exists to make answerable is CHRN-25 §5, and it is
-- the one CHRN-22's Mode C review turns on:
--
--     A transcript is durable iff its job reached `succeeded`
--     and `partial` is false.
--
-- Stated in the SERVICE's vocabulary there, and unanswerable in that
-- vocabulary here: the ASR job rows live in another database, their payloads
-- purge at seven days, and CHRN-22 fires at thirty. So the fact is PROJECTED
-- into this table when the result is collected, and the pruner reads Chronicle
-- and nothing else.

CREATE TABLE IF NOT EXISTS tier2.transcripts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- RESTRICT, deliberately, and not CASCADE. A transcript OUTLIVES its
    -- audio by design, so after a prune it is the only account of what
    -- somebody said. Cascading it away behind a memo delete would destroy
    -- authored content as a side effect of a row going missing; RESTRICT turns
    -- that into a conversation instead. tier2.memos uses RESTRICT against
    -- users for the same reason.
    memo_id           UUID NOT NULL REFERENCES tier2.memos(id) ON DELETE RESTRICT,

    -- NOT NULL and '' IS A VALID VALUE, and this is the column the whole of
    -- CHRN-25 §5 argues about.
    --
    -- A memo that is forty seconds of silence, or of traffic noise, has a true
    -- and complete answer and the answer is "no speech". Treating that as
    -- not-transcribed means every such memo keeps its audio forever, and the
    -- corpus accumulates exactly the recordings least worth keeping while the
    -- UI's PRUNES label quietly becomes a lie for them.
    --
    -- Nullable would be worse than wrong: the first thing anyone writes
    -- against a nullable transcript is a skip, and `if text == "" { return }`
    -- is the innocent-looking line that inverts the ruling in the direction
    -- nobody checks.
    text              TEXT NOT NULL,

    -- {start_ms, end_ms, text}[]. JSONB rather than a second table because
    -- nothing queries INSIDE a segment list: it is written once and read
    -- whole. A shredded table would be a second representation to keep in step
    -- with the contract's schema.
    segments          JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- THE GATE. A fact the ASR service recorded about whether its own run
    -- completed, carried across the boundary unchanged.
    --
    -- It is never computed here, and never from covered_ms against
    -- audio_duration_ms: whisper emits segments only where there is speech, so
    -- an ordinary memo with trailing silence has covered_ms short of its
    -- duration on a perfectly complete run. A pruner gated on that inference
    -- would mark most of the corpus not-durable and never fire — a safer
    -- failure than the other one, but still silent, and still makes the
    -- retention promise false in the direction nobody checks.
    partial           BOOLEAN NOT NULL,

    -- What produced it. Stored because a corpus transcribed by two different
    -- models over time is one whose quality varies invisibly, and because
    -- `medium.en` is a live upgrade path the benchmark deliberately left open.
    model             TEXT NOT NULL,
    backend           TEXT NOT NULL,

    -- Evidence, both of them, and NEITHER is a predicate. covered_ms is short
    -- of the duration on any recording that ends in silence; it is here
    -- because it is genuinely useful when a transcript looks wrong.
    audio_duration_ms BIGINT,
    covered_ms        BIGINT,

    transcribed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One transcript per memo per model. Not per memo: `medium.en` over a memo
-- already transcribed by `small.en` is a better answer to the same question and
-- both are worth keeping, which is also what makes the model column meaningful.
--
-- Collecting the same result twice is then a no-op rather than a duplicate
-- row, which matters because the thing that stops a double-collection is a
-- TIER 1 row, and tier 1 is allowed to be lost.
CREATE UNIQUE INDEX IF NOT EXISTS transcripts_memo_model
    ON tier2.transcripts (memo_id, model);

-- What CHRN-22 reads, and the only query it may use:
--
--   EXISTS (SELECT 1 FROM tier2.transcripts WHERE memo_id = $1 AND NOT partial)
--
-- Partial, so it is an index-only answer for the pruner's hot path and so that
-- a partial transcript cannot be reached through it by accident.
CREATE INDEX IF NOT EXISTS transcripts_durable
    ON tier2.transcripts (memo_id)
    WHERE NOT partial;

-- A complete run may REPLACE a partial one. A partial may never replace a
-- complete one, and nothing may replace a transcript with a different memo's.
--
-- This is the only UPDATE path onto authored content in this table, and it is
-- one-way by construction rather than by convention: the trigger refuses the
-- reverse direction outright, so a retry policy (CHRN-28) cannot downgrade a
-- good transcript by re-collecting a bad one.
CREATE OR REPLACE FUNCTION tier2.transcripts_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP = 'INSERT' THEN
        RETURN NEW;
    END IF;

    IF NEW.memo_id IS DISTINCT FROM OLD.memo_id
    OR NEW.model   IS DISTINCT FROM OLD.model THEN
        RAISE EXCEPTION 'a transcript may not be re-attributed to another memo or model'
            USING ERRCODE = 'CH004';
    END IF;

    IF OLD.partial = false AND NEW.partial = true THEN
        RAISE EXCEPTION 'a complete transcript may not be replaced by a partial one'
            USING ERRCODE = 'CH005';
    END IF;

    IF OLD.partial = false AND NEW.text IS DISTINCT FROM OLD.text THEN
        RAISE EXCEPTION 'a complete transcript is authored content and is not rewritten in place'
            USING ERRCODE = 'CH005';
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER transcripts_guard BEFORE INSERT OR UPDATE ON tier2.transcripts
    FOR EACH ROW EXECUTE FUNCTION tier2.transcripts_guard();

COMMENT ON TABLE tier2.transcripts IS
  'CHRN-27. What a memo said. Tier 2 — it outlives the audio CHRN-22 prunes '
  'at thirty days, and nothing regenerates it once that audio is gone. '
  '`partial` is the gate that pruner reads: durable iff a run completed and '
  'partial is false. Empty text with a completed run IS durable.';


-- ---------------------------------------------------------------------------
-- Tier 1: which ASR job a memo was submitted to.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tier1.memo_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NO FOREIGN KEY into tier2.memos, and that is not an oversight: 0004
    -- established that a tier-1 table referencing tier 2 would be the
    -- cross-schema path the doctrine exists to forbid, and 0005 repeated it.
    -- The consequence is that this row can outlive its memo; the sweep in
    -- internal/transcribe is what collects those.
    memo_id           UUID NOT NULL,

    -- WRITTEN BEFORE THE SUBMIT IS SENT. That ordering is the whole point of
    -- the row, and CHRN-25 §3 is explicit about why: the key must be stable
    -- across HTTP retries of one attempt, or the failure it prevents is not
    -- prevented. Chronicle submits, the process dies before it records the
    -- job id, it comes back and retries — without a persisted key that is a
    -- second job, the GPU transcribes the memo twice, and Chronicle has two
    -- results for one memo and no way to say which is the transcript.
    --
    -- Fresh per ATTEMPT, not per memo: a deliberate re-transcription is a
    -- different request and gets a different key.
    idempotency_key   TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 16 AND 200),

    -- What was asked for. Compared against the memo on a retry so that a key
    -- is never reused for a different request, which the service answers 409.
    model             TEXT NOT NULL,
    audio_sha256      TEXT NOT NULL CHECK (audio_sha256 ~ '^[0-9a-f]{64}$'),

    -- NULL until the submit returns. A row in this state is an attempt that
    -- may or may not have reached the service — which is exactly the situation
    -- the idempotency key exists to make safe to resolve, by re-submitting
    -- with the same key and reading the reply.
    job_id            UUID,
    submitted_at      TIMESTAMPTZ,

    -- Set when the result has been written to tier2.transcripts. This is what
    -- stops a second collection, and it is deliberately on the TIER 1 side:
    -- the tier-2 unique index is what makes losing this row harmless rather
    -- than duplicating a transcript.
    collected_at      TIMESTAMPTZ,

    -- Why the attempt ended badly, carried onto the memo's state_reason so a
    -- human can see it without a database session.
    failure_code      TEXT,
    failure_message   TEXT,

    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The key is Chronicle's handle on one attempt, so it is unique here for the
-- same reason it is unique in the service: two rows sharing a key would mean
-- two attempts that cannot be told apart.
CREATE UNIQUE INDEX IF NOT EXISTS memo_jobs_key
    ON tier1.memo_jobs (idempotency_key);

-- At most ONE attempt in flight per memo. Without this a slow poll and a fast
-- tick submit the same memo twice, which is a second GPU run and two results
-- for one memo -- the exact failure the key prevents ACROSS a retry and this
-- prevents WITHIN one process.
CREATE UNIQUE INDEX IF NOT EXISTS memo_jobs_in_flight
    ON tier1.memo_jobs (memo_id)
    WHERE collected_at IS NULL AND failure_code IS NULL;

CREATE INDEX IF NOT EXISTS memo_jobs_memo ON tier1.memo_jobs (memo_id);

COMMENT ON TABLE tier1.memo_jobs IS
  'CHRN-27. Which ASR job a memo was submitted to, under which idempotency '
  'key. Tier 1 — regenerable, because Chronicle still holds the audio: losing '
  'a row costs one repeated GPU run and nothing else. Holds no transcript and '
  'no reference into tier 2.';

-- Tier 2 is revoked from the regeneration role explicitly, as 0002 and 0003
-- did: redundant against 0001, and stated anyway as documentation of intent at
-- the boundary. tier1.memo_jobs gets no REVOKE for the mirror-image reason —
-- it is a tier-1 table and chronicle_tier1 is supposed to reach it.
REVOKE ALL ON tier2.transcripts FROM chronicle_tier1;
