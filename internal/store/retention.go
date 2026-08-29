package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Einlanzerous/chronicle/internal/audio"
)

// CHRN-22, the retention pruner's half of the store. The decision is
// docs/decisions/chrn-22-retention-pruner.md and this file carries what it
// concluded, not the argument.
//
// THE PREDICATE IS ONE STRING AND IT IS USED TWICE: by the dry run as a SELECT,
// and by the mark as the WHERE of a compare-and-swap. "A dry run lists exactly
// what a real run would delete" is then true by construction rather than by a
// test comparing two queries that can drift apart in the next PR.
//
// RetentionStatus and HeldBackFromPruning re-express the same conditions as a
// CASE and as a negation rather than reusing the constant, because neither
// shape can be built from a conjunction meant for a WHERE. They agree with it
// today and the tests below pin each branch — but the drift-proof property
// belongs to the two above, and saying otherwise would be claiming a guarantee
// this file does not provide.

// Retention statuses. They are what the UI renders and what the dry run
// reports, and they are a STATUS RATHER THAN A DATE because for a memo with no
// durable transcript there is no date the job will use — the label would
// otherwise pass and nothing would happen.
const (
	// RetentionPruned is checked FIRST. It is the state this job produces, so
	// a status function without it has an undefined case on exactly the rows
	// the pruner creates.
	RetentionPruned = "pruned"

	RetentionStatusPinned = "pinned"

	// RetentionAwaitingTranscript renders as "prunes when transcribed", which
	// is also the invitation to pin ahead.
	RetentionAwaitingTranscript = "awaiting_transcript"

	// RetentionDiscardPending is a DISCARD NOW memo whose gate is satisfied:
	// it goes at the next sweep. Named apart from the `discard_now` retention
	// VALUE it reads, so no call site has to ask which one is meant.
	RetentionDiscardPending = "discard_pending"

	RetentionScheduled = "scheduled"
)

// prunableClause is the whole of "may this memo's audio be deleted".
//
// $1 is the window in seconds, $2 the runner allow-list, $3 the model
// allow-list. It reads `m`, so every caller aliases tier2.memos as m.
//
// IT DOES NOT READ `state`. CHRN-18 §6 hands that rule over in one line: a
// destructive job must not rest on a second, softer fact, and a bug in the
// state machine must not be able to become data loss. So a discarded memo is
// treated exactly like any other.
const prunableClause = `
	    m.audio_pruned_at IS NULL
	AND m.retention <> 'forever'
	AND (m.retention = 'discard_now'
	     OR m.captured_at < now() - make_interval(secs => $1))
	AND EXISTS (SELECT 1 FROM tier2.transcripts
	             WHERE memo_id = m.id AND ` + DurableClause + `)`

// PrunableMemo is one recording the pruner may delete, with what it needs to
// find the file. Nothing here is read from a path column, because there is no
// path column: CHRN-23 made the path derivable so it could not become a second
// source of truth.
type PrunableMemo struct {
	MemoID      uuid.UUID
	AuthorID    uuid.UUID
	ContentHash string
	ByteSize    int64
	CapturedAt  time.Time
	Retention   string
}

// Ref is where this memo's audio lives.
func (p PrunableMemo) Ref() audio.Ref {
	return audio.Ref{AuthorID: p.AuthorID, ContentHash: p.ContentHash}
}

