// Package transcribe wires Chronicle up as client one of the estate ASR
// service: a captured memo becomes a job, a job becomes a transcript, and a
// failure becomes a state a human can see.
//
// It is a POLLING PUMP and not a request path. Nothing a person does waits on
// it: a memo is acknowledged when its audio is durable, and transcription
// happens afterwards, which is why a transcription outage delays a corpus
// rather than rejecting a recording.
//
// Three things here are binding rather than chosen, all from CHRN-25 §5, and
// all of them are the kind that would be closed silently by an implementer who
// had not read it:
//
//  1. tier2.transcripts carries `partial`, alongside `model` and `backend`.
//  2. A row is written for EVERY succeeded result, empty text included.
//  3. The durable gate is a Chronicle-side query and never an HTTP call.
package transcribe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Defaults.
const (
	// DefaultInterval is how often the pump sweeps. Short, because the point
	// of E3 is that a memo becomes searchable soon after it is spoken, and a
	// three-minute memo is about three seconds of GPU.
	DefaultInterval = 10 * time.Second

	// DefaultBatch bounds one sweep. A cap rather than "everything", so that a
	// backlog of eight hundred memos does not become eight hundred concurrent
	// HTTP calls the first time this is switched on.
	DefaultBatch = 20

	// DefaultModel matches the ASR service's own default.
	DefaultModel = "small.en"

	// MaxAttempts is a PLACEHOLDER CEILING, and deliberately a crude one.
	//
	// The retry policy — how many, with what backoff, and what to do about a
	// partial — is CHRN-28's, and this is not it. What this is: a bound, so
	// that a service which keeps losing jobs cannot put one memo through an
	// unmetered loop of GPU runs. Without it the requeue path below is an
	// infinite loop that costs money and shows up as a busy GPU nobody
	// ordered.
	MaxAttempts = 5
)

// Store is the slice of Chronicle's store this needs. An interface so the pump
// can be tested against a fake, and so the surface it touches is written down
// in one place rather than inferred from a hundred call sites.
type Store interface {
	MemosAwaitingTranscription(ctx context.Context, limit int) ([]store.Memo, error)
	AdvanceMemoState(ctx context.Context, id uuid.UUID, from, to, reason string) (store.Memo, error)
	GetMemo(ctx context.Context, id uuid.UUID) (store.Memo, error)

	HasDurableTranscript(ctx context.Context, memoID uuid.UUID) (bool, error)
	RecordTranscript(ctx context.Context, in store.TranscriptInput) (store.Transcript, error)

	BeginTranscription(ctx context.Context, memoID uuid.UUID, model, audioSHA256 string) (store.MemoJob, error)
	RecordJobSubmitted(ctx context.Context, id, jobID uuid.UUID) (store.MemoJob, error)
	RecordJobCollected(ctx context.Context, id uuid.UUID) error
	RecordJobFailure(ctx context.Context, id uuid.UUID, code, message string) error
	UnsubmittedJobs(ctx context.Context, limit int) ([]store.MemoJob, error)
	InFlightJobs(ctx context.Context, limit int) ([]store.MemoJob, error)
	CountMemoJobs(ctx context.Context, memoID uuid.UUID) (int, error)
}

// ASR is the slice of the generated client this uses. An interface for the
// same reason Store is one, and it is satisfied by *asrclient.ClientWithResponses
// as generated — nothing is wrapped or reimplemented.
type ASR interface {
	SubmitJobWithBodyWithResponse(ctx context.Context, params *asrclient.SubmitJobParams, contentType string, body io.Reader, reqEditors ...asrclient.RequestEditorFn) (*asrclient.SubmitJobResponse, error)
	GetJobWithResponse(ctx context.Context, id asrclient.JobID, reqEditors ...asrclient.RequestEditorFn) (*asrclient.GetJobResponse, error)
	GetJobResultWithResponse(ctx context.Context, id asrclient.JobID, reqEditors ...asrclient.RequestEditorFn) (*asrclient.GetJobResultResponse, error)
}

// Options configures the pump.
type Options struct {
	Store  Store
	Audio  *audio.Store
	ASR    ASR
	Logger *slog.Logger

	Model    string
	Interval time.Duration
	Batch    int
}

// Service is the pump.
type Service struct {
	store    Store
	audio    *audio.Store
	asr      ASR
	logger   *slog.Logger
	model    string
	interval time.Duration
	batch    int
}

