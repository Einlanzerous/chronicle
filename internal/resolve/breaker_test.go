package resolve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/markdown"
)

func manyRefs(n int) []markdown.Reference {
	out := make([]markdown.Reference, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, swRef(fmt.Sprintf("SWY-%d", i)))
	}
	return out
}

// TestTheBreakersSurplusIsUncheckedAndCostsNoDials.
//
// A reference behind a tripped breaker was NOT ASKED, so it is unchecked rather
// than unreachable: "Chronicle asked and could not obtain an answer" is a
// different claim from "nobody asked", and Chronicle carries unchecked in the
// payload -- where Switchyard keeps it in the browser -- precisely so a client
// can tell them apart.
func TestTheBreakersSurplusIsUncheckedAndCostsNoDials(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now(), cost: DefaultTimeout,
		answer: func(string) Answer { return Answer{Err: fmt.Errorf("dial tcp: connection refused")} }}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

	got := r.Resolve(context.Background(), manyRefs(30))

	if tr.count() != 1 {
		t.Errorf("made %d calls, want exactly one — the first pays the timeout and the rest do not", tr.count())
	}
	if got[0].State != StateUnreachable {
		t.Errorf("the reference that was asked is %q, want unreachable", got[0].State)
	}
	for i, res := range got[1:] {
		if res.State != StateUnchecked {
			t.Fatalf("ref %d behind the breaker is %q, want unchecked — nobody asked about it", i+1, res.State)
		}
		if !strings.Contains(res.Explain, "did not answer earlier") {
			t.Errorf("ref %d explain = %q, want it to name the breaker rather than the budget", i+1, res.Explain)
		}
		if !res.FetchedAt.IsZero() {
			t.Errorf("ref %d has a FetchedAt but was never attempted", i+1)
		}
	}
}

// hijackServer answers every request by counting it and then dropping the
// connection, which is a transport failure the server can still count.
func hijackServer(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// TestTheFailureStampStopsThePerRenderProbe.
//
// ============================================================================
// THE DEFECT THIS TEST EXISTS FOR.
// ============================================================================
//
// A cached unreachable is a HIT, and a hit trips nothing. With a breaker that
// died at the end of a render, every render during an outage found its uncached
// references, dialled one, and paid a full timeout -- for the outage's whole
// duration, once per concurrent poller, with the entries never converging
// because the 10s window expires them faster than one-probe-per-render refills
// them. That is the stampede the failure window exists to prevent, arriving
// through the one path that had no entry to cache.
//
// The stamp is FIDELITY_FAIL_TTL_MS's reasoning one level up, and it is not the
// poisoning the cache refuses: it records a measurement that WAS made, about
// the SERVICE, and every reference reading it still answers "not checked"
// rather than borrowing a verdict.
func TestTheFailureStampStopsThePerRenderProbe(t *testing.T) {
	srv, calls := hijackServer(t)
	clock := newClock()
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: overHTTP(t, srv)},
	})
	refs := manyRefs(30)

	first := r.Resolve(context.Background(), refs)
	if n := calls.Load(); n != 1 {
		t.Fatalf("first render made %d calls, want 1", n)
	}
	if first[0].State != StateUnreachable {
		t.Fatalf("first reference = %q, want unreachable", first[0].State)
	}

	// A second render inside the window asks NOTHING.
	clock.advance(DefaultFailureTTL / 2)
	second := r.Resolve(context.Background(), refs)
	if n := calls.Load(); n != 1 {
		t.Errorf("second render inside the failure window made %d calls in total, want still 1", n)
	}
	for i, res := range second {
		if i == 0 {
			// Served from its own cached failure.
			if res.State != StateUnreachable {
				t.Errorf("the cached failure is %q, want unreachable", res.State)
			}
			continue
		}
		if res.State != StateUnchecked {
			t.Errorf("ref %d = %q, want unchecked", i, res.State)
		}
	}

	// And the first render past the window probes exactly once again.
	clock.advance(DefaultFailureTTL)
	r.Resolve(context.Background(), refs)
	if n := calls.Load(); n != 2 {
		t.Errorf("after the window the render made %d calls in total, want 2", n)
	}
}