// PrunableAudio is the dry run: exactly the rows a real sweep would act on, in
// the order it would act on them.
func (s *Store) PrunableAudio(ctx context.Context, window time.Duration, limit int) ([]PrunableMemo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.author_id, m.content_hash, m.byte_size, m.captured_at, m.retention
		  FROM tier2.memos m
		 WHERE `+prunableClause+`
		 ORDER BY m.captured_at
		 LIMIT $4`,
		window.Seconds(), SufficientRunners, SufficientModels, limit)
	if err != nil {
		return nil, fmt.Errorf("store: prunable audio: %w", err)
	}
	defer rows.Close()

	var out []PrunableMemo
	for rows.Next() {
		var p PrunableMemo
		if err := rows.Scan(&p.MemoID, &p.AuthorID, &p.ContentHash, &p.ByteSize,
			&p.CapturedAt, &p.Retention); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkAudioPruned claims one memo's audio for deletion, and reports whether it
// got it.
//
// A COMPARE-AND-SWAP OVER THE WHOLE PREDICATE, not an update following a check.
// The window between reading a row and unlinking its file is the window in
// which a person pins the memo, and under a plain `WHERE id = $1` their pin
// would be honoured by the database and ignored by the pruner. Re-evaluating
// everything here closes it, in the same shape AdvanceMemoState uses.
//
// It is called BEFORE the unlink, never after. A crash between the two leaves a
// file with no memo pointing at it — an orphan, which the storage report counts
// and this job may retry. The other order leaves a memo claiming audio that is
// gone, which is indistinguishable from data loss.
func (s *Store) MarkAudioPruned(ctx context.Context, memoID uuid.UUID, window time.Duration) (bool, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `
		UPDATE tier2.memos m
		   SET audio_pruned_at = now()
		 WHERE m.id = $4
		   AND `+prunableClause+`
		RETURNING m.id`,
		window.Seconds(), SufficientRunners, SufficientModels, memoID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: mark audio pruned: %w", err)
	}
	return true, nil
}

// RetentionStatus is what will happen to one memo's audio, and when.
//
// The pruner is this function evaluated over the table — same clause, same
// order — which is what makes "the date the UI shows is the date the job uses"
// hold by construction. `at` is set only for `scheduled` and `pruned`; for
// everything else there is no date, and inventing one would be the label lying.
func (s *Store) RetentionStatus(ctx context.Context, memoID uuid.UUID, window time.Duration) (status string, at *time.Time, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT CASE
		         WHEN m.audio_pruned_at IS NOT NULL THEN '`+RetentionPruned+`'
		         WHEN m.retention = 'forever'       THEN '`+RetentionStatusPinned+`'
		         WHEN NOT EXISTS (SELECT 1 FROM tier2.transcripts
		                           WHERE memo_id = m.id AND `+DurableClause+`)
		                                            THEN '`+RetentionAwaitingTranscript+`'
		         WHEN m.retention = 'discard_now'   THEN '`+RetentionDiscardPending+`'
		         ELSE '`+RetentionScheduled+`'
		       END,
		       CASE
		         WHEN m.audio_pruned_at IS NOT NULL THEN m.audio_pruned_at
		         WHEN m.retention = 'forever' THEN NULL
		         WHEN NOT EXISTS (SELECT 1 FROM tier2.transcripts
		                           WHERE memo_id = m.id AND `+DurableClause+`) THEN NULL
		         WHEN m.retention = 'discard_now' THEN NULL
		         ELSE m.captured_at + make_interval(secs => $1)
		       END
		  FROM tier2.memos m
		 WHERE m.id = $4`,
		window.Seconds(), SufficientRunners, SufficientModels, memoID).Scan(&status, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, fmt.Errorf("store: retention status: %w", err)
	}
	return status, at, nil
}

// HeldBackFromPruning counts memos past their window that the gate is keeping.
//
// Reported by the dry run because it is the visible half of an accepted gap: a
// memo discarded before it was transcribed is never picked up by the pump, so
// its gate can never be satisfied and its audio stays indefinitely — under the
// one setting where the person said to throw it away. That is the safe
// direction and the rule gets no exception clause, but it should be a number
// somebody can see rather than a silence.
func (s *Store) HeldBackFromPruning(ctx context.Context, window time.Duration) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM tier2.memos m
		 WHERE m.audio_pruned_at IS NULL
		   AND m.retention <> 'forever'
		   AND (m.retention = 'discard_now'
		        OR m.captured_at < now() - make_interval(secs => $1))
		   AND NOT EXISTS (SELECT 1 FROM tier2.transcripts
		                    WHERE memo_id = m.id AND `+DurableClause+`)`,
		window.Seconds(), SufficientRunners, SufficientModels).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: held back from pruning: %w", err)
	}
	return n, nil
}

// AudioPrunedFor reports whether this author's memo for these bytes exists AND
// has already had its audio pruned.
//
// CHRN-22 Ruling 2: A MEMO'S AUDIO IS DELIVERED ONCE. Once pruned, pruned.
// Both arrival paths ask this before accepting a transfer, because the
// alternative — clear `audio_pruned_at` and let the file land — cannot work:
// `captured_at` is immutable (CH002), so a memo captured sixty days ago that is
// re-uploaded today is already past its deadline and the next sweep deletes it
// again. A re-delivery is not new information about when a memo was captured,
// what its retention is, or whether it has a transcript.
//
// FALSE FOR A MEMO THAT DOES NOT EXIST, and false for one whose audio is merely
// missing. That second case is CHRN-23's `missing` and the self-healing path
// CHRN-20 built for it stays exactly as it was: a crash between the rename and
// the memo row is repaired by the client sending the bytes again.
func (s *Store) AudioPrunedFor(ctx context.Context, authorID uuid.UUID, contentHash string) (bool, error) {
	var pruned bool
	err := s.pool.QueryRow(ctx, `
		SELECT audio_pruned_at IS NOT NULL FROM tier2.memos
		 WHERE author_id = $1 AND content_hash = $2`,
		authorID, contentHash).Scan(&pruned)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("store: audio pruned check: %w", err)
	}
	return pruned, nil
}
