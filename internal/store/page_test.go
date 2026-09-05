package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// CHRN-37's `Done when`: a page can be created, nested, renamed and moved with
// every note and backlink intact, and the old path either redirects or is
// explicitly gone.
//
// THE "NOTE AND BACKLINK INTACT" HALF IS PROVEN WHERE THOSE EXIST. Notes are
// CHRN-38 and backlinks are CHRN-42, and both address a page by `id`. What
// this file proves is the property they rest on — that a move changes a page's
// path and never its identity — plus the assertion that no query anywhere
// needs a stored path to find a page again. CHRN-38's first criterion ("CHR-0311
// resolves to exactly one note before and after that note is moved") is the
// same claim with a note attached, and it is asserted there.

// mkPage creates a page and fails the test rather than returning an error.
func mkPage(t *testing.T, s *Store, ctx context.Context, parent *uuid.UUID, slug string) Page {
	t.Helper()
	p, err := s.CreatePage(ctx, parent, slug)
	if err != nil {
		t.Fatalf("CreatePage(%v, %q): %v", parent, slug, err)
	}
	return p
}

// tree builds estate → conventions → naming and returns the three pages.
func tree(t *testing.T, s *Store, ctx context.Context) (estate, conventions, naming Page) {
	t.Helper()
	estate = mkPage(t, s, ctx, nil, "estate")
	conventions = mkPage(t, s, ctx, &estate.ID, "conventions")
	naming = mkPage(t, s, ctx, &conventions.ID, "naming")
	return
}

func mustPath(t *testing.T, s *Store, ctx context.Context, id uuid.UUID) string {
	t.Helper()
	p, err := s.PagePath(ctx, id)
	if err != nil {
		t.Fatalf("PagePath(%s): %v", id, err)
	}
	return p
}

func TestPagePathIsBuiltFromAncestry(t *testing.T) {
	s, ctx := newTestStore(t)
	_, conventions, naming := tree(t, s, ctx)

	if got := mustPath(t, s, ctx, naming.ID); got != "estate/conventions/naming" {
		t.Errorf("path = %q, want estate/conventions/naming", got)
	}
	if got := mustPath(t, s, ctx, conventions.ID); got != "estate/conventions" {
		t.Errorf("path = %q, want estate/conventions", got)
	}
}

func TestPageByPathResolvesTheTree(t *testing.T) {
	s, ctx := newTestStore(t)
	_, _, naming := tree(t, s, ctx)

	got, err := s.PageByPath(ctx, "estate/conventions/naming")
	if err != nil {
		t.Fatalf("PageByPath: %v", err)
	}
	if got.ID != naming.ID {
		t.Errorf("id = %s, want %s", got.ID, naming.ID)
	}

	if _, err := s.PageByPath(ctx, "estate/conventions/nothing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing page err = %v, want ErrNotFound", err)
	}
}

// Two pages under one parent cannot claim one path. The root case is separate
// because parent_id is NULL there and NULLs do not compare equal, so it needs
// its own partial unique index and therefore its own test.
func TestSiblingSlugsCannotCollide(t *testing.T) {
	s, ctx := newTestStore(t)
	estate, _, _ := tree(t, s, ctx)

	if _, err := s.CreatePage(ctx, &estate.ID, "conventions"); !errors.Is(err, ErrSiblingSlug) {
		t.Errorf("duplicate child err = %v, want ErrSiblingSlug", err)
	}
	if _, err := s.CreatePage(ctx, nil, "estate"); !errors.Is(err, ErrSiblingSlug) {
		t.Errorf("duplicate root err = %v, want ErrSiblingSlug", err)
	}
	// Same slug under a DIFFERENT parent is fine — that is nesting working.
	other := mkPage(t, s, ctx, nil, "personal")
	if _, err := s.CreatePage(ctx, &other.ID, "conventions"); err != nil {
		t.Errorf("same slug under another parent: %v", err)
	}
}

func TestSlugsAreOneSegmentAndLowercase(t *testing.T) {
	s, ctx := newTestStore(t)

	for _, bad := range []string{
		"",                   // names nothing
		"Estate",             // uppercase: two pages nobody can tell apart aloud
		"estate/conventions", // a slug that smuggles a second level
		"-estate", "estate-", // leading or trailing hyphen
		"estate--conventions", // doubled hyphen
		"estate conventions",  // space
		"estate_conventions",  // underscore is not the estate's separator
	} {
		if _, err := s.CreatePage(ctx, nil, bad); !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("CreatePage(%q) err = %v, want ErrInvalidSlug", bad, err)
		}
	}
	for _, good := range []string{"estate", "e", "conventions-and-naming", "chrn-37", "a1"} {
		if _, err := s.CreatePage(ctx, nil, good); err != nil {
			t.Errorf("CreatePage(%q): %v", good, err)
		}
	}
}