// TestTheBreakerAndStampArePerSystem.
//
// A shared breaker would let a Switchyard timeout answer every Amber card
// unchecked with an explain naming the wrong service -- a confident falsehood
// about a service that is fine, which is the mistake unconfigured exists to
// avoid, in a new spelling.
func TestTheBreakerAndStampArePerSystem(t *testing.T) {
	clock := newClock()
	base := clock.now()
	swDown := &scriptedTransport{clock: clock, base: base,
		answer: func(string) Answer { return Answer{Err: fmt.Errorf("connection refused")} }}
	amberUp := &scriptedTransport{clock: clock, base: base,
		answer: func(string) Answer { return Answer{Status: 200, Body: citeBody("held", "in the archive", false)} }}

	r := newResolver(t, clock, Options{Transports: map[string]Transport{
		markdown.SystemSwitchyard: swDown,
		markdown.SystemAmber:      amberUp,
	}})

	refs := []markdown.Reference{
		swRef("SWY-1"), amRef("amber1.a.b.0"), swRef("SWY-2"), amRef("amber1.c.d.1"),
		swRef("SWY-3"), amRef("amber1.e.f.2"),
	}
	got := r.Resolve(context.Background(), refs)

	for i, res := range got {
		if res.Ref.System != markdown.SystemAmber {
			continue
		}
		if res.State != StateResolved {
			t.Errorf("amber ref %d = %q, want resolved — Switchyard being down says nothing about the archive", i, res.State)
		}
		if strings.Contains(res.Explain, "ticket tracker") {
			t.Errorf("amber ref %d blames the ticket tracker: %q", i, res.Explain)
		}
	}
	if amberUp.count() != 3 {
		t.Errorf("amber was called %d times, want 3 — its breaker is its own", amberUp.count())
	}
}

// TestTheBudgetBoundsASlowButSucceedingUpstream.
//
// The breaker only fires on failure, and an upstream that is merely slow
// succeeds on every call while still running the page long. The budget asks
// whether a call can FINISH inside what is left -- `elapsed + timeout > budget`
// -- because gating the START would permit one more whole timeout past the
// ceiling, which is how a 5s ceiling becomes a 7s one.
func TestTheBudgetBoundsASlowButSucceedingUpstream(t *testing.T) {
	clock := newClock()
	base := clock.now()
	// Answers just under the per-call timeout, every time, and never fails.
	tr := &scriptedTransport{clock: clock, base: base, cost: DefaultTimeout - 100*time.Millisecond}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

	got := r.Resolve(context.Background(), manyRefs(30))

	counts := stateCounts(got)
	if counts[StateResolved] == 0 || counts[StateUnchecked] == 0 {
		t.Fatalf("states = %v, want some resolved and the surplus unchecked", counts)
	}
	if counts[StateResolved]+counts[StateUnchecked] != 30 {
		t.Fatalf("states = %v", counts)
	}
	if counts[StateUnreachable] != 0 {
		t.Errorf("a slow-but-succeeding upstream produced %d unreachable; the surplus was never asked",
			counts[StateUnreachable])
	}

	// The last call permitted to start does so at budget - timeout.
	window := DefaultBudget - DefaultTimeout
	tr.mu.Lock()
	started := append([]time.Duration(nil), tr.startedAt...)
	tr.mu.Unlock()
	for i, at := range started {
		if at > window {
			t.Errorf("call %d started at %v, past the effective window of %v", i, at, window)
		}
	}

	// The surplus says it was not checked, and says why.
	for _, res := range got {
		if res.State == StateUnchecked && !strings.Contains(res.Explain, "too slow") {
			t.Errorf("budget surplus explain = %q, want it to name the time rather than an outage", res.Explain)
		}
	}
}

