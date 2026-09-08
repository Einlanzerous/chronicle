-- 0016_read_markers — CHRN-45. Unread state per participant, per thread.
--
-- Decided before any code, in Mode B: the Switchyard plan on CHRN-45,
-- revision 2, approved 2026-09-08 with all eight rulings picked. Read that for
-- the argument; this file carries what the argument concluded. Section names
-- in these comments — `tier`, `schema`, `advance`, `counting`,
-- `store-surface`, `tradeoffs` — refer to it.
--
-- The eight picks, because a reader of this file should not have to fetch the
-- plan to know which branch it is on:
--
--   1 tier          TIER 2. Not regenerable, and its foreign keys are illegal
--                   anywhere else.
--   2 where         A COLUMN ON tier2.discussion_participants, not a table.
--   3 posting       POSTING IS READING — AppendTurn advances the author's own
--                   marker in the same transaction.
--   4 agents        AN AGENT HAS NO MARKER, refused by CH101.
--   5 monotonic     GREATEST IN THE STORE *AND* A GUARD. A stale report is a
--                   silent no-op; a direct rewind is refused.
--   6 aggregate     SHIPS HERE — UnreadByDiscussion.
--   7 removed       A REMOVED PERSON WHO POSTS STAYS REMOVED; the marker
--                   advances, the membership does not change.
--   8 non-member    MarkRead BY A NON-PARTICIPANT IS REFUSED, not an implicit
--                   join.
--
-- Depends on 0015 (CHRN-43) for tier2.discussion_participants and
-- tier2.discussion_turns, and on 0002 (CHRN-71) for tier2.users.kind.

-- ============================================================================
-- PART 1 · the marker.
-- ============================================================================
--
-- TIER 2, and the intuitive answer is the wrong one. "Unread is interface
-- state, so tier 1" fails both of CLAUDE.md's tests.
--
-- REGENERABILITY IS THE STATED TEST: "Tier 1 is disposable BECAUSE IT IS
-- REGENERABLE: delete it and it rebuilds." tier1.watch_seen passes it —
-- 0004:4-5 says the next scan rebuilds it by re-hashing the inbox. Nothing in
-- this corpus records that a person read up to turn 7, so nothing rebuilds
-- this; delete it and every thread is unread forever.
--
-- AND THE MECHANICAL ARGUMENT IS 0004's OWN. A read marker inherently
-- references a user and a discussion, both tier 2, and 0004:6-8 rules that out
-- in as many words: "a tier-1 table with a reference into tier 2 would be the
-- cross-schema write path CHRN-71 ruled out." The tier-1 version of this table
-- would have to drop its foreign keys and hold bare UUIDs — a marker that can
-- outlive the account and the thread it names, with nothing to refuse it.

ALTER TABLE tier2.discussion_participants
    -- NULL MEANS NEVER READ, AND IT IS NOT 0. They say different things — 0 is
    -- a marker somebody set, NULL is a marker nobody has — and only one of them
    -- survives a later question like "has this person opened this thread at
    -- all". The count treats them identically via COALESCE, which is what makes
    -- keeping the distinction free.
    ADD COLUMN IF NOT EXISTS last_read_seq INTEGER
        CHECK (last_read_seq IS NULL OR last_read_seq >= 0),

    -- NOT DECORATION. "When did I last look at this" is the ordinary
    -- thread-list sort, and it is unrecoverable if not written at the moment it
    -- is true.
    ADD COLUMN IF NOT EXISTS last_read_at TIMESTAMPTZ;

-- Set together or not at all — 0014:36-37's wording for the deletion pair, and
-- the same reason: one question, one answer, and no half-state for a reader to
-- interpret.
ALTER TABLE tier2.discussion_participants
    DROP CONSTRAINT IF EXISTS discussion_participants_read_pair;
ALTER TABLE tier2.discussion_participants
    ADD CONSTRAINT discussion_participants_read_pair
    CHECK ((last_read_seq IS NULL) = (last_read_at IS NULL));

