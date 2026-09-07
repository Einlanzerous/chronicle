package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// CHRN-43's twenty-two acceptance criteria, from the Switchyard plan revision 2
// approved 2026-09-07 with all six rulings picked. Criteria 0, 1, 9, 20 and 21
// are migration-CI and review checks with no Go assertion to make; everything
// else is here, numbered as it is there.

// person and agent, because half of this file is about the difference.
func discPerson(t *testing.T, s *Store, ctx context.Context, email string) uuid.UUID {
	t.Helper()
	u, err := s.CreateUser(ctx, email, "Person", KindPerson)
	if err != nil {
		t.Fatalf("CreateUser(%s, person): %v", email, err)
	}
	return u.ID
}

func discAgent(t *testing.T, s *Store, ctx context.Context, email string) uuid.UUID {
	t.Helper()
	u, err := s.CreateUser(ctx, email, "Scribe", KindAgent)
	if err != nil {
		t.Fatalf("CreateUser(%s, agent): %v", email, err)
	}
	return u.ID
}

// openThread opens a discussion whose first turn is a person's, which is the
// only way a thread can start (CH091).
func openThread(t *testing.T, s *Store, ctx context.Context, author uuid.UUID, title string) Discussion {
	t.Helper()
	d, _, err := s.OpenDiscussion(ctx, NewDiscussion{Title: title, AuthorID: author, Body: "opening"})
	if err != nil {
		t.Fatalf("OpenDiscussion(%q): %v", title, err)
	}
	return d
}

func appendTurn(t *testing.T, s *Store, ctx context.Context, d uuid.UUID, author uuid.UUID, body string) DiscussionTurn {
	t.Helper()
	turn, err := s.AppendTurn(ctx, NewTurn{DiscussionID: d, AuthorID: author, Body: body})
	if err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	return turn
}

// sqlstate pulls the SQLSTATE off an error, or "" if it carries none. The
// guards' codes are the contract these tests assert against — a message can be
// reworded, a code cannot without a migration.
func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// ============================================================================
// Criterion 2 — the tier boundary, asserted positively.
// ============================================================================

// Not "no GRANT was issued" but "the privilege is not held". 0007 granted
// SELECT on two tier-2 tables BY NAME and added no ALTER DEFAULT PRIVILEGES, so
// these three are unreachable by construction — which is exactly the kind of
// claim that stops being true quietly.
func TestTier1CannotReachTheDiscussionTables(t *testing.T) {
	_, ctx := newTestStore(t)
	pool := tier1Pool(t, ctx)
	defer pool.Close()

	for _, table := range []string{
		"tier2.discussions", "tier2.discussion_turns", "tier2.discussion_participants",
	} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
			var held bool
			if err := pool.QueryRow(ctx,
				`SELECT has_table_privilege(current_user, $1, $2)`, table, priv).Scan(&held); err != nil {
				t.Fatalf("privilege check %s on %s: %v", priv, table, err)
			}
			if held {
				t.Errorf("chronicle_tier1 holds %s on %s — a tier-1 path can reach authored conversation", priv, table)
			}
		}
	}
}

// ============================================================================
// Criterion 3 — ordering, under the thread's row lock.
// ============================================================================

// THE RACE. Concurrent appends all succeed with consecutive seq values and the
// thread reads back in that order. Without the row lock in AppendTurn they read
// the same MAX(seq), all but one fail on UNIQUE (discussion_id, seq), and the
// tail of the thread depends on commit order.
//
// EIGHT WORKERS RELEASED FROM ONE BARRIER, on TestConcurrentAppendsBothLand's
// finding: the two-goroutine version of that test passed with the lock removed,
// because the second append reliably began after the first had committed and so
// never contended at all.
func TestConcurrentTurnsAreOrderedWithNoGaps(t *testing.T) {
	s, ctx := newTestStore(t)
	author := discPerson(t, s, ctx, "race@example.com")
	d := openThread(t, s, ctx, author, "Contended")

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, workers)
	turns := make([]DiscussionTurn, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			turns[i], errs[i] = s.AppendTurn(ctx, NewTurn{
				DiscussionID: d.ID, AuthorID: author, Body: "concurrent",
			})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	// Consecutive and gapless: 2..workers+1, each exactly once. Turn 1 is the
	// opening post.
	seen := map[int]int{}
	for _, turn := range turns {
		seen[turn.Seq]++
	}
	for want := 2; want <= workers+1; want++ {
		if seen[want] != 1 {
			t.Errorf("seq %d appeared %d times, want exactly once (all: %v)", want, seen[want], seen)
		}
	}

	// And READABLE in that order, which is the half the ticket actually asks
	// for: "a thread reads in the same order everywhere".
	read, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(read) != workers+1 {
		t.Fatalf("read %d turns, want %d", len(read), workers+1)
	}
	for i, turn := range read {
		if turn.Seq != i+1 {
			t.Errorf("Turns()[%d].Seq = %d, want %d — the read order is not the seq order", i, turn.Seq, i+1)
		}
	}
}

