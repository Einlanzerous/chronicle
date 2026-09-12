package resolve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

var _ Transport = switchyardTransport{}

// fakeTracker is a Switchyard with a mutable board: a test transitions a
// ticket, or deletes it, between one render and the next.
//
// IT SOFT-DELETES THE WAY THE REAL ONE DOES. server/src/lib/lookups.ts's
// resolveTicket excludes rows carrying deleted_at and throws notFound, so a
// deleted ticket arrives at Chronicle as a 404 and never as a tombstone. It
// also answers 404 rather than 403 for a project the caller cannot read, "so
// existence stays hidden" -- which is why this fake has exactly one way to say
// no.
type fakeTracker struct {
	mu       sync.Mutex
	tickets  map[string]sy // key -> ticket
	aliases  map[string]string
	projects []map[string]any
	paths    []string
}

type sy struct {
	key, title, category, display string
}

func newTracker(t *testing.T) (*fakeTracker, *switchyard.Client) {
	t.Helper()
	tr := &fakeTracker{
		tickets: map[string]sy{
			"SWY-389": {key: "SWY-389", title: "Plan rulings as data", category: "in_progress", display: "In Progress"},
			"CHRN-7":  {key: "CHRN-7", title: "E7 — References: link, never copy", category: "in_progress", display: "In Progress"},
		},
		// CHRN itself graduated from IDEA-21, so the alias path is this
		// estate's history rather than a hypothetical.
		aliases: map[string]string{"IDEA-21": "CHRN-7"},
		projects: []map[string]any{
			{"key": "SWY", "name": "Switchyard"},
			{"key": "CHRN", "name": "Chronicle"},
			{"key": "AMBR", "name": "Amber"},
			{"key": "LOOP", "name": "Retired", "archived_at": "2026-01-01T00:00:00Z"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		tr.paths = append(tr.paths, r.URL.Path)
		tr.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/projects" {
			tr.mu.Lock()
			items := tr.projects
			tr.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "page": map[string]any{"has_more": false}})
			return
		}
		key := strings.TrimPrefix(r.URL.Path, "/v1/tickets/")

		tr.mu.Lock()
		tk, ok := tr.tickets[key]
		if !ok {
			if target, aliased := tr.aliases[key]; aliased {
				tk, ok = tr.tickets[target]
			}
		}
		tr.mu.Unlock()

		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "ticket not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":   tk.key,
			"title": tk.title,
			"status": map[string]any{
				"category": tk.category, "display_name": tk.display,
			},
		})
	}))
	t.Cleanup(srv.Close)

	c, err := switchyard.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return tr, c
}

func (tr *fakeTracker) transition(key, category, display string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tk := tr.tickets[key]
	tk.category, tk.display = category, display
	tr.tickets[key] = tk
}

func (tr *fakeTracker) delete(key string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	delete(tr.tickets, key)
}

func (tr *fakeTracker) asked() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]string(nil), tr.paths...)
}

// swResolver builds the resolver a render would have: the real client, the real
// transport, and an injected clock so a TTL can be crossed without sleeping.
func swResolver(t *testing.T, clock *fakeClock, c *switchyard.Client) *Resolver {
	t.Helper()
	return newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: NewSwitchyard(c)},
	})
}

// shown is the status a person would read off the card, and empty where there
// is nothing honest to show.
func shown(r Resolution) string {
	if r.Upstream == nil {
		return ""
	}
	return r.Upstream.DisplayName
}

func only(t *testing.T, res []Resolution) Resolution {
	t.Helper()
	if len(res) != 1 {
		t.Fatalf("want one resolution, got %d", len(res))
	}
	return res[0]
}

// ---------------------------------------------------------------------------

