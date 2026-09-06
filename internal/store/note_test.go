package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CHRN-38's seventeen acceptance criteria, from the Switchyard plan revision 2
// approved 2026-09-05. Criteria 12-15 are review and migration-CI checks with
// no Go assertion to make; everything else is here, numbered as it is there.

// notePage builds a page and an author to hang notes off.
func notePage(t *testing.T, s *Store, ctx context.Context, email string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	p := mkPage(t, s, ctx, nil, "estate")
	return p.ID, newAuthor(t, s, ctx, email)
}

func mkNote(t *testing.T, s *Store, ctx context.Context, page, author uuid.UUID, title, body string) Note {
	t.Helper()
	n, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, ConfirmedBy: author, Title: title, Body: body,
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

// Criterion 0 — CHR-0311 resolves to exactly one note before and after a move.
func TestANoteNumberSurvivesAMove(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "one@example.com")
	elsewhere := mkPage(t, s, ctx, nil, "personal")

	n := mkNote(t, s, ctx, page, author, "Naming", "one segment per level")

	before, err := s.NoteByRef(ctx, n.Ref())
	if err != nil {
		t.Fatalf("NoteByRef(%s): %v", n.Ref(), err)
	}

	moved, err := s.MoveNote(ctx, n.ID, elsewhere.ID)
	if err != nil {
		t.Fatalf("MoveNote: %v", err)
	}

	after, err := s.NoteByRef(ctx, n.Ref())
	if err != nil {
		t.Fatalf("NoteByRef after move: %v", err)
	}
	// Four independent assertions and not a switch: a switch stops at the
	// first true case, so a red run on the identity check would hide whether
	// the number moved too — one line of a four-line answer, on the assertion
	// carrying criterion 0.
	if after.ID != before.ID {
		t.Errorf("id changed across a move: %s -> %s", before.ID, after.ID)
	}
	if after.Number != before.Number {
		t.Errorf("number changed across a move: %d -> %d", before.Number, after.Number)
	}
	if after.PageID == before.PageID {
		t.Errorf("page_id did not change, so nothing was actually moved")
	}
	if moved.PageID != elsewhere.ID {
		t.Errorf("page_id = %s, want %s", moved.PageID, elsewhere.ID)
	}
}

// Criterion 1 — numbers are never reused, and rollback is the only reuse path
// that exists because hard deletion is impossible after this migration.
func TestNoteNumbersAreNeverReused(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "two@example.com")

	first := mkNote(t, s, ctx, page, author, "First", "")

	// A create against a page that does not exist fails on the notes insert —
	// after the row, and therefore the sequence value, has been built.
	if _, _, err := s.CreateNote(ctx, NewNote{
		PageID: uuid.New(), AuthorID: author, ConfirmedBy: author, Title: "Doomed", Body: "",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateNote against a missing page err = %v, want ErrNotFound", err)
	}

	second := mkNote(t, s, ctx, page, author, "Second", "")
	if second.Number <= first.Number+1 {
		t.Errorf("numbers %d then %d — the rolled-back insert did not burn one",
			first.Number, second.Number)
	}

	// The other half of the criterion: there is no deletion path to test,
	// because there is no deletion.
	if _, err := s.Pool().Exec(ctx, `DELETE FROM tier2.notes WHERE id = $1`, first.ID); sqlState(err) != pgForeignKeyViolation {
		t.Errorf("DELETE note SQLSTATE = %q (%v), want %s", sqlState(err), err, pgForeignKeyViolation)
	}
	if _, err := s.Pool().Exec(ctx, `DELETE FROM tier2.note_revisions WHERE note_id = $1`, first.ID); sqlState(err) != pgRevisionAppendOnly {
		t.Errorf("DELETE revision SQLSTATE = %q (%v), want %s", sqlState(err), err, pgRevisionAppendOnly)
	}
}