// A malformed path is refused as malformed rather than reported as missing.
// The two are different answers and a caller acts on them differently.
func TestSplitPathRejectsTheShapesThatLookHarmless(t *testing.T) {
	for _, bad := range []string{"", "/estate", "estate/", "estate//conventions", "/"} {
		if _, err := SplitPath(bad); !errors.Is(err, ErrInvalidSlug) {
			t.Errorf("SplitPath(%q) err = %v, want ErrInvalidSlug", bad, err)
		}
	}
	segs, err := SplitPath("estate/conventions/naming")
	if err != nil {
		t.Fatalf("SplitPath: %v", err)
	}
	if len(segs) != 3 || segs[0] != "estate" || segs[2] != "naming" {
		t.Errorf("segments = %v", segs)
	}
}

// A rename keeps the page and leaves its old path resolvable.
func TestRenameKeepsIdentityAndRedirects(t *testing.T) {
	s, ctx := newTestStore(t)
	_, conventions, _ := tree(t, s, ctx)

	moved, err := s.MovePage(ctx, conventions.ID, conventions.ParentID, "conventions-and-style")
	if err != nil {
		t.Fatalf("MovePage: %v", err)
	}
	if moved.ID != conventions.ID {
		t.Fatalf("id changed on rename: %s -> %s", conventions.ID, moved.ID)
	}
	if got := mustPath(t, s, ctx, conventions.ID); got != "estate/conventions-and-style" {
		t.Errorf("new path = %q", got)
	}

	p, redirected, err := s.ResolvePath(ctx, "estate/conventions")
	if err != nil {
		t.Fatalf("ResolvePath(old): %v", err)
	}
	if !redirected {
		t.Error("old path resolved without reporting a redirect")
	}
	if p.ID != conventions.ID {
		t.Errorf("old path resolved to %s, want %s", p.ID, conventions.ID)
	}
}

