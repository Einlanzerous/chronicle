package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-98's surface against an in-memory E5. The store's own guards are the
// store's tests; what is proved here is the HTTP contract — every declared
// status driven through apitest.Conform, the ETag honest, the tombstone bare,
// the miss feed fed exactly once per render, and no upstream dialled.

// fakeWiki holds pages by path, notes by number and revisions by note. Writes
// by an account it knows to be an agent are refused the way CH041 refuses
// them, so the handler's mapping of that refusal is driven.
type fakeWiki struct {
	mu        sync.Mutex
	pages     map[string]store.Page
	redirects map[string]string // old path -> live path
	notes     map[int64]store.Note
	revs      map[uuid.UUID][]store.NoteRevision
	threads   map[uuid.UUID][]store.Discussion
	links     map[int64][]store.Backlink // target number -> sources, as the index would answer
	hits      []store.SearchHit
	agents    map[uuid.UUID]bool
	next      int64
	err       error
}

func newFakeWiki() *fakeWiki {
	return &fakeWiki{
		pages:     map[string]store.Page{},
		redirects: map[string]string{},
		notes:     map[int64]store.Note{},
		revs:      map[uuid.UUID][]store.NoteRevision{},
		threads:   map[uuid.UUID][]store.Discussion{},
		links:     map[int64][]store.Backlink{},
		agents:    map[uuid.UUID]bool{},
		next:      1,
	}
}

func (f *fakeWiki) pathOf(id uuid.UUID) (string, bool) {
	for path, p := range f.pages {
		if p.ID == id {
			return path, true
		}
	}
	return "", false
}

func (f *fakeWiki) CreatePage(_ context.Context, parent *uuid.UUID, slug string) (store.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := store.ValidateSlug(slug); err != nil {
		return store.Page{}, err
	}
	path := slug
	if parent != nil {
		pp, ok := f.pathOf(*parent)
		if !ok {
			return store.Page{}, store.ErrNotFound
		}
		path = pp + "/" + slug
	}
	if _, taken := f.pages[path]; taken {
		return store.Page{}, store.ErrSiblingSlug
	}
	now := time.Now()
	p := store.Page{ID: uuid.New(), ParentID: parent, Slug: slug, CreatedAt: now, UpdatedAt: now}
	f.pages[path] = p
	return p, nil
}

func (f *fakeWiki) PageByPath(_ context.Context, path string) (store.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := store.SplitPath(path); err != nil {
		return store.Page{}, err
	}
	p, ok := f.pages[path]
	if !ok {
		return store.Page{}, store.ErrNotFound
	}
	return p, nil
}

func (f *fakeWiki) ResolvePath(ctx context.Context, path string) (store.Page, bool, error) {
	p, err := f.PageByPath(ctx, path)
	if err == nil {
		return p, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return store.Page{}, false, err
	}
	f.mu.Lock()
	live, ok := f.redirects[path]
	f.mu.Unlock()
	if !ok {
		return store.Page{}, false, store.ErrNotFound
	}
	p, err = f.PageByPath(ctx, live)
	return p, err == nil, err
}

func (f *fakeWiki) PagePath(_ context.Context, id uuid.UUID) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if path, ok := f.pathOf(id); ok {
		return path, nil
	}
	return "", store.ErrNotFound
}

func (f *fakeWiki) ListPagePaths(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]string, 0, len(f.pages))
	for path := range f.pages {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeWiki) CreateNote(_ context.Context, in store.NewNote) (store.Note, store.NoteRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agents[in.ConfirmedBy] || in.ConfirmedBy == uuid.Nil {
		return store.Note{}, store.NoteRevision{}, store.ErrConfirmerRequired
	}
	if _, ok := f.pathOf(in.PageID); !ok {
		return store.Note{}, store.NoteRevision{}, store.ErrNotFound
	}
	now := time.Now()
	rev := store.NoteRevision{ID: uuid.New(), Seq: 1, Title: in.Title, Body: in.Body,
		AuthorID: in.AuthorID, ConfirmedBy: &in.ConfirmedBy, CreatedAt: now, Verb: in.Verb, MemoID: in.MemoID}
	n := store.Note{ID: uuid.New(), Number: f.next, PageID: in.PageID, CurrentRevisionID: rev.ID,
		AuthorID: in.AuthorID, CreatedAt: now, UpdatedAt: now}
	rev.NoteID = n.ID
	f.next++
	f.notes[n.Number] = n
	f.revs[n.ID] = []store.NoteRevision{rev}
	return n, rev, nil
}

