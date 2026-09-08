package discuss

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-47's `Done when`: an agent reply appears in the thread attributed
// correctly, produces an unread for the human, and cannot trigger another agent
// reply.
//
// THE THIRD CLAUSE IS NOT TESTED IN THIS PACKAGE, and its absence is the
// design. CHRN-43's ruling 3 put "an agent turn may not be caused by an agent
// turn" in the store as CH091, precisely so it is not a property of this
// handler; internal/store's TestAgentAfterAgentIsRefusedBySQL is where it is
// asserted, against raw SQL that bypasses every Go path. What is tested here is
// that this package HANDLES the refusal rather than duplicating it — and the
// integration test at the bottom of internal/store covers the first two clauses
// against a real database.

// fakeStore is enough of the store to drive the reply path without Postgres.
// The clauses that need a real database are asserted in internal/store; these
// are about the decisions this package makes before it gets there.
type fakeStore struct {
	scribe    store.User
	scribeErr error
	turns     []store.DiscussionTurn
	turnsErr  error
	appended  []store.NewTurn
	appendErr error
}

func (f *fakeStore) Scribe(context.Context) (store.User, error) {
	return f.scribe, f.scribeErr
}

func (f *fakeStore) Turns(context.Context, uuid.UUID) ([]store.DiscussionTurn, error) {
	return f.turns, f.turnsErr
}

func (f *fakeStore) AppendTurn(_ context.Context, in store.NewTurn) (store.DiscussionTurn, error) {
	f.appended = append(f.appended, in)
	if f.appendErr != nil {
		return store.DiscussionTurn{}, f.appendErr
	}
	return store.DiscussionTurn{
		ID: uuid.New(), DiscussionID: in.DiscussionID, Seq: len(f.turns) + 1,
		AuthorID: in.AuthorID, AuthorKind: store.KindAgent, Body: in.Body,
	}, nil
}

func newFake(turns ...store.DiscussionTurn) *fakeStore {
	return &fakeStore{
		scribe: store.User{ID: uuid.New(), Email: store.ScribeEmail, Kind: store.KindAgent},
		turns:  turns,
	}
}

func personTurn(seq int, body string) store.DiscussionTurn {
	return store.DiscussionTurn{Seq: seq, AuthorID: uuid.New(), AuthorKind: store.KindPerson, Body: body}
}

func agentTurn(seq int, body string) store.DiscussionTurn {
	return store.DiscussionTurn{Seq: seq, AuthorID: uuid.New(), AuthorKind: store.KindAgent, Body: body}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newReplier(t *testing.T, f *fakeStore) *Replier {
	t.Helper()
	return New(f, quiet())
}

// ============================================================================
// The trigger is explicit — the ticket's first requirement.
// ============================================================================

// "Keep the trigger explicit rather than ambient. An agent that replies to
// everything turns discussions into noise." Nothing here decides for itself
// that a thread deserves an answer: without a mention there is no reply, and
// the refusal is the default rather than the exception.
func TestNoMentionMeansNoReply(t *testing.T) {
	f := newFake(personTurn(1, "how long do we keep audio"))
	r := newReplier(t, f)

	_, err := r.Reply(context.Background(), uuid.New(), "thirty days")
	if !errors.Is(err, ErrNotMentioned) {
		t.Fatalf("err = %v, want ErrNotMentioned", err)
	}
	if len(f.appended) != 0 {
		t.Errorf("it wrote a turn anyway: %+v", f.appended)
	}
}

func TestAMentionTriggersTheReply(t *testing.T) {
	f := newFake(personTurn(1, "@scribe how long do we keep audio?"))
	r := newReplier(t, f)

	turn, err := r.Reply(context.Background(), uuid.New(), "thirty days, gated on a durable transcript")
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if len(f.appended) != 1 {
		t.Fatalf("appended %d turns, want 1", len(f.appended))
	}
	// ATTRIBUTED CORRECTLY — the first `Done when` clause. The author is the
	// Scribe's account, and author_kind comes back from the store (CH092 sets
	// it) rather than being asserted by this package.
	if f.appended[0].AuthorID != f.scribe.ID {
		t.Errorf("author = %s, want the scribe %s", f.appended[0].AuthorID, f.scribe.ID)
	}
	if turn.AuthorKind != store.KindAgent {
		t.Errorf("author_kind = %q, want agent", turn.AuthorKind)
	}
}

// ONLY THE LAST TURN IS READ. A mention four turns ago was answered then, or
// deliberately was not; re-reading the whole thread would make every later turn
// re-trigger a reply to an old question.
func TestOnlyTheLastTurnTriggers(t *testing.T) {
	f := newFake(
		personTurn(1, "@scribe what about retention?"),
		agentTurn(2, "thirty days"),
		personTurn(3, "thanks, that settles it"),
	)
	r := newReplier(t, f)

	if _, err := r.Reply(context.Background(), uuid.New(), "you are welcome"); !errors.Is(err, ErrNotMentioned) {
		t.Errorf("err = %v, want ErrNotMentioned — an old mention re-triggered", err)
	}
}

// A THREAD THE SCRIBE ALREADY HAS THE LAST WORD IN. CH091 would refuse this in
// the store; catching it here means the ordinary "already answered" case does
// not reach the database as a guard violation.
func TestItWillNotAnswerTwice(t *testing.T) {
	f := newFake(
		personTurn(1, "@scribe thoughts?"),
		agentTurn(2, "thirty days"),
	)
	r := newReplier(t, f)

	_, err := r.Reply(context.Background(), uuid.New(), "and another thing")
	if !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("err = %v, want ErrAlreadyAnswered", err)
	}
	if len(f.appended) != 0 {
		t.Errorf("it tried to write anyway: %+v", f.appended)
	}
}

