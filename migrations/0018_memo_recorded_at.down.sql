-- Reverses 0018_memo_recorded_at.
--
-- THE GUARD IS RESTORED, NOT DROPPED, and it comes first — 0017's down
-- migration set this pattern and the reason repeats verbatim: a down that
-- removed only the column would leave a CH002 arm reading NEW.recorded_at on
-- a table with no such column, which plpgsql does not notice until the next
-- UPDATE and CI finds a commit later.
--
-- >>> 0003 VERBATIM >>>
CREATE OR REPLACE FUNCTION tier2.memos_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
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
-- <<< 0003 VERBATIM <<<

ALTER TABLE IF EXISTS tier2.memos
    DROP COLUMN IF EXISTS recorded_at;

ALTER TABLE IF EXISTS tier1.memo_uploads
    DROP COLUMN IF EXISTS recorded_at;
