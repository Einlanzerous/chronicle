-- 0011_notes — CHRN-38. The note, its permanent number, and where its text
-- lives.
--
-- Decided before any code, in Mode B: the Switchyard plan on CHRN-38,
-- revision 2, approved 2026-09-05 with all four rulings picked. Read that for
-- the argument; this file carries what the argument concluded. Section names
-- in these comments — `schema`, `guards`, `store-surface` — refer to it.
--
-- Depends on 0010 (CHRN-37) for tier2.pages.
--
-- ============================================================================
-- THERE IS NO AUTHORED TEXT ON tier2.notes, AND ITS ABSENCE IS THE DECISION.
-- ============================================================================
--
-- Not the body and NOT THE TITLE. Both live on tier2.note_revisions, so a
-- rename is a revision exactly as an edit is.
--
-- CHRN-39's `Done when` is "no code path in the service issues an UPDATE that
-- loses text". If the text lives on the note row, that is a promise Go has to
-- keep on every write path forever, including the ones E10 hands to an agent
-- at three in the morning. If it lives only in revisions, THERE IS NO TEXT
-- COLUMN TO OVERWRITE and the promise is a property of the schema.
--
-- Revision 1 of the plan left `title` here and was wrong in a way worth
-- recording: a rename was an UPDATE that lost the old title, sitting inside a
-- document whose whole argument was that no such UPDATE exists. A title is
-- text a person wrote and a title is worth restoring.
--
-- ============================================================================
-- PROVENANCE IS PER REVISION, AND IT IS THE TEXT'S ORIGIN.
-- ============================================================================
--
-- An idea arrives in more than one pass: a vague memo in March, then a
-- concrete one after a trade show in June. Those are TWO MEMOS feeding TWO
-- REVISIONS of one note. A single notes.memo_id would record only the first,
-- and the second memo's contribution becomes untraceable at exactly the moment
-- somebody asks where a paragraph came from.
--
-- This is NOT the decision to file a memo as a note. That is a
-- tier2.memo_links row, one per memo by UNIQUE (memo_id), written by CHRN-33.
-- Two tables mention the same memo and they answer different questions:
-- the link says a person chose NOTE, this column says which text that
-- produced. Neither is derivable from the other and neither may be read as the
-- other.

CREATE SEQUENCE IF NOT EXISTS tier2.note_number_seq AS BIGINT START WITH 1;