// ============================================================================
// Rate limiting — the ticket's "rate-limit and log it".
// ============================================================================

func TestRepliesAreRateLimitedPerThread(t *testing.T) {
	f := newFake(personTurn(1, "@scribe hello"))
	r := newReplier(t, f)
	// The fake never appends to its own turn list, so every call sees the same
	// mention as the last turn — which is what makes this a loop to bound.
	d := uuid.New()

	for i := 0; i < replyBurst; i++ {
		if _, err := r.Reply(context.Background(), d, "answer"); err != nil {
			t.Fatalf("reply %d: %v", i+1, err)
		}
	}
	if _, err := r.Reply(context.Background(), d, "answer"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("reply %d err = %v, want ErrRateLimited", replyBurst+1, err)
	}

	// PER THREAD. A different discussion has its own allowance — a global
	// limiter would let one runaway thread silence every other.
	if _, err := r.Reply(context.Background(), uuid.New(), "answer"); err != nil {
		t.Errorf("a different thread was rate-limited by the first: %v", err)
	}

	// And the window reopens.
	r.limiter.now = func() time.Time { return time.Now().Add(2 * replyWindow) }
	if _, err := r.Reply(context.Background(), d, "answer"); err != nil {
		t.Errorf("after the window: %v", err)
	}
}

// AN UNTRIGGERED THREAD CONSUMES NO ALLOWANCE. Otherwise a caller looping on a
// thread nobody mentioned the Scribe in would exhaust the budget of a thread
// that had legitimately asked.
func TestARefusedTriggerDoesNotSpendTheAllowance(t *testing.T) {
	f := newFake(personTurn(1, "no mention here"))
	r := newReplier(t, f)
	d := uuid.New()

	for i := 0; i < replyBurst*3; i++ {
		if _, err := r.Reply(context.Background(), d, "answer"); !errors.Is(err, ErrNotMentioned) {
			t.Fatalf("attempt %d err = %v, want ErrNotMentioned", i+1, err)
		}
	}

	f.turns = []store.DiscussionTurn{personTurn(1, "@scribe now I am asking")}
	if _, err := r.Reply(context.Background(), d, "answer"); err != nil {
		t.Errorf("the real request was rate-limited by refused ones: %v", err)
	}
}

// ============================================================================
// Store refusals are reported, not prevented.
// ============================================================================

