package discuss

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-47's `Done when`, end to end against a real database — because the two
// clauses that matter are properties of the store and the fakes cannot show
// them: `author_kind` is set by CH092, the unread count is CHRN-45's
// arithmetic, and CH091 is the guard this package deliberately does not
// duplicate.

// realStore brings up a migrated database and the Scribe account, or skips.
func realStore(t *testing.T) (*store.Store, context.Context, store.User, store.User) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := store.New(pool)
	owner, err := st.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	// The account CHRN-44 creates at boot. Created here because this package's
	// tests do not run cmd/chronicle's bootstrap.
	scribe, err := st.EnsureAgent(ctx, store.ScribeEmail, store.ScribeDisplayName)
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	return st, ctx, owner, scribe
}

// ALL THREE `Done when` CLAUSES IN ONE THREAD, because they are one claim:
// a person asks, the Scribe answers, the answer is attributed to the Scribe,
// the person has exactly one unread, and the Scribe cannot go again.
func TestAnAgentReplyIsAttributedUnreadAndCannotRepeat(t *testing.T) {
	st, ctx, owner, scribe := realStore(t)
	r := New(st, quiet())

	d, opening, err := st.OpenDiscussion(ctx, store.NewDiscussion{
		Title:    "How long do we keep audio",
		AuthorID: owner.ID,
		Body:     "@scribe how long do we keep audio, and what gates the deletion?",
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	if opening.ByAgent() {
		t.Fatal("the opening turn is an agent's; CH091 should have refused that")
	}

	// The opener has read their own turn — CHRN-45's ruling 3 — so there is
	// nothing unread before the reply. Asserted, because "exactly one" below
	// is only meaningful against a known starting point.
	if n, err := st.UnreadCount(ctx, d.ID, owner.ID); err != nil || n != 0 {
		t.Fatalf("unread before the reply = %d (err %v), want 0", n, err)
	}

	turn, err := r.Reply(ctx, d.ID, "Thirty days, and deletion is gated on a durable transcript rather than on the calendar.")
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}

	// CLAUSE 1 — ATTRIBUTED CORRECTLY. author_kind comes from CH092, which
	// derives it from the account and refuses a caller-supplied value, so this
	// is the database's answer rather than this package's claim.
	if turn.AuthorID != scribe.ID {
		t.Errorf("author = %s, want the scribe %s", turn.AuthorID, scribe.ID)
	}
	if !turn.ByAgent() {
		t.Errorf("author_kind = %q, want agent", turn.AuthorKind)
	}
	if turn.Seq != 2 {
		t.Errorf("seq = %d, want 2", turn.Seq)
	}

	// CLAUSE 2 — EXACTLY ONE UNREAD FOR THE HUMAN. Not "more than zero": the
	// `Done when` says "rather than none or two", and both are reachable.
	n, err := st.UnreadCount(ctx, d.ID, owner.ID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if n != 1 {
		t.Errorf("unread after the reply = %d, want EXACTLY 1", n)
	}

	// CLAUSE 3 — IT CANNOT TRIGGER ANOTHER AGENT REPLY. Refused here as
	// "already answered" before the store is asked; the store's CH091 is the
	// enforcement and is asserted in internal/store against raw SQL.
	if _, err := r.Reply(ctx, d.ID, "and another thing"); !errors.Is(err, ErrAlreadyAnswered) {
		t.Errorf("a second reply err = %v, want ErrAlreadyAnswered", err)
	}

	// AND THE STORE REFUSES IT TOO, with this package stepping aside. Bypassing
	// the handler's own check proves the guarantee does not depend on it.
	if _, err := st.AppendTurn(ctx, store.NewTurn{
		DiscussionID: d.ID, AuthorID: scribe.ID, Body: "going again",
	}); !errors.Is(err, store.ErrAgentMayNotFollowAgent) {
		t.Errorf("the store's refusal err = %v, want ErrAgentMayNotFollowAgent", err)
	}

	// The thread is two turns and reads in order.
	turns, err := st.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(turns) != 2 || turns[0].ByAgent() || !turns[1].ByAgent() {
		t.Errorf("thread = %+v, want person then agent", turns)
	}
}

// THE CONVERSATION CONTINUES. A person answers the Scribe, mentions it again,
// and it may reply — the rule refuses the loop, not the exchange.
func TestTheExchangeContinuesWhenThePersonSpeaksAgain(t *testing.T) {
	st, ctx, owner, _ := realStore(t)
	r := New(st, quiet())

	d, _, err := st.OpenDiscussion(ctx, store.NewDiscussion{
		Title: "Retention", AuthorID: owner.ID, Body: "@scribe thoughts on retention?",
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	if _, err := r.Reply(ctx, d.ID, "Thirty days."); err != nil {
		t.Fatalf("first reply: %v", err)
	}

	if _, err := st.AppendTurn(ctx, store.NewTurn{
		DiscussionID: d.ID, AuthorID: owner.ID, Body: "and what about pinned memos, @scribe?",
	}); err != nil {
		t.Fatalf("the person's follow-up: %v", err)
	}

	second, err := r.Reply(ctx, d.ID, "A pinned memo is never pruned.")
	if err != nil {
		t.Fatalf("second reply: %v", err)
	}
	if second.Seq != 4 {
		t.Errorf("seq = %d, want 4", second.Seq)
	}
	if n, err := st.UnreadCount(ctx, d.ID, owner.ID); err != nil || n != 1 {
		t.Errorf("unread = %d (err %v), want 1 — the person read up to their own turn 3", n, err)
	}
}

// A RESOLVED THREAD, which CHRN-43's plan specifically warns about: an agent
// that began composing before the thread resolved arrives after it did. CH093
// refuses the turn and this package reports it rather than pretending it wrote.
func TestAReplyIntoAThreadThatResolvedWhileComposing(t *testing.T) {
	st, ctx, owner, _ := realStore(t)
	r := New(st, quiet())

	d, _, err := st.OpenDiscussion(ctx, store.NewDiscussion{
		Title: "Concluded", AuthorID: owner.ID, Body: "@scribe last thoughts?",
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	if err := st.ResolveDiscussion(ctx, d.ID, owner.ID, nil); err != nil {
		t.Fatalf("ResolveDiscussion: %v", err)
	}

	if _, err := r.Reply(ctx, d.ID, "too late"); !errors.Is(err, store.ErrDiscussionResolved) {
		t.Errorf("err = %v, want ErrDiscussionResolved", err)
	}
	turns, err := st.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(turns) != 1 {
		t.Errorf("%d turns on the resolved thread, want 1 — the refused reply landed", len(turns))
	}
}

// THE SCRIBE DOES NOT JOIN THE THREAD BY SPEAKING, which is CHRN-45's ruling 4
// arriving from the other side: an agent carries no read marker, so
// AppendTurn skips the participant upsert for it. Worth asserting from here
// because CHRN-47 is the caller that makes it happen in production.
func TestReplyingDoesNotMakeTheScribeAParticipant(t *testing.T) {
	st, ctx, owner, scribe := realStore(t)
	r := New(st, quiet())

	d, _, err := st.OpenDiscussion(ctx, store.NewDiscussion{
		Title: "Membership", AuthorID: owner.ID, Body: "@scribe are you here?",
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	if _, err := r.Reply(ctx, d.ID, "I am."); err != nil {
		t.Fatalf("Reply: %v", err)
	}

	ps, err := st.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	for _, p := range ps {
		if p.UserID == scribe.ID {
			t.Errorf("the scribe joined the thread by replying: %+v", p)
		}
	}
	// And it replied anyway, which is the half that would break if the upsert
	// were unconditional — CH100 would refuse an agent as its own added_by and
	// take the turn down with it.
	turns, err := st.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("%d turns, want 2", len(turns))
	}
}
