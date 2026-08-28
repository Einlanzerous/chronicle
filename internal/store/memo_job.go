package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Which ASR job a memo was submitted to, under which idempotency key.
//
// TIER 1. Chronicle still holds the audio, so losing a row here costs one
// repeated GPU run and nothing else — where losing a transcript costs the
// corpus. That asymmetry is the whole test, and it is the same one 0004 and
// 0005 applied.

// ErrJobInFlight reports that this memo already has an uncollected attempt.
// Refused by a partial unique index rather than by a check in Go: a slow poll
// and a fast tick would otherwise submit the same memo twice, which is a second
// GPU run and two results for one memo.
var ErrJobInFlight = errors.New("store: this memo already has a transcription attempt in flight")

// MemoJob is one transcription attempt.
type MemoJob struct {
	ID     uuid.UUID
	MemoID uuid.UUID

	// IdempotencyKey is written BEFORE the submit is sent. That ordering is
	// the entire point of this row: the key must be stable across HTTP retries
	// of one attempt, or the failure it prevents is not prevented.
	IdempotencyKey string

	Model       string
	AudioSHA256 string

	// JobID is nil until the submit returns. A row in that state is an attempt
	// that may or may not have reached the service, which is exactly the case
	// the key makes safe to resolve: re-submit with the same key and read the
	// reply.
	JobID       *uuid.UUID
	SubmittedAt *time.Time

	CollectedAt    *time.Time
	FailureCode    *string
	FailureMessage *string

	// SupersededAt is set when a deliberate retry declares this attempt spent.
	// See SupersedeMemoJobs.
	SupersededAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Settled reports whether this attempt is over, either way.
func (j MemoJob) Settled() bool { return j.CollectedAt != nil || j.FailureCode != nil }

const memoJobColumns = `id, memo_id, idempotency_key, model, audio_sha256,
	job_id, submitted_at, collected_at, failure_code, failure_message,
	superseded_at, created_at, updated_at`

func scanMemoJob(row pgx.Row) (MemoJob, error) {
	var j MemoJob
	err := row.Scan(&j.ID, &j.MemoID, &j.IdempotencyKey, &j.Model, &j.AudioSHA256,
		&j.JobID, &j.SubmittedAt, &j.CollectedAt, &j.FailureCode,
		&j.FailureMessage, &j.SupersededAt, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoJob{}, ErrNotFound
	}
	return j, err
}

// BeginTranscription records an attempt BEFORE anything is submitted, minting
// the idempotency key it will be sent under.
//
// The ordering is the requirement, not an implementation detail. CHRN-25 §3:
// the key is "minted per transcription attempt and persisted by the client
// before the request is sent". Chronicle submits, the process dies before it
// records the returned job id, it comes back and retries — with a persisted key
// that retry is a replay and the answer is the original job; without one it is
// a second job, the GPU transcribes the memo twice, and Chronicle has two
// results for one memo and no way to say which is the transcript.
//
// A fresh UUIDv4 per ATTEMPT, matching CHRN-18's "per recording and not per
// HTTP call". A deliberate re-transcription is a different attempt and gets a
// different key, which is why this is not derived from the content hash — a
// content-keyed job either refuses the second run or replays the first model's
// transcript to a request that asked for a different model.
func (s *Store) BeginTranscription(ctx context.Context, memoID uuid.UUID, model, audioSHA256 string) (MemoJob, error) {
	j, err := scanMemoJob(s.pool.QueryRow(ctx, `
		INSERT INTO tier1.memo_jobs (memo_id, idempotency_key, model, audio_sha256)
		VALUES ($1, $2, $3, $4)
		RETURNING `+memoJobColumns,
		memoID, uuid.NewString(), model, audioSHA256))

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return MemoJob{}, ErrJobInFlight
	}
	if err != nil {
		return MemoJob{}, fmt.Errorf("store: begin transcription: %w", err)
	}
	return j, nil
}

// RecordJobSubmitted stores the job id the service returned.
func (s *Store) RecordJobSubmitted(ctx context.Context, id, jobID uuid.UUID) (MemoJob, error) {
	j, err := scanMemoJob(s.pool.QueryRow(ctx, `
		UPDATE tier1.memo_jobs
		   SET job_id = $2, submitted_at = now(), updated_at = now()
		 WHERE id = $1
		RETURNING `+memoJobColumns, id, jobID))
	if err != nil {
		return MemoJob{}, fmt.Errorf("store: record job submitted: %w", err)
	}
	return j, nil
}

