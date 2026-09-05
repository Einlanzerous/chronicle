package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CHRN-34's HOLD: a routing decision parked, without pretending the recording
// changed.
//
// These live on Store and not on Tier1Store even though the table is tier 1,
// and the reason is what Tier1Store is FOR. That type exists so derived work —
// Scribe — reaches the database on a role with no tier-2 write anywhere. A hold
// is not derived work: no re-run produces one, a person does, through the API,
// on the main role. Putting it there would widen "the pool Scribe uses" into
// "the pool anything tier-1-shaped uses", which is the drift the separate type
// was built to prevent.
//
// It also needs to read tier2.memos and tier2.transcripts in the same statement
// as tier1.triage_holds, which only the main role can do as one query. 0007's
// grant lets the tier-1 role SELECT both, so this would have *worked* there —
// and would have been the first tier-1-role statement whose purpose was a
// person's action rather than a derivation.
//
// NOTHING HERE IS CALLED `Held`, AND THAT IS ENFORCED BY THE COMPILER RATHER
// THAN BY THIS COMMENT. store.HeldMemo and store.HeldMemos already exist in
// memo.go and mean the OTHER hold — a memo stuck in tier-2 state `held`
// because transcription failed. The names collided on the first attempt at
// this file, which is the two-populations problem the ruling avoids in the
// schema showing up again in the vocabulary. So a deferral is `TriageHold` and
// a deferred memo is `DeferredMemo`, and no reader has to work out which inbox
// a `HeldMemo` belongs to.

// ErrNotHeld is returned when releasing a memo that is not on hold.
//
// DISTINCT FROM ErrNotFound, because a client can tell them apart and should:
// "there is no such memo" is a mistake to fix, and "that memo is not on hold"
// is very often a second tap on a button that already worked. The API answers
// the second one idempotently; it could not do that if this arrived as the same
// error as a bad id.
var ErrNotHeld = errors.New("store: memo is not on hold")

// ErrNotHoldable is returned when a memo cannot be held because it is not
// awaiting a routing decision.
//
// The check is on the memo's STATE and it is deliberately strict. Holding a
// `discarded` or `triaged` memo would record a deferral of a decision that has
// already been taken — a row that means nothing, in a listing whose whole
// purpose is that everything in it still needs a person.
var ErrNotHoldable = errors.New("store: memo is not awaiting a routing decision")

// TriageHold is one deferred routing decision.
type TriageHold struct {
	MemoID uuid.UUID
	HeldBy uuid.UUID
	Reason string
	HeldAt time.Time

	// Age is how long it has been deferred, and it comes from the DATABASE'S
	// clock like every other timestamp on this screen.
	//
	// It is here rather than left to the caller because the caller reaching for
	// time.Since(HeldAt) is the bug this field prevents: on the idempotent
	// re-hold of a memo parked three weeks ago, the app clock and the database
	// clock disagree by the skew between them, and the confirmation would
	// report an age the listing does not. One clock, one answer. (PR #53
	// review.)
	Age time.Duration
}

// DeferredMemo is one row of the deferred inbox: the hold, the memo it defers,
// and the same excerpt the triage screen labels a card with.
type DeferredMemo struct {
	Hold    TriageHold
	Memo    Memo
	Excerpt string

	// Age is how long it has been deferred, computed IN SQL from now() rather
	// than in Go from HeldAt.
	//
	// Not pedantry: the process clock and the database clock are different
	// clocks, and every other timestamp on this screen came from the database.
	// An age derived from a second source would disagree with the ordering it
	// is displayed next to, on exactly the deployments where it matters.
	Age time.Duration
}

