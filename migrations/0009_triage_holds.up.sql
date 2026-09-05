-- 0009_triage_holds — CHRN-34. Parking a routing decision without pretending
-- the recording changed.
--
-- ============================================================================
-- THIS MIGRATION DELIBERATELY DOES NOT TOUCH tier2.
-- ============================================================================
--
-- That is the ruling, not an accident of scope. CHRN-34 describes a HOLD that
-- "parks a memo in an inbox to decide later", and the obvious implementation is
-- to move the memo to `held`. It does not work, and 0003 is where you can read
-- why: from `held` the transition table permits exactly
--
--     'held>queued'   'held>discarded'
--
-- because `held` today means TRANSCRIPTION FAILED. It has one writer
-- (internal/transcribe) and `held>queued` is the retry. A memo parked from the
-- triage screen has a perfectly good transcript and wants to go back to the
-- triage screen — an edge that does not exist and, if added, would put two
-- unrelated populations in one inbox: memos with no transcript that need ASR
-- attention, and memos with a transcript that need a person's attention. The
-- ticket's own words are that the inbox must be "visible and countable, or it
-- becomes a place memos go to be forgotten", and merging those two is precisely
-- how that happens.
--
-- So: the memo stays `transcribed` and the deferral is recorded HERE. `held`
-- keeps its single meaning, tier2.memos_guard is untouched, and the half of
-- this ticket that would have been hard to undo was not done at all.
--
-- Ruled by magos 2026-09-04 (option B), on the stop-and-ask recorded against
-- CHRN-34 on 2026-09-01.
--
-- ============================================================================
-- WHY THIS IS TIER 1, WHICH IS THE ONE CLAIM HERE WORTH ARGUING WITH.
-- ============================================================================
--
-- 0008 put tier2.memo_links in tier 2 with a sentence that reads like it should
-- also apply here: "This table holds what a PERSON DECIDED, and nothing
-- regenerates that." A hold is also something a person did. So why is it not
-- beside it?
--
-- Because A HOLD IS THE ABSENCE OF A DECISION, NOT A DECISION. It records that
-- the operator has NOT yet said what this memo becomes. Delete every row in
-- this table and the memos reappear on the triage screen — which is exactly
-- where they were before anyone deferred them. Nothing is falsified, nothing
-- authored is lost, and the operator re-makes the deferral in one tap or
-- decides it properly instead.
--
-- Run the same test on tier2.memo_links and it fails immediately: delete those
-- rows and a Switchyard ticket exists that Chronicle no longer knows it
-- created. That asymmetry is the line, and it is the same one CHRN-33 drew:
--
--     The proposal is a tier-1 write. The acceptance is a tier-2 write.
--
-- A deferral is neither a proposal nor an acceptance, and it sits on the
-- tier-1 side of that line because losing it costs a second look at a card
-- rather than an account of something that happened in the world.

CREATE TABLE IF NOT EXISTS tier1.triage_holds (
    -- THE MEMO IS THE IDENTITY, so it is the key. tier2.memo_links carries a
    -- surrogate `id` with `UNIQUE (memo_id)` beside it because a link is a
    -- record of an attempt and wants an identity of its own to be referenced
    -- by. A hold is not a thing that happened; it is a flag on a memo, one at a
    -- time, and giving it a surrogate key would invite a second row.
    --
    -- NO FOREIGN KEY into tier2.memos, on the rule 0004 established and 0005,
    -- 0006, 0007 and 0008 each repeated: a tier-1 table referencing tier 2
    -- would be the cross-schema path the doctrine forbids. The consequence is
    -- the familiar one — a row can outlive its memo — and the answer is the
    -- familiar one too: releasing is idempotent, the listing joins through
    -- tier2.memos, and an orphan is therefore invisible rather than harmful.
    memo_id    UUID PRIMARY KEY,

    -- WHO DEFERRED IT. Also no foreign key, same rule: tier2.users is a tier-2
    -- table and 0002 revokes it by name from chronicle_tier1 as the one whose
    -- exposure would matter most.
    --
    -- Recorded because the listing is per-author on a shared deployment, and
    -- because "who has been sitting on this for three weeks" is a question the
    -- inbox exists to be able to answer.
    held_by    UUID NOT NULL,

    -- Optional, and optional on purpose. Most deferrals are "not now" and
    -- forcing a sentence would make the fast path slower than deciding badly,
    -- which is the failure the HOLD escape exists to prevent. When there IS a
    -- reason — "waiting to hear back", "needs the page tree" — it is the thing
    -- that makes the memo re-triageable weeks later.
    reason     TEXT,

    -- THE AGE IS THE PRODUCT. `Done when` asks for held memos "listed with an
    -- age", and this is what it is computed from. It is deliberately NOT
    -- touched by re-holding an already-held memo: a screen that could reset the
    -- clock would let a memo be deferred indefinitely while always looking
    -- fresh, which is the "place memos go to be forgotten" written as a bug.
    held_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The listing: oldest first, which is the order that makes an inbox honest.
CREATE INDEX IF NOT EXISTS triage_holds_age ON tier1.triage_holds (held_at);

COMMENT ON TABLE tier1.triage_holds IS
  'A routing decision deferred. TIER 1 because it is the ABSENCE of a decision: delete every row and the memos simply reappear on the triage screen. The memo itself stays `transcribed` — tier2.memos `held` means transcription failed and is a different thing (CHRN-34).';
