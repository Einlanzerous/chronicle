// Package resolve turns estate references into live upstream state.
//
// ============================================================================
// A RESOLUTION IS AN ANSWER *ABOUT* A REFERENCE, NEVER A COPY *OF* ONE.
// ============================================================================
//
// CHRN-48 shipped the parser under the rule that a marker carries "the kind and
// the token and NOTHING THAT CAN GO STALE". This package is the deliberate
// exception: it holds upstream state, in memory, for sixty seconds. Invariant 2
// permits that and draws the line one sentence later -- "a cache with no visible
// staleness is a copy that lies" -- so everything here exists to keep the cache
// on the right side of it.
//
// THE ONE DISTINCTION EVERYTHING RESTS ON:
//
//	a fact about the REFERENT  is not  a fact about our ABILITY TO ASK.
//
// Drawn on the type rather than left to a convention: Upstream is present when
// and only when an upstream answered, and State carries whether anybody could
// ask. Nil means nobody answered. Never nil beside a status, and never a status
// with no answer.
//
// ============================================================================
// NOTHING HERE IS STORED. THERE IS NO MIGRATION AND NO COLUMN.
// ============================================================================
//
// The cache is a map that dies with the process, readable by nothing but the
// code that wrote it. A cache in a table is a durable artefact with a schema,
// and the distance from there to a join is one convenience -- which is the
// third source of truth invariant 2 exists to prevent.
//
// ============================================================================
// WHAT THIS PACKAGE DOES NOT DO.
// ============================================================================
//
// It makes no HTTP call itself. CHRN-51 decided the contract, the cache and the
// classification; CHRN-49 and CHRN-50 supply one Transport each. That inversion
// of the ticket numbers is CHRN-96's: "E7's CHRN-51 decides what a resolved
// reference looks like in a payload […] designing GET /notes/CHR-0311 before
// that is designing the card blind."
//
// Chronicle's own CHR-#### and DSC-#### references are NOT this package's
// subject -- they resolve locally against tier2 number columns, with no network
// and no staleness. A caller either resolves them itself or registers a
// Transport for them; one arriving here with neither answers Unconfigured,
// which is true rather than a guess.
package resolve

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Einlanzerous/chronicle/internal/markdown"
)

// State is what Chronicle can say about one reference.
//
// FIVE, NOT TWO, AND THE LENGTH IS THE POINT -- the same argument AMBR-11 made
// when it published an outcome vocabulary instead of a 404, and Switchyard
// repeats in client/src/lib/citationOutcome.ts: "a finding is falsifiable only
// while its evidence stays reachable, so […] three different claims about how
// far the finding can still be trusted, and only one of them is anybody's
// fault."
//
// Collapsing Broken into Unreachable understates a real problem. Collapsing
// Unreachable into Broken states a confident falsehood about somebody's work.
// Collapsing either into Unchecked is the silent failure this package is
// written against.
type State string

const (
	// StateResolved -- the upstream answered and Upstream is its answer.
	StateResolved State = "resolved"

	// StateBroken -- the upstream answered that there is no such thing, or that
	// the reference is wrong. Still a fact about the referent, so Upstream is
	// present and carries whatever the answer contained.
	StateBroken State = "broken"

	// StateUnreachable -- Chronicle ASKED and could not obtain an answer. A
	// fact about Chronicle's knowledge, and the one no upstream can report:
	// a service that is down does not answer to say so.
	StateUnreachable State = "unreachable"

	// StateUnconfigured -- this deployment has no credential for that upstream,
	// so nothing was dialled and nothing ever will be until a redeploy.
	//
	// NOT FOLDED INTO Unreachable, for Switchyard's own reason: "an instance
	// with no Amber is in a permanent state, not an outage, and a renderer that
	// cannot tell those apart states a confident falsehood." A card reading
	// "Switchyard unreachable" on a Chronicle that was never given a token
	// sends somebody to check a service that is fine.
	StateUnconfigured State = "unconfigured"

	// StateUnchecked -- NOBODY ASKED. The cap, the budget or the failure stamp
	// declined to dial, and no claim about this reference is being made.
	//
	// SWY-270's lesson, promoted from Switchyard's browser into this payload
	// because a JSON client cannot derive it: an `undefined` that meant both
	// "in flight" and "never looked up" left citations reading `checking…`
	// permanently, "which is worse than any of the nine outcomes: the others
	// are claims that can be wrong, and this one is a claim that will never be
	// answered."
	StateUnchecked State = "unchecked"
)