// A HELD LOCK FAILS LEGIBLY RATHER THAN HANGING. Without SET LOCAL lock_timeout
// the waiter inherits none from the pool, so one stuck holder queues every
// other append behind it, each pinning a connection — memolink.go:49's argument,
// which applies here for the same reason.
func TestAppendTurnGivesUpOnAHeldLock(t *testing.T) {
	s, ctx := newTestStore(t)
	author := discPerson(t, s, ctx, "lock@example.com")
	d := openThread(t, s, ctx, author, "Held")

	defer SetLinkLockTimeoutForTest("200ms")()

	// Hold the thread's row from another transaction and never release it
	// until this test is done.
	holder, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx,
		`SELECT id FROM tier2.discussions WHERE id = $1 FOR UPDATE`, d.ID); err != nil {
		t.Fatalf("holder lock: %v", err)
	}

	started := time.Now()
	_, err = s.AppendTurn(ctx, NewTurn{DiscussionID: d.ID, AuthorID: author, Body: "blocked"})
	if !errors.Is(err, ErrLinkLocked) {
		t.Fatalf("AppendTurn against a held lock err = %v, want ErrLinkLocked", err)
	}
	// It gave up rather than waited out the context. The bound is loose on
	// purpose — the assertion is "bounded", not "exactly 200ms".
	if waited := time.Since(started); waited > 10*time.Second {
		t.Errorf("waited %s before giving up; the lock_timeout is not being set", waited)
	}
}

// ============================================================================
// Criterion 4 — an offline reply lands where it arrived.
// ============================================================================

// The ticket asks that "an offline reply lands where it belongs", and the plan's
// answer is that where it belongs is where it ARRIVED. Inserting by composed_at
// would mean renumbering, and renumbering is an UPDATE to the ordering CHRN-45's
// read markers are defined against.
func TestAnOldComposedAtStillLandsAtTheEnd(t *testing.T) {
	s, ctx := newTestStore(t)
	author := discPerson(t, s, ctx, "offline@example.com")
	d := openThread(t, s, ctx, author, "Retention")

	appendTurn(t, s, ctx, d.ID, author, "second")

	sixHoursAgo := time.Now().Add(-6 * time.Hour).UTC().Truncate(time.Millisecond)
	late, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: author, Body: "composed on the train",
		ComposedAt: &sixHoursAgo,
	})
	if err != nil {
		t.Fatalf("AppendTurn with a stale composed_at: %v", err)
	}

	if late.Seq != 3 {
		t.Errorf("seq = %d, want 3 — a six-hour-old composed_at reordered the thread", late.Seq)
	}
	if late.ComposedAt == nil {
		t.Fatal("composed_at came back nil; the client's claim was dropped")
	}
	if !late.ComposedAt.Equal(sixHoursAgo) {
		t.Errorf("composed_at = %s, want %s unchanged", late.ComposedAt, sixHoursAgo)
	}
	// It is advisory, and nothing sorts on it: the record still reads 1,2,3.
	read, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(read) != 3 || read[2].ID != late.ID {
		t.Errorf("the late turn is not last: %d turns, last id %v", len(read), read[len(read)-1].ID)
	}
}

// ============================================================================
// Criterion 5 — a turn is insert-only (CH090).
// ============================================================================

