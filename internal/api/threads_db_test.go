package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-99's Done-when, and E6's exit, over the REAL store: two authenticated
// sessions hold a threaded exchange over HTTP with an agent participant,
// unread is correct in both, and resolving a thread returns the note it
// produced with the link readable from either end. The rules under test —
// CH091, CH093, CH101, the marker clamp, the resolver test in the resolve
// statement — are the database's, which is why the fakes are not enough.

func realThreads(t *testing.T) (context.Context, http.Handler, map[string]string, map[string]store.User) {
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
		t.Fatal(err)
	}
	scribe, err := st.EnsureAgent(ctx, store.ScribeEmail, store.ScribeDisplayName)
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.CreateUser(ctx, "second@example.com", "Second", store.KindPerson)
	if err != nil {
		t.Fatal(err)
	}
	users := map[string]store.User{"owner": owner, "scribe": scribe, "second": second}
	tokens := map[string]string{}
	for _, name := range []string{"owner", "owner2", "scribe", "second"} {
		u := users[strings.TrimSuffix(name, "2")]
		tok, err := st.MintToken(ctx, u.ID, store.TokenSession, name, nil)
		if err != nil {
			t.Fatal(err)
		}
		tokens[name] = tok
	}
	h := NewRouter(Deps{
		DB: st, Accounts: st, Logger: discardLogger(), Version: "test", SecureCookies: true,
		Wiki: st, Threads: st, LocalReferences: st,
	})
	return ctx, h, tokens, users
}

