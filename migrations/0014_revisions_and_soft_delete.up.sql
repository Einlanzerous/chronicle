-- 0014_revisions_and_soft_delete — CHRN-39. Restore, soft delete, the verb
-- set, and the rule that nothing lands in authored text unattended.
--
-- Decided before any code, in Mode C: the Switchyard plan on CHRN-39,
-- revision 3, approved 2026-09-06 with all five rulings picked. Read that for
-- the argument; this file carries what the argument concluded. Section names
-- in these comments — `restore`, `softdelete`, `verbs`, `safety`, `schema` —
-- refer to it.
--
-- The five picks, because a reader of this file should not have to fetch the
-- plan to know which branch it is on:
--
--   1 restore        RESTORE BY APPENDING. current_revision_id only moves
--                    forward, and CH032 makes rewind unrepresentable.
--   2 soft delete    COLUMNS ON tier2.notes, not a tombstone revision.
--   3 verbs          FOUR PEER VERBS — create / append / supersede / relate.
--   4 enforcement    IN THE TRIGGER, ON EVERY INSERT — seq 1 included.
--   5 journaling     tier2.note_deletions.
--
-- ADDITIVE, EXCEPT ONE INDEX SWAP. 0011 promised CHRN-39 would only ever add
-- to its guards and never relax them, and that promise is kept: CH030 and
-- CH040 raise on exactly what they raised on before. notes_page is replaced by
-- a partial index, which is the one exception and is called out where it
-- happens.
--
-- Depends on 0011 (CHRN-38) for tier2.notes and tier2.note_revisions, 0013
-- (CHRN-42) for tier2.note_tags, and 0002 (CHRN-71) for tier2.users.kind.

-- ============================================================================
-- PART 1 · tier2.notes — soft delete.
-- ============================================================================
--
-- TIER 2, obviously: this is the irreplaceable store. What matters is that
-- deletion here is a STATE, not an event. The event is PART 3.
--
-- Null together or set together, so "deleted" is one question with one answer
-- and there is no row that is half-deleted for a reader to interpret.

ALTER TABLE tier2.notes
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS deleted_by UUID REFERENCES tier2.users(id) ON DELETE RESTRICT;

ALTER TABLE tier2.notes
    DROP CONSTRAINT IF EXISTS notes_deleted_pair;
ALTER TABLE tier2.notes
    ADD CONSTRAINT notes_deleted_pair
    CHECK ((deleted_at IS NULL) = (deleted_by IS NULL));

-- THE INDEX SWAP, and the one place this migration is not purely additive.
--
-- Every live read filters deleted_at IS NULL, so the partial index is the one
-- the queries actually want and keeping both would be one index too many.
--
-- WHAT IT NO LONGER COVERS, stated rather than discovered: the ON DELETE
-- RESTRICT check from tier2.pages, and any future listing of deleted notes,
-- both fall back to a scan. The corpus is one operator's notes — the same
-- reasoning CHRN-41 used to choose Postgres FTS over a second system — so this
-- is accepted. A trash view that wants an index adds its own.
DROP INDEX IF EXISTS tier2.notes_page;
CREATE INDEX IF NOT EXISTS notes_page_live
    ON tier2.notes (page_id) WHERE deleted_at IS NULL;

-- ============================================================================
-- PART 2 · tier2.note_revisions — provenance, and where a restore came from.
-- ============================================================================