// TestTheBudgetIsOneSharedWallClockAcrossBothSystems.
//
// Serial WITHIN a system -- the "fifty sockets at an archive that is already
// slow" argument is about one upstream's health -- and CONCURRENT ACROSS the
// two, because one serial pass would let Switchyard spend the whole window and
// leave a healthy archive's cards reading "too slow" on Switchyard's account.
func TestTheBudgetIsOneSharedWallClockAcrossBothSystems(t *testing.T) {
	clock := newClock()
	base := clock.now()

	const amberRefs = 5
	var amberSeen atomic.Int64
	amberDone := make(chan struct{})
	amberUp := &scriptedTransport{clock: clock, base: base, answer: func(string) Answer {
		if amberSeen.Add(1) == amberRefs {
			close(amberDone)
		}
		return Answer{Status: 200, Body: citeBody("held", "in the archive", false)}
	}}

	// Switchyard is slow, and holds off charging the clock until the archive
	// has answered everything -- which is what "Amber answers in milliseconds
	// while Switchyard stalls" looks like in wall-clock terms.
	swSlow := &scriptedTransport{clock: clock, base: base, cost: DefaultTimeout - 100*time.Millisecond,
		answer: func(string) Answer {
			select {
			case <-amberDone:
			case <-time.After(5 * time.Second):
				t.Error("the archive never finished; the two upstreams are not concurrent")
			}
			return Answer{Status: 200, Body: []byte(ticketBody)}
		}}

	r := newResolver(t, clock, Options{Transports: map[string]Transport{
		markdown.SystemSwitchyard: swSlow,
		markdown.SystemAmber:      amberUp,
	}})

	var refs []markdown.Reference
	for i := 0; i < 30; i++ {
		refs = append(refs, swRef(fmt.Sprintf("SWY-%d", i)))
	}
	for i := 0; i < amberRefs; i++ {
		refs = append(refs, amRef(fmt.Sprintf("amber1.a.b.%d", i)))
	}

	got := r.Resolve(context.Background(), refs)

	var amberResolved, swUnchecked int
	for _, res := range got {
		switch {
		case res.Ref.System == markdown.SystemAmber && res.State == StateResolved:
			amberResolved++
		case res.Ref.System == markdown.SystemSwitchyard && res.State == StateUnchecked:
			swUnchecked++
		}
	}
	if amberResolved != amberRefs {
		t.Errorf("%d of %d archive references resolved; a slow tracker must not spend the archive's budget",
			amberResolved, amberRefs)
	}
	if swUnchecked == 0 {
		t.Error("the slow tracker produced no unchecked surplus, so the shared budget never bound")
	}
}

// TestTheConstructorRefusesATimeoutThatCannotFinish.
//
// THE DEFECT, HELD AS A TEST. The finish-check works in Switchyard because
// AMBER_TIMEOUT_MS defaults to 5s against an 8s budget. Hand it Chronicle's
// 15s client timeout against an 8s budget and the FIRST reference fails
// `0 + 15000 > 8000`: nothing ever dials, every card answers without a call,
// and there is no error anywhere and no request in any log to say why.
func TestTheConstructorRefusesATimeoutThatCannotFinish(t *testing.T) {
	_, err := New(Options{Timeout: 15 * time.Second, Budget: 8 * time.Second})
	if err == nil {
		t.Fatal("a 15s timeout against an 8s budget was accepted; nothing would ever have dialled")
	}
	msg := err.Error()
	for _, want := range []string{"15s", "8s"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name %q", msg, want)
		}
	}

	if _, err := New(Options{Timeout: 5 * time.Second, Budget: 5 * time.Second}); err == nil {
		t.Error("an equal timeout and budget was accepted; the check is strictly smaller")
	}
	if _, err := New(Options{Timeout: DefaultTimeout, Budget: DefaultBudget}); err != nil {
		t.Errorf("the shipped numbers were refused: %v", err)
	}
}

