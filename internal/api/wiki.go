package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// E5's store, reachable (CHRN-98): pages, notes with their revisions, and
// search.
//
// ============================================================================
// THREE THINGS ARE DECIDED UPSTREAM AND ONLY TRANSCRIBED HERE.
// ============================================================================
//
// CHRN-97 ruling 3: a note payload carries the reference DESCRIPTORS its text
// produced and no upstream state. This file dials nothing — not Switchyard,
// not Amber — which is what lets a note's ETag mean what it says and keeps a
// slow upstream out of a note's latency. Cards are POST /references/resolve.
//
// Ruling 4: a soft-deleted note answers 410 with a tombstone — ref, deleted_at,
// deleted_by — and no title and no body. Withdrawn is a different fact from
// never existed, and CHRN-39 built a journal to keep the difference.
//
// Ruling 5: a read carries an ETag of its current revision id and honours
// If-None-Match; an append never carries If-Match, because nothing is
// overwritten and there is no lost update to prevent. Two people appending
// from one base produce two revisions, both in history, and the response says
// which one this append followed so E8 can render the gap.
//
// ============================================================================
// THE ONE CALLER OF THE MISS FEED IS HERE.
// ============================================================================
//
// The note handler is what SCANS, so the note handler is what calls
// Keys.NoteMisses — once per render, with the keys and nothing from the note's
// text. It is the only caller: the resolve endpoint parses tokens and holds no
// key set (references.go), so one dead key in one note logs exactly once per
// page render and resolve.logCounts keeps counting something.
//
// ============================================================================
// NO NEW STORE BEHAVIOUR, AND TWO PLACES THAT COST SOMETHING.
// ============================================================================
//
// The description forbids growing internal/store from here, and the plan's
// furniture requires cursor pagination. The store reads whole lists, so the
// cursor is honoured by windowing the list by number or seq after the read:
// correct semantics on an append-only table, and a keyset query is the thing
// to add when a page holds enough notes for it to matter. Likewise a listing
// needs each note's title and the note row carries none, so a page of N notes
// costs N revision reads. Both are raised on the ticket rather than fixed by
// widening a package that is not this ticket's.

// Wiki is the slice of the store this surface needs: pages, notes, revisions,
// the provenance link CHRN-46 left readable, and search. An interface so the
// handlers can be tested without Postgres; *store.Store satisfies it.
type Wiki interface {
	CreatePage(ctx context.Context, parent *uuid.UUID, slug string) (store.Page, error)
	PageByPath(ctx context.Context, path string) (store.Page, error)
	ResolvePath(ctx context.Context, path string) (store.Page, bool, error)
	PagePath(ctx context.Context, id uuid.UUID) (string, error)
	ListPagePaths(ctx context.Context) ([]string, error)

	CreateNote(ctx context.Context, in store.NewNote) (store.Note, store.NoteRevision, error)
	AppendRevision(ctx context.Context, noteID uuid.UUID, in store.NewRevision) (store.NoteRevision, error)
	NoteByNumber(ctx context.Context, number int64) (store.Note, error)
	NoteByID(ctx context.Context, id uuid.UUID) (store.Note, error)
	CurrentRevision(ctx context.Context, noteID uuid.UUID) (store.NoteRevision, error)
	NoteRevisions(ctx context.Context, noteID uuid.UUID) ([]store.NoteRevision, error)
	NotesOnPage(ctx context.Context, pageID uuid.UUID) ([]store.Note, error)
	DiscussionsResolvedInto(ctx context.Context, noteID uuid.UUID) ([]store.Discussion, error)
	Backlinks(ctx context.Context, number int64) ([]store.Backlink, error)

	Search(ctx context.Context, query string, limit int) ([]store.SearchHit, error)
}

// Bounds. Every one is a number somebody chose.
const (
	// maxPageBody bounds a page creation: a path.
	maxPageBody = 4 << 10
	// maxNoteBody bounds a note or a revision. A long note is tens of
	// kilobytes; a megabyte is a file somebody pasted, and the place for a file
	// is not a revision row.
	maxNoteBody = 1 << 20

	defaultListLimit = 50
	maxListLimit     = 200
	defaultSearch    = 50
	maxSearch        = 100

	wikiTimeout = 15 * time.Second
)

