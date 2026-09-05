package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// holdHash builds a valid content hash — 64 lowercase hex — from a short seed,
// so each memo in this file is distinct without a wall of repeated characters.
func holdHash(seed string) string {
	sum := sha256.Sum256([]byte("chrn-34/" + seed))
	return hex.EncodeToString(sum[:])
}

// CHRN-34's store half, tested against the three clauses of its `Done when`:
// held memos are listed with an age, discarded memos leave the triage flow
// without being destroyed on the spot, and neither state is a dead end.

// Done when #1, and the whole of ruling B in one assertion: A DEFERRED MEMO
// LEAVES THE TRIAGE SCREEN WITHOUT LEAVING `transcribed`.
//
// Both halves matter. If it did not leave the screen the hold would do nothing;
// if it left `transcribed` this would be option A, with a new edge in
// tier2.memos_guard and a released memo re-entering E3's worker.
func TestADeferredMemoLeavesTheScreenAndNotItsState(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	memoID := seedTriageable(t, s, ctx, holdHash("d1"))

	before, err := s.UntriagedMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("UntriagedMemos: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("triage screen before hold = %d memos, want 1", len(before))
	}

	if _, err := s.HoldForTriage(ctx, memoID, owner.ID, "waiting to hear back"); err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}

	after, err := s.UntriagedMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("UntriagedMemos: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("triage screen after hold = %d memos, want 0 — the hold did not take", len(after))
	}

	// THE MEMO DID NOT MOVE. This is the assertion that distinguishes the
	// ruling from the alternative, and it is why this ticket ships no tier-2
	// migration at all.
	memo, err := s.GetMemo(ctx, memoID)
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}
	if memo.State != StateTranscribed {
		t.Errorf("memo state after hold = %q, want %q — a triage hold is not a memo state",
			memo.State, StateTranscribed)
	}
}

// Done when #1 continued: LISTED WITH AN AGE.
func TestTheDeferredInboxCarriesAnAge(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	memoID := seedTriageable(t, s, ctx, holdHash("d2"))
	if _, err := s.HoldForTriage(ctx, memoID, owner.ID, "needs the page tree"); err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}

	held, err := s.DeferredMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("DeferredMemos: %v", err)
	}
	if len(held) != 1 {
		t.Fatalf("deferred inbox = %d rows, want 1", len(held))
	}
	got := held[0]
	if got.Memo.ID != memoID {
		t.Errorf("deferred memo = %s, want %s", got.Memo.ID, memoID)
	}
	if got.Hold.Reason != "needs the page tree" {
		t.Errorf("reason = %q, want it preserved", got.Hold.Reason)
	}
	if got.Hold.HeldBy != owner.ID {
		t.Errorf("held_by = %s, want the owner", got.Hold.HeldBy)
	}
	// Age is non-negative and small. The value is the database's clock, which
	// is the point — asserting a range rather than a number is what keeps this
	// from being a test of how fast the machine is.
	if got.Age < 0 || got.Age > time.Minute {
		t.Errorf("age = %v, want a small non-negative duration", got.Age)
	}
	// The card has to be recognisable, or a deferred memo is an id nobody can
	// decide weeks later.
	if got.Excerpt == "" {
		t.Error("deferred card carries no excerpt; it would be unrecognisable in the inbox")
	}
}

// Done when #3 for HOLD: NOT A DEAD END. Releasing puts it back, and the round
// trip costs no tier-2 write at all.
func TestReleasingAHoldPutsTheMemoBackOnTheScreen(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	memoID := seedTriageable(t, s, ctx, holdHash("d3"))

	if _, err := s.HoldForTriage(ctx, memoID, owner.ID, ""); err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}
	if err := s.ReleaseTriageHold(ctx, memoID); err != nil {
		t.Fatalf("ReleaseTriageHold: %v", err)
	}

	back, err := s.UntriagedMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("UntriagedMemos: %v", err)
	}
	if len(back) != 1 || back[0].Memo.ID != memoID {
		t.Fatalf("after release the screen has %d memos, want the released one back", len(back))
	}
	held, err := s.DeferredMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("DeferredMemos: %v", err)
	}
	if len(held) != 0 {
		t.Errorf("deferred inbox after release = %d, want 0", len(held))
	}
}

