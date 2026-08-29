package asr

import (
	"context"
	"errors"
	"testing"
	"time"
)

// CHRN-26 §5 and §8 at the store: the claim's ordering, and the release a dying
// child needs. No process and no GPU — the ordering is a property of one SQL
// statement, and it is worth testing where it is written.

const (
	testMaxWaitNever = time.Hour        // residency wins: nothing has starved
	testMaxWaitNow   = time.Millisecond // everything queued has starved
)

func submitFor(t *testing.T, s *Store, client, key, seed, model string) Job {
	t.Helper()
	in := submitInput(client, key, seed)
	in.Model = model
	job, _, err := s.Submit(context.Background(), in)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return job
}

// claimAndStart is one worker's turn: claim, then move to running.
//
// STARTING IS WHAT MAKES THE ROUND-ROBIN TURN. Fairness reads
// max(started_at) per client, so a test that claimed without starting would
// measure a queue where nobody has ever been served.
func claimAndStart(t *testing.T, s *Store, worker, resident string, maxWait time.Duration) (Job, bool) {
	t.Helper()
	ctx := context.Background()
	job, err := s.ClaimForModel(ctx, worker, testLease, resident, maxWait)
	if errors.Is(err, ErrNotFound) {
		return Job{}, false
	}
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := s.Start(ctx, job.ID, worker, testLease); err != nil {
		t.Fatalf("start: %v", err)
	}
	return job, true
}

// A BACKLOG ON ONE CLIENT DOES NOT STARVE THE OTHER — the ticket's own
// Done-when, at the level the rule is written.
//
// Catenary submits eight; Chronicle submits one, LAST. Under a global FIFO the
// memo somebody is waiting on would be ninth, and the service would look
// perfectly healthy the whole time. Round-robin puts it second.
func TestABacklogDoesNotStarveTheQuietClient(t *testing.T) {
	s := testStore(t)

	for i, key := range []string{
		"backfill-0000000001", "backfill-0000000002", "backfill-0000000003",
		"backfill-0000000004", "backfill-0000000005", "backfill-0000000006",
		"backfill-0000000007", "backfill-0000000008",
	} {
		submitFor(t, s, "catenary", key, "cat"+string(rune('a'+i)), "small.en")
	}
	memo := submitFor(t, s, "chronicle", "memo-00000000000001", "memo", "small.en")

	var order []string
	for i := 0; i < 3; i++ {
		job, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever)
		if !ok {
			t.Fatalf("the queue ran dry after %d claims", i)
		}
		order = append(order, job.ClientID)
		if job.ID == memo.ID {
			if i > 1 {
				t.Fatalf("the memo was served %d claims in, behind a backfill: %v", i+1, order)
			}
			return
		}
	}
	t.Fatalf("the memo was never reached; claim order was %v", order)
}

// Fairness is BETWEEN clients, not within one: a client's own jobs stay
// oldest-first, because they have no ranking this service could justify
// inventing.
func TestWithinOneClientTheOrderIsOldestFirst(t *testing.T) {
	s := testStore(t)

	first := submitFor(t, s, "chronicle", "one-0000000000000001", "one", "small.en")
	second := submitFor(t, s, "chronicle", "two-0000000000000002", "two", "small.en")

	got, _ := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever)
	if got.ID != first.ID {
		t.Fatalf("claimed %s first; want the older job %s (the second is %s)", got.ID, first.ID, second.ID)
	}
}

// RESIDENCY WINS while nothing has starved: a job for the model already loaded
// is preferred over an older one that would cost a model switch. This is the
// common case and the one CHRN-24's numbers describe.
func TestTheResidentModelIsPreferred(t *testing.T) {
	s := testStore(t)

	other := submitFor(t, s, "chronicle", "medium-000000000001", "medium", "medium.en")
	mine := submitFor(t, s, "chronicle", "small-0000000000001", "small", "small.en")

	got, _ := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever)
	if got.ID != mine.ID {
		t.Fatalf("claimed %s (the %s job); want the resident-model job %s",
			got.ID, other.Model, mine.ID)
	}
}

// STARVATION BEATS RESIDENCY, and it is the only rule here with an unbounded
// downside: a steady trickle of small.en work would otherwise keep a medium.en
// job waiting forever.
//
// The same knob is the fairness bound under mixed models — a memo behind a
// backfill on another model waits this long plus one job — which is why
// CHRN-29 publishes it rather than treating it as internal tuning.
func TestAStarvedNonResidentModelForcesASwitch(t *testing.T) {
	s := testStore(t)

	starved := submitFor(t, s, "chronicle", "medium-000000000001", "medium", "medium.en")
	submitFor(t, s, "chronicle", "small-0000000000001", "small", "small.en")
	time.Sleep(5 * time.Millisecond) // past testMaxWaitNow

	got, _ := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNow)
	if got.ID != starved.ID {
		t.Fatalf("claimed %s; want the starved medium.en job %s — starvation beats residency",
			got.ID, starved.ID)
	}
}

