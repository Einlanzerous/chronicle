package asr

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
)

// Worker is the claim loop over the resident transcriber. CHRN-26 replaced
// CHRN-25's per-invocation placeholder wholesale: "claim, shell out, write" is
// not the shape of a worker that holds a model and a device lease.
//
// SINGLE-FLIGHT IS NOT THIS TYPE'S DOING and that is deliberate. The GPU
// semaphore lives in the Resident, so N of these against one device still admit
// one inference at a time — which is what makes the Done-when testable with
// more than one worker goroutine rather than true only by construction.
type Worker struct {
	Store       *Store
	Transcriber Transcriber
	Logger      *slog.Logger

	// ID identifies this process in leased_by. It exists so that a claim can
	// be proved to belong to the caller in the same statement that acts on it,
	// which is what makes every worker-side write a compare-and-swap. It
	// carries ASR_DEVICE_ID, so a stalled lease names the card as well as the
	// container.
	ID string

	LeaseTTL time.Duration

	// Idle is how long to wait before asking again when the queue is empty.
	Idle time.Duration

	// ResidentModel reports what the transcriber currently holds, for the
	// claim's ordering. Empty — nothing loaded yet — matches no job and falls
	// through to plain round-robin, which is the right answer at startup.
	ResidentModel func() string

	// ModelSwitchMaxWait is how long a job for a non-resident model waits
	// before it forces a switch: the starvation bound, and the fairness bound
	// under mixed models.
	ModelSwitchMaxWait time.Duration

	// MaxAttempts and MaxAttemptsWedged are CHRN-28's ceilings, applied per
	// release reason by CeilingFor.
	MaxAttempts       int
	MaxAttemptsWedged int

	// Device reports whether this process holds the device lock. A standby
	// claims NOTHING: it serves the API, and the process that owns the card
	// does the work. nil means "no lock in play", which is what the tests that
	// are not about the lock want.
	Device func() bool
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
		"model_switch_max_wait", w.ModelSwitchMaxWait.String())

	for {
		if w.Device == nil || w.Device() {
			job, err := w.Store.ClaimForModel(ctx, w.ID, w.LeaseTTL, w.resident(), w.ModelSwitchMaxWait)
			switch {
			case errors.Is(err, ErrNotFound):
				// Empty queue. Not an event.
			case err != nil:
				if ctx.Err() != nil {
					return nil
				}
				w.Logger.Error("could not claim a job", "error", err)
			default:
				if !w.processOne(ctx, job) {
					continue // straight back for the next one; no sleep between jobs
				}
				// A RELEASED job pauses. It is going straight back into the
				// queue and this worker is about to claim it again, so a fault
				// that is not going away — a full disk, a child that will not
				// start — would otherwise spin claim/release as fast as the
				// database can answer. One idle interval is nothing against a
				// real job and is the difference between a stalled queue and a
				// hot loop against Postgres. CHRN-28's ceiling is what
				// eventually stops such a job for good; this is what keeps the
				// service civil until it exists.
			}
		}

		select {
		case <-ctx.Done():
			w.Logger.Info("transcription worker stopped", "worker", w.ID)
			return nil
		case <-time.After(idle):
		}
	}
}

func (w *Worker) resident() string {
	if w.ResidentModel == nil {
		return ""
	}
	return w.ResidentModel()
}