func (f *fakeWiki) AppendRevision(_ context.Context, noteID uuid.UUID, in store.NewRevision) (store.NoteRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.agents[in.ConfirmedBy] || in.ConfirmedBy == uuid.Nil {
		return store.NoteRevision{}, store.ErrConfirmerRequired
	}
	for number, n := range f.notes {
		if n.ID != noteID {
			continue
		}
		if n.Deleted() {
			return store.NoteRevision{}, store.ErrNoteDeleted
		}
		rev := store.NoteRevision{ID: uuid.New(), NoteID: n.ID, Seq: len(f.revs[n.ID]) + 1,
			Title: in.Title, Body: in.Body, AuthorID: in.AuthorID, ConfirmedBy: &in.ConfirmedBy,
			CreatedAt: time.Now(), Verb: in.Verb}
		f.revs[n.ID] = append(f.revs[n.ID], rev)
		n.CurrentRevisionID = rev.ID
		n.UpdatedAt = rev.CreatedAt
		f.notes[number] = n
		return rev, nil
	}
	return store.NoteRevision{}, store.ErrNotFound
}

func (f *fakeWiki) NoteByNumber(_ context.Context, number int64) (store.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Note{}, f.err
	}
	n, ok := f.notes[number]
	if !ok {
		return store.Note{}, store.ErrNotFound
	}
	return n, nil
}

func (f *fakeWiki) NoteByID(_ context.Context, id uuid.UUID) (store.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, n := range f.notes {
		if n.ID == id {
			return n, nil
		}
	}
	return store.Note{}, store.ErrNotFound
}

func (f *fakeWiki) CurrentRevision(_ context.Context, noteID uuid.UUID) (store.NoteRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	revs := f.revs[noteID]
	if len(revs) == 0 {
		return store.NoteRevision{}, store.ErrNotFound
	}
	return revs[len(revs)-1], nil
}

func (f *fakeWiki) NoteRevisions(_ context.Context, noteID uuid.UUID) ([]store.NoteRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.NoteRevision(nil), f.revs[noteID]...), nil
}

