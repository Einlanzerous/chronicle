package asr

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// CHRN-26's Done-when, against a real child process and no GPU. The decision
// they check is in docs/decisions/chrn-26-resident-worker.md.
//
// Every one of these is a case the placeholder could not have: it started a
// process per job, so there was nothing to hold a model, nothing to hang, and
// nothing whose death had to be told apart from a bad recording.

func testJob(model string) TranscribeRequest {
	return TranscribeRequest{
		Audio:     []byte("pretend this is opus"),
		MediaType: "audio/ogg",
		Model:     model,
	}
}

// ONE PROCESS HOLDS THE MODEL ACROSS JOBS. Done-when 1.
//
// The whole ticket in one assertion: three jobs, one process start, no reloads.
// R3 measured the per-process tax at a fixed 388 ms, which is why this matters
// most for the memos there are most of — a five-second voice note is 5.6x
// slower per-invocation.
func TestTheModelStaysResidentAcrossJobs(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeOK)
	r := f.resident(t, discardLogger())
	startResident(t, r)

	for i := 0; i < 3; i++ {
		tr, err := r.Transcribe(context.Background(), testJob("small.en"))
		if err != nil {
			t.Fatalf("job %d: %v", i, err)
		}
		if tr.Text != "hello there" {
			t.Fatalf("job %d text %q", i, tr.Text)
		}
		// SECONDS TO MILLISECONDS. Done-when 9: verbose_json reports float
		// seconds where whisper-cli wrote integer milliseconds, and a corpus
		// timed a thousand times short would look entirely plausible.
		if len(tr.Segments) != 2 || tr.Segments[1].StartMs != 1500 || tr.Segments[1].EndMs != 2600 {
			t.Fatalf("job %d segments %+v", i, tr.Segments)
		}
		if tr.AudioDurationMs != 3000 {
			t.Fatalf("job %d duration %d ms, want 3000 from the decoded WAV", i, tr.AudioDurationMs)
		}
	}

	if n := f.countEvents(t, "start "); n != 1 {
		t.Fatalf("the resident process started %d times for three jobs; want 1 — "+
			"that is the entire point of this ticket:\n%v", n, f.events(t))
	}
	if n := f.countEvents(t, "load "); n != 0 {
		t.Fatalf("%d model reloads for three jobs of one model", n)
	}
	// response_format is NOT optional: the default is `json`, which returns
	// text and no segments, and CHRN-25's contract makes that shape valid.
	if n := f.countEvents(t, "infer verbose_json"); n != 3 {
		t.Fatalf("inferences did not all ask for verbose_json:\n%v", f.events(t))
	}
}

// EXACTLY ONE INFERENCE RUNS AT A TIME. Done-when 3, proved by instrumenting
// the worker rather than by inspection.
//
// The fake deliberately does NOT serialise — the real whisper-server holds
// whisper_mutex for the whole of /inference and would hide this — so an
// overlapping pair is recorded and fails the test. Four callers, because two
// can miss a race that four will not.
func TestExactlyOneInferenceRunsAtATime(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeSlow)
	if err := os.WriteFile(filepath.Join(f.Dir, "sleep_ms"), []byte("150"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := f.resident(t, discardLogger())
	startResident(t, r)

	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = r.Transcribe(context.Background(), testJob("small.en"))
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	for _, e := range f.events(t) {
		if e == "OVERLAP" {
			t.Fatalf("two inferences ran on the device at once:\n%v", f.events(t))
		}
	}
	if n := f.countEvents(t, "answered"); n != 4 {
		t.Fatalf("%d inferences answered, want 4", n)
	}
}

// A HUNG CHILD IS KILLED AND ITS JOB RELEASED. Done-when 5.
//
// This is the case no lease can see and the finding that changed the ticket: a
// whisper-server that stops answering WITHOUT EXITING leaves asrd alive and
// renewing, so the job lease never expires, the reaper never fires, and the
// semaphore and the advisory lock are both held. Every lease reports healthy
// and nothing moves. A wall clock is the only thing that can tell a long job
// from a stuck one.
func TestAHungChildIsKilledAndTheJobReleased(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeHang)
	r := f.resident(t, discardLogger())
	r.MinDeadline = time.Second
	startResident(t, r)

	_, err := r.Transcribe(context.Background(), testJob("small.en"))

	var release *ReleaseError
	if !errors.As(err, &release) {
		t.Fatalf("got %v (%T); want a ReleaseError — nothing was wrong with the job", err, err)
	}
	if release.Reason != "inference_deadline" {
		t.Fatalf("reason %q, want inference_deadline; CHRN-28 prices a breach at five times a crash", release.Reason)
	}

	// And the queue is moving again: the child is restarted, so the next job
	// does not meet the same wedged process.
	waitFor(t, "the resident process to come back", 30*time.Second, func() bool {
		return f.countEvents(t, "start ") >= 2 && r.State().Up
	})
}