// wikiUnavailable answers a router assembled with no notes store behind it —
// guarded()'s shape for a nil Accounts, for the same reason: serve always
// supplies one, and a nil here must be a refusal rather than a dereference.
func (a *api) wikiUnavailable(w http.ResponseWriter) bool {
	if a.wiki != nil {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, codeWikiUnconfigured,
		"this router was assembled with no notes store behind it")
	return true
}

// clampLimit applies a default and a cap to a bound `limit`. The declared
// `minimum: 1` is not enforced by the generated binder — it binds types, not
// constraints — so a non-positive value is refused here. See limitOr.
func clampLimit(w http.ResponseWriter, v *wire.Limit, def, maximum int) (int, bool) {
	if v == nil {
		return def, true
	}
	if *v <= 0 {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "limit must be a positive integer")
		return 0, false
	}
	return min(*v, maximum), true
}

// cursorAfter reads a cursor as the position the previous page ended on.
// Opaque to clients and a decimal here, because every list this surface pages
// is keyed by an integer that only ever grows.
func cursorAfter(w http.ResponseWriter, c *wire.Cursor) (int64, bool) {
	if c == nil || *c == "" {
		return 0, true
	}
	n, err := strconv.ParseInt(*c, 10, 64)
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "cursor is not one this list issued")
		return 0, false
	}
	return n, true
}

// window pages an append-only list by an integer key: everything after
// `after`, at most `limit`, and the cursor for the next page when there is
// one. Correct because the key never repeats and never decreases — a note
// number is minted once, a revision seq only grows.
func window[T any](items []T, key func(T) int64, after int64, limit int) ([]T, *string) {
	out := make([]T, 0, limit)
	for _, it := range items {
		if key(it) <= after {
			continue
		}
		if len(out) == limit {
			next := strconv.FormatInt(key(out[len(out)-1]), 10)
			return out, &next
		}
		out = append(out, it)
	}
	return out, nil
}

// noteRef parses the {ref} of a note route, leniently as the store does, and
// refuses what is not a note reference with the flat envelope bindError uses.
func noteRef(w http.ResponseWriter, ref string) (int64, bool) {
	n, err := store.ParseNoteRef(ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "ref is not a note reference")
		return 0, false
	}
	return n, true
}

// requirePerson refuses an agent session before the round trip, with the same
// answer the store's guard gives after it. The store is the enforcement — CH041
// refuses a confirmer that is an agent whatever this says — and this is the
// message: a caller deserves to be told which mistake it made rather than
// handed a constraint name from a rolled-back transaction.
func requirePerson(w http.ResponseWriter, u store.User) bool {
	if u.Kind == store.KindAgent {
		writeError(w, http.StatusForbidden, codePersonRequired,
			"authored text needs a confirming person, and this session belongs to an agent")
		return false
	}
	return true
}

// etagFor is the ETag of a revision: its id, quoted, as RFC 9110 has it.
func etagFor(rev store.NoteRevision) string { return `"` + rev.ID.String() + `"` }

// etagMatches reports whether an If-None-Match names this revision. Weak
// validators are accepted as equal — a revision id is a strong validator, and
// a client that weakened it is not asking a different question.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(candidate), "W/"))
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

// ── pages ───────────────────────────────────────────────────────────────────

