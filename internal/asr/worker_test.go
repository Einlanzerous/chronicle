package asr

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The lease properties, tested against a REAL asrd process that is really
// killed. They are the reason CHRN-25 ships a worker at all: the property is
// that a crashed process releases its work, and a hand-expired lease on a
// synthetic row tests the reaper rather than the thing.

// A token long enough for the boot check, and obviously not a credential.
const testToken = "test-token-0000000000000000000000000000"

// JOB STATE SURVIVES kill -9 OF THE WORKER. Done-when 3.
//
// The job returns to `queued` with `attempts` incremented, and it is never
// dropped — dropping is indistinguishable from a memo that was never captured,
// which is the failure the whole system exists to avoid.
func TestKillNineReturnsTheJobToTheQueue(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	fake := newFakeRunner(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-kill9kill9kill9k", "kill"))
	if err != nil {
		t.Fatal(err)
	}

	// A short lease so the test does not sit for half a minute. The renewal
	// interval is a third of it, so a live worker still holds it comfortably —
	// which is what makes the assertion below about death rather than timing.
	asrd := startASRD(t, fake, 2*time.Second)

	waitFor(t, "the job to be running", 30*time.Second, func() bool {
		got, err := s.Get(ctx, "chronicle", job.ID)
		return err == nil && got.Status == StatusRunning
	})

	before, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.Attempts != 0 {
		t.Fatalf("attempts is %d before anything went wrong; want 0", before.Attempts)
	}

	// SIGKILL, not SIGTERM. A graceful stop would prove nothing: the case the
	// ticket names is the one where no shutdown code runs at all.
	asrd.kill(t)

	waitFor(t, "the lease to expire", 30*time.Second, func() bool {
		got, err := s.Get(ctx, "chronicle", job.ID)
		return err == nil && got.LeaseExpiresAt != nil && got.LeaseExpiresAt.Before(time.Now())
	})

	reaped, err := s.Reap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(reaped) != 1 || reaped[0].ID != job.ID {
		t.Fatalf("reaped %+v; want exactly the killed worker's job", reaped)
	}

	after, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatalf("the job disappeared when its worker was killed: %v", err)
	}
	if after.Status != StatusQueued {
		t.Fatalf("status %q after the worker was killed; want queued", after.Status)
	}
	if after.Attempts != 1 {
		t.Fatalf("attempts is %d; want 1 — the counter is what CHRN-28 sets a ceiling against", after.Attempts)
	}
	if after.LeasedBy != "" || after.LeaseExpiresAt != nil {
		t.Fatal("a requeued job still carries the dead worker's lease")
	}

	// And the audio is still there, because a requeued job has to be runnable.
	if _, err := s.Pool().Exec(ctx, `SELECT 1`); err != nil {
		t.Fatal(err)
	}
	var audio []byte
	if err := s.Pool().QueryRow(ctx, `SELECT audio FROM jobs WHERE id = $1`, job.ID).Scan(&audio); err != nil {
		t.Fatal(err)
	}
	if len(audio) == 0 {
		t.Fatal("a requeued job has no audio to re-run, so it can never leave the queue")
	}
}

// A CANCELLED RUNNING JOB WHOSE WORKER THEN DIES ENDS `cancelled`, NOT
// `queued`, and is never re-run. Done-when 6.
//
// This is the interaction the state-machine version of cancel got wrong, and
// the reason cancellation is a column: as a state, a running job that was
// cancelled and whose worker died would be reaped back to the queue and run
// again — the one outcome cancel exists to prevent.
func TestCancelledJobWhoseWorkerDiesIsNotRequeued(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	fake := newFakeRunner(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-cancelcancelcanc", "cancel"))
	if err != nil {
		t.Fatal(err)
	}

	asrd := startASRD(t, fake, 2*time.Second)

	waitFor(t, "the job to be running", 30*time.Second, func() bool {
		got, err := s.Get(ctx, "chronicle", job.ID)
		return err == nil && got.Status == StatusRunning
	})

	if _, err := s.Cancel(ctx, "chronicle", job.ID); err != nil {
		t.Fatal(err)
	}

	// Killed rather than allowed to observe the cancellation, because the
	// combination is the case under test: cancelled AND the worker gone.
	asrd.kill(t)

	waitFor(t, "the lease to expire", 30*time.Second, func() bool {
		got, err := s.Get(ctx, "chronicle", job.ID)
		return err == nil && got.LeaseExpiresAt != nil && got.LeaseExpiresAt.Before(time.Now())
	})

	if _, err := s.Reap(ctx); err != nil {
		t.Fatal(err)
	}

	after, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusCancelled {
		t.Fatalf("status %q; want cancelled — requeueing would re-run work somebody stopped", after.Status)
	}

	// And it stays cancelled: a second reap, and a claim, must not resurrect it.
	if _, err := s.Reap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "worker-2", testLease); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a cancelled job was claimable: %v", err)
	}

	// The database refuses the requeue outright, so this does not depend on
	// every future caller of Reap remembering the rule.
	if _, err := s.Pool().Exec(ctx, `
		UPDATE jobs SET status = 'queued', cancel_requested_at = cancel_requested_at
		 WHERE id = $1`, job.ID); err == nil {
		t.Fatal("the database allowed a cancelled job back into the queue")
	}
}