// BOTH OPERATIONS, ASSERTED SEPARATELY. An UPDATE-only guard would leave DELETE
// as the way to make a conversation say something it did not — and CHRN-44's
// worry is precisely that "six months later nobody can tell which conclusions
// in a thread were reasoned by a person".
func TestADiscussionTurnIsInsertOnly(t *testing.T) {
	s, ctx := newTestStore(t)
	author := discPerson(t, s, ctx, "immutable@example.com")
	d := openThread(t, s, ctx, author, "Permanent")
	turn := appendTurn(t, s, ctx, d.ID, author, "said once")

	for _, tc := range []struct {
		name  string
		query string
	}{
		{"update", `UPDATE tier2.discussion_turns SET body = 'rewritten' WHERE id = $1`},
		{"delete", `DELETE FROM tier2.discussion_turns WHERE id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.pool.Exec(ctx, tc.query, turn.ID)
			if got := sqlstate(err); got != pgTurnInsertOnly {
				t.Errorf("%s SQLSTATE = %q (err %v), want %s", tc.name, got, err, pgTurnInsertOnly)
			}
		})
	}

	// And the text is still there afterwards, which is the point of the rule
	// rather than a restatement of it.
	read, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(read) != 2 || read[1].Body != "said once" {
		t.Errorf("turns after the refused writes = %+v, want the original two", read)
	}
}

// ============================================================================
// Criteria 6, 7, 8 — ruling 3, an agent may not speak twice in a row.
// ============================================================================

// Criterion 6 — IN THE STORE AND NOT IN GO. Inserted by raw SQL, bypassing
// every Go code path, because CHRN-67's MCP reply tool is a second caller by
// design and a rule living in one handler is one it does not inherit.
func TestAgentAfterAgentIsRefusedBySQL(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "six-p@example.com")
	agent := discAgent(t, s, ctx, "six-a@example.com")

	d := openThread(t, s, ctx, person, "Loop")
	appendTurn(t, s, ctx, d.ID, agent, "agent replies once") // seq 2, legal

	// author_kind is deliberately not in the column list: CH092 refuses a
	// supplied one, so this is what a direct writer's INSERT has to look like.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tier2.discussion_turns (discussion_id, seq, author_id, body)
		VALUES ($1, 3, $2, 'and again')`, d.ID, agent)
	if got := sqlstate(err); got != pgAgentAfterAgent {
		t.Fatalf("agent-after-agent by raw SQL: SQLSTATE = %q (err %v), want %s", got, err, pgAgentAfterAgent)
	}

	// Through the package too, mapped to a sentinel CHRN-47 can act on.
	_, err = s.AppendTurn(ctx, NewTurn{DiscussionID: d.ID, AuthorID: agent, Body: "and again"})
	if !errors.Is(err, ErrAgentMayNotFollowAgent) {
		t.Errorf("AppendTurn err = %v, want ErrAgentMayNotFollowAgent", err)
	}
}

// Criterion 7 — AN AGENT CANNOT OPEN A THREAD. seq 1 has no preceding turn, so
// the same rule refuses it. Deferred rather than decided: whether an agent may
// ORIGINATE authored content is CHRN-67's argument at Mode C, mirroring
// CHRN-39's refusal to exempt seq 1 from CH041.
func TestAnAgentCannotOpenAThread(t *testing.T) {
	s, ctx := newTestStore(t)
	agent := discAgent(t, s, ctx, "seven@example.com")

	_, _, err := s.OpenDiscussion(ctx, NewDiscussion{
		Title: "Agent-opened", AuthorID: agent, Body: "shall we talk about retention",
	})
	if !errors.Is(err, ErrAgentMayNotFollowAgent) {
		t.Fatalf("OpenDiscussion by an agent err = %v, want ErrAgentMayNotFollowAgent", err)
	}

	// The rollback is the other half: a refused opening post must not leave a
	// thread with no turns behind it.
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tier2.discussions`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d discussions after a refused open, want 0", n)
	}

	// AND IN THE STORE, for a thread this package did not open. The SQLSTATE is
	// asserted on the raw path rather than the mapped one because
	// discussionError wraps the cause with %v, on note.go:731's house style —
	// the sentinel travels, the *pgconn.PgError does not.
	var bare uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`INSERT INTO tier2.discussions (title) VALUES ('Opened directly') RETURNING id`).Scan(&bare); err != nil {
		t.Fatalf("insert a bare discussion: %v", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tier2.discussion_turns (discussion_id, seq, author_id, body)
		VALUES ($1, 1, $2, 'shall we talk about retention')`, bare, agent)
	if got := sqlstate(err); got != pgAgentAfterAgent {
		t.Errorf("an agent at seq 1 by raw SQL: SQLSTATE = %q (err %v), want %s", got, err, pgAgentAfterAgent)
	}
}

// Criterion 8 — THE RULE REFUSES ONLY THE LOOP. Without this the test suite
// would pass on a guard that refused every agent turn, which is a different
// and much less useful rule.
func TestTheRuleRefusesOnlyTheLoop(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "eight-p@example.com")
	agent := discAgent(t, s, ctx, "eight-a@example.com")
	d := openThread(t, s, ctx, person, "Alternating")

	// person(1) → agent(2) → person(3) → agent(4), all accepted.
	second := appendTurn(t, s, ctx, d.ID, agent, "agent after a person")
	if !second.ByAgent() {
		t.Errorf("turn 2 author_kind = %q, want agent", second.AuthorKind)
	}
	third := appendTurn(t, s, ctx, d.ID, person, "person after an agent")
	if third.ByAgent() {
		t.Errorf("turn 3 author_kind = %q, want person", third.AuthorKind)
	}
	fourth := appendTurn(t, s, ctx, d.ID, agent, "agent again, legitimately")
	if fourth.Seq != 4 {
		t.Errorf("seq = %d, want 4", fourth.Seq)
	}

	// TWO DIFFERENT AGENTS ARE STILL A LOOP: the test is on kind, not identity,
	// which is what makes a loop impossible by construction rather than by
	// enumerating the agents that exist today.
	other := discAgent(t, s, ctx, "eight-a2@example.com")
	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: other, Body: "a second agent",
	}); !errors.Is(err, ErrAgentMayNotFollowAgent) {
		t.Errorf("a different agent following an agent err = %v, want ErrAgentMayNotFollowAgent", err)
	}
}

