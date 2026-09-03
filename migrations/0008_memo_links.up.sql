-- 0008_memo_links — CHRN-33. Where derived state becomes authored state.
--
-- Everything before this table is tier 1 and disposable. tier1.memo_proposals
-- holds what a model SAID; delete every row and re-run Scribe and they come
-- back. This table holds what a PERSON DECIDED, and nothing regenerates that.
-- It is the first row Chronicle writes that a re-run cannot reconstruct, which
-- is the whole of why it is in tier 2.
--
-- The plan is CHRN-33 revision 4, approved 2026-09-03. Section names in these
-- comments — `schema`, `approach`, `tradeoffs` — refer to it.
--
-- ============================================================================
-- THE ROW IS A LOCK BEFORE IT IS A RECORD.
-- ============================================================================
--
-- `UNIQUE (memo_id)` is not a tidiness constraint. It is the only thing in the
-- entire path that stops one memo becoming two tickets, and it earns that job
-- because the alternative everyone reaches for first does not work:
--
--     Switchyard's Idempotency-Key header REPLAYS A RESPONSE. It does not
--     SERIALISE A SIDE EFFECT.
--
-- Its middleware (server/src/lib/idempotency.ts) looks the key up, `await
-- next()`s, and then inserts the cache row. There is no lock between the lookup
-- and the handler, and the insert's own catch says the quiet part out loud —
-- "Concurrent same-key request landed first — that's fine, the other one wrote
-- the cache entry." Fine for the cache. The handler has already run twice, so
-- two tickets exist. That is the ORDINARY retry, not an exotic race: a phone
-- that gives up before Chronicle's 15-second client timeout and re-sends is
-- exactly the case CHRN-33 exists to survive.
--
-- Only Chronicle can prevent it, and only with a unique row taken BEFORE the
-- outward call. That is what this table is.
--
-- A pending row — no ticket_key, no confirmed_at — IS NOT A LINK. Nothing
-- renders it, nothing resolves it, and invariant 2 therefore has no quarrel
-- with it: it is a claim on a memo, not a copy of a ticket.
--
-- ============================================================================
-- WHAT IS STORED, AND WHAT IS DELIBERATELY NOT.
-- ============================================================================
--
-- INVARIANT 2 — Switchyard is linked, never copied. The `sent_*` columns look
-- at first glance like the copy the invariant forbids. They are not, and the
-- distinction is exact:
--
--   * They are the provenance of CHRONICLE'S OWN OUTPUT — the bytes Chronicle
--     put on the wire. They are what the sweep re-sends after a crash, and what
--     an operator reads to answer "what did it actually file". Nothing upstream
--     owns them and nothing upstream can move them.
--   * `ticket_key` is a HANDLE, not state. It carries no title and no status,
--     and there is no column here that could hold one. The card the operator
--     sees resolves live from this key at render time.
--
-- THE ROW IS NEVER RENDERED AS A CARD. If a later ticket wants to show the
-- decision, it shows `sent_title` labelled as what Chronicle sent, beside a
-- live card resolved from `ticket_key`. Those are two different claims and must
-- never be merged into one.
--
-- ============================================================================
-- A REFUSAL MARKS. IT DOES NOT DELETE.
-- ============================================================================
--
-- The plan's revision 3 had a non-retryable outward error drop the pending row,
-- refuse, and leave the memo `transcribed` so the operator could decide again.
-- Revision 4 found that it destroys the one thing this table exists for, and
-- marking instead buys three things:
--
--   1. The triage screen can tell the operator WHY their earlier decision
--      evaporated, instead of the memo silently reappearing in the list with no
--      account of what happened to it.
--   2. The sweep has somewhere to record an outcome. It has no client to answer.
--   3. A corrected decision RE-ARMS THE SAME ROW under the same
--      `UNIQUE (memo_id)`, rather than racing an insert against a delete.
--
-- A refused row is not pending: `refused_at` is set, nothing holds its lock,
-- and the sweep skips it.
CREATE TABLE IF NOT EXISTS tier2.memo_links (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- THE LOCK. One decision per memo, enforced here rather than in Go,
    -- because two Chronicle processes behind one Traefik would each hold their
    -- own copy of any in-process mutex and neither would know about the other.
    --
    -- ON DELETE RESTRICT, on tier2.transcripts' pattern rather than
    -- tier2.memo_arrivals'. An arrival is a sighting and cascades away with
    -- what it sighted; a decision is authored, and a memo carrying one is not
    -- something to remove without a conversation.
    memo_id       UUID NOT NULL UNIQUE REFERENCES tier2.memos(id) ON DELETE RESTRICT,

    -- What the operator chose. The proposal's four destinations, and no fifth:
    -- HOLD is an operator action on the memo (CHRN-34) and never a landing.
    destination   TEXT NOT NULL
                    CHECK (destination IN ('NOTE', 'TICKET', 'DISCUSSION', 'DISCARD')),

    -- ------------------------------------------------------------------
    -- What Chronicle SENT. Provenance of its own output; see invariant 2 above.
    -- ------------------------------------------------------------------
    sent_project_key TEXT,
    sent_type        TEXT,
    sent_title       TEXT,
    sent_description TEXT,

    -- PER DECISION, GENERATED AT T1, RE-SENT BY THE SWEEP — and this column is
    -- the correction that revision 4 exists for.
    --
    -- The obvious key is `chronicle-memo-<uuid>`: stable across retries by
    -- construction, no storage needed. It is a trap, and the trap is not
    -- hypothetical. Switchyard caches every response BELOW 500 that has a JSON
    -- body (`if (status >= 500) return;`) and renders every error as JSON
    -- (server/src/errors.ts:156). So a 4xx is cached under whatever key it was
    -- sent with, for twenty-four hours.
    --
    -- With a memo-derived key that POISONS THE MEMO. A create refused because
    -- its project was archived caches the 404; the operator corrects the
    -- project and re-decides; the corrected decision derives the same key; the
    -- middleware replays the cached 404. Round and round for a day, with
    -- nothing saying why, and the corrected decision never reaching Switchyard
    -- at all.
    --
    -- A key that belongs to the DECISION does not have that problem. A retry of
    -- the same decision re-sends this stored value and replays; a new decision
    -- re-arms the row with a fresh one and gets through. It also takes the
    -- 24-hour TTL out of the correctness argument permanently, rather than
    -- leaving it to bound how long a refusal sticks.
    sent_idempotency_key TEXT NOT NULL CHECK (sent_idempotency_key <> ''),

    -- ------------------------------------------------------------------
    -- What came back.
    -- ------------------------------------------------------------------

    -- A HANDLE AND NOTHING ELSE. Null until confirmed, and null forever for
    -- every destination but TICKET.
    ticket_key    TEXT,
    confirmed_at  TIMESTAMPTZ,

    -- ------------------------------------------------------------------
    -- What the sweep found. Written on EVERY sweep of a row, including the
    -- passes that resolve nothing: `swept_at` with no candidates is how
    -- "looked, found nothing" is distinguished from "never looked", and the
    -- admin report needs both.
    -- ------------------------------------------------------------------
    swept_at       TIMESTAMPTZ,

    -- None, one, or AMBIGUOUS. More than one match confirms nothing and needs a
    -- person: picking one turns the other into an orphan ticket nobody will
    -- ever find, and there is no evidence available to the sweep that says
    -- which is which.
    candidate_keys TEXT[],

    -- A decision that will never land, and why. `refused_status` is the HTTP
    -- status when Switchyard refused; it is null for a refusal Chronicle
    -- reached on its own (a held memo, naming CHRN-34), which is why the reason
    -- and not the status is the required half.
    refused_at     TIMESTAMPTZ,
    refused_status INTEGER,
    refused_reason TEXT,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- A ticket key on a NOTE is a row whose destination and whose evidence
    -- disagree, and the disagreement would be invisible.
    CONSTRAINT memo_links_ticket_key_only_on_ticket
        CHECK (ticket_key IS NULL OR destination = 'TICKET'),

    -- Confirmed and refused are the two terminal outcomes and a row has one of
    -- them, never both. Without this a sweep bug could mark a row refused after
    -- a ticket had already been confirmed against it, and the operator would be
    -- shown a reason for a decision that in fact landed.
    CONSTRAINT memo_links_confirmed_xor_refused
        CHECK (confirmed_at IS NULL OR refused_at IS NULL),

    -- The reason is what the triage screen shows. A refusal with no reason is
    -- a memo that reappears with an explanation nobody can read.
    CONSTRAINT memo_links_refusal_states_why
        CHECK ((refused_at IS NULL) = (refused_reason IS NULL)),

    -- A confirmed TICKET without a key is a link to nothing, which looks like
    -- success and is worse than a failure.
    CONSTRAINT memo_links_confirmed_ticket_has_a_key
        CHECK (confirmed_at IS NULL OR destination <> 'TICKET' OR ticket_key IS NOT NULL)
);

-- The sweep's own query: rows that are neither confirmed nor refused, oldest
-- first. Partial, because the pending set is the small one and stays small —
-- every row in it is a decision that has not finished landing.
CREATE INDEX IF NOT EXISTS memo_links_unresolved
    ON tier2.memo_links (created_at)
    WHERE confirmed_at IS NULL AND refused_at IS NULL;

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

-- Documentation of intent, on the pattern 0002 established and 0003 and 0006
-- repeated. chronicle_tier1 holds no privilege here — 0007 granted it SELECT on
-- tier2.memos and tier2.transcripts BY NAME and deliberately used no ALTER
-- DEFAULT PRIVILEGES, precisely so that a tier-2 table added later is
-- unreadable until somebody grants it in one deliberate act.
--
-- This is the table that most needs that to hold. Scribe proposes; a person
-- accepts. A tier-1 process that could write here would be a router that routes
-- AND commits, which is the fabrication the whole tier split exists to prevent.
REVOKE ALL ON tier2.memo_links FROM chronicle_tier1;

COMMENT ON TABLE tier2.memo_links IS
  'What a PERSON decided a memo becomes. Authored, not derived: nothing regenerates it. UNIQUE (memo_id) is the lock that keeps one memo from becoming two tickets.';
