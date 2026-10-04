package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-116's second Done-when: NO RESPONSE TO ANY NON-OWNER EVER CONTAINS A
// TRANSCRIPT HIT, driven through the router and the real store — "a test that
// builds its own input cannot prove a property of the caller".
//
// Every row is written through the store's own write paths and every request
// goes through NewRouter with a real session, so what is under test is the
// policy table, the generated registration, the handler, the mapper and the
// statement together. A fake store would prove the mapper.
//
// Seeded:
//
//	transcript A   "chiffchaff" and "zebrafinch"   the OWNER's memo — another author's, from the callers' side
//	transcript B   "chiffchaff" and "zebrafinch"   `other`'s memo — the caller's OWN
//	note N1        "chiffchaff", beside hostile markup
//	note N2        "chiffchaff", then soft-deleted
//
// `zebrafinch` is the sentinel: it is in both transcripts and in no note, so
// its presence anywhere in a non-owner answer means transcript text got out.
//
// Transcript B is there on purpose. The caller's own transcript is absent
// too: this ticket's scope is notes, and "own transcripts" is the delegation
// question it leaves to a ruling.
func TestNoNonOwnerSearchResponseCarriesATranscriptHit(t *testing.T) {
	rig := realMemos(t)

	const needle, sentinel = "chiffchaff", "zebrafinch"

	memoA := rig.memo(t, rig.owner, "owner audio bytes", "owner.m4a")
	memoB := rig.memo(t, rig.other, "other audio bytes", "other.m4a")
	rig.transcribe(t, memoA, "the owner heard a chiffchaff and then a zebrafinch by the gate",
		"whisper.cpp/small.en", false, 61000)
	rig.transcribe(t, memoB, "I also heard a chiffchaff and a zebrafinch on the walk home",
		"whisper.cpp/small.en", false, 42000)

	n1, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.owner.ID, ConfirmedBy: rig.owner.ID,
		Title: "Birds by the gate",
		// Two hostile fragments, because Postgres treats them differently.
		// ts_headline DROPS a well-formed tag — its parser reads `<img …>` as
		// a tag token and leaves it out of the fragment — so the first never
		// reaches the mapper at all. It keeps anything that is not one: an
		// unclosed tag and a bare ampersand arrive verbatim, and those are
		// what safeSnippet has to escape. Measured, not assumed: with only
		// the first in the body this test saw no `<img` and no `&lt;img`.
		Body: `a chiffchaff sang <img src=x onerror="steal()"> from the hedge & <img src=y onerror="steal()" all morning`,
	})
	if err != nil {
		t.Fatalf("CreateNote N1: %v", err)
	}
	n2, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.owner.ID, ConfirmedBy: rig.owner.ID,
		Title: "Withdrawn", Body: "a second chiffchaff, in a note that is then deleted",
	})
	if err != nil {
		t.Fatalf("CreateNote N2: %v", err)
	}
	if err := rig.st.SoftDeleteNote(rig.ctx, n2.ID, rig.owner.ID); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	// What must never appear in a non-owner's answer. The two memo ids and
	// the sentinel are the data; the rest are the words a transcript hit is
	// described in, as keys or as values.
	banned := []string{
		memoA.ID.String(), memoB.ID.String(), sentinel,
		"memo_id", "model", "kind", "transcript",
	}
	carries := func(t *testing.T, what, body string) {
		t.Helper()
		lower := strings.ToLower(body)
		for _, b := range banned {
			if strings.Contains(lower, strings.ToLower(b)) {
				t.Errorf("%s contains %q:\n%s", what, b, body)
			}
		}
	}

	// ── THE POSITIVE CONTROL, FIRST ─────────────────────────────────────────
	// Every assertion below is an absence, and an absence proves nothing if
	// the thing was never findable. The owner's search returns BOTH memos as
	// transcript hits for the needle and for the sentinel, so the transcripts
	// are in the corpus, indexed, and matched by exactly these queries. If the
	// seed were wrong the test fails here rather than passing for the wrong
	// reason.
	for _, q := range []string{needle, sentinel} {
		rec := rig.get(t, "/search?q="+url.QueryEscape(q), rig.ownerTok, "search", http.StatusOK)
		found := map[string]bool{}
		for _, h := range decodeInto[wire.SearchResults](t, rec).Items {
			if h.Kind == wire.SearchHitKindTranscript && h.MemoId != nil {
				found[h.MemoId.String()] = true
			}
		}
		if !found[memoA.ID.String()] || !found[memoB.ID.String()] || len(found) != 2 {
			t.Fatalf("control: the owner's /search?q=%s found transcripts %v, want both %s and %s",
				q, found, memoA.ID, memoB.ID)
		}
	}
	// And the banned list is one the checker can trip on: the owner's raw
	// answer carries every word of it. A `carries` that could not fail would
	// pass this test for any server.
	ownerBody := strings.ToLower(rig.get(t, "/search?q="+needle, rig.ownerTok, "search", http.StatusOK).Body.String())
	for _, b := range banned {
		if !strings.Contains(ownerBody, strings.ToLower(b)) {
			t.Fatalf("control: the owner's /search answer does not contain %q, so its absence below would mean nothing:\n%s", b, ownerBody)
		}
	}

	requests := []struct {
		name      string
		q         string
		limit     string // the separate `limit` parameter, not part of q
		wantLimit int
		wantN1    bool
	}{
		{"the needle", needle, "", defaultSearch, true},
		{"the sentinel alone", sentinel, "", defaultSearch, false},
		{"a phrase OR the sentinel", `"` + needle + `" OR ` + sentinel, "", defaultSearch, true},
		{"the needle at the cap", needle, "100", maxSearch, true},
	}
	callers := []struct {
		name, token string
	}{
		{"a second person", rig.otherTok},
		{"an agent", rig.agentTok},
	}

	for _, caller := range callers {
		for _, req := range requests {
			t.Run(caller.name+"/"+req.name, func(t *testing.T) {
				query := "?q=" + url.QueryEscape(req.q)
				if req.limit != "" {
					query += "&limit=" + req.limit
				}

				// 200, and the body is the document's NoteSearchResults.
				rec := rig.get(t, "/notes/search"+query, caller.token, "searchNotes", http.StatusOK)
				res := decodeInto[wire.NoteSearchResults](t, rec)
				if res.Query != req.q || res.Limit != req.wantLimit {
					t.Errorf("echo = (%q, %d), want (%q, %d)", res.Query, res.Limit, req.q, req.wantLimit)
				}

				// Exactly the one live note, or nothing. Errorf and not Fatalf:
				// a wrong item count must not stop the absence checks below
				// from saying WHAT got out.
				onlyN1 := len(res.Items) == 1 && res.Items[0].Ref == n1.Ref()
				switch {
				case req.wantN1 && !onlyN1:
					t.Errorf("items = %+v, want exactly %s (the soft-deleted one is %s)", res.Items, n1.Ref(), n2.Ref())
				case !req.wantN1 && len(res.Items) != 0:
					t.Errorf("items = %+v, want none: %q is in no note", res.Items, req.q)
				}
				if req.wantN1 && onlyN1 {
					hit := res.Items[0]
					if hit.Title == nil || *hit.Title != "Birds by the gate" {
						t.Errorf("title = %v", hit.Title)
					}
					// The CHRN-98 escaping, through the real ts_headline.
					if !strings.Contains(hit.Snippet, "<b>"+needle+"</b>") {
						t.Errorf("snippet does not mark the match: %q", hit.Snippet)
					}
					if !strings.Contains(hit.Snippet, "&lt;img src=y") || !strings.Contains(hit.Snippet, "&amp;") {
						t.Errorf("the markup ts_headline kept did not arrive escaped: %q", hit.Snippet)
					}
					if strings.Contains(hit.Snippet, "<img") || strings.Contains(hit.Snippet, `onerror="`) {
						t.Errorf("raw markup reached the wire: %q", hit.Snippet)
					}
				}

				// THE PROPERTY. The response echoes the caller's own query in
				// its top-level `query`, so a query that names the sentinel
				// puts the sentinel in the raw body by the caller's own hand.
				// That one field is removed and NOTHING ELSE is exempt: the
				// checks run over everything the server chose to say.
				var generic map[string]json.RawMessage
				if err := json.Unmarshal(rec.Body.Bytes(), &generic); err != nil {
					t.Fatalf("body is not an object: %v", err)
				}
				if _, ok := generic["query"]; !ok {
					t.Fatal("the response has no top-level query to remove")
				}
				delete(generic, "query")
				rest, err := json.Marshal(generic)
				if err != nil {
					t.Fatal(err)
				}
				carries(t, "GET /notes/search"+query+" (query echo removed)", string(rest))

				// And the item keys, read as keys rather than as substrings,
				// are the five NoteSearchHit declares.
				var items []map[string]json.RawMessage
				if err := json.Unmarshal(generic["items"], &items); err != nil {
					t.Fatalf("items: %v", err)
				}
				for _, item := range items {
					for key := range item {
						switch key {
						case "ref", "title", "snippet", "rank", "created_at":
						default:
							t.Errorf("an item carries the undeclared key %q", key)
						}
					}
				}

				// The owner's search, with the same token, is still refused —
				// and the refusal says nothing either. It has no query echo,
				// so the checks run on it raw.
				refused := rig.get(t, "/search"+query, caller.token, "search", http.StatusForbidden)
				if got := decodeInto[wire.Error](t, refused).Code; got != codeOwnerOnly {
					t.Errorf("/search refusal code = %q, want %q", got, codeOwnerOnly)
				}
				carries(t, "GET /search"+query+" (the 403)", refused.Body.String())
			})
		}
	}

	// The owner calling the notes search gets notes too: the route is
	// notes-only for everybody, not transcript-free for some.
	rec := rig.get(t, "/notes/search?q="+needle, rig.ownerTok, "searchNotes", http.StatusOK)
	if res := decodeInto[wire.NoteSearchResults](t, rec); len(res.Items) != 1 || res.Items[0].Ref != n1.Ref() {
		t.Errorf("the owner's /notes/search = %+v, want exactly %s", res.Items, n1.Ref())
	}

	// Anonymous is refused before any of it.
	anon := httptest.NewRecorder()
	rig.h.ServeHTTP(anon, httptest.NewRequest(http.MethodGet, "/notes/search?q="+needle, nil))
	mustStatus(t, anon, http.StatusUnauthorized, "searchNotes")
}
