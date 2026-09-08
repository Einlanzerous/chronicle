package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// CHRN-95 — the landing. One transaction, a back-pointer, and a person on the
// row that records the decision.

// landable seeds a memo in `transcribed` with an author, and a page to land on.
func landable(t *testing.T, s *Store, ctx context.Context, hash string) (memo uuid.UUID, author uuid.UUID, page Page) {
	t.Helper()
	sum := sha256.Sum256([]byte(hash))
	memoID := seedTriageable(t, s, ctx, hex.EncodeToString(sum[:]))
	m, err := s.GetMemo(ctx, memoID)
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}
	if m.State != StateTranscribed {
		t.Fatalf("memo state %q, want transcribed — the fixture is not landable", m.State)
	}
	page, err = s.PageByPath(ctx, "estate")
	if errors.Is(err, ErrNotFound) {
		page = mkPage(t, s, ctx, nil, "estate")
	} else if err != nil {
		t.Fatalf("PageByPath: %v", err)
	}
	return memoID, m.AuthorID, page
}

func noteDecision(memo uuid.UUID, dest string) Decision {
	return Decision{MemoID: memo, Destination: dest, IdempotencyKey: uuid.NewString()}
}

func linkRow(t *testing.T, s *Store, ctx context.Context, memo uuid.UUID) MemoLink {
	t.Helper()
	l, err := s.MemoLinkFor(ctx, memo)
	if err != nil {
		t.Fatalf("MemoLinkFor: %v", err)
	}
	return l
}

// Criterion: a create lands with its verb, its memo, its confirmer AND its
// author — the last is the half CH041 does not check, so it is the half a test
// has to.
func TestACreateLandsWithItsVerbMemoAuthorAndConfirmer(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "create-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")

	link, note, rev, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate", Title: "A landed note", Body: "the text",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote: %v", err)
	}

	if note.PageID != page.ID {
		t.Errorf("page %v, want %v", note.PageID, page.ID)
	}
	if rev.Seq != 1 {
		t.Errorf("seq %d, want 1", rev.Seq)
	}
	if rev.Verb == nil || *rev.Verb != VerbCreate {
		t.Errorf("verb %v, want create", rev.Verb)
	}
	if rev.MemoID == nil || *rev.MemoID != memo {
		t.Errorf("memo_id %v, want %v", rev.MemoID, memo)
	}
	if rev.ConfirmedBy == nil || *rev.ConfirmedBy != confirmer {
		t.Errorf("confirmed_by %v, want the deciding actor %v", rev.ConfirmedBy, confirmer)
	}
	// THE AUTHOR IS THE MEMO'S, NOT THE CONFIRMER'S. Scribe drafting the words
	// does not make Scribe the author, and neither does agreeing to them.
	if rev.AuthorID != author {
		t.Errorf("author_id %v, want the memo's author %v", rev.AuthorID, author)
	}

	if link.NoteID == nil || *link.NoteID != note.ID {
		t.Errorf("note_id %v, want %v", link.NoteID, note.ID)
	}
	if link.ConfirmedBy == nil || *link.ConfirmedBy != confirmer {
		t.Errorf("link confirmed_by %v, want %v", link.ConfirmedBy, confirmer)
	}
	if link.DiscussionID != nil {
		t.Error("a NOTE landing set discussion_id")
	}

	m, err := s.GetMemo(ctx, memo)
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}
	if m.State != StateTriaged {
		t.Errorf("memo state %q, want triaged", m.State)
	}
}