// Resolution is one reference's answer.
type Resolution struct {
	// Ref is the reference as written, echoed back so a caller can align the
	// answer with the token without depending on slice order.
	Ref markdown.Reference

	State State

	// Upstream is present whenever the upstream ANSWERED -- Resolved AND Broken
	// -- and nil on the three states where nobody answered.
	//
	// NIL MEANS NOBODY ANSWERED. The boundary is *did anyone answer*, not *was
	// the answer good news*, because Amber's five broken outcomes are exactly
	// where prompt_only and not_captured differ and Recoverable stops being a
	// constant.
	Upstream *Upstream

	// FetchedAt is when the attempt above was made. Set for Resolved, Broken
	// AND Unreachable; zero only where nothing was attempted at all, which is
	// Unchecked and Unconfigured.
	//
	// NOT OPTIONAL AND NOT A POINTER. A status without the instant it was true
	// is the copy that lies, so the type refuses to express one.
	FetchedAt time.Time

	// LastResolvedAt is when this reference last resolved SUCCESSFULLY, within
	// this process's memory. On Unreachable it is the only thing carried
	// forward -- "Switchyard unreachable, last answered 6 minutes ago" tells a
	// reader how long the outage has run and carries no status to misrender.
	LastResolvedAt *time.Time

	// Explain is the upstream's own sentence where it has one (Amber relays
	// `explain` verbatim rather than Chronicle keeping a second copy that
	// drifts from /v1/cite/format), and Chronicle's for the states no upstream
	// can report.
	//
	// IT NEVER CARRIES A RELATIVE TIME. "did not answer 4 s ago" is true at
	// serialisation and false in the hands of a client holding the payload --
	// an age with no timestamp beside it, which is the same defect this package
	// refuses for values. The operator's instant lives in the log line.
	Explain string
}

// Upstream is what the upstream said, and exists only when it said something.
type Upstream struct {
	// Key is the key AS ANSWERED, which is not always the key as written:
	// Switchyard's resolveTicket falls through to ticket_aliases after a move,
	// so GET /v1/tickets/IDEA-21 can answer 200 with key CHRN-7 -- and CHRN
	// itself graduated from IDEA-21. Empty for Amber, which has no key.
	Key string

	// Outcome is the UPSTREAM'S OWN vocabulary member, where the upstream
	// publishes one. Amber always does on a cite answer. Switchyard publishes a
	// status category for a live ticket and a bare 404 for a missing one -- it
	// has no member meaning "deleted" -- so this is EMPTY on a Switchyard
	// Broken, and State alone is the answer. Chronicle does not mint a member
	// in somebody else's namespace to avoid an empty field.
	Outcome string

	// DisplayName is the status as a person reads it. Switchyard only.
	DisplayName string

	// Title is Switchyard's. Amber's card has no title to show.
	Title string

	// URL is the outbound arrow's target, and is empty where one would not
	// land: a deleted ticket 404s, and Amber serves no HTML at all.
	URL string

	// Recoverable is Amber's, relayed verbatim, and is MEANINGFUL ONLY ON
	// Broken. cite.Describe()'s two true rows are not_in_capture and ambiguous;
	// held -- the only resolved outcome -- is a constant false, so reading it
	// on Resolved learns nothing.
	Recoverable bool
}

// clone copies an Upstream on its way out of the cache.
//
// ONE ENTRY IS READ BY EVERY CONCURRENT RENDER, and handing them all the same
// pointer would let a caller that touched one field change what every other
// reader sees -- a shared mutable copy of upstream state, which is the thing
// this package exists in order not to have. The struct is six fields; the copy
// is free beside the round trip it saved.
func (u *Upstream) clone() *Upstream {
	if u == nil {
		return nil
	}
	c := *u
	return &c
}