func (a *api) ListPages(w http.ResponseWriter, r *http.Request) {
	if a.wikiUnavailable(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	paths, err := a.wiki.ListPagePaths(ctx)
	if err != nil {
		a.serverError(w, r, "list pages", err)
		return
	}
	writeJSON(w, http.StatusOK, wire.PageTree{Paths: paths})
}

func (a *api) CreatePage(w http.ResponseWriter, r *http.Request) {
	if a.wikiUnavailable(w) {
		return
	}
	var req wire.NewPageRequest
	if !decodeJSONLimit(w, r, &req, maxPageBody) {
		return
	}
	segs, err := store.SplitPath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidBody,
			"path must be lowercase alphanumeric words joined by single hyphens, separated by single slashes")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	// THE PARENT IS RESOLVED WITHOUT FOLLOWING REDIRECTS. A caller writing to
	// a path needs to know it is writing where it thinks it is; a redirect
	// followed here would file a new page under a parent that has moved.
	var parent *uuid.UUID
	if len(segs) > 1 {
		p, err := a.wiki.PageByPath(ctx, strings.Join(segs[:len(segs)-1], "/"))
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, codeNotFound, "the parent path names no page")
			return
		case err != nil:
			a.serverError(w, r, "create page: parent", err)
			return
		}
		parent = &p.ID
	}

	p, err := a.wiki.CreatePage(ctx, parent, segs[len(segs)-1])
	switch {
	case errors.Is(err, store.ErrSiblingSlug):
		writeError(w, http.StatusConflict, codeSlugTaken, "a sibling page already uses that slug")
		return
	case errors.Is(err, store.ErrInvalidSlug):
		writeError(w, http.StatusBadRequest, codeInvalidBody, "the slug is not valid")
		return
	case err != nil:
		a.serverError(w, r, "create page", err)
		return
	}
	writeJSON(w, http.StatusCreated, toPage(p, req.Path))
}

// ── notes ───────────────────────────────────────────────────────────────────

func (a *api) ListNotes(w http.ResponseWriter, r *http.Request, params wire.ListNotesParams) {
	if a.wikiUnavailable(w) {
		return
	}
	limit, ok := clampLimit(w, params.Limit, defaultListLimit, maxListLimit)
	if !ok {
		return
	}
	after, ok := cursorAfter(w, params.Cursor)
	if !ok {
		return
	}
	if _, err := store.SplitPath(params.Page); err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "page is not a page path")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	// A REDIRECT IS FOLLOWED, and said so. A client holding an old path still
	// gets the page; moved_from tells it the path is no longer the page's own.
	page, redirected, err := a.wiki.ResolvePath(ctx, params.Page)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no page at that path")
		return
	case err != nil:
		a.serverError(w, r, "list notes: page", err)
		return
	}
	path := params.Page
	if redirected {
		if path, err = a.wiki.PagePath(ctx, page.ID); err != nil {
			a.serverError(w, r, "list notes: page path", err)
			return
		}
	}

	notes, err := a.wiki.NotesOnPage(ctx, page.ID)
	if err != nil {
		a.serverError(w, r, "list notes", err)
		return
	}
	pageOf, next := window(notes, func(n store.Note) int64 { return n.Number }, after, limit)

	items := make([]wire.NoteSummary, 0, len(pageOf))
	for _, n := range pageOf {
		rev, err := a.wiki.CurrentRevision(ctx, n.ID)
		if err != nil {
			a.serverError(w, r, "list notes: revision", err)
			return
		}
		items = append(items, toNoteSummary(n, rev, path))
	}

	out := wire.NoteList{Page: toPage(page, path), Items: items, NextCursor: next}
	if redirected {
		out.MovedFrom = &params.Page
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) CreateNote(w http.ResponseWriter, r *http.Request) {
	if a.wikiUnavailable(w) {
		return
	}
	var req wire.NewNoteRequest
	if !decodeJSONLimit(w, r, &req, maxNoteBody) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		writeError(w, http.StatusBadRequest, codeMissingField, "title is required")
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, codeMissingField, "body is required")
		return
	}
	u := userFrom(r.Context())
	if !requirePerson(w, u) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	// No redirect here either: a note filed on a path that has moved would
	// land on the old page's successor without the writer knowing.
	page, err := a.wiki.PageByPath(ctx, req.Page)
	switch {
	case errors.Is(err, store.ErrInvalidSlug):
		// Malformed is not missing. SplitPath's own comment says answering
		// "not found" for a request that is really malformed is precisely
		// what it exists to prevent, and listNotes already answers 400.
		writeError(w, http.StatusBadRequest, codeInvalidBody, "page is not a page path")
		return
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "the page path names no page")
		return
	case err != nil:
		a.serverError(w, r, "create note: page", err)
		return
	}

	// THE SESSION'S ACCOUNT IS BOTH AUTHOR AND CONFIRMER. Somebody typing a
	// note is agreeing to it; there is no verb, because nothing was proposed.
	n, rev, err := a.wiki.CreateNote(ctx, store.NewNote{
		PageID:      page.ID,
		AuthorID:    u.ID,
		ConfirmedBy: u.ID,
		Title:       req.Title,
		Body:        req.Body,
	})
	if err != nil {
		a.noteWriteError(w, r, "create note", err, nil)
		return
	}
	view, err := a.noteView(ctx, n, rev)
	if err != nil {
		a.serverError(w, r, "create note: view", err)
		return
	}
	w.Header().Set("ETag", etagFor(rev))
	writeJSON(w, http.StatusCreated, view)
}

