-- 0002_claim_fairness — CHRN-26. What the round-robin claim reads.
--
-- CHRN-26 §5 orders the claim by "the most recent started_at among that
-- client's jobs, nulls first" so that a Catenary backfill of eight hundred
-- voice messages cannot put every Chronicle memo behind it. That is a
-- `max(started_at)` per client, and the jobs table is unbounded by design
-- (CHRN-25 §9: one row per attempt, kept) — so without this index every claim
-- is a sequential scan over every job the service has ever run.
--
-- ADDING AN INDEX IS ALLOWED HERE, and §6 of the decision says so explicitly,
-- because it would otherwise read as forbidden: what that section freezes is
-- what the job table MEANS — its states, its columns' semantics, the
-- idempotency uniqueness — not additions to it. Nothing below changes an
-- existing caller's behaviour.
--
-- The bookkeeping is in the query rather than in memory, and that was decided
-- rather than left open (§5 [rev 2]). One process claims today, because the
-- device advisory lock admits one; the day a second device is added, in-memory
-- last-served stops being round-robin at all — each worker alternates on its
-- own view and the pair can serve one client twice while the other waits.

CREATE INDEX IF NOT EXISTS jobs_client_started
    ON jobs (client_id, started_at DESC NULLS LAST);

COMMENT ON INDEX jobs_client_started IS
  'CHRN-26 §5. Backs max(started_at) per client for the round-robin claim.';