func (f *fakeWiki) NotesOnPage(_ context.Context, pageID uuid.UUID) ([]store.Note, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Note
	for _, n := range f.notes {
		if n.PageID == pageID && !n.Deleted() {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (f *fakeWiki) DiscussionsResolvedInto(_ context.Context, noteID uuid.UUID) ([]store.Discussion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Discussion(nil), f.threads[noteID]...), nil
}

// Backlinks answers what the seeded index says, minus any source that has
// since been soft-deleted — the store's query has `n.deleted_at IS NULL` on
// the join, and the fake keeps that half of the contract so the handler test
// can drive it.
func (f *fakeWiki) Backlinks(_ context.Context, number int64) ([]store.Backlink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := []store.Backlink{}
	for _, b := range f.links[number] {
		if src, ok := f.notes[b.Number]; !ok || src.Deleted() {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (f *fakeWiki) Search(_ context.Context, query string, limit int) ([]store.SearchHit, error) {
	if strings.Trim(query, "!?.,") == "" {
		return nil, store.ErrEmptyQuery
	}
	if len(f.hits) > limit {
		return f.hits[:limit], nil
	}
	return f.hits, nil
}

// softDelete stamps a note deleted the way SoftDeleteNote would.
func (f *fakeWiki) softDelete(number int64, by uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.notes[number]
	at := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	n.DeletedAt, n.DeletedBy = &at, &by
	f.notes[number] = n
}

// dialGuard is a resolve.Transport that fails the test if it is ever asked:
// criterion 9 — a note read performs no upstream call.
type dialGuard struct {
	t     *testing.T
	calls atomic.Int32
}

func (g *dialGuard) Fetch(_ context.Context, ref markdown.Reference) resolve.Answer {
	g.calls.Add(1)
	g.t.Errorf("a note read dialled an upstream for %q; ruling 3 says it never does", ref.Token)
	return resolve.Answer{Status: 500}
}

func (g *dialGuard) URLFor(string) string { return "" }

type wikiRig struct {
	h      http.Handler
	logs   *bytes.Buffer
	wiki   *fakeWiki
	guard  *dialGuard
	member store.User
	agent  store.User
}

// newWikiRig builds the surface over the fake store, with a LIVE key set that
// knows SWY and CHRN so a scan can both recognise a ticket and miss one, and a
// resolver whose only transport fails the test on contact.
func newWikiRig(t *testing.T, withKeys bool) *wikiRig {
	t.Helper()
	rig := &wikiRig{logs: &bytes.Buffer{}, wiki: newFakeWiki(), guard: &dialGuard{t: t}}
	logger := jsonLogger(rig.logs)

	var keys *resolve.Keys
	if withKeys {
		keys = resolve.NewKeys(resolve.KeysOptions{
			Fetch:  func(context.Context) ([]string, error) { return []string{"SWY", "CHRN"}, nil },
			Logger: logger,
		})
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		go keys.Run(ctx)
		for deadline := time.Now().Add(2 * time.Second); !keys.Ready(); {
			if time.Now().After(deadline) {
				t.Fatal("the key set never loaded")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	resolver, err := resolve.New(resolve.Options{
		Transports: map[string]resolve.Transport{markdown.SystemSwitchyard: rig.guard, markdown.SystemAmber: rig.guard},
		Keys:       keys, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeAccounts()
	rig.member = f.signIn(person("member@example.com", false), "member-token")
	f.signIn(person("owner@example.com", true), "owner-token")
	rig.agent = f.signIn(store.User{ID: uuid.New(), Email: store.ScribeEmail, DisplayName: "Scribe", Kind: store.KindAgent}, "agent-token")
	rig.wiki.agents[rig.agent.ID] = true

	rig.h = NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: logger, Version: "test", SecureCookies: true,
		Wiki: rig.wiki, Keys: keys, References: resolver, LocalReferences: rig.wiki,
	})
	return rig
}

// LocalReferences, so the rig can also serve /references/resolve if a test
// wants the two phases together.
func (f *fakeWiki) DiscussionByNumber(context.Context, int64) (store.Discussion, error) {
	return store.Discussion{}, store.ErrNotFound
}

func (rig *wikiRig) do(method, path, body, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	var r *http.Request
	if body != "" {
		r = jsonReq(method, path, body)
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	rig.h.ServeHTTP(rec, r)
	return rec
}

func (rig *wikiRig) as(token string) func(method, path, body string) *httptest.ResponseRecorder {
	return func(method, path, body string) *httptest.ResponseRecorder {
		return rig.do(method, path, body, token)
	}
}

func decodeInto[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body did not decode as %T: %v\n%s", out, err, rec.Body.String())
	}
	return out
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int, op string) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("%s = %d, want %d: %s", op, rec.Code, want, rec.Body.String())
	}
	apitest.Conform(t, op, rec)
}

// ---------------------------------------------------------------------------

func TestPagesAreCreatedAndListedAsATree(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")

	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	rec := call(http.MethodPost, "/pages", `{"path":"estate/conventions"}`)
	mustStatus(t, rec, http.StatusCreated, "createPage")
	page := decodeInto[wire.Page](t, rec)
	if page.Path != "estate/conventions" || page.Slug != "conventions" || page.ParentId == nil {
		t.Errorf("created page = %+v", page)
	}

	// Two rows would claim one path.
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate/conventions"}`), http.StatusConflict, "createPage")
	// The parent is resolved without redirects, and must exist.
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"nowhere/child"}`), http.StatusNotFound, "createPage")
	// The slug rule, refused before the round trip.
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"Estate Wide"}`), http.StatusBadRequest, "createPage")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate//x"}`), http.StatusBadRequest, "createPage")

	rec = call(http.MethodGet, "/pages", "")
	mustStatus(t, rec, http.StatusOK, "listPages")
	tree := decodeInto[wire.PageTree](t, rec)
	if strings.Join(tree.Paths, ",") != "estate,estate/conventions" {
		t.Errorf("tree = %v", tree.Paths)
	}
}

// The Done-when's read half: create, read with an honest ETag, 304 on a
// match, append, the ETag moves, history in order, paginated by cursor.
func TestANoteRoundTripsWithAnHonestETag(t *testing.T) {
	rig := newWikiRig(t, true)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")

	body := "See SWY-389 and CHR-0311, and SY-412 which is prose. Also amber1.abc.def.0."
	rec := call(http.MethodPost, "/notes", `{"page":"estate","title":"Retention","body":`+quote(body)+`}`)
	mustStatus(t, rec, http.StatusCreated, "createNote")
	created := decodeInto[wire.Note](t, rec)
	etag := rec.Header().Get("ETag")
	if etag != `"`+created.Revision.Id.String()+`"` {
		t.Errorf("ETag %q is not the quoted revision id %s", etag, created.Revision.Id)
	}
	if created.Ref != "CHR-0001" || created.Page != "estate" || created.Body != body || created.Revision.Seq != 1 {
		t.Errorf("created note = %+v", created)
	}
	if !strings.Contains(created.Html, `data-ref="SWY-389"`) || !strings.Contains(created.Html, `data-ref="CHR-0311"`) {
		t.Errorf("html did not mark the references: %s", created.Html)
	}
	if strings.Contains(created.Html, `data-ref="SY-412"`) {
		t.Error("SY-412 names no live project and was marked anyway")
	}

	// The descriptors, in order, with nothing upstream: system, key, target,
	// token, number — and SY-412 absent, because it is prose.
	var tokens []string
	for _, d := range created.References {
		tokens = append(tokens, string(d.System)+":"+d.Token)
	}
	if strings.Join(tokens, " ") != "switchyard:SWY-389 chronicle:CHR-0311 amber:amber1.abc.def.0" {
		t.Errorf("references = %v", tokens)
	}
	if d := created.References[1]; d.Key == nil || *d.Key != "CHR" || d.Target == nil || string(*d.Target) != "note" || d.Number == nil || *d.Number != 311 {
		t.Errorf("CHR-0311 descriptor = %+v", d)
	}

	// Read it back: same ETag, and a conditional read answers 304 with no body.
	rec = call(http.MethodGet, "/notes/CHR-0001", "")
	mustStatus(t, rec, http.StatusOK, "getNote")
	if rec.Header().Get("ETag") != etag {
		t.Errorf("read ETag %q, create ETag %q", rec.Header().Get("ETag"), etag)
	}
	r := httptest.NewRequest(http.MethodGet, "/notes/chr-1", nil) // lenient ref
	r.Header.Set("Authorization", "Bearer member-token")
	r.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	rig.h.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusNotModified, "getNote")
	if rec.Header().Get("ETag") != etag {
		t.Error("a 304 carries no ETag")
	}

	// Append. Nothing is overwritten: the response says which revision it
	// produced and which one it followed.
	rec = call(http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"Second thoughts on SWY-389."}`)
	mustStatus(t, rec, http.StatusCreated, "appendRevision")
	appended := decodeInto[wire.AppendResult](t, rec)
	if appended.Followed.Id != created.Revision.Id || appended.Followed.Seq != 1 || appended.Revision.Seq != 2 {
		t.Errorf("append = %+v", appended)
	}
	newTag := rec.Header().Get("ETag")
	if newTag == etag || newTag != `"`+appended.Revision.Id.String()+`"` {
		t.Errorf("ETag after append %q; before %q", newTag, etag)
	}

	// The old validator no longer matches; the title survived the append.
	r = httptest.NewRequest(http.MethodGet, "/notes/CHR-0001", nil)
	r.Header.Set("Authorization", "Bearer member-token")
	r.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	rig.h.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusOK, "getNote")
	if got := decodeInto[wire.Note](t, rec); got.Title != "Retention" || got.Revision.Seq != 2 {
		t.Errorf("after append = %+v", got)
	}

	// History, oldest first, and a cursor that pages it without repeating.
	rec = call(http.MethodGet, "/notes/CHR-0001/revisions?limit=1", "")
	mustStatus(t, rec, http.StatusOK, "listNoteRevisions")
	first := decodeInto[wire.RevisionList](t, rec)
	if len(first.Items) != 1 || first.Items[0].Seq != 1 || first.NextCursor == nil {
		t.Fatalf("page 1 = %+v", first)
	}
	rec = call(http.MethodGet, "/notes/CHR-0001/revisions?limit=1&cursor="+*first.NextCursor, "")
	mustStatus(t, rec, http.StatusOK, "listNoteRevisions")
	second := decodeInto[wire.RevisionList](t, rec)
	if len(second.Items) != 1 || second.Items[0].Seq != 2 || second.NextCursor != nil || second.Items[0].Body != "Second thoughts on SWY-389." {
		t.Errorf("page 2 = %+v", second)
	}
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001/revisions?cursor=abc", ""), http.StatusBadRequest, "listNoteRevisions")

	// Nothing dialled, across every read above.
	if n := rig.guard.calls.Load(); n != 0 {
		t.Errorf("the upstream was dialled %d times by note reads", n)
	}
}

// The miss feed has one caller, and it is this handler: a well-shaped key
// that names no live project is reported once per render, carrying the key
// and nothing from the note's text.
func TestANoteReadFeedsTheMissFeedOncePerRenderAndDialsNothing(t *testing.T) {
	rig := newWikiRig(t, true)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")

	const secret = "SENTINEL-nothing-from-the-body-may-reach-a-log-line"
	body := "SY-412 and LOOP-9 are dead keys; SWY-389 is live. " + secret
	rec := call(http.MethodPost, "/notes", `{"page":"estate","title":"Dead keys","body":`+quote(body)+`}`)
	mustStatus(t, rec, http.StatusCreated, "createNote")

	rig.logs.Reset()
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001", ""), http.StatusOK, "getNote")
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001", ""), http.StatusOK, "getNote")

	logs := rig.logs.String()
	if n := strings.Count(logs, "well-shaped keys that name no live project"); n != 2 {
		t.Errorf("the miss line appeared %d times for two renders, want exactly one per render:\n%s", n, logs)
	}
	if !strings.Contains(logs, `"SY"`) || !strings.Contains(logs, `"LOOP"`) {
		t.Errorf("the miss line does not carry the keys:\n%s", logs)
	}
	if strings.Contains(logs, secret) || strings.Contains(logs, "dead keys") {
		t.Errorf("the note's text reached a log line:\n%s", logs)
	}
	if n := rig.guard.calls.Load(); n != 0 {
		t.Errorf("the upstream was dialled %d times", n)
	}

	// A 304 is not a render: no scan, no miss.
	rig.logs.Reset()
	rec = call(http.MethodGet, "/notes/CHR-0001", "")
	r := httptest.NewRequest(http.MethodGet, "/notes/CHR-0001", nil)
	r.Header.Set("Authorization", "Bearer member-token")
	r.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rig.logs.Reset()
	rec = httptest.NewRecorder()
	rig.h.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusNotModified, "getNote")
	if strings.Contains(rig.logs.String(), "well-shaped keys") {
		t.Error("a 304 scanned the note and fed the miss feed")
	}
}

// With no tracker configured there is no key set: ticket-shaped tokens stay
// prose, the local namespaces still mark, and there is nothing to report to.
func TestWithoutAKeySetTicketTokensStayProse(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	rec := call(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"SWY-389 beside CHR-0311"}`)
	mustStatus(t, rec, http.StatusCreated, "createNote")
	n := decodeInto[wire.Note](t, rec)
	if len(n.References) != 1 || n.References[0].Token != "CHR-0311" {
		t.Errorf("references with no key set = %+v, want only CHR-0311", n.References)
	}
}

// Ruling 4: the tombstone, and nothing else, on all four routes.
func TestADeletedNoteAnswersABareTombstone(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	const title = "WITHHELD-title"
	mustStatus(t, call(http.MethodPost, "/notes", `{"page":"estate","title":"`+title+`","body":"WITHHELD-body"}`), http.StatusCreated, "createNote")
	rig.wiki.softDelete(1, rig.member.ID)

	for _, tc := range []struct{ method, path, body, op string }{
		{http.MethodGet, "/notes/CHR-0001", "", "getNote"},
		{http.MethodGet, "/notes/CHR-0001/revisions", "", "listNoteRevisions"},
		{http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"more"}`, "appendRevision"},
		{http.MethodGet, "/notes/CHR-0001/backlinks", "", "listNoteBacklinks"},
	} {
		rec := call(tc.method, tc.path, tc.body)
		mustStatus(t, rec, http.StatusGone, tc.op)
		stone := decodeInto[wire.NoteTombstone](t, rec)
		if stone.Ref != "CHR-0001" || stone.DeletedBy != rig.member.ID || stone.DeletedAt.IsZero() {
			t.Errorf("%s tombstone = %+v", tc.op, stone)
		}
		if strings.Contains(rec.Body.String(), "WITHHELD") {
			t.Errorf("%s: the tombstone carries authored text: %s", tc.op, rec.Body.String())
		}
	}

	// And it is gone from the page's list — by the store, not by this handler.
	rec := call(http.MethodGet, "/notes?page=estate", "")
	mustStatus(t, rec, http.StatusOK, "listNotes")
	if l := decodeInto[wire.NoteList](t, rec); len(l.Items) != 0 {
		t.Errorf("a deleted note is still listed: %+v", l.Items)
	}
}

// CH041 on the wire: an agent session cannot confirm authored text, before
// the round trip and — driven through the fake's own refusal — after it.
func TestAnAgentSessionCannotConfirmAuthoredText(t *testing.T) {
	rig := newWikiRig(t, false)
	mustStatus(t, rig.do(http.MethodPost, "/pages", `{"path":"estate"}`, "member-token"), http.StatusCreated, "createPage")
	mustStatus(t, rig.do(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`, "member-token"), http.StatusCreated, "createNote")

	rec := rig.do(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`, "agent-token")
	mustStatus(t, rec, http.StatusForbidden, "createNote")
	if e := decodeInto[wire.Error](t, rec); e.Code != codePersonRequired {
		t.Errorf("code = %q, want %q", e.Code, codePersonRequired)
	}
	rec = rig.do(http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"b"}`, "agent-token")
	mustStatus(t, rec, http.StatusForbidden, "appendRevision")

	// The store's refusal reaches the same answer when the pre-check is not
	// what stopped it: a person whose confirmer the store rejects.
	rig.wiki.agents[rig.member.ID] = true
	rec = rig.do(http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"b"}`, "member-token")
	mustStatus(t, rec, http.StatusForbidden, "appendRevision")
	if e := decodeInto[wire.Error](t, rec); e.Code != codePersonRequired {
		t.Errorf("store refusal mapped to %q, want %q", e.Code, codePersonRequired)
	}

	// Agents may still read.
	mustStatus(t, rig.do(http.MethodGet, "/notes/CHR-0001", "", "agent-token"), http.StatusOK, "getNote")
}

func TestNotesOnAPageFollowARedirectAndPageByCursor(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	for _, title := range []string{"one", "two", "three"} {
		mustStatus(t, call(http.MethodPost, "/notes", `{"page":"estate","title":"`+title+`","body":"b"}`), http.StatusCreated, "createNote")
	}
	rig.wiki.redirects["old/estate"] = "estate"

	rec := call(http.MethodGet, "/notes?page=old/estate&limit=2", "")
	mustStatus(t, rec, http.StatusOK, "listNotes")
	l := decodeInto[wire.NoteList](t, rec)
	if l.MovedFrom == nil || *l.MovedFrom != "old/estate" || l.Page.Path != "estate" {
		t.Errorf("redirect not reported: moved_from %v page %+v", l.MovedFrom, l.Page)
	}
	if len(l.Items) != 2 || l.Items[0].Ref != "CHR-0001" || l.Items[1].Title != "two" || l.NextCursor == nil {
		t.Fatalf("page 1 = %+v", l)
	}
	rec = call(http.MethodGet, "/notes?page=estate&limit=2&cursor="+*l.NextCursor, "")
	mustStatus(t, rec, http.StatusOK, "listNotes")
	l = decodeInto[wire.NoteList](t, rec)
	if len(l.Items) != 1 || l.Items[0].Ref != "CHR-0003" || l.NextCursor != nil || l.MovedFrom != nil {
		t.Errorf("page 2 = %+v", l)
	}

	mustStatus(t, call(http.MethodGet, "/notes?page=nowhere", ""), http.StatusNotFound, "listNotes")
	mustStatus(t, call(http.MethodGet, "/notes?page=Bad%20Path", ""), http.StatusBadRequest, "listNotes")
	mustStatus(t, call(http.MethodGet, "/notes?page=estate&limit=0", ""), http.StatusBadRequest, "listNotes")
}

func TestSearchIsNotAList(t *testing.T) {
	rig := newWikiRig(t, false)
	// Owner only: the transcript half spans every author's memos.
	call := rig.as("owner-token")
	rec := rig.do(http.MethodGet, "/search?q=pruner", "", "member-token")
	mustStatus(t, rec, http.StatusForbidden, "search")
	noteID, memoID, number := uuid.New(), uuid.New(), int64(311)
	rig.wiki.hits = []store.SearchHit{
		{Kind: store.HitNote, NoteID: &noteID, Number: &number, Title: "Retention pruner",
			// ts_headline returns the rest of the document verbatim, and a
			// note body is stored raw: this is what a snippet of a hostile
			// note looks like on the way out of the store.
			Snippet: `the <b>pruner</b> gates on <img src=x onerror="steal()"> & <b>bold</b>`, Rank: 0.9, CreatedAt: time.Now()},
		{Kind: store.HitTranscript, MemoID: &memoID, Model: "whisper.cpp/small.en", Snippet: "we said the <b>pruner</b>", Rank: 0.4, CreatedAt: time.Now()},
	}

	rec = call(http.MethodGet, "/search?q=pruner&limit=500", "")
	mustStatus(t, rec, http.StatusOK, "search")
	res := decodeInto[wire.SearchResults](t, rec)
	if res.Query != "pruner" || res.Limit != maxSearch || len(res.Items) != 2 {
		t.Fatalf("results = %+v", res)
	}
	note, tr := res.Items[0], res.Items[1]
	if note.Kind != wire.SearchHitKindNote || note.Ref == nil || *note.Ref != "CHR-0311" || note.MemoId != nil {
		t.Errorf("note hit = %+v", note)
	}
	// Safe to embed: the markup in the body is escaped, the highlight tags
	// survive, and nothing with an attribute can.
	if want := `the <b>pruner</b> gates on &lt;img src=x onerror=&#34;steal()&#34;&gt; &amp; <b>bold</b>`; note.Snippet != want {
		t.Errorf("snippet = %q\nwant      %q", note.Snippet, want)
	}
	if strings.Contains(note.Snippet, "<img") || strings.Contains(note.Snippet, "onerror=\"") {
		t.Errorf("raw markup reached the wire: %q", note.Snippet)
	}
	if tr.Kind != wire.SearchHitKindTranscript || tr.MemoId == nil || *tr.MemoId != memoID || tr.Ref != nil || tr.Model == nil {
		t.Errorf("transcript hit = %+v", tr)
	}
	// No cursor anywhere in the shape: the type has none to decode into.
	if strings.Contains(rec.Body.String(), "cursor") {
		t.Error("search grew a cursor")
	}

	mustStatus(t, call(http.MethodGet, "/search?q=%20%20", ""), http.StatusBadRequest, "search")
	mustStatus(t, call(http.MethodGet, "/search?q=%21%21%21", ""), http.StatusBadRequest, "search")
	mustStatus(t, call(http.MethodGet, "/search?q=pruner&limit=0", ""), http.StatusBadRequest, "search")
}