// Answer is what one Transport call came back with, before anybody has decided
// what it means.
//
// RAW BYTES RATHER THAN A DECODED STRUCT, because which shape the body has is
// the classification question and not an input to it: Amber answers a cite body
// at 200/404/409/410 and an {"error": …} body at 400/401/503, and a client that
// picked a struct first would have had to guess which.
type Answer struct {
	// Err is a TRANSPORT failure: no usable response at all. A non-2xx status
	// is not an error here -- it is an answer, and for Amber it is the common
	// case. A *switchyard.Error is understood and read for its status.
	Err error

	Status int
	Body   []byte
}

// Transport makes one call. It never classifies and never retries.
//
// The seam CHRN-49 and CHRN-50 plug into. Splitting it here is what stops two
// evidence-mode tickets from independently inventing the broken-versus-
// unreachable mapping that this ticket exists to decide.
type Transport interface {
	// Fetch makes exactly one call for one token. ctx already carries the
	// per-call deadline; a Transport must not install its own.
	Fetch(ctx context.Context, ref markdown.Reference) Answer

	// URLFor is the deep link for a key the upstream answered with. It returns
	// "" when there is nowhere for a reader's browser to go -- Amber's case,
	// which serves no HTML at all.
	URLFor(key string) string
}

// Defaults. Every one of them is a number somebody chose; the reasons are in
// docs and in the plan on CHRN-51, and the ones that interact are guarded in
// New rather than left to agree by luck.
const (
	// DefaultSuccessTTL matches Switchyard's CITE_TTL_MS. A status change is
	// invisible for at most this long, which is the whole cost of not dialling
	// on every render.
	DefaultSuccessTTL = 60 * time.Second

	// DefaultFailureTTL matches Switchyard's FIDELITY_FAIL_TTL_MS, whose
	// comment is the argument: "the one state where Amber is already in trouble
	// is the state where we hammer it hardest […] recovery should be noticed
	// quickly, but not at the cost of a stampede."
	DefaultFailureTTL = 10 * time.Second

	// DefaultTimeout is per call, and is deliberately NOT switchyard's
	// DefaultTimeout of 15s. That number was chosen for a triage batch -- a
	// background write that must land, where waiting is correct. A render is a
	// user-facing read where waiting is not: the card says so and the next
	// render retries. Both upstreams are one hop away on construct_net, and
	// internal/switchyard's own comment says Switchyard "answers in
	// milliseconds; a slow answer is a problem rather than something to wait
	// patiently through".
	DefaultTimeout = 2 * time.Second

	// DefaultBudget is the wall-clock ceiling for one render, shared by both
	// upstreams because a reader's patience is a property of the page.
	//
	// CHOSEN, NOT DERIVED, and that is worth saying because Switchyard's 8s IS
	// derived -- from Bun's 10s idle timeout closing the socket. Chronicle's
	// server sets only ReadHeaderTimeout (cmd/chronicle/main.go), so there is
	// no WriteTimeout and NOTHING ELSE IN THE PROCESS WILL STOP A SLOW RENDER.
	// This is the only ceiling there is.
	DefaultBudget = 5 * time.Second

	// DefaultCap matches Switchyard's CITATION_RESOLVE_CAP, and it counts
	// ATTEMPTS rather than references -- see Resolve.
	DefaultCap = 50

	// DefaultCacheMax matches CITE_CACHE_MAX, and is cleared wholesale for its
	// reason: an LRU is "machinery for a cache whose entire job is to absorb
	// one page's worth of repeat lookups", and a cold rebuild costs one round
	// trip per visible card.
	DefaultCacheMax = 500
)

// Options configures a Resolver.
type Options struct {
	// Transports is keyed by markdown.System*. A reference whose system has no
	// entry answers Unconfigured and never dials.
	Transports map[string]Transport

	// Keys is the live project key set. Optional here: it is the renderer's
	// oracle rather than the resolver's, and is carried so that a miss observed
	// during a render can trigger its refresh.
	Keys *Keys

	Logger *slog.Logger

	// Now is injectable so every window in this package is testable without
	// sleeping.
	Now func() time.Time

	Timeout    time.Duration
	Budget     time.Duration
	Cap        int
	SuccessTTL time.Duration
	FailureTTL time.Duration
	CacheMax   int
}

