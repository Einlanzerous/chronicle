package resolve

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// ============================================================================
// THE PROJECT KEY SET: MEMBERSHIP, NOT STATE, AND HELD ACCORDINGLY.
// ============================================================================
//
// CHRN-48 ruling 2 handed this ticket the key set's lifetime, and its ruling 3
// settled that with NO key set every KEY-N token renders as prose. Those two
// collide in one case: if the set expired like a resolution does, a Switchyard
// outage would silently turn every ticket reference on every page back into
// prose -- "unreachable rendering as nothing", which CHRN-50's Done-when and
// CHRN-51's both forbid.
//
// The framing was the problem. A RESOLUTION IS VOLATILE -- a ticket transitions
// several times a week, which is why its TTL is a minute. MEMBERSHIP IS NOT: a
// project is created maybe monthly, the set is fifteen strings, and per
// CHRN-48's own argument it drives RECOGNITION, whose stale failure is visible
// on the page and self-correcting on refresh. Holding it is cheap and losing it
// is expensive.
//
// So this takes all three parts of the rule internal/api/cfaccess.go already
// applies to the JWKS, including the one that matters most:
//
//	"A failed fetch leaves the previous key set in place. Replacing a working
//	 set with nothing would turn a Cloudflare blip into a total [outage]."
//
// ============================================================================
// ONE RESIDUE, STATED RATHER THAN HIDDEN.
// ============================================================================
//
// A cold start that has never once reached Switchyard renders ticket tokens as
// prose. That is a real instance of unreachable-rendering-as-nothing, and it is
// accepted for a reason that is not convenience: WITH NO KEY SET, SWY-389 AND
// UTF-8 ARE GENUINELY INDISTINGUISHABLE. Marking both as unreachable cards is
// the wildcard grammar CHRN-48 ruling 1 refused, arriving through another door;
// leaving both as prose is that ruling's own answer, where "unknown" includes
// "nobody could ask". The window is one boot plus one successful call, the boot
// fetch retries with backoff rather than being one-shot, and it is logged.

const (
	// KeysMaxAge is how long a good set is served before a refresh, and matches
	// cfJWKSCacheMaxAge.
	KeysMaxAge = 10 * time.Minute

	// KeysCooldown throttles miss-triggered refreshes, and matches
	// cfJWKSRefreshCooldown. STAMPED BEFORE THE FETCH, NOT AFTER: cfaccess.go
	// records why the other way is inert in exactly the two situations it
	// exists for -- a cold start and a failing upstream, where nothing is ever
	// held so every request fetches again immediately.
	KeysCooldown = 30 * time.Second

	// keysBootBackoffMax bounds the cold-start retry.
	keysBootBackoffMax = 30 * time.Second
)

// KeysOptions configures a Keys.
type KeysOptions struct {
	// Fetch reads the live project keys. A func rather than a client, so this
	// package ships no transport -- CHRN-49 passes switchyard.Client.Projects.
	Fetch func(ctx context.Context) ([]string, error)

	Logger   *slog.Logger
	Now      func() time.Time
	MaxAge   time.Duration
	Cooldown time.Duration
}

// Keys is the live Switchyard project key set.
//
// It satisfies markdown.ProjectKeys structurally, which is how the renderer
// takes it without internal/markdown acquiring an import: that package is a
// leaf with no logger by design, and misses come back from it as data.
type Keys struct {
	fetch    func(ctx context.Context) ([]string, error)
	logger   *slog.Logger
	now      func() time.Time
	maxAge   time.Duration
	cooldown time.Duration

	// signal carries a miss from a render to the background refresher. Buffered
	// and sent non-blockingly, because A RENDER MUST NEVER WAIT ON THIS: a
	// paginated /v1/projects fetch inside the request is the precise thing the
	// batch budget exists to keep out.
	signal chan struct{}

	mu        sync.Mutex
	set       map[string]struct{}
	have      bool
	fetchedAt time.Time
	lastTry   time.Time
	warned    bool
}

// NewKeys builds a key set holder. It fetches nothing; call Run.
func NewKeys(o KeysOptions) *Keys {
	k := &Keys{
		fetch:    o.Fetch,
		logger:   o.Logger,
		now:      o.Now,
		maxAge:   o.MaxAge,
		cooldown: o.Cooldown,
		signal:   make(chan struct{}, 1),
		set:      map[string]struct{}{},
	}
	if k.logger == nil {
		k.logger = slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	}
	if k.now == nil {
		k.now = time.Now
	}
	if k.maxAge <= 0 {
		k.maxAge = KeysMaxAge
	}
	if k.cooldown <= 0 {
		k.cooldown = KeysCooldown
	}
	return k
}

