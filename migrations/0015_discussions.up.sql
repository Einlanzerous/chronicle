-- 0015_discussions — CHRN-43. Threads, turns, participants, and the rule that
-- an agent may not speak twice in a row.
--
-- Decided before any code, in Mode B: the Switchyard plan on CHRN-43,
-- revision 2, approved 2026-09-07 with all six rulings picked. Read that for
-- the argument; this file carries what the argument concluded. Section names
-- in these comments — `schema`, `ordering`, `depth`, `handle`, `agent-loop`,
-- `resolution-verb`, `guards`, `store-surface` — refer to it.
--
-- The six picks, because a reader of this file should not have to fetch the
-- plan to know which branch it is on:
--
--   1 depth         FLAT. No parent pointer at all; seq is the only structure.
--   2 handle        DSC-####, sequence-allocated, on CHR-####'s rules.
--   3 agent loop    AN AGENT TURN REQUIRES A PERSON'S TURN IMMEDIATELY BEFORE
--                   IT — in the store, CH091, not in the reply handler.
--   4 resolution    THE REVISION A RESOLUTION WRITES CARRIES verb NULL, with
--                   discussions.resolved_note_id as its provenance.
--   5 author kind   author_kind ON THE TURN, set by the trigger (CH092), so
--                   CH091 never reads a column a plain UPDATE can change.
--   6 resolved      REFUSE a turn on a resolved discussion (CH093).
--
-- Depends on 0010 (CHRN-37) for tier2.pages, 0011 (CHRN-38) for tier2.notes,
-- 0003 (CHRN-26) for tier2.memos, and 0002 (CHRN-71) for tier2.users.kind.
--
-- ============================================================================
-- THREE ITEMS THIS MIGRATION CARRIES THAT ITS TITLE DOES NOT NAME.
-- ============================================================================
--
-- Assigned here in the 2026-09-06 E6 planning pass, each because leaving it
-- where it fell would have put a schema or safety decision inside a ticket
-- nobody reads the diff of:
--
--   * THE PARTICIPANT SCHEMA, which CHRN-44 implements and does not invent.
--   * THE ANTI-AGENT-LOOP GUARANTEE, which CHRN-47's `Done when` asserts and
--     which is a property of this schema rather than of its reply handler.
--     CHRN-67's MCP `reply` tool is a second caller by design, and a rule
--     living in one handler is a rule the second caller does not know about.
--   * WHAT verb A THREAD RESOLUTION WRITES, which CHRN-46 passes and which is
--     a semantic on a column with a trigger on it.

-- ============================================================================
-- PART 1 · tier2.discussions — the thread.
-- ============================================================================
--
-- TIER 2. A conversation is what people said; it is derivable from nothing and
-- regenerable from nothing, which is the whole of the test.

-- THE PERMANENT HANDLE, rendered DSC-0007, on tier2.note_number_seq's rules:
-- sequence-allocated, never reused, gaps correct, four digits as a MINIMUM
-- width and not a cap. Reuse would make DSC-0007 resolve to two threads across
-- time, which is the same failure 0011:57-64 rules out for notes.
--
-- WHY IT EXISTS AT ALL, given a UUID would address the row (ruling 2): CH090
-- below makes turns immutable and `number` sits on the discussion row, so the
-- handle either exists from day one or is backfilled onto a populated table
-- later — the migration this ticket's tier note says it exists to avoid. The
-- consumer is CHRN-47's "a routed memo that lands in an existing thread": for
-- the Scribe to NAME a thread it needs something a model can produce from a
-- transcript, and a UUID is not that.
--
-- DSC collides with nothing: CHR-#### notes, CHRN-## this project, SY-###
-- Switchyard, AMB-#### Amber.
CREATE SEQUENCE IF NOT EXISTS tier2.discussion_number_seq AS BIGINT START WITH 1;

