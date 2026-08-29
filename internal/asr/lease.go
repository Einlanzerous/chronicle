package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"slices"

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
// when the queue is empty. Global FIFO, no fairness: kept for the tests that
// exercise the lease itself, where one client submits everything and the
// ordering is not what is under test.
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

// ClaimForModel is Claim with CHRN-26 §5's ordering: round-robin by client,
// biased towards the model this worker already holds.
//
// It is the claim the resident worker uses, and the CAS shape above is
// untouched — `WHERE status = 'queued'` and SKIP LOCKED are both still here.
// ONLY THE ORDERING CHANGES, which is what §5 promised and what keeps CHRN-25's
// durable half intact.
//
// The order, stated so it cannot be composed two ways:
//
//  1. A job for a NON-RESIDENT model that has waited longer than maxWait.
//     Starvation beats residency: it is the only rule here with an unbounded
//     downside, and maxWait is therefore also the fairness bound under mixed
//     models that CHRN-29 publishes to client two.
//  2. Otherwise, round-robin among jobs for the RESIDENT model — the common
//     case, and the one CHRN-24's numbers describe. `resident` is the CALLER'S,
//     passed in rather than read from a column, so two workers holding
//     different models drain different halves of a mixed queue without either
//     switching. An empty string means "nothing loaded yet", which matches no
//     job and falls through to rule 3.
//  3. Otherwise — nothing queued for the resident model — round-robin among
//     everything, and the worker switches to whatever wins.
//
// ROUND-ROBIN IS `max(started_at)` PER CLIENT, NULLS FIRST: the client least
// recently served goes next, and a client that has never been served goes
// before all of them. Within one client the order stays oldest-first, because a
// client's own jobs have no ranking this service could justify inventing.
//
// The bookkeeping is in the query and not in memory, decided rather than left
// open (§5 [rev 2]): in-memory last-served is not round-robin at all with two
// workers — each alternates on its own view, and the pair can serve one client
// twice while the other waits. 0002_claim_fairness is the index that makes the
// aggregate cheap over a table CHRN-25 calls unbounded by design.
//
// The honest limitation, recorded because it is otherwise discovered as a bug:
// with a deep backlog on one client this gives the quiet client HALF the
// device, not priority.
func (s *Store) ClaimForModel(ctx context.Context, workerID string, leaseTTL time.Duration, resident string, maxWait time.Duration) (Job, error) {
	return scanJob(s.pool.QueryRow(ctx, `
		WITH client_last AS (
		    SELECT client_id, max(started_at) AS last_started
		      FROM jobs
		     GROUP BY client_id
		)
		UPDATE jobs
		   SET status = 'leased',
		       leased_by = $1,
		       lease_expires_at = now() + make_interval(secs => $2)
		 WHERE id = (
		           SELECT j.id
		             FROM jobs j
		             JOIN client_last c ON c.client_id = j.client_id
		            WHERE j.status = 'queued'
		              AND j.cancel_requested_at IS NULL
		            ORDER BY CASE
		                       WHEN j.model <> $3
		                        AND j.created_at < now() - make_interval(secs => $4) THEN 0
		                       WHEN j.model = $3 THEN 1
		                       ELSE 2
		                     END,
		                     c.last_started ASC NULLS FIRST,
		                     j.created_at
		            LIMIT 1
		            FOR UPDATE OF j SKIP LOCKED
		       )
		   AND status = 'queued'
		RETURNING `+jobColumns,
		workerID, leaseTTL.Seconds(), resident, maxWait.Seconds()))
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
	Status   string // where it went: queued, cancelled, or failed
	Attempts int
}

// ExhaustedCode is what a job carries when the retry ceiling stopped it.
//
// A CODE RATHER THAN A MESSAGE because a client branches on it: CHRN-27's pump
// must not answer a dead-lettered job by starting another one, which is what a
// generic failure would produce.
const ExhaustedCode = "retries_exhausted"

// wedgedReasons are the release reasons the LOWER ceiling applies to.
//
// CHRN-26 handed this ticket six reasons and the observation that `attempts`
// cannot tell them apart while they cost very differently. These two are the
// expensive ones: a job killed by a deadline spent five times its expected run
// getting nowhere, up to 200 s of a stalled queue per attempt on `small.en` and
// eleven minutes on `large-v3`. A crash costs one wasted claim, and the file
// that crashes a decoder is usually the file that crashes it every time — but
// that loop is cheap enough to give more rope.
var wedgedReasons = []string{"inference_deadline", "decode_deadline"}

