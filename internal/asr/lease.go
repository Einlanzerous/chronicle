package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
)

// The worker side of the job table: claim, start, renew, finish — and the
// reaper, which is the entire mechanism behind "job state survives kill -9".
//
// THE LEASE IS THE MECHANISM, NOT A SHUTDOWN HOOK. A hook does not run on
// kill -9, and kill -9 is the case the ticket names. A worker holds
// lease_expires_at and renews it while it works; the reaper returns any job
// whose lease has passed. Nothing here asks a dying process to do anything.

// Claim takes the oldest queued job for this worker, or returns ErrNotFound
// when the queue is empty.
//
// COMPARE-AND-SWAP, and this is inherited rather than invented: CHRN-18's
// review measured SIX OF SIX workers winning the same claim once the `from`
// predicate was removed from its state advance. The outer `AND status =
// 'queued'` is that predicate. SKIP LOCKED alone would also serialise this, and
// the predicate is kept anyway — belt and braces on the one operation where
// losing the race silently means the GPU runs two inferences at once.
func (s *Store) Claim(ctx context.Context, workerID string, leaseTTL time.Duration) (Job, error) {
	return scanJob(s.pool.QueryRow(ctx, `
		UPDATE jobs
		   SET status = 'leased',
		       leased_by = $1,
		       lease_expires_at = now() + make_interval(secs => $2)
		 WHERE id = (
		           SELECT id FROM jobs
		            WHERE status = 'queued'
		              AND cancel_requested_at IS NULL
		            ORDER BY created_at
		            LIMIT 1
		            FOR UPDATE SKIP LOCKED
		       )
		   AND status = 'queued'
		RETURNING `+jobColumns,
		workerID, leaseTTL.Seconds()))
}

// Audio returns the submitted bytes for a job this worker holds.
//
// They live in the database rather than in a spool directory because a
// lease-expired job returns to `queued` needing them again, and a directory
// would reintroduce a filesystem dependency, a volume to mount, and a second
// cleanup path that can disagree with the first.
func (s *Store) Audio(ctx context.Context, id uuid.UUID, workerID string) ([]byte, error) {
	var b []byte
	err := s.pool.QueryRow(ctx,
		`SELECT audio FROM jobs WHERE id = $1 AND leased_by = $2`, id, workerID).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if b == nil {
		// The lease was lost and the job reached a terminal state under us.
		return nil, ErrNotFound
	}
	return b, nil
}

// Start moves a claimed job to running: inference is actually beginning.
//
// Separate from Claim because `leased` and `running` answer different
// questions, and a worker that dies between them wants different accounting
// from one that dies mid-inference. CHRN-26 is the ticket that benefits;
// keeping the edge here means it does not have to add it later against live
// rows.
func (s *Store) Start(ctx context.Context, id uuid.UUID, workerID string, leaseTTL time.Duration) (Job, error) {
	return scanJob(s.pool.QueryRow(ctx, `
		UPDATE jobs
		   SET status = 'running',
		       started_at = now(),
		       lease_expires_at = now() + make_interval(secs => $3)
		 WHERE id = $1 AND leased_by = $2 AND status = 'leased'
		RETURNING `+jobColumns,
		id, workerID, leaseTTL.Seconds()))
}

// RenewLease extends this worker's hold and reports whether cancellation has
// been requested. One round trip for both because a worker that renews without
// checking is a worker that finishes a job somebody cancelled.
//
// A false `held` means the lease was lost — reaped, or the job went terminal
// elsewhere. The worker's correct response is to stop, not to finish and write
// a result over whatever took its place.
func (s *Store) RenewLease(ctx context.Context, id uuid.UUID, workerID string, leaseTTL time.Duration) (held, cancelling bool, err error) {
	err = s.pool.QueryRow(ctx, `
		UPDATE jobs
		   SET lease_expires_at = now() + make_interval(secs => $3)
		 WHERE id = $1 AND leased_by = $2 AND status IN ('leased', 'running')
		RETURNING (cancel_requested_at IS NOT NULL)`,
		id, workerID, leaseTTL.Seconds()).Scan(&cancelling)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, cancelling, nil
}

// Finish writes a terminal result and releases the audio.
//
// `partial` is written from what the CALLER observed about its own run. It is
// never derived here from covered_ms against audio_duration_ms — whisper emits
// segments only where there is speech, so an ordinary memo with trailing
// silence has covered_ms short of its duration on a perfectly complete run, and
// a pruner gated on that inference would mark most of a corpus not-durable and
// never fire.
//
// The audio is nulled in the same statement as the status, not in a follow-up:
// the jobs_audio_present constraint holds that a terminal job carries no bytes,
// so the two cannot drift even if a future caller forgets.
func (s *Store) Finish(ctx context.Context, id uuid.UUID, workerID string, res asrclient.Result) (Job, error) {
	// A partial success is a real outcome — CHRN-28 decides what to do about
	// one — so it is stored as written rather than refused here. What it is
	// NOT is a durable transcript, and the predicate that says so is `succeeded
	// AND NOT partial`, read on the client side.
	payload, err := json.Marshal(res)
	if err != nil {
		return Job{}, fmt.Errorf("asr: encode result: %w", err)
	}
	return scanJob(s.pool.QueryRow(ctx, `
		UPDATE jobs
		   SET status = $3,
		       partial = $4,
		       result = $5,
		       result_purge_at = now() + make_interval(secs => $6),
		       audio = NULL,
		       finished_at = now(),
		       lease_expires_at = NULL
		 WHERE id = $1 AND leased_by = $2 AND status IN ('leased', 'running')
		RETURNING `+jobColumns,
		id, workerID, string(res.Status), res.Partial, payload, s.resultTTL.Seconds()))
}