CREATE TABLE IF NOT EXISTS tier2.discussions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    number      BIGINT NOT NULL UNIQUE
                    DEFAULT nextval('tier2.discussion_number_seq'),

    -- NULLABLE, and the difference from tier2.notes.page_id is the decision.
    -- A note is ADDRESSED by its page. A thread is not addressed at all until
    -- it resolves — it may be about a page, or about nothing yet.
    page_id     UUID REFERENCES tier2.pages(id) ON DELETE RESTRICT,

    -- ON THE ROW RATHER THAN IN A REVISION, which is the opposite of 0011's
    -- call for a note's title and is deliberate. A note's title is authored
    -- prose a rename would destroy, so it lives in a revision. A thread's
    -- title is a LABEL on a conversation; the conversation is the turns, and
    -- no turn is lost by renaming it. Revision-controlling this would rebuild
    -- all of CHRN-39's machinery for a field nobody restores.
    title       TEXT NOT NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- THE THREAD'S PRODUCT (CHRN-46). resolved_note_id is optional: some
    -- threads just end, and the ticket wants that to be a deliberate choice
    -- rather than an error.
    resolved_at      TIMESTAMPTZ,
    resolved_by      UUID REFERENCES tier2.users(id) ON DELETE RESTRICT,
    resolved_note_id UUID REFERENCES tier2.notes(id) ON DELETE RESTRICT,

    -- Null together or set together, so "resolved" is one question with one
    -- answer — 0014:36-37's wording for the deletion pair, and the same
    -- reason: there is no half-resolved row for a reader to interpret.
    CONSTRAINT discussions_resolved_pair
        CHECK ((resolved_at IS NULL) = (resolved_by IS NULL)),

    -- A NOTE WITHOUT A RESOLUTION IS NOT A STATE. It would claim a thread
    -- produced something while saying nothing about when or by whom.
    CONSTRAINT discussions_note_needs_resolution
        CHECK (resolved_note_id IS NULL OR resolved_at IS NOT NULL)
);

-- NO UNIQUE ON resolved_note_id, AND THAT IS INTENDED. Two threads may resolve
-- into one note, which is right when resolving into an EXISTING note is
-- allowed — CHRN-46's own example is "Resolved into PRINCIPLES §6", a section
-- of something already written, and a long-lived page collects several. Stated
-- because the absence would otherwise read as an oversight next to 0011's
-- note_revisions_memo, which is unique for the opposite reason.
CREATE INDEX IF NOT EXISTS discussions_page ON tier2.discussions (page_id);
CREATE INDEX IF NOT EXISTS discussions_resolved_note
    ON tier2.discussions (resolved_note_id) WHERE resolved_note_id IS NOT NULL;

COMMENT ON TABLE tier2.discussions IS
  'CHRN-43. One threaded conversation, addressed as DSC-####. Tier 2 — what '
  'people said, derivable from nothing. The turns are tier2.discussion_turns; '
  'the thread''s product, if it has one, is resolved_note_id.';

COMMENT ON COLUMN tier2.discussions.number IS
  'CHRN-43, ruling 2. The permanent handle, rendered DSC-0007. Sequence-'
  'allocated on tier2.note_number_seq''s rules: never reused, gaps correct, '
  'four digits as a minimum width rather than a cap.';

COMMENT ON COLUMN tier2.discussions.resolved_note_id IS
  'CHRN-43, ruling 4. The note this thread produced, new or pre-existing. It '
  'is also the PROVENANCE of the revision that resolution wrote: that '
  'revision carries verb NULL, and this column is what says where it came '
  'from — the shape 0014 already uses for a restore.';

-- ============================================================================
-- PART 2 · tier2.discussion_turns — what was actually said.
-- ============================================================================
--
-- FLAT (ruling 1). There is no parent pointer, and its absence is the
-- decision: `seq` is the only structure a turn has.
--
-- The participant count is two — a person and the Scribe. A tree solves many
-- simultaneous conversations in one thread, which is not this system's shape;
-- it makes "what did this conclude" a traversal rather than a read; and it
-- makes CHRN-45's unread state a traversal rather than one integer comparison.
-- Nesting is also free to add later and not free to remove: a nullable pointer
-- on an insert-only table is additive, and flattening a tree is not.

