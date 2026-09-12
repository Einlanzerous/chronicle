package resolve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// ---------------------------------------------------------------------------
// Harness. Every window in this package is driven by an injected clock rather
// than by sleeping: a test that sleeps for a 60s TTL is a test nobody runs.
// ---------------------------------------------------------------------------

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 9, 12, 9, 0, 0, 0, time.UTC)}
}
func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// scriptedTransport answers from a function, counts calls, and can charge the
// clock for each one -- which is how the budget is exercised with no real time.
type scriptedTransport struct {
	clock  *fakeClock
	cost   time.Duration
	answer func(token string) Answer

	mu        sync.Mutex
	calls     int
	startedAt []time.Duration // elapsed since the clock's base, per call
	base      time.Time
}

func (t *scriptedTransport) Fetch(_ context.Context, ref markdown.Reference) Answer {
	t.mu.Lock()
	t.calls++
	if t.clock != nil {
		t.startedAt = append(t.startedAt, t.clock.now().Sub(t.base))
	}
	t.mu.Unlock()

	// The answer is produced BEFORE the clock is charged, because time passes
	// DURING a call. It also lets a test block inside the answer to model an
	// upstream that is slow while another is not.
	out := Answer{Status: 200, Body: []byte(ticketBody)}
	if t.answer != nil {
		out = t.answer(ref.Token)
	}
	if t.clock != nil && t.cost > 0 {
		t.clock.advance(t.cost)
	}
	return out
}

func (t *scriptedTransport) URLFor(key string) string { return "https://sy/tickets/" + key }

func (t *scriptedTransport) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.calls
}

func swRef(token string) markdown.Reference {
	return markdown.Reference{
		System: markdown.SystemSwitchyard,
		Key:    token[:strings.IndexByte(token, '-')],
		Token:  token,
	}
}

func amRef(token string) markdown.Reference {
	return markdown.Reference{System: markdown.SystemAmber, Token: token}
}