// Criterion: an append gains seq n+1, composes old + blank line + new, and
// CARRIES THE TITLE FORWARD UNCHANGED (CHRN-39's verbs table).
func TestAnAppendCarriesTheTitleForward(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "append-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")
	target := mkNote(t, s, ctx, page.ID, author, "The original title", "first pass")

	_, note, rev, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbAppend,
		TargetNumber: &target.Number,
		Title:        "A TITLE THE MODEL INVENTED", Body: "second pass",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote(append): %v", err)
	}

	if note.ID != target.ID {
		t.Fatalf("appended to note %v, want %v", note.ID, target.ID)
	}
	if rev.Seq != 2 {
		t.Errorf("seq %d, want 2", rev.Seq)
	}
	if rev.Title != "The original title" {
		t.Errorf("title %q — an append must carry the note's own title forward", rev.Title)
	}
	if rev.Body != "first pass\n\nsecond pass" {
		t.Errorf("body %q, want old + blank line + new", rev.Body)
	}
	if rev.Verb == nil || *rev.Verb != VerbAppend {
		t.Errorf("verb %v, want append", rev.Verb)
	}
}

// Criterion: a supersede replaces the body AND takes the proposal's title, and
// the previous revision is still readable at seq n-1 — so the landing is
// restorable through CHRN-39's ruling 1 with no machinery of its own.
func TestASupersedeReplacesAndLeavesTheOldOneReadable(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "supersede-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")
	target := mkNote(t, s, ctx, page.ID, author, "Old title", "the old body")

	_, _, rev, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbSupersede,
		TargetNumber: &target.Number, Title: "New title", Body: "the new body",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote(supersede): %v", err)
	}
	if rev.Title != "New title" || rev.Body != "the new body" {
		t.Errorf("supersede left %q / %q", rev.Title, rev.Body)
	}

	revs, err := s.NoteRevisions(ctx, target.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("%d revisions, want 2", len(revs))
	}
	if revs[0].Body != "the old body" || revs[0].Title != "Old title" {
		t.Errorf("seq 1 is no longer the old note: %q / %q", revs[0].Title, revs[0].Body)
	}
}

// THE APPEND READS THE CURRENT REVISION INSIDE THE TRANSACTION.
//
// CurrentRevision runs on the pool. Composed from a read taken before the note
// was locked, an append would silently drop a revision committed in between —
// which is the 3 a.m. failure CHRN-39's revision log exists to make
// recoverable, arriving one layer earlier. Here a revision lands between the
// fixture's read and the landing, and the appended body must include it.
func TestAnAppendComposesFromTheRevisionThatIsCurrentAtWriteTime(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "race-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")
	target := mkNote(t, s, ctx, page.ID, author, "Racing", "body one")

	// Somebody edits the note after the batch was fetched and before it is
	// applied.
	if _, err := s.AppendRevision(ctx, target.ID, NewRevision{
		AuthorID: author, ConfirmedBy: author, Title: "Racing", Body: "body one\n\nbody two",
	}); err != nil {
		t.Fatalf("concurrent AppendRevision: %v", err)
	}

	_, _, rev, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbAppend,
		TargetNumber: &target.Number, Title: "Racing", Body: "body three",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote(append): %v", err)
	}
	if !strings.Contains(rev.Body, "body two") {
		t.Errorf("body %q dropped the revision that landed in between", rev.Body)
	}
	if rev.Body != "body one\n\nbody two\n\nbody three" {
		t.Errorf("body %q, want the current body plus the new text", rev.Body)
	}
}

// Criterion: a create whose page_path names a leaf that does not exist creates
// the missing pages INSIDE the landing transaction.
func TestACreateBuildsTheMissingPagesOnItsPath(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "pages-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")

	_, note, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate/storage/amber", Title: "Deep", Body: "text",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote: %v", err)
	}
	leaf, err := s.PageByPath(ctx, "estate/storage/amber")
	if err != nil {
		t.Fatalf("PageByPath: %v", err)
	}
	if note.PageID != leaf.ID {
		t.Errorf("note landed on %v, want the created leaf %v", note.PageID, leaf.ID)
	}
	if _, err := s.PageByPath(ctx, "estate/storage"); err != nil {
		t.Errorf("the intermediate page was not created: %v", err)
	}
}