CREATE TABLE IF NOT EXISTS tier2.discussion_turns (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    discussion_id UUID NOT NULL REFERENCES tier2.discussions(id) ON DELETE RESTRICT,

    -- SERVER-ASSIGNED, UNDER THE THREAD'S ROW LOCK. See `ordering`: without
    -- the lock two concurrent appends read the same max(seq), one violates the
    -- unique constraint below, and the thread's tail depends on commit order.
    -- 0011:95-98 says the same thing about a note's revisions, and AppendTurn
    -- copies AppendRevision's lock rather than inventing a retry loop.
    seq           INTEGER NOT NULL,

    author_id     UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,

    -- RULING 5, AND IT IS WHAT MAKES CH091 TRUSTWORTHY.
    --
    -- CH091 has to know the kind of the PREVIOUS turn's author. Reading it by
    -- joining tier2.users would read a MUTABLE column: nothing in any
    -- migration freezes users.kind, so flipping an account person→agent would
    -- make a legal thread retroactively read as the loop the rule forbids, and
    -- agent→person would make a real loop retroactively legitimate. A safety
    -- property cannot rest on that.
    --
    -- So the kind is DENORMALISED onto the turn at write time and the trigger
    -- keeps it honest. A later change to somebody's kind does not rewrite
    -- history, which is the opposite of what a join would do and is correct
    -- here. It is also literally what CHRN-44's "the distinction survives
    -- export" asks for: a fact about the row, readable without a join.
    --
    -- NOT NULL WITH NO DEFAULT, FILLED BY A BEFORE INSERT TRIGGER. That works,
    -- and it looks wrong on first read, so: NOT NULL is checked AFTER BEFORE
    -- triggers have run. It is also exactly how CH092 tells a supplied value
    -- from an omitted one — NEW.author_kind IS NOT NULL on entry to the
    -- trigger means the caller set it.
    author_kind   TEXT NOT NULL
                      CHECK (author_kind IN ('person','agent')),

    body          TEXT NOT NULL,

    -- ARRIVAL, and it orders nothing. seq does.
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- THE CLIENT'S CLAIM ABOUT WHEN IT WAS WRITTEN. Advisory, nullable, and
    -- NEVER SORTED ON.
    --
    -- The ticket asks that "an offline reply lands where it belongs", and
    -- where it belongs is WHERE IT ARRIVED. The alternative — inserting into
    -- the middle by composed_at — means renumbering, and renumbering is an
    -- UPDATE to the ordering CHRN-45's read markers are defined against, so
    -- every stored position would silently change meaning. This column is what
    -- makes "when did I say that" answerable without letting an unverifiable
    -- clock reorder the record.
    composed_at   TIMESTAMPTZ,

    memo_id       UUID REFERENCES tier2.memos(id) ON DELETE RESTRICT,

    UNIQUE (discussion_id, seq),
    CONSTRAINT discussion_turns_seq_positive CHECK (seq >= 1),

    -- A turn is something somebody said. Blank is not.
    CONSTRAINT discussion_turns_body_not_blank CHECK (btrim(body) <> '')
);

-- UNIQUE, not merely indexed — 0011:142-146's argument, transferred unchanged.
-- tier2.memo_links is UNIQUE (memo_id), so a memo lands exactly once; a plain
-- index here would silently permit one memo to author several turns, which is
-- a different decision from the one CHRN-33 made and would be made by
-- omission.
CREATE UNIQUE INDEX IF NOT EXISTS discussion_turns_memo
    ON tier2.discussion_turns (memo_id) WHERE memo_id IS NOT NULL;

COMMENT ON TABLE tier2.discussion_turns IS
  'CHRN-43. One thing said in a thread. INSERT-ONLY (CH090): a conversation '
  'whose turns can be edited is a record that lies, and a correction is '
  'another turn. Ordered by seq, which the server assigns under the thread''s '
  'row lock; created_at is arrival and composed_at is the client''s claim, '
  'and neither orders anything.';

COMMENT ON COLUMN tier2.discussion_turns.author_kind IS
  'CHRN-43, ruling 5. The author''s kind AT THE TIME THE TURN WAS WRITTEN, '
  'set by discussion_turns_guard from tier2.users.kind and refused from a '
  'caller (CH092). Denormalised on purpose: CH091 is a safety rule and '
  'tier2.users.kind is mutable, so reading it live would let an account edit '
  'rewrite the verdict on turns already written.';