// A DYING CHILD RELEASES ITS JOB RATHER THAN FAILING IT. Done-when 6.
//
// The placeholder turned a failed transcriber run into a `failed` job, so one
// child crash would have permanently failed a memo that nothing was wrong with.
// A transport error to a supervised child on loopback is the child dying, and
// never the audio's fault.
func TestACrashedChildReleasesRatherThanFailingTheJob(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeCrash)
	r := f.resident(t, discardLogger())
	startResident(t, r)

	_, err := r.Transcribe(context.Background(), testJob("small.en"))

	var release *ReleaseError
	if !errors.As(err, &release) {
		t.Fatalf("got %v (%T); want a ReleaseError. A FailureError here fails the memo forever", err, err)
	}
	if release.Reason != "worker_crash" {
		t.Fatalf("reason %q, want worker_crash", release.Reason)
	}
}

// A FAILED MODEL SWITCH DOES NOT BECOME A RESTART LOOP. Done-when 7.
//
// /load frees the old model BEFORE initialising the new one and calls exit(1)
// if that fails — upstream's own TODO — so a failed switch is not an error
// response, it is the resident process terminating. Restarting with the model
// that just failed reproduces the failure, and the supervisor's backoff then
// produces a restart loop rather than a service.
func TestAFailedModelSwitchComesBackOnTheLastKnownGoodModel(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeOK)
	r := f.resident(t, discardLogger())
	startResident(t, r)

	if _, err := r.Transcribe(context.Background(), testJob("small.en")); err != nil {
		t.Fatalf("setup job: %v", err)
	}

	f.setMode(t, fakeLoadFail)
	_, err := r.Transcribe(context.Background(), testJob("medium.en"))

	var failure *FailureError
	if !errors.As(err, &failure) {
		t.Fatalf("got %v (%T); a model that will not load is a deployment fault, "+
			"not a job to retry", err, err)
	}
	if failure.Code != "model_unloadable" {
		t.Fatalf("code %q, want model_unloadable", failure.Code)
	}

	waitFor(t, "the resident process to come back on small.en", 30*time.Second, func() bool {
		st := r.State()
		return st.Up && st.Model == "small.en"
	})

	// THE MODEL THAT FAILED IS REFUSED, NOT RETRIED. A second job naming it
	// must not reach /load at all — that is the difference between a service
	// and a crash loop.
	before := f.countEvents(t, "load-exit")
	if _, err := r.Transcribe(context.Background(), testJob("medium.en")); !errors.As(err, &failure) {
		t.Fatalf("the second job for an unloadable model got %v", err)
	}
	if after := f.countEvents(t, "load-exit"); after != before {
		t.Fatalf("the unloadable model was retried: %d load attempts became %d", before, after)
	}

	// And the resident model still works, which is what "comes back on
	// last-known-good" has to mean.
	if _, err := r.Transcribe(context.Background(), testJob("small.en")); err != nil {
		t.Fatalf("the surviving model stopped working after a failed switch: %v", err)
	}
}