func TestWikiWithoutAStoreAnswersTheDocumented503(t *testing.T) {
	f := newFakeAccounts()
	// The owner reaches every member route as well as search.
	f.signIn(person("owner@example.com", true), "member-token")
	h := testRouter(f)
	for _, tc := range []struct{ method, path, body, op string }{
		{http.MethodGet, "/pages", "", "listPages"},
		{http.MethodPost, "/pages", `{"path":"estate"}`, "createPage"},
		{http.MethodGet, "/notes?page=estate", "", "listNotes"},
		{http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`, "createNote"},
		{http.MethodGet, "/notes/CHR-0001", "", "getNote"},
		{http.MethodGet, "/notes/CHR-0001/revisions", "", "listNoteRevisions"},
		{http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"b"}`, "appendRevision"},
		{http.MethodGet, "/notes/CHR-0001/backlinks", "", "listNoteBacklinks"},
		{http.MethodGet, "/search?q=x", "", "search"},
	} {
		rec := httptest.NewRecorder()
		var r *http.Request
		if tc.body != "" {
			r = jsonReq(tc.method, tc.path, tc.body)
		} else {
			r = httptest.NewRequest(tc.method, tc.path, nil)
		}
		r.Header.Set("Authorization", "Bearer member-token")
		h.ServeHTTP(rec, r)
		mustStatus(t, rec, http.StatusServiceUnavailable, tc.op)
	}
}

