package triage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-34's HOLD, service half.
//
// HOLD IS AN OPERATOR ACTION AND NOT A DESTINATION, which CHRN-32 settled as
// ruling R1 and this file is where that becomes structural. There is no
// `DestHold` in scribe, nothing to accept, and no path through Apply: a model
// that could emit HOLD would be a router that abstains, and an abstention
// cannot be scored — which is the measurement E4 exits on.
//
// So holding does not go through the batch at all. It is its own verb, and the
// asymmetry with accept is deliberate: an accept is a decision that reaches
// Switchyard and advances tier-2 state, while a hold reaches one tier-1 row and
// nothing else. Routing them through one endpoint would put a destructive path
// and a reversible one behind the same handler.

// DeferredItem is one card of the deferred inbox.
type DeferredItem struct {
	MemoID     uuid.UUID `json:"memo_id"`
	CapturedAt time.Time `json:"captured_at"`
	DurationMS *int32    `json:"duration_ms,omitempty"`

	// Excerpt labels the card, exactly as it does on the triage screen. A
	// deferred memo the operator cannot recognise is one they cannot decide,
	// and "which memo was that" is the whole reason a bare id is not enough.
	Excerpt string `json:"excerpt"`

	Reason string    `json:"reason,omitempty"`
	HeldBy uuid.UUID `json:"held_by"`
	HeldAt time.Time `json:"held_at"`

	// AgeSeconds is the `Done when`'s "listed with an age", and it is a NUMBER
	// rather than a rendered string because the client decides how to say
	// "three weeks". Computed by the database — see store.DeferredMemo.Age.
	AgeSeconds int64 `json:"age_seconds"`
}

// Hold defers a memo's routing decision.
//
// Idempotent, and it keeps the original clock: see store.HoldForTriage. The
// second tap answers exactly like the first, which is what lets a client retry
// a hold it is not sure landed.
func (s *Service) Hold(ctx context.Context, actor store.User, memoID uuid.UUID, reason string) (DeferredItem, error) {
	// SCOPED LIKE THE POST, not like the list. A member reaching for another
	// member's memo gets the same answer as one reaching for an id that does
	// not exist — the pattern applyOne sets, and for the same reason: a probe
	// must not be able to tell a memo it may not see from one that never was.
	memo, err := s.store.GetMemo(ctx, memoID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return DeferredItem{}, ErrNoSuchMemo
	case err != nil:
		return DeferredItem{}, err
	case !canDecide(actor, memo):
		return DeferredItem{}, ErrNoSuchMemo
	}

	h, err := s.store.HoldForTriage(ctx, memoID, actor.ID, reason)
	if err != nil {
		return DeferredItem{}, err
	}
	return DeferredItem{
		MemoID:     h.MemoID,
		CapturedAt: memo.CapturedAt,
		DurationMS: memo.DurationMS,
		Reason:     h.Reason,
		HeldBy:     h.HeldBy,
		HeldAt:     h.HeldAt,
		AgeSeconds: int64(time.Since(h.HeldAt).Seconds()),
	}, nil
}

// Release puts a memo back on the triage screen.
//
// THE EXIT THAT MAKES HOLD NOT A DEAD END, which the `Done when` asks for by
// name. Under the ruling it is a DELETE of one tier-1 row: the memo never left
// `transcribed`, so there is no state transition to make and no guard to
// satisfy. Compare what option A would have cost — a new edge in
// tier2.memos_guard, and a released memo re-entering E3's worker.
func (s *Service) Release(ctx context.Context, actor store.User, memoID uuid.UUID) error {
	memo, err := s.store.GetMemo(ctx, memoID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return ErrNoSuchMemo
	case err != nil:
		return err
	case !canDecide(actor, memo):
		return ErrNoSuchMemo
	}
	return s.store.ReleaseTriageHold(ctx, memoID)
}

// Deferred is the inbox: what has been parked, oldest first, with an age.
//
// VISIBLE AND COUNTABLE is the ticket's requirement and the reason this exists
// as its own listing rather than as a flag on the triage screen. A deferral
// that only showed up as a greyed-out card among the live ones would be
// invisible on the evening it mattered — the point of parking a memo is that it
// leaves the screen you are working through.
func (s *Service) Deferred(ctx context.Context, actor store.User, limit int) ([]DeferredItem, error) {
	switch {
	case limit <= 0:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}

	scope := actor.ID
	if actor.IsAdmin() {
		scope = uuid.Nil
	}

	held, err := s.store.DeferredMemos(ctx, scope, limit)
	if err != nil {
		return nil, err
	}

	out := make([]DeferredItem, 0, len(held))
	for _, h := range held {
		out = append(out, DeferredItem{
			MemoID:     h.Memo.ID,
			CapturedAt: h.Memo.CapturedAt,
			DurationMS: h.Memo.DurationMS,
			Excerpt:    h.Excerpt,
			Reason:     h.Hold.Reason,
			HeldBy:     h.Hold.HeldBy,
			HeldAt:     h.Hold.HeldAt,
			AgeSeconds: int64(h.Age.Seconds()),
		})
	}
	return out, nil
}

// ErrNoSuchMemo is the single answer to "no such memo" and "not yours".
//
// One error for two situations ON PURPOSE. Telling them apart would make this
// endpoint an existence oracle for memos the caller may not read, which is the
// thing applyOne's own scoping comment refuses to do.
var ErrNoSuchMemo = errors.New("triage: no such memo, or it belongs to another author")