// HasProject reports whether a key names a live Switchyard project.
//
// With no set ever fetched this answers false for everything, so every
// ticket-shaped token stays prose -- and says so ONCE PER TRANSITION rather
// than once per render, because a hot page during a cold-start outage would
// otherwise flood the log at the moment somebody is reading it. It is the only
// signal there is: prose looks exactly like nothing being wrong.
func (k *Keys) HasProject(key string) bool {
	k.mu.Lock()
	if !k.have {
		warn := !k.warned
		k.warned = true
		k.mu.Unlock()
		if warn {
			k.logger.Warn("rendering with no Switchyard project key set: ticket references will stay plain text",
				"remedy", "set CHRONICLE_SWITCHYARD_URL and CHRONICLE_SWITCHYARD_TOKEN, and check the tracker is reachable")
		}
		return false
	}
	_, ok := k.set[key]
	k.mu.Unlock()
	return ok
}

// Ready reports whether a set has ever been fetched.
func (k *Keys) Ready() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.have
}

// NoteMisses records well-shaped keys the set rejected, and asks for a refresh.
//
// IT RETURNS IMMEDIATELY AND NEVER FETCHES. The caller is a render, and the
// refresh it triggers benefits the NEXT one: markdown.Renderer.Scan has already
// emitted the token as prose by the time the miss is visible here, and making
// this render benefit would mean re-scanning after a fetch -- a paginated call
// inside the request. One render's lag is the price, and it is cheap against
// the alternative of a full refresh interval.
func (k *Keys) NoteMisses(keys []string) {
	if len(keys) == 0 {
		return
	}
	k.logger.Debug("well-shaped keys that name no live project",
		"keys", keys, "note", "a stale key set, or somebody wrote a key that does not exist")
	select {
	case k.signal <- struct{}{}:
	default:
		// A refresh is already pending. One is enough.
	}
}

// Run keeps the set current until ctx is done. Intended as a goroutine.
func (k *Keys) Run(ctx context.Context) {
	if k.fetch == nil {
		return
	}

	// The boot fetch, RETRIED WITH BACKOFF rather than attempted once: a
	// Chronicle that started before Switchyard did would otherwise hold no key
	// set for as long as it stayed up, and every ticket reference in the corpus
	// would be prose with one warn line to explain it.
	//
	// It calls refresh directly rather than through the cooldown, because here
	// the backoff IS the throttle.
	backoff := time.Second
	for !k.Ready() {
		if err := k.refresh(ctx); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < keysBootBackoffMax {
			backoff *= 2
		}
	}

	t := time.NewTicker(k.maxAge)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			k.tryRefresh(ctx)
		case <-k.signal:
			k.tryRefresh(ctx)
		}
	}
}

// tryRefresh refreshes unless the cooldown says it is too soon.
//
// Returns whether a fetch was attempted, which is what the cooldown's test
// asserts: a hot note carrying a key that names nothing must provoke at most
// one call per cooldown, however many times it is rendered.
func (k *Keys) tryRefresh(ctx context.Context) bool {
	k.mu.Lock()
	if !k.lastTry.IsZero() && k.now().Sub(k.lastTry) < k.cooldown {
		k.mu.Unlock()
		return false
	}
	k.mu.Unlock()
	// The error is the refresh's to log and to absorb: the caller asked whether
	// the cooldown allowed an attempt, not whether the upstream was up.
	_ = k.refresh(ctx)
	return true
}

// refresh fetches the live set, and keeps the old one when it cannot.
func (k *Keys) refresh(ctx context.Context) error {
	if k.fetch == nil {
		return nil
	}

	// STAMPED FOR THE ATTEMPT, BEFORE THE FETCH. Stamping on success would let
	// a failing Switchyard be re-dialled by every render for as long as it
	// stayed down.
	k.mu.Lock()
	k.lastTry = k.now()
	k.mu.Unlock()

	keys, err := k.fetch(ctx)
	if err != nil {
		// THE PREVIOUS SET STAYS. This is the whole of ruling 2: a blip must
		// not silently un-link every ticket reference in the corpus.
		k.logger.Warn("could not refresh the Switchyard project key set; keeping the last good one",
			"error", err, "have_set", k.Ready())
		return err
	}

	next := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key != "" {
			next[key] = struct{}{}
		}
	}

	k.mu.Lock()
	first := !k.have
	k.set = next
	k.have = true
	k.fetchedAt = k.now()
	k.warned = false
	k.mu.Unlock()

	if first {
		k.logger.Info("Switchyard project key set loaded; ticket references will resolve", "projects", len(next))
	}
	return nil
}