// Reaped is one job the reaper moved, and why.
type Reaped struct {
	ID       uuid.UUID
	Status   string // where it went: queued, or cancelled
	Attempts int
}

// Reap returns every job whose lease has expired.
//
// This is the whole of `kill -9` survival: a worker that dies stops renewing,
// its lease passes, and the job comes back. Nothing runs in the dying process.
//
// THE CANCELLATION CLAUSE IS THE ONE TO READ. A running job that was cancelled
// and whose worker then died must go to `cancelled`, NEVER back to `queued` —
// requeueing it would re-run work somebody explicitly stopped, which is the one
// outcome cancel exists to prevent. The database refuses the other answer too
// (constraint AS004 in 0001_jobs.up.sql); this is not the only line standing
// between that and a re-run.
//
// `attempts` increments only on the requeue path, so the counter means "times
// this job's claim was lost and it went back" — which is what CHRN-28 will set
// a ceiling against.
func (s *Store) Reap(ctx context.Context) ([]Reaped, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE jobs
		   SET status   = CASE WHEN cancel_requested_at IS NOT NULL
		                       THEN 'cancelled' ELSE 'queued' END,
		       attempts = CASE WHEN cancel_requested_at IS NOT NULL
		                       THEN attempts ELSE attempts + 1 END,
		       audio    = CASE WHEN cancel_requested_at IS NOT NULL
		                       THEN NULL ELSE audio END,
		       result   = CASE WHEN cancel_requested_at IS NOT NULL
		                       THEN jsonb_build_object(
		                              'job_id',   id,
		                              'status',   'cancelled',
		                              'partial',  true,
		                              'text',     '',
		                              'segments', '[]'::jsonb,
		                              'model',    'whisper.cpp/' || model,
		                              'backend',  $1::text)
		                       ELSE result END,
		       result_purge_at = CASE WHEN cancel_requested_at IS NOT NULL
		                              THEN now() + make_interval(secs => $2)
		                              ELSE result_purge_at END,
		       finished_at = CASE WHEN cancel_requested_at IS NOT NULL
		                          THEN now() ELSE finished_at END,
		       started_at  = CASE WHEN cancel_requested_at IS NOT NULL
		                          THEN started_at ELSE NULL END,
		       lease_expires_at = NULL,
		       leased_by = NULL
		 WHERE status IN ('leased', 'running')
		   AND lease_expires_at < now()
		RETURNING id, status, attempts`,
		s.backend, s.resultTTL.Seconds())
	if err != nil {
		return nil, fmt.Errorf("asr: reap: %w", err)
	}
	defer rows.Close()

	var out []Reaped
	for rows.Next() {
		var r Reaped
		if err := rows.Scan(&r.ID, &r.Status, &r.Attempts); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PurgeResults drops result PAYLOADS that have aged out. The rows stay, which
// is what lets a late fetch answer 410 Gone rather than 404, and what keeps the
// idempotency uniqueness holding forever against a row that is still there.
func (s *Store) PurgeResults(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE jobs SET result = NULL WHERE result IS NOT NULL AND result_purge_at < now()`)
	if err != nil {
		return 0, fmt.Errorf("asr: purge results: %w", err)
	}
	return tag.RowsAffected(), nil
}

// terminalResult builds the payload for a job that ends without a run — today,
// a cancellation from the queue.
//
// It is written rather than left NULL so that "terminal implies a result, until
// it is purged" holds for every path. A client that has to distinguish "no
// result because cancelled" from "no result because purged" by inspecting
// timestamps is a client that will get it wrong.
//
// `partial` is TRUE on every one of these: no run completed, so this is
// emphatically not a durable transcript, and a client that reads only that
// field still reaches the safe answer.
func (s *Store) terminalResult(id uuid.UUID, model string, status asrclient.ResultStatus, failure *asrclient.Failure) ([]byte, error) {
	return json.Marshal(asrclient.Result{
		JobId:    id,
		Status:   status,
		Partial:  true,
		Text:     "",
		Segments: []asrclient.Segment{},
		Model:    "whisper.cpp/" + model,
		Backend:  s.backend,
		Failure:  failure,
	})
}
