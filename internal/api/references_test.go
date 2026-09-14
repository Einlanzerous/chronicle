package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/amber"
	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// CHRN-104's Done-when, driven end to end: a batch of references in one call
// against REGISTERED transports -- the real switchyard.Client and amber.Client
// over fake upstreams, through the real resolver -- and the local namespace
// against a fake store. Every response goes through apitest.Conform.

// ---------------------------------------------------------------------------
// Harness.
// ---------------------------------------------------------------------------

// refClock lets a TTL be crossed without sleeping.
type refClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *refClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *refClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// fakeLocal is the notes-and-discussions slice of the store: one live note,
// one soft-deleted note whose title must never surface, and one open thread.
type fakeLocal struct {
	notes       map[int64]store.Note
	revisions   map[uuid.UUID]store.NoteRevision
	discussions map[int64]store.Discussion
	err         error
}

// withheldTitle is on the deleted note's current revision. A tombstone carries
// no title and no body, and this is the string that proves it.
const withheldTitle = "WITHHELD-the-deleted-note-had-a-title"

func newFakeLocal() *fakeLocal {
	live := store.Note{ID: uuid.New(), Number: 311}
	deletedAt := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	deletedBy := uuid.New()
	gone := store.Note{ID: uuid.New(), Number: 400, DeletedAt: &deletedAt, DeletedBy: &deletedBy}
	return &fakeLocal{
		notes: map[int64]store.Note{311: live, 400: gone},
		revisions: map[uuid.UUID]store.NoteRevision{
			live.ID: {ID: uuid.New(), NoteID: live.ID, Title: "Retention pruner design"},
			gone.ID: {ID: uuid.New(), NoteID: gone.ID, Title: withheldTitle},
		},
		discussions: map[int64]store.Discussion{
			7: {ID: uuid.New(), Number: 7, Title: "Should the pruner gate on the calendar?"},
		},
	}
}

func (f *fakeLocal) NoteByNumber(_ context.Context, n int64) (store.Note, error) {
	if f.err != nil {
		return store.Note{}, f.err
	}
	note, ok := f.notes[n]
	if !ok {
		return store.Note{}, store.ErrNotFound
	}
	return note, nil
}

func (f *fakeLocal) CurrentRevision(_ context.Context, id uuid.UUID) (store.NoteRevision, error) {
	if f.err != nil {
		return store.NoteRevision{}, f.err
	}
	rev, ok := f.revisions[id]
	if !ok {
		return store.NoteRevision{}, store.ErrNotFound
	}
	return rev, nil
}

func (f *fakeLocal) DiscussionByNumber(_ context.Context, n int64) (store.Discussion, error) {
	if f.err != nil {
		return store.Discussion{}, f.err
	}
	d, ok := f.discussions[n]
	if !ok {
		return store.Discussion{}, store.ErrNotFound
	}
	return d, nil
}

// fakeTracker is a Switchyard that soft-deletes the way the real one does: a
// missing ticket is a 404, never a tombstone. It records every path asked.
type fakeTracker struct {
	mu      sync.Mutex
	tickets map[string]map[string]any
	paths   []string
}

func (tr *fakeTracker) asked() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.paths...)
}

func newTrackerServer(t *testing.T) (*fakeTracker, *httptest.Server) {
	t.Helper()
	tr := &fakeTracker{tickets: map[string]map[string]any{
		"SWY-389": {"key": "SWY-389", "title": "Plan rulings as data",
			"status": map[string]any{"category": "in_progress", "display_name": "In Progress"}},
		"CHRN-7": {"key": "CHRN-7", "title": "E7 — References: link, never copy",
			"status": map[string]any{"category": "closed", "display_name": "Closed"}},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		tr.paths = append(tr.paths, r.URL.Path)
		tk, ok := tr.tickets[strings.TrimPrefix(r.URL.Path, "/v1/tickets/")]
		tr.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "ticket not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(tk)
	}))
	t.Cleanup(srv.Close)
	return tr, srv
}