// ============================================================================
// Criterion 10 — ruling 5, and why author_kind is on the turn.
// ============================================================================

// THE THREE CLAIMS, and the third is the one the ruling exists for.
//
// Nothing in any migration freezes tier2.users.kind. Had CH091 joined to it,
// flipping an account person→agent would make a legal thread retroactively read
// as the loop the rule forbids, and agent→person would make a real loop
// retroactively legitimate. A safety property cannot rest on that.
func TestAuthorKindIsFrozenOnTheTurn(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "ten-p@example.com")
	agent := discAgent(t, s, ctx, "ten-a@example.com")
	d := openThread(t, s, ctx, person, "Frozen")

	// 1 — a caller supplying author_kind is refused.
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tier2.discussion_turns (discussion_id, seq, author_id, author_kind, body)
		VALUES ($1, 2, $2, 'person', 'I say what I am')`, d.ID, agent)
	if got := sqlstate(err); got != pgAuthorKindSupplied {
		t.Fatalf("supplying author_kind: SQLSTATE = %q (err %v), want %s", got, err, pgAuthorKindSupplied)
	}

	// 2 — the trigger sets it from tier2.users.kind.
	opening, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if opening[0].AuthorKind != KindPerson {
		t.Fatalf("turn 1 author_kind = %q, want %q", opening[0].AuthorKind, KindPerson)
	}

	// 3 — CHANGING THE ACCOUNT AFTERWARDS CHANGES NOTHING. Flip the opener to
	// an agent, then have a real agent reply: a guard reading users.kind live
	// would now see agent-after-agent and refuse. The frozen column does not.
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.users SET kind = 'agent' WHERE id = $1`, person); err != nil {
		t.Fatalf("flip the account kind: %v", err)
	}

	after, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns after the flip: %v", err)
	}
	if after[0].AuthorKind != KindPerson {
		t.Errorf("turn 1 author_kind = %q after the account was flipped, want %q — history was rewritten",
			after[0].AuthorKind, KindPerson)
	}

	reply, err := s.AppendTurn(ctx, NewTurn{DiscussionID: d.ID, AuthorID: agent, Body: "still legal"})
	if err != nil {
		t.Fatalf("an agent replying to a turn written by a then-person: %v — CH091 read a mutable column", err)
	}
	if reply.Seq != 2 {
		t.Errorf("seq = %d, want 2", reply.Seq)
	}
}

// ============================================================================
// Criterion 11 — ruling 6, a resolved thread takes no more turns (CH093).
// ============================================================================

