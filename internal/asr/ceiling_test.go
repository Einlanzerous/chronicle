package asr

import (
	"context"
	"testing"
)

// CHRN-28's retry ceiling. CHRN-25 built `attempts` and left the policy open;
// CHRN-26 handed over six release reasons and the observation that the counter
// cannot tell them apart while they cost very differently.

func TestCeilingForPricesTheExpensiveReasonsLower(t *testing.T) {
	for _, reason := range []string{"inference_deadline", "decode_deadline"} {
		if got := CeilingFor(reason, 5, 2); got != 2 {
			t.Fatalf("%s got ceiling %d, want the wedged one (2)", reason, got)
		}
	}
	for _, reason := range []string{"worker_crash", "worker_io", "worker_down", "lease_lost", ""} {
		if got := CeilingFor(reason, 5, 2); got != 5 {
			t.Fatalf("%s got ceiling %d, want the ordinary one (5)", reason, got)
		}
	}
}

// expireLease pushes a held job's lease into the past so the reaper sees it.
func expireLease(t *testing.T, s *Store, id interface{ String() string }) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`UPDATE jobs SET lease_expires_at = now() - interval '1 minute' WHERE id = $1`,
		id.String()); err != nil {
		t.Fatal(err)
	}
}

// AT THE CEILING THE REAPER DEAD-LETTERS RATHER THAN REQUEUES. Without this the
// requeue path is an unmetered loop: a file that wedges the GPU comes back
// forever, and the only evidence is a busy card nobody ordered.
func TestTheReaperDeadLettersAtTheCeiling(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "ceiling-000000000001", "ceiling", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}
	expireLease(t, s, job.ID)

	// A ceiling of one, so the first lost claim is the last one.
	reaped, err := s.Reap(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reaped) != 1 || reaped[0].Status != StatusFailed {
		t.Fatalf("reaped %+v; want exactly one job, failed", reaped)
	}

	after, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusFailed {
		t.Fatalf("status %q; want failed", after.Status)
	}

	res, err := s.Result(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatalf("a dead-lettered job carries no result, so a client cannot tell why: %v", err)
	}
	if res.Failure == nil || res.Failure.Code != ExhaustedCode {
		t.Fatalf("failure = %+v; a client branches on the code, and CHRN-27's pump "+
			"must not answer this by starting another attempt", res.Failure)
	}
	if !res.Partial {
		t.Fatal("a dead-lettered job's result is not partial; no run completed")
	}

	// Terminal means the audio is gone — the jobs_audio_present constraint says
	// so, and a dead-letter that kept its bytes would break it.
	var audio []byte
	if err := s.Pool().QueryRow(ctx, `SELECT audio FROM jobs WHERE id = $1`, job.ID).Scan(&audio); err != nil {
		t.Fatal(err)
	}
	if len(audio) != 0 {
		t.Fatal("a terminal job still holds its audio")
	}
}

// Below the ceiling nothing changes: the job comes back with attempts
// incremented, which is CHRN-25's behaviour and must survive this ticket.
func TestBelowTheCeilingTheReaperStillRequeues(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "under-00000000000001", "under", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}
	expireLease(t, s, job.ID)

	reaped, err := s.Reap(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(reaped) != 1 || reaped[0].Status != StatusQueued || reaped[0].Attempts != 1 {
		t.Fatalf("reaped %+v; want one job back in the queue with attempts 1", reaped)
	}
	after, _ := s.Get(ctx, "chronicle", job.ID)
	if after.Status != StatusQueued {
		t.Fatalf("status %q; want queued", after.Status)
	}
}

// A WEDGED JOB GETS THE LOWER CEILING. Five times the expected run, spent
// getting nowhere, is not worth five attempts.
func TestReleaseDeadLettersAWedgedJobSooner(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "wedged-000000000001", "wedged", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}

	out, err := s.Release(ctx, job.ID, "worker-1", "inference_deadline",
		CeilingFor("inference_deadline", 5, 1))
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusFailed {
		t.Fatalf("released to %q; a wedged job at its ceiling is dead-lettered", out.Status)
	}

	// And the same job under the ordinary ceiling would have come back — which
	// is what makes the two numbers mean something.
	other := submitFor(t, s, "chronicle", "crashed-00000000001", "crashed", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the second job was not claimed")
	}
	again, err := s.Release(ctx, other.ID, "worker-1", "worker_crash",
		CeilingFor("worker_crash", 5, 1))
	if err != nil {
		t.Fatal(err)
	}
	if again.Status != StatusQueued {
		t.Fatalf("a crash at attempt 1 went to %q; the ordinary ceiling is five", again.Status)
	}
}

// CANCELLATION STILL WINS. A cancelled job at its ceiling goes to `cancelled`,
// not `failed` — the database refuses the queue for it (AS004) and the client
// asked for a cancellation, not a failure.
func TestACancelledJobIsNotDeadLettered(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "cancelceil-00000001", "cancelceil", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}
	if _, err := s.Cancel(ctx, "chronicle", job.ID); err != nil {
		t.Fatal(err)
	}

	out, err := s.Release(ctx, job.ID, "worker-1", "inference_deadline", 1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != StatusCancelled {
		t.Fatalf("released to %q; a cancellation outranks the ceiling", out.Status)
	}
}

// The reason is recorded, so an operator asking why a job dead-lettered has an
// answer that outlives the log line.
func TestTheReleaseReasonIsRecorded(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "reason-000000000001", "reason", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}
	if _, err := s.Release(ctx, job.ID, "worker-1", "decode_deadline", 5); err != nil {
		t.Fatal(err)
	}

	var reason *string
	if err := s.Pool().QueryRow(ctx,
		`SELECT last_release_reason FROM jobs WHERE id = $1`, job.ID).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason == nil || *reason != "decode_deadline" {
		t.Fatalf("last_release_reason = %v; the counter cannot tell the reasons apart, "+
			"so the column has to", reason)
	}
}