// HoldForTriage defers a memo's routing decision. IDEMPOTENT, and the shape of that
// idempotence is the point rather than a convenience.
//
// Re-holding an already-held memo returns the EXISTING row and leaves `held_at`
// where it was. A screen that reset the clock would let a memo be deferred
// week after week while always looking fresh in a listing sorted by age — which
// is "a place memos go to be forgotten" implemented instead of prevented.
//
// The reason is not updated either, for the same reason: a second hold is not a
// correction. Releasing and holding again is how a deferral is restated, and it
// costs the honest thing — a new clock.
func (s *Store) HoldForTriage(ctx context.Context, memoID, heldBy uuid.UUID, reason string) (TriageHold, error) {
	reason = strings.TrimSpace(reason)

	// The state check and the insert are one statement, so a memo cannot be
	// accepted between them. `WHERE state = 'transcribed'` inside the SELECT is
	// what makes this safe without a transaction: if the memo moved, the INSERT
	// has no row to insert and the ON CONFLICT arm never runs either.
	var h TriageHold
	var reasonCol *string
	var ageSeconds float64
	err := s.pool.QueryRow(ctx,
		`WITH holdable AS (
		     SELECT id FROM tier2.memos WHERE id = $1 AND state = $4
		 ), ins AS (
		     INSERT INTO tier1.triage_holds (memo_id, held_by, reason)
		     SELECT id, $2, NULLIF($3, '') FROM holdable
		     ON CONFLICT (memo_id) DO NOTHING
		     RETURNING memo_id, held_by, reason, held_at
		 )
		 SELECT memo_id, held_by, reason, held_at,
		        EXTRACT(EPOCH FROM (now() - held_at)) FROM ins
		 UNION ALL
		 -- The already-held arm. Guarded by EXISTS(holdable) so that a memo
		 -- which has since been triaged reports ErrNotHoldable rather than
		 -- quietly returning a stale hold row.
		 SELECT memo_id, held_by, reason, held_at,
		        EXTRACT(EPOCH FROM (now() - held_at))
		   FROM tier1.triage_holds
		  WHERE memo_id = $1
		    AND EXISTS (SELECT 1 FROM holdable)
		    AND NOT EXISTS (SELECT 1 FROM ins)
		 LIMIT 1`,
		memoID, heldBy, reason, StateTranscribed).
		Scan(&h.MemoID, &h.HeldBy, &reasonCol, &h.HeldAt, &ageSeconds)

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// No row from either arm means `holdable` was empty. Distinguishing
		// "no such memo" from "wrong state" costs a second query and buys a
		// materially better message, and this is not a hot path.
		return TriageHold{}, s.holdRefusal(ctx, memoID)
	case err != nil:
		return TriageHold{}, fmt.Errorf("store: hold memo: %w", err)
	}
	if reasonCol != nil {
		h.Reason = *reasonCol
	}
	h.Age = time.Duration(ageSeconds * float64(time.Second))
	return h, nil
}

// holdRefusal says why a hold was refused, once it is known that it was.
func (s *Store) holdRefusal(ctx context.Context, memoID uuid.UUID) error {
	var state string
	err := s.pool.QueryRow(ctx, `SELECT state FROM tier2.memos WHERE id = $1`, memoID).Scan(&state)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return fmt.Errorf("store: hold memo: %w", err)
	}
	return fmt.Errorf("%w: it is %s", ErrNotHoldable, state)
}