// New validates the options and builds the pump.
func New(o Options) (*Service, error) {
	switch {
	case o.Store == nil:
		return nil, fmt.Errorf("transcribe: a store is required")
	case o.Audio == nil:
		return nil, fmt.Errorf("transcribe: an audio store is required")
	case o.ASR == nil:
		return nil, fmt.Errorf("transcribe: an ASR client is required")
	case o.Logger == nil:
		return nil, fmt.Errorf("transcribe: a logger is required")
	}
	s := &Service{
		store: o.Store, audio: o.Audio, asr: o.ASR, logger: o.Logger,
		model: o.Model, interval: o.Interval, batch: o.Batch,
	}
	if s.model == "" {
		s.model = DefaultModel
	}
	if s.interval <= 0 {
		s.interval = DefaultInterval
	}
	if s.batch <= 0 {
		s.batch = DefaultBatch
	}
	return s, nil
}

// Run sweeps until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	t := time.NewTicker(s.interval)
	defer t.Stop()

	s.logger.Info("transcription pump started",
		"interval", s.interval.String(), "model", s.model, "batch", s.batch)

	// One sweep immediately, so a restart after a backlog does not wait out a
	// full interval before doing anything.
	s.Tick(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("transcription pump stopped")
			return nil
		case <-t.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs one sweep. Exported so tests can drive it deterministically rather
// than sleeping past a ticker.
//
// The order matters: RESUME before SUBMIT, so an attempt whose key is already
// persisted is re-sent under that key rather than joined by a second attempt;
// and COLLECT last, so a job submitted in this sweep is polled once before the
// sweep ends. That poll is a cheap status call, and it means a short memo — the
// common case, three seconds of GPU — can be submitted and collected in one
// sweep rather than waiting out an interval it has already finished.
func (s *Service) Tick(ctx context.Context) {
	s.resume(ctx)
	s.submit(ctx)
	s.collect(ctx)
}

// resume re-sends attempts whose submit never completed.
//
// THIS IS WHAT THE PERSIST-BEFORE-SEND ORDERING BUYS. The row was written, the
// process died or the service was unreachable, and the key is still here — so
// the re-send is a replay of one attempt and the answer is the original job,
// rather than a second job transcribing the same memo.
func (s *Service) resume(ctx context.Context) {
	jobs, err := s.store.UnsubmittedJobs(ctx, s.batch)
	if err != nil {
		s.logError(ctx, "could not list unsubmitted attempts", err)
		return
	}
	for _, job := range jobs {
		memo, err := s.store.GetMemo(ctx, job.MemoID)
		if err != nil {
			// The memo is gone and the tier-1 row outlived it, which is
			// allowed: tier 1 holds no foreign key into tier 2. Settle the row
			// so it stops being swept.
			_ = s.store.RecordJobFailure(ctx, job.ID, "memo_missing",
				"the memo this attempt belongs to no longer exists")
			continue
		}
		// The same skip submit() makes, and it belongs on BOTH paths that
		// reach send(): a stale attempt row beside a transcript that arrived
		// some other way would otherwise be re-sent, and the check that exists
		// to stop a wasted GPU run would be on one of the two ways in.
		durable, err := s.store.HasDurableTranscript(ctx, memo.ID)
		if err != nil {
			s.logError(ctx, "could not check for an existing transcript", err, "memo", memo.ID)
			continue
		}
		if durable {
			_ = s.store.RecordJobFailure(ctx, job.ID, "already_transcribed",
				"a durable transcript already existed; the attempt was not sent")
			s.settle(ctx, memo)
			continue
		}
		s.send(ctx, job, memo)
	}
}

// submit starts an attempt for each memo that still needs one.
func (s *Service) submit(ctx context.Context) {
	memos, err := s.store.MemosAwaitingTranscription(ctx, s.batch)
	if err != nil {
		s.logError(ctx, "could not list memos awaiting transcription", err)
		return
	}

	for _, memo := range memos {
		log := s.logger.With("memo", memo.ID)

		// CHRN-18's Done-when #7: SKIP ASR WHEN A DURABLE TRANSCRIPT ALREADY
		// EXISTS. Read from Chronicle and never from the ASR service, whose
		// answer at thirty days is a 410 — see store.HasDurableTranscript.
		//
		// This is not merely an optimisation. A memo whose audio has been
		// pruned has nothing to send, and `held` keeps an exit to `queued`
		// even after a prune, so without this check a re-queued memo would
		// loop forever failing to read bytes that are gone.
		durable, err := s.store.HasDurableTranscript(ctx, memo.ID)
		if err != nil {
			s.logError(ctx, "could not check for an existing transcript", err, "memo", memo.ID)
			continue
		}
		if durable {
			log.Info("memo already has a durable transcript; skipping ASR")
			s.settle(ctx, memo)
			continue
		}

		// Mark intent before doing anything slow. A memo in `queued` with no
		// attempt is picked up again next tick; a memo in `captured` that was
		// half-processed would not be distinguishable from one nothing has
		// touched.
		if memo.State == store.StateCaptured {
			advanced, err := s.store.AdvanceMemoState(ctx, memo.ID, store.StateCaptured, store.StateQueued, "")
			if err != nil {
				// Lost the race to another sweep, or the memo moved. Either
				// way it is not ours.
				continue
			}
			// The LOCAL copy is updated too, and it matters: everything below
			// holds or requeues from memo.State, and a stale `captured` there
			// would CAS against a state the memo has already left — leaving a
			// failed submission in `queued` forever with nothing recorded
			// about why.
			memo = advanced
		}

		attempts, err := s.store.CountMemoJobs(ctx, memo.ID)
		if err != nil {
			s.logError(ctx, "could not count previous attempts", err, "memo", memo.ID)
			continue
		}
		if attempts >= MaxAttempts {
			// See MaxAttempts: a bound, not a policy. CHRN-28 replaces it.
			s.hold(ctx, memo.ID, store.StateQueued, fmt.Sprintf(
				"transcription failed %d times; held for review (retry policy is CHRN-28)", attempts))
			continue
		}

		// THE KEY IS PERSISTED BEFORE THE REQUEST IS SENT. Everything else in
		// this function is arrangement; this line is the requirement.
		job, err := s.store.BeginTranscription(ctx, memo.ID, s.model, memo.ContentHash)
		if errors.Is(err, store.ErrJobInFlight) {
			continue // a previous attempt is still being collected
		}
		if err != nil {
			s.logError(ctx, "could not begin a transcription attempt", err, "memo", memo.ID)
			continue
		}
		s.send(ctx, job, memo)
	}
}

// send posts the audio under an already-persisted key.
func (s *Service) send(ctx context.Context, job store.MemoJob, memo store.Memo) {
	log := s.logger.With("memo", memo.ID, "attempt", job.ID)

	path, err := s.audio.Path(audio.Ref{AuthorID: memo.AuthorID, ContentHash: memo.ContentHash})
	if err != nil {
		s.fail(ctx, job, memo, "audio_unreadable", err.Error())
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// The database says this memo has audio and the disk disagrees,
			// which is CHRN-23's `missing` — the one state that means
			// something irreplaceable is gone. Held rather than retried: no
			// number of retries produces a file that is not there.
			s.fail(ctx, job, memo, "audio_missing",
				"the recording is not on disk; see GET /admin/storage")
			return
		}
		s.logError(ctx, "could not read a recording", err, "memo", memo.ID)
		return
	}

	mediaType := mediaTypeFor(memo)
	payload, contentType, err := multipartBody(asrclient.JobSpec{
		AudioSha256: memo.ContentHash,
		Model:       &job.Model,
	}, mediaType, body)
	if err != nil {
		s.logError(ctx, "could not build the submission", err, "memo", memo.ID)
		return
	}

	resp, err := s.asr.SubmitJobWithBodyWithResponse(ctx,
		&asrclient.SubmitJobParams{IdempotencyKey: job.IdempotencyKey},
		contentType, bytes.NewReader(payload))
	if err != nil {
		// Unreachable. NOT a failure of the attempt: the key is persisted, so
		// the next sweep re-sends it and the service replays rather than
		// duplicating. Logged at warn because a transcription backlog that
		// nobody can see is how an outage becomes a corpus gap.
		s.logger.WarnContext(ctx, "the ASR service is unreachable; will retry",
			"memo", memo.ID, "error", err)
		return
	}

	switch {
	case resp.JSON201 != nil, resp.JSON200 != nil:
		accepted := resp.JSON201
		if accepted == nil {
			accepted = resp.JSON200 // a replay of this same attempt
		}
		if _, err := s.store.RecordJobSubmitted(ctx, job.ID, accepted.Id); err != nil {
			s.logError(ctx, "could not record the job id", err, "memo", memo.ID)
			return
		}
		if _, err := s.store.AdvanceMemoState(ctx, memo.ID, store.StateQueued, store.StateTranscribing, ""); err != nil {
			// The job exists and the memo did not move. The collect pass keys
			// off the tier-1 row rather than the memo state, so this is
			// recoverable; say so rather than swallowing it.
			s.logError(ctx, "submitted, but the memo state did not advance", err, "memo", memo.ID)
		}
		log.Info("submitted for transcription", "job", accepted.Id, "model", job.Model, "bytes", len(body))

	case resp.JSON409 != nil:
		// This key was used for a different spec or different audio, which
		// should be impossible: the key is minted per attempt and never
		// reused. It means Chronicle's row and the service's disagree, so
		// settle this attempt and let the next sweep mint a fresh key.
		s.fail(ctx, job, memo, "idempotency_mismatch", resp.JSON409.Message)

	case resp.JSON415 != nil:
		s.fail(ctx, job, memo, "unsupported_media_type",
			fmt.Sprintf("the ASR service does not accept %s", mediaType))

	default:
		// Includes 401 and 5xx. Not settled: a bad token or a restarting
		// service is a condition to fix, not a memo to give up on.
		s.logger.WarnContext(ctx, "the ASR service refused a submission; will retry",
			"memo", memo.ID, "status", resp.HTTPResponse.StatusCode,
			"body", firstLine(string(resp.Body)))
	}
}