// TestATicketResolvesToWhatSwitchyardSaysNow is the coral card's data, end to
// end through the shipped client: `SWY-389 · IN PROGRESS` with somewhere to go.
func TestATicketResolvesToWhatSwitchyardSaysNow(t *testing.T) {
	_, c := newTracker(t)
	clock := newClock()
	r := swResolver(t, clock, c)

	got := only(t, r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389")}))
	if got.State != StateResolved {
		t.Fatalf("state = %q, want resolved (%s)", got.State, got.Explain)
	}
	if got.Upstream == nil {
		t.Fatal("resolved with no upstream: the answer is missing the only thing the card shows")
	}
	if got.Upstream.Key != "SWY-389" || got.Upstream.Title != "Plan rulings as data" {
		t.Fatalf("upstream = %+v", got.Upstream)
	}
	if got.Upstream.DisplayName != "In Progress" || got.Upstream.Outcome != "in_progress" {
		t.Fatalf("the status a person reads and the category a client switches on must both travel: %+v", got.Upstream)
	}
	if !strings.HasSuffix(got.Upstream.URL, "/tickets/SWY-389") {
		t.Fatalf("the outbound arrow points at %q", got.Upstream.URL)
	}
	// A status with no instant it was true is the copy that lies.
	if got.FetchedAt.IsZero() {
		t.Fatal("resolved with no FetchedAt")
	}
	// The project is carried by the key that answered rather than by a field of
	// its own -- SWY-389 names project SWY -- which is also how a card knows
	// which project to show after a move.
	if key, _, ok := strings.Cut(got.Upstream.Key, "-"); !ok || key != "SWY" {
		t.Fatalf("the project is not recoverable from the answered key %q", got.Upstream.Key)
	}
}

// TestATransitionShowsOnTheNextRender is this ticket's Done-when, first half.
//
// Both halves of the promise are asserted, because either one alone is a
// different product: inside the success window the answer is the cached one
// (that is what stops thirty references being thirty calls on every render),
// and the first render after it shows what Switchyard says now.
func TestATransitionShowsOnTheNextRender(t *testing.T) {
	tr, c := newTracker(t)
	clock := newClock()
	r := swResolver(t, clock, c)
	ref := []markdown.Reference{swRef("SWY-389")}

	first := only(t, r.Resolve(context.Background(), ref))
	if first.Upstream.DisplayName != "In Progress" {
		t.Fatalf("before = %q", first.Upstream.DisplayName)
	}

	tr.transition("SWY-389", "closed", "Closed")

	clock.advance(DefaultSuccessTTL - 1)
	cached := only(t, r.Resolve(context.Background(), ref))
	if shown(cached) != "In Progress" {
		t.Fatalf("inside the TTL the cached answer should still be served, got %q", shown(cached))
	}
	if cached.FetchedAt != first.FetchedAt {
		t.Fatal("a cache hit must carry the instant it was fetched, not the instant it was served")
	}

	clock.advance(2)
	next := only(t, r.Resolve(context.Background(), ref))
	if next.State != StateResolved {
		t.Fatalf("state = %q (%s)", next.State, next.Explain)
	}
	if shown(next) != "Closed" {
		t.Fatalf("the next render still shows %q — a transition is invisible", shown(next))
	}
	if !next.FetchedAt.After(first.FetchedAt) {
		t.Fatal("the card would show a new status under an old timestamp")
	}
}

// TestADeletedTicketRendersAsVisiblyBroken is this ticket's Done-when, second
// half, and the sharpest clause in it: "not vanish and not silently show its
// last known title".
func TestADeletedTicketRendersAsVisiblyBroken(t *testing.T) {
	tr, c := newTracker(t)
	clock := newClock()
	r := swResolver(t, clock, c)
	ref := []markdown.Reference{swRef("SWY-389")}

	before := only(t, r.Resolve(context.Background(), ref))
	if before.State != StateResolved {
		t.Fatalf("setup: %q", before.State)
	}

	tr.delete("SWY-389")
	clock.advance(DefaultSuccessTTL)

	got := only(t, r.Resolve(context.Background(), ref))

	// DID NOT VANISH: one reference in, one resolution out, still carrying the
	// token so a renderer can align it with the marker on the page.
	if got.Ref.Token != "SWY-389" {
		t.Fatalf("the reference lost its token: %+v", got.Ref)
	}
	// IS VISIBLY BROKEN, and broken rather than unreachable: Switchyard
	// answered, and what it answered is a fact about the referent.
	if got.State != StateBroken {
		t.Fatalf("state = %q, want broken (%s)", got.State, got.Explain)
	}
	if got.Explain == "" {
		t.Fatal("a broken reference with no sentence is a card with nothing to render")
	}
	// DOES NOT SHOW ITS LAST KNOWN TITLE. The upstream is present because
	// somebody answered, and it carries the key and nothing that could be
	// mistaken for live state.
	if got.Upstream == nil {
		t.Fatal("Switchyard answered, so Upstream must be present")
	}
	if got.Upstream.Title != "" || got.Upstream.DisplayName != "" || got.Upstream.Outcome != "" {
		t.Fatalf("the last known state leaked onto a broken card: %+v", got.Upstream)
	}
	// An outbound arrow onto a 404 is a worse link than none.
	if got.Upstream.URL != "" {
		t.Fatalf("the arrow still points somewhere: %q", got.Upstream.URL)
	}
	// The key as WRITTEN: nothing answered with one of its own.
	if got.Upstream.Key != "SWY-389" {
		t.Fatalf("key = %q", got.Upstream.Key)
	}
}

// TestAKeyThatNeverExistedIsBrokenTheSameWay -- Switchyard answers 404 for a
// ticket in a project the credential cannot read, deliberately, "so existence
// stays hidden". Chronicle therefore cannot tell the two apart and must not
// pretend to: one state, one sentence naming both possibilities.
func TestAKeyThatNeverExistedIsBrokenTheSameWay(t *testing.T) {
	_, c := newTracker(t)
	r := swResolver(t, newClock(), c)

	got := only(t, r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-99999")}))
	if got.State != StateBroken {
		t.Fatalf("state = %q (%s)", got.State, got.Explain)
	}
	if !strings.Contains(got.Explain, "deleted") || !strings.Contains(got.Explain, "never existed") {
		t.Fatalf("the sentence claims more than Chronicle can know: %q", got.Explain)
	}
}

// TestAMovedTicketResolvesUnderTheKeyThatAnswered. CHRN graduated from IDEA-21,
// so a note written before the move still says `IDEA-21` -- and the card must
// show the ticket that exists, linked where it actually lives.
func TestAMovedTicketResolvesUnderTheKeyThatAnswered(t *testing.T) {
	_, c := newTracker(t)
	r := swResolver(t, newClock(), c)

	got := only(t, r.Resolve(context.Background(), []markdown.Reference{swRef("IDEA-21")}))
	if got.State != StateResolved {
		t.Fatalf("the alias did not resolve: %q (%s)", got.State, got.Explain)
	}
	if got.Ref.Token != "IDEA-21" {
		t.Fatalf("the written token must survive for the renderer: %q", got.Ref.Token)
	}
	if got.Upstream.Key != "CHRN-7" {
		t.Fatalf("upstream key = %q, want the key that answered", got.Upstream.Key)
	}
	if !strings.HasSuffix(got.Upstream.URL, "/tickets/CHRN-7") {
		t.Fatalf("the arrow deep-links the alias: %q", got.Upstream.URL)
	}
}

// TestAStoppedTrackerShowsNoStaleTitle proves the invariant through the real
// client rather than a fabricated error: "a cache with no visible staleness is
// a copy that lies".
func TestAStoppedTrackerShowsNoStaleTitle(t *testing.T) {
	// Its own server so it can be stopped mid-test without disturbing the
	// shared cleanup.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"SWY-389","title":"Plan rulings as data","status":{"category":"in_progress","display_name":"In Progress"}}`))
	}))
	c, err := switchyard.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	clock := newClock()
	r := swResolver(t, clock, c)
	ref := []markdown.Reference{swRef("SWY-389")}

	first := only(t, r.Resolve(context.Background(), ref))
	if first.State != StateResolved {
		t.Fatalf("setup: %q", first.State)
	}

	srv.Close()
	clock.advance(DefaultSuccessTTL)

	got := only(t, r.Resolve(context.Background(), ref))
	if got.State != StateUnreachable {
		t.Fatalf("state = %q, want unreachable (%s)", got.State, got.Explain)
	}
	if got.Upstream != nil {
		t.Fatalf("nobody answered, so there is nothing to show: %+v", got.Upstream)
	}
	if got.LastResolvedAt == nil || !got.LastResolvedAt.Equal(first.FetchedAt) {
		t.Fatal("the age of the outage is the one thing that does travel, and it did not")
	}
}

