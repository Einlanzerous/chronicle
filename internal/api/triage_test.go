package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/triage"
)

// fakeTriage records what the handlers pass through and answers with whatever a
// test needs back. The service's own behaviour is tested against a real
// database in internal/triage; these cases are about the HTTP contract.
type fakeTriage struct {
	items   []triage.BatchItem
	results []triage.Result
	err     error

	gotLimit int
	gotActor store.User
	gotItems []triage.Item
	calls    int
}

func (f *fakeTriage) Batch(_ context.Context, actor store.User, limit int) ([]triage.BatchItem, error) {
	f.calls++
	f.gotActor, f.gotLimit = actor, limit
	return f.items, f.err
}

func (f *fakeTriage) Apply(_ context.Context, actor store.User, items []triage.Item) ([]triage.Result, error) {
	f.calls++
	f.gotActor, f.gotItems = actor, items
	if f.err != nil {
		return nil, f.err
	}
	out := f.results
	if out == nil {
		out = make([]triage.Result, len(items))
		for i, it := range items {
			out[i] = triage.Result{MemoID: it.MemoID, Status: triage.StatusApplied}
		}
	}
	return out, nil
}

func (f *fakeTriage) Admin(context.Context) (triage.AdminReport, error) {
	f.calls++
	return triage.AdminReport{}, f.err
}

func triageRouter(f *fakeAccounts, tr Triage) http.Handler {
	return NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test",
		Triage: tr,
	})
}

// signedIn returns a router and a token for an account.
func signedInTriage(t *testing.T, owner bool, tr Triage) (http.Handler, string) {
	t.Helper()
	f := newFakeAccounts()
	u := person("someone@example.test", owner)
	f.sessions["tok"] = u
	return triageRouter(f, tr), "tok"
}

func withToken(r *http.Request, token string) *http.Request {
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

// AN UNKNOWN FIELD IS REFUSED. This is the shape assertion for the rule that
// there IS NO VERB FIELD: append-versus-supersede is CHRN-39's question and
// CHRN-32's contract cannot express it, so an item claiming to carry one must
// be told rather than silently ignored.
//
// The dangerous case is not `verb` — it is a MISSPELLED `generation`, which
// would decode to its zero value and turn the echo into a no-check while the
// request looked like it worked.
func TestAnItemCarryingAnUnknownFieldIsRefused(t *testing.T) {
	memo := uuid.New()
	for _, body := range []string{
		`{"items":[{"memo_id":"` + memo.String() + `","proposer":"p","generation":1,"verb":"append"}]}`,
		`{"items":[{"memo_id":"` + memo.String() + `","proposer":"p","generatoin":1}]}`,
		`{"items":[{"memo_id":"` + memo.String() + `","proposer":"p","override":{"destination":"TICKET","project":"CHRN"}}]}`,
	} {
		tr := &fakeTriage{}
		h, tok := signedInTriage(t, true, tr)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/accept", body), tok))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d for %s, want 400", w.Code, body)
		}
		if tr.calls != 0 {
			t.Fatalf("the batch reached the service despite an unknown field: %s", body)
		}
	}
}

// The well-formed shape IS accepted, or the test above would pass against an
// endpoint that refuses everything.
func TestAWellFormedBatchReachesTheService(t *testing.T) {
	memo := uuid.New()
	tr := &fakeTriage{}
	h, tok := signedInTriage(t, true, tr)

	body := `{"items":[
	  {"memo_id":"` + memo.String() + `","proposer":"ollama/g@v1","generation":3},
	  {"memo_id":"` + uuid.NewString() + `","proposer":"ollama/g@v1","generation":null,
	   "override":{"destination":"TICKET","project_key":"CHRN","ticket_type":"task",
	               "title":"By hand","description":"typed"}}]}`

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/accept", body), tok))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}

	if len(tr.gotItems) != 2 {
		t.Fatalf("the service got %d items, want 2", len(tr.gotItems))
	}
	first := tr.gotItems[0]
	if first.MemoID != memo || first.Proposer != "ollama/g@v1" ||
		first.Generation == nil || *first.Generation != 3 || first.Override != nil {
		t.Fatalf("item 0 = %+v", first)
	}
	// NULL IS CARRIED AS NULL. An item that echoed "I saw no proposal" must not
	// arrive as generation 0, which is a different claim and one the server
	// would check against the wrong thing.
	if tr.gotItems[1].Generation != nil {
		t.Fatalf("a null generation arrived as %v", *tr.gotItems[1].Generation)
	}
	if tr.gotItems[1].Override == nil || tr.gotItems[1].Override.ProjectKey != "CHRN" {
		t.Fatalf("item 1 = %+v", tr.gotItems[1])
	}

	var got acceptResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Results) != 2 {
		t.Fatalf("got %d results, want one per item", len(got.Results))
	}
}

// 200 WITH PER-ITEM RESULTS, EVEN WHEN EVERY ITEM FAILED. The request was
// understood and answered; a 4xx or 5xx would tell a client to retry a set it
// has already been given precise answers about.
func TestABatchWhereEverythingFailedStillAnswers200(t *testing.T) {
	memo := uuid.New()
	tr := &fakeTriage{results: []triage.Result{
		{MemoID: memo, Status: triage.StatusRefused, Reason: "a DISCARD is never accepted as shown"},
	}}
	h, tok := signedInTriage(t, true, tr)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/accept",
		`{"items":[{"memo_id":"`+memo.String()+`","proposer":"p","generation":1}]}`), tok))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got acceptResponse
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Results[0].Status != triage.StatusRefused || got.Results[0].Reason == "" {
		t.Fatalf("result = %+v, want a refusal with a reason", got.Results[0])
	}
}

