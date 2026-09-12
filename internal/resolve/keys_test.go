package resolve

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/markdown"
)

// The renderer takes the key set through markdown.ProjectKeys, which is
// satisfied STRUCTURALLY so that internal/markdown stays a leaf package with no
// internal imports and no logger.
var _ markdown.ProjectKeys = (*Keys)(nil)

func newKeys(t *testing.T, clock *fakeClock, fetch func(context.Context) ([]string, error)) (*Keys, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	k := NewKeys(KeysOptions{
		Fetch:  fetch,
		Now:    clock.now,
		Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	return k, &buf
}

// TestTheLastGoodKeySetSurvivesAFailedRefresh is ruling 2, and the whole of it.
//
// cfaccess.go, on the JWKS: "A failed fetch leaves the previous key set in
// place. Replacing a working set with nothing would turn a Cloudflare blip into
// a total [outage]." Here the outage would be every ticket reference in the
// corpus silently reverting to prose -- unreachable rendering as nothing, which
// is what CHRN-48 handed this ticket to prevent.
func TestTheLastGoodKeySetSurvivesAFailedRefresh(t *testing.T) {
	clock := newClock()
	var fail atomic.Bool
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) {
		if fail.Load() {
			return nil, fmt.Errorf("dial tcp 172.19.0.9:4002: connect: connection refused")
		}
		return []string{"SWY", "CHRN", "AMBR", "SERV", "IDEA"}, nil
	})

	if err := k.refresh(context.Background()); err != nil {
		t.Fatalf("priming the set: %v", err)
	}
	if !k.HasProject("SWY") {
		t.Fatal("SWY is not a live project after a good fetch")
	}

	fail.Store(true)
	clock.advance(KeysCooldown + time.Second)
	if err := k.refresh(context.Background()); err == nil {
		t.Fatal("the failing fetch reported success")
	}

	// THE SET IS STILL THERE. This is the assertion the ruling is about.
	if !k.HasProject("SWY") {
		t.Error("a failed refresh dropped the key set: every ticket reference in the corpus just became prose")
	}
	if !k.HasProject("CHRN") || k.HasProject("SY") {
		t.Error("the surviving set is not the one that was fetched")
	}
	if !k.Ready() {
		t.Error("Ready went false after a failed refresh")
	}
}

// TestTheCooldownIsStampedBeforeTheFetch.
//
// cfaccess.go records why the other way is wrong: gating on success makes the
// cooldown inert in exactly the two situations it exists for -- a cold start
// and a failing upstream -- because nothing is ever held, so every caller
// fetches again immediately.
func TestTheCooldownIsStampedBeforeTheFetch(t *testing.T) {
	clock := newClock()
	var calls atomic.Int64
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) {
		calls.Add(1)
		return nil, fmt.Errorf("connection refused")
	})

	if !k.tryRefresh(context.Background()) {
		t.Fatal("the first refresh was refused")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}

	// Every attempt inside the cooldown is refused, even though the one before
	// it FAILED -- which is the whole point.
	for i := 0; i < 5; i++ {
		clock.advance(KeysCooldown / 10)
		if k.tryRefresh(context.Background()) {
			t.Fatalf("attempt %d inside the cooldown was allowed through", i)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("a failing Switchyard was dialled %d times inside one cooldown", calls.Load())
	}

	clock.advance(KeysCooldown)
	if !k.tryRefresh(context.Background()) {
		t.Error("the cooldown never lapsed")
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
}

// TestNoteMissesNeverFetchesInsideARender.
//
// A paginated /v1/projects call inside the request is the precise thing the
// batch budget exists to keep out, and the miss is only visible after Scan has
// already emitted the token as prose -- so the refresh it asks for benefits the
// NEXT render, and this one must not wait for it.
func TestNoteMissesNeverFetchesInsideARender(t *testing.T) {
	clock := newClock()
	var calls atomic.Int64
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond) // a paginated fetch is not instant
		return []string{"SWY"}, nil
	})

	start := time.Now()
	for i := 0; i < 20; i++ {
		k.NoteMisses([]string{"SY", "HTTP"})
	}
	elapsed := time.Since(start)

	if calls.Load() != 0 {
		t.Errorf("NoteMisses fetched %d times; a render must never wait on the project list", calls.Load())
	}
	if elapsed > 20*time.Millisecond {
		t.Errorf("NoteMisses took %v; it must return immediately", elapsed)
	}

	// It asked for one refresh, not twenty: the signal coalesces.
	if n := len(k.signal); n != 1 {
		t.Errorf("twenty misses queued %d refreshes, want 1", n)
	}
}

