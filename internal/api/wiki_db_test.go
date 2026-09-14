package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-98's Done-when against a REAL store, because two of its clauses are
// properties of the database rather than of this package: CH041 is the guard
// the 403 rests on, and the search index is two expression indexes whose
// query has to match them byte for byte. The fakes prove the contract; this
// proves the contract holds over the store E5 actually shipped.
//
// It resets the shared chronicle_test like internal/store, internal/triage
// and internal/discuss do, which is why verify.sh runs one test binary at a
// time.

func realWiki(t *testing.T) (*store.Store, context.Context, http.Handler, string, string) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(pool)

	owner, err := st.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	scribe, err := st.EnsureAgent(ctx, store.ScribeEmail, store.ScribeDisplayName)
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	ownerTok, err := st.MintToken(ctx, owner.ID, store.TokenSession, "test", nil)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}
	// MintToken does not check kind (CHRN-44 flagged it for CHRN-65), which is
	// what lets this test hold an agent session at all.
	scribeTok, err := st.MintToken(ctx, scribe.ID, store.TokenSession, "test", nil)
	if err != nil {
		t.Fatalf("MintToken(scribe): %v", err)
	}

	h := NewRouter(Deps{
		DB: st, Accounts: st, Logger: discardLogger(), Version: "test", SecureCookies: true,
		Wiki: st, LocalReferences: st,
	})
	return st, ctx, h, ownerTok, scribeTok
}

