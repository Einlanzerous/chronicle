package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Einlanzerous/chronicle/asr/internal/wire"
)

// The five STORED statuses. `cancelling` is not here because it is not stored:
// it is derived on the wire from `running` plus a cancellation request, and
// WireStatus is the only place that derivation happens.
const (
	StatusQueued    = "queued"
	StatusLeased    = "leased"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Job is one transcription attempt. It sits beside the queries that return it
// rather than in a types-only package, matching the house layout.
type Job struct {
	ID             uuid.UUID
	ClientID       string
	IdempotencyKey string
	AudioSHA256    string
	AudioMediaType string
	AudioBytes     int64
	Model          string
	Language       string // "" when the client expressed no preference

	Status   string
	Attempts int

	LeaseExpiresAt    *time.Time
	LeasedBy          string
	CancelRequestedAt *time.Time

	// Partial is NULL until a run finishes. Never derive it from the
	// timestamps: see the column comment in 0001_jobs.up.sql.
	Partial *bool

	HasResult     bool
	ResultPurgeAt *time.Time

	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// Terminal reports whether the job has left the queue for good. A job leaves
// the queue ONLY by reaching one of these three, which is what makes "no job is
// ever dropped" a property rather than an aspiration.
func (j Job) Terminal() bool {
	switch j.Status {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// WireStatus is what a client sees. It is the ONLY place `cancelling` comes
// from: a running job whose cancellation has been requested. The derivation
// lives in one function so the two codebases that consume this enum cannot
// disagree about when it applies.
func (j Job) WireStatus() wire.JobStatus {
	if j.Status == StatusRunning && j.CancelRequestedAt != nil {
		return wire.JobStatusCancelling
	}
	return wire.JobStatus(j.Status)
}

// jobColumns is the projection every scanJob call expects, in order. One
// constant so a column added to the table cannot be added to five queries and
// forgotten in a sixth.
const jobColumns = `
	id, client_id, idempotency_key, audio_sha256, audio_media_type, audio_bytes,
	model, language, status, attempts, lease_expires_at, leased_by,
	cancel_requested_at, partial, (result IS NOT NULL), result_purge_at,
	created_at, updated_at, started_at, finished_at`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var language, leasedBy *string
	err := row.Scan(
		&j.ID, &j.ClientID, &j.IdempotencyKey, &j.AudioSHA256, &j.AudioMediaType,
		&j.AudioBytes, &j.Model, &language, &j.Status, &j.Attempts,
		&j.LeaseExpiresAt, &leasedBy, &j.CancelRequestedAt, &j.Partial,
		&j.HasResult, &j.ResultPurgeAt, &j.CreatedAt, &j.UpdatedAt,
		&j.StartedAt, &j.FinishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	if language != nil {
		j.Language = *language
	}
	if leasedBy != nil {
		j.LeasedBy = *leasedBy
	}
	return j, nil
}

// SubmitInput is one submission: the spec the client sent, the bytes, and the
// key it minted before sending either.
type SubmitInput struct {
	ClientID       string
	IdempotencyKey string
	AudioSHA256    string
	AudioMediaType string
	Audio          []byte
	Model          string
	Language       string
}

// Submit records a job, or returns the one this key already named.
//
// INSERT-THEN-COMPARE, never check-then-insert. CHRN-18's Done-when #3 exists
// because a check-then-insert "fails against a design that passes" a plain
// concurrency test, and the race here is between two retries of ONE attempt —
// precisely the situation this key is for. The uniqueness is the index
// jobs_client_key; this function reads the conflict rather than trying to
// avoid it.
//
// Returns created=true for a new job, created=false for a replay, and
// ErrKeyMismatch when the key was reused for a different spec or different
// audio.
func (s *Store) Submit(ctx context.Context, in SubmitInput) (job Job, created bool, err error) {
	var language *string
	if in.Language != "" {
		language = &in.Language
	}

	// Two passes at most. The second exists for one narrow case: the
	// conflicting transaction ROLLED BACK between our insert and our read, so
	// the row we conflicted with is not there to return. Retrying is correct
	// and terminating, because a rolled-back insert leaves nothing to conflict
	// with the next time round.
	for attempt := 0; attempt < 2; attempt++ {
		row := s.pool.QueryRow(ctx, `
			INSERT INTO jobs (client_id, idempotency_key, audio_sha256,
			                  audio_media_type, audio, audio_bytes, model, language)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (client_id, idempotency_key) DO NOTHING
			RETURNING `+jobColumns,
			in.ClientID, in.IdempotencyKey, in.AudioSHA256, in.AudioMediaType,
			in.Audio, len(in.Audio), in.Model, language)

		job, err = scanJob(row)
		if err == nil {
			return job, true, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Job{}, false, fmt.Errorf("asr: submit: %w", err)
		}

		// The insert conflicted. READ COMMITTED takes a fresh snapshot per
		// statement and ON CONFLICT waited for the other transaction to
		// settle, so a committed row is visible now.
		existing, err := s.byKey(ctx, in.ClientID, in.IdempotencyKey)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return Job{}, false, err
		}

		// "Same key, same spec, same audio hash" is a replay. Anything else
		// asked for a different thing under a handle that already means
		// something, and the client's move is a fresh key.
		if existing.AudioSHA256 != in.AudioSHA256 ||
			existing.Model != in.Model ||
			existing.Language != in.Language {
			return existing, false, ErrKeyMismatch
		}
		return existing, false, nil
	}
	return Job{}, false, fmt.Errorf("asr: submit: key %q conflicted twice with no surviving row", in.IdempotencyKey)
}

func (s *Store) byKey(ctx context.Context, clientID, key string) (Job, error) {
	return scanJob(s.pool.QueryRow(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE client_id = $1 AND idempotency_key = $2`,
		clientID, key))
}

// Get returns one of THIS CLIENT'S jobs. Scoped in the query rather than
// checked afterwards: a lookup that can return another client's row and relies
// on the caller to notice is a lookup that will eventually be called by
// somebody who does not.
func (s *Store) Get(ctx context.Context, clientID string, id uuid.UUID) (Job, error) {
	return scanJob(s.pool.QueryRow(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE id = $1 AND client_id = $2`, id, clientID))
}

// QueueDepth counts jobs that have not reached a terminal state. Reported by
// /readyz so a backed-up service is visible without a database session.
func (s *Store) QueueDepth(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM jobs WHERE status IN ('queued', 'leased', 'running')`).Scan(&n)
	return n, err
}

// Result returns the stored result payload for a terminal job.
//
// The three failures are deliberately distinct, and the handler maps each to
// its own status: not this client's job (404), not finished yet (409), and
// finished but purged (410). Collapsing the last two would tell a client that
// waited too long that its transcription failed.
func (s *Store) Result(ctx context.Context, clientID string, id uuid.UUID) (wire.Result, error) {
	var raw []byte
	var status string
	var purgeAt *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT result, status, result_purge_at FROM jobs WHERE id = $1 AND client_id = $2`,
		id, clientID).Scan(&raw, &status, &purgeAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return wire.Result{}, ErrNotFound
	}
	if err != nil {
		return wire.Result{}, err
	}
	if !(Job{Status: status}).Terminal() {
		return wire.Result{}, ErrNotTerminal
	}
	if raw == nil {
		return wire.Result{}, ErrResultPurged
	}
	var out wire.Result
	if err := json.Unmarshal(raw, &out); err != nil {
		return wire.Result{}, fmt.Errorf("asr: decode stored result: %w", err)
	}
	return out, nil
}

// Cancel asks for a job to stop, and is idempotent.
//
//   - queued / leased  -> cancelled immediately, by CAS, audio released.
//   - running          -> cancel_requested_at set; the worker observes it and
//     stops renewing its lease. On the wire the job then reads `cancelling`.
//   - already terminal -> a NO-OP returning the terminal state. Not an error:
//     a client that crashed after cancelling and retries must not receive a
//     409 for having succeeded.
func (s *Store) Cancel(ctx context.Context, clientID string, id uuid.UUID) (Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// FOR UPDATE so a concurrent claim cannot move the job between the read
	// and the write. The alternative is a CAS retry loop for a call that is
	// made once per job by a human-scale action.
	var status, model string
	err = tx.QueryRow(ctx,
		`SELECT status, model FROM jobs WHERE id = $1 AND client_id = $2 FOR UPDATE`,
		id, clientID).Scan(&status, &model)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}

	switch {
	case (Job{Status: status}).Terminal():
		// Nothing to do, and deliberately no write: rewriting a terminal row
		// would move updated_at and stamp a cancellation request onto a job
		// that succeeded.
	case status == StatusQueued || status == StatusLeased:
		var payload []byte
		payload, err = s.terminalResult(id, model, wire.ResultStatusCancelled, nil)
		if err != nil {
			return Job{}, err
		}
		_, err = tx.Exec(ctx, `
			UPDATE jobs SET status = 'cancelled',
			                cancel_requested_at = COALESCE(cancel_requested_at, now()),
			                audio = NULL,
			                lease_expires_at = NULL,
			                leased_by = NULL,
			                finished_at = now(),
			                result = $2,
			                result_purge_at = now() + make_interval(secs => $3)
			 WHERE id = $1`,
			id, payload, s.resultTTL.Seconds())
	default: // running
		// COALESCE, not an overwrite: the trigger refuses to change a
		// cancellation request once made, which is what stops the reaper rule
		// below being defeated by a second call.
		_, err = tx.Exec(ctx,
			`UPDATE jobs SET cancel_requested_at = COALESCE(cancel_requested_at, now()) WHERE id = $1`, id)
	}
	if err != nil {
		return Job{}, err
	}

	job, err := scanJob(tx.QueryRow(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id))
	if err != nil {
		return Job{}, err
	}
	return job, tx.Commit(ctx)
}