// RE-HOLDING DOES NOT RESET THE CLOCK, which is the difference between an
// inbox and a place memos go to be forgotten. A screen that let a memo be
// deferred weekly while always looking fresh would satisfy "listed with an age"
// and defeat the reason the age is listed.
func TestReHoldingKeepsTheOriginalClock(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	memoID := seedTriageable(t, s, ctx, holdHash("d4"))

	first, err := s.HoldForTriage(ctx, memoID, owner.ID, "first reason")
	if err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}
	second, err := s.HoldForTriage(ctx, memoID, owner.ID, "second reason")
	if err != nil {
		t.Fatalf("HoldForTriage again: %v", err)
	}
	if !second.HeldAt.Equal(first.HeldAt) {
		t.Errorf("held_at moved on re-hold: %v -> %v; the age would restart every tap",
			first.HeldAt, second.HeldAt)
	}
	if second.Reason != "first reason" {
		t.Errorf("reason = %q after re-hold, want the original — a second hold is not a correction",
			second.Reason)
	}
}

// A memo past the point of deferral is refused, and the error says which state
// it is in. Deferring an already-decided memo would put a row in an inbox whose
// entire purpose is that everything in it still needs a person.
func TestHoldingRefusesAMemoThatIsNoLongerAwaitingADecision(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	memoID := seedTriageable(t, s, ctx, holdHash("d5"))
	if _, err := s.AdvanceMemoState(ctx, memoID, StateTranscribed, StateDiscarded, "discarded at triage"); err != nil {
		t.Fatalf("AdvanceMemoState: %v", err)
	}

	_, err = s.HoldForTriage(ctx, memoID, owner.ID, "")
	if !errors.Is(err, ErrNotHoldable) {
		t.Fatalf("HoldForTriage on a discarded memo = %v, want ErrNotHoldable", err)
	}
	if !strings.Contains(err.Error(), StateDiscarded) {
		t.Errorf("error %q does not name the state that refused it", err)
	}

	// And a memo that never existed is ErrNotFound, not ErrNotHoldable — the
	// two are different mistakes and the caller fixes them differently.
	if _, err := s.HoldForTriage(ctx, uuid.New(), owner.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("HoldForTriage on a missing memo = %v, want ErrNotFound", err)
	}
}

// Releasing something that is not held is ErrNotHeld and not ErrNotFound, so
// the API can answer the second tap idempotently instead of reporting a failure
// for the state the caller wanted.
func TestReleasingAMemoThatIsNotHeldIsItsOwnError(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, holdHash("d6"))

	if err := s.ReleaseTriageHold(ctx, memoID); !errors.Is(err, ErrNotHeld) {
		t.Fatalf("ReleaseTriageHold on an unheld memo = %v, want ErrNotHeld", err)
	}
}

// Done when #2: A DISCARDED MEMO LEAVES THE TRIAGE FLOW WITHOUT BEING
// DESTROYED ON THE SPOT.
//
// This is the weak reading of "reversible for a window", ruled 2026-09-04 and
// anticipated by CHRN-32 §4: the window belongs to the PRUNER and to the audio,
// not to the memo state. So the assertions are deliberately in two halves —
// the state is terminal, and nothing about the content went anywhere.
func TestADiscardedMemoLeavesTheFlowWithNothingDestroyed(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, holdHash("e1"))

	before, err := s.GetMemo(ctx, memoID)
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}

	if _, err := s.AdvanceMemoState(ctx, memoID, StateTranscribed, StateDiscarded, "discarded at triage"); err != nil {
		t.Fatalf("AdvanceMemoState: %v", err)
	}

	// 1 · It left the flow.
	screen, err := s.UntriagedMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("UntriagedMemos: %v", err)
	}
	if len(screen) != 0 {
		t.Errorf("a discarded memo is still on the triage screen (%d rows)", len(screen))
	}

	// 2 · The memo row survives, with its identity intact.
	after, err := s.GetMemo(ctx, memoID)
	if err != nil {
		t.Fatalf("GetMemo after discard: %v", err)
	}
	if after.ContentHash != before.ContentHash || after.ByteSize != before.ByteSize {
		t.Error("discarding changed the memo's identity")
	}

	// 3 · THE AUDIO IS STILL THERE. Discarding marks; CHRN-22's pruner deletes,
	// on its own schedule and behind a durable transcript. A discard that
	// pruned on the spot would be the single worst thing this system can do,
	// per CLAUDE.md, and it would do it at the moment an operator is moving
	// fastest.
	if after.AudioPrunedAt != nil {
		t.Error("discarding pruned the audio; the pruner owns that window, not the discard")
	}

	// 4 · THE TRANSCRIPT IS STILL THERE. It is tier 2 and permanent, and after
	// the audio prunes it is the only remaining account of what was said.
	tr, err := derived(s).TranscriptForScribe(ctx, memoID)
	if err != nil {
		t.Fatalf("transcript after discard: %v — discarding destroyed the durable record", err)
	}
	if tr.Text == "" {
		t.Error("transcript is empty after discard")
	}
}