// A LANDING THAT FAILS LEAVES NO PAGES BEHIND, because they are created on the
// landing's own transaction rather than on the pool.
func TestAFailedLandingBuildsNoPages(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "rollback-pages")

	// An agent confirmer fails at CH041 on the revision, after the pages on the
	// path would have been created.
	scribeID := agent(t, s, ctx, "scribe@example.com")
	_, _, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate/invented/branch", Title: "Doomed", Body: "text",
		AuthorID: author, ConfirmedBy: scribeID,
	})
	if err == nil {
		t.Fatal("an agent confirmed a note revision")
	}
	if _, err := s.PageByPath(ctx, "estate/invented"); !errors.Is(err, ErrNotFound) {
		t.Errorf("PageByPath after a failed landing = %v, want ErrNotFound", err)
	}
}

// relate with no page_path lands on the TARGET'S page; an explicit page_path
// overrides that, because a model that named a page answered the question
// directly.
func TestRelateLandsNearItsTargetUnlessToldOtherwise(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "relate-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")
	elsewhere := mkPage(t, s, ctx, nil, "elsewhere")
	target := mkNote(t, s, ctx, page.ID, author, "The target", "text")

	_, near, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbRelate,
		TargetNumber: &target.Number, Title: "Nearby", Body: "see CHR-0001",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote(relate): %v", err)
	}
	if near.PageID != page.ID {
		t.Errorf("relate landed on %v, want the target's page %v", near.PageID, page.ID)
	}

	memo2, author2, _ := landable(t, s, ctx, "relate-hash-2")
	_, far, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo2, LinkNote), Verb: VerbRelate,
		TargetNumber: &target.Number, PagePath: "elsewhere",
		Title: "Deliberately elsewhere", Body: "text",
		AuthorID: author2, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote(relate, explicit page): %v", err)
	}
	if far.PageID != elsewhere.ID {
		t.Errorf("an explicit page_path did not win: %v", far.PageID)
	}
}

// APPEND AND SUPERSEDE IGNORE page_path, AND IT IS NOT A MOVE. A landing must
// never relocate authored text as a side effect of adding a paragraph to it.
func TestAnAppendDoesNotMoveTheNote(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "nomove-hash")
	confirmer := person(t, s, ctx, "confirmer@example.com")
	mkPage(t, s, ctx, nil, "somewhere-else")
	target := mkNote(t, s, ctx, page.ID, author, "Stays put", "body")

	_, note, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbAppend,
		TargetNumber: &target.Number, PagePath: "somewhere-else",
		Title: "Stays put", Body: "more",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandNote(append): %v", err)
	}
	if note.PageID != page.ID {
		t.Errorf("the note moved to %v; append is not a move", note.PageID)
	}
}