func TestAHumanAndAnAgentHoldAThreadOverTheRealStore(t *testing.T) {
	_, h, tokens, users := realThreads(t)
	call := func(token, method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		var r *http.Request
		if body != "" {
			r = jsonReq(method, path, body)
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("Authorization", "Bearer "+tokens[token])
		h.ServeHTTP(rec, r)
		return rec
	}
	// The thread's ref is taken from the open, not assumed: a refused open
	// still consumes a discussion number, because a Postgres sequence is not
	// transactional, and the agent's refusal below burns one before the
	// person's open lands. A number is a permanent handle and a gap in the
	// numbering is nothing; a test that hardcoded DSC-0001 was wrong about
	// the store, not the store about the test.
	var ref string
	unread := func(token string) *int {
		rec := call(token, http.MethodGet, "/discussions/"+ref, "")
		mustStatus(t, rec, http.StatusOK, "getDiscussion")
		return decodeInto[wire.Thread](t, rec).Unread
	}
	want := func(token string, n int) {
		t.Helper()
		got := unread(token)
		if got == nil || *got != n {
			t.Errorf("%s sees unread %v, want %d", token, got, n)
		}
	}

	mustStatus(t, call("owner", http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")

	// An agent cannot open a thread — CH091 at seq 1.
	mustStatus(t, call("scribe", http.MethodPost, "/discussions", `{"title":"t","body":"b"}`), http.StatusConflict, "openDiscussion")

	rec := call("owner", http.MethodPost, "/discussions", `{"title":"How long do we keep audio","page":"estate","body":"@scribe what gates the deletion?"}`)
	mustStatus(t, rec, http.StatusCreated, "openDiscussion")
	opened := decodeInto[wire.Thread](t, rec)
	if !strings.HasPrefix(opened.Discussion.Ref, "DSC-") || opened.Unread == nil || *opened.Unread != 0 {
		t.Fatalf("opened = %+v", opened)
	}
	ref = opened.Discussion.Ref

	// The Scribe replies through the same door, and CH092 attributes it.
	rec = call("scribe", http.MethodPost, "/discussions/"+ref+"/turns", `{"body":"A durable transcript, never the calendar."}`)
	mustStatus(t, rec, http.StatusCreated, "appendTurn")
	if turn := decodeInto[wire.Turn](t, rec); turn.AuthorKind != wire.TurnAuthorKindAgent || turn.Seq != 2 || turn.AuthorId != users["scribe"].ID {
		t.Errorf("agent turn = %+v", turn)
	}
	// Exactly one unread for the person, in both of their sessions.
	want("owner", 1)
	want("owner2", 1)
	// The agent cannot go again, and sees no unread.
	rec = call("scribe", http.MethodPost, "/discussions/"+ref+"/turns", `{"body":"and"}`)
	mustStatus(t, rec, http.StatusConflict, "appendTurn")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeAgentAfterAgent {
		t.Errorf("code = %q", e.Code)
	}
	if got := unread("scribe"); got != nil {
		t.Errorf("the agent was handed unread %d", *got)
	}

	// Marking read in one session is reflected in the other, through the
	// real clamp: 999 becomes 2, and the next turn is exactly one unread.
	mustStatus(t, call("owner2", http.MethodPost, "/discussions/"+ref+"/read", `{"through_seq":999}`), http.StatusNoContent, "markRead")
	want("owner", 0)
	rec = call("owner", http.MethodGet, "/discussions/unread", "")
	mustStatus(t, rec, http.StatusOK, "listUnread")
	if badge := decodeInto[wire.UnreadList](t, rec); len(badge.Items) != 0 {
		t.Errorf("badge = %+v", badge.Items)
	}
	mustStatus(t, call("second", http.MethodPost, "/discussions/"+ref+"/turns", `{"body":"Agreed."}`), http.StatusCreated, "appendTurn")
	want("owner", 1)
	want("owner2", 1)
	want("second", 0)
	rec = call("owner", http.MethodGet, "/discussions/unread", "")
	mustStatus(t, rec, http.StatusOK, "listUnread")
	if badge := decodeInto[wire.UnreadList](t, rec); len(badge.Items) != 1 || badge.Items[0].Unread != 1 {
		t.Errorf("badge = %+v", badge.Items)
	}

	// Reading is not joining (CHRN-45 ruling 8) and an agent has no marker
	// (CH101), both from the store.
	rec = call("scribe", http.MethodPost, "/discussions/"+ref+"/read", `{"through_seq":3}`)
	mustStatus(t, rec, http.StatusForbidden, "markRead")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeAgentHasNoMarker {
		t.Errorf("code = %q", e.Code)
	}

	// The agent is put on the thread by a person, and listed as an agent
	// with no marker.
	mustStatus(t, call("scribe", http.MethodPost, "/discussions/"+ref+"/participants", `{"user_id":"`+users["scribe"].ID.String()+`"}`), http.StatusForbidden, "addParticipant")
	mustStatus(t, call("owner", http.MethodPost, "/discussions/"+ref+"/participants", `{"user_id":"`+users["scribe"].ID.String()+`"}`), http.StatusNoContent, "addParticipant")
	rec = call("owner", http.MethodGet, "/discussions/"+ref, "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	th := decodeInto[wire.Thread](t, rec)
	found := false
	for _, p := range th.Participants {
		if p.UserId == users["scribe"].ID {
			found = true
			if p.Kind != wire.ParticipantKindAgent || p.LastReadSeq != nil || p.AddedBy != users["owner"].ID {
				t.Errorf("agent participant = %+v", p)
			}
		}
	}
	if !found {
		t.Errorf("the agent is not a participant: %+v", th.Participants)
	}
	if len(th.Turns) != 3 || th.Turns[1].AuthorKind != wire.TurnAuthorKindAgent {
		t.Errorf("turns = %+v", th.Turns)
	}

	// Resolve into a new note, by a person only; the link reads from both
	// ends, over HTTP, on both surfaces.
	mustStatus(t, call("scribe", http.MethodPost, "/discussions/"+ref+"/resolve", `{"into":"nothing"}`), http.StatusForbidden, "resolveDiscussion")
	rec = call("owner", http.MethodPost, "/discussions/"+ref+"/resolve",
		`{"into":"new_note","page":"estate","title":"Retention","body":"Deletion is gated on a durable transcript, never on the calendar."}`)
	mustStatus(t, rec, http.StatusOK, "resolveDiscussion")
	res := decodeInto[wire.DiscussionResolution](t, rec)
	if res.Discussion.Resolved == nil || res.Discussion.Resolved.Note == nil || res.Discussion.Resolved.By != users["owner"].ID {
		t.Fatalf("resolved = %+v", res.Discussion.Resolved)
	}
	noteRef := *res.Discussion.Resolved.Note
	if res.Note == nil || res.Note.Ref != noteRef || len(res.Note.ResolvedFrom) != 1 || res.Note.ResolvedFrom[0].Ref != ref || res.Note.Revision.Seq != 1 {
		t.Errorf("the note end = %+v", res.Note)
	}
	rec = call("second", http.MethodGet, "/notes/"+noteRef, "")
	mustStatus(t, rec, http.StatusOK, "getNote")
	if n := decodeInto[wire.Note](t, rec); len(n.ResolvedFrom) != 1 || n.ResolvedFrom[0].Ref != ref || n.Revision.ConfirmedBy == nil || *n.Revision.ConfirmedBy != users["owner"].ID {
		t.Errorf("GET /notes/%s = %+v", noteRef, n)
	}
	rec = call("second", http.MethodGet, "/discussions/"+ref, "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	if th := decodeInto[wire.Thread](t, rec); deref(th.Discussion.Resolved.Note) != noteRef {
		t.Errorf("GET /discussions/%s = %+v", ref, th.Discussion)
	}

	// CH093: no more turns. CH080: the conclusion is not rewritten.
	rec = call("owner", http.MethodPost, "/discussions/"+ref+"/turns", `{"body":"late"}`)
	mustStatus(t, rec, http.StatusConflict, "appendTurn")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeDiscussionResolved {
		t.Errorf("code = %q", e.Code)
	}
	rec = call("owner", http.MethodPost, "/discussions/"+ref+"/resolve", `{"into":"new_note","page":"estate","title":"x","body":"y"}`)
	mustStatus(t, rec, http.StatusConflict, "resolveDiscussion")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeResolutionFixed {
		t.Errorf("code = %q", e.Code)
	}
	// And the resolved thread is still listed on its page.
	rec = call("owner", http.MethodGet, "/discussions?page=estate", "")
	mustStatus(t, rec, http.StatusOK, "listDiscussions")
	if l := decodeInto[wire.DiscussionList](t, rec); len(l.Items) != 1 || l.Items[0].Resolved == nil {
		t.Errorf("list = %+v", l)
	}
}
