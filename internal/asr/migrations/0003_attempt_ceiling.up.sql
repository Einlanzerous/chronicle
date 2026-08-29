-- 0003_attempt_ceiling — CHRN-28. Why an attempt ended, so the ceiling can
-- price the reasons differently.
--
-- CHRN-25 built `attempts` and left the policy open; CHRN-26 handed this ticket
-- six reasons a job goes back to the queue and pointed out that the counter
-- cannot tell them apart while they cost very differently. A crash is one
-- wasted claim. A deadline breach is five times the expected run — up to 200 s
-- of a stalled queue per attempt on `small.en`, eleven minutes on `large-v3`.
-- A decode breach costs no GPU at all. One ceiling over all of them is either
-- too generous for the expensive ones or too mean for the cheap ones.
--
-- ADDITIVE, AND IT CHANGES NOTHING THAT EXISTS. CHRN-26 §6's test for a change
-- to this table is whether an existing caller's behaviour changes: this column
-- is nullable, written by the two paths that already increment `attempts`, and
-- read by nothing that ran before it. The states, the column semantics and the
-- idempotency uniqueness are untouched.

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS last_release_reason TEXT;

COMMENT ON COLUMN jobs.last_release_reason IS
  'CHRN-28. Why this job last went back to the queue: the reaper''s '
  '`lease_expired`, or the worker''s reason from Store.Release. NULL on a job '
  'that has never been released, and unchanged by a cancellation, which is not '
  'a release. DIAGNOSTIC, not read by any query: the ceiling prices the reason '
  'in Go, at the call site that knows it (asr.CeilingFor). This is what an '
  'operator reads when asking why a job dead-lettered, after the log line has '
  'rotated away.';
