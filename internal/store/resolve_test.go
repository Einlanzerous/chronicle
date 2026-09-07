package store

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// CHRN-46's three `Done when` clauses. CHRN-43 shipped the mechanical half —
// the columns, CH080, and ResolveDiscussion — so what is asserted here is the
// ACT: the note a thread becomes, the link read backwards, and a resolved
// thread that is still there.

// ============================================================================
// Done when 1 — resolving records what the thread concluded.
// ============================================================================

func TestResolvingIntoANewNoteWritesBothOrNeither(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "forty-six@example.com")
	page := mkPage(t, s, ctx, nil, "estate")
	d := openThread(t, s, ctx, person, "How long do we keep audio")
	appendTurn(t, s, ctx, d.ID, person, "thirty days, gated on a durable transcript")

	note, rev, err := s.ResolveIntoNewNote(ctx, d.ID, person, Resolution{
		PageID: page.ID,
		Title:  "Audio retention",
		Body:   "Thirty days, and deletion is gated on a durable transcript.",
	})
	if err != nil {
		t.Fatalf("ResolveIntoNewNote: %v", err)
	}

	got, err := s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if !got.Resolved() {
		t.Fatal("the thread is not resolved")
	}
	if got.ResolvedNoteID == nil || *got.ResolvedNoteID != note.ID {
		t.Errorf("resolved_note_id = %v, want %s", got.ResolvedNoteID, note.ID)
	}
	if got.ResolvedBy == nil || *got.ResolvedBy != person {
		t.Errorf("resolved_by = %v, want %s", got.ResolvedBy, person)
	}

	// RULING 4 — the revision carries verb NULL, with resolved_note_id as its
	// provenance. Resolution has no Verb field, so this is unrepresentable
	// otherwise; asserted anyway, because the field could be added back.
	if rev.Verb != nil {
		t.Errorf("the resolution's revision carries verb %q, want NULL (ruling 4)", *rev.Verb)
	}
	if rev.ConfirmedBy == nil || *rev.ConfirmedBy != person {
		t.Errorf("confirmed_by = %v, want the resolver %s", rev.ConfirmedBy, person)
	}
}

// BOTH OR NEITHER. A note created without the link is a conclusion nothing
// points at; a link to a note that failed is a thread claiming a product it
// does not have. One transaction makes both unreachable.
func TestAFailedResolutionLeavesNoNoteBehind(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "atomic@example.com")
	page := mkPage(t, s, ctx, nil, "estate")

	before := countNotes(t, s, ctx)

	// The note insert succeeds and the resolution fails, which is the ordering
	// that would leave an orphan if these were two calls.
	_, _, err := s.ResolveIntoNewNote(ctx, uuid.New(), person, Resolution{
		PageID: page.ID, Title: "Orphan", Body: "nothing points at this",
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("resolving a thread that does not exist err = %v, want ErrNotFound", err)
	}
	if after := countNotes(t, s, ctx); after != before {
		t.Errorf("notes went from %d to %d; the note and the resolution are not one transaction", before, after)
	}

	// And the other ordering: a resolution refused by CH080 must take the
	// revision it just wrote down with it.
	d := openThread(t, s, ctx, person, "Already concluded")
	first, _, err := s.ResolveIntoNewNote(ctx, d.ID, person, Resolution{
		PageID: page.ID, Title: "First conclusion", Body: "x",
	})
	if err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	beforeSecond := countNotes(t, s, ctx)
	if _, _, err := s.ResolveIntoNewNote(ctx, d.ID, person, Resolution{
		PageID: page.ID, Title: "Second conclusion", Body: "y",
	}); !errors.Is(err, ErrResolutionFixed) {
		t.Fatalf("re-resolving into a different note err = %v, want ErrResolutionFixed", err)
	}
	if after := countNotes(t, s, ctx); after != beforeSecond {
		t.Errorf("a refused re-resolution left a note behind: %d -> %d", beforeSecond, after)
	}
	// The original conclusion is untouched.
	got, err := s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if got.ResolvedNoteID == nil || *got.ResolvedNoteID != first.ID {
		t.Errorf("resolved_note_id = %v, want the first note %s", got.ResolvedNoteID, first.ID)
	}
}

