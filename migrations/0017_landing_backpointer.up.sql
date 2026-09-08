-- 0017_landing_backpointer — CHRN-95. What a memo BECAME, when what it became
-- lives in this database.
--
-- Decided before any code, in Mode B: the Switchyard plan on CHRN-95, revision
-- 2, approved 2026-09-08 with all nine rulings picked. Read that for the
-- argument; this file carries what the argument concluded. Section names in
-- these comments — `schema`, `landing`, `pages`, `discussion` — refer to it.
--
-- The picks this file implements, because a reader of this file should not
-- have to fetch the plan to know which branch it is on:
--
--   4 back-pointer   TWO NULLABLE TYPED FOREIGN KEYS — note_id and
--                    discussion_id — with per-destination CHECKs.
--   5 freezing       CH021'S FROZEN SET GROWS BY BOTH, so a confirmed row's
--                    back-pointer cannot be re-pointed. The guard keeps its
--                    deny-list shape; the allow-list rewrite is a follow-up.
--   9 turns          tier2.discussion_turns does NOT gain confirmed_by. A turn
--                    is conversation, not the record; the confirmer is on the
--                    memo_links row this file extends.
--
-- RULING 9 WAS PICKED ON A PREMISE THAT WAS NOT TRUE, and this migration is
-- where that is repaired rather than inherited. The plan said the confirmer is
-- "recorded on tier2.memo_links, so nothing is unattributed" — and 0008 gave
-- that table no actor column at all. Taken literally, the pick would have
-- landed a thread in tier 2 with no record of who approved it, which is the
-- one thing the ticket exists to prevent. Confirmed with magos 2026-09-08:
-- ADD THE COLUMN THE PICK ASSUMED, rather than reopening a settled ruling.
-- Part 3 does that, and CH023 gives the DISCUSSION arm the person-kind
-- enforcement CH041 already gives the NOTE arm.
--
-- AND ONE PICK THAT LEAVES NO TRACE HERE, named so its absence is not read as
-- an omission: ruling 3 puts the claim, the write and the confirm in ONE
-- transaction, so a pending NOTE or DISCUSSION row can no longer exist. That
-- is a property of the landing path, not of the schema — there is no state to
-- add for it, and the CHECKs below are what make its result legible.
--
-- NO REVOKE LINE, AND THE ABSENCE IS DELIBERATE. This migration creates no
-- table. chronicle_tier1 holds no privilege on tier2.memo_links at all — 0007
-- granted it SELECT on tier2.memos and tier2.transcripts BY NAME and
-- deliberately used no ALTER DEFAULT PRIVILEGES — so a column added here is
-- unreadable by that role until somebody grants it in one deliberate act.
-- 0008's own closing comment is the argument for why that must stay true of
-- this table in particular.
--
-- Depends on 0008 (CHRN-33) for tier2.memo_links and its guard, 0011 (CHRN-38)
-- for tier2.notes, and 0015 (CHRN-43) for tier2.discussions.

-- ============================================================================
-- PART 1 · tier2.memo_links — the back-pointer.
-- ============================================================================

-- REAL FOREIGN KEYS, WHICH ticket_key COULD NOT HAVE.
--
-- ticket_key is TEXT because Switchyard is another system and there is nothing
-- in this database to reference. A note and a thread are rows here, so the
-- integrity is available, and declining it would be a choice rather than a
-- constraint of the situation.
--
-- ON DELETE RESTRICT because a note a memo became is not something to remove
-- out from under the record of that decision. tier2.notes is soft-deleted
-- (0014) precisely so this never has to be tested in anger.
ALTER TABLE tier2.memo_links
    ADD COLUMN IF NOT EXISTS note_id       UUID REFERENCES tier2.notes(id)       ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS discussion_id UUID REFERENCES tier2.discussions(id) ON DELETE RESTRICT;

COMMENT ON COLUMN tier2.memo_links.note_id IS
    'The note this memo became. On ticket_key''s pattern: what the decision '
    'PRODUCED, not what it was. Null for every destination but NOTE, and NOT '
    'NULL once a NOTE row is confirmed. Distinct from '
    'tier2.note_revisions.memo_id, which answers which TEXT came from this '
    'memo — 0011 is explicit that neither may be read as the other.';

COMMENT ON COLUMN tier2.memo_links.discussion_id IS
    'The thread this memo opened. note_id''s twin; see that comment.';

-- On memo_links_ticket_key_only_on_ticket's shape and for its stated reason: a
-- ticket key on a NOTE is a row whose destination and whose evidence disagree,
-- and the disagreement would be invisible.
ALTER TABLE tier2.memo_links
    DROP CONSTRAINT IF EXISTS memo_links_note_id_only_on_note,
    ADD CONSTRAINT memo_links_note_id_only_on_note
        CHECK (note_id IS NULL OR destination = 'NOTE');

ALTER TABLE tier2.memo_links
    DROP CONSTRAINT IF EXISTS memo_links_discussion_id_only_on_discussion,
    ADD CONSTRAINT memo_links_discussion_id_only_on_discussion
        CHECK (discussion_id IS NULL OR destination = 'DISCUSSION');

-- memo_links_confirmed_ticket_has_a_key's twin, and its argument transfers
-- word for word: a confirmed row without the thing it produced is a link to
-- nothing, which looks like success and is worse than a failure.
ALTER TABLE tier2.memo_links
    DROP CONSTRAINT IF EXISTS memo_links_confirmed_note_has_a_note,
    ADD CONSTRAINT memo_links_confirmed_note_has_a_note
        CHECK (confirmed_at IS NULL OR destination <> 'NOTE' OR note_id IS NOT NULL);