// TestWithNoKeySetEveryTicketTokenIsProseAndSaysSoOnce.
//
// The residue this package accepts: with no set ever fetched, SWY-389 and UTF-8
// are genuinely indistinguishable, so both stay prose -- CHRN-48 ruling 3's own
// answer, where "unknown" includes "nobody could ask". Prose looks exactly like
// nothing being wrong, so the log line is the only signal there is, and it is
// emitted ONCE PER TRANSITION because a hot page would otherwise flood the log
// at the moment somebody is reading it.
func TestWithNoKeySetEveryTicketTokenIsProseAndSaysSoOnce(t *testing.T) {
	clock := newClock()
	keys := []string{"SWY", "CHRN"}
	k, logs := newKeys(t, clock, func(context.Context) ([]string, error) {
		return keys, nil
	})

	// Thirty renders' worth of lookups, with no set.
	for i := 0; i < 30; i++ {
		if k.HasProject("SWY") {
			t.Fatal("HasProject guessed with no set")
		}
		if k.HasProject("UTF") {
			t.Fatal("HasProject guessed with no set")
		}
	}
	if n := strings.Count(logs.String(), "rendering with no Switchyard project key set"); n != 1 {
		t.Errorf("the no-key-set warning was logged %d times across 60 lookups, want once per transition", n)
	}

	// The transition ends when a set arrives.
	if err := k.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if !k.HasProject("SWY") {
		t.Error("SWY is still not a project after a good fetch")
	}
	if k.HasProject("UTF") {
		t.Error("UTF became a project")
	}
	if n := strings.Count(logs.String(), "rendering with no Switchyard project key set"); n != 1 {
		t.Errorf("the warning was repeated after the set arrived")
	}
}

// TestAKeySetThatIsHeldKeepsTicketReferencesResolvable is the end-to-end shape
// of ruling 2: the grammar does not collapse when the tracker does.
func TestAKeySetThatIsHeldKeepsTicketReferencesResolvable(t *testing.T) {
	clock := newClock()
	var fail atomic.Bool
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) {
		if fail.Load() {
			return nil, fmt.Errorf("connection refused")
		}
		return []string{"SWY"}, nil
	})
	if err := k.refresh(context.Background()); err != nil {
		t.Fatalf("priming: %v", err)
	}

	fail.Store(true)
	clock.advance(KeysCooldown + time.Second)
	_ = k.tryRefresh(context.Background())

	// The token still MARKS -- it does not silently become prose ...
	if !k.HasProject("SWY") {
		t.Fatal("the key set collapsed with the tracker")
	}

	// ... and it resolves to an honest unreachable rather than to nothing.
	tr := &scriptedTransport{clock: clock, base: clock.now(),
		answer: func(string) Answer { return Answer{Err: fmt.Errorf("connection refused")} }}
	r := newResolver(t, clock, Options{
		Transports: map[string]Transport{markdown.SystemSwitchyard: tr},
		Keys:       k,
	})
	got := r.Resolve(context.Background(), []markdown.Reference{swRef("SWY-389")})[0]
	if got.State != StateUnreachable {
		t.Errorf("state = %q, want unreachable — an outage must not render as nothing", got.State)
	}
}

// TestRunFetchesAtBootAndAgainOnAMiss covers the goroutine that is the only
// thing calling tryRefresh in production.
func TestRunFetchesAtBootAndAgainOnAMiss(t *testing.T) {
	clock := newClock()
	fetched := make(chan struct{}, 8)
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) {
		fetched <- struct{}{}
		return []string{"SWY", "CHRN"}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)

	select {
	case <-fetched:
	case <-time.After(2 * time.Second):
		t.Fatal("Run never made the boot fetch")
	}

	deadline := time.Now().Add(2 * time.Second)
	for !k.Ready() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !k.Ready() {
		t.Fatal("the set never became ready after a successful boot fetch")
	}

	// A miss past the cooldown provokes exactly one more fetch, in the
	// background, benefiting the next render.
	clock.advance(KeysCooldown + time.Second)
	k.NoteMisses([]string{"SY"})
	select {
	case <-fetched:
	case <-time.After(2 * time.Second):
		t.Fatal("a miss did not provoke a background refresh")
	}
}

// TestTheBootFetchRetriesRatherThanGivingUp.
//
// A Chronicle that started before Switchyard did would otherwise hold no key
// set for as long as it stayed up, and every ticket reference in the corpus
// would be prose with one warn line to explain it.
func TestTheBootFetchRetriesRatherThanGivingUp(t *testing.T) {
	clock := newClock()
	var attempts atomic.Int64
	ready := make(chan struct{})
	k, _ := newKeys(t, clock, func(context.Context) ([]string, error) {
		if attempts.Add(1) == 1 {
			return nil, fmt.Errorf("connection refused")
		}
		close(ready)
		return []string{"SWY"}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go k.Run(ctx)

	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the boot fetch gave up after one failure")
	}
	if n := attempts.Load(); n < 2 {
		t.Errorf("attempts = %d, want a retry", n)
	}
}