// CH091 and CH093 belong to the store — CHRN-43's ruling 3, and the condition
// on which this ticket stayed at Mode A. This asserts the handler passes them
// through as themselves rather than swallowing or re-deciding them.
func TestStoreRefusalsSurface(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"agent after agent (CH091)", store.ErrAgentMayNotFollowAgent},
		{"resolved thread (CH093)", store.ErrDiscussionResolved},
		{"thread busy", store.ErrLinkLocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(personTurn(1, "@scribe hello"))
			f.appendErr = tc.err
			r := newReplier(t, f)

			_, err := r.Reply(context.Background(), uuid.New(), "answer")
			if !errors.Is(err, tc.err) {
				t.Errorf("err = %v, want %v to survive", err, tc.err)
			}
			// It did attempt the write — the store is the enforcement, so this
			// package must not have decided the answer for itself.
			if len(f.appended) != 1 {
				t.Errorf("appended %d, want 1 attempt", len(f.appended))
			}
		})
	}
}

// THE CHRN-44 GUARD REACHES HERE. If scribe@localhost belongs to a person,
// Scribe() refuses, and this must refuse with it rather than authoring a turn
// as that person — which CH090 would then make permanent.
func TestAPersonHoldingTheScribeAddressStopsTheReply(t *testing.T) {
	f := newFake(personTurn(1, "@scribe hello"))
	f.scribeErr = store.ErrNotAnAgent
	r := newReplier(t, f)

	if _, err := r.Reply(context.Background(), uuid.New(), "answer"); !errors.Is(err, store.ErrNotAnAgent) {
		t.Errorf("err = %v, want ErrNotAnAgent", err)
	}
	if len(f.appended) != 0 {
		t.Errorf("it authored a turn as a person: %+v", f.appended)
	}
}

func TestABlankReplyIsRefusedBeforeTheStore(t *testing.T) {
	f := newFake(personTurn(1, "@scribe hello"))
	r := newReplier(t, f)

	if _, err := r.Reply(context.Background(), uuid.New(), "   \n "); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("err = %v, want ErrInvalidInput", err)
	}
	if len(f.appended) != 0 {
		t.Errorf("a blank body reached the store: %+v", f.appended)
	}
}

func TestAnEmptyThreadIsNotFound(t *testing.T) {
	r := newReplier(t, newFake())
	if _, err := r.Reply(context.Background(), uuid.New(), "answer"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// ============================================================================
// The handle, and what it is tied to.
// ============================================================================

// MentionHandle is derived from the account's ADDRESS, not its display name,
// because a display name is something PATCH /auth/me can change and a trigger
// that breaks on a rename is a trigger nobody trusts. This asserts the two
// have not drifted.
func TestTheHandleMatchesTheScribeAddress(t *testing.T) {
	local, _, ok := strings.Cut(store.ScribeEmail, "@")
	if !ok {
		t.Fatalf("store.ScribeEmail = %q, which has no local part", store.ScribeEmail)
	}
	if local != MentionHandle {
		t.Errorf("MentionHandle = %q but store.ScribeEmail's local part is %q — the trigger and the account have drifted",
			MentionHandle, local)
	}
}

func TestMentionsScribe(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{"@scribe what do you think", true},
		{"what do you think @scribe", true},
		{"@Scribe capitalised still means the scribe", true},
		{"hey @SCRIBE", true},
		{"ask @scribe, then @someone-else", true},
		{"no mention at all", false},
		{"scribe without the at sign", false},
		{"@scribbler is somebody else", false},
		{"@scribes is a longer handle", false},
		{"@scribe-2 is somebody else", false},
		// SKIPPING CODE is the point of routing through internal/markdown: a
		// person documenting the feature must not summon it.
		{"it replies when you write `@scribe`", false},
		{"```\n@scribe\n```", false},
		{"    @scribe\n", false}, // indented code block
		{"`@scribe` in code, but @scribe in prose", true},
	} {
		if got := mentionsScribe(tc.body); got != tc.want {
			t.Errorf("mentionsScribe(%q) = %v, want %v", tc.body, got, tc.want)
		}
	}
}
