package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// E6's store, reachable (CHRN-99), and where E6's exit is finally
// demonstrable: a human and an agent hold a threaded exchange over HTTP,
// unread is correct in both, and resolving a thread writes a link to the note
// it produced.
//
// ============================================================================
// THREE THINGS ARE THE STORE'S. THIS FILE IS NOT A SECOND PLACE THEY LIVE.
// ============================================================================
//
// ORDERING is `seq`, allocated under the thread's row lock (CHRN-43). The
// payload returns it and never sorts by a client timestamp; `composed_at` is
// carried as the advisory claim it is.
//
// READ MARKERS are server-side and monotonic (CHRN-45). A client REPORTS the
// position it has read through — MarkRead clamps it forward-only and never
// past the thread — and never computes a count. Unread is max(seq) minus the
// marker, computed by the store, so two sessions cannot disagree about it.
//
// THE LOOP is CH091: an agent turn requires the immediately preceding turn to
// be a person's, which also means an agent cannot open a thread. And CH093: a
// resolved thread takes no more turns. Both are refused by the store and
// REPORTED here — this surface does not duplicate the rule and does not get to
// bypass it. CHRN-67's MCP write tools are the second caller by design, and a
// rule that lived here would be one they do not inherit.
//
// ============================================================================
// AN AGENT PARTICIPATES THROUGH THE SAME DOOR AS A PERSON.
// ============================================================================
//
// An agent account holding a session posts a turn through POST /turns like
// anybody else, and the store derives author_kind from the account and
// freezes it on the row. What it cannot do is reply to itself, open a thread,
// resolve one, carry a read marker, or add a participant — each of those is a
// store refusal, mapped below. Being on a thread's participant list — who is
// expected to read — is a separate fact from having written in it, and an
// agent is put there by a person through POST /participants.

// Threads is the slice of the store this surface needs. *store.Store
// satisfies it; the page and note lookups a thread also needs come through
// Wiki, so the two interfaces name the two stores' worth of E5 and E6 rather
// than one bag.
type Threads interface {
	OpenDiscussion(ctx context.Context, in store.NewDiscussion) (store.Discussion, store.DiscussionTurn, error)
	AppendTurn(ctx context.Context, in store.NewTurn) (store.DiscussionTurn, error)
	DiscussionByNumber(ctx context.Context, number int64) (store.Discussion, error)
	DiscussionByID(ctx context.Context, id uuid.UUID) (store.Discussion, error)
	DiscussionsOnPage(ctx context.Context, pageID uuid.UUID) ([]store.Discussion, error)
	Turns(ctx context.Context, discussionID uuid.UUID) ([]store.DiscussionTurn, error)
	Participants(ctx context.Context, discussionID uuid.UUID) ([]store.DiscussionParticipant, error)
	AddParticipant(ctx context.Context, discussionID, userID, addedBy uuid.UUID) error
	RemoveParticipant(ctx context.Context, discussionID, userID, removedBy uuid.UUID) error
	MarkRead(ctx context.Context, discussionID, userID uuid.UUID, throughSeq int) error
	UnreadCount(ctx context.Context, discussionID, userID uuid.UUID) (int, error)
	UnreadByDiscussion(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error)
	ResolveIntoNewNote(ctx context.Context, discussionID, by uuid.UUID, in store.Resolution) (store.Note, store.NoteRevision, error)
	ResolveIntoExistingNote(ctx context.Context, discussionID, noteID, by uuid.UUID, title, body string) (store.NoteRevision, error)
	ResolveWithoutNote(ctx context.Context, discussionID, by uuid.UUID) error
}

// The three ways a thread resolves, as the request names them. Required and
// without a default: resolving with no note is allowed and must be chosen.
const (
	intoNewNote      = "new_note"
	intoExistingNote = "existing_note"
	intoNothing      = "nothing"
)

func (a *api) threadsUnavailable(w http.ResponseWriter) bool {
	if a.threads != nil && a.wiki != nil {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, codeThreadsUnconfigured,
		"this router was assembled with no discussions store behind it")
	return true
}