// Criterion 2 — no authored text outside tier2.note_revisions, and a rename is
// a revision rather than an overwrite.
func TestTheNoteRowHoldsNoAuthoredText(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "three@example.com")

	var cols int
	if err := s.Pool().QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		 WHERE table_schema = 'tier2' AND table_name = 'notes'
		   AND column_name IN ('title', 'body')`).Scan(&cols); err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	if cols != 0 {
		t.Errorf("tier2.notes has %d authored-text columns, want 0", cols)
	}

	n := mkNote(t, s, ctx, page, author, "Naming", "the original body")
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, ConfirmedBy: author, Title: "Naming conventions", Body: "the original body",
	}); err != nil {
		t.Fatalf("AppendRevision (rename): %v", err)
	}

	revs, err := s.NoteRevisions(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteRevisions: %v", err)
	}
	if len(revs) != 2 {
		t.Fatalf("revisions after a rename = %d, want 2", len(revs))
	}
	if revs[0].Title != "Naming" {
		t.Errorf("the pre-rename title is gone: %q", revs[0].Title)
	}
	cur, err := s.CurrentRevision(ctx, n.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if cur.Title != "Naming conventions" {
		t.Errorf("current title = %q", cur.Title)
	}
}

// Criterion 3 — a note cannot point at another note's revision, and
// current_revision_id is NOT NULL.
func TestANotePointsOnlyAtItsOwnRevision(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "four@example.com")

	a := mkNote(t, s, ctx, page, author, "A", "a")
	b := mkNote(t, s, ctx, page, author, "B", "b")

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.notes SET current_revision_id = $2 WHERE id = $1`,
		a.ID, b.CurrentRevisionID)
	if got := sqlState(err); got != pgForeignKeyViolation {
		t.Errorf("cross-note pointer SQLSTATE = %q (%v), want %s", got, err, pgForeignKeyViolation)
	}

	var nullable string
	if err := s.Pool().QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		 WHERE table_schema = 'tier2' AND table_name = 'notes'
		   AND column_name = 'current_revision_id'`).Scan(&nullable); err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	if nullable != "NO" {
		t.Errorf("current_revision_id is_nullable = %q, want NO", nullable)
	}
}

// Criterion 4 — one transaction, and a failure partway leaves neither row.
func TestCreateNoteIsAllOrNothing(t *testing.T) {
	s, ctx := newTestStore(t)
	_, author := notePage(t, s, ctx, "five@example.com")

	before := countRevisions(t, s, ctx)
	if _, _, err := s.CreateNote(ctx, NewNote{
		PageID: uuid.New(), AuthorID: author, ConfirmedBy: author, Title: "Doomed", Body: "text that must not survive",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateNote against a missing page err = %v, want ErrNotFound", err)
	}
	if got := countRevisions(t, s, ctx); got != before {
		t.Errorf("revisions %d -> %d: the failed create left a row behind", before, got)
	}
}

func countRevisions(t *testing.T, s *Store, ctx context.Context) int {
	t.Helper()
	var n int
	if err := s.Pool().QueryRow(ctx, `SELECT count(*) FROM tier2.note_revisions`).Scan(&n); err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	return n
}

// Criterion 5 — THE RACE. Concurrent appends all succeed with consecutive seq
// values, and the highest is the one left current. Without the row lock in
// AppendRevision they read the same max(seq), all but one fail on
// UNIQUE (note_id, seq), and which is current depends on commit order.
//
// EIGHT WORKERS RELEASED FROM ONE BARRIER, not two goroutines started in a
// loop. The two-goroutine version of this test passed with the lock REMOVED —
// the second append reliably began after the first had committed, so it never
// contended and the test asserted nothing. Verified the other way round too:
// with the lock removed this body fails on 23505 every run.
func TestConcurrentAppendsBothLand(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "six@example.com")
	n := mkNote(t, s, ctx, page, author, "Contended", "v1")

	const workers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, workers)
	revs := make([]NoteRevision, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			revs[i], errs[i] = s.AppendRevision(ctx, n.ID, NewRevision{
				AuthorID: author, ConfirmedBy: author, Title: "Contended", Body: "concurrent",
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

	// Consecutive and gapless: 2..workers+1, each exactly once.
	seen := map[int]int{}
	highest := revs[0]
	for _, r := range revs {
		seen[r.Seq]++
		if r.Seq > highest.Seq {
			highest = r
		}
	}
	for want := 2; want <= workers+1; want++ {
		if seen[want] != 1 {
			t.Errorf("seq %d appeared %d times, want exactly once (all: %v)", want, seen[want], seen)
		}
	}

	after, err := s.NoteByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteByID: %v", err)
	}
	if after.CurrentRevisionID != highest.ID {
		t.Errorf("current revision is not the highest seq (%d)", highest.Seq)
	}
}

// Criterion 6 — history is append-only, refused by the database.
func TestRevisionsAreAppendOnly(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "seven@example.com")
	n := mkNote(t, s, ctx, page, author, "Naming", "the body a person wrote")

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.note_revisions SET body = 'rewritten' WHERE note_id = $1`, n.ID)
	if got := sqlState(err); got != pgRevisionAppendOnly {
		t.Errorf("UPDATE SQLSTATE = %q (%v), want %s", got, err, pgRevisionAppendOnly)
	}
	_, err = s.Pool().Exec(ctx,
		`DELETE FROM tier2.note_revisions WHERE note_id = $1`, n.ID)
	if got := sqlState(err); got != pgRevisionAppendOnly {
		t.Errorf("DELETE SQLSTATE = %q (%v), want %s", got, err, pgRevisionAppendOnly)
	}

	cur, err := s.CurrentRevision(ctx, n.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if cur.Body != "the body a person wrote" {
		t.Errorf("body = %q — something got through", cur.Body)
	}
}

