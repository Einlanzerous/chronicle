package store

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// CHRN-45's twenty-five acceptance criteria, from the Switchyard plan revision
// 2 approved 2026-09-08 with all eight rulings picked. Criteria 0, 1, 23 and 24
// are migration-CI and review checks with no Go assertion to make; everything
// else is here, numbered as it is there.

func unread(t *testing.T, s *Store, ctx context.Context, d, u uuid.UUID) int {
	t.Helper()
	n, err := s.UnreadCount(ctx, d, u)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	return n
}

// markerOf reads the raw column, which is the only way to tell "never read"
// from "read nothing" — the distinction NULL carries and the count does not.
func markerOf(t *testing.T, s *Store, ctx context.Context, d, u uuid.UUID) *int {
	t.Helper()
	var seq *int
	if err := s.pool.QueryRow(ctx,
		`SELECT last_read_seq FROM tier2.discussion_participants
		  WHERE discussion_id = $1 AND user_id = $2`, d, u).Scan(&seq); err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return seq
}

// openSecondPool is a genuinely separate connection pool — the point of the two
// `Done when` clauses about sessions is that the marker is server-side, and two
// calls on one pool would not show that.
func openSecondPool(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	pool, err := Connect(ctx, testDSN(t))
	if err != nil {
		t.Fatalf("second pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return New(pool)
}

// ============================================================================
// Criterion 2 — the tier boundary.
// ============================================================================

// The marker is two columns on a table 0015 already revoked, so this asserts
// the property rather than re-testing 0015: a column inherits its table's
// privileges, and that is why 0016 creates nothing to revoke.
func TestTier1CannotReachTheReadMarker(t *testing.T) {
	_, ctx := newTestStore(t)
	pool := tier1Pool(t, ctx)
	defer pool.Close()

	for _, priv := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
		var held bool
		if err := pool.QueryRow(ctx,
			`SELECT has_table_privilege(current_user, 'tier2.discussion_participants', $1)`,
			priv).Scan(&held); err != nil {
			t.Fatalf("privilege check %s: %v", priv, err)
		}
		if held {
			t.Errorf("chronicle_tier1 holds %s on tier2.discussion_participants", priv)
		}
	}
	// And the column specifically, since a column grant is a thing that exists.
	var held bool
	if err := pool.QueryRow(ctx,
		`SELECT has_column_privilege(current_user, 'tier2.discussion_participants', 'last_read_seq', 'SELECT')`).
		Scan(&held); err != nil {
		t.Fatalf("column privilege check: %v", err)
	}
	if held {
		t.Error("chronicle_tier1 can read last_read_seq; a column grant was issued")
	}
}

// ============================================================================
// Criteria 3, 4 — the two `Done when` clauses about sessions.
// ============================================================================

// Done when 1. Two INDEPENDENT POOLS, not two calls on one — the point is that
// the marker is server-side, so there is no cache in either to invalidate.
func TestMarkingReadInOneSessionIsVisibleInAnother(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "sessions@example.com")
	other := discPerson(t, s, ctx, "sessions-2@example.com")
	d := openThread(t, s, ctx, person, "Two sessions")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, d.ID, person, "two")
	appendTurn(t, s, ctx, d.ID, person, "three")

	first := New(s.pool)
	second := openSecondPool(t, ctx)

	if got := unread(t, second, ctx, d.ID, other); got != 3 {
		t.Fatalf("unread before marking = %d, want 3", got)
	}
	if err := first.MarkRead(ctx, d.ID, other, 2); err != nil {
		t.Fatalf("MarkRead through session one: %v", err)
	}
	if got := unread(t, second, ctx, d.ID, other); got != 1 {
		t.Errorf("session two sees %d unread, want 1 — the marker did not cross", got)
	}
}

// Done when 2. The pool that marked read is CLOSED, so nothing it held can be
// answering; a fresh connection reads the same number.
func TestUnreadCountsSurviveAReconnect(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "reconnect@example.com")
	other := discPerson(t, s, ctx, "reconnect-2@example.com")
	d := openThread(t, s, ctx, person, "Reconnect")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, d.ID, person, "two")
	appendTurn(t, s, ctx, d.ID, person, "three")

	marking := openSecondPool(t, ctx)
	if err := marking.MarkRead(ctx, d.ID, other, 2); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	marking.pool.Close()

	fresh := openSecondPool(t, ctx)
	if got := unread(t, fresh, ctx, d.ID, other); got != 1 {
		t.Errorf("after a reconnect: %d unread, want 1", got)
	}
}