// A batch bigger than a screen is a client bug and answers 400, because there
// is nothing per-item to say about items that were never attempted.
func TestAnOversizedBatchIs400(t *testing.T) {
	tr := &fakeTriage{err: triage.ErrTooLarge}
	h, tok := signedInTriage(t, true, tr)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/accept",
		`{"items":[{"memo_id":"`+uuid.NewString()+`","proposer":"p","generation":1}]}`), tok))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestAnEmptyBatchIsRefused(t *testing.T) {
	tr := &fakeTriage{}
	h, tok := signedInTriage(t, true, tr)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/accept", `{"items":[]}`), tok))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if tr.calls != 0 {
		t.Fatal("an empty batch reached the service")
	}
}

// The Content-Type check is shared with the sign-in route because it is what
// closes login CSRF, and an endpoint that skipped it would be a second answer
// to the same question.
func TestTheAcceptEndpointRequiresJSON(t *testing.T) {
	tr := &fakeTriage{}
	h, tok := signedInTriage(t, true, tr)
	r := httptest.NewRequest("POST", "/triage/accept", strings.NewReader(`{"items":[]}`))
	r.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(r, tok))
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", w.Code)
	}
}

// The limit is CLAMPED rather than refused: a client asking for a hundred cards
// gets the cap and is told what it got, and an over-eager client is never
// unable to triage anything at all.
func TestTheBatchLimitIsClampedAndEchoed(t *testing.T) {
	tr := &fakeTriage{}
	h, tok := signedInTriage(t, true, tr)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(httptest.NewRequest("GET", "/triage/batch?limit=1000", nil), tok))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if tr.gotLimit != triage.MaxLimit {
		t.Fatalf("the service got limit %d, want the cap %d", tr.gotLimit, triage.MaxLimit)
	}

	var got batchResponse
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Limit != triage.MaxLimit {
		t.Fatalf("echoed limit = %d, want the cap the POST also enforces", got.Limit)
	}

	// A nonsense limit is a client bug, not something to guess about.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, withToken(httptest.NewRequest("GET", "/triage/batch?limit=-1", nil), tok))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d for limit=-1, want 400", w.Code)
	}
}

// The session's account reaches the service, because the author scoping is the
// service's to apply and it needs to know who is asking. Nothing about the
// request body can name a different one.
func TestTheActorComesFromTheSessionAndNotTheRequest(t *testing.T) {
	tr := &fakeTriage{}
	f := newFakeAccounts()
	u := person("member@example.test", false)
	f.sessions["tok"] = u
	h := triageRouter(f, tr)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(httptest.NewRequest("GET", "/triage/batch", nil), "tok"))
	if tr.gotActor.ID != u.ID {
		t.Fatalf("actor = %s, want the session's account %s", tr.gotActor.ID, u.ID)
	}
	if tr.gotActor.IsAdmin() {
		t.Fatal("a member reached the service as an admin")
	}
}

// Both triage routes need a session; the admin report needs the owner. A member
// reading every author's decisions would be a corpus-wide read behind a
// per-author endpoint.
func TestTheTriageRoutesAreGuarded(t *testing.T) {
	tr := &fakeTriage{}

	// Unauthenticated: nothing.
	h := triageRouter(newFakeAccounts(), tr)
	for _, rt := range []struct{ method, path string }{
		{"GET", "/triage/batch"}, {"POST", "/triage/accept"}, {"GET", "/admin/triage"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, jsonReq(rt.method, rt.path, `{"items":[]}`))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401", rt.method, rt.path, w.Code)
		}
	}
	if tr.calls != 0 {
		t.Fatal("an unauthenticated request reached the service")
	}

	// A member: the two triage routes, but not the report.
	memberRouter, memberTok := signedInTriage(t, false, tr)
	w := httptest.NewRecorder()
	memberRouter.ServeHTTP(w, withToken(httptest.NewRequest("GET", "/triage/batch", nil), memberTok))
	if w.Code != http.StatusOK {
		t.Fatalf("a member cannot read their own triage batch: %d", w.Code)
	}
	w = httptest.NewRecorder()
	memberRouter.ServeHTTP(w, withToken(httptest.NewRequest("GET", "/admin/triage", nil), memberTok))
	if w.Code != http.StatusForbidden {
		t.Fatalf("GET /admin/triage for a member = %d, want 403", w.Code)
	}
}

// UNCONFIGURED ANSWERS 503 NAMING THE VARIABLES, not 404. "Not configured here"
// and "wrong URL" are different facts and a client must be able to tell them
// apart — the same shape /admin/storage and /admin/transcription already use.
func TestTriageWithoutConfigurationAnswers503(t *testing.T) {
	f := newFakeAccounts()
	f.sessions["tok"] = person("someone@example.test", true)
	h := triageRouter(f, nil)

	for _, rt := range []struct{ method, path string }{
		{"GET", "/triage/batch"}, {"POST", "/triage/accept"}, {"GET", "/admin/triage"},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withToken(jsonReq(rt.method, rt.path, `{"items":[]}`), "tok"))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s = %d, want 503", rt.method, rt.path, w.Code)
		}
		var body map[string]string
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		if !strings.Contains(body["detail"], "CHRONICLE_SWITCHYARD_URL") {
			t.Fatalf("%s: detail = %q, want it to name what turns triage on", rt.path, body["detail"])
		}
	}
}
