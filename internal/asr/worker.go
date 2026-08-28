package asr

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
)

// Worker is the PLACEHOLDER per-invocation loop. CHRN-26 deletes it.
//
// It exists because the ticket asks for job state to survive `kill -9` of the
// worker, and the first draft of the decision assigned the worker wholly to
// CHRN-26 — leaving nothing to kill. Two reasons this shape was preferred over
// proving lease expiry against a hand-expired synthetic row: the property under
// test is that A CRASHED PROCESS RELEASES ITS WORK, and a hand-expired lease
// tests the reaper rather than the thing; and it gives CHRN-27 an end-to-end
// path to build against instead of blocking it behind CHRN-26.
//
// SINGLE-FLIGHT BY CONSTRUCTION: one worker process, one job at a time, no
// concurrency inside the loop. The epic's exit criterion is that the R9700 is
// never running two inferences at once, and a placeholder is not exempt from
// it. Anything CHRN-26 adds — a resident model, per-client fairness, a real
// GPU lease — replaces this rather than being layered on top.
type Worker struct {
	Store       *Store
	Transcriber Transcriber
	Logger      *slog.Logger

	// ID identifies this process in leased_by. It exists so that a claim can
	// be proved to belong to the caller in the same statement that acts on it,
	// which is what makes every worker-side write a compare-and-swap.
	ID string

	LeaseTTL time.Duration

	// Idle is how long to wait before asking again when the queue is empty.
	Idle time.Duration
}

// Run claims and processes jobs until ctx is cancelled.
//
// Errors from one job do not stop the loop: a job that fails to transcribe is a
// failed job, not a broken service. Errors from the DATABASE do pause it, on
// the same backoff as an empty queue, because hammering a Postgres that is down
// helps nobody.
func (w *Worker) Run(ctx context.Context) error {
	idle := w.Idle
	if idle <= 0 {
		idle = time.Second
	}
	w.Logger.Info("transcription worker started",
		"worker", w.ID, "lease_ttl", w.LeaseTTL.String(),
		"shape", "per-invocation placeholder, replaced by CHRN-26")

	for {
		job, err := w.Store.Claim(ctx, w.ID, w.LeaseTTL)
		switch {
		case errors.Is(err, ErrNotFound):
			// Empty queue. Not an event.
		case err != nil:
			if ctx.Err() != nil {
				return nil
			}
			w.Logger.Error("could not claim a job", "error", err)
		default:
			w.processOne(ctx, job)
			continue // straight back for the next one; no sleep between jobs
		}

		select {
		case <-ctx.Done():
			w.Logger.Info("transcription worker stopped", "worker", w.ID)
			return nil
		case <-time.After(idle):
		}
	}
}

