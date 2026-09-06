package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// CHRN-39's acceptance criteria, from the Switchyard plan revision 3 approved
// 2026-09-06 with all five rulings picked. Criteria that are migration-CI or
// review checks have no Go assertion to make; everything else is here.
//
// The rulings this file is testing the consequences of:
//
//	1 RESTORE BY APPENDING          2 COLUMNS ON tier2.notes
//	3 FOUR PEER VERBS               4 TRIGGER, ON EVERY INSERT
//	5 tier2.note_deletions

// person makes a confirming human. newAuthor already makes KindPerson; this
// exists so the tests below read as what they are testing.
func person(t *testing.T, s *Store, ctx context.Context, email string) uuid.UUID {
	return newAuthor(t, s, ctx, email)
}

func agent(t *testing.T, s *Store, ctx context.Context, email string) uuid.UUID {
	t.Helper()
	u, err := s.CreateUser(ctx, email, "Scribe", KindAgent)
	if err != nil {
		t.Fatalf("CreateUser(agent): %v", err)
	}
	return u.ID
}

func verb(v string) *string { return &v }

// --- Ruling 4 · nothing lands in authored text unattended -------------------

// A revision with no confirming person is refused — INCLUDING seq 1, which
// CreateNote writes. That is the ruling: an exemption for a create would put
// the hole exactly where a Scribe-routed note arrives, and whether an agent may
// create notes unattended is CHRN-67's argument to make.
func TestARevisionNeedsAConfirmingPerson(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "confirm@example.com")

	_, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, Title: "Unconfirmed", Body: "x",
	})
	if !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("CreateNote with no confirmer = %v, want ErrConfirmerRequired", err)
	}

	n := mkNote(t, s, ctx, page, author, "Confirmed", "x")
	_, err = s.AppendRevision(ctx, n.ID, NewRevision{AuthorID: author, Title: "t", Body: "b"})
	if !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("AppendRevision with no confirmer = %v, want ErrConfirmerRequired", err)
	}
}

// An agent may be the AUTHOR — for a Scribe-routed memo it legitimately is —
// but never the confirmer. The two columns answer different questions.
func TestAnAgentCannotConfirmButCanAuthor(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "author@example.com")
	scribe := agent(t, s, ctx, "scribe@chronicle.local")

	_, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, Title: "Agent-confirmed", Body: "x",
		ConfirmedBy: scribe,
	})
	if !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("agent as confirmer = %v, want ErrConfirmerRequired", err)
	}

	// The mirror image: agent authors, person confirms. This is the shape a
	// Scribe proposal accepted by the operator actually has.
	if _, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: scribe, Title: "Agent-authored", Body: "x",
		ConfirmedBy: author, Verb: verb(VerbCreate),
	}); err != nil {
		t.Errorf("agent author with person confirmer: %v", err)
	}
}

// --- Ruling 3 · the verb set ------------------------------------------------

func TestTheVerbIsRecordedAndAgreesWithSeq(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "verbs@example.com")

	n, rev, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, Title: "Smart calendar", Body: "the first pass",
		ConfirmedBy: author, Verb: verb(VerbCreate),
	})
	if err != nil {
		t.Fatalf("CreateNote(create): %v", err)
	}
	if rev.Verb == nil || *rev.Verb != VerbCreate {
		t.Errorf("seq-1 verb = %v, want %q", rev.Verb, VerbCreate)
	}

	app, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "Smart calendar", Body: "the first pass\n\nand a second",
		ConfirmedBy: author, Verb: verb(VerbAppend),
	})
	if err != nil {
		t.Fatalf("AppendRevision(append): %v", err)
	}
	if app.Verb == nil || *app.Verb != VerbAppend {
		t.Errorf("append verb = %v", app.Verb)
	}

	// create and relate produce a NEW note and are seq 1; append and supersede
	// cannot be. note_revisions_verb_seq refuses the disagreement rather than
	// leaving a row that claims to have appended to nothing.
	if _, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, Title: "Impossible", Body: "x",
		ConfirmedBy: author, Verb: verb(VerbAppend),
	}); err == nil {
		t.Error("CreateNote with verb=append succeeded, want refusal")
	}
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "t", Body: "b",
		ConfirmedBy: author, Verb: verb(VerbCreate),
	}); err == nil {
		t.Error("AppendRevision with verb=create succeeded, want refusal")
	}
}