// With NOTHING queued for the resident model, the claim falls through to plain
// round-robin and the worker switches to whatever wins. A worker that refused
// would sit idle beside a full queue.
func TestNothingQueuedForTheResidentModelStillClaims(t *testing.T) {
	s := testStore(t)

	want := submitFor(t, s, "chronicle", "medium-000000000001", "medium", "medium.en")

	got, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever)
	if !ok {
		t.Fatal("nothing was claimed; a queue full of another model is still a queue")
	}
	if got.ID != want.ID {
		t.Fatalf("claimed %s, want %s", got.ID, want.ID)
	}
}

// An empty resident model is startup: nothing is loaded yet, so nothing is
// preferred and the claim is plain round-robin.
func TestAnEmptyResidentModelClaimsAnything(t *testing.T) {
	s := testStore(t)
	want := submitFor(t, s, "chronicle", "small-0000000000001", "small", "small.en")

	got, ok := claimAndStart(t, s, "worker-1", "", testMaxWaitNever)
	if !ok || got.ID != want.ID {
		t.Fatalf("claimed %v ok=%v; want %s", got.ID, ok, want.ID)
	}
}

// A DYING CHILD RELEASES ITS JOB RATHER THAN FAILING IT. One crash must not
// permanently fail a memo that nothing was wrong with — and waiting for the
// lease instead costs the TTL plus a reap interval of idle GPU with a live
// worker sitting next to it.
func TestReleaseRequeuesWithAttemptsIncremented(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "release-00000000001", "release", "small.en")
	claimed, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever)
	if !ok || claimed.ID != job.ID {
		t.Fatal("setup: the job was not claimed")
	}

	out, err := s.Release(ctx, job.ID, "worker-1", "worker_crash")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if out.Status != StatusQueued {
		t.Fatalf("released to %q, want queued", out.Status)
	}
	if out.Attempts != 1 {
		t.Fatalf("attempts %d, want 1 — the counter is what CHRN-28 sets a ceiling against", out.Attempts)
	}
	if out.Reason != "worker_crash" {
		t.Fatalf("reason %q; a crash and a deadline breach cost differently and the counter cannot tell them apart", out.Reason)
	}

	after, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LeasedBy != "" || after.LeaseExpiresAt != nil || after.StartedAt != nil {
		t.Fatalf("a released job still carries its lease: %+v", after)
	}

	// And it is immediately claimable again, which is the entire point of not
	// waiting for the lease.
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("a released job was not claimable")
	}
}

// A CANCELLED JOB IS NEVER RELEASED BACK INTO THE QUEUE. A child that dies
// while running a job somebody cancelled is exactly that row, and requeueing it
// would re-run work that was explicitly stopped. The database refuses it
// (AS004); Release must not need the database to catch it.
func TestReleaseOfACancelledJobEndsCancelled(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "cancelrel-000000001", "cancelrel", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}
	if _, err := s.Cancel(ctx, "chronicle", job.ID); err != nil {
		t.Fatal(err)
	}

	out, err := s.Release(ctx, job.ID, "worker-1", "worker_crash")
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if out.Status != StatusCancelled {
		t.Fatalf("released to %q; a cancelled job may not return to the queue", out.Status)
	}

	res, err := s.Result(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatalf("a cancelled job carries no result, so a late fetch cannot tell it from a purge: %v", err)
	}
	if !res.Partial {
		t.Fatal("a cancelled job's result is not partial; no run completed")
	}
}

// Releasing a job this worker does not hold says nothing about it. The lease
// was lost, and whoever holds it now is entitled to finish it.
func TestReleaseNeedsTheLease(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := submitFor(t, s, "chronicle", "notmine-0000000001", "notmine", "small.en")
	if _, ok := claimAndStart(t, s, "worker-1", "small.en", testMaxWaitNever); !ok {
		t.Fatal("setup: the job was not claimed")
	}

	if _, err := s.Release(ctx, job.ID, "worker-2", "worker_crash"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("release by a worker that does not hold the lease returned %v", err)
	}
	after, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusRunning {
		t.Fatalf("status %q; the holder's job was moved by somebody else", after.Status)
	}
}
