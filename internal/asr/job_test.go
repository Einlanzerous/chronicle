package asr

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
)

// A replayed key returns the ORIGINAL job. Done-when 4, first half.
func TestSubmitReplayReturnsTheSameJob(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	first, created, err := s.Submit(ctx, submitInput("chronicle", "key-aaaaaaaaaaaaaaaa", "a"))
	if err != nil || !created {
		t.Fatalf("first submit: created=%v err=%v", created, err)
	}

	again, created, err := s.Submit(ctx, submitInput("chronicle", "key-aaaaaaaaaaaaaaaa", "a"))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if created {
		t.Fatal("a replay reported itself as a new job; the GPU would transcribe the memo twice")
	}
	if again.ID != first.ID {
		t.Fatalf("replay returned job %s, want the original %s", again.ID, first.ID)
	}
}

// A mismatched key is refused. Done-when 4, second half.
//
// Both halves of the mismatch are checked — different audio and different
// model — because §3 rejects the content hash AND (hash, model) as keys, and a
// service that only compared one of them would accept the case those
// paragraphs are about.
func TestSubmitMismatchedKeyIsRefused(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	if _, _, err := s.Submit(ctx, submitInput("chronicle", "key-bbbbbbbbbbbbbbbb", "b")); err != nil {
		t.Fatalf("first submit: %v", err)
	}

	differentAudio := submitInput("chronicle", "key-bbbbbbbbbbbbbbbb", "DIFFERENT")
	if _, _, err := s.Submit(ctx, differentAudio); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("same key, different audio: got %v, want ErrKeyMismatch", err)
	}

	differentModel := submitInput("chronicle", "key-bbbbbbbbbbbbbbbb", "b")
	differentModel.Model = "medium.en"
	if _, _, err := s.Submit(ctx, differentModel); !errors.Is(err, ErrKeyMismatch) {
		t.Fatalf("same key, different model: got %v, want ErrKeyMismatch", err)
	}
}

// N CONCURRENT SUBMITS SHARING ONE KEY PRODUCE ONE JOB AND NO ERROR.
//
// Its own test, and not folded into the replay case, because CHRN-18 found
// exactly this: a check-then-insert design "fails against a design that passes"
// the plain sequential test. The race is between two retries of one attempt,
// which is precisely the situation the key exists for.
func TestConcurrentSubmitsWithOneKeyProduceOneJob(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	const n = 12
	ids := make([]uuid.UUID, n)
	errs := make([]error, n)
	createdCount := make([]bool, n)

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait() // release them together, or this is a sequential test
			job, created, err := s.Submit(ctx, submitInput("chronicle", "key-cccccccccccccccc", "c"))
			ids[i], createdCount[i], errs[i] = job.ID, created, err
		}(i)
	}
	start.Done()
	done.Wait()

	creates := 0
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("submit %d: %v — concurrent retries of one attempt must not error", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("submit %d produced job %s, but submit 0 produced %s", i, ids[i], ids[0])
		}
		if createdCount[i] {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("%d of %d submits reported creating the job; want exactly 1", creates, n)
	}

	var rows int
	if err := s.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("%d job rows in the table; want 1", rows)
	}
}

// The key is scoped to the client, so one client cannot deny another a job by
// choosing its string.
func TestIdempotencyKeysAreScopedToTheClient(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	a, _, err := s.Submit(ctx, submitInput("chronicle", "key-dddddddddddddddd", "d"))
	if err != nil {
		t.Fatal(err)
	}
	b, created, err := s.Submit(ctx, submitInput("catenary", "key-dddddddddddddddd", "d"))
	if err != nil {
		t.Fatalf("second client, same key: %v", err)
	}
	if !created || a.ID == b.ID {
		t.Fatal("one client's key collided with another's")
	}
}

// A job is only ever visible to the client that submitted it, and the answer
// for someone else's job is NOT FOUND rather than forbidden.
func TestAnotherClientsJobIsNotFound(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-eeeeeeeeeeeeeeee", "e"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "catenary", job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound — a 403 would confirm the id exists", err)
	}
}

// A result cannot be fetched before the job is terminal, and the three failure
// modes stay distinguishable.
func TestResultBeforeTerminalIsNotTerminal(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-ffffffffffffffff", "f"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Result(ctx, "chronicle", job.ID); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("got %v, want ErrNotTerminal", err)
	}
}