// A CANCELLED RUNNING JOB ACTUALLY STOPS THE INFERENCE. Done-when 8.
//
// Against the placeholder, cancelling the context killed whisper-cli. Against a
// resident server it does not: /inference holds whisper_mutex for the whole
// synchronous call, so dropping the request leaves it running to completion
// holding the device, with every job behind it blocked on the mutex. Killing
// the child costs about 2.3 s and is the only thing that stops the work.
func TestCancellingAJobStopsTheResidentProcess(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeGate) // and the gate is never released
	r := f.resident(t, discardLogger())
	startResident(t, r)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := r.Transcribe(ctx, testJob("small.en"))
		done <- err
	}()

	waitFor(t, "the inference to reach the device", 30*time.Second, func() bool {
		return f.countEvents(t, "infer ") >= 1
	})
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("cancelling did not return; the inference was left running")
	}

	waitFor(t, "the resident process to be killed and restarted", 30*time.Second, func() bool {
		return f.countEvents(t, "start ") >= 2
	})
	if n := f.countEvents(t, "answered"); n != 0 {
		t.Fatalf("the cancelled inference ran to completion anyway (%d answers)", n)
	}
}

// A STANDBY HOLDS NO MODEL. §3 [rev 3], and it is why the lock is taken for the
// process's lifetime rather than per inference: a process that cannot get the
// device loads nothing, so it holds no VRAM either.
func TestAStandbySpawnsNothingUntilItOwnsTheDevice(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeOK)
	r := f.resident(t, discardLogger())

	var owns atomic.Bool
	r.Gate = owns.Load

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		_ = r.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	time.Sleep(time.Second)
	st := r.State()
	if st.Up || st.Model != "" {
		t.Fatalf("a standby started a child: %+v", st)
	}
	if !st.Standby {
		t.Fatal("a process without the device lock does not report itself as a standby, " +
			"so /readyz cannot name the check")
	}
	if n := f.countEvents(t, "start "); n != 0 {
		t.Fatalf("a standby spawned %d resident processes", n)
	}

	// Promoted — which is what a redeploy looks like from the new process's
	// side once the old one lets go.
	owns.Store(true)
	waitFor(t, "the promoted process to take the device", 30*time.Second, func() bool {
		return r.State().Up
	})
}

// CONTENTION IS DETECTED AND LOGGED — ruling 2, settled at 2x the expected run.
//
// §3 gives up ARBITRATING the R9700 against Ollama, and giving up visibility as
// well would mean the first symptom is a timeout in some other service. The
// warning fires well under the 5x kill threshold, so a deadline kill under
// contention is never the first thing anybody sees.
func TestSlowInferenceIsLoggedAsLikelyContention(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeSlow)
	if err := os.WriteFile(filepath.Join(f.Dir, "sleep_ms"), []byte("120"), 0o644); err != nil {
		t.Fatal(err)
	}

	var log syncBuffer
	r := f.resident(t, slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
	// A rate this device could never miss, so 120 ms is emphatically past 2x.
	r.ExpectedRates = map[string]float64{"small.en": 100000}
	startResident(t, r)

	if _, err := r.Transcribe(context.Background(), testJob("small.en")); err != nil {
		t.Fatal(err)
	}

	out := log.String()
	if !strings.Contains(out, "contention") {
		t.Fatalf("an inference far past its expected rate was not flagged:\n%s", out)
	}
}

// A slow inference is not a wedged one. The floor exists so that a five-second
// memo is not tripped by a cold cache, and a run inside the deadline must
// simply succeed.
func TestASlowButFinishingInferenceIsNotKilled(t *testing.T) {
	f := newFakeRunner(t)
	f.setMode(t, fakeSlow)
	if err := os.WriteFile(filepath.Join(f.Dir, "sleep_ms"), []byte("300"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := f.resident(t, discardLogger())
	r.MinDeadline = 5 * time.Second
	startResident(t, r)

	if _, err := r.Transcribe(context.Background(), testJob("small.en")); err != nil {
		t.Fatalf("a job well inside its deadline was killed: %v", err)
	}
	if n := f.countEvents(t, "start "); n != 1 {
		t.Fatalf("the resident process was restarted %d times for one slow job", n)
	}
}

// syncBuffer is a bytes.Buffer that survives the supervisor logging from its
// own goroutine while a test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