func TestAResolvedThreadTakesNoMoreTurns(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "eleven@example.com")
	d := openThread(t, s, ctx, person, "Concluded")

	if err := s.ResolveDiscussion(ctx, d.ID, person, nil); err != nil {
		t.Fatalf("ResolveDiscussion: %v", err)
	}

	// Through the package, which refuses on the locked row before burning a seq.
	_, err := s.AppendTurn(ctx, NewTurn{DiscussionID: d.ID, AuthorID: person, Body: "one more thing"})
	if !errors.Is(err, ErrDiscussionResolved) {
		t.Errorf("AppendTurn on a resolved thread err = %v, want ErrDiscussionResolved", err)
	}

	// And in the store, for the direct writer that skipped the check above.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO tier2.discussion_turns (discussion_id, seq, author_id, body)
		VALUES ($1, 2, $2, 'one more thing')`, d.ID, person)
	if got := sqlstate(err); got != pgTurnOnResolved {
		t.Errorf("raw insert on a resolved thread: SQLSTATE = %q (err %v), want %s", got, err, pgTurnOnResolved)
	}
}

// ============================================================================
// Criterion 12 — CH080 is an allow list.
// ============================================================================

// PER COLUMN, because that is the only way to tell an allow list from a deny
// list that happens to name the same columns today. 0014:199-208 rewrote
// notes_guard for this reason: the day somebody adds a text-bearing column, a
// deny list permits writing it.
func TestDiscussionUpdateIsAnAllowList(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "twelve@example.com")
	page := mkPage(t, s, ctx, nil, "estate")
	d := openThread(t, s, ctx, person, "Mutable bits")

	permitted := []struct {
		name  string
		query string
		args  []any
	}{
		{"title", `UPDATE tier2.discussions SET title = 'Renamed' WHERE id = $1`, nil},
		{"page_id", `UPDATE tier2.discussions SET page_id = $2 WHERE id = $1`, []any{page.ID}},
	}
	for _, tc := range permitted {
		t.Run("permits/"+tc.name, func(t *testing.T) {
			args := append([]any{d.ID}, tc.args...)
			if _, err := s.pool.Exec(ctx, tc.query, args...); err != nil {
				t.Errorf("%s should be writable: %v", tc.name, err)
			}
		})
	}

	refused := []struct {
		name  string
		query string
	}{
		{"id", `UPDATE tier2.discussions SET id = gen_random_uuid() WHERE id = $1`},
		{"number", `UPDATE tier2.discussions SET number = 9999 WHERE id = $1`},
		{"created_at", `UPDATE tier2.discussions SET created_at = now() - interval '1 day' WHERE id = $1`},
	}
	for _, tc := range refused {
		t.Run("refuses/"+tc.name, func(t *testing.T) {
			_, err := s.pool.Exec(ctx, tc.query, d.ID)
			if got := sqlstate(err); got != pgDiscussionGuard {
				t.Errorf("%s SQLSTATE = %q (err %v), want %s", tc.name, got, err, pgDiscussionGuard)
			}
			if !errors.Is(discussionError(err), ErrDiscussionColumnFrozen) {
				t.Errorf("%s did not map to ErrDiscussionColumnFrozen: %v", tc.name, discussionError(err))
			}
		})
	}

	// THE RESOLUTION TRIPLE: settable once from NULL, and never cleared.
	if err := s.ResolveDiscussion(ctx, d.ID, person, nil); err != nil {
		t.Fatalf("ResolveDiscussion: %v", err)
	}
	t.Run("refuses/clearing resolved_at", func(t *testing.T) {
		_, err := s.pool.Exec(ctx,
			`UPDATE tier2.discussions SET resolved_at = NULL, resolved_by = NULL WHERE id = $1`, d.ID)
		if got := sqlstate(err); got != pgDiscussionGuard {
			t.Errorf("un-resolving SQLSTATE = %q (err %v), want %s", got, err, pgDiscussionGuard)
		}
		if !errors.Is(discussionError(err), ErrResolutionFixed) {
			t.Errorf("un-resolving did not map to ErrResolutionFixed: %v", discussionError(err))
		}
	})
	t.Run("refuses/rewriting resolved_by", func(t *testing.T) {
		other := discPerson(t, s, ctx, "twelve-other@example.com")
		_, err := s.pool.Exec(ctx,
			`UPDATE tier2.discussions SET resolved_by = $2 WHERE id = $1`, d.ID, other)
		if got := sqlstate(err); got != pgDiscussionGuard {
			t.Errorf("rewriting the resolver SQLSTATE = %q (err %v), want %s", got, err, pgDiscussionGuard)
		}
	})
}

// ============================================================================
// Criterion 13 — OpenDiscussion is one transaction, and returns the opener.
// ============================================================================

func TestOpenDiscussionIsAtomicAndReturnsTheOpener(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "thirteen@example.com")
	memo := newTranscribableMemo(t, s, ctx, "thirteen-memo@example.com")

	// A FORCED FAILURE ON THE TURN INSERT. An author that does not exist gets
	// past the discussion insert and dies on the turn, which is the ordering
	// that would leave a thread with no turns behind if this were two
	// transactions.
	_, _, err := s.OpenDiscussion(ctx, NewDiscussion{
		Title: "Doomed", AuthorID: uuid.New(), Body: "nobody wrote this",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("OpenDiscussion with an unknown author err = %v, want ErrNotFound", err)
	}
	var orphans int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tier2.discussions`).Scan(&orphans); err != nil {
		t.Fatalf("count: %v", err)
	}
	if orphans != 0 {
		t.Errorf("%d discussions after a failed open, want 0 — the two inserts are not one transaction", orphans)
	}

	// THE TURN IS WHERE THE OPENER AND THE OPENING MEMO ARE READ FROM, now that
	// opened_by and discussions.memo_id are gone: both were the same fact
	// written twice, and an unconstrained opened_by could have named an agent
	// while a person authored turn 1.
	d, turn, err := s.OpenDiscussion(ctx, NewDiscussion{
		Title: "Retention", AuthorID: person, Body: "how long do we keep audio", MemoID: &memo.ID,
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	if turn.Seq != 1 {
		t.Errorf("opening turn seq = %d, want 1", turn.Seq)
	}
	if turn.AuthorID != person {
		t.Errorf("opener = %s, want %s", turn.AuthorID, person)
	}
	if turn.MemoID == nil || *turn.MemoID != memo.ID {
		t.Errorf("opening memo = %v, want %s", turn.MemoID, memo.ID)
	}
	if turn.DiscussionID != d.ID {
		t.Errorf("turn belongs to %s, want %s", turn.DiscussionID, d.ID)
	}

	// The columns are gone, and stay gone: this is the assertion that catches
	// somebody re-adding one for convenience.
	for _, col := range []string{"opened_by", "memo_id"} {
		var exists bool
		if err := s.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			                WHERE table_schema = 'tier2' AND table_name = 'discussions'
			                  AND column_name = $1)`, col).Scan(&exists); err != nil {
			t.Fatalf("column check: %v", err)
		}
		if exists {
			t.Errorf("tier2.discussions.%s exists; it is derivable from turn 1 and was removed for that reason", col)
		}
	}
}

// ============================================================================
// Criterion 14 — the author's type is a fact about the row.
// ============================================================================

// "Without joining a mutable column" is the operative half. Read straight off
// tier2.discussion_turns with no join at all, which is also literally what
// CHRN-44's "the distinction survives export" asks for.
func TestEveryTurnCarriesItsAuthorType(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "fourteen-p@example.com")
	agent := discAgent(t, s, ctx, "fourteen-a@example.com")
	d := openThread(t, s, ctx, person, "Attribution")
	appendTurn(t, s, ctx, d.ID, agent, "an agent said this")

	rows, err := s.pool.Query(ctx,
		`SELECT seq, author_kind FROM tier2.discussion_turns
		  WHERE discussion_id = $1 ORDER BY seq`, d.ID)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[int]string{}
	for rows.Next() {
		var seq int
		var kind string
		if err := rows.Scan(&seq, &kind); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[seq] = kind
	}
	if rows.Err() != nil {
		t.Fatalf("rows: %v", rows.Err())
	}
	if got[1] != KindPerson || got[2] != KindAgent {
		t.Errorf("author kinds = %v, want {1: person, 2: agent} read from the turn alone", got)
	}
}

// ============================================================================
// Criterion 15 — one memo, at most one turn.
// ============================================================================

// 0011:142-146's argument transferred: tier2.memo_links is UNIQUE (memo_id), so
// a memo lands exactly once. A plain index here would silently permit one memo
// to author several turns — a different decision from CHRN-33's, made by
// omission.
func TestAMemoAuthorsAtMostOneTurn(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "fifteen@example.com")
	memo := newTranscribableMemo(t, s, ctx, "fifteen-memo@example.com")
	d := openThread(t, s, ctx, person, "One memo")

	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: person, Body: "from a memo", MemoID: &memo.ID,
	}); err != nil {
		t.Fatalf("first turn from a memo: %v", err)
	}
	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: person, Body: "again", MemoID: &memo.ID,
	}); !errors.Is(err, ErrMemoAlreadySpoke) {
		t.Errorf("second turn from the same memo err = %v, want ErrMemoAlreadySpoke", err)
	}

	// ANY NUMBER WITH A NULL ONE. A partial unique index is what buys this; a
	// plain UNIQUE would make the second directly-typed turn a conflict.
	for i := 0; i < 3; i++ {
		appendTurn(t, s, ctx, d.ID, person, "typed directly")
	}
	read, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(read) != 5 {
		t.Errorf("%d turns, want 5 (open + memo + three authored)", len(read))
	}
}

// ============================================================================
// Criteria 16 and 18 — participants.
// ============================================================================

// Criterion 16 — REMOVAL IS A STATE CHANGE, NOT A DELETION. A turn carries its
// own author_id and author_kind and nothing reads membership to decide
// authorship, which is exactly why this table can be current state rather than
// the journal tier2.note_deletions had to be.
func TestRemovingAParticipantLeavesTheirTurnsAlone(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "sixteen-o@example.com")
	guest := discPerson(t, s, ctx, "sixteen-g@example.com")
	d := openThread(t, s, ctx, owner, "Membership")

	if err := s.AddParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	said := appendTurn(t, s, ctx, d.ID, guest, "something worth keeping")

	if err := s.RemoveParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}

	read, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("%d turns after a removal, want 2", len(read))
	}
	if read[1].ID != said.ID || read[1].Body != "something worth keeping" {
		t.Errorf("the removed participant's turn changed: %+v", read[1])
	}
	if read[1].AuthorID != guest || read[1].AuthorKind != KindPerson {
		t.Errorf("the removed participant's turn lost its author: %+v", read[1])
	}

	// They are still listed, carrying their removal — a reader rendering the
	// thread needs them, because their turns are still in it.
	ps, err := s.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(ps) != 1 || ps[0].UserID != guest || ps[0].Active() {
		t.Fatalf("participants = %+v, want the guest, removed", ps)
	}
	firstAdd := ps[0].AddedAt

	// A RE-ADD CLEARS THE REMOVAL AND DOES NOT REATTRIBUTE THE FIRST ONE.
	// added_at / added_by mean FIRST added, frozen by CH100.
	if err := s.AddParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("re-add: %v", err)
	}
	ps, err = s.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(ps) != 1 || !ps[0].Active() {
		t.Fatalf("participants after a re-add = %+v, want one active", ps)
	}
	if !ps[0].AddedAt.Equal(firstAdd) {
		t.Errorf("added_at moved on a re-add: %s -> %s; it means FIRST added", firstAdd, ps[0].AddedAt)
	}

	// And the allow list refuses rewriting it directly.
	_, err = s.pool.Exec(ctx,
		`UPDATE tier2.discussion_participants SET added_at = now() WHERE discussion_id = $1 AND user_id = $2`,
		d.ID, guest)
	if got := sqlstate(err); got != pgParticipantGuard {
		t.Errorf("rewriting added_at: SQLSTATE = %q (err %v), want %s", got, err, pgParticipantGuard)
	}
	if !errors.Is(discussionError(err), ErrParticipantColumnFrozen) {
		t.Errorf("rewriting added_at did not map to ErrParticipantColumnFrozen: %v", discussionError(err))
	}
}

// Criterion 18 — A PERSON RESOLVES, AND A PERSON ADDS AND REMOVES. The same
// rule CH041 states about a note's confirmer, and the reason is the same: these
// are acts on the corpus, not contributions to it.
func TestOnlyAPersonResolvesOrChangesMembership(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "eighteen-p@example.com")
	agent := discAgent(t, s, ctx, "eighteen-a@example.com")
	d := openThread(t, s, ctx, person, "Who may")

	if err := s.ResolveDiscussion(ctx, d.ID, agent, nil); !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("ResolveDiscussion by an agent err = %v, want ErrConfirmerRequired", err)
	}
	if err := s.AddParticipant(ctx, d.ID, person, agent); !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("AddParticipant by an agent err = %v, want ErrConfirmerRequired", err)
	}

	// The removal half is asserted separately, because an INSERT-only person
	// test would leave it reading as enforced while doing nothing — the failure
	// 0014:378-384 names one table over.
	if err := s.AddParticipant(ctx, d.ID, agent, person); err != nil {
		t.Fatalf("AddParticipant (the agent as a participant, which is fine): %v", err)
	}
	if err := s.RemoveParticipant(ctx, d.ID, agent, agent); !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("RemoveParticipant by an agent err = %v, want ErrConfirmerRequired", err)
	}
}

// ============================================================================
// Criterion 17 — resolving.
// ============================================================================

func TestResolvingRecordsWhatAThreadConcluded(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "seventeen@example.com")
	page := mkPage(t, s, ctx, nil, "estate")

	// RESOLVING WITH NO NOTE IS ACCEPTED. Some threads just end, and CHRN-46
	// wants that to be a deliberate choice rather than an error.
	bare := openThread(t, s, ctx, person, "Just ended")
	if err := s.ResolveDiscussion(ctx, bare.ID, person, nil); err != nil {
		t.Fatalf("ResolveDiscussion with no note: %v", err)
	}
	got, err := s.DiscussionByID(ctx, bare.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if !got.Resolved() || got.ResolvedBy == nil || *got.ResolvedBy != person {
		t.Errorf("resolution = %+v, want resolved by %s", got, person)
	}
	if got.ResolvedNoteID != nil {
		t.Errorf("resolved_note_id = %v, want nil", got.ResolvedNoteID)
	}

	// RESOLVING INTO AN EXISTING NOTE APPENDS A REVISION RATHER THAN CREATING A
	// SECOND NOTE. CHRN-46's own example — "Resolved into PRINCIPLES §6" — is a
	// section of something already written, so this is the ordinary case and
	// not the exotic one.
	principles := mkNote(t, s, ctx, page.ID, person, "PRINCIPLES", "§1..§5")
	before := countNotes(t, s, ctx)
	if _, err := s.AppendRevision(ctx, principles.ID, NewRevision{
		AuthorID: person, ConfirmedBy: person, Title: "PRINCIPLES", Body: "§1..§6",
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}

	d := openThread(t, s, ctx, person, "How long do we keep audio")
	if err := s.ResolveDiscussion(ctx, d.ID, person, &principles.ID); err != nil {
		t.Fatalf("ResolveDiscussion into an existing note: %v", err)
	}
	if after := countNotes(t, s, ctx); after != before {
		t.Errorf("notes went from %d to %d; resolving into an existing note created one", before, after)
	}
	revs, err := s.NoteRevisions(ctx, principles.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("%d revisions on the resolved-into note, want 2", len(revs))
	}
	// RULING 4 — the revision a resolution writes carries verb NULL, with
	// discussions.resolved_note_id as its provenance. 0014:84-87's shape for a
	// restore, applied to a resolution.
	if revs[1].Verb != nil {
		t.Errorf("the resolution's revision carries verb %q, want NULL (ruling 4)", *revs[1].Verb)
	}

	got, err = s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if got.ResolvedNoteID == nil || *got.ResolvedNoteID != principles.ID {
		t.Errorf("resolved_note_id = %v, want %s", got.ResolvedNoteID, principles.ID)
	}

	// A NOTE WITH NO RESOLUTION IS NOT A STATE — it would claim a thread
	// produced something while saying nothing about when or by whom.
	open := openThread(t, s, ctx, person, "Still going")
	_, err = s.pool.Exec(ctx,
		`UPDATE tier2.discussions SET resolved_note_id = $2 WHERE id = $1`, open.ID, principles.ID)
	if err == nil {
		t.Error("a resolved_note_id with no resolved_at was accepted")
	}

	// A LATER LINK COMPLETES THE RECORD; A DIFFERENT ONE REWRITES IT AND IS
	// REFUSED. This is why CH080's once-from-NULL clause is per column.
	if err := s.ResolveDiscussion(ctx, bare.ID, person, &principles.ID); err != nil {
		t.Errorf("linking a note to an already-resolved thread: %v — a resolution should be completable", err)
	}
	other := mkNote(t, s, ctx, page.ID, person, "SOMETHING ELSE", "x")
	if err := s.ResolveDiscussion(ctx, bare.ID, person, &other.ID); !errors.Is(err, ErrResolutionFixed) {
		t.Errorf("relinking to a different note err = %v, want ErrResolutionFixed", err)
	}
}

func countNotes(t *testing.T, s *Store, ctx context.Context) int {
	t.Helper()
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tier2.notes`).Scan(&n); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	return n
}