// RecordJobCollected marks the attempt done. Called AFTER the transcript is
// written, never before: if the process dies between the two, the attempt is
// collected again and the tier-2 upsert makes that a no-op — where the reverse
// order would lose a transcript that was never written.
func (s *Store) RecordJobCollected(ctx context.Context, id uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE tier1.memo_jobs SET collected_at = now(), updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: record job collected: %w", err)
	}
	return nil
}

// RecordJobFailure ends the attempt badly, with a reason a human can read.
func (s *Store) RecordJobFailure(ctx context.Context, id uuid.UUID, code, message string) error {
	if code == "" {
		// An attempt that failed for no stated reason is one nobody can act
		// on, and it would also leave the in-flight index still holding the
		// memo. Refused rather than defaulted.
		return fmt.Errorf("store: record job failure: a failure needs a code")
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE tier1.memo_jobs
		   SET failure_code = $2, failure_message = $3, updated_at = now()
		 WHERE id = $1`, id, code, nullable(message))
	if err != nil {
		return fmt.Errorf("store: record job failure: %w", err)
	}
	return nil
}

// UnsubmittedJobs returns attempts whose submit never completed — the row was
// written, then the process died or the service was unreachable.
//
// They are RESUMED rather than abandoned, by re-sending under the same key.
// That is the case the whole persist-before-send ordering exists for, and a
// sweep that only ever created new attempts would quietly make the ordering
// pointless.
func (s *Store) UnsubmittedJobs(ctx context.Context, limit int) ([]MemoJob, error) {
	return s.memoJobs(ctx, `
		SELECT `+memoJobColumns+` FROM tier1.memo_jobs
		 WHERE job_id IS NULL AND failure_code IS NULL
		 ORDER BY created_at
		 LIMIT $1`, limit)
}

// InFlightJobs returns attempts that were submitted and not yet settled.
func (s *Store) InFlightJobs(ctx context.Context, limit int) ([]MemoJob, error) {
	return s.memoJobs(ctx, `
		SELECT `+memoJobColumns+` FROM tier1.memo_jobs
		 WHERE job_id IS NOT NULL AND collected_at IS NULL AND failure_code IS NULL
		 ORDER BY submitted_at
		 LIMIT $1`, limit)
}

// LatestMemoJob returns the most recent attempt for a memo, settled or not.
func (s *Store) LatestMemoJob(ctx context.Context, memoID uuid.UUID) (MemoJob, error) {
	j, err := scanMemoJob(s.pool.QueryRow(ctx,
		`SELECT `+memoJobColumns+` FROM tier1.memo_jobs
		  WHERE memo_id = $1 ORDER BY created_at DESC LIMIT 1`, memoID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MemoJob{}, fmt.Errorf("store: latest memo job: %w", err)
	}
	return j, err
}

func (s *Store) memoJobs(ctx context.Context, query string, args ...any) ([]MemoJob, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: memo jobs: %w", err)
	}
	defer rows.Close()

	var out []MemoJob
	for rows.Next() {
		j, err := scanMemoJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CountMemoJobs reports how many attempts a memo has had that a deliberate
// retry has not declared spent.
//
// Read by the pump's crude attempt ceiling. The real retry policy — how many,
// with what backoff, what to do about a partial — is CHRN-28's; this exists so
// that until then a service which keeps losing jobs cannot put one memo through
// an unmetered loop of GPU runs.
//
// THE `superseded_at IS NULL` IS LOAD-BEARING and is not a filter for
// tidiness. Counting every row ever would make `chronicle retranscribe` a
// command that reports success and achieves nothing: the operator releases a
// memo from `held`, the next sweep counts the same rows, and the ceiling holds
// it again — leaving the memo untranscribable by any path the service or the
// CLI offers.
func (s *Store) CountMemoJobs(ctx context.Context, memoID uuid.UUID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tier1.memo_jobs
		  WHERE memo_id = $1 AND superseded_at IS NULL`, memoID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: count memo jobs: %w", err)
	}
	return n, nil
}

// SupersedeMemoJobs declares a memo's settled attempts spent, so the ceiling
// starts again. This is what makes a deliberate retry actually retry.
//
// SETTLED ONLY. An attempt still in flight is not superseded, because the
// partial unique index that keeps one attempt per memo would then admit a
// second while the first is still running — a second GPU run over the same
// audio, which is the failure the whole idempotency arrangement exists to
// prevent.
//
// The rows are kept, not deleted. They are the record of what was tried.
func (s *Store) SupersedeMemoJobs(ctx context.Context, memoID uuid.UUID) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE tier1.memo_jobs
		   SET superseded_at = now(), updated_at = now()
		 WHERE memo_id = $1
		   AND superseded_at IS NULL
		   AND (collected_at IS NOT NULL OR failure_code IS NOT NULL)`, memoID)
	if err != nil {
		return 0, fmt.Errorf("store: supersede memo jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}
