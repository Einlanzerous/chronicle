package store

import (
	"errors"
	"testing"
)

// CHRN-42's `Done when`: linking A to B makes B show A, a renamed note keeps
// both directions, and tags are a filter on the tree rather than a second
// hierarchy.

// THE FIRST CLAIM, and the one the whole ticket is for. Nobody typed a link
// row: A mentions B's number in its prose and B's backlink list grows.
func TestLinkingAToBMakesBShowA(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "backlink@example.com")

	b := mkNote(t, s, ctx, page, author, "Naming conventions", "one segment per level")
	a := mkNote(t, s, ctx, page, author, "Slug rules",
		"this follows from "+b.Ref()+" and extends it")

	back, err := s.Backlinks(ctx, b.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("backlinks = %+v, want 1", back)
	}
	if back[0].NoteID != a.ID || back[0].Title != "Slug rules" {
		t.Errorf("backlink = %+v, want %s / Slug rules", back[0], a.ID)
	}

	out, err := s.OutboundLinks(ctx, a.ID)
	if err != nil {
		t.Fatalf("OutboundLinks: %v", err)
	}
	if len(out) != 1 || out[0].Target == nil || out[0].Target.NoteID != b.ID {
		t.Errorf("outbound = %+v, want a resolved link to %s", out, b.ID)
	}
}

// A reference written before its target exists is RECORDED, not dropped, and
// starts resolving the moment the target arrives. Nothing repairs it — the
// join does the work, which is why the edge stores the number and not an id.
func TestADanglingReferenceResolvesItselfLater(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "dangling@example.com")

	// Point at a number that does not exist yet.
	a := mkNote(t, s, ctx, page, author, "Early", "see CHR-0002 once it is written")

	out, err := s.OutboundLinks(ctx, a.ID)
	if err != nil {
		t.Fatalf("OutboundLinks: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("outbound = %+v, want the dangling edge recorded", out)
	}
	if out[0].Target != nil {
		t.Errorf("target resolved before it existed: %+v", out[0])
	}
	if out[0].ToNumber != 2 || out[0].Ref() != "CHR-0002" {
		t.Errorf("dangling edge = %+v, want CHR-0002", out[0])
	}

	// The target arrives. Nothing touches the link row.
	b := mkNote(t, s, ctx, page, author, "Late", "here it is")
	if b.Number != 2 {
		t.Fatalf("expected the next note to be number 2, got %d", b.Number)
	}

	out, err = s.OutboundLinks(ctx, a.ID)
	if err != nil {
		t.Fatalf("OutboundLinks: %v", err)
	}
	if len(out) != 1 || out[0].Target == nil || out[0].Target.NoteID != b.ID {
		t.Errorf("outbound = %+v, want it resolved now", out)
	}
	back, err := s.Backlinks(ctx, b.Number)
	if err != nil || len(back) != 1 || back[0].NoteID != a.ID {
		t.Errorf("backlinks = %+v (%v), want the earlier note", back, err)
	}
}

// `Done when` #2 — a renamed note keeps both directions. A rename is a
// revision, so the graph is re-derived; the number is what the edge holds, and
// a number does not change.
func TestARenamedNoteKeepsBothDirections(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "rename@example.com")
	elsewhere := mkPage(t, s, ctx, nil, "personal")

	b := mkNote(t, s, ctx, page, author, "Naming", "the original")
	a := mkNote(t, s, ctx, page, author, "Pointer", "see "+b.Ref())

	// Rename the TARGET, and move it to another page for good measure.
	if _, err := s.AppendRevision(ctx, b.ID, NewRevision{
		AuthorID: author, Title: "Naming conventions, renamed", Body: "the original",
	}); err != nil {
		t.Fatalf("rename target: %v", err)
	}
	if _, err := s.MoveNote(ctx, b.ID, elsewhere.ID); err != nil {
		t.Fatalf("MoveNote: %v", err)
	}
	// And rename the SOURCE, whose text still carries the reference.
	if _, err := s.AppendRevision(ctx, a.ID, NewRevision{
		AuthorID: author, Title: "Pointer, renamed", Body: "see " + b.Ref(),
	}); err != nil {
		t.Fatalf("rename source: %v", err)
	}

	back, err := s.Backlinks(ctx, b.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 1 || back[0].NoteID != a.ID {
		t.Fatalf("backlinks after rename = %+v, want the source", back)
	}
	if back[0].Title != "Pointer, renamed" {
		t.Errorf("backlink shows a stale title: %q", back[0].Title)
	}
	out, err := s.OutboundLinks(ctx, a.ID)
	if err != nil || len(out) != 1 || out[0].Target == nil {
		t.Fatalf("outbound after rename = %+v (%v)", out, err)
	}
	if out[0].Target.Title != "Naming conventions, renamed" {
		t.Errorf("outbound shows a stale title: %q", out[0].Target.Title)
	}
}