const heldCitation = "amber1.9d654a7c-1ef5-49c8-bbe5-076164725a9d.4b1e7c2a-0c3d-4e5f-8a9b-1c2d3e4f5a6b.0"

// newArchiveServer is an Amber that holds exactly one citation and answers
// every other one not_captured -- at 404, WITH the cite body, which is the
// shape CHRN-50 built its own transport to read.
func newArchiveServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.TrimPrefix(r.URL.Path, "/v1/cite/") == heldCitation {
			_, _ = w.Write([]byte(`{"ref":"` + heldCitation + `","outcome":"held",` +
				`"explain":"the cited block is in the archive","recoverable":false}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"outcome":"not_captured","explain":"no capture holds this session","recoverable":false}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type refRig struct {
	h       http.Handler
	logs    *bytes.Buffer
	clock   *refClock
	tracker *fakeTracker
	sy      *httptest.Server
	am      *httptest.Server
	local   *fakeLocal
}

// newRefRig builds the surface with the REAL clients and the REAL resolver
// behind it, registering a transport for each upstream asked for.
func newRefRig(t *testing.T, withSwitchyard, withAmber bool) *refRig {
	t.Helper()
	rig := &refRig{
		logs:  &bytes.Buffer{},
		clock: &refClock{t: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)},
		local: newFakeLocal(),
	}
	transports := map[string]resolve.Transport{}
	if withSwitchyard {
		rig.tracker, rig.sy = newTrackerServer(t)
		c, err := switchyard.New(rig.sy.URL, "tok")
		if err != nil {
			t.Fatal(err)
		}
		transports["switchyard"] = resolve.NewSwitchyard(c)
	}
	if withAmber {
		rig.am = newArchiveServer(t)
		c, err := amber.New(rig.am.URL, "tok")
		if err != nil {
			t.Fatal(err)
		}
		transports["amber"] = c
	}
	logger := jsonLogger(rig.logs)
	resolver, err := resolve.New(resolve.Options{Transports: transports, Logger: logger, Now: rig.clock.now})
	if err != nil {
		t.Fatal(err)
	}

	f := newFakeAccounts()
	f.signIn(person("member@example.com", false), "member-token")
	rig.h = NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: logger, Version: "test", SecureCookies: true,
		References: resolver, LocalReferences: rig.local,
	})
	return rig
}

func (rig *refRig) post(body string) *httptest.ResponseRecorder {
	return postResolve(rig.h, body)
}

func postResolve(h http.Handler, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := jsonReq(http.MethodPost, "/references/resolve", body)
	r.Header.Set("Authorization", "Bearer member-token")
	h.ServeHTTP(rec, r)
	return rec
}

func decodeResolutions(t *testing.T, rec *httptest.ResponseRecorder) []wire.Resolution {
	t.Helper()
	var res wire.ResolveResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("body is not a ResolveResponse: %v\n%s", err, rec.Body.String())
	}
	return res.Resolutions
}

func str(p *string) string {
	if p == nil {
		return "<absent>"
	}
	return *p
}

// ---------------------------------------------------------------------------

// The Done-when in one call: a live coral card, a live gold card, a local CHR-
// card, a broken for a deleted note, a broken for a ticket that does not exist,
// a local discussion -- and then, after the tracker goes away, an unreachable
// carrying its last_resolved_at and an unchecked with no fetched_at at all.
func TestResolveAnswersTheWholeCardVocabularyInOneCall(t *testing.T) {
	rig := newRefRig(t, true, true)

	rec := rig.post(`{"references":[
		{"system":"switchyard","key":"SWY","token":"SWY-389","number":389},
		{"system":"amber","token":"` + heldCitation + `"},
		{"system":"chronicle","key":"CHR","target":"note","token":"CHR-0311","number":311},
		{"system":"chronicle","key":"CHR","target":"note","token":"CHR-400","number":400},
		{"system":"switchyard","key":"SWY","token":"SWY-999","number":999},
		{"system":"chronicle","key":"DSC","target":"discussion","token":"DSC-0007","number":7},
		{"system":"switchyard","key":"SY","token":"SY-412","number":412}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	apitest.Conform(t, "resolveReferences", rec)
	res := decodeResolutions(t, rec)
	if len(res) != 7 {
		t.Fatalf("got %d resolutions, want 7", len(res))
	}
	for i, want := range []string{"SWY-389", heldCitation, "CHR-0311", "CHR-400", "SWY-999", "DSC-0007", "SY-412"} {
		if res[i].Token != want {
			t.Errorf("resolutions[%d].token = %q, want %q (request order)", i, res[i].Token, want)
		}
	}

	// A live coral card: the tracker answered, with a status, a title and
	// somewhere for the arrow to go.
	sy := res[0]
	if sy.State != wire.Resolved || sy.Upstream == nil {
		t.Fatalf("SWY-389 = %s, upstream %v; want resolved with an upstream", sy.State, sy.Upstream)
	}
	if str(sy.Upstream.DisplayName) != "In Progress" || str(sy.Upstream.Outcome) != "in_progress" ||
		str(sy.Upstream.Key) != "SWY-389" || !strings.HasSuffix(str(sy.Upstream.Url), "/tickets/SWY-389") {
		t.Errorf("SWY-389 upstream = %+v", *sy.Upstream)
	}
	if sy.FetchedAt == nil {
		t.Error("a resolved card carries no fetched_at")
	}
	if sy.Upstream.Recoverable != nil {
		t.Error("recoverable is Amber's and leaked onto a Switchyard card")
	}

	// A live gold card: held, with Amber's own sentence relayed and its flag
	// present, and no URL because Amber serves no HTML.
	am := res[1]
	if am.State != wire.Resolved || am.Upstream == nil || str(am.Upstream.Outcome) != "held" {
		t.Fatalf("citation = %s / %+v, want resolved held", am.State, am.Upstream)
	}
	if am.Upstream.Recoverable == nil || *am.Upstream.Recoverable {
		t.Errorf("recoverable = %v on held, want present and false", am.Upstream.Recoverable)
	}
	if am.Upstream.Url != nil || am.Upstream.Key != nil {
		t.Errorf("an Amber card grew a url %v or a key %v", am.Upstream.Url, am.Upstream.Key)
	}
	if str(am.Explain) != "the cited block is in the archive" {
		t.Errorf("explain = %q, want Amber's sentence verbatim", str(am.Explain))
	}

	// A local CHR- card: no transport, no cache, the key as answered and the
	// current revision's title.
	note := res[2]
	if note.State != wire.Resolved || note.Upstream == nil ||
		str(note.Upstream.Key) != "CHR-0311" || str(note.Upstream.Title) != "Retention pruner design" {
		t.Errorf("CHR-0311 = %s / %+v", note.State, note.Upstream)
	}
	if note.FetchedAt == nil {
		t.Error("a local resolution carries no fetched_at")
	}
	if note.Upstream.Outcome != nil {
		t.Errorf("a live note has no state to report, but outcome = %q", *note.Upstream.Outcome)
	}

	// A soft-deleted note: broken, with the one member Chronicle mints, and
	// nothing else -- no title, no URL.
	gone := res[3]
	if gone.State != wire.Broken || gone.Upstream == nil || str(gone.Upstream.Outcome) != "deleted" {
		t.Fatalf("CHR-400 = %s / %+v, want broken with outcome deleted", gone.State, gone.Upstream)
	}
	if gone.Upstream.Title != nil || gone.Upstream.Url != nil {
		t.Errorf("a deleted note's card carries title %v / url %v", gone.Upstream.Title, gone.Upstream.Url)
	}
	if str(gone.Upstream.Key) != "CHR-0400" {
		t.Errorf("CHR-400 answered key %q, want the key as answered, CHR-0400", str(gone.Upstream.Key))
	}
	if strings.Contains(rec.Body.String(), withheldTitle) {
		t.Error("the deleted note's title reached the response")
	}

	// A ticket the tracker has never heard of: broken, key as written, no URL.
	missing := res[4]
	if missing.State != wire.Broken || missing.Upstream == nil || str(missing.Upstream.Key) != "SWY-999" ||
		missing.Upstream.Url != nil {
		t.Errorf("SWY-999 = %s / %+v, want broken, key as written, no url", missing.State, missing.Upstream)
	}

	// A local discussion, with its own state.
	thread := res[5]
	if thread.State != wire.Resolved || thread.Upstream == nil || str(thread.Upstream.Outcome) != "open" ||
		str(thread.Upstream.Key) != "DSC-0007" || str(thread.Upstream.Title) == "<absent>" {
		t.Errorf("DSC-0007 = %s / %+v", thread.State, thread.Upstream)
	}

	// A well-shaped key naming no live project is the TRACKER'S to refuse,
	// and it does, with a 404: broken. It is not this endpoint's to report as
	// a miss -- see below.
	if res[6].State != wire.Broken {
		t.Errorf("SY-412 = %s, want broken from the tracker's 404", res[6].State)
	}

	// -----------------------------------------------------------------------
	// The tracker goes away and the success window expires.
	// -----------------------------------------------------------------------
	firstFetched := *sy.FetchedAt
	rig.sy.Close()
	rig.clock.advance(resolve.DefaultSuccessTTL + time.Second)

	rec = rig.post(`{"references":[
		{"system":"switchyard","key":"SWY","token":"SWY-389","number":389},
		{"system":"switchyard","key":"CHRN","token":"CHRN-7","number":7}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	apitest.Conform(t, "resolveReferences", rec)
	res = decodeResolutions(t, rec)

	// Unreachable, carrying WHEN IT LAST RESOLVED and no status to misrender.
	down := res[0]
	if down.State != wire.Unreachable || down.Upstream != nil {
		t.Fatalf("SWY-389 with the tracker gone = %s / %v, want unreachable with no upstream", down.State, down.Upstream)
	}
	if down.FetchedAt == nil {
		t.Error("an unreachable carries no fetched_at, and an attempt was made")
	}
	if down.LastResolvedAt == nil || !down.LastResolvedAt.Equal(firstFetched) {
		t.Errorf("last_resolved_at = %v, want the first render's fetched_at %v", down.LastResolvedAt, firstFetched)
	}

	// The breaker tripped on the first, so the second was never asked: NO
	// fetched_at, because nothing was attempted. Ruling 7, pinned on the wire.
	skipped := res[1]
	if skipped.State != wire.Unchecked {
		t.Fatalf("CHRN-7 after a trip = %s, want unchecked", skipped.State)
	}
	if skipped.FetchedAt != nil {
		t.Errorf("unchecked carries fetched_at %v; ruling 7 says absent", *skipped.FetchedAt)
	}
	if skipped.Upstream != nil || skipped.LastResolvedAt != nil {
		t.Errorf("unchecked grew upstream %v / last_resolved_at %v", skipped.Upstream, skipped.LastResolvedAt)
	}

	// -----------------------------------------------------------------------
	// Log hygiene, over everything both renders wrote.
	// -----------------------------------------------------------------------
	logs := rig.logs.String()
	for _, tok := range []string{"SWY-389", "SWY-999", "CHR-0311", "CHR-400", "DSC-0007", "SY-412", heldCitation, "CHRN-7"} {
		if strings.Contains(logs, tok) {
			t.Errorf("token %q reached a log line:\n%s", tok, logs)
		}
	}
	if strings.Contains(logs, withheldTitle) || strings.Contains(logs, "Retention pruner") {
		t.Error("authored content reached a log line")
	}
	// The miss feed has exactly one caller and it is not this endpoint: SY-412
	// went to the tracker and came back broken, and nothing here called
	// Keys.NoteMisses about it.
	if strings.Contains(logs, "well-shaped keys") {
		t.Error("the resolve endpoint fed the miss feed; the note handler is its only caller")
	}
	if !strings.Contains(logs, "resolved a reference batch") {
		t.Error("the batch size was not logged")
	}
}

// Unconfigured is proven by the ABSENCE OF A TRANSPORT, not by the absence of
// an upstream: no server is stood up, no transport registered, and the state
// says so -- while Chronicle's own namespace still resolves.
func TestResolveAnswersUnconfiguredOnlyWhenNoTransportIsRegistered(t *testing.T) {
	rig := newRefRig(t, false, false)

	rec := rig.post(`{"references":[
		{"system":"switchyard","key":"SWY","token":"SWY-389","number":389},
		{"system":"amber","token":"` + heldCitation + `"},
		{"system":"chronicle","key":"CHR","target":"note","token":"CHR-0311","number":311}
	]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	apitest.Conform(t, "resolveReferences", rec)
	res := decodeResolutions(t, rec)

	for _, r := range res[:2] {
		if r.State != wire.Unconfigured {
			t.Errorf("%s = %s, want unconfigured", r.Token, r.State)
		}
		if r.FetchedAt != nil {
			t.Errorf("%s: unconfigured carries fetched_at %v; ruling 7 says absent", r.Token, *r.FetchedAt)
		}
		if r.Upstream != nil {
			t.Errorf("%s: nobody answered, but upstream is present", r.Token)
		}
		if !strings.Contains(str(r.Explain), "configured") {
			t.Errorf("%s: explain = %q does not say the deployment is unconfigured", r.Token, str(r.Explain))
		}
	}
	if res[2].State != wire.Resolved {
		t.Errorf("CHR-0311 = %s with no upstreams; the local namespace needs none", res[2].State)
	}
}

// Ruling 8: the server re-derives every descriptor from its token and refuses
// one that disagrees -- BEFORE anything is dialled. A client cannot choose
// which upstream a token goes to.
func TestResolveRefusesADescriptorThatDisagreesWithItsToken(t *testing.T) {
	rig := newRefRig(t, true, true)

	for _, tc := range []struct {
		name, descriptor, code string
	}{
		{"a ticket relabelled as a citation", `{"system":"amber","token":"SWY-389"}`, codeReferenceMismatch},
		{"a local note pointed at the tracker", `{"system":"switchyard","key":"SWY","token":"CHR-0311"}`, codeReferenceMismatch},
		{"the wrong number", `{"system":"switchyard","key":"SWY","token":"SWY-389","number":390}`, codeReferenceMismatch},
		{"the wrong key", `{"system":"switchyard","key":"CHRN","token":"SWY-389","number":389}`, codeReferenceMismatch},
		{"the wrong target", `{"system":"chronicle","key":"CHR","target":"discussion","token":"CHR-0311","number":311}`, codeReferenceMismatch},
		{"prose", `{"system":"switchyard","token":"hello"}`, codeInvalidReference},
		{"a sentence containing one", `{"system":"switchyard","token":"see SWY-389"}`, codeInvalidReference},
		{"an empty token", `{"system":"switchyard","token":""}`, codeInvalidReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := rig.post(`{"references":[` + tc.descriptor + `]}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			apitest.Conform(t, "resolveReferences", rec)
			var body wire.Error
			_ = json.Unmarshal(rec.Body.Bytes(), &body)
			if body.Code != tc.code {
				t.Errorf("code = %q, want %q", body.Code, tc.code)
			}
			// The message names the position and never the token.
			if strings.Contains(body.Message, "SWY") || strings.Contains(body.Message, "CHR-") || strings.Contains(body.Message, "hello") {
				t.Errorf("message %q echoes the token", body.Message)
			}
		})
	}
	if asked := rig.tracker.asked(); len(asked) != 0 {
		t.Errorf("a refused descriptor still reached the tracker: %v", asked)
	}

	// Omitting what the note payload omitted is not disagreeing.
	rec := rig.post(`{"references":[{"system":"switchyard","token":"SWY-389"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("a descriptor with only system and token = %d: %s", rec.Code, rec.Body.String())
	}
	apitest.Conform(t, "resolveReferences", rec)
}

// The statuses the shared code can answer, driven rather than only declared.
func TestResolveDrivesTheSharedRefusals(t *testing.T) {
	rig := newRefRig(t, false, false)
	one := `{"system":"chronicle","key":"CHR","target":"note","token":"CHR-0311","number":311}`

	t.Run("415 on the wrong media type", func(t *testing.T) {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/references/resolve", strings.NewReader(`{"references":[`+one+`]}`))
		r.Header.Set("Content-Type", "text/plain")
		r.Header.Set("Authorization", "Bearer member-token")
		rig.h.ServeHTTP(rec, r)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status = %d, want 415", rec.Code)
		}
		apitest.Conform(t, "resolveReferences", rec)
	})
	t.Run("400 on a body that is not JSON", func(t *testing.T) {
		rec := rig.post(`{"references": [`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		apitest.Conform(t, "resolveReferences", rec)
	})
	t.Run("400 on an unknown field", func(t *testing.T) {
		rec := rig.post(`{"references":[` + one + `],"resolve_now":true}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		apitest.Conform(t, "resolveReferences", rec)
	})
	t.Run("400 on an empty batch", func(t *testing.T) {
		rec := rig.post(`{"references":[]}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		apitest.Conform(t, "resolveReferences", rec)
	})
	t.Run("400 over the cap", func(t *testing.T) {
		items := make([]string, resolve.DefaultCap+1)
		for i := range items {
			items[i] = one
		}
		rec := rig.post(`{"references":[` + strings.Join(items, ",") + `]}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
		}
		apitest.Conform(t, "resolveReferences", rec)
		var body wire.Error
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if !strings.Contains(body.Message, "50") {
			t.Errorf("message %q does not state the cap", body.Message)
		}
	})
	t.Run("exactly the cap is accepted", func(t *testing.T) {
		items := make([]string, resolve.DefaultCap)
		for i := range items {
			items[i] = one
		}
		rec := rig.post(`{"references":[` + strings.Join(items, ",") + `]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
		}
		apitest.Conform(t, "resolveReferences", rec)
	})
	t.Run("413 on a body over the limit", func(t *testing.T) {
		huge := `{"references":[{"system":"switchyard","token":"` + strings.Repeat("A", maxResolveBody) + `"}]}`
		rec := rig.post(huge)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status = %d, want 413", rec.Code)
		}
		apitest.Conform(t, "resolveReferences", rec)
	})
}

// A router assembled with no resolver answers 503, declared and driven -- and
// never dereferences a nil inside a render.
func TestResolveWithoutAResolverAnswersTheDocumented503(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("member@example.com", false), "member-token")
	h := testRouter(f)

	rec := postResolve(h, `{"references":[{"system":"switchyard","token":"SWY-389"}]}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	apitest.Conform(t, "resolveReferences", rec)
	var body wire.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != codeReferencesUnconfigured {
		t.Errorf("code = %q, want %q", body.Code, codeReferencesUnconfigured)
	}
}

// A store that fails is not an answer about the reference: the 500 is driven,
// the envelope is the documented one, and the cause stays in the log.
func TestResolveAnswersTheDocumented500WhenTheStoreFails(t *testing.T) {
	rig := newRefRig(t, false, false)
	rig.local.err = errors.New("connection reset by peer")

	rec := rig.post(`{"references":[{"system":"chronicle","key":"CHR","target":"note","token":"CHR-0311","number":311}]}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	apitest.Conform(t, "resolveReferences", rec)
	var body wire.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Code != codeInternal || strings.Contains(body.Message, "connection reset") {
		t.Errorf("body = %+v: want the internal code and no cause", body)
	}
}

// toDescriptor is what CHRN-98's note payload will emit, and it must round-trip
// through descriptorAgrees for every system, or a client echoing the payload
// back would be refused for repeating what it was told.
func TestADescriptorTheNotePayloadEmitsIsAcceptedBack(t *testing.T) {
	for _, tok := range []string{"SWY-389", "CHR-0311", "DSC-7", heldCitation, "SWY-0"} {
		ref, ok := markdownParse(t, tok)
		if !ok {
			t.Fatalf("%q did not parse", tok)
		}
		if !descriptorAgrees(toDescriptor(ref), ref) {
			t.Errorf("%q: the descriptor the note payload emits disagrees with its own token", tok)
		}
	}
}

func markdownParse(t *testing.T, tok string) (markdown.Reference, bool) {
	t.Helper()
	return markdown.ParseReference(tok)
}
