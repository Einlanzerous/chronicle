-- Reverses 0017_landing_backpointer.
--
-- THE GUARD IS RESTORED, NOT DROPPED, and it comes first. 0017 replaced a
-- function 0008 wrote; a down that removed only the columns would leave a
-- CH021 arm behind that tests NEW.note_id on a table with no such column —
-- and a CH023 arm reading NEW.confirmed_by on a table with no such column —
-- and 0017 also WIDENED the trigger to BEFORE INSERT OR UPDATE, so the
-- declaration is restored here beside the body —
-- which plpgsql does not notice until the next UPDATE, and which
-- up-then-down-then-up would not render byte-identically into schema.sql
-- either. CI finds that a commit later, on somebody else's branch. CHRN-43 and
-- CHRN-45 both learned this the expensive way; 0016's down says the same
-- thing.
--
-- Everything between the markers below is 0008_memo_links.up.sql VERBATIM,
-- copied mechanically rather than retyped.

-- >>> 0008 VERBATIM >>>
CREATE OR REPLACE FUNCTION tier2.memo_links_guard() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
    -- On tier1.memo_proposals' pattern and tier2.transcripts' before it: the
    -- identity is what makes the row mean anything, and a re-attributed
    -- decision is a decision credited to a memo nobody made it about.
    IF NEW.memo_id IS DISTINCT FROM OLD.memo_id THEN
        RAISE EXCEPTION 'a memo link may not be re-attributed to another memo'
            USING ERRCODE = 'CH020';
    END IF;

    -- CONFIRMED IS TERMINAL, and this is load-bearing rather than tidy.
    --
    -- The accept path answers `applied` with the stored key for a memo that is
    -- already triaged, and it does that WITHOUT an outward call. That answer is
    -- only honest while a confirmation cannot be withdrawn — if a later sweep
    -- or a later batch could un-confirm a row, the key an operator was told
    -- about could stop being the ticket their memo became, and nothing would
    -- have logged the change.
    IF OLD.confirmed_at IS NOT NULL
       AND (NEW.confirmed_at IS DISTINCT FROM OLD.confirmed_at
            OR NEW.ticket_key IS DISTINCT FROM OLD.ticket_key
            OR NEW.destination IS DISTINCT FROM OLD.destination) THEN
        RAISE EXCEPTION 'a confirmed memo link is immutable (memo %)', OLD.memo_id
            USING ERRCODE = 'CH021';
    END IF;

    -- RE-ARMING A REFUSED ROW MUST BE A WHOLE NEW DECISION.
    --
    -- A refusal is an outcome, and the row keeps it so the operator can be told
    -- why. Clearing `refused_at` is how a corrected decision reclaims the row —
    -- but a corrected decision that re-sent the SAME idempotency key would
    -- replay the cached refusal it was correcting, which is the twenty-four
    -- hour trap `sent_idempotency_key` exists to close. Re-arming without a
    -- fresh key is therefore refused here, where it cannot be forgotten,
    -- rather than left to the one code path that does it today.
    IF OLD.refused_at IS NOT NULL AND NEW.refused_at IS NULL
       AND NEW.sent_idempotency_key = OLD.sent_idempotency_key THEN
        RAISE EXCEPTION 're-arming a refused memo link needs a fresh idempotency key (memo %)', OLD.memo_id
            USING ERRCODE = 'CH022';
    END IF;

    NEW.updated_at := now();
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER memo_links_guard
    BEFORE UPDATE ON tier2.memo_links
    FOR EACH ROW EXECUTE FUNCTION tier2.memo_links_guard();
-- <<< 0008 VERBATIM <<<

ALTER TABLE IF EXISTS tier2.memo_links
    DROP CONSTRAINT IF EXISTS memo_links_confirmed_local_has_an_actor,
    DROP CONSTRAINT IF EXISTS memo_links_confirmed_discussion_has_a_thread,
    DROP CONSTRAINT IF EXISTS memo_links_confirmed_note_has_a_note,
    DROP CONSTRAINT IF EXISTS memo_links_discussion_id_only_on_discussion,
    DROP CONSTRAINT IF EXISTS memo_links_note_id_only_on_note;

ALTER TABLE IF EXISTS tier2.memo_links
    DROP COLUMN IF EXISTS confirmed_by,
    DROP COLUMN IF EXISTS discussion_id,
    DROP COLUMN IF EXISTS note_id;