// A revision that drops a reference drops the edge. The graph describes the
// CURRENT text — a link the note no longer makes is not a link.
func TestRemovingAReferenceRemovesTheEdge(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "remove@example.com")
	b := mkNote(t, s, ctx, page, author, "Target", "x")
	a := mkNote(t, s, ctx, page, author, "Source", "see "+b.Ref())

	if back, _ := s.Backlinks(ctx, b.Number); len(back) != 1 {
		t.Fatalf("precondition: backlinks = %+v", back)
	}
	if _, err := s.AppendRevision(ctx, a.ID, NewRevision{
		AuthorID: author, Title: "Source", Body: "the reference is gone now",
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	back, err := s.Backlinks(ctx, b.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 0 {
		t.Errorf("backlinks = %+v, want none — the text no longer refers to it", back)
	}
}

// References in code are not links, the same rule CHRN-40 applies to
// rendering. A note explaining a bug by quoting a number is discussing a
// string.
func TestReferencesInCodeAreNotEdges(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "code@example.com")
	b := mkNote(t, s, ctx, page, author, "Target", "x")
	mkNote(t, s, ctx, page, author, "Source", "the key is `"+b.Ref()+"` in the map")

	back, err := s.Backlinks(ctx, b.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 0 {
		t.Errorf("a quoted reference became an edge: %+v", back)
	}
}

// A note quoting its own number is not in its own backlink list.
func TestANoteDoesNotLinkToItself(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "self@example.com")
	n := mkNote(t, s, ctx, page, author, "Self", "x")
	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, Title: "Self", Body: "this note, " + n.Ref() + ", is about itself",
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	back, err := s.Backlinks(ctx, n.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(back) != 0 {
		t.Errorf("a note linked to itself: %+v", back)
	}
}

// THE TIER-1 CLAIM, MADE GOOD. "Disposable because it is regenerable" needs
// something that regenerates it, or the table quietly becomes the only copy of
// something — the failure the tier split exists to prevent.
func TestTheLinkGraphIsRegenerable(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "rebuild@example.com")
	b := mkNote(t, s, ctx, page, author, "Target", "x")
	mkNote(t, s, ctx, page, author, "One", "see "+b.Ref())
	mkNote(t, s, ctx, page, author, "Two", "also "+b.Ref())

	before, err := s.Backlinks(ctx, b.Number)
	if err != nil || len(before) != 2 {
		t.Fatalf("precondition: backlinks = %+v (%v)", before, err)
	}

	// Throw the whole derived table away, the way tier 1 is allowed to be.
	if _, err := s.Pool().Exec(ctx, `DELETE FROM tier1.note_links`); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got, _ := s.Backlinks(ctx, b.Number); len(got) != 0 {
		t.Fatalf("clear did not clear: %+v", got)
	}

	n, err := s.RebuildNoteLinks(ctx)
	if err != nil {
		t.Fatalf("RebuildNoteLinks: %v", err)
	}
	if n != 2 {
		t.Errorf("rebuilt %d edges, want 2", n)
	}
	after, err := s.Backlinks(ctx, b.Number)
	if err != nil {
		t.Fatalf("Backlinks: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("rebuild produced %d backlinks, want %d", len(after), len(before))
	}
	for i := range before {
		if after[i].NoteID != before[i].NoteID {
			t.Errorf("rebuilt graph differs at %d: %s vs %s", i, after[i].NoteID, before[i].NoteID)
		}
	}
}

// `Done when` #3 — tags are a filter on the tree, not a second hierarchy.
func TestTagsAreAFilterAcrossTheTree(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "tags@example.com")
	other := mkPage(t, s, ctx, nil, "personal")

	a := mkNote(t, s, ctx, page, author, "On the estate page", "x")
	b := mkNote(t, s, ctx, other.ID, author, "On the personal page", "y")
	c := mkNote(t, s, ctx, page, author, "Untagged", "z")

	for _, n := range []Note{a, b} {
		if err := s.TagNote(ctx, n.ID, "hardware"); err != nil {
			t.Fatalf("TagNote: %v", err)
		}
	}

	// THE FILTER CUTS ACROSS PAGES. If tags were a second hierarchy this would
	// have to be scoped to one of them.
	got, err := s.NotesByTag(ctx, "hardware")
	if err != nil {
		t.Fatalf("NotesByTag: %v", err)
	}
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("NotesByTag = %+v, want the two tagged notes from both pages", got)
	}
	if got[0].PageID == got[1].PageID {
		t.Error("the two results are on one page, so this proves nothing about cutting across")
	}
	_ = c

	// Tagging twice is the same claim, not an error.
	if err := s.TagNote(ctx, a.ID, "HARDWARE"); err != nil {
		t.Fatalf("re-tag: %v", err)
	}
	tags, err := s.NoteTags(ctx, a.ID)
	if err != nil {
		t.Fatalf("NoteTags: %v", err)
	}
	if len(tags) != 1 || tags[0] != "hardware" {
		t.Errorf("tags = %v, want one normalised hardware", tags)
	}

	if err := s.UntagNote(ctx, a.ID, "hardware"); err != nil {
		t.Fatalf("UntagNote: %v", err)
	}
	if tags, _ := s.NoteTags(ctx, a.ID); len(tags) != 0 {
		t.Errorf("tags after untag = %v", tags)
	}
	// Removing one that is not there is not an error.
	if err := s.UntagNote(ctx, a.ID, "hardware"); err != nil {
		t.Errorf("idempotent untag: %v", err)
	}
}