// Resolver resolves references against zero or more upstreams, caching what it
// learns for as long as it is worth believing.
type Resolver struct {
	transports map[string]Transport
	keys       *Keys
	logger     *slog.Logger
	now        func() time.Time

	timeout    time.Duration
	budget     time.Duration
	cap        int
	successTTL time.Duration
	failureTTL time.Duration
	cacheMax   int

	// mu guards entries and stamps. Renders are concurrent -- three clients
	// poll the same corpus in E8, E9 and E10 -- and both maps are shared.
	mu      sync.Mutex
	entries map[string]entry
	stamps  map[string]stamp
}

// entry is one cached answer.
type entry struct {
	state    State
	upstream *Upstream
	explain  string
	at       time.Time

	// lastOK SURVIVES A FAILURE OVERWRITE. A failure written at the 10s window
	// must not clear it, or "last answered 6 minutes ago" becomes "never
	// answered" the first time an upstream blinks.
	lastOK *time.Time
}

// stamp records the last answer that was a claim about the SERVICE rather than
// about one reference.
//
// THE BREAKER AND THE CROSS-RENDER STAMP ARE ONE MECHANISM, not two. Read
// within a render it is Switchyard's per-batch breaker; read across renders it
// is FIDELITY_FAIL_TTL_MS's reasoning one level up. Keeping them separate
// bought a second Unchecked door that a client would have had to tell apart by
// string, for one fact.
//
// WITHOUT IT, an outage costs every render a full timeout, forever. A cached
// Unreachable is a HIT, and a hit does not trip anything -- so each render found
// its uncached references, dialled one, and waited; once per concurrent poller;
// with the entries never converging because the 10s window expires them faster
// than one-probe-per-render refills them. That is the stampede the failure
// window exists to prevent, arriving through the one path with no entry.
//
// IT IS NOT THE POISONING THE CACHE REFUSES, and the difference is what the
// record is about: caching an Unchecked would record a conclusion about a
// REFERENCE that was never measured, while this records a measurement that WAS
// made, about the SERVICE. Every reference reading it still answers "not
// checked" rather than borrowing a verdict.
type stamp struct {
	at      time.Time
	explain string
}

// New builds a Resolver, refusing a configuration that cannot dial.
func New(o Options) (*Resolver, error) {
	r := &Resolver{
		transports: o.Transports,
		keys:       o.Keys,
		logger:     o.Logger,
		now:        o.Now,
		timeout:    o.Timeout,
		budget:     o.Budget,
		cap:        o.Cap,
		successTTL: o.SuccessTTL,
		failureTTL: o.FailureTTL,
		cacheMax:   o.CacheMax,
		entries:    map[string]entry{},
		stamps:     map[string]stamp{},
	}
	if r.transports == nil {
		r.transports = map[string]Transport{}
	}
	if r.logger == nil {
		r.logger = slog.New(slog.NewTextHandler(discard{}, &slog.HandlerOptions{Level: slog.LevelError + 1}))
	}
	if r.now == nil {
		r.now = time.Now
	}
	if r.timeout <= 0 {
		r.timeout = DefaultTimeout
	}
	if r.budget <= 0 {
		r.budget = DefaultBudget
	}
	if r.cap <= 0 {
		r.cap = DefaultCap
	}
	if r.successTTL <= 0 {
		r.successTTL = DefaultSuccessTTL
	}
	if r.failureTTL <= 0 {
		r.failureTTL = DefaultFailureTTL
	}
	if r.cacheMax <= 0 {
		r.cacheMax = DefaultCacheMax
	}

	// THE GUARD THAT MAKES THE ARITHMETIC SAFE, and it is here because the
	// alternative shipped silently in a draft of this plan.
	//
	// The budget asks whether a call can FINISH in it -- `elapsed + timeout >
	// budget` -- because gating the START would permit one more whole timeout
	// on top of the budget. Switchyard's copy works because AMBER_TIMEOUT_MS
	// defaults to 5s against an 8s budget. Hand it a 15s timeout against an 8s
	// budget and the FIRST reference fails the check: 0 + 15000 > 8000. Nothing
	// ever dials, every card answers without a call, and there is no error
	// anywhere and no request in any log to say why.
	if r.timeout >= r.budget {
		return nil, &ConfigError{Timeout: r.timeout, Budget: r.budget}
	}
	return r, nil
}