// Criterion 7 — identity immutable, page_id mutable, updated_at maintained by
// the trigger rather than by whoever remembered to set it.
func TestNoteIdentityIsImmutableAndUpdatedAtIsMaintained(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "eight@example.com")
	other := newAuthor(t, s, ctx, "eight-b@example.com")
	n := mkNote(t, s, ctx, page, author, "Naming", "")

	for _, tc := range []struct {
		what string
		sql  string
		arg  any
	}{
		{"id", `UPDATE tier2.notes SET id = gen_random_uuid() WHERE id = $1`, nil},
		{"number", `UPDATE tier2.notes SET number = number + 1000 WHERE id = $1`, nil},
		{"author", `UPDATE tier2.notes SET author_id = $2 WHERE id = $1`, other},
	} {
		var err error
		if tc.arg == nil {
			_, err = s.Pool().Exec(ctx, tc.sql, n.ID)
		} else {
			_, err = s.Pool().Exec(ctx, tc.sql, n.ID, tc.arg)
		}
		if got := sqlState(err); got != pgNoteIdentity {
			t.Errorf("%s: SQLSTATE = %q (%v), want %s", tc.what, got, err, pgNoteIdentity)
		}
	}

	// The trigger overwrites whatever a writer supplies, so a backdated value
	// does not survive — which is what "maintained" has to mean for the column
	// to be worth reading.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.notes SET updated_at = '2020-01-01T00:00:00Z' WHERE id = $1`, n.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	after, err := s.NoteByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("NoteByID: %v", err)
	}
	if after.UpdatedAt.Year() == 2020 {
		t.Error("updated_at kept the backdated value; the guard is not maintaining it")
	}
	if time.Since(after.UpdatedAt) > time.Minute {
		t.Errorf("updated_at = %s, want roughly now", after.UpdatedAt)
	}
}

// Criterion 8 — provenance, both cases, through one code path.
func TestProvenanceIsRecordedWhereItExists(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "nine@example.com")

	memo := newTranscribableMemo(t, s, ctx, "nine-memo@example.com")
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: memo.ID, Text: "what somebody said", Model: "whisper.cpp/small.en",
		Backend: "whisper.cpp",
	}); err != nil {
		t.Fatalf("RecordTranscript: %v", err)
	}

	fromMemo, rev, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, ConfirmedBy: author, Title: "From a memo", Body: "what somebody said",
		MemoID: &memo.ID,
	})
	if err != nil {
		t.Fatalf("CreateNote from memo: %v", err)
	}
	if rev.MemoID == nil || *rev.MemoID != memo.ID {
		t.Fatalf("revision memo_id = %v, want %s", rev.MemoID, memo.ID)
	}
	// And the transcript is reachable through it.
	tr, err := s.GetTranscript(ctx, *rev.MemoID)
	if err != nil {
		t.Fatalf("GetTranscript through provenance: %v", err)
	}
	if tr.Text != "what somebody said" {
		t.Errorf("transcript text = %q", tr.Text)
	}

	authored := mkNote(t, s, ctx, page, author, "Typed directly", "no memo behind this one")
	authoredRev, err := s.CurrentRevision(ctx, authored.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if authoredRev.MemoID != nil {
		t.Errorf("authored note has provenance %v, want nil", authoredRev.MemoID)
	}

	// SAME CODE PATH: both are readable, revisable and resolvable identically.
	for _, n := range []Note{fromMemo, authored} {
		if _, err := s.NoteByRef(ctx, n.Ref()); err != nil {
			t.Errorf("NoteByRef(%s): %v", n.Ref(), err)
		}
		if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
			AuthorID: author, ConfirmedBy: author, Title: "revised", Body: "revised",
		}); err != nil {
			t.Errorf("AppendRevision(%s): %v", n.Ref(), err)
		}
	}
}