// TestTheCapCountsAttemptsNotReferences.
//
// A cache hit is not a round trip. Counting references would mean a
// sixty-reference page never resolving positions 51-60 on ANY render, even when
// the first fifty are free; counting attempts lets a long page complete across
// two renders inside the success TTL, which is what a reader expects from a
// refresh. SWY-270: the surplus must read as *not checked*, never as a spinner
// with nothing behind it.
func TestTheCapCountsAttemptsNotReferences(t *testing.T) {
	clock := newClock()
	tr := &scriptedTransport{clock: clock, base: clock.now()}
	r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

	refs := manyRefs(60)
	first := r.Resolve(context.Background(), refs)

	if tr.count() != DefaultCap {
		t.Fatalf("first render attempted %d, want the cap of %d", tr.count(), DefaultCap)
	}
	counts := stateCounts(first)
	if counts[StateResolved] != DefaultCap || counts[StateUnchecked] != 60-DefaultCap {
		t.Fatalf("states = %v, want %d resolved and %d unchecked", counts, DefaultCap, 60-DefaultCap)
	}
	for _, res := range first {
		if res.State != StateUnchecked {
			continue
		}
		if !res.FetchedAt.IsZero() || res.Upstream != nil {
			t.Errorf("%s is unchecked but carries an attempt", res.Ref.Token)
		}
		if !strings.Contains(res.Explain, "more references than one render checks") {
			t.Errorf("cap surplus explain = %q", res.Explain)
		}
	}

	// The page completes on the next render: the first fifty are cache hits,
	// which cost no attempts, so the remaining ten get them.
	second := r.Resolve(context.Background(), refs)
	if n := stateCounts(second)[StateResolved]; n != 60 {
		t.Errorf("second render resolved %d of 60; a long page must complete across renders", n)
	}
	if tr.count() != 60 {
		t.Errorf("total calls = %d, want 60", tr.count())
	}
}

// TestOnlyTheOutcomeOfARealAttemptIsCached.
//
// Writing an unchecked would let one slow call condemn twenty-nine references
// on a measurement that was never made -- the copy-that-lies shape in
// miniature. Proved at both doors, because they fail differently.
func TestOnlyTheOutcomeOfARealAttemptIsCached(t *testing.T) {
	// Door 1: the breaker. One attempt is cached; the four behind it are not
	// knowledge and are not written.
	t.Run("behind the breaker", func(t *testing.T) {
		clock := newClock()
		fail := true
		tr := &scriptedTransport{clock: clock, base: clock.now(), answer: func(string) Answer {
			if fail {
				return Answer{Err: fmt.Errorf("connection refused")}
			}
			return Answer{Status: 200, Body: []byte(ticketBody)}
		}}
		r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

		refs := manyRefs(5)
		r.Resolve(context.Background(), refs)

		r.mu.Lock()
		cached := len(r.entries)
		r.mu.Unlock()
		if cached != 1 {
			t.Fatalf("%d entries after one attempt, want 1 — unchecked is not knowledge", cached)
		}

		// Past the window, with the upstream healthy, ALL FIVE are attempted:
		// nothing about the four was remembered, not even that they failed.
		fail = false
		clock.advance(DefaultFailureTTL + time.Millisecond)
		before := tr.count()
		got := r.Resolve(context.Background(), refs)
		if n := tr.count() - before; n != 5 {
			t.Errorf("re-render attempted %d, want all 5", n)
		}
		for i, res := range got {
			if res.State != StateResolved {
				t.Errorf("ref %d = %q, want resolved", i, res.State)
			}
		}
	})

	// Door 2: the cap, which sets no stamp, so the count is the direct proof.
	t.Run("past the cap", func(t *testing.T) {
		clock := newClock()
		tr := &scriptedTransport{clock: clock, base: clock.now()}
		r := newResolver(t, clock, Options{Transports: map[string]Transport{markdown.SystemSwitchyard: tr}})

		refs := manyRefs(60)
		r.Resolve(context.Background(), refs)

		r.mu.Lock()
		cached := len(r.entries)
		r.mu.Unlock()
		if cached != DefaultCap {
			t.Fatalf("%d entries after %d attempts, want %d — the ten past the cap were never asked",
				cached, DefaultCap, DefaultCap)
		}

		// The fifty hits cost no attempts, so the ten get them -- and are
		// resolved rather than served back as a remembered non-answer.
		before := tr.count()
		got := r.Resolve(context.Background(), refs)
		if n := tr.count() - before; n != 10 {
			t.Errorf("second render attempted %d, want the 10 that were never asked", n)
		}
		if n := stateCounts(got)[StateResolved]; n != 60 {
			t.Errorf("resolved %d of 60", n)
		}
	})
}