// ============================================================================
// Criterion 5 — THE clause. Exactly one, not more than zero.
// ============================================================================

// A person opens a thread and the Scribe answers. Both failure modes the
// `Done when` names are reachable and neither is exotic: "two" if nothing
// advances the author's own marker, "none" if the agent's turn advances the
// human's. Asserted as == 1 rather than > 0, because > 0 passes on both.
func TestAnAgentReplyProducesExactlyOneUnread(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "exactly-one@example.com")
	scribe := discAgent(t, s, ctx, "exactly-one-agent@example.com")

	d := openThread(t, s, ctx, person, "How long do we keep audio")
	// The opener has read their own turn — ruling 3.
	if got := unread(t, s, ctx, d.ID, person); got != 0 {
		t.Fatalf("unread after opening = %d, want 0 — the opener's own turn counted", got)
	}

	appendTurn(t, s, ctx, d.ID, scribe, "thirty days, gated on a durable transcript")

	if got := unread(t, s, ctx, d.ID, person); got != 1 {
		t.Errorf("unread after the agent replied = %d, want EXACTLY 1", got)
	}
	// And the agent did not read for them.
	if seq := markerOf(t, s, ctx, d.ID, person); seq == nil || *seq != 1 {
		t.Errorf("the person's marker = %v, want 1 — the agent's turn moved it", seq)
	}
}

// ============================================================================
// Criteria 6, 7, 8, 19 — ruling 3, and what posting does to membership.
// ============================================================================

// Criterion 6 — SAME TRANSACTION. A refused append must leave the marker where
// it was; a refused reply that silently marked a thread read would be worse
// than the bug ruling 3 avoids.
func TestARefusedAppendLeavesTheMarkerAlone(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "atomic-marker@example.com")
	scribe := discAgent(t, s, ctx, "atomic-marker-agent@example.com")
	d := openThread(t, s, ctx, person, "Atomic")
	appendTurn(t, s, ctx, d.ID, scribe, "agent, legally")

	before := markerOf(t, s, ctx, d.ID, person)

	// CH091: an agent may not follow an agent.
	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: scribe, Body: "and again",
	}); !errors.Is(err, ErrAgentMayNotFollowAgent) {
		t.Fatalf("expected CH091, got %v", err)
	}
	// CH093: a resolved thread takes no more turns.
	appendTurn(t, s, ctx, d.ID, person, "three")
	mid := markerOf(t, s, ctx, d.ID, person)
	if err := s.ResolveDiscussion(ctx, d.ID, person, nil); err != nil {
		t.Fatalf("ResolveDiscussion: %v", err)
	}
	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: person, Body: "one more",
	}); !errors.Is(err, ErrDiscussionResolved) {
		t.Fatalf("expected CH093, got %v", err)
	}

	after := markerOf(t, s, ctx, d.ID, person)
	if before == nil || mid == nil || after == nil {
		t.Fatalf("markers went nil: %v %v %v", before, mid, after)
	}
	if *after != *mid {
		t.Errorf("a refused append moved the marker: %d -> %d", *mid, *after)
	}
}

// Criterion 7 — posting into a thread you were not on joins you to it, with
// added_by naming yourself. 0015 already said "a person who posts is thereby
// in the conversation"; this is that sentence made true.
func TestPostingJoinsTheThread(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "joins-o@example.com")
	guest := discPerson(t, s, ctx, "joins-g@example.com")
	d := openThread(t, s, ctx, owner, "Joining")

	ps, err := s.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(ps) != 1 {
		t.Fatalf("%d participants before the guest speaks, want 1 (the opener)", len(ps))
	}

	turn := appendTurn(t, s, ctx, d.ID, guest, "a passer-by speaks")

	ps, err = s.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	var found bool
	for _, p := range ps {
		if p.UserID == guest {
			found = true
			if p.AddedBy != guest {
				t.Errorf("added_by = %s, want the author themselves (%s)", p.AddedBy, guest)
			}
			if p.LastReadSeq == nil || *p.LastReadSeq != turn.Seq {
				t.Errorf("marker = %v, want %d — their own turn", p.LastReadSeq, turn.Seq)
			}
		}
	}
	if !found {
		t.Error("posting did not put the author on the thread")
	}
}