ALTER TABLE tier2.memo_links
    DROP CONSTRAINT IF EXISTS memo_links_confirmed_discussion_has_a_thread,
    ADD CONSTRAINT memo_links_confirmed_discussion_has_a_thread
        CHECK (confirmed_at IS NULL OR destination <> 'DISCUSSION' OR discussion_id IS NOT NULL);

-- ============================================================================
-- PART 2 · tier2.memo_links — who confirmed it (ruling 9's missing premise).
-- ============================================================================

-- WHAT THE PICK ASSUMED EXISTED.
--
-- Ruling 9 kept confirmed_by off tier2.discussion_turns on the argument that a
-- turn is conversation and the confirmer lives on the link row. The link row
-- had nowhere to put one. Rather than reopen a settled ruling, the column the
-- argument depends on is added here, which is also the reading that makes the
-- ruling's own consequence text true: "a landed note names its confirmer on
-- the revision, a landed thread names it one table over."
--
-- NULLABLE, because a TICKET confirms through Switchyard with no actor
-- recorded and 0008's rows predate any of this. The CHECK below is therefore
-- scoped to the destinations this ticket lands, which are exactly the ones
-- that write into tier 2 without leaving the building.
ALTER TABLE tier2.memo_links
    ADD COLUMN IF NOT EXISTS confirmed_by UUID REFERENCES tier2.users(id) ON DELETE RESTRICT;

COMMENT ON COLUMN tier2.memo_links.confirmed_by IS
    'The person who agreed to this landing. Required once a NOTE or '
    'DISCUSSION row is confirmed, and refused for an agent by CH023 — the '
    'link-row equivalent of CH041 on tier2.note_revisions. Null on a TICKET, '
    'which confirms through Switchyard and records no actor here.';

ALTER TABLE tier2.memo_links
    DROP CONSTRAINT IF EXISTS memo_links_confirmed_local_has_an_actor,
    ADD CONSTRAINT memo_links_confirmed_local_has_an_actor
        CHECK (confirmed_at IS NULL
               OR destination NOT IN ('NOTE', 'DISCUSSION')
               OR confirmed_by IS NOT NULL);

-- ============================================================================
-- PART 3 · memo_links_guard — CH021's frozen set, and CH023 (rulings 5 and 9).
-- ============================================================================

-- THE CHECKS ABOVE CONSTRAIN SHAPE, NOT CHANGE.
--
-- memo_links_note_id_only_on_note is perfectly happy to see a CONFIRMED NOTE
-- row re-pointed at a different note: both the old and the new value are
-- non-null on a NOTE, which is all it asks. Nothing else stopped it either,
-- because this guard is a DENY LIST — CH021 names confirmed_at, ticket_key and
-- destination, and freezes nothing else.
--
-- CH021's own argument is the argument for extending it, with one word
-- changed: "the key an operator was told about could stop being the ticket
-- their memo became, and nothing would have logged the change." Read `note`
-- for `ticket` and it is the same sentence.
--
-- THE SHAPE IS NOT CHANGED, and that is a decision rather than laziness. The
-- allow-list rewrite on notes_guard's to_jsonb(NEW) pattern — where a column
-- added by 0018 would be frozen by DEFAULT rather than by somebody remembering
-- — is the better property and is filed as a follow-up. It is a rewrite of the
-- guard on the table that records what a person decided, and this migration is
-- not the place to fold one in. Ruling 5, and the plan says so in as many
-- words.
--
-- Everything in this function except the CH021 arm is 0008 VERBATIM.
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
    --
    -- note_id, discussion_id and confirmed_by joined this list in 0017
    -- (CHRN-95, ruling 5).
    -- A local landing has no outward call, so its whole record of what the
    -- memo became is these two columns and the row they sit on: re-pointing
    -- one silently is the same failure as re-pointing ticket_key, minus the
    -- remote system that might have contradicted it.
    IF OLD.confirmed_at IS NOT NULL
       AND (NEW.confirmed_at IS DISTINCT FROM OLD.confirmed_at
            OR NEW.ticket_key IS DISTINCT FROM OLD.ticket_key
            OR NEW.note_id IS DISTINCT FROM OLD.note_id
            OR NEW.discussion_id IS DISTINCT FROM OLD.discussion_id
            OR NEW.confirmed_by IS DISTINCT FROM OLD.confirmed_by
            OR NEW.destination IS DISTINCT FROM OLD.destination) THEN
        RAISE EXCEPTION 'a confirmed memo link is immutable (memo %)', OLD.memo_id
            USING ERRCODE = 'CH021';
    END IF;

    -- A PERSON CONFIRMS, ON CH041'S ARGUMENT AND IN ITS ABSENCE.
    --
    -- The NOTE arm cannot land without a person: every revision carries
    -- confirmed_by and CH041 refuses an agent there. The DISCUSSION arm has no
    -- such column to guard — ruling 9 kept a turn as conversation — so without
    -- this the same landing path would be person-checked for one destination
    -- and unchecked for the other, and the difference would be invisible.
    --
    -- Scoped to the local destinations because they are the ones this ticket
    -- lands. A TICKET confirms through Switchyard with no actor recorded
    -- today, and widening that is not this migration's business.
    IF NEW.confirmed_by IS NOT NULL
       AND NEW.confirmed_by IS DISTINCT FROM OLD.confirmed_by
       AND NOT EXISTS (SELECT 1 FROM tier2.users
                        WHERE id = NEW.confirmed_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a memo link is confirmed by a person, not by an agent (memo %)', NEW.memo_id
            USING ERRCODE = 'CH023';
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