COMMENT ON COLUMN tier2.discussion_turns.composed_at IS
  'CHRN-43. When the client says this was written, for the phone that was '
  'offline for six hours. ADVISORY: unverifiable, nullable, and never sorted '
  'on — seq is arrival order and is the only ordering.';

-- ============================================================================
-- PART 3 · tier2.discussion_participants — who is expected to read this.
-- ============================================================================
--
-- CURRENT STATE, NOT A JOURNAL, and the difference from tier2.note_deletions
-- is worth stating because that table exists for the opposite reason.
--
-- CHRN-39 needed a journal because undelete CLEARS notes.deleted_at and would
-- otherwise erase that a deletion ever happened. Membership carries no such
-- risk: a removed participant's turns still render, because a turn holds its
-- own author_id and author_kind and NOTHING READS MEMBERSHIP TO DECIDE
-- AUTHORSHIP. Re-adding clears the removed pair; add/remove history is not
-- kept, and would be an insert-only sibling table if it were ever wanted.

CREATE TABLE IF NOT EXISTS tier2.discussion_participants (
    discussion_id UUID NOT NULL REFERENCES tier2.discussions(id) ON DELETE RESTRICT,
    user_id       UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,

    -- FROZEN, AND THEY MEAN *FIRST* ADDED. The allow list in
    -- discussion_participants_guard refuses rewriting them, so a re-add after
    -- a removal does not quietly reattribute the original invitation. Both
    -- readings lose a fact — overwriting loses who first added them, keeping
    -- loses who re-added them — and this table has already accepted that
    -- history is not kept, so the one that does not rewrite a recorded fact
    -- wins.
    added_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by      UUID NOT NULL REFERENCES tier2.users(id) ON DELETE RESTRICT,

    removed_at    TIMESTAMPTZ,
    removed_by    UUID REFERENCES tier2.users(id) ON DELETE RESTRICT,

    PRIMARY KEY (discussion_id, user_id),
    CONSTRAINT discussion_participants_removed_pair
        CHECK ((removed_at IS NULL) = (removed_by IS NULL))
);

COMMENT ON TABLE tier2.discussion_participants IS
  'CHRN-43 / CHRN-44. Who is expected to READ a thread — CHRN-45''s question, '
  'not "who may write". Current state rather than a journal: a removed '
  'participant''s turns still render, so nothing is lost by not keeping the '
  'add/remove history. added_at and added_by are frozen and mean FIRST added.';

-- ============================================================================
-- PART 4 · THE GUARDS.
-- ============================================================================
--
-- Error codes continue the one-block-per-table shape 0011 named and 0014
-- extended: CH001-CH005 memos, CH010-CH011 proposals, CH020-CH022 memo_links,
-- CH030-CH033 notes, CH040-CH041 note_revisions, CH050-CH051 pages, CH060
-- note_tags, CH070 note_deletions. This migration opens three more blocks:
--
--   CH080  discussions             UPDATE allow list; resolved_by is a person
--   CH090  discussion_turns        immutable — UPDATE and DELETE refused
--   CH091  discussion_turns        an agent turn requires a person's turn
--                                  immediately before it (ruling 3)
--   CH092  discussion_turns        author_kind is the trigger's to set, never
--                                  the caller's (ruling 5)
--   CH093  discussion_turns        no turn on a resolved discussion (ruling 6)
--   CH100  discussion_participants added_by / removed_by are people; the
--                                  removed pair is the only mutable thing