// ============================================================================
// Criterion 19 — the handle (ruling 2). Unit, no database.
// ============================================================================

// LENIENT IN, STRICT OUT, and the last two cases are CHRN-94's lesson: a value
// that passes a regex and fails ParseInt is well-formed in one place and
// unresolvable in the other, so the parser has to PARSE rather than
// pattern-match.
func TestParseAndFormatDiscussionRef(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"DSC-7", 7},
		{"dsc-0007", 7},
		{"DSC-00007", 7},
		{"DSC-0007", 7},
		{"Dsc-10000", 10000},
	} {
		got, err := ParseDiscussionRef(tc.in)
		if err != nil {
			t.Errorf("ParseDiscussionRef(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseDiscussionRef(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}

	for _, bad := range []string{
		"", "7", "DSC7", "DSC-", "DSC-x", "CHR-0007", "DSC-0007x", " DSC-7",
		// Zero and negative: the sequence starts at 1, so DSC-0000 names
		// nothing and must not resolve to a lookup for 0.
		"DSC-0", "DSC-0000",
		// int64 OVERFLOW. Matches the pattern and cannot be a number, which is
		// the exact shape CHRN-94's reviewer found in IsNoteRef.
		"DSC-99999999999999999999",
	} {
		if got, err := ParseDiscussionRef(bad); err == nil {
			t.Errorf("ParseDiscussionRef(%q) = %d, want an error", bad, got)
		} else if !errors.Is(err, ErrInvalidDiscussionRef) {
			t.Errorf("ParseDiscussionRef(%q) err = %v, want ErrInvalidDiscussionRef", bad, err)
		}
	}

	// STRICT OUT: four digits as a MINIMUM width, not a cap.
	for n, want := range map[int64]string{1: "DSC-0001", 7: "DSC-0007", 9999: "DSC-9999", 10000: "DSC-10000"} {
		if got := FormatDiscussionRef(n); got != want {
			t.Errorf("FormatDiscussionRef(%d) = %q, want %q", n, got, want)
		}
	}
	if !strings.HasPrefix(FormatDiscussionRef(1), "DSC-") {
		t.Error("FormatDiscussionRef lost its prefix")
	}
}

// The handle resolves, which is the half a parser test cannot assert.
func TestDiscussionByRefResolvesTheHandle(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "nineteen@example.com")
	d := openThread(t, s, ctx, person, "Addressable")

	for _, ref := range []string{d.Ref(), strings.ToLower(d.Ref()), FormatDiscussionRef(d.Number)} {
		got, err := s.DiscussionByRef(ctx, ref)
		if err != nil {
			t.Fatalf("DiscussionByRef(%q): %v", ref, err)
		}
		if got.ID != d.ID {
			t.Errorf("DiscussionByRef(%q) = %s, want %s", ref, got.ID, d.ID)
		}
	}

	// Never reused, gaps correct: a rolled-back open burns a number, and a
	// burned number is strictly better than one that resolves to two threads
	// across time.
	if _, _, err := s.OpenDiscussion(ctx, NewDiscussion{
		Title: "Doomed", AuthorID: uuid.New(), Body: "x",
	}); err == nil {
		t.Fatal("expected the doomed open to fail")
	}
	next := openThread(t, s, ctx, person, "After the gap")
	if next.Number <= d.Number {
		t.Errorf("numbers went backwards: %d then %d", d.Number, next.Number)
	}
	if _, err := s.DiscussionByNumber(ctx, d.Number+1); !errors.Is(err, ErrNotFound) {
		t.Errorf("the burned number resolves to something: %v", err)
	}
}