func (a *api) GetNote(w http.ResponseWriter, r *http.Request, ref string, params wire.GetNoteParams) {
	if a.wikiUnavailable(w) {
		return
	}
	number, ok := noteRef(w, ref)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	n, ok := a.liveNote(w, r, ctx, number)
	if !ok {
		return
	}
	rev, err := a.wiki.CurrentRevision(ctx, n.ID)
	if err != nil {
		a.serverError(w, r, "get note: revision", err)
		return
	}

	// THE ETAG IS THE REVISION ID, and it can be compared before anything is
	// rendered: nothing in the payload changes unless a revision is appended,
	// because the payload carries no upstream state (ruling 3). A 304 costs
	// three primary-key reads and no render, no scan and no miss.
	etag := etagFor(rev)
	w.Header().Set("ETag", etag)
	if params.IfNoneMatch != nil && etagMatches(*params.IfNoneMatch, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	view, err := a.noteView(ctx, n, rev)
	if err != nil {
		a.serverError(w, r, "get note: view", err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (a *api) ListNoteRevisions(w http.ResponseWriter, r *http.Request, ref string, params wire.ListNoteRevisionsParams) {
	if a.wikiUnavailable(w) {
		return
	}
	number, ok := noteRef(w, ref)
	if !ok {
		return
	}
	limit, ok := clampLimit(w, params.Limit, defaultListLimit, maxListLimit)
	if !ok {
		return
	}
	after, ok := cursorAfter(w, params.Cursor)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	n, ok := a.liveNote(w, r, ctx, number)
	if !ok {
		return
	}
	revs, err := a.wiki.NoteRevisions(ctx, n.ID)
	if err != nil {
		a.serverError(w, r, "list revisions", err)
		return
	}
	pageOf, next := window(revs, func(rv store.NoteRevision) int64 { return int64(rv.Seq) }, after, limit)
	items := make([]wire.Revision, 0, len(pageOf))
	for _, rv := range pageOf {
		items = append(items, toRevision(rv))
	}
	writeJSON(w, http.StatusOK, wire.RevisionList{Items: items, NextCursor: next})
}

func (a *api) AppendRevision(w http.ResponseWriter, r *http.Request, ref string) {
	if a.wikiUnavailable(w) {
		return
	}
	number, ok := noteRef(w, ref)
	if !ok {
		return
	}
	var req wire.AppendRevisionRequest
	if !decodeJSONLimit(w, r, &req, maxNoteBody) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, codeMissingField, "body is required")
		return
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) == "" {
		writeError(w, http.StatusBadRequest, codeMissingField, "a title, when given, cannot be blank")
		return
	}
	u := userFrom(r.Context())
	if !requirePerson(w, u) {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	n, ok := a.liveNote(w, r, ctx, number)
	if !ok {
		return
	}

	// WHAT THIS APPEND FOLLOWED: the revision current when the request was
	// read. Not a precondition (ruling 5 — nothing is overwritten, so there
	// is nothing to refuse) but an answer: a gap between followed.seq and the
	// new seq means somebody else appended in between, and E8 renders that
	// rather than the server refusing it.
	followed, err := a.wiki.CurrentRevision(ctx, n.ID)
	if err != nil {
		a.serverError(w, r, "append revision: current", err)
		return
	}
	title := followed.Title
	if req.Title != nil {
		title = *req.Title
	}

	rev, err := a.wiki.AppendRevision(ctx, n.ID, store.NewRevision{
		AuthorID:    u.ID,
		ConfirmedBy: u.ID,
		Title:       title,
		Body:        req.Body,
	})
	if err != nil {
		a.noteWriteError(w, r, "append revision", err, &n)
		return
	}
	w.Header().Set("ETag", etagFor(rev))
	writeJSON(w, http.StatusCreated, wire.AppendResult{
		Revision: toRevisionMeta(rev),
		Followed: wire.RevisionPointer{Id: followed.ID, Seq: followed.Seq},
	})
}

// ListNoteBacklinks answers what links here (CHRN-105).
//
// A SIBLING OF GetNote, NOT A FIELD ON IT, because of the ETag. Ruling 5 makes
// a note's ETag its revision id, and that is honest only while nothing in the
// payload changes unless a revision is appended. Backlinks change when OTHER
// notes change: fold them in and an unchanged note answers 304 over a stale
// link list, or the ETag has to become a hash of the whole graph. So they live
// here, without a validator.
//
// NOTHING HERE RUNS ON THE TIER-1 POOL. The row is tier 1 — tier1.note_links
// is derived from the text and RebuildNoteLinks regenerates it — but the
// resolution to a ref and a title joins tier2.notes and tier2.note_revisions,
// which chronicle_tier1 may not read (the decision on CHRN-100, 2026-09-14,
// and TestTheTierOneRoleCannotResolveBacklinks in internal/store). The read
// is tier 2's, over the same Wiki the rest of this group holds; the payload
// carries the marking because the LIST is derived even though every note in
// it is authored.
//
// Each row costs a page-path read, as the listing's title does; the cursor
// windows the store's whole-list read by source number. Both are the trade
// CHRN-98 made and raised, and this route makes it no worse.
func (a *api) ListNoteBacklinks(w http.ResponseWriter, r *http.Request, ref string, params wire.ListNoteBacklinksParams) {
	if a.wikiUnavailable(w) {
		return
	}
	number, ok := noteRef(w, ref)
	if !ok {
		return
	}
	limit, ok := clampLimit(w, params.Limit, defaultListLimit, maxListLimit)
	if !ok {
		return
	}
	after, ok := cursorAfter(w, params.Cursor)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	if _, ok := a.liveNote(w, r, ctx, number); !ok {
		return
	}
	links, err := a.wiki.Backlinks(ctx, number)
	if err != nil {
		a.serverError(w, r, "list backlinks", err)
		return
	}
	pageOf, next := window(links, func(b store.Backlink) int64 { return b.Number }, after, limit)
	items := make([]wire.Backlink, 0, len(pageOf))
	for _, b := range pageOf {
		path, err := a.wiki.PagePath(ctx, b.PageID)
		if err != nil {
			a.serverError(w, r, "list backlinks: page path", err)
			return
		}
		items = append(items, toBacklink(b, path))
	}
	writeJSON(w, http.StatusOK, wire.BacklinkList{
		Items:      items,
		NextCursor: next,
		Generated:  generatedByChronicle(),
	})
}

// liveNote reads a note by number and answers 404 or the 410 tombstone itself,
// so the four routes under /notes/{ref} cannot disagree about a deleted note.
func (a *api) liveNote(w http.ResponseWriter, r *http.Request, ctx context.Context, number int64) (store.Note, bool) {
	n, err := a.wiki.NoteByNumber(ctx, number)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such note")
		return store.Note{}, false
	case err != nil:
		a.serverError(w, r, "note by number", err)
		return store.Note{}, false
	}
	if n.Deleted() {
		writeTombstone(w, n)
		return store.Note{}, false
	}
	return n, true
}

// writeTombstone is ruling 4: 410, the ref, when and by whom — and nothing
// else. The title is authored text and stays withheld with the body.
func writeTombstone(w http.ResponseWriter, n store.Note) {
	writeJSON(w, http.StatusGone, wire.NoteTombstone{
		Ref:       n.Ref(),
		DeletedAt: *n.DeletedAt,
		DeletedBy: *n.DeletedBy,
	})
}

// noteWriteError maps the store's refusals on a note write. The guard is the
// store's; this is the translation.
func (a *api) noteWriteError(w http.ResponseWriter, r *http.Request, what string, err error, n *store.Note) {
	switch {
	case errors.Is(err, store.ErrConfirmerRequired):
		// CH041 said no after requirePerson said yes — the account's kind
		// changed under the request, or a caller bypassed the check. The
		// store is the enforcement; this is the same answer, later.
		writeError(w, http.StatusForbidden, codePersonRequired,
			"authored text needs a confirming person")
	case errors.Is(err, store.ErrNoteDeleted) && n != nil:
		// Deleted between liveNote's read and the write. The tombstone is
		// the answer liveNote would have given a moment later; read the row
		// again so the pair it carries is the recorded one.
		if fresh, rerr := a.wiki.NoteByNumber(r.Context(), n.Number); rerr == nil && fresh.Deleted() {
			writeTombstone(w, fresh)
			return
		}
		a.serverError(w, r, what, err)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such note")
	default:
		a.serverError(w, r, what, err)
	}
}

// render turns one authored body into what a payload carries: the HTML, and
// the references the text names as descriptors.
//
// THIS IS THE SCAN, AND SO THIS IS THE MISS FEED'S ONE CALL SITE. Render and
// Scan share the grammar; UnknownKeys are the well-shaped keys the live
// project set rejected, and they go to Keys.NoteMisses with nothing else — not
// the body, not a token's surrounding prose. A nil key set (no tracker
// configured) has nothing to report to, and every ticket-shaped token was
// already prose. Notes and discussion turns both come through here, which is
// what keeps "one caller" a fact about the code rather than a discipline.
func (a *api) render(body string) (string, []wire.ReferenceDescriptor, error) {
	html, err := a.renderer.Render([]byte(body))
	if err != nil {
		return "", nil, fmt.Errorf("render: %w", err)
	}
	scan := a.renderer.Scan([]byte(body))
	if a.keys != nil {
		a.keys.NoteMisses(scan.UnknownKeys)
	}
	refs := make([]wire.ReferenceDescriptor, 0, len(scan.References))
	for _, ref := range scan.References {
		refs = append(refs, toDescriptor(ref))
	}
	return string(html), refs, nil
}

// noteView builds the payload for one note: the current text rendered, the
// references its text names, and the threads that concluded into it.
func (a *api) noteView(ctx context.Context, n store.Note, rev store.NoteRevision) (wire.Note, error) {
	path, err := a.wiki.PagePath(ctx, n.PageID)
	if err != nil {
		return wire.Note{}, fmt.Errorf("page path: %w", err)
	}
	html, refs, err := a.render(rev.Body)
	if err != nil {
		return wire.Note{}, err
	}
	threads, err := a.wiki.DiscussionsResolvedInto(ctx, n.ID)
	if err != nil {
		return wire.Note{}, fmt.Errorf("resolved from: %w", err)
	}
	return wire.Note{
		Ref:          n.Ref(),
		Title:        rev.Title,
		Page:         path,
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		Body:         rev.Body,
		Html:         html,
		References:   refs,
		Revision:     toRevisionMeta(rev),
		ResolvedFrom: toDiscussionSummaries(threads),
	}, nil
}

// ── search ──────────────────────────────────────────────────────────────────

func (a *api) Search(w http.ResponseWriter, r *http.Request, params wire.SearchParams) {
	if a.wikiUnavailable(w) {
		return
	}
	// minLength: 1 is declared and not enforced by the binder, and a query of
	// whitespace is the same empty question.
	q := strings.TrimSpace(params.Q)
	if q == "" {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "q must carry a query")
		return
	}
	limit, ok := clampLimit(w, params.Limit, defaultSearch, maxSearch)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	hits, err := a.wiki.Search(ctx, q, limit)
	switch {
	case errors.Is(err, store.ErrEmptyQuery):
		// Pure punctuation parses to an empty tsquery. An empty question,
		// reported as one rather than as an empty corpus.
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "q has no searchable terms")
		return
	case err != nil:
		a.serverError(w, r, "search", err)
		return
	}
	writeJSON(w, http.StatusOK, wire.SearchResults{Query: q, Limit: limit, Items: toSearchHits(hits)})
}

// The pure-path renderer, for a router given no key set: CHR, DSC and amber1
// still mark, and every ticket-shaped token stays prose (CHRN-48 ruling 3).
func newRenderer(keys markdown.ProjectKeys) *markdown.Renderer { return markdown.NewRenderer(keys) }