COMMENT ON COLUMN tier2.discussion_participants.last_read_seq IS
  'CHRN-45. How far this participant has read, as a tier2.discussion_turns.seq. '
  'NULL means never read, which is NOT the same as 0. Monotonic and bounded: '
  'it never decreases and is never cleared once set (CH100), and never exceeds '
  'the thread''s last turn (CH102). Always NULL for an agent (CH101).';

COMMENT ON COLUMN tier2.discussion_participants.last_read_at IS
  'CHRN-45. When this participant last LOOKED — the thread-list sort, and the '
  'plan''s wording. Not "when the marker moved": a stale mark-read is a no-op '
  'for last_read_seq (ruling 5) and still bumps this, because a phone '
  'reconnecting and reporting an old position did look at the thread. Paired '
  'with last_read_seq by discussion_participants_read_pair.';

-- ============================================================================
-- PART 2 · the guard.
-- ============================================================================
--
-- REPLACED WHOLE rather than patched, because 0015 wrote it and this migration
-- widens its allow list and adds three clauses. 0016_read_markers.down.sql
-- restores 0015's body BYTE-FOR-BYTE — a down that only drops the columns
-- would leave the widened allow list behind and make up-then-down-then-up
-- produce a different schema.sql. That is CHRN-43's lesson, one migration on.
--
-- CH100 KEEPS ITS MEANING and gains two names; the new rules get their own
-- codes on 0014:191-193's reasoning for opening CH060 — they enforce different
-- things, and a reader tracing one should not find three unrelated refusals
-- under a single number.
--
--   CH100  the allow list, and the actor tests            (0015, widened here)
--   CH101  an agent carries no marker                     (ruling 4)
--   CH102  a marker may not run past the thread           (ruling 5)
--
-- WHY CH101 EXISTS AT ALL, given the store also refuses it: ruling 4 was
-- STATED in Go in revision 1 of the plan and that was not enough. 0011:187's
-- rule is "ENFORCED HERE AND NOT IN GO", and CHRN-67's MCP tools are a second
-- caller by design — a promise a handler keeps is one they do not inherit.
-- The review of CHRN-43's PR found exactly this shape open on the resolve
-- path, which is recent enough to be worth naming here.
--
-- WHY CH102 EXISTS, and it is the subtler one. Ruling 5 forbids a DECREASE.
-- Nothing bounded an increase, so a marker set past the end of a thread —
-- MarkRead(999) on a five-turn thread, which one off-by-one against a stale
-- turn list produces — would sit there forever and report 0 unread until the
-- thread had a thousand turns. That is this ticket's own stated failure ("the
-- web says none") with a different cause, and unlike the one it worries about
-- it is silent and never self-corrects.
CREATE OR REPLACE FUNCTION tier2.discussion_participants_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    changed TEXT;
    head    INTEGER;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        SELECT string_agg(n.key, ', ' ORDER BY n.key) INTO changed
          FROM jsonb_each(to_jsonb(NEW)) n
          JOIN jsonb_each(to_jsonb(OLD)) o ON o.key = n.key
         WHERE n.value IS DISTINCT FROM o.value
           AND n.key <> ALL (ARRAY['removed_at', 'removed_by',
                                   'last_read_seq', 'last_read_at']);
        IF changed IS NOT NULL THEN
            RAISE EXCEPTION
                'tier2.discussion_participants permits updating removed_at, removed_by, last_read_seq, last_read_at only; added_at and added_by mean FIRST added; refused: %',
                changed
                USING ERRCODE = 'CH100',
                      CONSTRAINT = 'discussion_participants_update_allow_list';
        END IF;

        -- MONOTONIC (ruling 5). The store never sends a decrease — it wraps the
        -- value in GREATEST — so this is unreachable through the package, the
        -- same shape CH031 has on tier2.notes and stated for the same reason.
        --
        -- CLEARING IT IS A DECREASE, AND THE LARGEST ONE. A version of this
        -- clause that tested only NEW < OLD accepted last_read_seq = NULL on a
        -- row sitting at 3, which puts the whole thread back to unread — the
        -- one decrease the rule was supposed to forbid and the only one nobody
        -- would notice, because the row that results is indistinguishable from
        -- a participant who has never read anything.
        IF OLD.last_read_seq IS NOT NULL
        AND (NEW.last_read_seq IS NULL OR NEW.last_read_seq < OLD.last_read_seq) THEN
            RAISE EXCEPTION
                'a read marker only moves forward: % is behind %',
                COALESCE(NEW.last_read_seq::text, 'never read'), OLD.last_read_seq
                USING ERRCODE = 'CH100',
                      CONSTRAINT = 'discussion_participants_marker_forward';
        END IF;
    END IF;

    -- A PERSON ADDS AND A PERSON REMOVES. Membership decides who is expected
    -- to read a thread, and an agent quietly removing a person from a
    -- conversation is the shape CH041 exists to refuse one table over. Both
    -- ends, because an INSERT-only test would leave the removal half reading
    -- as enforced while doing nothing — 0014:378-384's failure, named there.
    --
    -- CONSTRAINT named for CH080's reason, in 0015.
    IF NOT EXISTS (SELECT 1 FROM tier2.users
                    WHERE id = NEW.added_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a participant is added by a person, not by an agent'
            USING ERRCODE = 'CH100',
                  CONSTRAINT = 'discussion_participants_actor_is_a_person';
    END IF;
    IF NEW.removed_by IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM tier2.users
                     WHERE id = NEW.removed_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a participant is removed by a person, not by an agent'
            USING ERRCODE = 'CH100',
                  CONSTRAINT = 'discussion_participants_actor_is_a_person';
    END IF;

    -- CH101 — AN AGENT HAS NO UNREAD (ruling 4). Unread answers "what should I
    -- look at", and an agent is never asked that: CHRN-47 keeps its trigger
    -- "explicit rather than ambient" precisely because "an agent that replies
    -- to everything turns discussions into noise", and an agent that scanned
    -- unread threads and replied to them is that failure wearing a hat.
    IF NEW.last_read_seq IS NOT NULL
    AND EXISTS (SELECT 1 FROM tier2.users
                 WHERE id = NEW.user_id AND kind = 'agent') THEN
        RAISE EXCEPTION 'an agent carries no read marker: unread is a person''s question'
            USING ERRCODE = 'CH101',
                  CONSTRAINT = 'discussion_participants_agent_has_no_marker';
    END IF;

    -- CH102 — A MARKER MAY NOT RUN PAST THE THREAD (ruling 5, the upper bound).
    -- The store clamps with LEAST, so this too is unreachable through the
    -- package and exists for the direct writer.
    IF NEW.last_read_seq IS NOT NULL THEN
        SELECT COALESCE(MAX(seq), 0) INTO head
          FROM tier2.discussion_turns WHERE discussion_id = NEW.discussion_id;
        IF NEW.last_read_seq > head THEN
            RAISE EXCEPTION
                'a read marker cannot run past the thread: % with % turns',
                NEW.last_read_seq, head
                USING ERRCODE = 'CH102',
                      CONSTRAINT = 'discussion_participants_marker_bounded';
        END IF;
    END IF;

    RETURN NEW;
END
$fn$;

-- The trigger itself is 0015's and is not recreated: CREATE OR REPLACE
-- FUNCTION swaps the body under it, which is what 0014 does to notes_guard.

COMMENT ON TABLE tier2.discussion_participants IS
  'CHRN-43 / CHRN-44 / CHRN-45. Who is expected to READ a thread, and how far '
  'they have got. Current state rather than a journal: a removed participant''s '
  'turns still render, so nothing is lost by not keeping the add/remove '
  'history. added_at and added_by are frozen and mean FIRST added; '
  'last_read_seq is the read marker, monotonic and bounded by the thread.';

-- ============================================================================
-- THE TIER BOUNDARY.
-- ============================================================================
--
-- NOTHING TO REVOKE, and that is a statement rather than an omission. This
-- migration creates no table and no sequence: it adds two columns to
-- tier2.discussion_participants, which 0015 already revoked from
-- chronicle_tier1 by name. A column inherits its table's privileges, so the
-- marker is unreachable from a tier-1 path for the reason the table is.
--
-- 0007:53 remains the only tier-2 table grant in any migration.
