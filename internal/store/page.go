package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CHRN-37 — the page tree. Pages are addressed by path
// (`estate/conventions/naming`) and identified by UUID, and those are two
// different things on purpose: the path is where a page currently is, the id
// is what it is. A move changes the first and never the second, which is what
// lets CHRN-38's notes and CHRN-42's backlinks survive one.
//
// THE PATH IS DERIVED, NEVER STORED. See 0010_pages.up.sql for the argument;
// the short version is that a materialised path is a second representation of
// parent_id and slug, and the move that has to keep them in step is exactly
// the operation this ticket exists to get right.

// ErrPageCycle is returned when a move would make a page its own ancestor.
// The subtree would still be a valid set of rows and would be unreachable from
// every root, so this is refused rather than repaired.
var ErrPageCycle = errors.New("store: a page may not be its own ancestor")

// ErrSiblingSlug is returned when a create or a move would put two pages with
// the same slug under one parent — two rows claiming one path.
var ErrSiblingSlug = errors.New("store: a sibling page already uses that slug")

// ErrInvalidSlug is returned for a slug the schema's CHECK would refuse. It is
// reported before the round trip so the caller gets the rule rather than a
// SQLSTATE.
var ErrInvalidSlug = errors.New("store: a slug must be lowercase alphanumeric words joined by single hyphens")

// Page-guard SQLSTATEs raised by tier2.pages_guard.
const (
	pgPageCycle       = "CH050"
	pgPageIDImmutable = "CH051"
)

// maxPathSegments bounds a path the same way the guard bounds ancestry, so a
// pathological input is refused before it becomes a query.
const maxPathSegments = 64

// slugPattern mirrors the CHECK constraint in 0010 exactly. Two copies of one
// rule is worth it here: the database's is the one that holds for every
// connection, and this one turns a violation into ErrInvalidSlug naming the
// rule instead of a 23514 naming a constraint.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Page is one node of the tree. It carries no path, because it does not have
// one on its own — a path is a fact about a page's ancestry, and PagePath is
// what computes it.
type Page struct {
	ID        uuid.UUID
	ParentID  *uuid.UUID
	Slug      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// pathCTE walks DOWN from every root, building each page's full path as it
// goes. Used wherever a path has to be produced or matched.
//
// Recursion terminates on the tree structure, which tier2.pages_guard
// guarantees is acyclic — that guard is what makes this query safe to run
// without a depth cap.
const pathCTE = `
WITH RECURSIVE tree AS (
    SELECT id, slug::text AS path
      FROM tier2.pages
     WHERE parent_id IS NULL
    UNION ALL
    SELECT p.id, tree.path || '/' || p.slug
      FROM tier2.pages p
      JOIN tree ON p.parent_id = tree.id
)`

// ValidateSlug reports whether s is one legal path segment.
func ValidateSlug(s string) error {
	if !slugPattern.MatchString(s) {
		return fmt.Errorf("%w: %q", ErrInvalidSlug, s)
	}
	return nil
}

// SplitPath breaks `estate/conventions/naming` into its segments and checks
// each one. It is deliberately strict about the shapes that look harmless and
// are not: a leading or trailing slash, a doubled slash, or an empty path all
// produce a segment that no slug can equal, so accepting them would mean
// resolving a path that can never match anything and reporting "not found" for
// what is really a malformed request.
func SplitPath(path string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: the empty path names no page", ErrInvalidSlug)
	}
	segs := strings.Split(path, "/")
	if len(segs) > maxPathSegments {
		return nil, fmt.Errorf("%w: a path may not exceed %d segments", ErrInvalidSlug, maxPathSegments)
	}
	for _, s := range segs {
		if err := ValidateSlug(s); err != nil {
			return nil, err
		}
	}
	return segs, nil
}

// CreatePage adds a page under parent, or at the root when parent is nil.
func (s *Store) CreatePage(ctx context.Context, parent *uuid.UUID, slug string) (Page, error) {
	if err := ValidateSlug(slug); err != nil {
		return Page{}, err
	}
	const q = `
		INSERT INTO tier2.pages (parent_id, slug)
		VALUES ($1, $2)
		RETURNING id, parent_id, slug, created_at, updated_at`
	var p Page
	err := s.pool.QueryRow(ctx, q, parent, slug).
		Scan(&p.ID, &p.ParentID, &p.Slug, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Page{}, pageError(err)
	}
	return p, nil
}

// PageByID reads one page.
func (s *Store) PageByID(ctx context.Context, id uuid.UUID) (Page, error) {
	const q = `
		SELECT id, parent_id, slug, created_at, updated_at
		  FROM tier2.pages WHERE id = $1`
	var p Page
	err := s.pool.QueryRow(ctx, q, id).
		Scan(&p.ID, &p.ParentID, &p.Slug, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("store: page by id: %w", err)
	}
	return p, nil
}