// newResolver wires a Resolver onto a fake clock with the plan's numbers.
func newResolver(t *testing.T, clock *fakeClock, o Options) *Resolver {
	t.Helper()
	if o.Now == nil && clock != nil {
		o.Now = clock.now
	}
	r, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func stateCounts(res []Resolution) map[State]int {
	m := map[State]int{}
	for _, r := range res {
		m[r.State]++
	}
	return m
}

// ---------------------------------------------------------------------------

// TestAStoppedUpstreamCarriesTheAgeAndNeverTheValue is ruling 1, which is the
// decision this whole package was tiered opus for.
//
// Carry the age, drop the value: outside the TTL with the upstream unreachable,
// the card says unreachable, its Upstream is nil, and LastResolvedAt says how
// long the outage has run. A reader learns the duration and is told no status
// that might be false.
func TestAStoppedUpstreamCarriesTheAgeAndNeverTheValue(t *testing.T) {
	clock := newClock()
	up := true
	tr := &scriptedTransport{clock: clock, base: clock.now(), answer: func(string) Answer {
		if up {
			return Answer{Status: 200, Body: []byte(ticketBody)}
		}
		return Answer{Err: fmt.Errorf("dial tcp 172.19.0.4:4002: connect: connection refused")}
	}}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

	first := r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389")})
	if first[0].State != StateResolved {
		t.Fatalf("first render: state = %q, want resolved", first[0].State)
	}
	resolvedAt := first[0].FetchedAt

	up = false
	clock.advance(DefaultSuccessTTL + time.Second)
	failedAt := clock.now()

	got := r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389")})[0]

	if got.State != StateUnreachable {
		t.Fatalf("state = %q, want unreachable", got.State)
	}
	if got.Upstream != nil {
		t.Errorf("Upstream is present on unreachable: %+v — a stale value dressed as current", got.Upstream)
	}
	if !got.FetchedAt.Equal(failedAt) {
		t.Errorf("FetchedAt = %v, want the instant of the failed attempt %v", got.FetchedAt, failedAt)
	}
	if got.LastResolvedAt == nil || !got.LastResolvedAt.Equal(resolvedAt) {
		t.Errorf("LastResolvedAt = %v, want the first resolution's instant %v", got.LastResolvedAt, resolvedAt)
	}

	// No status, title, outcome or display name anywhere in the payload.
	rendered := fmt.Sprintf("%+v", got)
	for _, banned := range []string{"in_progress", "In Progress", "A real ticket"} {
		if strings.Contains(rendered, banned) {
			t.Errorf("payload carries %q from the last good answer: %s", banned, rendered)
		}
	}
}

// TestUpstreamIsPresentExactlyWhenTheUpstreamAnswered.
//
// The boundary is *did anyone answer*, not *was the answer good news*. Nil
// means nobody answered: never nil beside a status, and never a status with no
// answer.
func TestUpstreamIsPresentExactlyWhenTheUpstreamAnswered(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now(), answer: func(token string) Answer {
		switch token {
		case "SWY-1":
			return Answer{Status: 200, Body: []byte(ticketBody)}
		case "SWY-2":
			return Answer{Status: 404}
		default:
			return Answer{Err: fmt.Errorf("connection refused")}
		}
	}}
	// A cap of 3 so the fourth Switchyard reference is unchecked; no Amber
	// transport at all so the Amber reference is unconfigured.
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: tr},
		Cap:        3,
	})

	refs := []markdown.Reference{
		swRef("SWY-1"), swRef("SWY-2"), swRef("SWY-3"), swRef("SWY-4"), amRef("amber1.a.b.0"),
	}
	got := r.Resolve(context.Background(), refs)

	want := []State{StateResolved, StateBroken, StateUnreachable, StateUnchecked, StateUnconfigured}
	for i, w := range want {
		if got[i].State != w {
			t.Fatalf("ref %d (%s): state = %q, want %q", i, refs[i].Token, got[i].State, w)
		}
	}
	for i, res := range got {
		answered := res.State == StateResolved || res.State == StateBroken
		if answered && res.Upstream == nil {
			t.Errorf("%s is %q with no Upstream: the upstream answered", res.Ref.Token, res.State)
		}
		if !answered && res.Upstream != nil {
			t.Errorf("%s is %q with an Upstream: nobody answered", res.Ref.Token, res.State)
		}
		// FetchedAt is zero only where nothing was attempted.
		attempted := answered || res.State == StateUnreachable
		if attempted && res.FetchedAt.IsZero() {
			t.Errorf("%s is %q with a zero FetchedAt", res.Ref.Token, res.State)
		}
		if !attempted && !res.FetchedAt.IsZero() {
			t.Errorf("%s is %q with a FetchedAt: nothing was attempted", res.Ref.Token, res.State)
		}
		_ = i
	}

	// A Switchyard broken carries the key as written and nothing to click.
	if u := got[1].Upstream; u == nil || u.Key != "SWY-2" || u.URL != "" || u.Outcome != "" {
		t.Errorf("broken Upstream = %+v, want the written key with no URL and no minted outcome", u)
	}
}

// TestLastResolvedAtMeansWhenWeLastKnew, not when we last asked.
func TestLastResolvedAtMeansWhenWeLastKnew(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now(), answer: func(string) Answer {
		return Answer{Err: fmt.Errorf("connection refused")}
	}}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

	got := r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389")})[0]
	if got.State != StateUnreachable {
		t.Fatalf("state = %q", got.State)
	}
	if got.LastResolvedAt != nil {
		t.Errorf("LastResolvedAt = %v on a reference that never resolved", got.LastResolvedAt)
	}
}

// TestUnconfiguredIsDistinctFromUnreachableAndNeverDials.
//
// "A card reading 'Switchyard unreachable' on a Chronicle that was never given
// a token sends somebody to check a service that is fine."
func TestUnconfiguredIsDistinctFromUnreachableAndNeverDials(t *testing.T) {
	dialed := false
	tr := &scriptedTransport{answer: func(string) Answer {
		dialed = true
		return Answer{Status: 200, Body: []byte(ticketBody)}
	}}
	// A transport exists for Amber only; the Switchyard references have none.
	r := newResolver(t, newClock(), Options{Transports: map[string]Transport{markdown.SystemAmber: tr}})

	got := r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389"), swRef("SWY-1")})
	for _, res := range got {
		if res.State != StateUnconfigured {
			t.Errorf("%s: state = %q, want unconfigured", res.Ref.Token, res.State)
		}
		if !res.FetchedAt.IsZero() {
			t.Errorf("%s: FetchedAt set on a state that never dialled", res.Ref.Token)
		}
		if res.Upstream != nil {
			t.Errorf("%s: Upstream set on a state that never dialled", res.Ref.Token)
		}
	}
	if dialed {
		t.Error("an unconfigured upstream was dialled")
	}
}