// relate adds NO table, NO link row and NO write primitive. It is a note whose
// body references the target, and CHRN-42's derived graph does the rest — which
// is why an authored tier-2 link table was not needed.
func TestRelateAddsNoPrimitiveAndItsLinkDerives(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "relate@example.com")

	target := mkNote(t, s, ctx, page, author, "Smart calendar", "the original idea")

	related, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author,
		Title: "Maps add-on", Body: "a distinct idea.\n\nRelated: " + target.Ref(),
		ConfirmedBy: author, Verb: verb(VerbRelate),
	})
	if err != nil {
		t.Fatalf("CreateNote(relate): %v", err)
	}

	back, err := s.Backlinks(ctx, target.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].NoteID != related.ID {
		t.Errorf("backlinks = %+v, want the relating note %s", back, related.Ref())
	}
}

// --- Ruling 1 · restore by appending ----------------------------------------

func TestRestoreAppendsAndLeavesTheSourceAlone(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "restore@example.com")

	n := mkNote(t, s, ctx, page, author, "Naming conventions", "the original body")
	first, err := s.CurrentRevision(ctx, n.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "Naming conventions", Body: "something confidently wrong",
		ConfirmedBy: author, Verb: verb(VerbSupersede),
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}

	restored, err := s.RestoreRevision(ctx, n.ID, first.ID, author)
	if err != nil {
		t.Fatalf("RestoreRevision: %v", err)
	}

	if restored.Title != first.Title || restored.Body != first.Body {
		t.Errorf("restored (%q, %q), want (%q, %q)",
			restored.Title, restored.Body, first.Title, first.Body)
	}
	if restored.Seq != 3 {
		t.Errorf("restored seq = %d, want 3 — a restore APPENDS", restored.Seq)
	}
	if restored.RestoredFrom == nil || *restored.RestoredFrom != first.ID {
		t.Errorf("restored_from = %v, want %v", restored.RestoredFrom, first.ID)
	}
	// A restore is not a verb, and it does not claim the source's memo.
	if restored.Verb != nil {
		t.Errorf("restored verb = %v, want nil", restored.Verb)
	}
	if restored.MemoID != nil {
		t.Errorf("restored memo_id = %v, want nil — provenance is restored_from", restored.MemoID)
	}

	revs, err := s.NoteRevisions(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}
	if len(revs) != 3 {
		t.Fatalf("revisions = %d, want 3", len(revs))
	}
	for i, r := range revs {
		if r.Seq != i+1 {
			t.Errorf("revision %d seq = %d, want strictly increasing", i, r.Seq)
		}
	}
	// The source row is untouched — that is what append-only means.
	if revs[0].ID != first.ID || revs[0].Body != first.Body {
		t.Errorf("source revision changed: %+v", revs[0])
	}
}

// A note cannot restore from ANOTHER note's revision. The composite foreign
// key makes it unrepresentable rather than merely untested.
func TestRestoreCannotCrossNotes(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "cross@example.com")

	a := mkNote(t, s, ctx, page, author, "A", "a")
	b := mkNote(t, s, ctx, page, author, "B", "b")

	_, err := s.RestoreRevision(ctx, a.ID, b.CurrentRevisionID, author)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-note restore = %v, want ErrNotFound", err)
	}
}