// Criterion: the landing is atomic, asserted at BOTH seams — and in the
// stronger form ruling 3 buys, which is that no memo_links row survives at all
// rather than an unconfirmed one.
func TestALandingLeavesNothingBehindWhenItFails(t *testing.T) {
	s, ctx := newTestStore(t)

	t.Run("the write fails", func(t *testing.T) {
		memo, author, page := landable(t, s, ctx, "atomic-write")
		confirmer := person(t, s, ctx, "w@example.com")
		gone := mkNote(t, s, ctx, page.ID, author, "Deleted", "body")
		if err := s.SoftDeleteNote(ctx, gone.ID, author); err != nil {
			t.Fatalf("SoftDeleteNote: %v", err)
		}

		_, _, _, err := s.LandNote(ctx, NoteLanding{
			Decision: noteDecision(memo, LinkNote), Verb: VerbAppend,
			TargetNumber: &gone.Number, Title: "x", Body: "y",
			AuthorID: author, ConfirmedBy: confirmer,
		})
		if !errors.Is(err, ErrNoteDeleted) {
			t.Fatalf("LandNote onto a deleted note = %v, want ErrNoteDeleted", err)
		}
		if _, err := s.MemoLinkFor(ctx, memo); !errors.Is(err, ErrNotFound) {
			t.Errorf("a memo_links row survived a failed landing: %v", err)
		}
	})

	t.Run("the confirm fails", func(t *testing.T) {
		memo, author, _ := landable(t, s, ctx, "atomic-confirm")
		// A discussion turn has no confirmed_by, so an agent gets past the
		// write and is refused by CH023 on the link row — which is the confirm.
		scribeID := agent(t, s, ctx, "scribe2@example.com")

		_, _, _, err := s.LandDiscussion(ctx, DiscussionLanding{
			Decision: noteDecision(memo, LinkDiscussion),
			Title:    "Doomed thread", Body: "opening post",
			AuthorID: author, ConfirmedBy: scribeID,
		})
		if err == nil {
			t.Fatal("an agent confirmed a landing")
		}
		if !strings.Contains(err.Error(), "CH023") && !strings.Contains(err.Error(), "person") {
			t.Errorf("error %v does not name the rule", err)
		}
		if _, err := s.MemoLinkFor(ctx, memo); !errors.Is(err, ErrNotFound) {
			t.Errorf("a memo_links row survived a failed confirm: %v", err)
		}
		var threads int
		if err := s.pool.QueryRow(ctx,
			`SELECT count(*) FROM tier2.discussions WHERE title = 'Doomed thread'`).Scan(&threads); err != nil {
			t.Fatalf("count: %v", err)
		}
		if threads != 0 {
			t.Errorf("%d threads survived a failed confirm", threads)
		}
	})
}

// THE STRONGER CLAIM RULING 3 BUYS: the landing path cannot produce a PENDING
// local row at all. Asserted by forcing a failure at each seam and then looking
// for one, rather than by asserting that a sweep would tidy it up.
func TestTheLandingPathNeverLeavesAPendingLocalRow(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "pending-hash")
	gone := mkNote(t, s, ctx, page.ID, author, "Deleted", "body")
	if err := s.SoftDeleteNote(ctx, gone.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}
	confirmer := person(t, s, ctx, "p@example.com")
	_, _, _, _ = s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbAppend,
		TargetNumber: &gone.Number, Title: "x", Body: "y",
		AuthorID: author, ConfirmedBy: confirmer,
	})

	var pending int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM tier2.memo_links
		 WHERE destination IN ('NOTE', 'DISCUSSION')
		   AND confirmed_at IS NULL AND refused_at IS NULL`).Scan(&pending); err != nil {
		t.Fatalf("count: %v", err)
	}
	if pending != 0 {
		t.Errorf("%d pending local rows exist; the landing is not one transaction", pending)
	}
}

// Criterion: a replayed batch is idempotent — one note, one row, and the second
// call reports the first call's note rather than writing a second.
func TestAReplayedLandingIsIdempotent(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "replay-hash")
	confirmer := person(t, s, ctx, "r@example.com")

	in := NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate", Title: "Once", Body: "text",
		AuthorID: author, ConfirmedBy: confirmer,
	}
	_, first, _, err := s.LandNote(ctx, in)
	if err != nil {
		t.Fatalf("first LandNote: %v", err)
	}

	// A fresh key, as a genuine replay would carry.
	in.Decision.IdempotencyKey = uuid.NewString()
	again, _, _, err := s.LandNote(ctx, in)
	if !errors.Is(err, ErrAlreadyLanded) {
		t.Fatalf("second LandNote = %v, want ErrAlreadyLanded", err)
	}
	if again.NoteID == nil || *again.NoteID != first.ID {
		t.Errorf("the replay reported %v, want the first landing's note %v", again.NoteID, first.ID)
	}

	var notes int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tier2.notes WHERE page_id IS NOT NULL`).Scan(&notes); err != nil {
		t.Fatalf("count: %v", err)
	}
	if notes != 1 {
		t.Errorf("%d notes exist after a replay, want 1", notes)
	}
}