// "Resolved into PRINCIPLES §6" — the ticket's own example, and the ordinary
// case rather than the exotic one. It APPENDS, so the note's earlier text
// survives as a revision.
func TestResolvingIntoAnExistingNoteAppends(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "principles@example.com")
	page := mkPage(t, s, ctx, nil, "estate")
	principles := mkNote(t, s, ctx, page.ID, person, "PRINCIPLES", "§1..§5")
	before := countNotes(t, s, ctx)

	d := openThread(t, s, ctx, person, "Retention")
	rev, err := s.ResolveIntoExistingNote(ctx, d.ID, principles.ID, person,
		"PRINCIPLES", "§1..§5\n\n§6 Audio is pruned at thirty days.")
	if err != nil {
		t.Fatalf("ResolveIntoExistingNote: %v", err)
	}

	if after := countNotes(t, s, ctx); after != before {
		t.Errorf("notes went from %d to %d; resolving into an existing note created one", before, after)
	}
	if rev.Seq != 2 {
		t.Errorf("revision seq = %d, want 2 — it replaced rather than appended", rev.Seq)
	}
	if rev.Verb != nil {
		t.Errorf("verb = %q, want NULL at seq 2 as well (ruling 4)", *rev.Verb)
	}

	// The earlier text survives, which is CHRN-39's rule inherited rather than
	// restated.
	revs, err := s.NoteRevisions(ctx, principles.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}
	if len(revs) != 2 || revs[0].Body != "§1..§5" {
		t.Errorf("the note's history = %d revisions, first body %q", len(revs), revs[0].Body)
	}

	// TWO THREADS INTO ONE NOTE. resolved_note_id carries no UNIQUE precisely
	// so a long-lived page can collect several conclusions.
	second := openThread(t, s, ctx, person, "Pinning")
	if _, err := s.ResolveIntoExistingNote(ctx, second.ID, principles.ID, person,
		"PRINCIPLES", "§1..§6\n\n§7 A pinned memo is never pruned."); err != nil {
		t.Fatalf("a second thread into the same note: %v", err)
	}
}

// ============================================================================
// Done when 2 — the note shows its provenance. LINKED BOTH WAYS.
// ============================================================================

// The reverse link is a QUERY rather than a column on tier2.notes: a
// notes.discussion_id would be a second copy of what tier2.discussions already
// holds, and it could not represent the truth anyway, because a note may be
// what several threads concluded.
func TestANoteNamesTheThreadsThatProducedIt(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "provenance@example.com")
	page := mkPage(t, s, ctx, nil, "estate")

	d := openThread(t, s, ctx, person, "How long do we keep audio")
	note, _, err := s.ResolveIntoNewNote(ctx, d.ID, person, Resolution{
		PageID: page.ID, Title: "Audio retention", Body: "Thirty days.",
	})
	if err != nil {
		t.Fatalf("ResolveIntoNewNote: %v", err)
	}

	// FORWARD: the thread names the note.
	fwd, err := s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if fwd.ResolvedNoteID == nil || *fwd.ResolvedNoteID != note.ID {
		t.Fatalf("forward link = %v, want %s", fwd.ResolvedNoteID, note.ID)
	}

	// BACKWARD: the note names the thread.
	back, err := s.DiscussionsResolvedInto(ctx, note.ID)
	if err != nil {
		t.Fatalf("DiscussionsResolvedInto: %v", err)
	}
	if len(back) != 1 || back[0].ID != d.ID {
		t.Fatalf("reverse link = %+v, want the one thread %s", back, d.ID)
	}
	// It carries the handle, so a renderer can print "Resolved into" with
	// something a person can quote.
	if back[0].Ref() != FormatDiscussionRef(d.Number) {
		t.Errorf("reverse link ref = %q, want %q", back[0].Ref(), FormatDiscussionRef(d.Number))
	}

	// SEVERAL THREADS, ONE NOTE — the case a column could not hold.
	other := openThread(t, s, ctx, person, "Pinning")
	if _, err := s.ResolveIntoExistingNote(ctx, other.ID, note.ID, person,
		"Audio retention", "Thirty days, and a pinned memo is never pruned."); err != nil {
		t.Fatalf("second thread into the same note: %v", err)
	}
	back, err = s.DiscussionsResolvedInto(ctx, note.ID)
	if err != nil {
		t.Fatalf("DiscussionsResolvedInto: %v", err)
	}
	if len(back) != 2 {
		t.Errorf("%d threads resolved into the note, want 2", len(back))
	}

	// A note nothing concluded into has an empty list, not an error.
	plain := mkNote(t, s, ctx, page.ID, person, "Typed directly", "no thread behind this")
	back, err = s.DiscussionsResolvedInto(ctx, plain.ID)
	if err != nil {
		t.Fatalf("DiscussionsResolvedInto(plain): %v", err)
	}
	if len(back) != 0 {
		t.Errorf("a directly-typed note names %d threads, want 0", len(back))
	}
}