// Criterion 9 — one memo, at most one revision; any number of authored ones.
func TestAMemoLandsInExactlyOneRevision(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "ten@example.com")
	memo := newTranscribableMemo(t, s, ctx, "ten-memo@example.com")

	if _, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, ConfirmedBy: author, Title: "First landing", Body: "x", MemoID: &memo.ID,
	}); err != nil {
		t.Fatalf("first landing: %v", err)
	}

	_, _, err := s.CreateNote(ctx, NewNote{
		PageID: page, AuthorID: author, ConfirmedBy: author, Title: "Second landing", Body: "x", MemoID: &memo.ID,
	})
	if !errors.Is(err, ErrMemoAlreadyLanded) {
		t.Errorf("second landing err = %v, want ErrMemoAlreadyLanded", err)
	}

	// AND THROUGH THE APPEND PATH, which is where a caller meets this in
	// practice: the second pass at an idea arrives as a revision of the note
	// the first pass created, not as a new note.
	other := mkNote(t, s, ctx, page, author, "Somewhere else", "")
	_, err = s.AppendRevision(ctx, other.ID, NewRevision{
		AuthorID: author, ConfirmedBy: author, Title: "Second landing", Body: "x", MemoID: &memo.ID,
	})
	if !errors.Is(err, ErrMemoAlreadyLanded) {
		t.Errorf("append with a landed memo err = %v, want ErrMemoAlreadyLanded", err)
	}

	// NULL provenance is not constrained — most notes will be typed.
	for i := 0; i < 3; i++ {
		mkNote(t, s, ctx, page, author, "Authored", "")
	}
}

// Criterion 10 — render strictly, parse leniently.
func TestNoteRefRendersStrictlyAndParsesLeniently(t *testing.T) {
	if got := FormatNoteRef(311); got != "CHR-0311" {
		t.Errorf("FormatNoteRef(311) = %q, want CHR-0311", got)
	}
	if got := FormatNoteRef(10000); got != "CHR-10000" {
		t.Errorf("FormatNoteRef(10000) = %q, want CHR-10000", got)
	}
	if got := FormatNoteRef(1); got != "CHR-0001" {
		t.Errorf("FormatNoteRef(1) = %q, want CHR-0001", got)
	}

	for _, in := range []string{"CHR-0311", "CHR-311", "chr-0311", "CHR-00311", "Chr-311"} {
		got, err := ParseNoteRef(in)
		if err != nil {
			t.Errorf("ParseNoteRef(%q): %v", in, err)
			continue
		}
		if got != 311 {
			t.Errorf("ParseNoteRef(%q) = %d, want 311", in, got)
		}
	}
	for _, in := range []string{"", "CHR-", "CHR-abc", "311", "CHR 311", "CHR-311x", "CHR-0", "SY-311"} {
		if _, err := ParseNoteRef(in); !errors.Is(err, ErrInvalidNoteRef) {
			t.Errorf("ParseNoteRef(%q) err = %v, want ErrInvalidNoteRef", in, err)
		}
	}
}

// Criterion 11 — an emptied note is authored and complete. The
// tier2.transcripts.text ruling, applied to what a person wrote.
func TestAnEmptyTitleAndBodyAreValues(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "eleven@example.com")

	n := mkNote(t, s, ctx, page, author, "", "")
	cur, err := s.CurrentRevision(ctx, n.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if cur.Title != "" || cur.Body != "" {
		t.Errorf("title = %q body = %q, want both empty", cur.Title, cur.Body)
	}

	// Deliberately blanking a note that had text is a revision like any other.
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{AuthorID: author, ConfirmedBy: author}); err != nil {
		t.Fatalf("AppendRevision(blank): %v", err)
	}
	revs, err := s.NoteRevisions(ctx, n.ID)
	if err != nil || len(revs) != 2 {
		t.Fatalf("revisions = %d (%v), want 2", len(revs), err)
	}
}

// Not a numbered criterion: the notes a page holds, which is what CHRN-37's
// move has to keep intact and what CHRN-42 will hang backlinks off.
func TestNotesOnPageFollowThePage(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "twelve@example.com")
	child := mkPage(t, s, ctx, &page, "conventions")

	a := mkNote(t, s, ctx, child.ID, author, "A", "")
	b := mkNote(t, s, ctx, child.ID, author, "B", "")

	// Moving the PAGE does not touch the notes: they address it by id.
	if _, err := s.MovePage(ctx, child.ID, nil, "conventions"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	notes, err := s.NotesOnPage(ctx, child.ID)
	if err != nil {
		t.Fatalf("NotesOnPage: %v", err)
	}
	if len(notes) != 2 || notes[0].ID != a.ID || notes[1].ID != b.ID {
		t.Errorf("notes = %+v, want %s then %s", notes, a.ID, b.ID)
	}
}