ALTER TABLE tier2.note_revisions
    ADD COLUMN IF NOT EXISTS confirmed_by  UUID REFERENCES tier2.users(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS verb          TEXT,
    ADD COLUMN IF NOT EXISTS restored_from UUID;

-- NULLABLE, AND IT HAS TO BE. Ruling 4 requires a confirming person on every
-- insert, which reads like NOT NULL and cannot be: adding a NOT NULL column
-- with no default to a populated table needs a backfill UPDATE, and CH040
-- refuses every UPDATE on this table. There is also no honest default — no
-- revision written before this migration was confirmed by anybody, because the
-- concept did not exist.
--
-- So the COLUMN is nullable and the TRIGGER is what requires it. A null
-- confirmed_by means "landed before 0014" and nothing else; from here on the
-- guard refuses the insert.

-- WHAT verb MEANS: what the person confirmed about a proposal, recorded on the
-- revision as provenance. NULL means AUTHORED DIRECTLY — someone typed it —
-- mirroring memo_id, which is null for the same reason. Three row classes are
-- legitimately null: a direct edit, a restore (restored_from says so), and
-- every row that predates this migration.
--
-- TEXT + CHECK rather than a Postgres ENUM. Adding a value to a CHECK is a
-- one-line migration; adding one to an enum type is not, and E10 is likely to
-- want one.
ALTER TABLE tier2.note_revisions
    DROP CONSTRAINT IF EXISTS note_revisions_verb;
ALTER TABLE tier2.note_revisions
    ADD CONSTRAINT note_revisions_verb
    CHECK (verb IS NULL OR verb IN ('create', 'append', 'supersede', 'relate'));

-- THE VERB AND THE SEQ AGREE, or the row is refused. create and relate both
-- produce a NEW note, so they are seq 1; append and supersede both act on text
-- that already exists, so they cannot be. Stated as a constraint because the
-- alternative is a row that claims to have appended to nothing, and nothing
-- downstream would notice.
ALTER TABLE tier2.note_revisions
    DROP CONSTRAINT IF EXISTS note_revisions_verb_seq;
ALTER TABLE tier2.note_revisions
    ADD CONSTRAINT note_revisions_verb_seq
    CHECK (verb IS NULL OR (seq = 1) = (verb IN ('create', 'relate')));

-- COMPOSITE, for the reason 0011 made notes_current_revision composite: a
-- plain foreign key on restored_from would prove the source revision exists
-- and say NOTHING about whose it is, so a note could restore from another
-- note's text and the database would not object. 0011:124's UNIQUE (id,
-- note_id) exists for exactly this shape.
ALTER TABLE tier2.note_revisions
    DROP CONSTRAINT IF EXISTS note_revisions_restored_from;
ALTER TABLE tier2.note_revisions
    ADD CONSTRAINT note_revisions_restored_from
    FOREIGN KEY (restored_from, note_id)
    REFERENCES tier2.note_revisions (id, note_id);

COMMENT ON COLUMN tier2.note_revisions.confirmed_by IS
  'CHRN-39. Who agreed to this text landing — never an agent, enforced by '
  'CH041. Distinct from author_id, which is whose words these are and may '
  'legitimately be an agent. NULL only on rows written before 0014.';

COMMENT ON COLUMN tier2.note_revisions.verb IS
  'CHRN-39. What a person confirmed about a Scribe proposal: create, append, '
  'supersede or relate. NULL means authored directly.';

COMMENT ON COLUMN tier2.note_revisions.restored_from IS
  'CHRN-39. The revision this one was restored from. A restore APPENDS rather '
  'than rewinding (ruling 1), so this is what distinguishes a restore from an '
  'ordinary edit that happens to reproduce old text.';

-- ============================================================================
-- PART 3 · tier2.note_deletions — the journal (ruling 5).
-- ============================================================================
--
-- TIER 2. It is a record of what a person did to authored text, derivable from
-- nothing: notes.deleted_at holds the CURRENT state, and clearing it on
-- undelete would otherwise erase both that the delete happened and who did it.
--
-- The plan rejects exactly that property for revisions — "the fact and time of
-- the restore are recorded nowhere" is the argument against rewinding — so
-- accepting it for deletion would have been inconsistent. Ruling 5 took the
-- table rather than the inconsistency.
--
-- THE DENORMALISATION IS DELIBERATE. notes.deleted_at stays as the fast
-- predicate every read filters on; this table is the history. They must agree,
-- and the store writes both in one transaction.

CREATE TABLE IF NOT EXISTS tier2.note_deletions (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    note_id  UUID NOT NULL REFERENCES tier2.notes(id) ON DELETE RESTRICT,

    deleted_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_by   UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,

    undeleted_at TIMESTAMPTZ,
    undeleted_by UUID REFERENCES tier2.users(id) ON DELETE RESTRICT,

    CONSTRAINT note_deletions_undeleted_pair
        CHECK ((undeleted_at IS NULL) = (undeleted_by IS NULL))
);

-- AT MOST ONE OPEN DELETION PER NOTE. This is what makes SoftDeleteNote's
-- idempotency a property of the database rather than a promise Go keeps: a
-- second delete of an already-deleted note cannot open a second row, so the
-- original deleter cannot be quietly replaced.
CREATE UNIQUE INDEX IF NOT EXISTS note_deletions_open
    ON tier2.note_deletions (note_id) WHERE undeleted_at IS NULL;

CREATE INDEX IF NOT EXISTS note_deletions_note
    ON tier2.note_deletions (note_id, deleted_at DESC);

COMMENT ON TABLE tier2.note_deletions IS
  'CHRN-39, ruling 5. One row per deletion of a note, closed out on undelete. '
  'Tier 2 — a record of a person''s act on authored text, regenerable from '
  'nothing. tier2.notes.deleted_at is the current state; this is the history.';

-- ============================================================================
-- PART 4 · THE GUARDS.
-- ============================================================================
--
-- Error codes continue the one-block-per-table shape 0011 named: CH001-CH005
-- memos, CH010-CH011 proposals, CH020-CH022 memo_links, CH030-CH033 notes,
-- CH040-CH041 note_revisions, CH050-CH051 pages, CH060 note_tags, CH070
-- note_deletions.
--
-- CH060 opens a new block rather than borrowing a notes code, because the rule
-- it enforces lives on tier2.note_tags and a reader tracing CH03x would not
-- find it.

-- ----------------------------------------------------------------------------
-- notes_guard — REWRITTEN TO SAY WHAT IS MUTABLE, not only what is frozen.
-- ----------------------------------------------------------------------------
--
-- 0011's version froze id, number and author_id. That is a DENY LIST, and the
-- third `Done when` on CHRN-39 — "no code path in the service issues an UPDATE
-- that loses text" — cannot be kept by one: the day somebody adds a
-- text-bearing column to tier2.notes, a deny list permits writing it.
--
-- So the enumeration is inverted. Anything not named here is refused, and a
-- future column is refused by default until a migration adds it to the list.
-- That is the mechanical answer to the third Done when, and it is why the
-- check is written over to_jsonb(NEW) rather than as a list of IF statements:
-- a column that does not exist yet still has to be caught.
CREATE OR REPLACE FUNCTION tier2.notes_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    changed  TEXT;
    old_seq  INTEGER;
    new_seq  INTEGER;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Identity first, and kept byte-identical to 0011. It is a subset of
        -- the enumeration below, and it stays because it names the rule a
        -- caller has actually broken instead of listing a column.
        IF NEW.id        IS DISTINCT FROM OLD.id
        OR NEW.number    IS DISTINCT FROM OLD.number
        OR NEW.author_id IS DISTINCT FROM OLD.author_id THEN
            RAISE EXCEPTION 'note identity is immutable (id, number, author)'
                USING ERRCODE = 'CH030';
        END IF;

        -- THE ALLOW LIST. page_id is a move, current_revision_id is an edit,
        -- deleted_at/deleted_by are a soft delete, updated_at is maintained
        -- below. Everything else — including every column added after this
        -- migration — is refused.
        SELECT string_agg(n.key, ', ' ORDER BY n.key) INTO changed
          FROM jsonb_each(to_jsonb(NEW)) n
          JOIN jsonb_each(to_jsonb(OLD)) o ON o.key = n.key
         WHERE n.value IS DISTINCT FROM o.value
           AND n.key <> ALL (ARRAY['page_id', 'current_revision_id',
                                   'deleted_at', 'deleted_by', 'updated_at']);
        IF changed IS NOT NULL THEN
            RAISE EXCEPTION
                'tier2.notes permits updating page_id, current_revision_id, deleted_at, deleted_by only; refused: %',
                changed
                USING ERRCODE = 'CH031';
        END IF;

        -- A DELETED NOTE IS NOT EDITABLE, AND ITS DELETION PAIR IS NOT
        -- REWRITABLE. Undelete first — which is itself recorded, in
        -- tier2.note_deletions.
        --
        -- The first arm is for an agent holding a CHRN-67 write scope: without
        -- it, it can go on editing a note nobody can see, and every read
        -- surface filters it out so nobody would.
        --
        -- The second arm is for a direct writer. The store keeps this row and
        -- the journal in step by writing both in one transaction, but that is
        -- a promise Go keeps; an UPDATE that replaced deleted_at or deleted_by
        -- on an already-deleted note would leave this row naming a deleter the
        -- journal never heard of. So while deleted, the ONLY accepted change
        -- to the pair is clearing it — the undelete — which is what makes
        -- "a second caller cannot become the recorded deleter" a property of
        -- the database on this table too, not only on the journal.
        IF OLD.deleted_at IS NOT NULL THEN
            IF NEW.current_revision_id IS DISTINCT FROM OLD.current_revision_id
            OR NEW.page_id             IS DISTINCT FROM OLD.page_id THEN
                RAISE EXCEPTION 'note % is deleted: undelete it before writing to it', OLD.number
                    USING ERRCODE = 'CH033';
            END IF;
            IF NEW.deleted_at IS NOT NULL
            AND (NEW.deleted_at IS DISTINCT FROM OLD.deleted_at
              OR NEW.deleted_by IS DISTINCT FROM OLD.deleted_by) THEN
                RAISE EXCEPTION 'note % is already deleted: its deletion pair is not rewritable, only cleared', OLD.number
                    USING ERRCODE = 'CH033';
            END IF;
        END IF;

        -- FORWARD ONLY (ruling 1). A restore APPENDS a new revision carrying
        -- the old text, so current_revision_id never needs to move backwards,
        -- and refusing it here makes a silent rewind unrepresentable rather
        -- than merely un-coded. This is what stops a CHRN-67 writer moving the
        -- pointer back and leaving no trace that anything happened.
        --
        -- BOTH LOOKUPS ARE SCOPED TO THIS NOTE, and that is load-bearing
        -- rather than tidy. A pointer at ANOTHER note's revision is not a
        -- rewind, it is nonsense, and 0011's composite notes_current_revision
        -- is what refuses it — with 23503, which CHRN-38's criterion 3 asserts
        -- on. Unscoped, this would find that revision's seq, compare it, and
        -- raise CH032 first, so a cross-note pointer would report itself as a
        -- failed restore. Scoped, new_seq comes back NULL and the guard steps
        -- aside for the foreign key that owns the question.
        IF NEW.current_revision_id IS DISTINCT FROM OLD.current_revision_id THEN
            SELECT seq INTO old_seq FROM tier2.note_revisions
             WHERE id = OLD.current_revision_id AND note_id = OLD.id;
            SELECT seq INTO new_seq FROM tier2.note_revisions
             WHERE id = NEW.current_revision_id AND note_id = OLD.id;
            IF new_seq IS NOT NULL AND old_seq IS NOT NULL AND new_seq <= old_seq THEN
                RAISE EXCEPTION
                    'note % cannot move from revision % back to %: restore by appending',
                    OLD.number, old_seq, new_seq
                    USING ERRCODE = 'CH032';
            END IF;
        END IF;

        -- A PERSON DELETES, AND A PERSON UNDELETES (ruling 4). Same rule and
        -- same code as the confirmer on a revision, because it is the same
        -- rule: no agent removes authored text from view unattended.
        IF NEW.deleted_by IS DISTINCT FROM OLD.deleted_by AND NEW.deleted_by IS NOT NULL THEN
            IF NOT EXISTS (SELECT 1 FROM tier2.users
                            WHERE id = NEW.deleted_by AND kind = 'person') THEN
                RAISE EXCEPTION 'a note is deleted by a person, not by an agent'
                    USING ERRCODE = 'CH041';
            END IF;
        END IF;

        -- As memos_guard, transcripts_guard and memo_links_guard all do.
        -- Without it, updated_at is a column nothing maintains.
        NEW.updated_at := now();
    END IF;

    -- A NOTE IS NOT BORN DELETED. Deletion is an act on a note that already
    -- exists, done by a person and journaled; an INSERT arriving with the pair
    -- already set would skip the person test above (which reads OLD) and the
    -- journal alike. CreateNote never does this — the arm is for a direct
    -- writer, and it is the INSERT-side twin of the rewrite refusal above.
    IF TG_OP = 'INSERT' AND NEW.deleted_at IS NOT NULL THEN
        RAISE EXCEPTION 'a note is not created deleted: delete it after it exists, so the deletion is journaled'
            USING ERRCODE = 'CH033';
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER notes_guard BEFORE INSERT OR UPDATE ON tier2.notes
    FOR EACH ROW EXECUTE FUNCTION tier2.notes_guard();

-- ----------------------------------------------------------------------------
-- note_revisions_guard — APPEND-ONLY, plus a confirming person on every insert.
-- ----------------------------------------------------------------------------
--
-- 0011 raised unconditionally, because the trigger fired only on UPDATE and
-- DELETE. It now fires on INSERT too, so it branches — and the UPDATE/DELETE
-- arm is byte-identical to what 0011 wrote, message and code both. Nothing is
-- relaxed here; an arm is added.
--
-- SEQ 1 IS NOT EXEMPT, and that is ruling 4 rather than an oversight.
-- CreateNote inserts the first revision itself, so an exemption would be a
-- hole exactly where a Scribe-routed note arrives. The ticket's rule says
-- "nothing appends to authored text unattended" and a create appends to
-- nothing — but an exemption for agent-CREATED notes is a write-access
-- decision belonging to CHRN-67, which is Mode C precisely so it is argued
-- rather than inherited. Granting it here by omission would decide it
-- silently, so it is refused here and CHRN-67 may argue for it.
CREATE OR REPLACE FUNCTION tier2.note_revisions_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        RAISE EXCEPTION 'a note revision is append-only: % is refused', TG_OP
            USING ERRCODE = 'CH040';
    END IF;

    -- INSERT.
    IF NEW.confirmed_by IS NULL THEN
        RAISE EXCEPTION 'a note revision needs a confirming person: nothing lands in authored text unattended'
            USING ERRCODE = 'CH041';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM tier2.users
                    WHERE id = NEW.confirmed_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a note revision is confirmed by a person, not by an agent'
            USING ERRCODE = 'CH041';
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER note_revisions_guard
    BEFORE INSERT OR UPDATE OR DELETE ON tier2.note_revisions
    FOR EACH ROW EXECUTE FUNCTION tier2.note_revisions_guard();

-- ----------------------------------------------------------------------------
-- note_deletions_guard — a person on both ends, and the record stays put.
-- ----------------------------------------------------------------------------
--
-- THE UNDELETER IS TESTED HERE BECAUSE notes_guard CANNOT SEE IT. Ruling 4
-- wants the same person rule on whoever brings a note back, but undeleted_by
-- is a column on THIS table: the UPDATE that undeletes sets notes.deleted_by
-- to NULL, so notes_guard's test — which fires only on a non-null new value —
-- is correctly silent. Without this trigger the undelete half of the rule
-- would read as enforced and do nothing, which is the failure CH060 exists to
-- avoid one table over.
--
-- CH041 is deliberately reused rather than given a new number: it is the same
-- rule as the confirmer on a revision, and a caller that catches "a person is
-- required" should not have to know how many tables it can come from.
--
-- The row is otherwise closed to rewriting (CH070). A deletion record that can
-- be edited is not a record, and this is a tier-2 table for the same reason
-- note_revisions is: it is what a person did, and it is regenerable from
-- nothing.
CREATE OR REPLACE FUNCTION tier2.note_deletions_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'a deletion record is not removable'
            USING ERRCODE = 'CH070';
    END IF;

    IF TG_OP = 'UPDATE' THEN
        -- The only legal UPDATE is closing an open record out.
        IF NEW.id         IS DISTINCT FROM OLD.id
        OR NEW.note_id    IS DISTINCT FROM OLD.note_id
        OR NEW.deleted_at IS DISTINCT FROM OLD.deleted_at
        OR NEW.deleted_by IS DISTINCT FROM OLD.deleted_by THEN
            RAISE EXCEPTION 'a deletion record is immutable except for being closed out'
                USING ERRCODE = 'CH070';
        END IF;
        IF OLD.undeleted_at IS NOT NULL THEN
            RAISE EXCEPTION 'that deletion record is already closed'
                USING ERRCODE = 'CH070';
        END IF;
    END IF;

    IF NOT EXISTS (SELECT 1 FROM tier2.users
                    WHERE id = NEW.deleted_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a note is deleted by a person, not by an agent'
            USING ERRCODE = 'CH041';
    END IF;
    IF NEW.undeleted_by IS NOT NULL
    AND NOT EXISTS (SELECT 1 FROM tier2.users
                     WHERE id = NEW.undeleted_by AND kind = 'person') THEN
        RAISE EXCEPTION 'a note is undeleted by a person, not by an agent'
            USING ERRCODE = 'CH041';
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER note_deletions_guard
    BEFORE INSERT OR UPDATE OR DELETE ON tier2.note_deletions
    FOR EACH ROW EXECUTE FUNCTION tier2.note_deletions_guard();

-- ----------------------------------------------------------------------------
-- note_tags_guard — a deleted note cannot be tagged.
-- ----------------------------------------------------------------------------
--
-- IT LIVES HERE AND NOT IN notes_guard, and the reason is mechanical rather
-- than stylistic: TagNote inserts into tier2.note_tags and never touches the
-- notes row, so notes_guard — which fires on UPDATE of tier2.notes — cannot
-- see the write at all. CH033 would never fire, and the rule would read as
-- enforced while doing nothing.
-- INSERT **OR DELETE**, and the second arm is not symmetry for its own sake.
-- An INSERT-only guard refuses adding a tag to a deleted note while permitting
-- its removal, and tier2.note_tags has no journal — so the tag would simply be
-- gone, from a note no read surface shows. "An agent must not keep editing
-- something nobody can see" is not a rule about additions.
--
-- NEW is unassigned on a DELETE, so the row is chosen by TG_OP rather than by
-- COALESCE(NEW.note_id, ...), which would raise before it could be nullish.
CREATE OR REPLACE FUNCTION tier2.note_tags_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    target UUID;
BEGIN
    IF TG_OP = 'DELETE' THEN
        target := OLD.note_id;
    ELSE
        target := NEW.note_id;
    END IF;

    IF EXISTS (SELECT 1 FROM tier2.notes
                WHERE id = target AND deleted_at IS NOT NULL) THEN
        RAISE EXCEPTION 'that note is deleted: undelete it before changing its tags'
            USING ERRCODE = 'CH060';
    END IF;

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER note_tags_guard
    BEFORE INSERT OR DELETE ON tier2.note_tags
    FOR EACH ROW EXECUTE FUNCTION tier2.note_tags_guard();

-- ============================================================================
-- THE TIER BOUNDARY.
-- ============================================================================
--
-- Redundant, and stated anyway as documentation of intent at the point the
-- tier boundary is defined, per the pattern 0002 established and 0003 words.
--
-- WHAT MAKES IT REDUNDANT IS NARROWER THAN "0001 REVOKED THE SCHEMA", and the
-- difference matters because 0007:52 re-granted USAGE on schema tier2. The
-- fact that survives 0007: no GRANT on tier2.note_deletions was ever issued,
-- and 0007 deliberately added no ALTER DEFAULT PRIVILEGES on schema tier2 — so
-- a tier-2 table created today is unreachable by chronicle_tier1 on table
-- privileges alone, whatever it holds on the schema.
--
-- NOTHING HERE GRANTS ANYTHING. 0007:53 remains the only tier-2 table grant in
-- any migration, and per CHRN-91 a GRANT on a tier-2 notes table is an open
-- finding under REVIEW.md §1 rather than an expected step.
REVOKE ALL ON tier2.note_deletions FROM chronicle_tier1;

-- ============================================================================
-- FOR THE HTTP SURFACE THAT DOES NOT EXIST YET.
-- ============================================================================
--
-- There is no /notes route in internal/api, and this ticket deliberately did
-- not add one — CHRN-38 shipped store-only for the same reason and the web
-- note view is CHRN-56, in E8. Recorded here so the decision is not re-derived
-- when it does land:
--
-- A SOFT-DELETED NOTE IS 410 GONE, NOT 404. The note exists, it is
-- recoverable, and CHR-#### is a permanent identifier — 404 would say it never
-- existed and invite the number's reuse. note_number_seq already guarantees a
-- number is never reissued; this is about what the reader is told.