func discussionRef(w http.ResponseWriter, ref string) (int64, bool) {
	n, err := store.ParseDiscussionRef(ref)
	if err != nil {
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "ref is not a discussion reference")
		return 0, false
	}
	return n, true
}

// thread reads a discussion by number and answers the 404 itself.
func (a *api) thread(w http.ResponseWriter, r *http.Request, ctx context.Context, number int64) (store.Discussion, bool) {
	d, err := a.threads.DiscussionByNumber(ctx, number)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such discussion")
		return store.Discussion{}, false
	case err != nil:
		a.serverError(w, r, "discussion by number", err)
		return store.Discussion{}, false
	}
	return d, true
}

// turnRefused maps the store's answers on an append. Every one is a fact
// about the thread's current state, so every one is a 409, and the code says
// which so a client can act: a person can be asked to speak, a resolved
// thread wants a new one opened, a busy thread wants a retry.
func (a *api) turnRefused(w http.ResponseWriter, r *http.Request, what string, err error) {
	switch {
	case errors.Is(err, store.ErrAgentMayNotFollowAgent):
		writeError(w, http.StatusConflict, codeAgentAfterAgent,
			"an agent turn must follow a person's turn")
	case errors.Is(err, store.ErrDiscussionResolved):
		writeError(w, http.StatusConflict, codeDiscussionResolved,
			"this discussion is resolved and takes no more turns; open a new one that cites it")
	case errors.Is(err, store.ErrLinkLocked):
		writeError(w, http.StatusConflict, codeThreadBusy,
			"another append holds this thread; retry")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such discussion, page or memo")
	case errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, codeInvalidBody, "the turn was refused as invalid")
	default:
		a.serverError(w, r, what, err)
	}
}

// ── list and open ───────────────────────────────────────────────────────────

func (a *api) ListDiscussions(w http.ResponseWriter, r *http.Request, params wire.ListDiscussionsParams) {
	if a.threadsUnavailable(w) {
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

	page, redirected, err := a.wiki.ResolvePath(ctx, params.Page)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no page at that path")
		return
	case err != nil:
		a.serverError(w, r, "list discussions: page", err)
		return
	}
	path := params.Page
	if redirected {
		if path, err = a.wiki.PagePath(ctx, page.ID); err != nil {
			a.serverError(w, r, "list discussions: page path", err)
			return
		}
	}
	threads, err := a.threads.DiscussionsOnPage(ctx, page.ID)
	if err != nil {
		a.serverError(w, r, "list discussions", err)
		return
	}
	pageOf, next := window(threads, func(d store.Discussion) int64 { return d.Number }, after, limit)
	items := make([]wire.Discussion, 0, len(pageOf))
	for _, d := range pageOf {
		noteRef, err := a.resolvedNoteRef(ctx, d)
		if err != nil {
			a.serverError(w, r, "list discussions: resolved note", err)
			return
		}
		items = append(items, toDiscussion(d, &path, noteRef))
	}
	out := wire.DiscussionList{Page: toPage(page, path), Items: items, NextCursor: next}
	if redirected {
		out.MovedFrom = &params.Page
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) OpenDiscussion(w http.ResponseWriter, r *http.Request) {
	if a.threadsUnavailable(w) {
		return
	}
	var req wire.NewDiscussionRequest
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
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	in := store.NewDiscussion{Title: req.Title, AuthorID: u.ID, Body: req.Body, ComposedAt: req.ComposedAt}
	if req.Page != nil && *req.Page != "" {
		page, err := a.wiki.PageByPath(ctx, *req.Page)
		switch {
		case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalidSlug):
			writeError(w, http.StatusNotFound, codeNotFound, "the page path names no page")
			return
		case err != nil:
			a.serverError(w, r, "open discussion: page", err)
			return
		}
		in.PageID = &page.ID
	}

	// AN AGENT CANNOT OPEN A THREAD, and nothing here says so: CH091 refuses
	// an agent at seq 1 because no person's turn precedes it, and turnRefused
	// relays that as the same 409 a self-reply gets.
	d, _, err := a.threads.OpenDiscussion(ctx, in)
	if err != nil {
		a.turnRefused(w, r, "open discussion", err)
		return
	}
	view, err := a.threadView(ctx, d, u)
	if err != nil {
		a.serverError(w, r, "open discussion: view", err)
		return
	}
	writeJSON(w, http.StatusCreated, view)
}