// PagePath computes a page's current path by walking to the root.
func (s *Store) PagePath(ctx context.Context, id uuid.UUID) (string, error) {
	return pagePath(ctx, s.pool, id)
}

// pagePath is PagePath against any querier, so it can run inside MovePage's
// transaction and see that transaction's own uncommitted move.
func pagePath(ctx context.Context, q querier, id uuid.UUID) (string, error) {
	const sql = `
		WITH RECURSIVE up AS (
		    SELECT id, parent_id, slug, 0 AS depth
		      FROM tier2.pages WHERE id = $1
		    UNION ALL
		    SELECT p.id, p.parent_id, p.slug, up.depth + 1
		      FROM tier2.pages p JOIN up ON p.id = up.parent_id
		)
		SELECT string_agg(slug, '/' ORDER BY depth DESC) FROM up`
	var path *string
	if err := q.QueryRow(ctx, sql, id).Scan(&path); err != nil {
		return "", fmt.Errorf("store: page path: %w", err)
	}
	if path == nil {
		return "", ErrNotFound
	}
	return *path, nil
}

// PageByPath resolves a live path. It does NOT follow redirects — ResolvePath
// is the one that does, and the two are separate because a caller writing to a
// path needs to know it is writing where it thinks it is.
func (s *Store) PageByPath(ctx context.Context, path string) (Page, error) {
	if _, err := SplitPath(path); err != nil {
		return Page{}, err
	}
	const q = pathCTE + `
		SELECT p.id, p.parent_id, p.slug, p.created_at, p.updated_at
		  FROM tree t JOIN tier2.pages p ON p.id = t.id
		 WHERE t.path = $1`
	var p Page
	err := s.pool.QueryRow(ctx, q, path).
		Scan(&p.ID, &p.ParentID, &p.Slug, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("store: page by path: %w", err)
	}
	return p, nil
}

// ResolvePath resolves a path, following a redirect if the live tree has no
// such page. The bool reports whether a redirect was used, so a caller can
// answer 301 rather than 200 and an operator can see that an old path is still
// in circulation.
//
// LIVE PAGES WIN. A path that is both a real page and a stale redirect
// resolves to the real page — MovePage keeps that case from arising, and the
// ordering here means a leftover row could never shadow a live page even if
// one did.
func (s *Store) ResolvePath(ctx context.Context, path string) (Page, bool, error) {
	p, err := s.PageByPath(ctx, path)
	switch {
	case err == nil:
		return p, false, nil
	case !errors.Is(err, ErrNotFound):
		return Page{}, false, err
	}

	var id uuid.UUID
	err = s.pool.QueryRow(ctx,
		`SELECT page_id FROM tier2.page_redirects WHERE from_path = $1`, path).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, false, ErrNotFound
	}
	if err != nil {
		return Page{}, false, fmt.Errorf("store: resolve redirect: %w", err)
	}
	p, err = s.PageByID(ctx, id)
	if err != nil {
		return Page{}, false, err
	}
	return p, true, nil
}

// ListPagePaths returns every page path, sorted. This is the destination
// vocabulary CHRN-30's prompt renders and CHRN-32's stage 2 validates against;
// see the note on the PR for why the catalogue is not wired to it here.
func (s *Store) ListPagePaths(ctx context.Context) ([]string, error) {
	const q = pathCTE + ` SELECT path FROM tree ORDER BY path`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("store: list page paths: %w", err)
	}
	defer rows.Close()
	paths := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("store: list page paths: %w", err)
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list page paths: %w", err)
	}
	return paths, nil
}

// ChildPages lists the direct children of parent, or the roots when nil.
func (s *Store) ChildPages(ctx context.Context, parent *uuid.UUID) ([]Page, error) {
	const q = `
		SELECT id, parent_id, slug, created_at, updated_at
		  FROM tier2.pages
		 WHERE parent_id IS NOT DISTINCT FROM $1
		 ORDER BY slug`
	rows, err := s.pool.Query(ctx, q, parent)
	if err != nil {
		return nil, fmt.Errorf("store: child pages: %w", err)
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.ParentID, &p.Slug, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: child pages: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: child pages: %w", err)
	}
	return out, nil
}