// Ruling 1's mechanical half: rewind is not merely un-coded, it is refused.
// This is what stops a CHRN-67 writer moving the pointer back silently.
func TestThePointerCannotMoveBackwards(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "rewind@example.com")

	n := mkNote(t, s, ctx, page, author, "Naming", "first")
	first, err := s.CurrentRevision(ctx, n.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "Naming", Body: "second",
		ConfirmedBy: author, Verb: verb(VerbSupersede),
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}

	_, err = s.Pool().Exec(ctx,
		`UPDATE tier2.notes SET current_revision_id = $2 WHERE id = $1`, n.ID, first.ID)
	if got := sqlState(err); got != pgNoteRewind {
		t.Errorf("rewind SQLSTATE = %q (%v), want %s", got, err, pgNoteRewind)
	}
}

// --- The guard says what is MUTABLE, not only what is frozen ----------------

// The third `Done when`: no code path issues an UPDATE that loses text. A deny
// list cannot keep that promise, because a column added tomorrow is permitted
// by omission. This asserts the allow list refuses one it was never told about.
func TestNotesGuardRefusesAnUnlistedColumn(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "frozen@example.com")
	n := mkNote(t, s, ctx, page, author, "Naming", "body")

	// created_at is not in the allow list and is not part of the identity
	// triple 0011 froze, so before 0014 this UPDATE would have succeeded.
	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.notes SET created_at = now() - interval '1 day' WHERE id = $1`, n.ID)
	if got := sqlState(err); got != pgNoteColumnFrozen {
		t.Errorf("unlisted-column SQLSTATE = %q (%v), want %s", got, err, pgNoteColumnFrozen)
	}
}

// 0011's guard is added to, never relaxed: CH040 still refuses both operations.
func TestRevisionsAreStillAppendOnly(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "appendonly@example.com")
	n := mkNote(t, s, ctx, page, author, "Naming", "body")

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.note_revisions SET body = 'rewritten' WHERE note_id = $1`, n.ID)
	if got := sqlState(err); got != pgRevisionAppendOnly {
		t.Errorf("UPDATE SQLSTATE = %q, want %s", got, pgRevisionAppendOnly)
	}
	_, err = s.Pool().Exec(ctx, `DELETE FROM tier2.note_revisions WHERE note_id = $1`, n.ID)
	if got := sqlState(err); got != pgRevisionAppendOnly {
		t.Errorf("DELETE SQLSTATE = %q, want %s", got, pgRevisionAppendOnly)
	}
}

// --- Ruling 2 · soft delete, and Ruling 5 · the journal ---------------------

