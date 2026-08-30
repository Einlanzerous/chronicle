-- 0007_proposals — CHRN-32. The proposal, and the grant that lets a tier-1
-- process read the corpus it is supposed to derive from.
--
-- Two parts. The first is a correction to 0001 and is the one to read slowly.
--
-- ============================================================================
-- PART 1 · chronicle_tier1 gains SELECT on two tier-2 tables.
-- ============================================================================
--
-- 0001 wrote `REVOKE ALL ON SCHEMA tier2 FROM chronicle_tier1` under a comment
-- saying the role "cannot see tier 2 at all". That is STRICTER THAN THE
-- DOCTRINE IT WAS ENFORCING, and the difference is not academic: it makes the
-- second half of tier 1's own definition unimplementable.
--
-- CLAUDE.md defines tier 1 as the estate's account of what exists "plus
-- whatever Chronicle derives from its own corpus: Scribe proposals, extracted
-- entities, search indexes". All three of those derive FROM TIER 2. You cannot
-- derive from a corpus you cannot read. CHRN-32's Scribe is the first to hit
-- it — its input is tier2.transcripts joined to tier2.memos — and CHRN-41's
-- index over "notes and transcripts" cannot be built at all without this.
--
-- The invariant itself is untouched, because the invariant is about WRITES:
--
--     "a test proving no tier-1 write path can reach a tier-2 table is the
--      proof"                                              — CLAUDE.md
--
-- SELECT is not a write path. What changes is 0001's comment, not 0001's
-- guarantee. CHRN-52 is still the test, and it now has a sharper subject: not
-- "the role sees nothing" but "the role reads two tables and writes none".
--
-- Decided as ruling R4 of docs/decisions/chrn-32-proposal-contract.md §1.1,
-- accepted by magos 2026-08-30. Deliberately a new migration rather than an
-- edit to 0001: the grant moved, and the record should say when and why.
--
-- THREE THINGS THIS DOES NOT DO, each on purpose:
--
--   * No ALTER DEFAULT PRIVILEGES. A tier-2 table added tomorrow is unreadable
--     until somebody grants it by name, which is one deliberate act rather
--     than a widening that happens by existing. Contrast 0001's tier-1 grant,
--     which IS a default privilege, because owning tier 1 outright is the
--     point of the role.
--   * No INSERT, UPDATE or DELETE. Ever. This is the line.
--   * Nothing for tier2.users or tier2.user_tokens, which 0002 revokes by name
--     as the two tables whose exposure would matter most. They stay revoked.
--
-- A later widening from SELECT to INSERT does not pass unseen: pg_dump emits
-- non-default ACLs, so a loosened GRANT lands in schema.sql and CHRN-77's
-- staleness guard fails on the diff. (That is CHRN-78's correction to 0002's
-- comment, and it is what makes a narrow deliberate grant safe rather than a
-- hole.)

GRANT USAGE ON SCHEMA tier2 TO chronicle_tier1;
GRANT SELECT ON tier2.memos, tier2.transcripts TO chronicle_tier1;

-- ============================================================================
-- PART 2 · tier1.memo_proposals — what Scribe says a memo should become.
-- ============================================================================
--
-- TIER 1, and CLAUDE.md says so by name. The test holds: delete every row in
-- this table, re-run Scribe over the transcripts, and you have them back.
-- Nothing a person authored is lost. Contrast tier2.transcripts, which after a
-- thirty-day prune is the only remaining account of what was said.
--
-- The consequence is the line CHRN-52's test is drawn along, and it is worth
-- stating where the table is defined rather than only in the decision:
--
--     The proposal is a tier-1 write. The acceptance is a tier-2 write.
--
-- Scribe fills this table all day and never marks a memo `triaged`. That
-- transition is tier2.memos' and is driven by a person. A router that could
-- route AND commit would be a tier-1 process authoring tier-2 state, which is
-- the fabrication the tier split exists to prevent.