-- ----------------------------------------------------------------------------
-- discussions_guard — CH080. An ALLOW LIST, and a resolution that happens once.
-- ----------------------------------------------------------------------------
--
-- AN ALLOW LIST FOR 0014:199-208's REASON, which transfers unchanged: the day
-- somebody adds a text-bearing column to this table, a deny list permits
-- writing it. Anything not named here is refused, and a future column is
-- refused by default until a migration adds it to the list.
--
-- WHAT MAY CHANGE: title (a relabel), page_id (filing the thread against a
-- page), and each of the three resolution columns ONCE, FROM NULL.
--
-- UN-RESOLVING IS REFUSED BY THE SAME CLAUSE. A resolved thread that quietly
-- becomes unresolved loses the record that it concluded, and CHRN-46's "a
-- resolved thread is still readable rather than hidden" means the resolution
-- is part of the record. Ruling 6 leans on this: because un-resolving is
-- impossible, refusing turns on a resolved thread closes it permanently, and
-- continuing means opening a new thread that cites it.
--
-- ONCE-FROM-NULL IS PER COLUMN, WHICH IS WHAT LETS A NOTE BE LINKED LATER.
-- CHRN-46 makes resolving with no note a deliberate choice, and reads as
-- permitting the thread to be pointed at a note afterwards; so
-- resolved_note_id may go NULL → set while resolved_at is already set. What it
-- may never do is change or be cleared, which is the property that matters —
-- a recorded resolution is never rewritten, only completed.
CREATE OR REPLACE FUNCTION tier2.discussions_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    changed TEXT;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- THE ALLOW LIST. Written over to_jsonb(NEW) rather than as a list of
        -- IF statements for 0014:206-208's reason: a column that does not
        -- exist yet still has to be caught.
        SELECT string_agg(n.key, ', ' ORDER BY n.key) INTO changed
          FROM jsonb_each(to_jsonb(NEW)) n
          JOIN jsonb_each(to_jsonb(OLD)) o ON o.key = n.key
         WHERE n.value IS DISTINCT FROM o.value
           AND n.key <> ALL (ARRAY['title', 'page_id',
                                   'resolved_at', 'resolved_by', 'resolved_note_id']);
        IF changed IS NOT NULL THEN
            RAISE EXCEPTION
                'tier2.discussions permits updating title, page_id and the resolution triple only; refused: %',
                changed
                USING ERRCODE = 'CH080',
                      CONSTRAINT = 'discussions_update_allow_list';
        END IF;

        -- SET ONCE, FROM NULL. Covers clearing and rewriting in one clause,
        -- per column, so a resolution can be COMPLETED (a note linked later)
        -- but never revised or withdrawn.
        IF (OLD.resolved_at IS NOT NULL AND NEW.resolved_at IS DISTINCT FROM OLD.resolved_at)
        OR (OLD.resolved_by IS NOT NULL AND NEW.resolved_by IS DISTINCT FROM OLD.resolved_by)
        OR (OLD.resolved_note_id IS NOT NULL
            AND NEW.resolved_note_id IS DISTINCT FROM OLD.resolved_note_id) THEN
            RAISE EXCEPTION
                'discussion %: a resolution is recorded once and is not rewritten or withdrawn',
                OLD.number
                USING ERRCODE = 'CH080',
                      CONSTRAINT = 'discussions_resolution_once';
        END IF;
    END IF;

    -- A PERSON RESOLVES A THREAD. The same rule CH041 states about a note's
    -- confirmer and deleter, for the same reason: resolving decides what a
    -- conversation concluded and writes it into the corpus, and no agent does
    -- that unattended. CHRN-46 passes the actor; CHRN-67 may argue for more.
    --
    -- TESTED ON INSERT TOO, not only on UPDATE. A row that arrived already
    -- resolved would otherwise skip this entirely — the same hole 0014:316-324
    -- closes for a note born deleted. A discussion born resolved is not itself
    -- forbidden (OpenDiscussion never writes one, and the plan does not ask
    -- for the rule), but it does not get to name an agent.
    --
    -- THE CONSTRAINT NAME IS WHAT MAKES THIS ARM DISTINGUISHABLE IN GO. Both
    -- arms of CH080 are CH080 because the plan's error table says so and
    -- criterion 18 asserts it — but "you named an agent" and "that resolution
    -- is already recorded" are different answers a handler owes a caller (403
    -- against 409), and reading them apart by matching the message text would
    -- be a string comparison against a sentence. RAISE ... USING CONSTRAINT
    -- puts the distinction in pgconn.PgError.ConstraintName, which note.go:752
    -- already reads for note_revisions_memo.
    IF NEW.resolved_by IS NOT NULL
    AND (TG_OP = 'INSERT' OR NEW.resolved_by IS DISTINCT FROM OLD.resolved_by) THEN
        IF NOT EXISTS (SELECT 1 FROM tier2.users
                        WHERE id = NEW.resolved_by AND kind = 'person') THEN
            RAISE EXCEPTION 'a discussion is resolved by a person, not by an agent'
                USING ERRCODE = 'CH080',
                      CONSTRAINT = 'discussions_resolver_is_a_person';
        END IF;
    END IF;

    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER discussions_guard
    BEFORE INSERT OR UPDATE ON tier2.discussions
    FOR EACH ROW EXECUTE FUNCTION tier2.discussions_guard();