// collect polls submitted attempts and writes the results.
func (s *Service) collect(ctx context.Context) {
	jobs, err := s.store.InFlightJobs(ctx, s.batch)
	if err != nil {
		s.logError(ctx, "could not list in-flight attempts", err)
		return
	}

	for _, job := range jobs {
		if job.JobID == nil {
			continue // resume() owns these
		}
		memo, err := s.store.GetMemo(ctx, job.MemoID)
		if err != nil {
			_ = s.store.RecordJobFailure(ctx, job.ID, "memo_missing",
				"the memo this attempt belongs to no longer exists")
			continue
		}

		resp, err := s.asr.GetJobWithResponse(ctx, *job.JobID)
		if err != nil {
			s.logger.WarnContext(ctx, "the ASR service is unreachable; will retry",
				"memo", memo.ID, "error", err)
			continue
		}

		switch {
		case resp.JSON200 != nil:
			s.observe(ctx, job, memo, *resp.JSON200)
		case resp.JSON404 != nil:
			// The service lost the job. That is ALLOWED — its whole store is
			// disposable by design — so this is a fresh attempt, not a
			// failure of transcription. Settle the row and return the memo to
			// the queue.
			s.requeue(ctx, job, memo, "job_missing",
				"the ASR service no longer has this job; re-submitting")
		default:
			s.logger.WarnContext(ctx, "unexpected status polling a job",
				"memo", memo.ID, "job", *job.JobID,
				"status", resp.HTTPResponse.StatusCode)
		}
	}
}