// Criterion 8 — THE AGENT APPEND IS NOT ABORTED BY ITS OWN UPSERT.
//
// CH100 requires added_by to be a person, and a BEFORE INSERT trigger fires on
// INSERT … ON CONFLICT DO UPDATE even when the row already exists and only the
// update path can run — so an unconditional self-upsert would refuse the
// Scribe's participant row and, in the same transaction, take its reply with
// it. BOTH cases are asserted because the "already a participant" one is the
// counter-intuitive half.
func TestAnAgentCanReplyWhetherOrNotItIsAParticipant(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "agentreply-p@example.com")
	scribe := discAgent(t, s, ctx, "agentreply-a@example.com")

	// (a) not a participant.
	stranger := openThread(t, s, ctx, person, "Uninvited")
	turn := appendTurn(t, s, ctx, stranger.ID, scribe, "replying without an invitation")
	if !turn.ByAgent() {
		t.Errorf("turn author kind = %q, want agent", turn.AuthorKind)
	}
	for _, p := range mustParticipants(t, s, ctx, stranger.ID) {
		if p.UserID == scribe {
			t.Error("the agent joined the thread by speaking; ruling 4 says it carries no marker and ruling 3 skips it")
		}
	}

	// (b) already a participant, added properly by a person.
	invited := openThread(t, s, ctx, person, "Invited")
	if err := s.AddParticipant(ctx, invited.ID, scribe, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: invited.ID, AuthorID: scribe, Body: "replying as a member",
	}); err != nil {
		t.Fatalf("the agent could not reply on a thread it is a participant of: %v", err)
	}
	if seq := markerOf(t, s, ctx, invited.ID, scribe); seq != nil {
		t.Errorf("the agent's marker = %v, want NULL (ruling 4)", *seq)
	}
}

func mustParticipants(t *testing.T, s *Store, ctx context.Context, d uuid.UUID) []DiscussionParticipant {
	t.Helper()
	ps, err := s.Participants(ctx, d)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	return ps
}

// Criterion 19, ruling 7 — A REMOVED PERSON WHO POSTS STAYS REMOVED.
//
// Nothing requires membership to post, so this is reachable. Clearing the
// removal here would let anybody a person removed put themselves back by
// speaking, with the remover neither consulted nor told. Asserted on
// removed_at directly, because Participants alone would not distinguish it.
func TestARemovedPersonWhoPostsStaysRemoved(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "removed-o@example.com")
	guest := discPerson(t, s, ctx, "removed-g@example.com")
	d := openThread(t, s, ctx, owner, "Removed")

	if err := s.AddParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if err := s.RemoveParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}

	turn := appendTurn(t, s, ctx, d.ID, guest, "speaking anyway")

	var removedAt *string
	var seq *int
	if err := s.pool.QueryRow(ctx,
		`SELECT removed_at::text, last_read_seq FROM tier2.discussion_participants
		  WHERE discussion_id = $1 AND user_id = $2`, d.ID, guest).Scan(&removedAt, &seq); err != nil {
		t.Fatalf("read the row: %v", err)
	}
	if removedAt == nil {
		t.Error("posting cleared the removal; anybody removed can now re-admit themselves by speaking")
	}
	if seq == nil || *seq != turn.Seq {
		t.Errorf("marker = %v, want %d — the marker should advance even though membership does not", seq, turn.Seq)
	}

	// And they stay out of the badge, because removal means "not expected to
	// read this". Asserted after a turn they have NOT read, so the thread would
	// otherwise qualify.
	appendTurn(t, s, ctx, d.ID, owner, "a later turn they have not read")
	agg, err := s.UnreadByDiscussion(ctx, guest)
	if err != nil {
		t.Fatalf("UnreadByDiscussion: %v", err)
	}
	if _, ok := agg[d.ID]; ok {
		t.Errorf("a removed participant's thread is in the badge: %v", agg)
	}
}

// ============================================================================
// Criteria 9, 10 — ruling 4, ENFORCED rather than stated.
// ============================================================================