// ConfigError is a Resolver that could never have dialled.
type ConfigError struct {
	Timeout time.Duration
	Budget  time.Duration
}

func (e *ConfigError) Error() string {
	return "resolve: per-call timeout " + e.Timeout.String() +
		" must be strictly smaller than the batch budget " + e.Budget.String() +
		": the budget admits only calls that can finish inside it, so nothing would ever be dialled"
}

// discard is an io.Writer for the silent default logger.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// Resolve answers every reference, in order, and returns one Resolution per
// input.
//
// SERIAL WITHIN A SYSTEM, CONCURRENT ACROSS THE TWO. The "fifty sockets at an
// archive that is already slow" argument is about ONE upstream's health and is
// satisfied by not parallelising within a system. One serial pass over both
// would let Switchyard spend the whole window and leave a healthy archive's
// cards reading "too slow" on Switchyard's account.
//
// THE CAP COUNTS ATTEMPTS, NOT REFERENCES. A cache hit is not a round trip, so
// counting references would mean a sixty-reference page never resolving
// positions 51-60 on ANY render even when the first fifty were free. Counting
// attempts lets a long page complete across two renders inside the success TTL,
// which is what a reader expects from a refresh.
func (r *Resolver) Resolve(ctx context.Context, refs []markdown.Reference) []Resolution {
	out := make([]Resolution, len(refs))
	if len(refs) == 0 {
		return out
	}
	start := r.now()

	// Grouped by system so each upstream gets its own serial pass and its own
	// breaker, and so a Switchyard timeout can never stamp an Amber card.
	bySystem := map[string][]int{}
	for i, ref := range refs {
		out[i] = Resolution{Ref: ref}
		bySystem[ref.System] = append(bySystem[ref.System], i)
	}

	var attempts atomic.Int64
	var wg sync.WaitGroup
	for system, idxs := range bySystem {
		tr, ok := r.transports[system]
		if !ok {
			// Never dials, and says why in the state rather than in a comment.
			explain := "not checked: this Chronicle has no " + systemName(system) +
				" configured, so references to it cannot be resolved here at all"
			if system == markdown.SystemChronicle {
				// Not an outage and not a misconfiguration: CHR-#### and
				// DSC-#### resolve locally against tier2 number columns, with
				// no network and no staleness, and that is the caller's to do.
				explain = "not checked: Chronicle's own notes and discussions resolve locally, " +
					"and this resolver was given no local resolver for them"
			}
			for _, i := range idxs {
				out[i].State = StateUnconfigured
				out[i].Explain = explain
			}
			continue
		}
		wg.Add(1)
		go func(system string, tr Transport, idxs []int) {
			defer wg.Done()
			r.resolveSystem(ctx, system, tr, refs, idxs, out, start, &attempts)
		}(system, tr, idxs)
	}
	wg.Wait()

	r.logCounts(out)
	return out
}

// ResolveScan resolves a scan's references AND feeds its misses back to the key
// set, which is the pair a render always wants.
//
// It exists so the second half cannot be forgotten. markdown.Result carries
// UnknownKeys precisely because internal/markdown is a leaf with no logger --
// "the caller with a logger logs it" -- and a caller that took References and
// dropped UnknownKeys would leave a stale key set with no signal at all, since
// the rendered output of a miss is ordinary prose.
func (r *Resolver) ResolveScan(ctx context.Context, scan markdown.Result) []Resolution {
	if r.keys != nil {
		r.keys.NoteMisses(scan.UnknownKeys)
	}
	return r.Resolve(ctx, scan.References)
}