// ---------------------------------------------------------------------------
// The cache, counted by a real test server rather than asserted by timing.
// ---------------------------------------------------------------------------

// overHTTP is the SHIPPED Switchyard transport pointed at a test server, so the
// call-counting claims below are made against something that actually serves
// HTTP -- and against the code that will make the calls in production, rather
// than a double that could be counting itself.
//
// CHRN-51 carried a hand-rolled stand-in here because no transport existed yet;
// CHRN-49 shipped one, and a second spelling of the same request is a second
// place for the cache's premise to stop being true.
func overHTTP(t *testing.T, srv *httptest.Server) Transport {
	t.Helper()
	c, err := switchyard.New(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return NewSwitchyard(c)
}

func newTicketServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		key := strings.TrimPrefix(req.URL.Path, "/v1/tickets/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"key":%q,"title":"t","status":{"category":"in_progress","display_name":"In Progress"}}`, key)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// TestThirtyReferencesDoNotMakeThirtyCalls is the ticket's own premise.
//
// Switchyard has no multi-key filter -- TicketListFilters takes project,
// status, type, label, assignee, reporter, parent, text and dates and nothing
// taking a list of ticket keys -- so thirty distinct references really are
// thirty calls, and the cache is load-bearing rather than an optimisation.
func TestThirtyReferencesDoNotMakeThirtyCalls(t *testing.T) {
	srv, calls := newTicketServer(t)
	clock := newClock()
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: overHTTP(t, srv)},
	})

	// Thirty references to ten distinct tickets.
	var refs []markdown.Reference
	for i := 0; i < 30; i++ {
		refs = append(refs, swRef(fmt.Sprintf("SWY-%d", i%10)))
	}

	got := r.Resolve(context.Background(), refs)
	if n := stateCounts(got)[StateResolved]; n != 30 {
		t.Fatalf("resolved = %d of 30", n)
	}
	if n := calls.Load(); n != 10 {
		t.Errorf("cold cache made %d calls, want 10 — one per distinct ticket", n)
	}

	before := calls.Load()
	r.Resolve(context.Background(), refs)
	if n := calls.Load() - before; n != 0 {
		t.Errorf("second render inside the TTL made %d calls, want 0", n)
	}
}

// TestTheCacheWindowsAreTheRulingsNumbers.
func TestTheCacheWindowsAreTheRulingsNumbers(t *testing.T) {
	srv, calls := newTicketServer(t)
	clock := newClock()
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: overHTTP(t, srv)},
	})
	refs := []markdown.Reference{swRef("SWY-1")}

	r.Resolve(context.Background(), refs)
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}

	// Served from cache right up to the success window.
	clock.advance(DefaultSuccessTTL - time.Millisecond)
	r.Resolve(context.Background(), refs)
	if calls.Load() != 1 {
		t.Errorf("re-dialled inside the success window: calls = %d", calls.Load())
	}

	// And re-dialled one tick past it.
	clock.advance(2 * time.Millisecond)
	r.Resolve(context.Background(), refs)
	if calls.Load() != 2 {
		t.Errorf("did not re-dial past the success window: calls = %d", calls.Load())
	}
}