// THE ONE THAT MATTERS. Moving a page moves everything under it, so the old
// path of every DESCENDANT has to redirect too — otherwise the rows survive
// and are unreachable by the only address anybody had for them, which is the
// orphaning this ticket is about in its least visible form.
func TestMoveRedirectsEveryDescendant(t *testing.T) {
	s, ctx := newTestStore(t)
	_, conventions, naming := tree(t, s, ctx)
	personal := mkPage(t, s, ctx, nil, "personal")

	if _, err := s.MovePage(ctx, conventions.ID, &personal.ID, "conventions"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	if got := mustPath(t, s, ctx, naming.ID); got != "personal/conventions/naming" {
		t.Fatalf("descendant path = %q, want personal/conventions/naming", got)
	}

	// The descendant's OLD path resolves to the DESCENDANT, not to the page
	// that was named in the move.
	p, redirected, err := s.ResolvePath(ctx, "estate/conventions/naming")
	if err != nil {
		t.Fatalf("ResolvePath(old descendant): %v", err)
	}
	if !redirected || p.ID != naming.ID {
		t.Errorf("old descendant path -> %s (redirected=%v), want %s", p.ID, redirected, naming.ID)
	}

	// And the moved page's own old path resolves to it.
	p, _, err = s.ResolvePath(ctx, "estate/conventions")
	if err != nil {
		t.Fatalf("ResolvePath(old parent): %v", err)
	}
	if p.ID != conventions.ID {
		t.Errorf("old parent path -> %s, want %s", p.ID, conventions.ID)
	}
}

// A path that becomes live again must not still be a redirect, or two things
// answer for one string and which wins depends on query order.
func TestALivePathIsNeverAlsoARedirect(t *testing.T) {
	s, ctx := newTestStore(t)
	estate, conventions, _ := tree(t, s, ctx)

	// Vacate estate/conventions...
	if _, err := s.MovePage(ctx, conventions.ID, &estate.ID, "conventions-old"); err != nil {
		t.Fatalf("MovePage(vacate): %v", err)
	}
	// ...then move a different page into the path it left behind.
	replacement := mkPage(t, s, ctx, nil, "replacement")
	if _, err := s.MovePage(ctx, replacement.ID, &estate.ID, "conventions"); err != nil {
		t.Fatalf("MovePage(occupy): %v", err)
	}

	p, redirected, err := s.ResolvePath(ctx, "estate/conventions")
	if err != nil {
		t.Fatalf("ResolvePath: %v", err)
	}
	if redirected {
		t.Error("a live path resolved through a redirect")
	}
	if p.ID != replacement.ID {
		t.Errorf("estate/conventions -> %s, want the replacement %s", p.ID, replacement.ID)
	}

	for _, r := range mustRedirects(t, s, ctx, conventions.ID) {
		if r == "estate/conventions" {
			t.Error("the vacated path is still recorded as a redirect to the page that left it")
		}
	}
}

func mustRedirects(t *testing.T, s *Store, ctx context.Context, id uuid.UUID) []string {
	t.Helper()
	r, err := s.RedirectsFor(ctx, id)
	if err != nil {
		t.Fatalf("RedirectsFor: %v", err)
	}
	return r
}

// A cycle leaves every row valid and the whole subtree unreachable from any
// root, which is why it is refused by the database rather than by the caller.
func TestACycleIsRefused(t *testing.T) {
	s, ctx := newTestStore(t)
	estate, conventions, naming := tree(t, s, ctx)

	// A page under its own descendant.
	if _, err := s.MovePage(ctx, estate.ID, &naming.ID, "estate"); !errors.Is(err, ErrPageCycle) {
		t.Errorf("ancestor cycle err = %v, want ErrPageCycle", err)
	}
	// A page under itself.
	if _, err := s.MovePage(ctx, conventions.ID, &conventions.ID, "conventions"); !errors.Is(err, ErrPageCycle) {
		t.Errorf("self-parent err = %v, want ErrPageCycle", err)
	}
	// The tree is unchanged by the refusals.
	if got := mustPath(t, s, ctx, naming.ID); got != "estate/conventions/naming" {
		t.Errorf("tree changed after a refused move: %q", got)
	}
}

// The guard, not the Go: identity is immutable for anything holding a
// connection, which is the reason it is a trigger.
func TestAPageIDIsImmutableInTheDatabase(t *testing.T) {
	s, ctx := newTestStore(t)
	_, _, naming := tree(t, s, ctx)

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier2.pages SET id = gen_random_uuid() WHERE id = $1`, naming.ID)
	if got := sqlState(err); got != pgPageIDImmutable {
		t.Errorf("SQLSTATE = %q (err %v), want %s", got, err, pgPageIDImmutable)
	}
}

func TestListPagePathsIsTheDestinationVocabulary(t *testing.T) {
	s, ctx := newTestStore(t)
	tree(t, s, ctx)
	mkPage(t, s, ctx, nil, "personal")

	got, err := s.ListPagePaths(ctx)
	if err != nil {
		t.Fatalf("ListPagePaths: %v", err)
	}
	want := []string{"estate", "estate/conventions", "estate/conventions/naming", "personal"}
	if len(got) != len(want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("paths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestChildPagesListsOneLevel(t *testing.T) {
	s, ctx := newTestStore(t)
	estate, conventions, _ := tree(t, s, ctx)
	mkPage(t, s, ctx, &estate.ID, "adr")

	kids, err := s.ChildPages(ctx, &estate.ID)
	if err != nil {
		t.Fatalf("ChildPages: %v", err)
	}
	if len(kids) != 2 || kids[0].Slug != "adr" || kids[1].Slug != "conventions" {
		t.Fatalf("children = %+v, want adr then conventions", kids)
	}

	roots, err := s.ChildPages(ctx, nil)
	if err != nil {
		t.Fatalf("ChildPages(root): %v", err)
	}
	if len(roots) != 1 || roots[0].Slug != "estate" {
		t.Errorf("roots = %+v, want one estate", roots)
	}

	// A leaf has no children, and that is an empty slice rather than an error.
	leaves, err := s.ChildPages(ctx, &conventions.ID)
	if err != nil || len(leaves) != 1 {
		t.Errorf("ChildPages(conventions) = %+v, %v", leaves, err)
	}
}

// A page still holding children cannot be deleted out from under them. There is
// no DeletePage in the store, so this asserts the database's answer directly —
// the guarantee CHRN-38's notes will rest on.
func TestAPageWithChildrenCannotBeDeleted(t *testing.T) {
	s, ctx := newTestStore(t)
	estate, _, _ := tree(t, s, ctx)

	_, err := s.Pool().Exec(ctx, `DELETE FROM tier2.pages WHERE id = $1`, estate.ID)
	if got := sqlState(err); got != pgForeignKeyViolation {
		t.Errorf("SQLSTATE = %q (err %v), want %s", got, err, pgForeignKeyViolation)
	}
}

// A move that changes nothing is not an error — a caller need not diff first.
func TestAMoveToWhereItAlreadyIsSucceedsAndLeavesNoRedirect(t *testing.T) {
	s, ctx := newTestStore(t)
	_, conventions, _ := tree(t, s, ctx)

	if _, err := s.MovePage(ctx, conventions.ID, conventions.ParentID, "conventions"); err != nil {
		t.Fatalf("no-op move: %v", err)
	}
	if r := mustRedirects(t, s, ctx, conventions.ID); len(r) != 0 {
		t.Errorf("no-op move left redirects: %v", r)
	}
	if got := mustPath(t, s, ctx, conventions.ID); got != "estate/conventions" {
		t.Errorf("path after no-op move = %q", got)
	}
}