// processOne runs a single claimed job to a terminal state, releases it, or
// abandons it.
//
// THE THREE NON-TERMINAL OUTCOMES ARE THE POINT OF THIS TICKET.
//
//   - ABANDONED. The job was cancelled while running: nothing is written, the
//     worker stops renewing, and the reaper sends it to `cancelled` — never
//     back to `queued`. Same for a shutdown mid-job, which is the kill -9 case
//     with a tidier signal: the lease is the mechanism, so nothing has to run
//     in a dying process.
//   - RELEASED. The resident process died or wedged under a job that nothing
//     was wrong with. CHRN-25's placeholder had no way to say this and would
//     have FAILED the memo permanently on one child crash.
//   - FAILED. The audio, or the model, was the problem. That is a real answer
//     and the client gets a code.
//
// It reports whether the job was RELEASED rather than finished, which is the
// one outcome the caller paces itself against.
func (w *Worker) processOne(ctx context.Context, job Job) (released bool) {
	log := w.Logger.With("job", job.ID, "worker", w.ID, "model", job.Model)

	audio, err := w.Store.Audio(ctx, job.ID, w.ID)
	if err != nil {
		// The lease was lost between the claim and this read. Say nothing to
		// the job: whoever holds it now is entitled to finish it.
		log.Warn("claimed job has no audio to read; releasing", "error", err)
		return false
	}

	// The renewal goroutine holds the lease open and watches for cancellation.
	// It starts HERE, before the decode, because a forty-minute memo spends
	// real time in ffmpeg before it ever reaches the device — and NOTHING ON
	// THIS PATH MAY TOUCH THE GPU SEMAPHORE. A renewal that waited on the same
	// lock the inference holds would expire its own lease, and the reaper would
	// then hand the job to the worker still running it.
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	cancelled := make(chan struct{})
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		w.renew(runCtx, job.ID, log, stopRun, cancelled)
	}()

	started := time.Now()
	tr, transcribeErr := w.Transcriber.Transcribe(runCtx, TranscribeRequest{
		Audio:     audio,
		MediaType: job.AudioMediaType,
		Model:     job.Model,
		Language:  job.Language,

		// `leased` becomes `running` only when inference actually begins. The
		// gap between the two is the queue for the device, and CHRN-25 kept
		// the states apart so it would be visible here rather than inferred.
		OnInference: func(audioDurationMs int64) error {
			if _, err := w.Store.Start(runCtx, job.ID, w.ID, w.LeaseTTL); err != nil {
				// Failing to move `leased` -> `running` means the lease is
				// gone, not that the audio is bad. A ReleaseError so this
				// abandons the job instead of writing `failed` over work
				// somebody else may now be doing.
				return &ReleaseError{Reason: "lease_lost", Detail: err.Error()}
			}
			log.Debug("inference starting", "audio_ms", audioDurationMs,
				"waited", time.Since(started).Round(time.Millisecond).String())
			return nil
		},
	})

	stopRun()
	<-renewDone

	select {
	case <-cancelled:
		// Deliberately no write. The reaper is what moves a cancelled running
		// job, and it moves it to `cancelled` — see Reap. Writing `failed`
		// here would be a lie about why the job stopped.
		log.Info("job cancelled while running; leaving it for the reaper")
		return false
	default:
	}
	if ctx.Err() != nil {
		// The PROCESS is shutting down, not the job. Same handling as a crash:
		// let the lease expire and the job come back. A shutdown hook that
		// tried to tidy up here would be the mechanism kill -9 does not run.
		log.Info("shutting down mid-job; releasing the lease")
		return false
	}

	// Deliberately context.WithoutCancel on every write below: the work is
	// done and the outcome is in memory only. Losing it to a shutdown that
	// arrived one millisecond later would waste the GPU time and, worse,
	// re-run the job.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	var release *ReleaseError
	if errors.As(transcribeErr, &release) {
		w.release(writeCtx, job, release, time.Since(started), log)
		return true
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
			"audio_ms", tr.AudioDurationMs, "covered_ms", tr.CoveredMs,
			"took", time.Since(started).Round(time.Millisecond).String())
	}

	if _, err := w.Store.Finish(writeCtx, job.ID, w.ID, res); err != nil {
		// The lease was lost, so somebody else owns this job now. The work is
		// wasted; the job is not lost, which is the property that matters.
		log.Warn("could not record the result; the lease was lost", "error", err)
	}
	return false
}

// release hands a job back because the WORKER failed, not the job.
//
// At warn and naming the reason, because a crash and a deadline breach both
// increment `attempts` and the counter cannot tell them apart — while they cost
// differently by a factor of five. That difference is CHRN-28's to price, and
// this line is where it reads it.
func (w *Worker) release(ctx context.Context, job Job, cause *ReleaseError, elapsed time.Duration, log *slog.Logger) {
	max, wedged := w.MaxAttempts, w.MaxAttemptsWedged
	if max < 1 {
		max = DefaultMaxAttempts
	}
	if wedged < 1 {
		wedged = DefaultMaxAttemptsWedged
	}
	out, err := w.Store.Release(ctx, job.ID, w.ID, cause.Reason, CeilingFor(cause.Reason, max, wedged))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			log.Warn("could not release the job; the lease was already lost",
				"reason", cause.Reason, "detail", cause.Detail)
			return
		}
		log.Error("could not release the job", "reason", cause.Reason, "error", err)
		return
	}
	if out.Status == StatusFailed {
		log.Error("RETRY CEILING REACHED; job dead-lettered and will not be retried",
			"reason", out.Reason, "detail", cause.Detail,
			"attempts", out.Attempts, "code", ExhaustedCode)
		return
	}
	log.Warn("job released without a result; nothing was wrong with the job itself",
		"reason", out.Reason, "detail", cause.Detail, "to", out.Status,
		"attempts", out.Attempts, "elapsed", elapsed.Round(time.Millisecond).String())
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
			// is a column rather than a state. Cancelling runCtx is what
			// actually stops the inference — the resident process is killed,
			// because dropping the HTTP request would leave it running to
			// completion with the queue blocked behind it.
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