// A memo held between the batch's GET and its POST gains no note. memos_guard
// has no held → triaged edge, so without the check under the lock this would
// write a note and then fail the advance.
func TestALandingRefusesAMemoThatIsNoLongerAwaitingTriage(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "held-hash")
	confirmer := person(t, s, ctx, "h@example.com")
	if _, err := s.AdvanceMemoState(ctx, memo, StateTranscribed, StateHeld, "thinking about it"); err != nil {
		t.Fatalf("AdvanceMemoState: %v", err)
	}

	_, _, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate", Title: "Nope", Body: "text",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if !errors.Is(err, ErrMemoNotTranscribed) {
		t.Fatalf("LandNote on a held memo = %v, want ErrMemoNotTranscribed", err)
	}
	var notes int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tier2.notes`).Scan(&notes); err != nil {
		t.Fatalf("count: %v", err)
	}
	if notes != 0 {
		t.Errorf("%d notes were written for a held memo", notes)
	}
}

// THE sent_* COLUMNS ARE WHAT CHRONICLE PUT ON THE WIRE, and a NOTE puts
// nothing on one. A note body in sent_description would be a copy of tier 2
// into a column whose own comment says it is not one.
func TestALocalLandingSendsNothingOnAWire(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "sent-hash")
	confirmer := person(t, s, ctx, "s@example.com")

	if _, _, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate", Title: "Titled", Body: "a body that goes nowhere",
		AuthorID: author, ConfirmedBy: confirmer,
	}); err != nil {
		t.Fatalf("LandNote: %v", err)
	}

	l := linkRow(t, s, ctx, memo)
	if l.SentTitle != "" || l.SentDescription != "" || l.SentProjectKey != "" || l.SentType != "" {
		t.Errorf("a local landing filled the wire columns: %+v", l)
	}
}

// ============================================================================
// 0017's constraints and guard.
// ============================================================================

// The back-pointer is legal only on its own destination.
func TestABackPointerBelongsToItsOwnDestination(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "check-hash")
	note := mkNote(t, s, ctx, page.ID, author, "A note", "body")

	// A TICKET row, claimed but not confirmed, then pointed at a note.
	if _, _, err := s.ClaimMemoLink(ctx, Decision{
		MemoID: memo, Destination: LinkTicket, ProjectKey: "SY", Type: "task",
		Title: "t", Description: "d", IdempotencyKey: uuid.NewString(),
	}); err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE tier2.memo_links SET note_id = $2 WHERE memo_id = $1`, memo, note.ID)
	if err == nil {
		t.Fatal("a TICKET row accepted a note_id")
	}
	if !strings.Contains(err.Error(), "memo_links_note_id_only_on_note") {
		t.Errorf("refused by %v, want memo_links_note_id_only_on_note", err)
	}
}