func TestNotesRoundTripThroughTheRealStore(t *testing.T) {
	st, ctx, h, owner, scribe := realWiki(t)
	call := func(method, path, body, token string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		var r *http.Request
		if body != "" {
			r = jsonReq(method, path, body)
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		h.ServeHTTP(rec, r)
		return rec
	}

	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate"}`, owner), http.StatusCreated, "createPage")
	mustStatus(t, call(http.MethodPost, "/pages", `{"path":"estate/conventions"}`, owner), http.StatusCreated, "createPage")

	rec := call(http.MethodPost, "/notes",
		`{"page":"estate/conventions","title":"Retention pruner design","body":"The pruner gates on a durable transcript, never on the calendar. See CHR-0002."}`, owner)
	mustStatus(t, rec, http.StatusCreated, "createNote")
	created := decodeInto[wire.Note](t, rec)
	etag := rec.Header().Get("ETag")
	if created.Ref != "CHR-0001" || created.Page != "estate/conventions" || created.Revision.Seq != 1 {
		t.Fatalf("created = %+v", created)
	}
	if len(created.References) != 1 || created.References[0].Token != "CHR-0002" {
		t.Errorf("references = %+v", created.References)
	}
	if !strings.Contains(created.Html, `data-ref="CHR-0002"`) {
		t.Errorf("html = %s", created.Html)
	}
	// The confirming person is the session's account, recorded on the row.
	if created.Revision.ConfirmedBy == nil || created.Revision.AuthorId != *created.Revision.ConfirmedBy {
		t.Errorf("revision meta = %+v: author and confirmer should both be the session", created.Revision)
	}

	// Conditional read.
	r := httptest.NewRequest(http.MethodGet, "/notes/CHR-0001", nil)
	r.Header.Set("Authorization", "Bearer "+owner)
	r.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusNotModified, "getNote")

	// Append, then history.
	rec = call(http.MethodPost, "/notes/CHR-0001/revisions", `{"title":"Retention pruner","body":"Gated on the transcript. Still CHR-0002."}`, owner)
	mustStatus(t, rec, http.StatusCreated, "appendRevision")
	app := decodeInto[wire.AppendResult](t, rec)
	if app.Followed.Seq != 1 || app.Revision.Seq != 2 {
		t.Errorf("append = %+v", app)
	}
	rec = call(http.MethodGet, "/notes/CHR-0001/revisions", "", owner)
	mustStatus(t, rec, http.StatusOK, "listNoteRevisions")
	if hist := decodeInto[wire.RevisionList](t, rec); len(hist.Items) != 2 || hist.Items[1].Title != "Retention pruner" {
		t.Errorf("history = %+v", hist)
	}

	// Listing on the page, with the title from the CURRENT revision.
	rec = call(http.MethodGet, "/notes?page=estate/conventions", "", owner)
	mustStatus(t, rec, http.StatusOK, "listNotes")
	if l := decodeInto[wire.NoteList](t, rec); len(l.Items) != 1 || l.Items[0].Title != "Retention pruner" {
		t.Errorf("list = %+v", l)
	}

	// Search across the real index finds it, as a note -- and a hostile note
	// beside it comes back escaped, with ts_headline's real markers intact.
	mustStatus(t, call(http.MethodPost, "/notes",
		`{"page":"estate","title":"Hostile","body":"A transcript mention beside <img src=x onerror=\"steal()\"> markup."}`, owner), http.StatusCreated, "createNote")
	rec = call(http.MethodGet, "/search?q=transcript", "", owner)
	mustStatus(t, rec, http.StatusOK, "search")
	res := decodeInto[wire.SearchResults](t, rec)
	if len(res.Items) != 2 {
		t.Fatalf("search = %+v", res)
	}
	for _, hit := range res.Items {
		if hit.Kind != wire.SearchHitKindNote || hit.Ref == nil {
			t.Errorf("hit = %+v", hit)
		}
		if strings.Contains(hit.Snippet, "<img") {
			t.Errorf("raw markup reached the wire: %q", hit.Snippet)
		}
		if !strings.Contains(hit.Snippet, "<b>transcript</b>") {
			t.Errorf("the highlight did not survive escaping: %q", hit.Snippet)
		}
	}
	mustStatus(t, call(http.MethodGet, "/search?q=%21%21%21", "", owner), http.StatusBadRequest, "search")

	// CHRN-105 over the real index: CHR-0001's text names CHR-0002 — which did
	// not exist when the edge was extracted — and the list on CHR-0002 shows
	// it, under the current title, resolved by tier 2 from tier1.note_links.
	rec = call(http.MethodGet, "/notes/CHR-0002/backlinks", "", owner)
	mustStatus(t, rec, http.StatusOK, "listNoteBacklinks")
	back := decodeInto[wire.BacklinkList](t, rec)
	if len(back.Items) != 1 || back.Items[0] != (wire.Backlink{Ref: "CHR-0001", Title: "Retention pruner", Page: "estate/conventions"}) {
		t.Errorf("backlinks of CHR-0002 = %+v", back.Items)
	}
	if back.Generated.Source != wire.GeneratedSourceChronicle || back.Generated.Tier != wire.GeneratedTierOne {
		t.Errorf("backlinks unmarked: %+v", back.Generated)
	}

	// CH041 IS REAL: the guard the 403 rests on refuses an agent confirmer at
	// the store, whatever the handler checked first.
	_, _, err := st.CreateNote(ctx, store.NewNote{
		PageID: mustPage(t, st, ctx, "estate"), AuthorID: created.Revision.AuthorId,
		ConfirmedBy: mustScribe(t, st, ctx).ID, Title: "t", Body: "b",
	})
	if !errors.Is(err, store.ErrConfirmerRequired) {
		t.Fatalf("the store accepted an agent confirmer: %v", err)
	}
	rec = call(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`, scribe)
	mustStatus(t, rec, http.StatusForbidden, "createNote")
	mustStatus(t, call(http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"b"}`, scribe), http.StatusForbidden, "appendRevision")
	// And the agent may read.
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001", "", scribe), http.StatusOK, "getNote")

	// Ruling 4 over the real journal: the tombstone carries the recorded pair.
	note, err := st.NoteByNumber(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SoftDeleteNote(ctx, note.ID, created.Revision.AuthorId); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}
	rec = call(http.MethodGet, "/notes/CHR-0001", "", owner)
	mustStatus(t, rec, http.StatusGone, "getNote")
	stone := decodeInto[wire.NoteTombstone](t, rec)
	if stone.DeletedBy != created.Revision.AuthorId || strings.Contains(rec.Body.String(), "pruner") {
		t.Errorf("tombstone = %+v / %s", stone, rec.Body.String())
	}
	mustStatus(t, call(http.MethodPost, "/notes/CHR-0001/revisions", `{"body":"b"}`, owner), http.StatusGone, "appendRevision")
	rec = call(http.MethodGet, "/notes?page=estate/conventions", "", owner)
	mustStatus(t, rec, http.StatusOK, "listNotes")
	if l := decodeInto[wire.NoteList](t, rec); len(l.Items) != 0 {
		t.Errorf("a deleted note is still listed: %+v", l.Items)
	}
	// And out of the backlink list it was the only entry of, by the store's
	// join rather than by this handler.
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001/backlinks", "", owner), http.StatusGone, "listNoteBacklinks")
	rec = call(http.MethodGet, "/notes/CHR-0002/backlinks", "", owner)
	mustStatus(t, rec, http.StatusOK, "listNoteBacklinks")
	if back := decodeInto[wire.BacklinkList](t, rec); len(back.Items) != 0 {
		t.Errorf("a deleted source is still a backlink: %+v", back.Items)
	}

	// Undelete, and the same URL answers 200 again.
	if err := st.UndeleteNote(ctx, note.ID, created.Revision.AuthorId); err != nil {
		t.Fatalf("UndeleteNote: %v", err)
	}
	mustStatus(t, call(http.MethodGet, "/notes/CHR-0001", "", owner), http.StatusOK, "getNote")
	_ = apitest.Doc(t)
}

func mustPage(t *testing.T, st *store.Store, ctx context.Context, path string) uuid.UUID {
	t.Helper()
	p, err := st.PageByPath(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func mustScribe(t *testing.T, st *store.Store, ctx context.Context) store.User {
	t.Helper()
	u, err := st.Scribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