-- notes FIRST, because tier2.note_revisions references it. The composite
-- foreign key back the other way is added by ALTER once both exist.
CREATE TABLE IF NOT EXISTS tier2.notes (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- THE PERMANENT HANDLE, rendered CHR-0311. Sequence-allocated, never
    -- reused, never changed, and still resolving after CHRN-39 soft-deletes
    -- the note.
    --
    -- GAPS ARE CORRECT. A rolled-back insert burns a number, and a burned
    -- number is strictly better than a reused one: reuse makes CHR-0311
    -- resolve to two different notes across time, which is the exact failure
    -- the word "forever" in this ticket rules out. max(number)+1 would reuse;
    -- a sequence will not.
    --
    -- The INTEGER is the fact. CHR-0311 is a rendering of it, four digits wide
    -- as a MINIMUM and not as a cap, so the corpus does not stop at 9999.
    number   BIGINT NOT NULL UNIQUE
                 DEFAULT nextval('tier2.note_number_seq'),

    -- ADDRESS, NOT IDENTITY. CHRN-37's page tree moves; this column changes
    -- and id and number do not, which is what "permanent IDs that survive
    -- moves" means in practice. RESTRICT so a page cannot be deleted out from
    -- under the authored text on it.
    page_id  UUID NOT NULL REFERENCES tier2.pages(id) ON DELETE RESTRICT,

    -- NOT NULL, which is only possible because the revision is inserted FIRST
    -- (see `store-surface`). NOT NULL is not deferrable in Postgres, so the
    -- other insert order would have forced this column nullable forever and
    -- made every reader handle a state that cannot survive a commit.
    current_revision_id UUID NOT NULL,

    author_id  UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tier2.note_revisions (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- DEFERRABLE because this row is written BEFORE the note it belongs to.
    -- RESTRICT for the reason tier2.transcripts uses it against memos:
    -- authored rows are not swept away as a side effect of another row going
    -- missing.
    note_id  UUID NOT NULL REFERENCES tier2.notes(id) ON DELETE RESTRICT
                 DEFERRABLE INITIALLY DEFERRED,

    -- Monotone per note, allocated under the note's row lock. See
    -- `store-surface`: without that lock two concurrent appends read the same
    -- max(seq), one violates the unique constraint below, and which of them
    -- ends up current is whichever committed second.
    seq      INTEGER NOT NULL CHECK (seq > 0),

    -- BOTH HALVES OF THE AUTHORED TEXT, versioned together, because a title
    -- and a body are edited in the same breath and restored in the same
    -- breath.
    title    TEXT NOT NULL,

    -- NOT NULL AND '' IS A VALID VALUE — the ruling tier2.transcripts.text
    -- carries, for the same reason. An emptied note is authored and complete,
    -- and `if body == "" { return }` is the innocent-looking line that turns
    -- deliberate blanking into a silent no-op.
    body     TEXT NOT NULL,

    -- NULL means authored directly in the web client. Equally durable —
    -- provenance is metadata, not a tier.
    memo_id  UUID REFERENCES tier2.memos(id) ON DELETE RESTRICT,

    author_id  UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (note_id, seq),

    -- Redundant for uniqueness (id is already the primary key) and REQUIRED as
    -- the target of the composite foreign key below: Postgres will only
    -- reference a unique constraint over exactly those columns.
    UNIQUE (id, note_id)
);

-- COMPOSITE, AND THAT IS THE POINT. A plain foreign key on
-- current_revision_id proves the revision row exists and says NOTHING about
-- whose it is — a note could point at another note's text and the database
-- would not object. Carrying notes.id into the match makes a cross-note
-- pointer unrepresentable rather than merely untested.
ALTER TABLE tier2.notes
    ADD CONSTRAINT notes_current_revision
    FOREIGN KEY (current_revision_id, id)
    REFERENCES tier2.note_revisions (id, note_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX IF NOT EXISTS notes_page ON tier2.notes (page_id);
CREATE INDEX IF NOT EXISTS note_revisions_note
    ON tier2.note_revisions (note_id, seq DESC);

-- UNIQUE, not merely indexed. tier2.memo_links is UNIQUE (memo_id) — a memo
-- lands exactly once — and this says the same about the text that landing
-- produced. A plain index here would silently permit one memo to author
-- several revisions, which is a different decision from the one CHRN-33 made
-- and would be made by omission.
CREATE UNIQUE INDEX IF NOT EXISTS note_revisions_memo
    ON tier2.note_revisions (memo_id) WHERE memo_id IS NOT NULL;

-- ============================================================================
-- THE GUARDS.
-- ============================================================================
--
-- Error codes continue the one-block-per-table shape: CH001-CH005 memos,
-- CH010-CH011 proposals, CH020-CH022 memo_links, CH050-CH051 pages.
--
-- WHAT THE COMBINATION MEANS, stated rather than discovered: note_id's
-- ON DELETE RESTRICT plus a guard that refuses DELETE on revisions means NO
-- NOTE AND NO REVISION CAN BE HARD-DELETED BY THE APPLICATION ROLE once this
-- migration lands. That is the intent — this is the irreplaceable store, and
-- CHRN-39 is about to make deletion soft and recoverable. Written here it is a
-- decision; found by the first operator who tries it, it is a bug report.
CREATE OR REPLACE FUNCTION tier2.notes_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Identity is immutable. page_id and current_revision_id deliberately
        -- are NOT: changing the first is a move, changing the second is an
        -- edit, and those are the two things a note is supposed to survive.
        IF NEW.id        IS DISTINCT FROM OLD.id
        OR NEW.number    IS DISTINCT FROM OLD.number
        OR NEW.author_id IS DISTINCT FROM OLD.author_id THEN
            RAISE EXCEPTION 'note identity is immutable (id, number, author)'
                USING ERRCODE = 'CH030';
        END IF;

        -- As memos_guard, transcripts_guard and memo_links_guard all do.
        -- Without it, updated_at is a column nothing maintains.
        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER notes_guard BEFORE INSERT OR UPDATE ON tier2.notes
    FOR EACH ROW EXECUTE FUNCTION tier2.notes_guard();

-- APPEND-ONLY, ENFORCED HERE AND NOT IN GO. Armed by this migration rather
-- than by CHRN-39 (ruling 3, picked "arm it here") because CHRN-40, CHRN-41
-- and CHRN-42 all land in the window between the two tickets, and without it
-- that window is one in which a revision row can be quietly rewritten.
--
-- CHRN-39 only ever ADDS to this: restore, soft delete and the verb set. It
-- relaxes nothing. Both of its available restore strategies still work —
-- moving notes.current_revision_id backwards, or appending a new revision
-- carrying the old body and title — because neither updates a revision row.
CREATE OR REPLACE FUNCTION tier2.note_revisions_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    RAISE EXCEPTION 'a note revision is append-only: % is refused', TG_OP
        USING ERRCODE = 'CH040';
END
$fn$;

CREATE OR REPLACE TRIGGER note_revisions_guard
    BEFORE UPDATE OR DELETE ON tier2.note_revisions
    FOR EACH ROW EXECUTE FUNCTION tier2.note_revisions_guard();

-- Redundant, and stated anyway as documentation of intent at the point the
-- tier boundary is defined. 0003_memos.up.sql is the wording.
--
-- WHAT MAKES IT REDUNDANT IS NARROWER THAN "0001 REVOKED THE SCHEMA", and the
-- difference matters because 0007:52 re-granted USAGE on schema tier2. The
-- fact that survives 0007: no GRANT on tier2.notes or tier2.note_revisions was
-- ever issued, and 0007 deliberately added no ALTER DEFAULT PRIVILEGES on
-- schema tier2 — so a tier-2 table created today is unreachable by
-- chronicle_tier1 on table privileges alone, whatever it holds on the schema.
--
-- NOTE FOR CHRN-41: its index reads these tables as chronicle_tier1 and will
-- need GRANT SELECT. That grant is CHRN-41's to raise and CHRN-52's to issue —
-- see CHRN-32 §1.1, which named CHRN-41 as the ticket that hits the question
-- hardest. Nothing here grants anything.
REVOKE ALL ON tier2.notes, tier2.note_revisions FROM chronicle_tier1;
REVOKE ALL ON SEQUENCE tier2.note_number_seq FROM chronicle_tier1;