-- ----------------------------------------------------------------------------
-- discussion_turns_guard — CH090/CH091/CH092/CH093. The load-bearing one.
-- ----------------------------------------------------------------------------
--
-- CH090 — A TURN IS INSERT-ONLY, and this is the one the rest rests on. A
-- conversation whose turns can be edited is a record that lies, in the
-- direction that matters: CHRN-44's stated worry is that "six months later
-- nobody can tell which conclusions in a thread were reasoned by a person",
-- and an editable turn makes that unverifiable. There is no revision machinery
-- here and none is wanted — A CORRECTION TO A THREAD IS ANOTHER TURN, which is
-- how conversations work. 0011:187's "APPEND-ONLY, ENFORCED HERE AND NOT IN
-- GO" is the precedent and the wording.
--
-- CH091 — AN AGENT MAY NOT SPEAK TWICE IN A ROW (ruling 3). The rule is: a
-- turn authored by an agent requires the immediately preceding turn in the
-- same thread to be authored by a person.
--
--     seq  author   verdict
--     1    person   ok
--     2    agent    ok      — preceded by a person
--     3    agent    REFUSED — the loop this exists to prevent
--     3    person   ok
--     4    agent    ok
--
-- A loop is impossible BY CONSTRUCTION, including between two different
-- agents, because the test is on kind and not on identity.
--
-- AN AGENT CANNOT OPEN A THREAD falls out of it: seq 1 has no preceding turn.
-- That is the safe direction and deliberately the same deferral CHRN-39 made
-- when it refused to exempt seq 1 from CH041 — whether an agent may ORIGINATE
-- authored content is CHRN-67's argument to make at Mode C, not this
-- migration's to grant by omission.
--
-- WHY IT IS HERE AND NOT IN internal/api'S REPLY PATH: CHRN-67's MCP `reply`
-- tool is a second caller by design, and a rule living in one handler is one
-- the agent surface does not inherit. CHRN-47 is Mode A — nobody reads its
-- diff — which is exactly why the guarantee was moved upstream to here.
--
-- WHAT IT COSTS: an agent cannot legitimately split a long reply into two
-- turns. Accepted — one turn can be long, and the alternative is counting
-- consecutive agent turns, which is a threshold where a structural rule will
-- do.
--
-- CORRECT UNDER CONCURRENCY because AppendTurn holds the thread's row lock, so
-- appends to one thread are serial and "the turn at seq - 1 is visible" is a
-- fact rather than snapshot reasoning a reader has to re-derive.
--
-- A NOTE FOR CHRN-68's RESTORE DRILL. A full pg_dump/pg_restore is SAFE from
-- CH092: triggers are post-data, so every turn is loaded before this function
-- exists. A selective row-level replay — INSERT … SELECT out of a dump into a
-- live database — is NOT: it carries author_kind and CH092 refuses every row.
-- The same is already true of CH041 on confirmed_by for pre-0014 rows; this
-- makes it two triggers with the property, which is worth knowing before the
-- drill rather than during it.
CREATE OR REPLACE FUNCTION tier2.discussion_turns_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    kind      TEXT;
    prev_kind TEXT;
    resolved  TIMESTAMPTZ;
    thread    BIGINT;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') THEN
        RAISE EXCEPTION 'a discussion turn is insert-only: % is refused; a correction is another turn', TG_OP
            USING ERRCODE = 'CH090';
    END IF;

    -- CH092 — author_kind IS THE TRIGGER'S TO SET. The column is NOT NULL with
    -- no default, so a caller that omits it arrives here with NULL and a
    -- caller that supplied one arrives with a value. That is the whole test,
    -- and it is only available because NOT NULL is checked after BEFORE
    -- triggers run.
    IF NEW.author_kind IS NOT NULL THEN
        RAISE EXCEPTION 'author_kind is derived from the author''s account, not supplied by the caller'
            USING ERRCODE = 'CH092';
    END IF;

    SELECT u.kind INTO kind FROM tier2.users u WHERE u.id = NEW.author_id;
    IF kind IS NULL THEN
        -- The foreign key owns this question, but it is checked at the end of
        -- the statement — AFTER the NOT NULL on author_kind, which would
        -- otherwise report a missing author as a null-constraint failure on a
        -- column the caller is forbidden to set. Raising 23503 here gives the
        -- store the same SQLSTATE it already maps to ErrNotFound (note.go:755).
        RAISE EXCEPTION 'no such author: %', NEW.author_id
            USING ERRCODE = '23503';
    END IF;
    NEW.author_kind := kind;

    -- CH093 — RULING 6. Free, because AppendTurn already holds this row.
    SELECT d.resolved_at, d.number INTO resolved, thread
      FROM tier2.discussions d WHERE d.id = NEW.discussion_id;
    IF resolved IS NOT NULL THEN
        RAISE EXCEPTION 'discussion %: resolved on %, and a resolved thread takes no more turns; open a new one citing it',
            thread, resolved
            USING ERRCODE = 'CH093';
    END IF;

    -- CH091 — RULING 3. Reads the PRECEDING TURN'S OWN author_kind, with no
    -- join to tier2.users and so no dependency on a mutable column. seq 1
    -- finds nothing, which refuses an agent opening a thread.
    IF NEW.author_kind = 'agent' THEN
        SELECT t.author_kind INTO prev_kind
          FROM tier2.discussion_turns t
         WHERE t.discussion_id = NEW.discussion_id AND t.seq = NEW.seq - 1;
        IF prev_kind IS DISTINCT FROM 'person' THEN
            RAISE EXCEPTION
                'an agent turn must follow a person''s turn: discussion % seq % would follow %',
                thread, NEW.seq, COALESCE(prev_kind, 'nothing')
                USING ERRCODE = 'CH091';
        END IF;
    END IF;

    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER discussion_turns_guard
    BEFORE INSERT OR UPDATE OR DELETE ON tier2.discussion_turns
    FOR EACH ROW EXECUTE FUNCTION tier2.discussion_turns_guard();