func TestAnAgentCarriesNoReadMarker(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "nomarker-p@example.com")
	scribe := discAgent(t, s, ctx, "nomarker-a@example.com")
	d := openThread(t, s, ctx, person, "No marker")
	if err := s.AddParticipant(ctx, d.ID, scribe, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	// Through the package — a message, mirroring requireActor.
	if err := s.MarkRead(ctx, d.ID, scribe, 1); !errors.Is(err, ErrAgentHasNoMarker) {
		t.Errorf("MarkRead for an agent err = %v, want ErrAgentHasNoMarker", err)
	}

	// AND IN THE STORE, which is the half that matters: revision 1 of the plan
	// stated this in Go, and a promise a handler keeps is one CHRN-67's MCP
	// tools do not inherit.
	_, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussion_participants SET last_read_seq = 1, last_read_at = now()
		 WHERE discussion_id = $1 AND user_id = $2`, d.ID, scribe)
	if got := sqlstate(err); got != pgAgentHasNoMarker {
		t.Errorf("raw UPDATE of an agent's marker: SQLSTATE = %q (err %v), want %s",
			got, err, pgAgentHasNoMarker)
	}
	if !errors.Is(discussionError(err), ErrAgentHasNoMarker) {
		t.Errorf("CH101 did not map: %v", discussionError(err))
	}

	// Criterion 10 — and the aggregate is empty for it.
	agg, err := s.UnreadByDiscussion(ctx, scribe)
	if err != nil {
		t.Fatalf("UnreadByDiscussion(agent): %v", err)
	}
	if len(agg) != 0 {
		t.Errorf("the agent's badge = %v, want empty", agg)
	}
}

// ============================================================================
// Criteria 11, 12, 13, 14 — ruling 5, both bounds.
// ============================================================================

// Criterion 11 — a stale report is a NO-OP, not an error. A phone showing turn
// 4 while the web already marked 9 is being slow, not wrong.
func TestAStaleMarkReadDoesNotRewind(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "stale@example.com")
	other := discPerson(t, s, ctx, "stale-2@example.com")
	d := openThread(t, s, ctx, person, "Stale")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	for i := 0; i < 8; i++ {
		appendTurn(t, s, ctx, d.ID, person, "turn")
	}

	if err := s.MarkRead(ctx, d.ID, other, 9); err != nil {
		t.Fatalf("MarkRead(9): %v", err)
	}
	if err := s.MarkRead(ctx, d.ID, other, 4); err != nil {
		t.Errorf("a stale MarkRead returned an error: %v — lag is not a mistake", err)
	}
	if seq := markerOf(t, s, ctx, d.ID, other); seq == nil || *seq != 9 {
		t.Errorf("marker = %v after a stale report, want 9", seq)
	}
}

// Criterion 12 — and a direct rewind IS refused, because CHRN-67's tools are a
// second writer by design. Asserted on the raw path, since GREATEST makes it
// unreachable through the package.
func TestADirectRewindIsRefused(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "rewind@example.com")
	d := openThread(t, s, ctx, person, "Rewind")
	appendTurn(t, s, ctx, d.ID, person, "two")
	appendTurn(t, s, ctx, d.ID, person, "three")

	_, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussion_participants SET last_read_seq = 1
		 WHERE discussion_id = $1 AND user_id = $2`, d.ID, person)
	if got := sqlstate(err); got != pgParticipantGuard {
		t.Errorf("a direct rewind: SQLSTATE = %q (err %v), want %s", got, err, pgParticipantGuard)
	}
}