// The happy path, end to end through a real process: claim, decode, transcribe,
// write the result, release the audio.
func TestWorkerRunsAJobToSuccess(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	fake := newFakeRunner(t)

	job, _, err := s.Submit(ctx, submitInput("chronicle", "key-happyhappyhappy1", "happy"))
	if err != nil {
		t.Fatal(err)
	}

	startASRD(t, fake, 10*time.Second)
	fake.release(t)

	waitFor(t, "the job to succeed", 60*time.Second, func() bool {
		got, err := s.Get(ctx, "chronicle", job.ID)
		return err == nil && got.Terminal()
	})

	got, err := s.Get(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSucceeded {
		t.Fatalf("status %q, want succeeded", got.Status)
	}
	if got.Partial == nil || *got.Partial {
		t.Fatal("a completed run was recorded as partial")
	}

	res, err := s.Result(ctx, "chronicle", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello there" {
		t.Fatalf("text %q, want %q", res.Text, "hello there")
	}
	if len(res.Segments) != 2 {
		t.Fatalf("%d segments, want 2", len(res.Segments))
	}
	if res.Backend != "vulkan" || res.Model != "whisper.cpp/small.en" {
		t.Fatalf("result names backend=%q model=%q; both are stored with the transcript, "+
			"so a corpus transcribed by two models does not vary invisibly", res.Backend, res.Model)
	}
	// covered_ms is EVIDENCE and is short of the duration here, exactly as it
	// is on any real recording that ends in silence. Asserting the gap is the
	// point: a pruner gated on covered_ms == audio_duration_ms would refuse
	// this transcript.
	if res.AudioDurationMs == nil || res.CoveredMs == nil {
		t.Fatal("the result carries no duration evidence")
	}
	if *res.CoveredMs >= *res.AudioDurationMs {
		t.Fatalf("covered_ms %d is not short of audio_duration_ms %d; the fixture no longer "+
			"exercises the trap §5 warns about", *res.CoveredMs, *res.AudioDurationMs)
	}
}

// --- running a real asrd ---------------------------------------------------

type asrdProcess struct {
	cmd *exec.Cmd
	out *bytes.Buffer
}

// startASRD builds and runs the service against the fake runner. It is a real
// process because the property under test is what happens when a real process
// dies.
func startASRD(t *testing.T, fake *fakeRunner, leaseTTL time.Duration) *asrdProcess {
	t.Helper()

	bin := buildASRD(t)
	cmd := exec.Command(bin, "serve")
	cmd.Env = append(fake.env(os.Getenv("ASR_TEST_DATABASE_URL"), "chronicle:"+testToken, leaseTTL),
		"ASR_BACKEND=vulkan")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start asrd: %v", err)
	}

	p := &asrdProcess{cmd: cmd, out: &out}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		if t.Failed() {
			t.Logf("asrd output:\n%s", out.String())
		}
	})
	return p
}

// kill sends SIGKILL. Not SIGTERM: the case under test is the one where no
// shutdown path runs, so a graceful stop would be testing the wrong thing.
func (p *asrdProcess) kill(t *testing.T) {
	t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("kill asrd: %v", err)
	}
	_ = p.cmd.Wait()
}

// buildASRD compiles the service once per package run.
var builtASRD string

func buildASRD(t *testing.T) string {
	t.Helper()
	if builtASRD != "" {
		return builtASRD
	}
	dir, err := os.MkdirTemp("", "asrd-bin-*")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "asrd")
	build := exec.Command("go", "build", "-o", bin, "github.com/Einlanzerous/chronicle/cmd/asrd")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build asrd: %v\n%s", err, out)
	}
	builtASRD = bin
	t.Cleanup(func() {}) // the binary outlives individual tests on purpose
	return bin
}

// A boot with no client tokens must FAIL rather than serve anonymously. There
// is no ASR_AUTH flag for the same reason Chronicle has no CHRONICLE_AUTH:
// auth is unconditional, so there is nothing to leave off by accident.
func TestBootWithoutClientTokensFails(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ASR_TEST_DATABASE_URL")) == "" {
		t.Skip("ASR_TEST_DATABASE_URL unset")
	}
	bin := buildASRD(t)
	cmd := exec.Command(bin, "serve")
	cmd.Env = []string{
		"ASR_DATABASE_URL=" + os.Getenv("ASR_TEST_DATABASE_URL"),
		"PATH=" + os.Getenv("PATH"),
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("asrd booted with no client tokens configured")
	}
	if !strings.Contains(string(out), "ASR_CLIENT_TOKENS") {
		t.Fatalf("the boot error does not name the variable that is missing:\n%s", out)
	}
}