// resolveSystem is one upstream's serial pass.
func (r *Resolver) resolveSystem(
	ctx context.Context,
	system string,
	tr Transport,
	refs []markdown.Reference,
	idxs []int,
	out []Resolution,
	start time.Time,
	attempts *atomic.Int64,
) {
	name := systemName(system)
	for _, i := range idxs {
		ref := refs[i]

		// THE CACHE IS CONSULTED BEFORE EVERY GUARD, for Switchyard's stated
		// reason: "a cache hit is not a round trip, so it cannot contribute to
		// the timeout they prevent, and answering unreachable for a reference
		// resolved held twenty seconds ago would trade an answer this batch
		// already holds for a pessimistic one."
		if res, ok := r.cached(ref); ok {
			out[i] = res
			continue
		}

		// The stamp: this upstream already told us something that cannot be
		// different for the next reference.
		if explain, ok := r.stamped(system); ok {
			out[i] = r.unchecked(ref, explain)
			continue
		}

		// The budget: can a call FINISH inside what is left of the page's
		// wall-clock? Asking whether it can START would permit one more whole
		// timeout past the ceiling.
		if r.now().Sub(start)+r.timeout > r.budget {
			out[i] = r.unchecked(ref, "not checked: "+name+
				" was too slow to resolve this page in time")
			continue
		}

		// The cap, incremented immediately before the dial so that it counts
		// what it claims to count.
		if n := attempts.Add(1); int(n) > r.cap {
			attempts.Add(-1)
			out[i] = r.unchecked(ref, "not checked: this page has more references than one render checks")
			continue
		}

		out[i] = r.attempt(ctx, system, name, tr, ref)
	}
}

// attempt makes one call and records what it learned.
func (r *Resolver) attempt(ctx context.Context, system, name string, tr Transport, ref markdown.Reference) Resolution {
	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	at := r.now()
	ans := tr.Fetch(callCtx, ref)

	var c classified
	switch system {
	case markdown.SystemAmber:
		c = classifyAmber(ref.Token, ans, tr.URLFor)
	default:
		c = classifySwitchyard(ref.Token, ans, tr.URLFor)
	}

	switch {
	case c.trips:
		r.trip(system, name, c.explain)
	case c.state == StateResolved || c.state == StateBroken:
		// THE UPSTREAM ANSWERED ABOUT THE REFERENT, so it is up. A
		// non-tripping Unreachable -- a body that would not decode -- is not
		// evidence of recovery and must not clear the stamp.
		r.recovered(system, name)
	}
	return r.store(ref, c, at)
}

// unchecked builds an answer for a reference nobody asked about.
//
// LastResolvedAt still travels: what this process last knew does not stop being
// true because this render declined to ask again. FetchedAt stays zero, because
// nothing was attempted.
func (r *Resolver) unchecked(ref markdown.Reference, explain string) Resolution {
	res := Resolution{Ref: ref, State: StateUnchecked, Explain: explain}
	r.mu.Lock()
	if e, ok := r.entries[cacheKey(ref)]; ok {
		res.LastResolvedAt = e.lastOK
	}
	r.mu.Unlock()
	return res
}

func cacheKey(ref markdown.Reference) string { return ref.System + "\n" + ref.Token }

// cached returns a still-valid answer, and whether there was one.
func (r *Resolver) cached(ref markdown.Reference) (Resolution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[cacheKey(ref)]
	if !ok {
		return Resolution{}, false
	}
	// An answer about the referent is worth the success window; "we could not
	// ask" is worth the shorter one, so recovery is noticed quickly.
	ttl := r.successTTL
	if e.state == StateUnreachable {
		ttl = r.failureTTL
	}
	if r.now().Sub(e.at) >= ttl {
		return Resolution{}, false
	}
	return Resolution{
		Ref: ref, State: e.state, Upstream: e.upstream.clone(),
		FetchedAt: e.at, LastResolvedAt: e.lastOK, Explain: e.explain,
	}, true
}

