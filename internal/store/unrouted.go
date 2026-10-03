package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The three reads the proposal pump needs (CHRN-126). All on Tier1Store, all
// SELECTs, and all inside what chronicle_tier1 already holds: tier2.memos and
// tier2.transcripts by 0007's grant, tier1.* by 0001's default privileges. NO
// MIGRATION AND NO GRANT, which is the plan's tier-boundary section; the pump's
// tests run as chronicle_tier1, so a statement here that needed more would fail
// them with `permission denied` rather than pass on the owner's pool.

// UnroutedMemos returns memos the Scribe still owes a proposal under one
// proposer, newest captured_at first.
//
// "OWES" IS ⚖1's DEFINITION OF CURRENT, AND NOTHING ELSE: a memo is routed when
// a row exists for (memo_id, proposer), whatever its status. A `valid`,
// `needs_input` or `invalid` row all count — `invalid` is a final answer for
// this proposer, and a person routes it by hand. A row under another proposer
// does not count, so a new model or prompt version re-routes every memo still
// `transcribed` and nothing that has been decided.
//
// THE TRANSCRIPT IS NOT COMPARED, and that is correct only while nothing can
// give a `transcribed` memo a new transcript. Nothing can: tier2.memos_guard
// lets `transcribed` go only to triaged, held or discarded, and `chronicle
// retranscribe` refuses anything that is not `held`. A ticket that adds such a
// path owes this query a transcript axis — and a column, because a failed
// re-run deliberately leaves transcript_id alone (saveFailedProposal).
//
// NEWEST FIRST, the opposite of the triage screen, on purpose: the memo
// somebody recorded a minute ago is routed ahead of a backlog, so "within
// minutes of its transcript" holds during a backfill and not only on an idle
// queue. The backlog is finite and drains behind it.
//
// HELD-FOR-TRIAGE MEMOS ARE INCLUDED. A tier1.triage_holds row defers a
// decision; the memo is still `transcribed` and still owed a proposal, which is
// what it shows when it is released.
//
// exclude is the pump's per-memo backoff, applied here rather than after the
// fact so the limit counts only memos that can actually be tried: a run of
// failing memos at the head of the order cannot fill the window.
func (s *Tier1Store) UnroutedMemos(ctx context.Context, proposer string, exclude []uuid.UUID, limit int) ([]Memo, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	}
	// A nil slice encodes as NULL, and `id = ANY(NULL)` is NULL rather than
	// false — which would make NOT(...) exclude every memo there is. The
	// COALESCE below says the same thing in SQL, so neither side relies on the
	// other remembering.
	if exclude == nil {
		exclude = []uuid.UUID{}
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+prefixed(memoColumns, "m")+`
		   FROM tier2.memos m
		  WHERE m.state = $5
		    AND EXISTS (SELECT 1 FROM tier2.transcripts
		                 WHERE memo_id = m.id AND `+DurableClause+`)
		    AND NOT EXISTS (SELECT 1 FROM tier1.memo_proposals p
		                     WHERE p.memo_id = m.id AND p.proposer = $1)
		    AND NOT (m.id = ANY(COALESCE($4::uuid[], '{}')))
		  ORDER BY m.captured_at DESC, m.id
		  LIMIT $6`,
		proposer, SufficientRunners, SufficientModels, exclude, StateTranscribed, limit)
	if err != nil {
		return nil, fmt.Errorf("store: unrouted memos: %w", err)
	}
	defer rows.Close()

	var out []Memo
	for rows.Next() {
		m, err := scanMemo(rows)
		if err != nil {
			return nil, fmt.Errorf("store: unrouted memos: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: unrouted memos: %w", err)
	}
	return out, nil
}

// TranscriptionLoad is what Chronicle has asked of the GPU and not yet had
// back: the two halves of the pump's busy signal (CHRN-126 ⚖2), counted
// separately so the long-yield warning can say which one is holding it.
type TranscriptionLoad struct {
	// Captured is memos the transcription pump will submit on its next tick.
	Captured int
	// InFlight is attempts submitted to asrd and not yet settled, within the
	// window TranscriptionBusy was given.
	InFlight int
}

// Busy reports whether routing should stay off the GPU.
func (l TranscriptionLoad) Busy() bool { return l.Captured > 0 || l.InFlight > 0 }

// TranscriptionBusy reads the busy signal: a memo in `captured` with its audio
// still on disk, or a tier1.memo_jobs attempt submitted within inFlightWithin
// and not yet collected or failed.
//
// `queued` IS DELIBERATELY NOT COUNTED. A memo waiting out the transcription
// pump's retry backoff, or one whose submit cannot reach asrd, is not using the
// GPU, and counting it would stop all routing for as long as transcription is
// broken.
//
// THE IN-FLIGHT CLAUSE IS BOUNDED for the same reason. An unsettled attempt is
// settled only by the transcription pump reaching asrd; with asrd unreachable
// the row stays in flight, the GPU sits idle, and an unbounded clause would
// hold routing off for as long as the outage lasts. The predicate is
// InFlightJobs' plus that bound.
func (s *Tier1Store) TranscriptionBusy(ctx context.Context, inFlightWithin time.Duration) (TranscriptionLoad, error) {
	var l TranscriptionLoad
	err := s.pool.QueryRow(ctx,
		`SELECT
		   (SELECT count(*) FROM tier2.memos
		     WHERE state = $1 AND audio_pruned_at IS NULL),
		   (SELECT count(*) FROM tier1.memo_jobs
		     WHERE job_id IS NOT NULL AND collected_at IS NULL AND failure_code IS NULL
		       AND submitted_at > now() - make_interval(secs => $2))`,
		StateCaptured, inFlightWithin.Seconds()).Scan(&l.Captured, &l.InFlight)
	if err != nil {
		return TranscriptionLoad{}, fmt.Errorf("store: transcription busy: %w", err)
	}
	return l, nil
}

// MemoState reads one memo's state on the tier-1 pool.
//
// It exists for the pump's last look before it writes: a person can decide an
// `absent` card by hand while its generation is running, and a proposal for a
// memo that is no longer `transcribed` is a row nothing will read. The re-read
// narrows that window; it does not close it, and does not need to.
func (s *Tier1Store) MemoState(ctx context.Context, memoID uuid.UUID) (string, error) {
	var state string
	err := s.pool.QueryRow(ctx, `SELECT state FROM tier2.memos WHERE id = $1`, memoID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: memo state: %w", err)
	}
	return state, nil
}