// TestAFailureIsCachedForTheShorterWindow: recovery should be noticed quickly,
// but not at the cost of a stampede.
func TestAFailureIsCachedForTheShorterWindow(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now(), answer: func(string) Answer {
		return Answer{Status: 500} // unreachable, and does NOT trip
	}}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})
	refs := []markdown.Reference{swRef("SWY-1")}

	r.Resolve(context.Background(), refs)
	if tr.count() != 1 {
		t.Fatalf("calls = %d", tr.count())
	}
	clock.advance(DefaultFailureTTL - time.Millisecond)
	r.Resolve(context.Background(), refs)
	if tr.count() != 1 {
		t.Errorf("re-dialled inside the failure window: calls = %d", tr.count())
	}
	clock.advance(2 * time.Millisecond)
	r.Resolve(context.Background(), refs)
	if tr.count() != 2 {
		t.Errorf("did not re-dial past the failure window: calls = %d", tr.count())
	}
}

// TestTheCacheIsBoundedAndClearedWholesale.
//
// The key space is not bounded: every token ever written in a note is an entry,
// and the map lives for the process. An LRU would be machinery for a cache
// whose entire job is absorbing one page's worth of repeat lookups.
func TestTheCacheIsBoundedAndClearedWholesale(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now()}
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: tr},
		CacheMax:   4,
	})
	for i := 0; i < 40; i++ {
		r.Resolve(context.Background(), []markdown.Reference{swRef(fmt.Sprintf("SWY-%d", i))})
		r.mu.Lock()
		n := len(r.entries)
		r.mu.Unlock()
		if n > 4 {
			t.Fatalf("cache holds %d entries, over the ceiling of 4", n)
		}
	}
}

// TestTheCacheIsSafeUnderConcurrentRenders. Run under -race.
func TestTheCacheIsSafeUnderConcurrentRenders(t *testing.T) {
	srv, _ := newTicketServer(t)
	r := newResolver(t, nil, Options{
		Now:        time.Now,
		Transports: map[string]Transport{markdown.SystemSwitchyard: overHTTP(t, srv)},
	})
	var refs []markdown.Reference
	for i := 0; i < 12; i++ {
		refs = append(refs, swRef(fmt.Sprintf("SWY-%d", i)))
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				r.Resolve(context.Background(), refs)
			}
		}()
	}
	wg.Wait()
}

// TestTwoRendersDoNotShareOneUpstream: a cache entry is read by every
// concurrent render, and a shared mutable pointer to upstream state is exactly
// what this package exists in order not to have.
func TestTwoRendersDoNotShareOneUpstream(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now()}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})
	refs := []markdown.Reference{swRef("SWY-1")}

	a := r.Resolve(context.Background(), refs)[0]
	b := r.Resolve(context.Background(), refs)[0] // served from cache
	if a.Upstream == nil || b.Upstream == nil {
		t.Fatal("no Upstream")
	}
	if a.Upstream == b.Upstream {
		t.Fatal("two renders were handed the same *Upstream")
	}
	a.Upstream.DisplayName = "Closed"
	if b.Upstream.DisplayName != "In Progress" {
		t.Errorf("one render's edit changed another's: %q", b.Upstream.DisplayName)
	}
	c := r.Resolve(context.Background(), refs)[0]
	if c.Upstream.DisplayName != "In Progress" {
		t.Errorf("the cache itself was mutated through a returned pointer: %q", c.Upstream.DisplayName)
	}
}

// TestResolveScanFeedsMissesBackToTheKeySet.
//
// markdown.Result carries UnknownKeys because internal/markdown is a leaf with
// no logger, and a caller that took References and dropped UnknownKeys would
// leave a stale key set with no signal at all: the rendered output of a miss is
// ordinary prose, which looks exactly like nothing being wrong.
func TestResolveScanFeedsMissesBackToTheKeySet(t *testing.T) {
	clock := newClock()
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) { return []string{"SWY"}, nil })
	tr := &scriptedTransport{clock: clock, base: clock.now()}
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: tr},
		Keys:       k,
	})

	got := r.ResolveScan(context.Background(), markdown.Result{
		References:  []markdown.Reference{swRef("SWY-1")},
		UnknownKeys: []string{"NEWP"},
	})
	if len(got) != 1 || got[0].State != StateResolved {
		t.Fatalf("resolutions = %+v", got)
	}
	if n := len(k.signal); n != 1 {
		t.Errorf("the miss was not passed to the key set: %d refreshes queued, want 1", n)
	}
}