// A PURGED RESULT IS GONE, NOT MISSING. The job row survives the purge, which
// is what lets a late fetch answer 410 rather than 404 — "result expired" is
// not "transcription failed", and a client that cannot tell them apart will
// treat an aged-out transcript as a failure and mark the memo broken.
func TestPurgedResultIsDistinctFromAMissingJob(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := runToSuccess(t, s, "key-gggggggggggggggg", "g")

	if _, err := s.Result(ctx, "chronicle", job.ID); err != nil {
		t.Fatalf("result before purge: %v", err)
	}

	// Age the payload out rather than waiting for the TTL.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE jobs SET result_purge_at = now() - interval '1 hour' WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}
	n, err := s.PurgeResults(ctx)
	if err != nil || n != 1 {
		t.Fatalf("purge: n=%d err=%v", n, err)
	}

	if _, err := s.Result(ctx, "chronicle", job.ID); !errors.Is(err, ErrResultPurged) {
		t.Fatalf("got %v, want ErrResultPurged", err)
	}
	// And the row is still there, which is the half that makes the 410 honest.
	if _, err := s.Get(ctx, "chronicle", job.ID); err != nil {
		t.Fatalf("the job row did not survive its result purge: %v", err)
	}
}

// EMPTY TEXT WITH A COMPLETED RUN IS A SUCCEEDED, NON-PARTIAL RESULT.
//
// This is §5's ruling, and it is the one CHRN-27 will get wrong: `if text == ""
// { return }` is an innocent-looking line that silently inverts it in the safe
// direction, stranding the audio of exactly the memos that should prune. The
// assertion is here, at the service boundary, because that is where the fact
// originates.
func TestEmptyTranscriptIsSucceededAndNotPartial(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-hhhhhhhhhhhhhhhh", "h"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx, "worker-1", testLease)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, claimed.ID, "worker-1", testLease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(ctx, job.ID, "worker-1", asrclient.Result{
		JobId: job.ID, Status: asrclient.ResultStatusSucceeded, Partial: false,
		Text: "", Segments: []asrclient.Segment{}, Model: "whisper.cpp/small.en", Backend: "test-backend",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSucceeded {
		t.Fatalf("status %q, want succeeded", got.Status)
	}
	if got.Partial == nil || *got.Partial {
		t.Fatal("a completed run that produced no text was recorded as partial; " +
			"forty seconds of silence has a true and complete answer, and the answer is \"no speech\"")
	}

	res, err := s.Result(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "" || res.Segments == nil {
		t.Fatalf("result text=%q segments=%v; want empty text and a non-nil segment list", res.Text, res.Segments)
	}
}

// A TERMINAL JOB HOLDS NO AUDIO. The constraint, not the caller, is what makes
// this true on every path there will ever be.
func TestTerminalJobReleasesItsAudio(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := runToSuccess(t, s, "key-iiiiiiiiiiiiiiii", "i")

	var audio []byte
	if err := s.Pool().QueryRow(ctx, `SELECT audio FROM jobs WHERE id = $1`, job.ID).Scan(&audio); err != nil {
		t.Fatal(err)
	}
	if audio != nil {
		t.Fatal("a terminal job still holds its submitted bytes")
	}

	// And the constraint refuses the other direction, which is what stops a
	// future caller reintroducing them.
	_, err := s.Pool().Exec(ctx, `UPDATE jobs SET audio = '\x00'::bytea WHERE id = $1`, job.ID)
	if err == nil {
		t.Fatal("a terminal job accepted audio being written back into it")
	}
}

// Cancelling an already-terminal job is a NO-OP, not an error. A client that
// crashed after cancelling and retries must not be told off for having
// succeeded.
func TestCancellingATerminalJobIsANoOp(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := runToSuccess(t, s, "key-jjjjjjjjjjjjjjjj", "j")

	after, err := s.Cancel(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatalf("cancelling a succeeded job: %v", err)
	}
	if after.Status != StatusSucceeded {
		t.Fatalf("status %q after cancelling a succeeded job; want it left alone", after.Status)
	}
	if after.CancelRequestedAt != nil {
		t.Fatal("a cancellation was stamped onto a job that had already succeeded")
	}
}

// The wire status `cancelling` is derived and never stored.
func TestCancellingIsDerivedNotStored(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-kkkkkkkkkkkkkkkk", "k"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "worker-1", testLease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, job.ID, "worker-1", testLease); err != nil {
		t.Fatal(err)
	}

	after, err := s.Cancel(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusRunning {
		t.Fatalf("stored status is %q; cancellation must not be a stored state", after.Status)
	}
	if after.WireStatus() != asrclient.JobStatusCancelling {
		t.Fatalf("wire status is %q, want cancelling", after.WireStatus())
	}

	var stored string
	if err := s.Pool().QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, job.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "cancelling" {
		t.Fatal("`cancelling` was written to the database; it is a derived wire status only")
	}
}

// A claim is a compare-and-swap: only one worker can win it. CHRN-18's review
// measured six of six workers winning the same claim once the `from` predicate
// was dropped, so this is a regression test for a mistake already made once in
// this estate.
func TestOneWorkerWinsEachClaim(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	if _, _, err := s.Submit(ctx, submitInput("chronicle", "key-llllllllllllllll", "l")); err != nil {
		t.Fatal(err)
	}

	const workers = 6
	won := make([]bool, workers)
	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			_, err := s.Claim(ctx, "worker-"+strings.Repeat("x", i+1), testLease)
			won[i] = err == nil
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	for _, w := range won {
		if w {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d workers won the same claim; want exactly 1 — "+
			"more than one means the GPU runs two inferences at once", winners, workers)
	}
}

// A job may not be created in any state but queued, and the terminal states are
// terminal. Asserted against the DATABASE rather than through Go, because Go is
// not the only thing that will ever hold a connection to it.
func TestTheDatabaseRefusesIllegalTransitions(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := runToSuccess(t, s, "key-mmmmmmmmmmmmmmmm", "m")

	for _, to := range []string{"queued", "leased", "running", "failed"} {
		if _, err := s.Pool().Exec(ctx,
			`UPDATE jobs SET status = $2 WHERE id = $1`, job.ID, to); err == nil {
			t.Fatalf("a succeeded job was moved to %q; terminal must mean terminal", to)
		}
	}
}

const testLease = 30 * 1e9 // 30s, as a time.Duration

// runToSuccess drives one job all the way through, for tests that need a
// terminal job rather than a particular path to one.
func runToSuccess(t *testing.T, s *Store, key, seed string) Job {
	t.Helper()
	ctx := context.Background()

	job, _, err := s.Submit(ctx, submitInput("chronicle", key, seed))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "worker-1", testLease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(ctx, job.ID, "worker-1", testLease); err != nil {
		t.Fatal(err)
	}
	done, err := s.Finish(ctx, job.ID, "worker-1", asrclient.Result{
		JobId: job.ID, Status: asrclient.ResultStatusSucceeded, Partial: false,
		Text: "hello there", Segments: []asrclient.Segment{{StartMs: 0, EndMs: 1500, Text: "hello there"}},
		Model: "whisper.cpp/small.en", Backend: "test-backend",
	})
	if err != nil {
		t.Fatal(err)
	}
	return done
}

// CHRN-18's Done-when #7, the half that lands in this service.
//
// The full clause was *"that E3's worker skips ASR when a durable transcript
// already exists"*, deferred to E3 as needing E3's worker and E3's table. §5's
// revision then split it in two by moving the durable-transcript row to
// CHRONICLE's side: the pruner cannot read job rows, because they live in
// another database and their payloads purge at seven days.
//
// So the Chronicle-side half — do not submit when tier2.transcripts already
// holds a non-partial row for this memo — is CHRN-27's, and §5 binds it there.
// What this service owes is the half it can actually state: A JOB THAT HAS
// ALREADY PRODUCED A TRANSCRIPT IS NEVER RUN AGAIN. Nothing may re-claim it,
// and the transitions that would allow it are refused by the database rather
// than merely avoided by the worker.
func TestAJobThatAlreadyHasATranscriptIsNeverRunAgain(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	job := runToSuccess(t, s, "key-nevertwicenevert", "nt")

	if _, err := s.Claim(ctx, "worker-2", testLease); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a job with a transcript was claimable: %v", err)
	}
	if _, err := s.Pool().Exec(ctx,
		`UPDATE jobs SET status = 'queued' WHERE id = $1`, job.ID); err == nil {
		t.Fatal("a succeeded job was put back in the queue; its transcript would be " +
			"overwritten by a second run of the same audio")
	}

	// And the same audio, re-submitted under the same key, replays rather than
	// queueing a second run — which is the other way the GPU would end up
	// transcribing one memo twice.
	replay, created, err := s.Submit(ctx, submitInput("chronicle", "key-nevertwicenevert", "nt"))
	if err != nil {
		t.Fatal(err)
	}
	if created || replay.ID != job.ID {
		t.Fatal("re-submitting a finished attempt queued a second job")
	}
}