// observe acts on one poll.
func (s *Service) observe(ctx context.Context, job store.MemoJob, memo store.Memo, wire asrclient.Job) {
	switch wire.Status {
	case asrclient.JobStatusQueued, asrclient.JobStatusLeased,
		asrclient.JobStatusRunning, asrclient.JobStatusCancelling:
		// Still working. The server sets poll pressure via retry_after_ms;
		// this pump sweeps on its own interval and does not need to honour it
		// per job, but a deep queue is worth seeing.
		return

	case asrclient.JobStatusSucceeded:
		s.fetch(ctx, job, memo)

	case asrclient.JobStatusFailed:
		s.fail(ctx, job, memo, "transcription_failed", s.failureDetail(ctx, job))

	case asrclient.JobStatusCancelled:
		// Nothing in Chronicle cancels a job today, so this means somebody or
		// something else did. Held rather than retried: re-submitting work
		// that was deliberately stopped is the outcome cancel exists to
		// prevent.
		s.fail(ctx, job, memo, "transcription_cancelled",
			"the transcription was cancelled")
	}
}

// fetch collects a succeeded result and writes the transcript.
func (s *Service) fetch(ctx context.Context, job store.MemoJob, memo store.Memo) {
	resp, err := s.asr.GetJobResultWithResponse(ctx, *job.JobID)
	if err != nil {
		s.logger.WarnContext(ctx, "the ASR service is unreachable; will retry",
			"memo", memo.ID, "error", err)
		return
	}

	switch {
	case resp.JSON200 == nil && resp.JSON410 != nil:
		// The payload aged out before Chronicle collected it. "RESULT EXPIRED"
		// IS NOT "TRANSCRIPTION FAILED" — CHRN-25 §9 is explicit — so the memo
		// goes back to the queue for a fresh attempt rather than being held as
		// though something went wrong with it.
		s.requeue(ctx, job, memo, "result_expired",
			"the result aged out before it was collected; re-submitting")
		return
	case resp.JSON200 == nil:
		s.logger.WarnContext(ctx, "unexpected status fetching a result",
			"memo", memo.ID, "job", *job.JobID, "status", resp.HTTPResponse.StatusCode)
		return
	}
	res := *resp.JSON200

	// A ROW IS WRITTEN FOR EVERY SUCCEEDED RESULT, EMPTY TEXT INCLUDED.
	//
	// There is deliberately no `if res.Text == "" { return }` here, and this
	// comment is the reason: CHRN-25 §5 names that exact line as the one that
	// silently inverts the ruling, in the SAFE direction so nothing ever
	// complains, stranding the audio of precisely the memos the ruling argued
	// should prune. Forty seconds of silence has a true and complete answer,
	// and the answer is "no speech".
	segments := make([]store.Segment, 0, len(res.Segments))
	for _, seg := range res.Segments {
		segments = append(segments, store.Segment{
			StartMS: seg.StartMs, EndMS: seg.EndMs, Text: seg.Text,
		})
	}

	if _, err := s.store.RecordTranscript(ctx, store.TranscriptInput{
		MemoID: memo.ID,
		Text:   res.Text,
		// `partial` is carried across the boundary UNCHANGED. It is a fact the
		// service recorded about its own run, and it is never recomputed here
		// from covered_ms against audio_duration_ms.
		Partial:         res.Partial,
		Segments:        segments,
		Model:           res.Model,
		Backend:         res.Backend,
		AudioDurationMS: res.AudioDurationMs,
		CoveredMS:       res.CoveredMs,
	}); err != nil {
		s.logError(ctx, "could not record a transcript", err, "memo", memo.ID)
		return
	}

	// Collected AFTER the transcript is written, never before. If the process
	// dies between the two, the next sweep collects again and the tier-2
	// upsert makes that a no-op — where the other order would settle an
	// attempt whose transcript was never stored.
	if err := s.store.RecordJobCollected(ctx, job.ID); err != nil {
		s.logError(ctx, "could not settle a collected attempt", err, "memo", memo.ID)
	}

	if _, err := s.store.AdvanceMemoState(ctx, memo.ID, store.StateTranscribing, store.StateTranscribed, ""); err != nil {
		s.logError(ctx, "transcript stored, but the memo state did not advance", err, "memo", memo.ID)
	}

	s.logger.InfoContext(ctx, "transcribed",
		"memo", memo.ID, "model", res.Model, "backend", res.Backend,
		"partial", res.Partial, "chars", len(res.Text), "segments", len(res.Segments))
}