-- ----------------------------------------------------------------------------
-- discussion_participants_guard — CH100. People on both ends, and a frozen add.
-- ----------------------------------------------------------------------------
--
-- AN ALLOW LIST, for the same reason CH080 is one. The table has three writes
-- — add, remove, re-add — and the third is the interesting one: re-adding
-- clears the removed pair, and it must not silently reattribute the original
-- invitation. So the ONLY mutable thing is the removed pair, and added_at /
-- added_by mean FIRST added.
--
-- CHRN-45 adds its read marker to this list by migration if the marker lands
-- on this table.
--
-- NO GUARD ON WHO MAY WRITE A TURN. Nothing requires a turn's author to be a
-- participant: a person who posts is thereby in the conversation, and
-- enforcing membership would make the ordering of two writes load-bearing for
-- no gain. This table answers "who is expected to READ this" — CHRN-45's
-- question — and not "who may write".
CREATE OR REPLACE FUNCTION tier2.discussion_participants_guard() RETURNS TRIGGER LANGUAGE plpgsql AS $fn$
DECLARE
    changed TEXT;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        SELECT string_agg(n.key, ', ' ORDER BY n.key) INTO changed
          FROM jsonb_each(to_jsonb(NEW)) n
          JOIN jsonb_each(to_jsonb(OLD)) o ON o.key = n.key
         WHERE n.value IS DISTINCT FROM o.value
           AND n.key <> ALL (ARRAY['removed_at', 'removed_by']);
        IF changed IS NOT NULL THEN
            RAISE EXCEPTION
                'tier2.discussion_participants permits updating removed_at, removed_by only; added_at and added_by mean FIRST added; refused: %',
                changed
                USING ERRCODE = 'CH100',
                      CONSTRAINT = 'discussion_participants_update_allow_list';
        END IF;
    END IF;

    -- A PERSON ADDS AND A PERSON REMOVES. Membership decides who is expected
    -- to read a thread, and an agent quietly removing a person from a
    -- conversation is the shape CH041 exists to refuse one table over. Both
    -- ends, because an INSERT-only test would leave the removal half reading
    -- as enforced while doing nothing — 0014:378-384's failure, named there.
    --
    -- CONSTRAINT named for CH080's reason, one function up.
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

    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER discussion_participants_guard
    BEFORE INSERT OR UPDATE ON tier2.discussion_participants
    FOR EACH ROW EXECUTE FUNCTION tier2.discussion_participants_guard();