CREATE TABLE IF NOT EXISTS tier1.memo_proposals (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- NO FOREIGN KEY into tier2.memos, and not an oversight: 0004 established
    -- that a tier-1 table referencing tier 2 would be the cross-schema path
    -- the doctrine forbids, 0005 repeated it and 0006 said it again on
    -- tier1.memo_jobs. The consequence is the same one memo_jobs carries — a
    -- row can outlive its memo — and the same answer applies: a sweep
    -- collects the orphans. Part 1 above grants SELECT, which makes the
    -- reference readable; it does not make it enforceable, and should not.
    memo_id            UUID NOT NULL,

    -- WHICH TRANSCRIPT THIS WAS DERIVED FROM. Also no foreign key.
    --
    -- The third input, and the one the decision's first draft missed.
    -- tier2.transcripts is unique on (memo_id, model), so a memo can carry
    -- several, and `chronicle retranscribe` makes that ordinary rather than
    -- hypothetical. Without this column a re-run under an unchanged proposer
    -- produces a different answer for a reason nothing records, and CHRN-36
    -- reads it as nondeterminism when the input text simply changed.
    transcript_id      UUID NOT NULL,

    -- RUNNER-QUALIFIED, on tier2.transcripts.model's pattern — that column
    -- holds `whisper.cpp/small.en` and not `small.en`, because a bare model
    -- name says nothing about what ran it, and CHRN-22's floor had to be
    -- two-axis for exactly that reason.
    --
    -- Here it is three parts, `runner/model@promptversion`:
    --
    --     ollama/gemma4:31b@v1
    --
    -- The PROMPT VERSION is not decoration. A proposal is the output of a
    -- model AND a prompt, and a prompt revision changes the answer as surely
    -- as a model swap does. Without it CHRN-36 cannot tell a prompt
    -- regression from a model regression, which is the one comparison the
    -- eval set exists to make.
    --
    -- The CHECK is the same defence DurableClause relies on: an unqualified
    -- string is rejected here rather than silently becoming a proposer nobody
    -- can attribute.
    proposer           TEXT NOT NULL CHECK (proposer ~ '^[^/@]+/[^/@]+@[^/@]+$'),

    -- Bumped on EVERY PAYLOAD MUTATION, not on every Scribe run.
    --
    -- A supersede is not the only door. Stage 2 at acceptance can clear
    -- project_key when a project was archived mid-week — a payload change with
    -- no run behind it — and a counter that only tracked runs would sit still
    -- while the bytes moved. An accept echoes this value and a mismatch is
    -- re-shown rather than accepted, which is what stops an operator
    -- committing a proposal they never saw.
    generation         INTEGER NOT NULL DEFAULT 1 CHECK (generation >= 1),

    status             TEXT NOT NULL DEFAULT 'valid'
                         CHECK (status IN ('valid', 'needs_input', 'invalid')),

    payload            JSONB,
    superseded_payload JSONB,

    -- What stage 2 removed, and why. Never a silent drop: an advisory
    -- nearest_page that named a page nobody has ever created is cleared
    -- WITHOUT changing the status, and the clearing is recorded here so
    -- CHRN-36 can report a hallucination rate per proposer. A failure with no
    -- trace is a fact about the prompt that nothing can measure.
    cleared_fields     JSONB,

    error              TEXT,

    -- ALWAYS THE TEXT `payload` WAS PARSED FROM. The pairing is the point: a
    -- failed re-run keeps the payload that validated (see the status CHECK
    -- below) and must not overwrite the output that produced it, or CHRN-36
    -- diffs attempt N's junk against attempt N-1's proposal.
    raw_output         TEXT,

    -- Where a failed re-run's output goes instead. Kept because it is the only
    -- evidence of why a prompt stopped working, and it is tier 1 and cheap.
    last_attempt_raw   TEXT,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- `invalid` MEANS EXACTLY ONE THING: no run has ever produced a valid
    -- proposal for this memo under this proposer.
    --
    -- It cannot mean "the last attempt failed", because a failed re-run keeps
    -- the earlier payload and its status and records the failure beside it —
    -- otherwise a prompt regression on Tuesday would cost the operator a
    -- perfectly good proposal from Monday.
    CONSTRAINT proposals_invalid_iff_no_payload
        CHECK ((status = 'invalid') = (payload IS NULL)),

    -- The pairing above, as a constraint rather than a convention.
    CONSTRAINT proposals_raw_output_pairs_with_payload
        CHECK ((payload IS NULL) = (raw_output IS NULL))
);

-- Identity: one proposal per memo per proposer.
--
-- transcript_id is deliberately NOT in the key. It is recorded so a difference
-- can be attributed, but keying on it would let two transcripts of one memo
-- each carry their own proposal — which is a triage screen showing the same
-- memo twice, and an operator routing it twice.
CREATE UNIQUE INDEX IF NOT EXISTS memo_proposals_memo_proposer
    ON tier1.memo_proposals (memo_id, proposer);

-- The batch read: everything for a set of memos, newest first.
CREATE INDEX IF NOT EXISTS memo_proposals_memo
    ON tier1.memo_proposals (memo_id);

-- The triage screen's own filter — what still needs a person.
CREATE INDEX IF NOT EXISTS memo_proposals_needs_attention
    ON tier1.memo_proposals (status, updated_at DESC)
    WHERE status <> 'valid';

CREATE OR REPLACE FUNCTION tier1.memo_proposals_guard() RETURNS trigger
LANGUAGE plpgsql AS $fn$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- On tier2.transcripts' pattern, which refuses to re-attribute a row
        -- to another memo or model. Same reason: the identity is what makes
        -- the row mean anything, and a re-attributed proposal is a proposal
        -- credited to a model that never said it.
        IF NEW.memo_id  IS DISTINCT FROM OLD.memo_id
        OR NEW.proposer IS DISTINCT FROM OLD.proposer THEN
            RAISE EXCEPTION 'a proposal may not be re-attributed to another memo or proposer'
                USING ERRCODE = 'CH010';
        END IF;

        -- MONOTONE, like CHRN-22's audio_pruned_at. A generation that can go
        -- backwards is a generation an accept can be replayed against, which
        -- is the drift this column exists to close.
        IF NEW.generation < OLD.generation THEN
            RAISE EXCEPTION 'proposal generation may not decrease (% -> %)',
                OLD.generation, NEW.generation
                USING ERRCODE = 'CH011';
        END IF;

        NEW.updated_at := now();
    END IF;
    RETURN NEW;
END
$fn$;

CREATE OR REPLACE TRIGGER memo_proposals_guard
    BEFORE UPDATE ON tier1.memo_proposals
    FOR EACH ROW EXECUTE FUNCTION tier1.memo_proposals_guard();

COMMENT ON TABLE tier1.memo_proposals IS
  'Derived from tier2.transcripts by Scribe. Regenerable: delete and re-run. Never hand-edited.';