// failureDetail reads the failure the service recorded, so the memo's
// state_reason says what went wrong rather than that something did.
func (s *Service) failureDetail(ctx context.Context, job store.MemoJob) string {
	resp, err := s.asr.GetJobResultWithResponse(ctx, *job.JobID)
	if err != nil || resp.JSON200 == nil || resp.JSON200.Failure == nil {
		return "the ASR service reported a failure with no detail"
	}
	return resp.JSON200.Failure.Code + ": " + resp.JSON200.Failure.Message
}

// fail settles the attempt and HOLDS the memo, which is the half of the ticket
// that says a transcription failure must leave a memo "in a state a human can
// see and retry". `held` is visible in GET /admin/transcription with its
// reason, and `chronicle retranscribe` is the retry.
func (s *Service) fail(ctx context.Context, job store.MemoJob, memo store.Memo, code, message string) {
	if err := s.store.RecordJobFailure(ctx, job.ID, code, message); err != nil {
		s.logError(ctx, "could not record a failed attempt", err, "memo", memo.ID)
	}
	s.hold(ctx, memo.ID, memo.State, code+": "+message)
	s.logger.WarnContext(ctx, "transcription failed; memo held",
		"memo", memo.ID, "code", code, "detail", message)
}

// requeue settles the attempt and returns the memo to the queue for a FRESH
// one. Distinct from fail: nothing went wrong with the memo, the service
// simply no longer has the answer.
func (s *Service) requeue(ctx context.Context, job store.MemoJob, memo store.Memo, code, message string) {
	if err := s.store.RecordJobFailure(ctx, job.ID, code, message); err != nil {
		s.logError(ctx, "could not settle an attempt", err, "memo", memo.ID)
	}
	if _, err := s.store.AdvanceMemoState(ctx, memo.ID, memo.State, store.StateQueued, ""); err != nil {
		s.logError(ctx, "could not return a memo to the queue", err, "memo", memo.ID)
	}
	s.logger.InfoContext(ctx, "re-submitting", "memo", memo.ID, "reason", code)
}