// TestAWholeNoteResolvesItsTicketReferences is the render, assembled from the
// three shipped pieces: the key set says which tokens are references, the
// parser marks them, and the resolver answers them.
func TestAWholeNoteResolvesItsTicketReferences(t *testing.T) {
	_, c := newTracker(t)
	keys, _ := newKeys(t, newClock(), SwitchyardProjectKeys(c))
	if err := keys.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}

	clock := newClock()
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: NewSwitchyard(c)},
		Keys:       keys,
	})

	note := []byte("Picked up SWY-389 today; it blocks CHRN-7. Not UTF-8, and not SY-412 either.")
	rend := markdown.NewRenderer(keys)

	scan := rend.Scan(note)
	if len(scan.References) != 2 {
		t.Fatalf("references = %+v, want SWY-389 and CHRN-7 only", scan.References)
	}
	// SY-412 is the canvas illustration CHRN-48 found being read as an
	// identifier, and UTF-8 is the prose that a wildcard grammar would have
	// turned into a broken card. Both are well-shaped and name no live project,
	// so both come back as misses and neither is ever dialled -- which is the
	// whole reason this resolver is handed a key set rather than a regexp.
	if want := []string{"UTF", "SY"}; len(scan.UnknownKeys) != len(want) {
		t.Fatalf("unknown keys = %v, want %v", scan.UnknownKeys, want)
	} else {
		for i, k := range want {
			if scan.UnknownKeys[i] != k {
				t.Fatalf("unknown keys = %v, want %v", scan.UnknownKeys, want)
			}
		}
	}

	res := r.ResolveScan(context.Background(), scan)
	for _, got := range res {
		if got.State != StateResolved {
			t.Fatalf("%s: %q (%s)", got.Ref.Token, got.State, got.Explain)
		}
		if got.Upstream.Title == "" || got.Upstream.DisplayName == "" {
			t.Fatalf("%s resolved with nothing to render: %+v", got.Ref.Token, got.Upstream)
		}
	}

	// The colour is not this package's to choose and is not in the payload: it
	// keys off the marker's system, which is what the estate rule is about.
	html, err := rend.Render(note)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(html), `data-ref-system="switchyard"`); n != 2 {
		t.Fatalf("the coral marker appears %d times in %s", n, html)
	}
	if strings.Contains(string(html), "Plan rulings as data") {
		t.Fatal("a title was baked into the stored-bytes render: that is the copy invariant 2 forbids")
	}
}