func TestWikiAnswersTheDocumented500(t *testing.T) {
	rig := newWikiRig(t, false)
	rig.wiki.err = errors.New("connection reset by peer")
	call := rig.as("member-token")
	rec := call(http.MethodGet, "/pages", "")
	mustStatus(t, rec, http.StatusInternalServerError, "listPages")
	rec = call(http.MethodGet, "/notes/CHR-0001", "")
	mustStatus(t, rec, http.StatusInternalServerError, "getNote")
	if strings.Contains(rec.Body.String(), "connection reset") {
		t.Error("the cause reached the body")
	}
	rec = call(http.MethodGet, "/notes/CHR-0001/backlinks", "")
	mustStatus(t, rec, http.StatusInternalServerError, "listNoteBacklinks")
}

// The shared decoder's refusals, driven on each body-taking operation.
func TestWikiWritesDriveTheSharedRefusals(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	mustStatus(t, call(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`), http.StatusCreated, "createNote")

	for _, tc := range []struct {
		path, op, valid string
		limit           int
	}{
		{"/pages", "createPage", `{"path":"other"}`, maxPageBody},
		{"/notes", "createNote", `{"page":"estate","title":"t","body":"b"}`, maxNoteBody},
		{"/notes/CHR-0001/revisions", "appendRevision", `{"body":"b"}`, maxNoteBody},
	} {
		t.Run(tc.op, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.valid))
			r.Header.Set("Content-Type", "text/plain")
			r.Header.Set("Authorization", "Bearer member-token")
			rig.h.ServeHTTP(rec, r)
			mustStatus(t, rec, http.StatusUnsupportedMediaType, tc.op)

			mustStatus(t, call(http.MethodPost, tc.path, `{"nope":`), http.StatusBadRequest, tc.op)
			mustStatus(t, call(http.MethodPost, tc.path, `{"unknown_field":1}`), http.StatusBadRequest, tc.op)
			mustStatus(t, call(http.MethodPost, tc.path, `{}`), http.StatusBadRequest, tc.op)

			huge := `{"body":"` + strings.Repeat("x", tc.limit) + `"}`
			mustStatus(t, call(http.MethodPost, tc.path, huge), http.StatusRequestEntityTooLarge, tc.op)
		})
	}
	// A malformed {ref} on every note route.
	for _, tc := range []struct{ method, path, body, op string }{
		{http.MethodGet, "/notes/not-a-ref", "", "getNote"},
		{http.MethodGet, "/notes/CHR-0/revisions", "", "listNoteRevisions"},
		{http.MethodPost, "/notes/SWY-1/revisions", `{"body":"b"}`, "appendRevision"},
	} {
		rec := call(tc.method, tc.path, tc.body)
		mustStatus(t, rec, http.StatusBadRequest, tc.op)
		if e := decodeInto[wire.Error](t, rec); e.Code != codeInvalidParameter {
			t.Errorf("%s: code = %q", tc.op, e.Code)
		}
	}
	mustStatus(t, call(http.MethodGet, "/notes/CHR-9999", ""), http.StatusNotFound, "getNote")
	mustStatus(t, call(http.MethodGet, "/notes/CHR-9999/revisions", ""), http.StatusNotFound, "listNoteRevisions")
	mustStatus(t, call(http.MethodPost, "/notes/CHR-9999/revisions", `{"body":"b"}`), http.StatusNotFound, "appendRevision")
	mustStatus(t, call(http.MethodPost, "/notes", `{"page":"nowhere","title":"t","body":"b"}`), http.StatusNotFound, "createNote")
	// Malformed is not missing: the same string answers 400 on both routes.
	mustStatus(t, call(http.MethodPost, "/notes", `{"page":"Estate//","title":"t","body":"b"}`), http.StatusBadRequest, "createNote")
	mustStatus(t, call(http.MethodGet, "/notes?page=Estate//", ""), http.StatusBadRequest, "listNotes")
	// An agent may create a page; it is a container, not authored text.
	mustStatus(t, rig.do(http.MethodPost, "/pages", `{"path":"by-an-agent"}`, "agent-token"), http.StatusCreated, "createPage")
}

// The provenance link, read backwards: a note carries the threads that
// concluded into it (CHRN-46), and a note nothing concluded into carries an
// empty list rather than nothing.
func TestANoteCarriesTheThreadsThatConcludedIntoIt(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	rec := call(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`)
	mustStatus(t, rec, http.StatusCreated, "createNote")
	if n := decodeInto[wire.Note](t, rec); n.ResolvedFrom == nil || len(n.ResolvedFrom) != 0 {
		t.Errorf("resolved_from on a fresh note = %v, want an empty list", n.ResolvedFrom)
	}

	note := rig.wiki.notes[1]
	at := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	rig.wiki.threads[note.ID] = []store.Discussion{{ID: uuid.New(), Number: 7, Title: "Should the pruner gate on the calendar?", ResolvedAt: &at}}
	rec = call(http.MethodGet, "/notes/CHR-0001", "")
	mustStatus(t, rec, http.StatusOK, "getNote")
	n := decodeInto[wire.Note](t, rec)
	if len(n.ResolvedFrom) != 1 || n.ResolvedFrom[0].Ref != "DSC-0007" || !n.ResolvedFrom[0].ResolvedAt.Equal(at) {
		t.Errorf("resolved_from = %+v", n.ResolvedFrom)
	}
}

// CHRN-105: what links here is a sibling route, marked derived, read from
// the same store the rest of the group holds — and it changes when OTHER notes
// do, which is why it is not a field on the note and carries no validator.
func TestBacklinksAreASiblingMarkedDerived(t *testing.T) {
	rig := newWikiRig(t, false)
	call := rig.as("member-token")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate/conventions"}`), http.StatusCreated, "createPage")
	for _, in := range []string{
		`{"page":"estate","title":"Target","body":"the note everything points at"}`,
		`{"page":"estate/conventions","title":"First source","body":"see CHR-0001"}`,
		`{"page":"estate","title":"Second source","body":"also CHR-0001"}`,
		`{"page":"estate","title":"Third source","body":"and CHR-0001 again"}`,
	} {
		mustStatus(t, call(http.MethodPost, "/notes", in), http.StatusCreated, "createNote")
	}

	// A fresh note has an empty list, not a null, and the marking is present
	// even when there is nothing under it.
	rec := call(http.MethodGet, "/notes/CHR-0001/backlinks", "")
	mustStatus(t, rec, http.StatusOK, "listNoteBacklinks")
	empty := decodeInto[wire.BacklinkList](t, rec)
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Errorf("backlinks of an unlinked note = %+v, want an empty list", empty)
	}
	if empty.Generated.Tier != wire.GeneratedTierOne || empty.Generated.Source != wire.GeneratedSourceChronicle ||
		empty.Generated.Regenerable != wire.GeneratedRegenerableTrue || empty.Generated.Notice != noticeChronicle {
		t.Errorf("generated = %+v, want Chronicle's own marking", empty.Generated)
	}
	if empty.Generated.Ref != nil || empty.Generated.GeneratedAt != nil {
		t.Errorf("generated carries a stamp nothing produced: %+v", empty.Generated)
	}
	if rec.Header().Get("ETag") != "" {
		t.Error("a backlink list carries an ETag; it changes when other notes do, so nothing validates it")
	}

	// The index says three notes point here. The handler resolves each to
	// the page it is filed on, oldest source first, and windows by number.
	for _, n := range []int64{2, 3, 4} {
		src := rig.wiki.notes[n]
		rig.wiki.links[1] = append(rig.wiki.links[1], store.Backlink{
			NoteID: src.ID, Number: src.Number, Title: rig.wiki.revs[src.ID][0].Title, PageID: src.PageID,
		})
	}
	rec = call(http.MethodGet, "/notes/CHR-0001/backlinks?limit=2", "")
	mustStatus(t, rec, http.StatusOK, "listNoteBacklinks")
	l := decodeInto[wire.BacklinkList](t, rec)
	if len(l.Items) != 2 || l.NextCursor == nil {
		t.Fatalf("page 1 = %+v", l)
	}
	if l.Items[0] != (wire.Backlink{Ref: "CHR-0002", Title: "First source", Page: "estate/conventions"}) ||
		l.Items[1] != (wire.Backlink{Ref: "CHR-0003", Title: "Second source", Page: "estate"}) {
		t.Errorf("page 1 items = %+v", l.Items)
	}
	rec = call(http.MethodGet, "/notes/CHR-0001/backlinks?limit=2&cursor="+*l.NextCursor, "")
	mustStatus(t, rec, http.StatusOK, "listNoteBacklinks")
	if l = decodeInto[wire.BacklinkList](t, rec); len(l.Items) != 1 || l.Items[0].Ref != "CHR-0004" || l.NextCursor != nil {
		t.Errorf("page 2 = %+v", l)
	}

	// A soft-deleted source drops out; the target still answers.
	rig.wiki.softDelete(3, rig.member.ID)
	rec = call(http.MethodGet, "/notes/CHR-0001/backlinks", "")
	mustStatus(t, rec, http.StatusOK, "listNoteBacklinks")
	if l = decodeInto[wire.BacklinkList](t, rec); len(l.Items) != 2 || l.Items[0].Ref != "CHR-0002" || l.Items[1].Ref != "CHR-0004" {
		t.Errorf("after deleting a source = %+v", l.Items)
	}

	// The shared refusals: a ref that is not a note reference, a note that
	// never existed, and the bounds the other lists refuse.
	mustStatus(t, call(http.MethodGet, "/notes/SWY-0001/backlinks", ""), http.StatusBadRequest, "listNoteBacklinks")
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0099/backlinks", ""), http.StatusNotFound, "listNoteBacklinks")
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001/backlinks?limit=0", ""), http.StatusBadRequest, "listNoteBacklinks")
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001/backlinks?cursor=abc", ""), http.StatusBadRequest, "listNoteBacklinks")
	if rig.guard.calls.Load() != 0 {
		t.Error("a backlink read dialled an upstream")
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