// CeilingFor is the retry ceiling that applies to one release reason. The
// policy lives here rather than at the two call sites so that "which reasons
// are expensive" is answered in one place and cannot drift between the reaper
// and the worker.
func CeilingFor(reason string, max, wedged int) int {
	if slices.Contains(wedgedReasons, reason) {
		return wedged
	}
	return max
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
// `attempts` increments on every path that is not a cancellation, so the counter
// means "times this job's claim was lost" — and CHRN-28's ceiling is read from
// it here: a job that would come back for the (maxAttempts)th time is DEAD
// LETTERED instead, to `failed` with ExhaustedCode, rather than requeued into a
// loop nobody is watching.
func (s *Store) Reap(ctx context.Context, maxAttempts int) ([]Reaped, error) {
	rows, err := s.pool.Query(ctx, `
		WITH expired AS (
		    SELECT id,
		           CASE WHEN cancel_requested_at IS NOT NULL THEN 'cancelled'
		                WHEN attempts + 1 >= $3                THEN 'failed'
		                ELSE 'queued' END AS target
		      FROM jobs
		     WHERE status IN ('leased', 'running')
		       AND lease_expires_at < now()
		)
		UPDATE jobs j
		   SET status   = e.target,
		       attempts = CASE WHEN e.target = 'cancelled'
		                       THEN j.attempts ELSE j.attempts + 1 END,
		       audio    = CASE WHEN e.target = 'queued' THEN j.audio ELSE NULL END,
		       -- Unchanged on the cancelled branch: a cancellation is not a
		       -- release, and recording one would contradict this column's
		       -- own meaning on exactly the rows nobody re-reads.
		       last_release_reason = CASE WHEN e.target = 'cancelled'
		                                  THEN j.last_release_reason
		                                  ELSE 'lease_expired' END,
		       result   = CASE WHEN e.target = 'queued' THEN j.result
		                       ELSE jsonb_build_object(
		                              'job_id',   j.id,
		                              'status',   e.target,
		                              'partial',  true,
		                              'text',     '',
		                              'segments', '[]'::jsonb,
		                              'model',    'whisper.cpp/' || j.model,
		                              'backend',  $1::text)
		                            || CASE WHEN e.target = 'failed'
		                                    THEN jsonb_build_object('failure',
		                                           jsonb_build_object(
		                                             'code',    '`+ExhaustedCode+`',
		                                             'message', 'the retry ceiling stopped this job after ' ||
		                                                        (j.attempts + 1) || ' attempts'))
		                                    ELSE '{}'::jsonb END END,
		       result_purge_at = CASE WHEN e.target = 'queued'
		                              THEN j.result_purge_at
		                              ELSE now() + make_interval(secs => $2) END,
		       finished_at = CASE WHEN e.target = 'queued' THEN j.finished_at ELSE now() END,
		       started_at  = CASE WHEN e.target = 'queued' THEN NULL ELSE j.started_at END,
		       lease_expires_at = NULL,
		       leased_by = NULL
		  FROM expired e
		 WHERE j.id = e.id
		   -- The status and lease predicates are REPEATED here on purpose. The
		   -- CTE chose the target from one snapshot; without them a second
		   -- reaper that blocked on this row would apply its own choice to a
		   -- row already reaped and increment attempts twice.
		   AND j.status IN ('leased', 'running')
		   AND j.lease_expires_at < now()
		RETURNING j.id, j.status, j.attempts`,
		s.backend, s.resultTTL.Seconds(), maxAttempts)
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

// Released is one job Release moved, and why.
type Released struct {
	ID       uuid.UUID
	Status   string // where it went: queued, or cancelled
	Attempts int
	Reason   string
}

// Release hands a job this worker holds back to the queue, explicitly.
//
// IT EXISTS BECAUSE "THE REAPER HANDLES IT" WAS WRONG. The reaper is the path
// for asrd DYING. When the resident child dies, asrd is alive: it sees a
// connection reset from a request it made, and treating that as a transcription
// failure would permanently fail a memo that nothing was wrong with — one child
// crash, one lost memo. Waiting for the lease instead costs the TTL plus a reap
// interval, about 35 s of an idle GPU with a live worker sitting next to it.
//
// `running > queued` is ALREADY LEGAL in CHRN-25's trigger — it is the reaper's
// edge — so this is a new method over an existing edge and not a schema change.
// `leased > queued` is legal too, which is the path for a child that died while
// loading a model, before inference began.
//
// THE CANCEL CLAUSE IS NOT OPTIONAL AND IS COPIED FROM Reap DELIBERATELY. A
// child that dies while running a job somebody cancelled is a `running` row
// with cancel_requested_at set, and sending that back to `queued` raises AS004
// — a cancelled job may not return to the queue. The database would catch it,
// but catching it in the one path nobody tests is how a mechanical PR stops
// being mechanical, so such a job goes to `cancelled` with the reaper's
// terminal payload instead.
//
// `maxAttempts` is CHRN-28's ceiling for THIS reason — see CeilingFor. A job
// that would come back for the (maxAttempts)th time is dead-lettered to
// `failed` with ExhaustedCode instead, because a file that wedges the GPU on
// every attempt is a file that will wedge it on the next one too.
//
// `reason` travels with the outcome so exactly one line says why. It matters
// because a crash and a deadline breach both increment `attempts` and the
// counter cannot tell them apart — while they cost differently by a factor of
// five, which is CHRN-28's ceiling to set.
func (s *Store) Release(ctx context.Context, id uuid.UUID, workerID, reason string, maxAttempts int) (Released, error) {
	out := Released{Reason: reason}
	err := s.pool.QueryRow(ctx, `
		WITH held AS (
		    SELECT id,
		           CASE WHEN cancel_requested_at IS NOT NULL THEN 'cancelled'
		                WHEN attempts + 1 >= $5                THEN 'failed'
		                ELSE 'queued' END AS target
		      FROM jobs
		     WHERE id = $3 AND leased_by = $4 AND status IN ('leased', 'running')
		)
		UPDATE jobs j
		   SET status   = h.target,
		       attempts = CASE WHEN h.target = 'cancelled'
		                       THEN j.attempts ELSE j.attempts + 1 END,
		       audio    = CASE WHEN h.target = 'queued' THEN j.audio ELSE NULL END,
		       last_release_reason = CASE WHEN h.target = 'cancelled'
		                                  THEN j.last_release_reason
		                                  ELSE $6 END,
		       result   = CASE WHEN h.target = 'queued' THEN j.result
		                       ELSE jsonb_build_object(
		                              'job_id',   j.id,
		                              'status',   h.target,
		                              'partial',  true,
		                              'text',     '',
		                              'segments', '[]'::jsonb,
		                              'model',    'whisper.cpp/' || j.model,
		                              'backend',  $1::text)
		                            || CASE WHEN h.target = 'failed'
		                                    THEN jsonb_build_object('failure',
		                                           jsonb_build_object(
		                                             'code',    '`+ExhaustedCode+`',
		                                             'message', 'the retry ceiling stopped this job after ' ||
		                                                        (j.attempts + 1) || ' attempts; last reason: ' || $6::text))
		                                    ELSE '{}'::jsonb END END,
		       result_purge_at = CASE WHEN h.target = 'queued'
		                              THEN j.result_purge_at
		                              ELSE now() + make_interval(secs => $2) END,
		       finished_at = CASE WHEN h.target = 'queued' THEN j.finished_at ELSE now() END,
		       started_at  = CASE WHEN h.target = 'queued' THEN NULL ELSE j.started_at END,
		       lease_expires_at = NULL,
		       leased_by = NULL
		  FROM held h
		 WHERE j.id = h.id
		   -- Repeated for the reason Reap repeats them: the CTE chose from one
		   -- snapshot, and the lease may have moved since.
		   AND j.leased_by = $4
		   AND j.status IN ('leased', 'running')
		RETURNING j.id, j.status, j.attempts`,
		s.backend, s.resultTTL.Seconds(), id, workerID, maxAttempts, reason).Scan(&out.ID, &out.Status, &out.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		// The lease was lost between the failure and this call. Whoever holds
		// the job now is entitled to finish it; saying nothing is correct.
		return Released{}, ErrNotFound
	}
	if err != nil {
		return Released{}, fmt.Errorf("asr: release: %w", err)
	}
	return out, nil
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