// Criterion 13 — THE UPPER BOUND, and the failure it prevents is silent.
//
// Ruling 5 only ever forbade a decrease. An unbounded increase means
// MarkRead(999) on a five-turn thread sits at 999 forever and reports 0 unread
// until the thread has a thousand turns — this ticket's own "the web says
// none", from one off-by-one against a stale turn list.
func TestAMarkerCannotRunPastTheThread(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "ahead@example.com")
	other := discPerson(t, s, ctx, "ahead-2@example.com")
	d := openThread(t, s, ctx, person, "Ahead")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	for i := 0; i < 4; i++ {
		appendTurn(t, s, ctx, d.ID, person, "turn")
	}

	if err := s.MarkRead(ctx, d.ID, other, 999); err != nil {
		t.Fatalf("MarkRead(999): %v", err)
	}
	if seq := markerOf(t, s, ctx, d.ID, other); seq == nil || *seq != 5 {
		t.Fatalf("marker = %v after marking through 999, want 5 (the thread's head)", seq)
	}
	if got := unread(t, s, ctx, d.ID, other); got != 0 {
		t.Errorf("unread = %d, want 0", got)
	}

	// THE HALF THAT PROVES IT WAS CLAMPED rather than merely reported as 0: one
	// more turn makes the count 1. Unclamped, it would stay 0 for 994 more.
	appendTurn(t, s, ctx, d.ID, person, "the next one")
	if got := unread(t, s, ctx, d.ID, other); got != 1 {
		t.Errorf("unread after one more turn = %d, want 1 — the marker was not clamped", got)
	}

	// And the guard refuses it on the raw path.
	_, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussion_participants SET last_read_seq = 99, last_read_at = now()
		 WHERE discussion_id = $1 AND user_id = $2`, d.ID, other)
	if got := sqlstate(err); got != pgMarkerPastTheThread {
		t.Errorf("a direct marker past the thread: SQLSTATE = %q (err %v), want %s",
			got, err, pgMarkerPastTheThread)
	}
}

// Criterion 14 — concurrent marks converge on the highest, none lost.
func TestConcurrentMarksConvergeOnTheHighest(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "converge@example.com")
	other := discPerson(t, s, ctx, "converge-2@example.com")
	d := openThread(t, s, ctx, person, "Converge")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	const turns = 8
	for i := 0; i < turns-1; i++ {
		appendTurn(t, s, ctx, d.ID, person, "turn")
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, turns)
	for i := 0; i < turns; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = s.MarkRead(ctx, d.ID, other, i+1)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("mark %d: %v", i+1, err)
		}
	}
	if seq := markerOf(t, s, ctx, d.ID, other); seq == nil || *seq != turns {
		t.Errorf("marker = %v after concurrent marks, want %d", seq, turns)
	}
}

// ============================================================================
// Criteria 15, 16 — the count is exact, and NULL counts as nothing read.
// ============================================================================

// SUBTRACTION, NOT count(*) — exact because seq is dense. Both mechanisms that
// keep it dense are exercised: a rolled-back append leaves no gap, and CH090
// makes a gap unrepresentable afterwards.
func TestTheUnreadCountIsExactBecauseSeqIsDense(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "dense@example.com")
	other := discPerson(t, s, ctx, "dense-2@example.com")
	d := openThread(t, s, ctx, person, "Dense")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}

	const n = 6
	for i := 0; i < n; i++ {
		appendTurn(t, s, ctx, d.ID, person, "turn")
	}

	// A REFUSED APPEND BURNS NO SEQ, unlike discussions.number's sequence.
	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: uuid.New(), Body: "doomed",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected the doomed append to fail with ErrNotFound, got %v", err)
	}

	if got := unread(t, s, ctx, d.ID, other); got != n+1 {
		t.Errorf("unread = %d, want %d (the opening turn plus %d)", got, n+1, n)
	}

	// The arithmetic and a count(*) agree, which is the whole claim.
	var counted, arithmetic int
	if err := s.pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM tier2.discussion_turns WHERE discussion_id = $1),
		       (SELECT COALESCE(MAX(seq), 0) FROM tier2.discussion_turns WHERE discussion_id = $1)`,
		d.ID).Scan(&counted, &arithmetic); err != nil {
		t.Fatalf("compare: %v", err)
	}
	if counted != arithmetic {
		t.Errorf("count(*) = %d but MAX(seq) = %d — the sequence has a gap and the arithmetic is wrong",
			counted, arithmetic)
	}
}

// Criterion 16 — a never-read participant is behind by the whole thread, and
// the column is still distinguishably NULL.
func TestANeverReadParticipantIsBehindByTheWholeThread(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "neverread@example.com")
	other := discPerson(t, s, ctx, "neverread-2@example.com")
	d := openThread(t, s, ctx, person, "Never read")
	if err := s.AddParticipant(ctx, d.ID, other, person); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, d.ID, person, "two")
	appendTurn(t, s, ctx, d.ID, person, "three")

	if got := unread(t, s, ctx, d.ID, other); got != 3 {
		t.Errorf("unread = %d, want 3", got)
	}
	if seq := markerOf(t, s, ctx, d.ID, other); seq != nil {
		t.Errorf("marker = %v, want NULL — never read is not read-nothing", *seq)
	}

	// Marking through 0 is a DIFFERENT state that counts the same, which is
	// what makes keeping NULL free.
	if err := s.MarkRead(ctx, d.ID, other, 0); err != nil {
		t.Fatalf("MarkRead(0): %v", err)
	}
	if seq := markerOf(t, s, ctx, d.ID, other); seq == nil || *seq != 0 {
		t.Errorf("marker = %v after marking 0, want 0 and not NULL", seq)
	}
	if got := unread(t, s, ctx, d.ID, other); got != 3 {
		t.Errorf("unread after marking 0 = %d, want 3 — same count, different state", got)
	}
}

