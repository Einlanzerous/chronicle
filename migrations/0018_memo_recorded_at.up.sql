-- 0018_memo_recorded_at — CHRN-118. A display-only, client-asserted
-- recorded_at, riding beside captured_at and never replacing it.
--
-- Decided before any code, in Mode B: the Switchyard plan on CHRN-118,
-- revision 1. Read that for the argument; this file carries what it
-- concluded.
--
-- captured_at is arrival time — when Chronicle first saw the bytes — and it
-- is the only clock CHRN-22's pruner may run from (CHRN-18 §4). recorded_at
-- is when a person says the recording actually happened, asserted by a client
-- that may have been offline for days and whose clock this database has no
-- way to check. Giving it any weight in prunableClause or RetentionStatus
-- would let a wrong clock move a deletion deadline onto today; neither
-- function is touched here.
--
-- TWO TABLES, because the value has to survive the trip from OpenUpload's
-- declaration (which may be minutes or days before finalise) to the memo it
-- eventually becomes.

-- ============================================================================
-- PART 1 · tier1.memo_uploads — carried across the session, like retention.
-- ============================================================================
--
-- Nullable, uncompared on a resume — the same treatment retention and
-- original_filename already get in 0005: "the stored declaration wins,
-- including its retention. A resume is a continuation of one attempt, not a
-- chance to redeclare it." A session that reopens under the same key with a
-- different recorded_at keeps whatever it first declared.
ALTER TABLE tier1.memo_uploads
    ADD COLUMN IF NOT EXISTS recorded_at TIMESTAMPTZ;

COMMENT ON COLUMN tier1.memo_uploads.recorded_at IS
    'Carried through to the memo at finalise. Tier 1, so losing this row '
    'loses nothing durable — the client still holds the recording and its '
    'own idea of when it happened, and re-opens.';

-- ============================================================================
-- PART 2 · tier2.memos — the durable, immutable column.
-- ============================================================================
--
-- NULLABLE, WITH NO DEFAULT. Unlike captured_at, most rows will never carry
-- one: the watcher path has no way to assert it, same as it has no
-- idempotency key, and a memo captured before this ticket has none either.
ALTER TABLE tier2.memos
    ADD COLUMN IF NOT EXISTS recorded_at TIMESTAMPTZ;

COMMENT ON COLUMN tier2.memos.recorded_at IS
    'When a person says this was recorded, asserted by the client and never '
    'verified. Display only — never read by the retention pruner or by any '
    'prunes_at projection, which stay on captured_at. Set once, at the '
    'arrival that creates the row, and immutable after (CH002): the first '
    'writer wins, the same treatment original_filename already gets in '
    'IngestMemo''s upsert, and a re-delivery or a second arrival path never '
    'revises it.';

-- ============================================================================
-- PART 3 · tier2.memos_guard — CH002 grows by one column.
-- ============================================================================
--
-- Worded exactly like the three checks already there. recorded_at is set
-- ONLY in the INSERT branch of IngestMemo's upsert — the ON CONFLICT DO
-- UPDATE touches retention alone — so nothing in Go ever attempts to change
-- it once a row exists. This is defence in depth for the same reason the
-- other three are: the guard exists so a psql session, a migration, or a
-- future worker in another language cannot move it either, not just so Go's
-- own callers cannot.
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
    OR NEW.captured_at  IS DISTINCT FROM OLD.captured_at
    OR NEW.recorded_at  IS DISTINCT FROM OLD.recorded_at THEN
        RAISE EXCEPTION 'memo identity, captured_at and recorded_at are immutable'
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
