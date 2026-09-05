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

	// CHRN-34.
	deferred  []triage.DeferredItem
	held      triage.DeferredItem
	gotMemoID uuid.UUID
	gotReason string
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

func (f *fakeTriage) Hold(_ context.Context, actor store.User, memoID uuid.UUID, reason string) (triage.DeferredItem, error) {
	f.calls++
	f.gotActor, f.gotMemoID, f.gotReason = actor, memoID, reason
	if f.err != nil {
		return triage.DeferredItem{}, f.err
	}
	return f.held, nil
}

func (f *fakeTriage) Release(_ context.Context, actor store.User, memoID uuid.UUID) error {
	f.calls++
	f.gotActor, f.gotMemoID = actor, memoID
	return f.err
}

func (f *fakeTriage) Deferred(_ context.Context, actor store.User, limit int) ([]triage.DeferredItem, error) {
	f.calls++
	f.gotActor, f.gotLimit = actor, limit
	return f.deferred, f.err
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

// ============================================================================
// CHRN-34 — the two escapes.
// ============================================================================

// The status codes are the contract here, and two of them are deliberate
// choices rather than the obvious answer. Both exist so a client can retry
// without having to tell success from failure.
func TestHoldAndReleaseAnswerRetriesAsSuccess(t *testing.T) {
	memo := uuid.New()

	t.Run("a hold answers 200, not 201", func(t *testing.T) {
		tr := &fakeTriage{held: triage.DeferredItem{MemoID: memo, AgeSeconds: 0}}
		h, tok := signedInTriage(t, true, tr)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/hold",
			`{"memo_id":"`+memo.String()+`","reason":"not now"}`), tok))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 — holding is idempotent, so there is no "+
				"created-versus-existing distinction to carry", w.Code)
		}
		if tr.gotMemoID != memo || tr.gotReason != "not now" {
			t.Errorf("service got (%s, %q), want (%s, %q)", tr.gotMemoID, tr.gotReason, memo, "not now")
		}
	})

	t.Run("releasing an unheld memo is 204, not 404", func(t *testing.T) {
		// The state the caller asked for is the state that exists. Answering
		// 404 would make a client treat a successful release as a failure.
		tr := &fakeTriage{err: store.ErrNotHeld}
		h, tok := signedInTriage(t, true, tr)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/release",
			`{"memo_id":"`+memo.String()+`"}`), tok))

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", w.Code)
		}
	})
}

// A memo past deferral is 409 and not 404: it is real and the caller may see
// it, so telling them it does not exist would be a lie they cannot act on.
func TestHoldingAMemoPastDeferralIs409(t *testing.T) {
	tr := &fakeTriage{err: store.ErrNotHoldable}
	h, tok := signedInTriage(t, true, tr)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/hold",
		`{"memo_id":"`+uuid.New().String()+`"}`), tok))

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", w.Code)
	}
}

// AND A MEMO THAT IS NOT YOURS IS 404, never 403. The service collapses "no
// such memo" and "not yours" into one error precisely so a caller cannot tell
// them apart; answering 403 here would hand the distinction straight back and
// turn the endpoint into an existence oracle.
func TestHoldingSomeoneElsesMemoIs404(t *testing.T) {
	for _, path := range []string{"/triage/hold", "/triage/release"} {
		tr := &fakeTriage{err: triage.ErrNoSuchMemo}
		h, tok := signedInTriage(t, false, tr)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withToken(jsonReq("POST", path,
			`{"memo_id":"`+uuid.New().String()+`"}`), tok))

		if w.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, w.Code)
		}
	}
}

// An unknown field is refused here for the same reason it is on the accept
// path: a misspelled field that decoded to its zero value would look like it
// worked. `reason` is the one that matters — a deferral whose note silently
// vanished is one nobody can decide in three weeks.
func TestAHoldCarryingAnUnknownFieldIsRefused(t *testing.T) {
	tr := &fakeTriage{}
	h, tok := signedInTriage(t, true, tr)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(jsonReq("POST", "/triage/hold",
		`{"memo_id":"`+uuid.New().String()+`","resaon":"typo"}`), tok))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if tr.calls != 0 {
		t.Error("the hold reached the service despite an unknown field")
	}
}

// A missing memo_id is refused before the service is reached, so a zero UUID
// never becomes a lookup.
func TestAHoldWithoutAMemoIDIsRefused(t *testing.T) {
	for _, path := range []string{"/triage/hold", "/triage/release"} {
		tr := &fakeTriage{}
		h, tok := signedInTriage(t, true, tr)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withToken(jsonReq("POST", path, `{}`), tok))

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400", path, w.Code)
		}
		if tr.calls != 0 {
			t.Errorf("%s reached the service with no memo_id", path)
		}
	}
}

// The deferred listing clamps its limit exactly as the batch does. The two are
// read by one screen, and an asymmetry would be a client-side special case for
// no reason.
func TestTheDeferredListingClampsItsLimitLikeTheBatch(t *testing.T) {
	tr := &fakeTriage{}
	h, tok := signedInTriage(t, true, tr)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, withToken(httptest.NewRequest("GET", "/triage/deferred?limit=1000", nil), tok))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if tr.gotLimit != triage.MaxLimit {
		t.Errorf("limit = %d, want it clamped to %d", tr.gotLimit, triage.MaxLimit)
	}
}