// The four read surfaces, all of them. A delete one surface forgets about is
// not a delete — and the rows are all still there, which is the point.
func TestASoftDeletedNoteLeavesEveryReadSurface(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "gone@example.com")

	n := mkNote(t, s, ctx, page, author, "Chiffchaff", "a note about chiffchaff")
	if err := s.TagNote(ctx, n.ID, "birds"); err != nil {
		t.Fatalf("TagNote: %v", err)
	}
	pointer := mkNote(t, s, ctx, page, author, "Pointer", "see "+n.Ref())
	_ = pointer

	if err := s.SoftDeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	onPage, err := s.NotesOnPage(ctx, page)
	if err != nil {
		t.Fatalf("NotesOnPage: %v", err)
	}
	for _, got := range onPage {
		if got.ID == n.ID {
			t.Error("NotesOnPage still lists the deleted note")
		}
	}

	hits, err := s.Search(ctx, "chiffchaff", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, h := range hits {
		if h.NoteID != nil && *h.NoteID == n.ID {
			t.Error("Search still returns the deleted note")
		}
	}

	back, err := s.Backlinks(ctx, n.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	_ = back // the deleted note is the TARGET here; see the dangling test.

	tagged, err := s.NotesByTag(ctx, "birds")
	if err != nil {
		t.Fatalf("NotesByTag: %v", err)
	}
	if len(tagged) != 0 {
		t.Errorf("NotesByTag returned %d, want 0", len(tagged))
	}

	// Nothing was destroyed. This is the whole difference between soft and not.
	var notes, revs int
	if err := s.Pool().QueryRow(ctx,
		`SELECT (SELECT count(*) FROM tier2.notes WHERE id = $1),
		        (SELECT count(*) FROM tier2.note_revisions WHERE note_id = $1)`,
		n.ID).Scan(&notes, &revs); err != nil {
		t.Fatalf("count: %v", err)
	}
	if notes != 1 || revs < 1 {
		t.Errorf("rows after delete: notes=%d revisions=%d, want the note and its history intact", notes, revs)
	}
}

// A reference to a deleted note reads as DANGLING, exactly like a reference to
// a note that was never created. Dropping the row would silently edit what the
// author wrote — they typed the reference.
func TestALinkToADeletedNoteDangles(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "dangle@example.com")

	target := mkNote(t, s, ctx, page, author, "Target", "the target")
	from := mkNote(t, s, ctx, page, author, "From", "see "+target.Ref())

	if err := s.SoftDeleteNote(ctx, target.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	out, err := s.OutboundLinks(ctx, from.ID)
	if err != nil {
		t.Fatalf("OutboundLinks: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("outbound = %d, want 1 — the reference must survive", len(out))
	}
	if out[0].ToNumber != target.Number {
		t.Errorf("to_number = %d, want %d", out[0].ToNumber, target.Number)
	}
	if out[0].Target != nil {
		t.Error("link to a deleted note resolved, want dangling")
	}
}

func TestADeletedNoteRefusesWrites(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "frozen2@example.com")
	elsewhere := mkPage(t, s, ctx, nil, "personal")

	n := mkNote(t, s, ctx, page, author, "Naming", "body")
	if err := s.SoftDeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "t", Body: "b", ConfirmedBy: author, Verb: verb(VerbAppend),
	}); !errors.Is(err, ErrNoteDeleted) {
		t.Errorf("append to deleted = %v, want ErrNoteDeleted", err)
	}

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.notes SET page_id = $2 WHERE id = $1`, n.ID, elsewhere.ID)
	if got := sqlState(err); got != pgNoteDeleted {
		t.Errorf("move of deleted SQLSTATE = %q (%v), want %s", got, err, pgNoteDeleted)
	}

	// CH060, not CH033: TagNote writes tier2.note_tags and never touches the
	// note row, so notes_guard cannot see it and a rule written there would
	// read as enforced while doing nothing.
	if err := s.TagNote(ctx, n.ID, "birds"); !errors.Is(err, ErrNoteDeleted) {
		t.Errorf("tag of deleted = %v, want ErrNoteDeleted", err)
	}

	// And it all works again once the note is back.
	if err := s.UndeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("UndeleteNote: %v", err)
	}
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "t", Body: "b", ConfirmedBy: author, Verb: verb(VerbAppend),
	}); err != nil {
		t.Errorf("append after undelete: %v", err)
	}
}

// Idempotent, and it does not overwrite: a second deleter cannot quietly
// become the recorded one.
func TestASecondDeleteDoesNotReplaceTheDeleter(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "first@example.com")
	second := person(t, s, ctx, "second@example.com")

	n := mkNote(t, s, ctx, page, author, "Naming", "body")
	if err := s.SoftDeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}
	if err := s.SoftDeleteNote(ctx, n.ID, second); err != nil {
		t.Errorf("second delete = %v, want a no-op", err)
	}

	got, err := s.NoteByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteByID: %v", err)
	}
	if got.DeletedBy == nil || *got.DeletedBy != author {
		t.Errorf("deleted_by = %v, want the FIRST deleter %v", got.DeletedBy, author)
	}

	dels, err := s.NoteDeletions(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteDeletions: %v", err)
	}
	if len(dels) != 1 {
		t.Errorf("deletion records = %d, want 1", len(dels))
	}
}

// Ruling 5. Without this table a delete followed by an undelete leaves the
// database holding no trace either happened — the same property Ruling 1
// refuses for revisions.
func TestDeleteAndUndeleteAreJournaled(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "journal@example.com")
	restorer := person(t, s, ctx, "restorer@example.com")

	n := mkNote(t, s, ctx, page, author, "Naming", "body")
	before, err := s.NoteRevisions(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}

	if err := s.SoftDeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}
	if err := s.UndeleteNote(ctx, n.ID, restorer); err != nil {
		t.Fatalf("UndeleteNote: %v", err)
	}

	dels, err := s.NoteDeletions(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteDeletions: %v", err)
	}
	if len(dels) != 1 {
		t.Fatalf("deletion records = %d, want 1", len(dels))
	}
	d := dels[0]
	if d.DeletedBy != author {
		t.Errorf("deleted_by = %v, want %v", d.DeletedBy, author)
	}
	if d.UndeletedBy == nil || *d.UndeletedBy != restorer {
		t.Errorf("undeleted_by = %v, want %v", d.UndeletedBy, restorer)
	}
	if d.UndeletedAt == nil || d.UndeletedAt.Before(d.DeletedAt) {
		t.Errorf("undeleted_at = %v, want at or after deleted_at %v", d.UndeletedAt, d.DeletedAt)
	}

	// The note is back everywhere, with its history and no new text revision.
	after, err := s.NoteRevisions(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}
	if len(after) != len(before) {
		t.Errorf("revisions after undelete = %d, want %d — undelete writes no text",
			len(after), len(before))
	}
	onPage, err := s.NotesOnPage(ctx, page)
	if err != nil {
		t.Fatalf("NotesOnPage: %v", err)
	}
	var found bool
	for _, g := range onPage {
		if g.ID == n.ID {
			found = true
		}
	}
	if !found {
		t.Error("NotesOnPage does not list the undeleted note")
	}
}

// The undeleter is a person too, and notes_guard cannot check it — the undelete
// sets notes.deleted_by to NULL, so there is no new value there to test. It is
// note_deletions_guard that refuses this.
func TestAnAgentCannotUndelete(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "undel@example.com")
	scribe := agent(t, s, ctx, "scribe2@chronicle.local")

	n := mkNote(t, s, ctx, page, author, "Naming", "body")
	if err := s.SoftDeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	err := s.UndeleteNote(ctx, n.ID, scribe)
	if !errors.Is(err, ErrConfirmerRequired) {
		t.Errorf("agent undelete = %v, want ErrConfirmerRequired", err)
	}

	// And the refusal rolled the whole transaction back — the note is still
	// gone rather than half-restored.
	got, err := s.NoteByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteByID: %v", err)
	}
	if !got.Deleted() {
		t.Error("note was undeleted by an agent")
	}
}

// A deletion record is not a working note. It closes out once and stays put.
func TestADeletionRecordIsNotRewritable(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "immutable@example.com")

	// A DIFFERENT person, or the UPDATE changes nothing and the guard is
	// right to allow it — an idempotent write is not a rewrite.
	someoneElse := person(t, s, ctx, "someone-else@example.com")

	n := mkNote(t, s, ctx, page, author, "Naming", "body")
	if err := s.SoftDeleteNote(ctx, n.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.note_deletions SET deleted_by = $2 WHERE note_id = $1`, n.ID, someoneElse)
	if got := sqlState(err); got != pgDeletionRecordFixed {
		t.Errorf("rewrite SQLSTATE = %q (%v), want %s", got, err, pgDeletionRecordFixed)
	}

	_, err = s.Pool().Exec(ctx, `DELETE FROM tier2.note_deletions WHERE note_id = $1`, n.ID)
	if got := sqlState(err); got != pgDeletionRecordFixed {
		t.Errorf("delete SQLSTATE = %q (%v), want %s", got, err, pgDeletionRecordFixed)
	}
}