// TestTheProjectKeySetComesFromSwitchyard -- the adapter keys.go named. A key
// set is membership, and an archived project is not a live destination.
func TestTheProjectKeySetComesFromSwitchyard(t *testing.T) {
	_, c := newTracker(t)
	got, err := SwitchyardProjectKeys(c)(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"SWY": true, "CHRN": true, "AMBR": true}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want exactly %v", got, want)
	}
	for _, k := range got {
		if !want[k] {
			t.Fatalf("archived or unknown project key %q is in the set", k)
		}
	}
}

// TestAFailedKeyFetchIsReportedRatherThanAnsweredEmpty. Keys.refresh keeps the
// last good set only because it can tell a failure from an empty estate; an
// adapter that swallowed the error would silently un-link every reference.
func TestAFailedKeyFetchIsReportedRatherThanAnsweredEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c, err := switchyard.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := SwitchyardProjectKeys(c)(context.Background())
	if err == nil {
		t.Fatalf("a 500 answered %v and no error", keys)
	}
	if keys != nil {
		t.Fatalf("a failed fetch must return nothing, not an empty set: %v", keys)
	}
}

// TestResolutionIsNeverGatedOnAHealthProbe discharges CHRN-51's criterion of
// the same name, for the Switchyard half.
//
// Switchyard's own handleCite deliberately does not call indexReady, for the
// reason that generalises: a red /readyz does not mean a reference cannot
// resolve, and asking first would double every render's calls to buy a worse
// answer than simply asking for the ticket.
func TestResolutionIsNeverGatedOnAHealthProbe(t *testing.T) {
	tr, c := newTracker(t)
	r := swResolver(t, newClock(), c)

	r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389"), swRef("SWY-99999")})

	asked := tr.asked()
	if len(asked) == 0 {
		t.Fatal("nothing was dialled at all")
	}
	for _, p := range asked {
		if !strings.HasPrefix(p, "/v1/tickets/") {
			t.Fatalf("the resolve path dialled %q; resolution asks for the ticket and nothing else", p)
		}
	}
}