// ============================================================================
// Criteria 17, 18 — the allow list, and the marker across a re-add.
// ============================================================================

func TestTheParticipantAllowListNamesTheMarker(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "allowlist@example.com")
	d := openThread(t, s, ctx, person, "Allow list")
	appendTurn(t, s, ctx, d.ID, person, "two")

	// Permitted.
	if _, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussion_participants SET last_read_seq = 2, last_read_at = now()
		 WHERE discussion_id = $1 AND user_id = $2`, d.ID, person); err != nil {
		t.Errorf("the marker columns should be writable: %v", err)
	}

	for _, tc := range []struct{ name, query string }{
		{"added_at", `UPDATE tier2.discussion_participants SET added_at = now() - interval '1 day' WHERE discussion_id = $1 AND user_id = $2`},
		{"discussion_id", `UPDATE tier2.discussion_participants SET discussion_id = gen_random_uuid() WHERE discussion_id = $1 AND user_id = $2`},
		{"user_id", `UPDATE tier2.discussion_participants SET user_id = gen_random_uuid() WHERE discussion_id = $1 AND user_id = $2`},
	} {
		t.Run("refuses/"+tc.name, func(t *testing.T) {
			_, err := s.pool.Exec(ctx, tc.query, d.ID, person)
			if got := sqlstate(err); got != pgParticipantGuard {
				t.Errorf("%s SQLSTATE = %q (err %v), want %s", tc.name, got, err, pgParticipantGuard)
			}
		})
	}
	// added_by separately: it needs a real user to distinguish the allow list
	// from the actor test.
	other := discPerson(t, s, ctx, "allowlist-2@example.com")
	_, err := s.pool.Exec(ctx,
		`UPDATE tier2.discussion_participants SET added_by = $3 WHERE discussion_id = $1 AND user_id = $2`,
		d.ID, person, other)
	if got := sqlstate(err); got != pgParticipantGuard {
		t.Errorf("added_by SQLSTATE = %q (err %v), want %s", got, err, pgParticipantGuard)
	}
}

// Criterion 18 — a remove/re-add cycle does not reset the marker. Re-reading a
// thread from the top because somebody took you off it for an afternoon is the
// kind of small wrongness that costs trust in the count.
func TestAMarkerSurvivesARemoveAndReAdd(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "readd-o@example.com")
	guest := discPerson(t, s, ctx, "readd-g@example.com")
	d := openThread(t, s, ctx, owner, "Re-add")
	if err := s.AddParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, d.ID, owner, "two")
	appendTurn(t, s, ctx, d.ID, owner, "three")
	if err := s.MarkRead(ctx, d.ID, guest, 2); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	ps := mustParticipants(t, s, ctx, d.ID)
	firstAdd := ps[0].AddedAt
	for _, p := range ps {
		if p.UserID == guest {
			firstAdd = p.AddedAt
		}
	}

	if err := s.RemoveParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}
	if err := s.AddParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("re-add: %v", err)
	}

	if seq := markerOf(t, s, ctx, d.ID, guest); seq == nil || *seq != 2 {
		t.Errorf("marker = %v after a remove/re-add, want 2", seq)
	}
	if got := unread(t, s, ctx, d.ID, guest); got != 1 {
		t.Errorf("unread = %d after a remove/re-add, want 1 — the count reset", got)
	}
	for _, p := range mustParticipants(t, s, ctx, d.ID) {
		if p.UserID == guest && !p.AddedAt.Equal(firstAdd) {
			t.Errorf("added_at moved on a re-add: %s -> %s", firstAdd, p.AddedAt)
		}
	}
}

// ============================================================================
// Criterion 20 — ruling 8, reading is not joining.
// ============================================================================

func TestMarkReadByANonParticipantIsRefused(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "nonmember-o@example.com")
	stranger := discPerson(t, s, ctx, "nonmember-s@example.com")
	d := openThread(t, s, ctx, owner, "Not yours")

	before := len(mustParticipants(t, s, ctx, d.ID))

	if err := s.MarkRead(ctx, d.ID, stranger, 1); !errors.Is(err, ErrNotAParticipant) {
		t.Errorf("MarkRead by a non-participant err = %v, want ErrNotAParticipant", err)
	}
	// THE HALF THAT DISTINGUISHES THE TWO READINGS: no row was created, so
	// Participants stays a list somebody chose rather than a list of who
	// happened to look.
	if after := len(mustParticipants(t, s, ctx, d.ID)); after != before {
		t.Errorf("participants went from %d to %d; reading joined the thread", before, after)
	}

	// And a missing thread still reads as missing rather than as "not yours".
	if err := s.MarkRead(ctx, uuid.New(), owner, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("MarkRead on a thread that does not exist err = %v, want ErrNotFound", err)
	}
}

// ============================================================================
// Criterion 21 — a resolved thread's count stops moving.
// ============================================================================

func TestAResolvedThreadsUnreadCountIsStable(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "resolved-count-o@example.com")
	guest := discPerson(t, s, ctx, "resolved-count-g@example.com")
	d := openThread(t, s, ctx, owner, "Concluded")
	if err := s.AddParticipant(ctx, d.ID, guest, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, d.ID, owner, "two")

	if err := s.ResolveDiscussion(ctx, d.ID, owner, nil); err != nil {
		t.Fatalf("ResolveDiscussion: %v", err)
	}
	before := unread(t, s, ctx, d.ID, guest)

	if _, err := s.AppendTurn(ctx, NewTurn{
		DiscussionID: d.ID, AuthorID: owner, Body: "one more",
	}); !errors.Is(err, ErrDiscussionResolved) {
		t.Fatalf("expected CH093, got %v", err)
	}
	if after := unread(t, s, ctx, d.ID, guest); after != before {
		t.Errorf("the count moved on a resolved thread: %d -> %d", before, after)
	}

	// A RESOLVED THREAD CAN STILL BE UNREAD, deliberately — usually including
	// the turn that concluded it.
	if before != 2 {
		t.Errorf("unread on a resolved thread = %d, want 2", before)
	}
}

// ============================================================================
// Criterion 22 — ruling 6, the aggregate. All three filters at once.
// ============================================================================

func TestUnreadByDiscussionIsTheBadge(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "badge-o@example.com")
	me := discPerson(t, s, ctx, "badge-me@example.com")

	// (a) something unread — should appear.
	live := openThread(t, s, ctx, owner, "Live")
	if err := s.AddParticipant(ctx, live.ID, me, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, live.ID, owner, "two")

	// (b) nothing unread — should be omitted rather than reported as 0.
	caughtUp := openThread(t, s, ctx, owner, "Caught up")
	if err := s.AddParticipant(ctx, caughtUp.ID, me, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if err := s.MarkRead(ctx, caughtUp.ID, me, 1); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	// (c) removed — should be omitted, because removal means "not expected to
	// read this" and counting it would make removal do nothing visible.
	gone := openThread(t, s, ctx, owner, "Removed from")
	if err := s.AddParticipant(ctx, gone.ID, me, owner); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	appendTurn(t, s, ctx, gone.ID, owner, "two")
	if err := s.RemoveParticipant(ctx, gone.ID, me, owner); err != nil {
		t.Fatalf("RemoveParticipant: %v", err)
	}

	// (d) a thread I am not on at all.
	notMine := openThread(t, s, ctx, owner, "Not mine")
	appendTurn(t, s, ctx, notMine.ID, owner, "two")

	agg, err := s.UnreadByDiscussion(ctx, me)
	if err != nil {
		t.Fatalf("UnreadByDiscussion: %v", err)
	}
	if len(agg) != 1 {
		t.Fatalf("badge = %v, want exactly the one live thread", agg)
	}
	if agg[live.ID] != 2 {
		t.Errorf("badge[live] = %d, want 2", agg[live.ID])
	}
	for _, absent := range []struct {
		name string
		id   uuid.UUID
	}{{"caught up", caughtUp.ID}, {"removed from", gone.ID}, {"not mine", notMine.ID}} {
		if _, ok := agg[absent.id]; ok {
			t.Errorf("the %q thread is in the badge and should not be", absent.name)
		}
	}
}