// A confirmed local row without the thing it produced, or without an actor, is
// a link to nothing — which looks like success and is worse than a failure.
// Written by direct SQL because the store never produces either state.
func TestAConfirmedLocalRowMustSayWhatItBecameAndWhoAgreed(t *testing.T) {
	s, ctx := newTestStore(t)
	confirmer := person(t, s, ctx, "c@example.com")

	// A confirmed NOTE with an actor but nothing to point at.
	memo, _, _ := landable(t, s, ctx, "confirmed-hash")
	if _, _, err := s.ClaimMemoLink(ctx, noteDecision(memo, LinkNote)); err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE tier2.memo_links SET confirmed_at = now(), confirmed_by = $2 WHERE memo_id = $1`,
		memo, confirmer)
	if err == nil || !strings.Contains(err.Error(), "memo_links_confirmed_note_has_a_note") {
		t.Errorf("confirming a NOTE with no note = %v, want memo_links_confirmed_note_has_a_note", err)
	}

	// A confirmed NOTE that says what it became and not who agreed. Ruling 9's
	// missing premise: without confirmed_by the DISCUSSION arm recorded the
	// decider nowhere at all.
	memo2, author2, page := landable(t, s, ctx, "actorless-hash")
	note := mkNote(t, s, ctx, page.ID, author2, "A note", "body")
	if _, _, err := s.ClaimMemoLink(ctx, noteDecision(memo2, LinkNote)); err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}
	_, err = s.pool.Exec(ctx,
		`UPDATE tier2.memo_links SET confirmed_at = now(), note_id = $2 WHERE memo_id = $1`,
		memo2, note.ID)
	if err == nil || !strings.Contains(err.Error(), "memo_links_confirmed_local_has_an_actor") {
		t.Errorf("confirming with no actor = %v, want memo_links_confirmed_local_has_an_actor", err)
	}
}

// CH021 — a confirmed row's back-pointer is frozen, on exactly the argument
// that froze ticket_key: the note an operator was told about could stop being
// the note their memo became, and nothing would have logged the change.
func TestAConfirmedBackPointerCannotBeRePointed(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, page := landable(t, s, ctx, "frozen-hash")
	confirmer := person(t, s, ctx, "f@example.com")
	other := mkNote(t, s, ctx, page.ID, author, "Somewhere else", "body")

	if _, _, _, err := s.LandNote(ctx, NoteLanding{
		Decision: noteDecision(memo, LinkNote), Verb: VerbCreate,
		PagePath: "estate", Title: "Landed", Body: "text",
		AuthorID: author, ConfirmedBy: confirmer,
	}); err != nil {
		t.Fatalf("LandNote: %v", err)
	}

	_, err := s.pool.Exec(ctx,
		`UPDATE tier2.memo_links SET note_id = $2 WHERE memo_id = $1`, memo, other.ID)
	if err == nil {
		t.Fatal("a confirmed note_id was re-pointed")
	}
	if !strings.Contains(err.Error(), "CH021") && !strings.Contains(err.Error(), "immutable") {
		t.Errorf("refused by %v, want CH021", err)
	}

	// THE FREEZE IS THE CONFIRMATION, NOT THE COLUMN. A pending row's
	// back-pointer moves freely; it is the confirmed answer that is terminal.
	memo2, _, _ := landable(t, s, ctx, "pending-move")
	if _, _, err := s.ClaimMemoLink(ctx, noteDecision(memo2, LinkNote)); err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memo_links SET note_id = $2 WHERE memo_id = $1`, memo2, other.ID); err != nil {
		t.Errorf("a PENDING row refused a note_id: %v", err)
	}
}

