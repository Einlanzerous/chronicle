package store

import (
	"errors"
	"testing"
)

// The tier-1 bookkeeping behind an attempt. What matters here is the ORDERING
// it makes possible — the key exists before the request does — and the
// uniqueness that stops one memo being submitted twice.

func TestOnlyOneAttemptPerMemoIsInFlight(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "inflight@example.test")

	first, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}

	// A slow poll and a fast sweep would otherwise submit the same memo
	// twice: a second GPU run, and two results for one memo.
	if _, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash); !errors.Is(err, ErrJobInFlight) {
		t.Fatalf("got %v, want ErrJobInFlight", err)
	}

	// Once the attempt is settled, a new one is allowed — that is the retry
	// path, and it takes a FRESH key because it is a different attempt.
	if err := s.RecordJobFailure(ctx, first.ID, "transcription_failed", "no"); err != nil {
		t.Fatal(err)
	}
	second, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash)
	if err != nil {
		t.Fatalf("a settled attempt blocked the retry: %v", err)
	}
	if second.IdempotencyKey == first.IdempotencyKey {
		t.Fatal("a deliberate re-transcription reused the first attempt's key. §3: the key " +
			"is stable across retries of ONE attempt and fresh for a new one")
	}
}

// An attempt exists before the submit does, and is listed for resumption
// until the submit lands. This is the state persist-before-send creates on
// purpose.
func TestAnUnsubmittedAttemptIsResumable(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "resume@example.test")

	job, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID != nil {
		t.Fatal("a job id existed before anything was submitted")
	}

	pending, err := s.UnsubmittedJobs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != job.ID {
		t.Fatalf("unsubmitted = %+v; the attempt has to be findable or the key buys nothing", pending)
	}
	if inFlight, err := s.InFlightJobs(ctx, 10); err != nil || len(inFlight) != 0 {
		t.Fatalf("in-flight = %+v err=%v; an unsent attempt is not in flight", inFlight, err)
	}

	// Once submitted it moves to the other list, and off the first.
	if _, err := s.RecordJobSubmitted(ctx, job.ID, m.ID); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.UnsubmittedJobs(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("unsubmitted = %+v err=%v", pending, err)
	}
	inFlight, err := s.InFlightJobs(ctx, 10)
	if err != nil || len(inFlight) != 1 {
		t.Fatalf("in-flight = %+v err=%v", inFlight, err)
	}

	// And collected takes it off both.
	if err := s.RecordJobCollected(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	if inFlight, err := s.InFlightJobs(ctx, 10); err != nil || len(inFlight) != 0 {
		t.Fatalf("a collected attempt is still in flight: %+v err=%v", inFlight, err)
	}
}

// A failure with no code would leave the memo un-actionable AND leave the
// in-flight index still holding it. Refused rather than defaulted.
func TestAFailureNeedsACode(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "nocode@example.test")
	job, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordJobFailure(ctx, job.ID, "", "something went wrong"); err == nil {
		t.Fatal("a failure with no code was accepted")
	}
}

// The tier-1 row holds no reference into tier 2, so a memo can be deleted out
// from under it. 0004 established that a foreign key here would be the
// cross-schema path the doctrine forbids.
func TestMemoJobsHoldNoForeignKeyIntoTier2(t *testing.T) {
	s, ctx := newTestStore(t)
	var count int
	if err := s.Pool().QueryRow(ctx, `
		SELECT count(*)
		  FROM information_schema.table_constraints tc
		  JOIN information_schema.constraint_column_usage ccu
		    ON tc.constraint_name = ccu.constraint_name
		 WHERE tc.table_schema = 'tier1'
		   AND tc.table_name = 'memo_jobs'
		   AND tc.constraint_type = 'FOREIGN KEY'
		   AND ccu.table_schema = 'tier2'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tier1.memo_jobs holds %d foreign key(s) into tier 2", count)
	}
}

// SUPERSEDING TOUCHES SETTLED ATTEMPTS ONLY.
//
// An in-flight attempt is left alone, because the partial unique index that
// keeps one attempt per memo would otherwise admit a second while the first is
// still running — a second GPU run over the same audio.
func TestSupersedingLeavesAnInFlightAttemptAlone(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "supersede@example.test")

	settled, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordJobFailure(ctx, settled.ID, "transcription_failed", "no"); err != nil {
		t.Fatal(err)
	}
	inFlight, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}

	spent, err := s.SupersedeMemoJobs(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if spent != 1 {
		t.Fatalf("superseded %d attempts; only the settled one should be", spent)
	}

	// The in-flight attempt still counts, and still blocks a second one.
	n, err := s.CountMemoJobs(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("countable attempts = %d, want 1 (the in-flight one)", n)
	}
	if _, err := s.BeginTranscription(ctx, m.ID, "small.en", m.ContentHash); !errors.Is(err, ErrJobInFlight) {
		t.Fatalf("got %v; superseding must not open a second attempt alongside a running one", err)
	}

	// And once THAT settles, superseding clears it too and the count is zero.
	if err := s.RecordJobCollected(ctx, inFlight.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SupersedeMemoJobs(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountMemoJobs(ctx, m.ID); err != nil || n != 0 {
		t.Fatalf("countable attempts = %d err=%v, want 0", n, err)
	}
}