// The scoping the listing relies on: a member sees their own deferrals, not
// everyone's. The unscoped call is the owner's view.
func TestTheDeferredInboxIsScopedByAuthor(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	memoID := seedTriageable(t, s, ctx, holdHash("d7"))
	if _, err := s.HoldForTriage(ctx, memoID, owner.ID, ""); err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}

	mine, err := s.DeferredMemos(ctx, owner.ID, 10)
	if err != nil {
		t.Fatalf("DeferredMemos(owner): %v", err)
	}
	if len(mine) != 1 {
		t.Errorf("owner's own deferrals = %d, want 1", len(mine))
	}

	someoneElse, err := s.DeferredMemos(ctx, uuid.New(), 10)
	if err != nil {
		t.Fatalf("DeferredMemos(other): %v", err)
	}
	if len(someoneElse) != 0 {
		t.Errorf("another author sees %d of the owner's deferrals, want 0", len(someoneElse))
	}
}

// The admin count agrees with the listing rather than with the table, so an
// orphaned hold — possible because doctrine forbids the foreign key — cannot
// show up as work in a number an operator is trying to drive to zero.
func TestTheHoldCountMatchesWhatIsListed(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	for _, h := range []string{holdHash("c1"), holdHash("c2")} {
		if _, err := s.HoldForTriage(ctx, seedTriageable(t, s, ctx, h), owner.ID, ""); err != nil {
			t.Fatalf("HoldForTriage: %v", err)
		}
	}

	// An orphan: a hold whose memo does not exist. Reachable in production
	// because there is no foreign key, by design.
	if _, err := s.Pool().Exec(ctx,
		`INSERT INTO tier1.triage_holds (memo_id, held_by) VALUES ($1, $2)`,
		uuid.New(), owner.ID); err != nil {
		t.Fatalf("seed orphan: %v", err)
	}

	n, err := s.CountTriageHolds(ctx)
	if err != nil {
		t.Fatalf("CountTriageHolds: %v", err)
	}
	if n != 2 {
		t.Errorf("hold count = %d, want 2 — the orphan is not work anybody can do", n)
	}
	held, err := s.DeferredMemos(ctx, uuid.Nil, 10)
	if err != nil {
		t.Fatalf("DeferredMemos: %v", err)
	}
	if len(held) != n {
		t.Errorf("count %d disagrees with the listing (%d rows)", n, len(held))
	}
}

// The backlog must not count deferred memos. An operator driving the backlog to
// zero cannot do it if half of it is work they deliberately set aside.
func TestTheBacklogExcludesDeferredMemos(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	keep := seedTriageable(t, s, ctx, holdHash("b1"))
	defer_ := seedTriageable(t, s, ctx, holdHash("b2"))
	_ = keep

	full, err := s.TriageBacklog(ctx)
	if err != nil {
		t.Fatalf("TriageBacklog: %v", err)
	}
	if full.Total != 2 {
		t.Fatalf("backlog before hold = %d, want 2", full.Total)
	}

	if _, err := s.HoldForTriage(ctx, defer_, owner.ID, ""); err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}
	after, err := s.TriageBacklog(ctx)
	if err != nil {
		t.Fatalf("TriageBacklog: %v", err)
	}
	if after.Total != 1 {
		t.Errorf("backlog after hold = %d, want 1 — a deferral is not backlog", after.Total)
	}
}