// A tag cannot be a path, which is what stops it becoming a hierarchy.
func TestATagCannotBeAPath(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "tagshape@example.com")
	n := mkNote(t, s, ctx, page, author, "Note", "x")

	for _, bad := range []string{"hardware/audio", "", "Hardware Audio", "-lead", "trail-", "a--b", "under_score"} {
		if err := s.TagNote(ctx, n.ID, bad); !errors.Is(err, ErrInvalidTag) {
			t.Errorf("TagNote(%q) err = %v, want ErrInvalidTag", bad, err)
		}
	}
	for _, good := range []string{"hardware", "audio-gear", "chrn-42", "a1"} {
		if err := s.TagNote(ctx, n.ID, good); err != nil {
			t.Errorf("TagNote(%q): %v", good, err)
		}
	}
}

func TestAllTagsIsTheFilterVocabulary(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "vocab@example.com")
	a := mkNote(t, s, ctx, page, author, "A", "x")
	b := mkNote(t, s, ctx, page, author, "B", "y")

	for _, tag := range []string{"hardware", "audio"} {
		if err := s.TagNote(ctx, a.ID, tag); err != nil {
			t.Fatalf("TagNote: %v", err)
		}
	}
	if err := s.TagNote(ctx, b.ID, "hardware"); err != nil {
		t.Fatalf("TagNote: %v", err)
	}

	got, err := s.AllTags(ctx)
	if err != nil {
		t.Fatalf("AllTags: %v", err)
	}
	if got["hardware"] != 2 || got["audio"] != 1 || len(got) != 2 {
		t.Errorf("AllTags = %v, want hardware:2 audio:1", got)
	}
}

// Tags are AUTHORED, so a note carrying them cannot be deleted out from under
// them — the same RESTRICT the rest of tier 2 uses.
func TestTagsAreTier2AndRestrictDeletion(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "tier@example.com")
	n := mkNote(t, s, ctx, page, author, "Note", "x")
	if err := s.TagNote(ctx, n.ID, "hardware"); err != nil {
		t.Fatalf("TagNote: %v", err)
	}

	_, err := s.Pool().Exec(ctx, `DELETE FROM tier2.notes WHERE id = $1`, n.ID)
	if got := sqlState(err); got != pgForeignKeyViolation {
		t.Errorf("SQLSTATE = %q (%v), want %s", got, err, pgForeignKeyViolation)
	}

	// And the tier-1 half carries no such reference, deliberately: 0004
	// through 0007 forbid a tier-1 table pointing into tier 2, so a link row
	// can outlive its note and RebuildNoteLinks is what collects it.
	var fks int
	if err := s.Pool().QueryRow(ctx, `
		SELECT count(*) FROM information_schema.table_constraints
		 WHERE constraint_schema = 'tier1' AND table_name = 'note_links'
		   AND constraint_type = 'FOREIGN KEY'`).Scan(&fks); err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	if fks != 0 {
		t.Errorf("tier1.note_links has %d foreign keys, want 0 — see 0004", fks)
	}
}