// MovePage renames or reparents a page, leaving a redirect behind for its old
// path and for the old path of every descendant.
//
// THE WHOLE SUBTREE MOVES, and that is why this is not an UPDATE the caller
// could have written. Moving `estate/conventions` changes the path of
// `estate/conventions/naming` too, and a redirect written only for the page
// that was named would leave every page under it unreachable by its old path
// — the orphaning the ticket's `Done when` is about, in the one form that
// leaves the rows themselves perfectly intact.
//
// Passing the page's current parent and slug is a no-op that still succeeds,
// so a caller need not diff before calling.
func (s *Store) MovePage(ctx context.Context, id uuid.UUID, newParent *uuid.UUID, newSlug string) (Page, error) {
	if err := ValidateSlug(newSlug); err != nil {
		return Page{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("store: move page: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the page for the duration. Two concurrent moves of one subtree would
	// otherwise interleave a path computation with the other's UPDATE and write
	// redirects for a tree that never existed.
	var locked uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM tier2.pages WHERE id = $1 FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("store: move page: lock: %w", err)
	}

	// The subtree, as offsets from the page being moved. These do not change:
	// the move reparents one page, and everything below keeps its shape. That
	// is what lets one prefix substitution produce every old and new path.
	rel, err := subtreeOffsets(ctx, tx, id)
	if err != nil {
		return Page{}, err
	}
	oldPrefix, err := pagePath(ctx, tx, id)
	if err != nil {
		return Page{}, err
	}

	const upd = `
		UPDATE tier2.pages SET parent_id = $2, slug = $3
		 WHERE id = $1
		RETURNING id, parent_id, slug, created_at, updated_at`
	var p Page
	err = tx.QueryRow(ctx, upd, id, newParent, newSlug).
		Scan(&p.ID, &p.ParentID, &p.Slug, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return Page{}, pageError(err)
	}

	newPrefix, err := pagePath(ctx, tx, id)
	if err != nil {
		return Page{}, err
	}

	for pageID, suffix := range rel {
		oldPath := joinPath(oldPrefix, suffix)
		newPath := joinPath(newPrefix, suffix)
		if oldPath == newPath {
			continue
		}
		// ON CONFLICT: a path may be vacated, reoccupied and vacated again, and
		// the newest occupant is the one an old link should reach.
		if _, err := tx.Exec(ctx, `
			INSERT INTO tier2.page_redirects (from_path, page_id)
			VALUES ($1, $2)
			ON CONFLICT (from_path) DO UPDATE SET page_id = EXCLUDED.page_id`,
			oldPath, pageID); err != nil {
			return Page{}, fmt.Errorf("store: move page: redirect: %w", err)
		}
		// A path that has just become live again must not also be a redirect,
		// or the tree and the redirect table both answer for one string.
		if _, err := tx.Exec(ctx,
			`DELETE FROM tier2.page_redirects WHERE from_path = $1`, newPath); err != nil {
			return Page{}, fmt.Errorf("store: move page: clear redirect: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Page{}, fmt.Errorf("store: move page: commit: %w", err)
	}
	return p, nil
}

// RedirectsFor lists the old paths that still resolve to a page, newest first.
func (s *Store) RedirectsFor(ctx context.Context, id uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT from_path FROM tier2.page_redirects
		 WHERE page_id = $1 ORDER BY created_at DESC, from_path`, id)
	if err != nil {
		return nil, fmt.Errorf("store: redirects for: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("store: redirects for: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: redirects for: %w", err)
	}
	return out, nil
}

// subtreeOffsets returns every page at or below root, keyed by id, with its
// path relative to root ("" for root itself).
func subtreeOffsets(ctx context.Context, q querier, root uuid.UUID) (map[uuid.UUID]string, error) {
	const sql = `
		WITH RECURSIVE sub AS (
		    SELECT id, ''::text AS rel FROM tier2.pages WHERE id = $1
		    UNION ALL
		    SELECT p.id,
		           CASE WHEN sub.rel = '' THEN p.slug ELSE sub.rel || '/' || p.slug END
		      FROM tier2.pages p JOIN sub ON p.parent_id = sub.id
		)
		SELECT id, rel FROM sub`
	rows, err := q.Query(ctx, sql, root)
	if err != nil {
		return nil, fmt.Errorf("store: subtree: %w", err)
	}
	defer rows.Close()
	out := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var rel string
		if err := rows.Scan(&id, &rel); err != nil {
			return nil, fmt.Errorf("store: subtree: %w", err)
		}
		out[id] = rel
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: subtree: %w", err)
	}
	return out, nil
}

// joinPath appends a relative offset to a prefix, where "" means the prefix.
func joinPath(prefix, rel string) string {
	if rel == "" {
		return prefix
	}
	return prefix + "/" + rel
}

// pageError maps the guard's SQLSTATEs and the sibling-slug unique indexes
// onto this package's sentinels, so callers match on a value rather than
// parsing a message.
func pageError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgPageCycle:
			return fmt.Errorf("%w: %v", ErrPageCycle, err)
		case pgUniqueViolation:
			return fmt.Errorf("%w: %v", ErrSiblingSlug, err)
		case pgPageIDImmutable:
			return fmt.Errorf("store: page id is immutable: %w", err)
		}
	}
	return fmt.Errorf("store: page: %w", err)
}