// processOne runs a single claimed job to a terminal state, or abandons it.
//
// ABANDONING IS A REAL OUTCOME and the two cases that reach it are the point of
// the design. If the job was cancelled while running, this returns without
// writing anything: the worker stops renewing, the lease passes, and the reaper
// sends it to `cancelled` — never back to `queued`. If the process dies here,
// nothing at all happens, which is the case `kill -9` names, and the same lease
// expiry brings the job back with attempts incremented.
func (w *Worker) processOne(ctx context.Context, job Job) {
	log := w.Logger.With("job", job.ID, "worker", w.ID, "model", job.Model)

	audio, err := w.Store.Audio(ctx, job.ID, w.ID)
	if err != nil {
		// The lease was lost between the claim and this read. Say nothing to
		// the job: whoever holds it now is entitled to finish it.
		log.Warn("claimed job has no audio to read; releasing", "error", err)
		return
	}

	if _, err := w.Store.Start(ctx, job.ID, w.ID, w.LeaseTTL); err != nil {
		log.Warn("could not start claimed job; releasing", "error", err)
		return
	}

	// The renewal goroutine holds the lease open and watches for cancellation.
	// Cancelling `runCtx` is what stops ffmpeg and whisper-cli: exec.CommandContext
	// kills the child, so a cancelled job releases the GPU rather than running
	// to completion and having its result discarded.
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	cancelled := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		w.renew(runCtx, job.ID, log, stopRun, cancelled)
	}()

	tr, transcribeErr := w.Transcriber.Transcribe(runCtx, audio, job.AudioMediaType, job.Model, job.Language)

	stopRun()
	<-renewDone

	select {
	case <-cancelled:
		// Deliberately no write. The reaper is what moves a cancelled running
		// job, and it moves it to `cancelled` — see Reap. Writing `failed`
		// here would be a lie about why the job stopped.
		log.Info("job cancelled while running; leaving it for the reaper")
		return
	default:
	}
	if ctx.Err() != nil {
		// The PROCESS is shutting down, not the job. Same handling as a crash:
		// let the lease expire and the job come back. A shutdown hook that
		// tried to tidy up here would be the mechanism kill -9 does not run.
		log.Info("shutting down mid-job; releasing the lease")
		return
	}

	res := asrclient.Result{
		JobId:    job.ID,
		Model:    "whisper.cpp/" + job.Model,
		Backend:  w.Store.backend,
		Segments: []asrclient.Segment{},
	}
	if transcribeErr != nil {
		var fe *FailureError
		if !errors.As(transcribeErr, &fe) {
			fe = &FailureError{Code: "internal_error", Message: transcribeErr.Error()}
		}
		res.Status = asrclient.ResultStatusFailed
		// A run that did not complete is partial, always. The predicate
		// CHRN-22 gates on is `succeeded AND NOT partial`, so a failure is
		// already not durable — this is the belt to that brace, for a client
		// that reads only the one field.
		res.Partial = true
		res.Failure = &asrclient.Failure{Code: fe.Code, Message: fe.Message}
		log.Warn("transcription failed", "code", fe.Code, "detail", fe.Message)
	} else {
		res.Status = asrclient.ResultStatusSucceeded
		// The run completed, so it is not partial — INDEPENDENTLY of whether
		// it produced any text. A memo that is forty seconds of silence has a
		// true and complete answer, and treating "no speech" as not-durable
		// would keep the audio of exactly the recordings least worth keeping.
		res.Partial = false
		res.Text = tr.Text
		res.Segments = tr.Segments
		res.AudioDurationMs = &tr.AudioDurationMs
		res.CoveredMs = &tr.CoveredMs
		log.Info("transcribed", "chars", len(tr.Text), "segments", len(tr.Segments),
			"audio_ms", tr.AudioDurationMs, "covered_ms", tr.CoveredMs)
	}

	// Deliberately context.WithoutCancel: the transcription is done and the
	// result is in memory only. Losing it to a shutdown that arrived one
	// millisecond later would waste the GPU time and, worse, re-run the job.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()
	if _, err := w.Store.Finish(writeCtx, job.ID, w.ID, res); err != nil {
		// The lease was lost, so somebody else owns this job now. The work is
		// wasted; the job is not lost, which is the property that matters.
		log.Warn("could not record the result; the lease was lost", "error", err)
	}
}

// renew holds the lease open and reports a cancellation request.
//
// Renewing at a third of the TTL rather than at the TTL: a single missed tick —
// a GC pause, a slow database — must not expire a lease that a live worker is
// holding, because that hands the job to a second worker while the first is
// still on the GPU.
func (w *Worker) renew(ctx context.Context, id uuid.UUID, log *slog.Logger, stopRun context.CancelFunc, cancelled chan<- struct{}) {
	interval := w.LeaseTTL / 3
	if interval <= 0 {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		held, cancelling, err := w.Store.RenewLease(ctx, id, w.ID, w.LeaseTTL)
		switch {
		case err != nil:
			if ctx.Err() == nil {
				log.Warn("could not renew the lease", "error", err)
			}
			continue
		case cancelling:
			// STOP RENEWING. That is the whole mechanism: the lease passes and
			// the reaper reads cancel_requested_at, which is why cancellation
			// is a column rather than a state.
			log.Info("cancellation requested; stopping")
			close(cancelled)
			stopRun()
			return
		case !held:
			// Reaped, or finished elsewhere. Stop the run rather than burn GPU
			// on a result nobody will accept.
			log.Warn("lost the lease mid-job; stopping")
			stopRun()
			return
		}
	}
}