// ── the badge ───────────────────────────────────────────────────────────────

func (a *api) ListUnread(w http.ResponseWriter, r *http.Request) {
	if a.threadsUnavailable(w) {
		return
	}
	u := userFrom(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	counts, err := a.threads.UnreadByDiscussion(ctx, u.ID)
	if err != nil {
		a.serverError(w, r, "unread by discussion", err)
		return
	}
	// One read per thread wanting attention, to name it. Bounded by how far
	// behind one person is, which is the number the badge exists to keep
	// small; a store read that joins the title is the thing to add if it is
	// ever not.
	items := make([]wire.UnreadItem, 0, len(counts))
	for id, n := range counts {
		d, err := a.threads.DiscussionByID(ctx, id)
		if err != nil {
			a.serverError(w, r, "unread: discussion", err)
			return
		}
		items = append(items, wire.UnreadItem{Ref: d.Ref(), Title: d.Title, Unread: n})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ref < items[j].Ref })
	writeJSON(w, http.StatusOK, wire.UnreadList{Items: items})
}

// ── one thread ──────────────────────────────────────────────────────────────

func (a *api) GetDiscussion(w http.ResponseWriter, r *http.Request, ref string) {
	if a.threadsUnavailable(w) {
		return
	}
	number, ok := discussionRef(w, ref)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	d, ok := a.thread(w, r, ctx, number)
	if !ok {
		return
	}
	view, err := a.threadView(ctx, d, userFrom(r.Context()))
	if err != nil {
		a.serverError(w, r, "get discussion", err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// threadView assembles a thread for one caller: the turns rendered — each
// through the same render helper the note handler uses, so the miss feed
// still has one call site — the participants, and the caller's own unread.
func (a *api) threadView(ctx context.Context, d store.Discussion, caller store.User) (wire.Thread, error) {
	var path *string
	if d.PageID != nil {
		p, err := a.wiki.PagePath(ctx, *d.PageID)
		if err != nil {
			return wire.Thread{}, fmt.Errorf("page path: %w", err)
		}
		path = &p
	}
	noteRef, err := a.resolvedNoteRef(ctx, d)
	if err != nil {
		return wire.Thread{}, err
	}
	turns, err := a.threads.Turns(ctx, d.ID)
	if err != nil {
		return wire.Thread{}, fmt.Errorf("turns: %w", err)
	}
	out := wire.Thread{Discussion: toDiscussion(d, path, noteRef), Turns: make([]wire.Turn, 0, len(turns))}
	for _, t := range turns {
		turn, err := a.toTurn(t)
		if err != nil {
			return wire.Thread{}, err
		}
		out.Turns = append(out.Turns, turn)
	}
	participants, err := a.threads.Participants(ctx, d.ID)
	if err != nil {
		return wire.Thread{}, fmt.Errorf("participants: %w", err)
	}
	out.Participants = make([]wire.Participant, 0, len(participants))
	for _, p := range participants {
		out.Participants = append(out.Participants, toParticipant(p))
	}

	// THE CALLER'S UNREAD, and only a person's: an agent has no unread
	// (CHRN-45 ruling 4), and a non-participant has no marker to count from
	// — reading is not joining, so the count is absent rather than invented.
	if caller.Kind != store.KindAgent {
		n, err := a.threads.UnreadCount(ctx, d.ID, caller.ID)
		switch {
		case errors.Is(err, store.ErrNotAParticipant):
		case err != nil:
			return wire.Thread{}, fmt.Errorf("unread: %w", err)
		default:
			out.Unread = &n
		}
	}
	return out, nil
}

// toTurn renders one turn. The body is authored markdown like a note's, and
// it goes through the one render helper: html, descriptors, and the misses
// to the one feed.
func (a *api) toTurn(t store.DiscussionTurn) (wire.Turn, error) {
	html, refs, err := a.render(t.Body)
	if err != nil {
		return wire.Turn{}, fmt.Errorf("render turn %d: %w", t.Seq, err)
	}
	return wire.Turn{
		Id:         t.ID,
		Seq:        t.Seq,
		AuthorId:   t.AuthorID,
		AuthorKind: wire.TurnAuthorKind(t.AuthorKind),
		Body:       t.Body,
		Html:       html,
		References: refs,
		CreatedAt:  t.CreatedAt,
		ComposedAt: t.ComposedAt,
		MemoId:     t.MemoID,
	}, nil
}

// ── turns, markers, participants ────────────────────────────────────────────

func (a *api) AppendTurn(w http.ResponseWriter, r *http.Request, ref string) {
	if a.threadsUnavailable(w) {
		return
	}
	number, ok := discussionRef(w, ref)
	if !ok {
		return
	}
	var req wire.NewTurnRequest
	if !decodeJSONLimit(w, r, &req, maxNoteBody) {
		return
	}
	if strings.TrimSpace(req.Body) == "" {
		writeError(w, http.StatusBadRequest, codeMissingField, "body is required")
		return
	}
	u := userFrom(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	d, ok := a.thread(w, r, ctx, number)
	if !ok {
		return
	}
	// No seq and no kind in the request: the store allocates one under the
	// lock and derives the other from the account.
	t, err := a.threads.AppendTurn(ctx, store.NewTurn{
		DiscussionID: d.ID, AuthorID: u.ID, Body: req.Body, ComposedAt: req.ComposedAt,
	})
	if err != nil {
		a.turnRefused(w, r, "append turn", err)
		return
	}
	turn, err := a.toTurn(t)
	if err != nil {
		a.serverError(w, r, "append turn: render", err)
		return
	}
	writeJSON(w, http.StatusCreated, turn)
}

func (a *api) MarkRead(w http.ResponseWriter, r *http.Request, ref string) {
	if a.threadsUnavailable(w) {
		return
	}
	number, ok := discussionRef(w, ref)
	if !ok {
		return
	}
	var req wire.MarkReadRequest
	if !decodeJSONLimit(w, r, &req, maxPageBody) {
		return
	}
	if req.ThroughSeq < 0 {
		writeError(w, http.StatusBadRequest, codeInvalidBody, "through_seq cannot be negative")
		return
	}
	u := userFrom(r.Context())
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	d, ok := a.thread(w, r, ctx, number)
	if !ok {
		return
	}
	// A POSITION, clamped by the store: forward only, never past the thread.
	// A stale report is a no-op, not an error.
	err := a.threads.MarkRead(ctx, d.ID, u.ID, req.ThroughSeq)
	switch {
	case errors.Is(err, store.ErrNotAParticipant):
		writeError(w, http.StatusForbidden, codeNotAParticipant,
			"reading is not joining: this account is not a participant of the discussion")
	case errors.Is(err, store.ErrAgentHasNoMarker):
		writeError(w, http.StatusForbidden, codeAgentHasNoMarker, "an agent carries no read marker")
	case errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, codeInvalidBody, "the position was refused")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such discussion")
	case err != nil:
		a.serverError(w, r, "mark read", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) AddParticipant(w http.ResponseWriter, r *http.Request, ref string) {
	if a.threadsUnavailable(w) {
		return
	}
	number, ok := discussionRef(w, ref)
	if !ok {
		return
	}
	var req wire.ParticipantRequest
	if !decodeJSONLimit(w, r, &req, maxPageBody) {
		return
	}
	if req.UserId == uuid.Nil {
		writeError(w, http.StatusBadRequest, codeMissingField, "user_id is required")
		return
	}
	u := userFrom(r.Context())
	if !requirePerson(w, u) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	d, ok := a.thread(w, r, ctx, number)
	if !ok {
		return
	}
	err := a.threads.AddParticipant(ctx, d.ID, req.UserId, u.ID)
	switch {
	case errors.Is(err, store.ErrConfirmerRequired):
		writeError(w, http.StatusForbidden, codePersonRequired, "a participant is added by a person")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such account")
	case err != nil:
		a.serverError(w, r, "add participant", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) RemoveParticipant(w http.ResponseWriter, r *http.Request, ref string, id wire.UserId) {
	if a.threadsUnavailable(w) {
		return
	}
	number, ok := discussionRef(w, ref)
	if !ok {
		return
	}
	u := userFrom(r.Context())
	if !requirePerson(w, u) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	d, ok := a.thread(w, r, ctx, number)
	if !ok {
		return
	}
	err := a.threads.RemoveParticipant(ctx, d.ID, id, u.ID)
	switch {
	case errors.Is(err, store.ErrConfirmerRequired):
		writeError(w, http.StatusForbidden, codePersonRequired, "a participant is removed by a person")
	case err != nil:
		a.serverError(w, r, "remove participant", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── resolve ─────────────────────────────────────────────────────────────────

func (a *api) ResolveDiscussion(w http.ResponseWriter, r *http.Request, ref string) {
	if a.threadsUnavailable(w) {
		return
	}
	number, ok := discussionRef(w, ref)
	if !ok {
		return
	}
	var req wire.ResolveDiscussionRequest
	if !decodeJSONLimit(w, r, &req, maxNoteBody) {
		return
	}
	u := userFrom(r.Context())
	// THE RESOLVER IS A PERSON. The store tests this in the same statement
	// that writes, on both resolve paths; this is the message before the
	// round trip.
	if !requirePerson(w, u) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), wikiTimeout)
	defer cancel()

	d, ok := a.thread(w, r, ctx, number)
	if !ok {
		return
	}

	var note *store.Note
	switch string(req.Into) {
	case intoNewNote:
		if deref(req.Page) == "" || strings.TrimSpace(deref(req.Title)) == "" || strings.TrimSpace(deref(req.Body)) == "" {
			writeError(w, http.StatusBadRequest, codeMissingField, "new_note needs page, title and body")
			return
		}
		page, err := a.wiki.PageByPath(ctx, *req.Page)
		switch {
		case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrInvalidSlug):
			writeError(w, http.StatusNotFound, codeNotFound, "the page path names no page")
			return
		case err != nil:
			a.serverError(w, r, "resolve: page", err)
			return
		}
		n, _, err := a.threads.ResolveIntoNewNote(ctx, d.ID, u.ID, store.Resolution{
			PageID: page.ID, Title: *req.Title, Body: *req.Body,
		})
		if err != nil {
			a.resolveRefused(w, r, "resolve into a new note", err)
			return
		}
		note = &n

	case intoExistingNote:
		if deref(req.NoteRef) == "" || strings.TrimSpace(deref(req.Body)) == "" {
			writeError(w, http.StatusBadRequest, codeMissingField, "existing_note needs note_ref and body")
			return
		}
		noteNumber, err := store.ParseNoteRef(*req.NoteRef)
		if err != nil {
			writeError(w, http.StatusBadRequest, codeInvalidBody, "note_ref is not a note reference")
			return
		}
		n, err := a.wiki.NoteByNumber(ctx, noteNumber)
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeError(w, http.StatusNotFound, codeNotFound, "no such note")
			return
		case err != nil:
			a.serverError(w, r, "resolve: note", err)
			return
		}
		if n.Deleted() {
			writeError(w, http.StatusConflict, codeNoteDeleted, "that note is deleted; undelete it first")
			return
		}
		title := deref(req.Title)
		if strings.TrimSpace(title) == "" {
			current, err := a.wiki.CurrentRevision(ctx, n.ID)
			if err != nil {
				a.serverError(w, r, "resolve: current revision", err)
				return
			}
			title = current.Title
		}
		if _, err := a.threads.ResolveIntoExistingNote(ctx, d.ID, n.ID, u.ID, title, *req.Body); err != nil {
			a.resolveRefused(w, r, "resolve into an existing note", err)
			return
		}
		note = &n

	case intoNothing:
		if err := a.threads.ResolveWithoutNote(ctx, d.ID, u.ID); err != nil {
			a.resolveRefused(w, r, "resolve without a note", err)
			return
		}

	default:
		writeError(w, http.StatusBadRequest, codeInvalidBody, "into must be new_note, existing_note or nothing")
		return
	}

	// Re-read the thread for the recorded resolution — the instant and the
	// resolver are the store's, and on a completion they are the ORIGINAL
	// resolver's, not this caller's.
	resolved, err := a.threads.DiscussionByID(ctx, d.ID)
	if err != nil {
		a.serverError(w, r, "resolve: reread", err)
		return
	}
	var noteRef *string
	if note != nil {
		ref := note.Ref()
		noteRef = &ref
	} else if noteRef, err = a.resolvedNoteRef(ctx, resolved); err != nil {
		// A completion of a thread that resolved without a note reaches
		// here with the recorded note; "nothing" on a thread with one is
		// refused by the store before this.
		a.serverError(w, r, "resolve: resolved note", err)
		return
	}
	var path *string
	if d.PageID != nil {
		p, err := a.wiki.PagePath(ctx, *d.PageID)
		if err != nil {
			a.serverError(w, r, "resolve: page path", err)
			return
		}
		path = &p
	}
	out := wire.DiscussionResolution{Discussion: toDiscussion(resolved, path, noteRef)}
	if note != nil {
		view, err := a.resolvedNoteView(ctx, *note)
		if err != nil {
			a.serverError(w, r, "resolve: note view", err)
			return
		}
		out.Note = &view
	}
	writeJSON(w, http.StatusOK, out)
}

// resolvedNoteRef turns the note id a resolved row carries into the ref a
// client addresses. Nil when the thread is unresolved or resolved into
// nothing.
func (a *api) resolvedNoteRef(ctx context.Context, d store.Discussion) (*string, error) {
	if d.ResolvedNoteID == nil {
		return nil, nil
	}
	n, err := a.wiki.NoteByID(ctx, *d.ResolvedNoteID)
	if err != nil {
		return nil, fmt.Errorf("resolved note: %w", err)
	}
	ref := n.Ref()
	return &ref, nil
}

// resolvedNoteView is the note's end of the link: its summary, its current
// revision, and every thread that concluded into it — this one included.
func (a *api) resolvedNoteView(ctx context.Context, n store.Note) (wire.ResolvedNote, error) {
	path, err := a.wiki.PagePath(ctx, n.PageID)
	if err != nil {
		return wire.ResolvedNote{}, fmt.Errorf("page path: %w", err)
	}
	rev, err := a.wiki.CurrentRevision(ctx, n.ID)
	if err != nil {
		return wire.ResolvedNote{}, fmt.Errorf("current revision: %w", err)
	}
	threads, err := a.wiki.DiscussionsResolvedInto(ctx, n.ID)
	if err != nil {
		return wire.ResolvedNote{}, fmt.Errorf("resolved from: %w", err)
	}
	return wire.ResolvedNote{
		Ref:          n.Ref(),
		Title:        rev.Title,
		Page:         path,
		CreatedAt:    n.CreatedAt,
		UpdatedAt:    n.UpdatedAt,
		Revision:     toRevisionMeta(rev),
		ResolvedFrom: toDiscussionSummaries(threads),
	}, nil
}

func (a *api) resolveRefused(w http.ResponseWriter, r *http.Request, what string, err error) {
	switch {
	case errors.Is(err, store.ErrResolutionFixed):
		writeError(w, http.StatusConflict, codeResolutionFixed,
			"this discussion's resolution is recorded and names a different note")
	case errors.Is(err, store.ErrConfirmerRequired):
		writeError(w, http.StatusForbidden, codePersonRequired, "a discussion is resolved by a person")
	case errors.Is(err, store.ErrNoteDeleted):
		writeError(w, http.StatusConflict, codeNoteDeleted, "that note is deleted; undelete it first")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such discussion, page or note")
	case errors.Is(err, store.ErrLinkLocked):
		writeError(w, http.StatusConflict, codeThreadBusy, "another write holds this thread; retry")
	default:
		a.serverError(w, r, what, err)
	}
}