// ============================================================================
// Done when 3 — a resolved thread is still readable rather than hidden.
// ============================================================================

// THE READ SURFACE IS WHERE HIDING WOULD HAPPEN, so it is the one that has to
// assert it. Deliberately the opposite of NotesOnPage, which DOES filter
// soft-deleted notes: a deleted note was taken out of view on purpose, and a
// resolved thread concluded, which is the most interesting thing a thread does.
func TestAResolvedThreadIsStillReadable(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "readable@example.com")
	page := mkPage(t, s, ctx, nil, "estate")

	open, _, err := s.OpenDiscussion(ctx, NewDiscussion{
		Title: "Still going", AuthorID: person, Body: "one", PageID: &page.ID,
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	done, _, err := s.OpenDiscussion(ctx, NewDiscussion{
		Title: "Concluded", AuthorID: person, Body: "two", PageID: &page.ID,
	})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	appendTurn(t, s, ctx, done.ID, person, "and the answer is thirty days")
	if _, _, err := s.ResolveIntoNewNote(ctx, done.ID, person, Resolution{
		PageID: page.ID, Title: "Audio retention", Body: "Thirty days.",
	}); err != nil {
		t.Fatalf("ResolveIntoNewNote: %v", err)
	}

	listed, err := s.DiscussionsOnPage(ctx, page.ID)
	if err != nil {
		t.Fatalf("DiscussionsOnPage: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("%d threads listed on the page, want 2 — a resolved thread was hidden", len(listed))
	}
	seen := map[uuid.UUID]bool{}
	for _, l := range listed {
		seen[l.ID] = true
	}
	if !seen[open.ID] || !seen[done.ID] {
		t.Errorf("listed %+v, want both %s and %s", listed, open.ID, done.ID)
	}

	// Its turns read in full, and its handle still resolves.
	turns, err := s.Turns(ctx, done.ID)
	if err != nil {
		t.Fatalf("Turns on a resolved thread: %v", err)
	}
	if len(turns) != 2 || turns[1].Body != "and the answer is thirty days" {
		t.Errorf("a resolved thread's turns = %+v, want both intact", turns)
	}
	if _, err := s.DiscussionByRef(ctx, done.Ref()); err != nil {
		t.Errorf("a resolved thread's handle stopped resolving: %v", err)
	}
}

// ============================================================================
// "A deliberate choice rather than the default path of least resistance."
// ============================================================================

// A NAMED CALL, NOT A NIL ARGUMENT. The ticket asks that resolving with no note
// be deliberate; a nilable parameter makes it the easiest thing to type, and a
// caller that reaches ResolveWithoutNote has said what it means.
func TestResolvingWithoutANoteIsAllowedAndSaysSo(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "just-ended@example.com")
	page := mkPage(t, s, ctx, nil, "estate")
	d := openThread(t, s, ctx, person, "Never went anywhere")

	before := countNotes(t, s, ctx)
	if err := s.ResolveWithoutNote(ctx, d.ID, person); err != nil {
		t.Fatalf("ResolveWithoutNote: %v", err)
	}
	if after := countNotes(t, s, ctx); after != before {
		t.Errorf("resolving with no note wrote one: %d -> %d", before, after)
	}

	got, err := s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if !got.Resolved() {
		t.Error("the thread is not resolved")
	}
	if got.ResolvedNoteID != nil {
		t.Errorf("resolved_note_id = %v, want nil", got.ResolvedNoteID)
	}

	// AND IT CAN BE COMPLETED LATER. CH080's once-from-NULL clause is per
	// column exactly so a thread that ended without a note can be pointed at
	// one afterwards — completing the record rather than rewriting it.
	note, _, err := s.ResolveIntoNewNote(ctx, d.ID, person, Resolution{
		PageID: page.ID, Title: "Late conclusion", Body: "it did produce something after all",
	})
	if err != nil {
		t.Fatalf("linking a note to an already-resolved thread: %v", err)
	}
	got, err = s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if got.ResolvedNoteID == nil || *got.ResolvedNoteID != note.ID {
		t.Errorf("resolved_note_id = %v, want %s", got.ResolvedNoteID, note.ID)
	}
	// The original resolver and timestamp stand — this completed the record,
	// it did not replace it.
	if got.ResolvedBy == nil || *got.ResolvedBy != person {
		t.Errorf("resolved_by = %v, want the original resolver", got.ResolvedBy)
	}
}

// An agent cannot decide what a conversation concluded — CH080, the same rule
// CH041 states about a note's confirmer. Asserted on the ACT rather than only
// on the primitive, because this is the path CHRN-67 would reach for.
func TestAnAgentCannotResolveAThreadIntoANote(t *testing.T) {
	s, ctx := newTestStore(t)
	person := discPerson(t, s, ctx, "resolver@example.com")
	agent := discAgent(t, s, ctx, "resolver-agent@example.com")
	page := mkPage(t, s, ctx, nil, "estate")
	d := openThread(t, s, ctx, person, "Who concludes")

	before := countNotes(t, s, ctx)
	if _, _, err := s.ResolveIntoNewNote(ctx, d.ID, agent, Resolution{
		PageID: page.ID, Title: "Decided by a machine", Body: "x",
	}); !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("ResolveIntoNewNote by an agent err = %v, want ErrConfirmerRequired", err)
	}
	if after := countNotes(t, s, ctx); after != before {
		t.Errorf("a refused resolution left a note behind: %d -> %d", before, after)
	}
	if err := s.ResolveWithoutNote(ctx, d.ID, agent); !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("ResolveWithoutNote by an agent err = %v, want ErrConfirmerRequired", err)
	}

	// THE COMPLETION PATH THROUGH THIS FILE, which is the one the review of
	// PR #68 found open on the primitive. A person resolves with no note; an
	// agent then tries to say what the thread produced. CH080 cannot see the
	// caller here, because COALESCE leaves resolved_by unchanged.
	if err := s.ResolveWithoutNote(ctx, d.ID, person); err != nil {
		t.Fatalf("the person's resolve: %v", err)
	}
	before = countNotes(t, s, ctx)
	if _, _, err := s.ResolveIntoNewNote(ctx, d.ID, agent, Resolution{
		PageID: page.ID, Title: "Completed by a machine", Body: "x",
	}); !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("an agent completing a resolution err = %v, want ErrConfirmerRequired", err)
	}
	if after := countNotes(t, s, ctx); after != before {
		t.Errorf("a refused completion left a note behind: %d -> %d", before, after)
	}
	got, err := s.DiscussionByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("DiscussionByID: %v", err)
	}
	if got.ResolvedNoteID != nil {
		t.Errorf("the agent linked %v; nothing should have been written", got.ResolvedNoteID)
	}
}