-- ============================================================================
-- PART 5 · WHAT A RESOLUTION WRITES ON A NOTE REVISION (ruling 4).
-- ============================================================================
--
-- CHRN-46 resolves a thread into a note, new or existing, and whichever
-- revision that writes carries verb NULL.
--
-- 0014:83-87 already names three legitimately-NULL classes and one of them is
-- "a restore (restored_from says so)" — a NULL verb whose SIBLING COLUMN
-- records where the revision came from. A resolution is that shape exactly,
-- with tier2.discussions.resolved_note_id as the sibling, so this is a
-- precedent rather than a reinterpretation.
--
-- The alternatives, and why they lose: `create` is mechanically true at seq 1
-- but claims a proposal that never existed, and says nothing at seq n — and
-- CHRN-46's own example ("Resolved into PRINCIPLES §6") is an APPEND to
-- something already written. A fifth verb `resolve` costs more than it looks:
-- scribe.Verbs and the store's constants are asserted IDENTICAL by
-- TestScribeVerbsMatchTheColumn, which CHRN-94 shipped on 2026-09-06, so a
-- store-only verb means deciding those sets are not the same and weakening
-- that guard.
--
-- THE COMMENT IS AMENDED RATHER THAN LEFT TO CONTRADICT THE PICK. Three
-- legitimately-NULL classes become four. 0015_discussions.down.sql restores
-- 0014's text verbatim, or up-then-down-then-up would not be byte-identical
-- and CI's schema staleness guard would catch it one commit later.
COMMENT ON COLUMN tier2.note_revisions.verb IS
  'CHRN-39. What a person confirmed about a Scribe proposal: create, append, '
  'supersede or relate. NULL means authored directly — or, since CHRN-43, '
  'written by resolving a discussion, where tier2.discussions.resolved_note_id '
  'is the provenance.';

-- ============================================================================
-- THE TIER BOUNDARY.
-- ============================================================================
--
-- Redundant, and stated anyway as documentation of intent at the point the
-- tier boundary is defined, per the pattern 0002 established and 0003 words.
--
-- WHAT MAKES IT REDUNDANT IS NARROWER THAN "0001 REVOKED THE SCHEMA", and the
-- difference matters because 0007:52 re-granted USAGE on schema tier2. The
-- fact that survives 0007: no GRANT on any of these three tables was ever
-- issued, and 0007 deliberately added no ALTER DEFAULT PRIVILEGES on schema
-- tier2 — so a tier-2 table created today is unreachable by chronicle_tier1 on
-- table privileges alone, whatever it holds on the schema.
--
-- NOTHING HERE GRANTS ANYTHING, and it is written down so that a later GRANT
-- shows up as a diff against something rather than appearing from nothing.
-- 0007:53 remains the only tier-2 table grant in any migration, and per
-- CHRN-91 a GRANT on a tier-2 discussion table is an open finding under
-- REVIEW.md §1 rather than an expected step.
REVOKE ALL ON tier2.discussions, tier2.discussion_turns,
              tier2.discussion_participants FROM chronicle_tier1;
REVOKE ALL ON SEQUENCE tier2.discussion_number_seq FROM chronicle_tier1;

-- ============================================================================
-- FOR THE HTTP SURFACE THAT DOES NOT EXIST YET.
-- ============================================================================
--
-- There is no /discussions route in internal/api and this ticket deliberately
-- did not add one: E5 shipped store-only, CHRN-39 put the notes HTTP surface
-- explicitly out of scope, and CHRN-99 (E7.5) owns discussions over HTTP.
--
-- E6's exit — "a human and an agent can hold a threaded exchange" — is
-- therefore asserted THERE and not here, against two real sessions, because no
-- layer inside E6 can demonstrate it. Recorded so the gap reads as a decision
-- rather than as something E6 forgot.