// store writes what an attempt learned and returns it.
//
// ONLY THE OUTCOME OF A REAL ATTEMPT IS EVER WRITTEN. Unchecked never reaches
// here, because nothing was learned: writing it at the failure window would let
// one slow call condemn twenty-nine references on a measurement never made.
func (r *Resolver) store(ref markdown.Reference, c classified, at time.Time) Resolution {
	key := cacheKey(ref)

	r.mu.Lock()
	defer r.mu.Unlock()

	lastOK := r.entries[key].lastOK
	if c.state == StateResolved {
		t := at
		lastOK = &t
	}

	// Cleared wholesale rather than evicted one by one -- CITE_CACHE_MAX's
	// method as well as its number. The key space is unbounded (every token
	// ever written in a note) and the map lives for the process.
	if len(r.entries) >= r.cacheMax {
		r.entries = map[string]entry{}
	}
	r.entries[key] = entry{
		state: c.state, upstream: c.upstream, explain: c.explain, at: at, lastOK: lastOK,
	}

	return Resolution{
		Ref: ref, State: c.state, Upstream: c.upstream.clone(),
		FetchedAt: at, LastResolvedAt: lastOK, Explain: c.explain,
	}
}

// stamped reports whether this upstream is inside its failure window.
func (r *Resolver) stamped(system string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stamps[system]
	if !ok || r.now().Sub(s.at) >= r.failureTTL {
		return "", false
	}
	return s.explain, true
}

// trip records a claim about the service, and logs the transition ONCE.
//
// Once per transition rather than once per reference: a breaker firing is one
// event about a service, and thirty lines about tokens is a flood arriving at
// exactly the moment somebody is reading the log.
func (r *Resolver) trip(system, name, explain string) {
	now := r.now()

	r.mu.Lock()
	s, had := r.stamps[system]
	fresh := had && now.Sub(s.at) < r.failureTTL
	r.stamps[system] = stamp{at: now, explain: "not checked: " + name + " did not answer earlier"}
	r.mu.Unlock()

	if !fresh {
		// The instant belongs HERE, not in Explain: a relative age in a payload
		// is false as soon as the payload is held.
		r.logger.Warn("an upstream stopped answering; references to it will not be checked for now",
			"system", system, "reason", explain, "at", now.Format(time.RFC3339),
			"window", r.failureTTL.String())
	}
}

// recovered clears a system's failure stamp when a call succeeds, and logs the
// end of the outage.
//
// An outage with no recorded end is an outage somebody is still investigating.
func (r *Resolver) recovered(system, name string) {
	r.mu.Lock()
	_, had := r.stamps[system]
	delete(r.stamps, system)
	r.mu.Unlock()
	if had {
		r.logger.Warn("an upstream is answering again", "system", system, "name", name,
			"at", r.now().Format(time.RFC3339))
	}
}

// logCounts reports one line per system per render.
//
// PER SYSTEM, because a single line mixing both hides exactly what the
// per-system stamp exists to separate. The unchecked count is the one to watch:
// non-zero against a healthy upstream means the budget or the cap is binding,
// which is a number to revisit rather than a card to restyle.
func (r *Resolver) logCounts(out []Resolution) {
	if len(out) == 0 {
		return
	}
	type tally struct{ resolved, broken, unreachable, unconfigured, unchecked int }
	byState := map[string]*tally{}
	for _, res := range out {
		t := byState[res.Ref.System]
		if t == nil {
			t = &tally{}
			byState[res.Ref.System] = t
		}
		switch res.State {
		case StateResolved:
			t.resolved++
		case StateBroken:
			t.broken++
		case StateUnreachable:
			t.unreachable++
		case StateUnconfigured:
			t.unconfigured++
		case StateUnchecked:
			t.unchecked++
		}
	}
	for system, t := range byState {
		r.logger.Debug("resolved references",
			"system", system, "resolved", t.resolved, "broken", t.broken,
			"unreachable", t.unreachable, "unconfigured", t.unconfigured,
			"unchecked", t.unchecked)
	}
}

// systemName is how a system is named to a reader, in a sentence.
func systemName(system string) string {
	switch system {
	case markdown.SystemSwitchyard:
		return "the ticket tracker"
	case markdown.SystemAmber:
		return "the capture archive"
	case markdown.SystemChronicle:
		return "Chronicle"
	default:
		return system
	}
}