// settle walks a memo that already has a durable transcript to `transcribed`.
//
// It goes the long way round — captured to queued to transcribing to
// transcribed — because those are the edges the state machine has, and the
// state machine is in the database where every future writer meets it. Adding
// a shortcut edge would mean altering CHRN-18's guard from inside an E3
// ticket, to save two writes on a case that only arises when a transcript
// arrived by some other path.
//
// It is also the more truthful account: the memo WAS queued and WAS claimed.
// What is unusual is only that the answer turned out to be already known.
func (s *Service) settle(ctx context.Context, memo store.Memo) {
	walk := []struct{ from, to string }{
		{store.StateCaptured, store.StateQueued},
		{store.StateQueued, store.StateTranscribing},
		{store.StateTranscribing, store.StateTranscribed},
	}
	state := memo.State
	for _, step := range walk {
		if state != step.from {
			continue
		}
		advanced, err := s.store.AdvanceMemoState(ctx, memo.ID, step.from, step.to, "")
		if err != nil {
			s.logError(ctx, "could not settle an already-transcribed memo", err,
				"memo", memo.ID, "from", step.from, "to", step.to)
			return
		}
		state = advanced.State
	}
}

func (s *Service) hold(ctx context.Context, memoID uuid.UUID, from, reason string) {
	if _, err := s.store.AdvanceMemoState(ctx, memoID, from, store.StateHeld, reason); err != nil {
		s.logError(ctx, "could not hold a memo", err, "memo", memoID)
	}
}

func (s *Service) logError(ctx context.Context, msg string, err error, args ...any) {
	s.logger.ErrorContext(ctx, msg, append([]any{"error", err}, args...)...)
}

// multipartBody builds the two-part submission the contract describes: `spec`
// as application/json and `audio` carrying its own media type.
func multipartBody(spec asrclient.JobSpec, mediaType string, audio []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="spec"`},
		"Content-Type":        {"application/json"},
	})
	if err != nil {
		return nil, "", err
	}
	if err := json.NewEncoder(part).Encode(spec); err != nil {
		return nil, "", err
	}

	part, err = mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="audio"; filename="memo"`},
		"Content-Type":        {mediaType},
	})
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(audio); err != nil {
		return nil, "", err
	}
	if err := mw.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), mw.FormDataContentType(), nil
}

// mediaTypeFor names what the recording is, for the audio part's own
// Content-Type.
//
// The codec CHRN-21 read from the headers is preferred over the filename,
// because a filename is display-only in this system and is never used to
// derive anything. The extension is the fallback for a memo whose headers
// could not be read, and audio/ogg is the fallback for that: every recording
// either ingest path has produced is Opus in Ogg, and the service probes the
// content anyway.
func mediaTypeFor(m store.Memo) string {
	if m.Codec != nil {
		switch strings.ToLower(*m.Codec) {
		case "opus", "vorbis":
			return "audio/ogg"
		}
	}
	if m.OriginalFilename != nil {
		switch strings.ToLower(filepath.Ext(*m.OriginalFilename)) {
		case ".webm":
			return "audio/webm"
		case ".m4a", ".mp4", ".aac":
			return "audio/mp4"
		case ".mp3":
			return "audio/mpeg"
		case ".wav":
			return "audio/wav"
		case ".ogg", ".opus", ".oga":
			return "audio/ogg"
		}
	}
	return "audio/ogg"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// BearerAuth is the request editor that presents Chronicle's client token.
//
// The token identifies Chronicle to the ASR service, and client_id is derived
// from it there — never sent as a field. It is added here rather than baked
// into the generated client so the generated file stays a pure function of the
// contract.
func BearerAuth(token string) asrclient.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}