// 0017 amends memo_links_guard, so what it refused before must still be
// refused. A migration that loosened a guard while adding to it is the failure
// this asserts against.
func TestTheGuardStillRefusesWhatItRefusedBefore(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, _, _ := landable(t, s, ctx, "guard-hash")
	other, _, _ := landable(t, s, ctx, "guard-hash-2")
	l, _, err := s.ClaimMemoLink(ctx, noteDecision(memo, LinkNote))
	if err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}

	// CH020 — a decision may not be re-attributed to another memo.
	_, err = s.pool.Exec(ctx, `UPDATE tier2.memo_links SET memo_id = $2 WHERE id = $1`, l.ID, other)
	if err == nil || !strings.Contains(err.Error(), "CH020") {
		t.Errorf("re-attributing a link = %v, want CH020", err)
	}

	// CH022 — re-arming a refused row needs a fresh idempotency key.
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memo_links SET refused_at = now(), refused_reason = 'because' WHERE id = $1`,
		l.ID); err != nil {
		t.Fatalf("refuse: %v", err)
	}
	_, err = s.pool.Exec(ctx, `UPDATE tier2.memo_links SET refused_at = NULL WHERE id = $1`, l.ID)
	if err == nil || !strings.Contains(err.Error(), "CH022") {
		t.Errorf("re-arming under the same key = %v, want CH022", err)
	}
}

// CH023 — a link is confirmed by a person. It is CH041's argument applied to
// the one arm that has no CH041: a discussion turn carries no confirmed_by, so
// without this the DISCUSSION landing would be unchecked where the NOTE landing
// is checked, and the difference would be invisible.
func TestAnAgentCannotConfirmALanding(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "agent-hash")
	scribeID := agent(t, s, ctx, "scribe3@example.com")

	_, _, _, err := s.LandDiscussion(ctx, DiscussionLanding{
		Decision: noteDecision(memo, LinkDiscussion),
		Title:    "A thread", Body: "opening",
		AuthorID: author, ConfirmedBy: scribeID,
	})
	if err == nil {
		t.Fatal("an agent confirmed a discussion landing")
	}
	if !strings.Contains(err.Error(), "CH023") && !strings.Contains(err.Error(), "person") {
		t.Errorf("error %v does not name the rule", err)
	}
}

// A DISCUSSION lands as a NEW thread whose turn 1 is the MEMO'S AUTHOR — not
// the deciding actor — so CH091 accepts it.
func TestADiscussionLandsAsANewThreadAuthoredByTheMemosAuthor(t *testing.T) {
	s, ctx := newTestStore(t)
	memo, author, _ := landable(t, s, ctx, "thread-hash")
	confirmer := person(t, s, ctx, "d@example.com")

	link, disc, turn, err := s.LandDiscussion(ctx, DiscussionLanding{
		Decision: noteDecision(memo, LinkDiscussion),
		Title:    "Worth discussing", Body: "the opening post",
		AuthorID: author, ConfirmedBy: confirmer,
	})
	if err != nil {
		t.Fatalf("LandDiscussion: %v", err)
	}
	if turn.Seq != 1 {
		t.Errorf("seq %d, want 1", turn.Seq)
	}
	if turn.AuthorID != author {
		t.Errorf("turn 1 authored by %v, want the memo's author %v", turn.AuthorID, author)
	}
	if turn.AuthorID == confirmer {
		t.Error("the deciding actor authored the opening post")
	}
	if turn.Body != "the opening post" {
		t.Errorf("body %q", turn.Body)
	}
	if link.DiscussionID == nil || *link.DiscussionID != disc.ID {
		t.Errorf("discussion_id %v, want %v", link.DiscussionID, disc.ID)
	}
	if link.NoteID != nil {
		t.Error("a DISCUSSION landing set note_id")
	}
	if link.ConfirmedBy == nil || *link.ConfirmedBy != confirmer {
		t.Errorf("link confirmed_by %v, want %v", link.ConfirmedBy, confirmer)
	}
}

// chronicle_tier1 cannot read or write the columns 0017 adds. Asserted
// POSITIVELY rather than inferred from the absence of a GRANT — 0008's closing
// comment is explicit that this is the table that most needs that to hold.
func TestTier1CannotReachTheLandingColumns(t *testing.T) {
	s, ctx := newTestStore(t)
	_ = s

	pool := tier1Pool(t, ctx)
	for _, col := range []string{"note_id", "discussion_id", "confirmed_by"} {
		for _, priv := range []string{"SELECT", "UPDATE", "INSERT"} {
			var held bool
			if err := pool.QueryRow(ctx,
				`SELECT has_column_privilege(current_user, 'tier2.memo_links', $1, $2)`,
				col, priv).Scan(&held); err != nil {
				// A role with no privilege on the table at all cannot even ask,
				// which is a stronger answer than false.
				if strings.Contains(err.Error(), "permission denied") {
					continue
				}
				t.Fatalf("has_column_privilege(%s, %s): %v", col, priv, err)
			}
			if held {
				t.Errorf("chronicle_tier1 holds %s on tier2.memo_links.%s", priv, col)
			}
		}
	}
}