// ReleaseTriageHold puts a memo back on the triage screen.
//
// This is the exit the `Done when` means by "neither state is a dead end", and
// under the ruling it costs nothing: the memo never left `transcribed`, so
// releasing is a DELETE and not a state transition. There is no tier-2 write
// here at all, which is the whole dividend of holding outside the state machine.
func (s *Store) ReleaseTriageHold(ctx context.Context, memoID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tier1.triage_holds WHERE memo_id = $1`, memoID)
	if err != nil {
		return fmt.Errorf("store: release hold: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotHeld
	}
	return nil
}

// DeferredMemos is the inbox: what has been deferred, OLDEST FIRST, with an age.
//
// Oldest first for the same reason UntriagedMemos is — the thing most likely to
// be forgotten is the oldest one, and a listing that buries it under this
// evening's deferrals is the failure the ticket names.
//
// An ORPHANED HOLD IS INVISIBLE, by the JOIN rather than by a sweep. There is no
// foreign key (doctrine: no tier-1 table references tier 2), so a hold can
// outlive its memo. Joining through tier2.memos means such a row simply does
// not appear, which is the correct behaviour and needs no cleanup job to be
// correct — a deleted memo has no decision left to defer.
func (s *Store) DeferredMemos(ctx context.Context, authorID uuid.UUID, limit int) ([]DeferredMemo, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	}
	var author *uuid.UUID
	if authorID != uuid.Nil {
		author = &authorID
	}

	rows, err := s.pool.Query(ctx,
		`SELECT `+prefixed(memoColumns, "m")+`,
		        h.held_by, h.reason, h.held_at,
		        EXTRACT(EPOCH FROM (now() - h.held_at)),
		        COALESCE(t.excerpt, '')
		   FROM tier1.triage_holds h
		   JOIN tier2.memos m ON m.id = h.memo_id
		   LEFT JOIN LATERAL (
		         SELECT left(text, $4) AS excerpt
		           FROM tier2.transcripts
		          WHERE memo_id = m.id AND `+DurableClause+`
		          ORDER BY transcribed_at DESC
		          LIMIT 1
		        ) t ON TRUE
		  WHERE ($1::uuid IS NULL OR m.author_id = $1)
		    -- THE ENTRY PREDICATE, CARRIED THROUGH TO THE READ. HoldForTriage
		    -- only ever creates a hold for a transcribed memo, and nothing
		    -- keeps the two in step afterwards: /triage/accept names a memo id
		    -- directly and never consults this table, so a deferred memo can be
		    -- decided and leave its hold behind.
		    --
		    -- Without this line that row lists forever with a growing age, is
		    -- counted in a number an operator is meant to drive to zero, and
		    -- cannot be cleared by re-holding — HoldForTriage refuses a decided
		    -- memo. Which is the ticket's own "place memos go to be forgotten",
		    -- built instead of prevented. (PR #53 review.)
		    --
		    -- Filtered here rather than deleted on the accept path, because a
		    -- DELETE there would be a tier-1 write on the tier-2 decision path
		    -- and memolink_test.go bans exactly that.
		    AND m.state = $5
		  ORDER BY h.held_at
		  LIMIT $6`,
		author, SufficientRunners, SufficientModels, excerptBudget, StateTranscribed, limit)
	if err != nil {
		return nil, fmt.Errorf("store: deferred memos: %w", err)
	}
	defer rows.Close()

	var out []DeferredMemo
	for rows.Next() {
		var it DeferredMemo
		var m Memo
		var reason *string
		var ageSeconds float64
		if err := rows.Scan(&m.ID, &m.AuthorID, &m.ContentHash, &m.ByteSize, &m.CapturedAt,
			&m.State, &m.StateReason, &m.Retention, &m.AudioPrunedAt,
			&m.DurationMS, &m.Codec, &m.SampleRateHz, &m.OriginalFilename,
			&m.CreatedAt, &m.UpdatedAt,
			&it.Hold.HeldBy, &reason, &it.Hold.HeldAt, &ageSeconds, &it.Excerpt); err != nil {
			return nil, fmt.Errorf("store: deferred memos: %w", err)
		}
		it.Hold.MemoID = m.ID
		if reason != nil {
			it.Hold.Reason = *reason
		}
		it.Memo = m
		it.Excerpt = firstLine(it.Excerpt)
		it.Age = time.Duration(ageSeconds * float64(time.Second))
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: deferred memos: %w", err)
	}
	return out, nil
}

// CountTriageHolds is the number in the admin report. Unscoped: the backlog view is
// the owner's.
func (s *Store) CountTriageHolds(ctx context.Context) (int, error) {
	var n int
	// Through the JOIN, so the count agrees with the listing rather than with
	// the table. An orphaned hold is not something an operator can act on and
	// must not appear in a number they are meant to drive to zero.
	// Same predicate as DeferredMemos, and it must stay the same one: a count
	// that disagreed with the listing is a backlog figure an operator cannot
	// reconcile with the screen it is meant to describe.
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tier1.triage_holds h
		   JOIN tier2.memos m ON m.id = h.memo_id
		  WHERE m.state = $1`, StateTranscribed).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count holds: %w", err)
	}
	return n, nil
}
