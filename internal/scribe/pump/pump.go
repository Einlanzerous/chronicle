// Package pump is the step E4 never built: the loop that asks the Scribe for a
// proposal on every `transcribed` memo and writes the answer on the tier-1
// pool (CHRN-126).
//
// Every piece around that call already existed — the prompt, the contract, the
// live catalogue, the batch triage API — and production had 35 memos and zero
// proposals, because nothing called the router outside `chronicle eval`. This
// package is that caller, and it is deliberately the ONLY one: one loop in
// `serve`, one generation in flight, a sweep rather than a hook (⚖0), because a
// sweep is the only thing that backfills a corpus and heals a crash between a
// transcript and its proposal.
//
// The plan is CHRN-126's, approved 2026-10-03 (rev 2). Its seven rulings are
// cited by number where the code carries them.
//
// TIER 1 ONLY. The pump reads tier 2 (memos, transcripts) by 0007's grant and
// writes tier1.memo_proposals and nothing else. It never moves a memo's state:
// `transcribed>triaged` remains a person's accept through internal/triage. Its
// Store is satisfied by *store.Tier1Store, which has no method that writes tier
// 2, so the pump cannot be handed one.
package pump

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/scribe/prompt"
	"github.com/Einlanzerous/chronicle/internal/scribe/router"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// The pump's numbers. CONSTANTS AND NOT CONFIGURATION, for the reason
// internal/transcribe gives about its own: internal/scribe/ is in
// `sensitive_paths`, so a constant here is reviewed at the expensive tier and an
// environment variable is reviewed by nobody.
const (
	// DefaultInterval is how often an idle pump looks for work (⚖0). A hook
	// would save at most this on a step that then takes tens of seconds of
	// generation, against a requirement of "within minutes".
	DefaultInterval = 30 * time.Second

	// PassLimit caps one pass, and with it how old a pass's catalogue snapshot
	// can get inside a long backfill. A pass that routes all of them starts the
	// next immediately, with a fresh snapshot, rather than waiting out the
	// interval.
	PassLimit = 10

	// PollInterval is how often transcription is re-checked while a generation
	// is in flight, and while the pump waits for transcription to finish (⚖2).
	PollInterval = 2 * time.Second

	// InFlightBound is how long a submitted, unsettled transcription job keeps
	// routing off the GPU. Past the longest inference asrd allows (CHRN-26 §7
	// sizes its deadline for a forty-minute memo); without a bound an asrd
	// outage would stop routing for as long as it lasted.
	InFlightBound = 60 * time.Minute

	// MemoBackoff is how long a memo that failed at the transport is set aside
	// (⚖3). As long as the pump-level cap, so a memo that always times out stays
	// aside while the passes behind it run.
	MemoBackoff = 15 * time.Minute

	// OutageBackoff and OutageBackoffMax bound the wait after an outage: 30 s,
	// doubling, reset by the first success.
	OutageBackoff    = 30 * time.Second
	OutageBackoffMax = 15 * time.Minute

	// LongYield is how long the pump yields to transcription before it says so,
	// so a stuck job reads as a stuck job and not as a Scribe that stopped.
	LongYield = 10 * time.Minute

	// remainingCap bounds the count on the per-pass log line. It is a count for
	// a person, not a number anything acts on.
	remainingCap = 1000
)

// Store is what the pump reads and writes, and it is *store.Tier1Store.
type Store interface {
	UnroutedMemos(ctx context.Context, proposer string, exclude []uuid.UUID, limit int) ([]store.Memo, error)
	TranscriptionBusy(ctx context.Context, inFlightWithin time.Duration) (store.TranscriptionLoad, error)
	MemoState(ctx context.Context, memoID uuid.UUID) (string, error)
	TranscriptForScribe(ctx context.Context, memoID uuid.UUID) (store.Transcript, error)
	SaveProposal(ctx context.Context, memoID, transcriptID uuid.UUID, proposer string, out scribe.Outcome) (store.Proposal, error)
}

// Catalogue fetches one live snapshot. catalogue.Live satisfies it.
type Catalogue interface {
	Fetch(ctx context.Context) (*catalogue.Snapshot, error)
}

// Options constructs a Pump.
type Options struct {
	Store     Store
	Catalogue Catalogue
	Logger    *slog.Logger

	// OllamaURL and Model are CHRONICLE_SCRIBE_OLLAMA_URL and _MODEL. Together
	// with the prompt version they are the proposer, which must be the one
	// internal/triage reads by — both derive it from scribe.Proposer.
	OllamaURL   string
	Model       string
	MaxAttempts int

	// TranscriptionGate is on exactly when serve built a transcription pump
	// (⚖2). With no pump, a `captured` memo is never submitted and would hold
	// routing off forever.
	TranscriptionGate bool

	// Timeout is the router's per-call budget. Zero is router.DefaultTimeout;
	// tests shorten it.
	Timeout time.Duration
}

// Pump routes transcribed memos, one at a time.
type Pump struct {
	store       Store
	cat         Catalogue
	logger      *slog.Logger
	ollamaURL   string
	model       string
	maxAttempts int
	gate        bool
	timeout     time.Duration
	proposer    string

	// Tunables, at their constants in production and shortened by tests.
	interval      time.Duration
	poll          time.Duration
	memoBackoff   time.Duration
	outageBase    time.Duration
	outageMax     time.Duration
	longYield     time.Duration
	now           func() time.Time
	onRouteCalled func(memoID uuid.UUID) // test hook: a Route call is about to start

	// State that lives for the process and dies with it. A restart forgets the
	// backoffs and tries again, which is one extra attempt per deploy for a memo
	// that keeps timing out — stated in the plan's trade-offs.
	backoff     map[uuid.UUID]time.Time
	outageWait  time.Duration
	outageNamed bool // the outage WARN has been said and no success has followed
	digestRead  bool
	yieldSince  time.Time
	yieldWarned bool
}

// New validates what can be validated without a network.
func New(o Options) (*Pump, error) {
	if o.Store == nil {
		return nil, errors.New("pump: no store")
	}
	if o.Catalogue == nil {
		return nil, errors.New("pump: no catalogue")
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.Default()
	}
	proposer, err := scribe.Proposer("ollama", o.Model, prompt.Version)
	if err != nil {
		return nil, fmt.Errorf("pump: %w", err)
	}
	// The router is rebuilt every pass over that pass's snapshot. Building one
	// here, over an empty one, is how a bad URL is a boot error rather than
	// something found on the first memo — and how the proposer the router will
	// write under is checked against the one the pump selects by.
	r, err := router.New(router.Options{BaseURL: o.OllamaURL, Model: o.Model,
		Catalogue: &catalogue.Snapshot{}, MaxAttempts: o.MaxAttempts, Timeout: o.Timeout})
	if err != nil {
		return nil, fmt.Errorf("pump: %w", err)
	}
	if r.Proposer() != proposer {
		return nil, fmt.Errorf("pump: the router would write under %q and the pump selects by %q", r.Proposer(), proposer)
	}
	return &Pump{
		store: o.Store, cat: o.Catalogue, logger: logger,
		ollamaURL: o.OllamaURL, model: o.Model, maxAttempts: o.MaxAttempts,
		gate: o.TranscriptionGate, timeout: o.Timeout, proposer: proposer,
		interval: DefaultInterval, poll: PollInterval, memoBackoff: MemoBackoff,
		outageBase: OutageBackoff, outageMax: OutageBackoffMax, longYield: LongYield,
		now:        time.Now,
		backoff:    map[uuid.UUID]time.Time{},
		outageWait: OutageBackoff,
	}, nil
}

// Proposer is the identity every row this pump writes carries.
func (p *Pump) Proposer() string { return p.proposer }

// Run routes until ctx is cancelled.
//
// SHUTDOWN STAYS INSIDE CHRONICLE_SHUTDOWN_GRACE even though one generation may
// take up to router.DefaultTimeout: the in-flight request is cancelled through
// its context, nothing is written for that memo, and the next boot routes it.
func (p *Pump) Run(ctx context.Context) error {
	p.logger.Info("scribe pump started", "proposer", p.proposer,
		"interval", p.interval.String(), "transcription_gate", p.gate)

	for {
		rep := p.Tick(ctx)
		if ctx.Err() != nil {
			p.logger.Info("scribe pump stopped")
			return nil
		}

		var wait time.Duration
		switch {
		case rep.Yielded:
			// Wait for transcription to finish, then start a NEW pass — never
			// resume the old list. That is what makes newest-first hold during
			// a backfill: a memo recorded mid-pass made transcription busy on
			// its way in, so by the time routing resumes it is the newest
			// unrouted memo and comes first.
			if !p.awaitIdle(ctx) {
				p.logger.Info("scribe pump stopped")
				return nil
			}
			continue
		case rep.Outage:
			wait = p.outageWait
			p.outageWait = min(2*p.outageWait, p.outageMax)
		case rep.Full:
			continue
		default:
			wait = p.interval
		}

		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			p.logger.Info("scribe pump stopped")
			return nil
		case <-t.C:
		}
	}
}

// awaitIdle polls until transcription has nothing in flight. It reports false
// when ctx ended first.
func (p *Pump) awaitIdle(ctx context.Context) bool {
	t := time.NewTicker(p.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-t.C:
		}
		load, err := p.store.TranscriptionBusy(ctx, InFlightBound)
		if err != nil {
			if ctx.Err() != nil {
				return false
			}
			// A database that cannot answer is not transcription finishing.
			// Keep waiting; the next poll asks again.
			p.logger.Error("scribe pump: could not read whether transcription is busy", "error", err)
			continue
		}
		if !load.Busy() {
			p.endYield()
			return true
		}
		p.noteYield(load)
	}
}

// noteYield records that the pump is yielding, and says so once when it has
// been yielding continuously for LongYield.
func (p *Pump) noteYield(load store.TranscriptionLoad) {
	now := p.now()
	if p.yieldSince.IsZero() {
		p.yieldSince = now
		return
	}
	if !p.yieldWarned && now.Sub(p.yieldSince) >= p.longYield {
		p.yieldWarned = true
		p.logger.Warn("scribe pump has yielded to transcription continuously; routing is waiting, not stopped",
			"for", now.Sub(p.yieldSince).Round(time.Second).String(),
			"captured_memos", load.Captured, "in_flight_jobs", load.InFlight,
			"visible_at", "GET /admin/transcription")
	}
}

func (p *Pump) endYield() {
	if p.yieldWarned {
		p.logger.Info("scribe pump resumed: transcription is idle",
			"yielded_for", p.now().Sub(p.yieldSince).Round(time.Second).String())
	}
	p.yieldSince, p.yieldWarned = time.Time{}, false
}

// Report is what one pass did.
type Report struct {
	Routed    map[scribe.Status]int
	Preempted int
	// Failed counts outage-class failures: transport errors that wrote nothing.
	Failed int
	// Dropped counts results thrown away because the memo was decided while
	// its generation ran.
	Dropped int

	// Yielded: the pass found transcription busy and ended.
	Yielded bool
	// Outage: two failures back to back, or a catalogue that could not be read.
	Outage bool
	// Full: the pass worked through a whole PassLimit without ending early, so
	// there may be more and the next pass starts now.
	Full bool

	successes int
}

func (r Report) did() bool {
	n := r.Preempted + r.Failed + r.Dropped
	for _, c := range r.Routed {
		n += c
	}
	return n > 0
}

// Tick runs one pass. Exported so tests drive it without a ticker.
func (p *Pump) Tick(ctx context.Context) Report {
	rep := Report{Routed: map[scribe.Status]int{}}

	if p.busy(ctx, &rep) {
		return rep
	}
	p.endYield()

	memos, err := p.store.UnroutedMemos(ctx, p.proposer, p.inBackoff(), PassLimit)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.Error("scribe pump: could not list unrouted memos", "error", err)
		}
		return rep
	}
	if len(memos) == 0 {
		return rep
	}

	// ONE SNAPSHOT PER PASS, rendered into the prompt and used by
	// scribe.Reconcile, as router.Catalogue requires. A failed fetch is an
	// outage by itself: no memo is attempted, nothing is written.
	snap, err := p.cat.Fetch(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return rep
		}
		rep.Outage = true
		p.sayOutage(ctx, "switchyard", err)
		return rep
	}
	r, err := router.New(router.Options{BaseURL: p.ollamaURL, Model: p.model,
		Catalogue: snap, MaxAttempts: p.maxAttempts, Timeout: p.timeout})
	if err != nil {
		// New validated this configuration at boot; reaching here is a bug.
		p.logger.Error("scribe pump: could not build the router", "error", err)
		return rep
	}

	consecutive := 0
	var lastErr error
	attempted := 0
	for _, m := range memos {
		if p.busy(ctx, &rep) {
			break
		}
		attempted++
		res := p.routeOne(ctx, r, m.ID)
		if ctx.Err() != nil {
			// Shutdown. Nothing was written for this memo, and the next boot
			// routes it; not a failure.
			return rep
		}
		switch res.kind {
		case preempted:
			rep.Preempted++
			rep.Yielded = true
		case failed:
			rep.Failed++
			lastErr = res.err
			p.backoff[m.ID] = p.now().Add(p.memoBackoff)
			consecutive++
			// TWO IN A ROW IS THE UPSTREAM'S. One memo failing between
			// successes is that memo's problem.
			if consecutive >= 2 {
				rep.Outage = true
			}
		case dropped:
			rep.Dropped++
		case skipped:
		case routed:
			consecutive = 0
			rep.successes++
			rep.Routed[res.status]++
			p.readDigest(ctx, r)
		case broken:
			// A database error: logged where it happened. End the pass; the
			// next one asks again.
			return rep
		}
		if rep.Yielded || rep.Outage {
			break
		}
	}
	rep.Full = !rep.Yielded && !rep.Outage && attempted == PassLimit

	if rep.Failed > 0 && rep.successes == 0 {
		p.sayOutage(ctx, "ollama", lastErr)
	}
	if rep.successes > 0 {
		p.sayRecovered()
	}
	if rep.did() {
		p.logPass(ctx, rep)
	}
	return rep
}

// busy runs the transcription gate. It reports true when the pass must end.
func (p *Pump) busy(ctx context.Context, rep *Report) bool {
	if !p.gate {
		return false
	}
	load, err := p.store.TranscriptionBusy(ctx, InFlightBound)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.Error("scribe pump: could not read whether transcription is busy", "error", err)
		}
		// Not known to be idle, so not routing. Not a yield either: the pump
		// waits out its interval and asks again.
		return true
	}
	if load.Busy() {
		rep.Yielded = true
		p.noteYield(load)
		return true
	}
	return false
}

func (p *Pump) inBackoff() []uuid.UUID {
	now := p.now()
	out := make([]uuid.UUID, 0, len(p.backoff))
	for id, until := range p.backoff {
		if now.Before(until) {
			out = append(out, id)
		} else {
			delete(p.backoff, id)
		}
	}
	return out
}

type resultKind int

const (
	routed    resultKind = iota // a row was written
	failed                      // outage-class: nothing written, retried after backoff
	preempted                   // cancelled for transcription: nothing written, not a failure
	dropped                     // the memo was decided mid-generation: nothing written
	skipped                     // no durable transcript after all: nothing to route from
	broken                      // a database error
)

type result struct {
	kind   resultKind
	status scribe.Status
	err    error
}

// routeOne asks for one memo's proposal and writes it.
func (p *Pump) routeOne(ctx context.Context, r *router.Router, memoID uuid.UUID) result {
	tr, err := p.store.TranscriptForScribe(ctx, memoID)
	if errors.Is(err, store.ErrNotFound) {
		// UnroutedMemos checked the same floor a moment ago; something moved.
		return result{kind: skipped}
	}
	if err != nil {
		if ctx.Err() == nil {
			p.logger.Error("scribe pump: could not read the transcript", "memo_id", memoID, "error", err)
		}
		return result{kind: broken}
	}

	// PREEMPTION (⚖2). While the generation runs, poll the gate and cancel the
	// request the moment transcription has work. Whether a cancelled call was a
	// failure is decided from THIS flag and the parent context, never from the
	// error: http.Client.Timeout produces an error that also satisfies
	// context.DeadlineExceeded, so the error alone cannot tell a timeout (a
	// failure) from a preemption (not one).
	genCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wasPreempted atomic.Bool
	watching := make(chan struct{})
	stopWatch := make(chan struct{})
	if p.gate {
		go func() {
			defer close(watching)
			t := time.NewTicker(p.poll)
			defer t.Stop()
			for {
				select {
				case <-stopWatch:
					return
				case <-genCtx.Done():
					return
				case <-t.C:
				}
				load, err := p.store.TranscriptionBusy(ctx, InFlightBound)
				if err == nil && load.Busy() {
					wasPreempted.Store(true)
					cancel()
					return
				}
			}
		}()
	} else {
		close(watching)
	}

	if p.onRouteCalled != nil {
		p.onRouteCalled(memoID)
	}
	out, routeErr := r.Route(genCtx, tr.Text)
	close(stopWatch)
	<-watching

	if ctx.Err() != nil {
		return result{kind: skipped}
	}
	if wasPreempted.Load() {
		return result{kind: preempted}
	}

	// ⚖3. A transport error writes nothing — EXCEPT the two truncations, which
	// are facts about this transcript against the pinned options and would be
	// the same answer forever. Those are recorded `invalid`, which is what
	// `invalid` means: no run has produced a valid proposal for this memo
	// under this proposer.
	if routeErr != nil && !errors.Is(routeErr, router.ErrContextFull) && !errors.Is(routeErr, router.ErrAnswerTruncated) {
		return result{kind: failed, err: routeErr}
	}

	// THE LAST LOOK. A person can decide an `absent` card by hand while its
	// generation runs; a row for a decided memo is one nothing reads. This
	// narrows that window and does not close it, and does not need to: a row
	// that lands first makes the accept come back `stale`, which is the
	// generation echo doing its job.
	state, err := p.store.MemoState(ctx, memoID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && state != store.StateTranscribed) {
		return result{kind: dropped}
	}
	if err != nil {
		if ctx.Err() == nil {
			p.logger.Error("scribe pump: could not re-read the memo", "memo_id", memoID, "error", err)
		}
		return result{kind: broken}
	}

	row, err := p.store.SaveProposal(ctx, memoID, tr.ID, p.proposer, out)
	if err != nil {
		if ctx.Err() == nil {
			p.logger.Error("scribe pump: could not save the proposal", "memo_id", memoID, "error", err)
		}
		return result{kind: broken}
	}
	return result{kind: routed, status: row.Status}
}

// readDigest logs the model's content hash once per process, after the first
// generation that worked. The tag is mutable and the digest is deliberately not
// part of the proposer, so this line is where a re-pull shows. A failed read is
// logged and gates nothing.
func (p *Pump) readDigest(ctx context.Context, r *router.Router) {
	if p.digestRead {
		return
	}
	p.digestRead = true
	d, err := r.Digest(ctx)
	if err != nil {
		p.logger.Warn("scribe pump: could not read the model digest", "model", p.model, "error", err)
		return
	}
	p.logger.Info("scribe model", "model", p.model, "digest", d, "proposer", p.proposer)
}

// sayOutage warns once per outage, naming the upstream. Not repeated until a
// success has intervened: one line per outage, not one per memo per retry.
func (p *Pump) sayOutage(ctx context.Context, upstream string, err error) {
	if p.outageNamed {
		return
	}
	p.outageNamed = true
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	p.logger.Warn("scribe pump: "+upstream+" is failing; memos are waiting, not invalid",
		"upstream", upstream, "error", msg, "waiting", p.remaining(ctx),
		"visible_at", "GET /triage/batch (status absent)")
}

func (p *Pump) sayRecovered() {
	p.outageWait = p.outageBase
	if !p.outageNamed {
		return
	}
	p.outageNamed = false
	p.logger.Info("scribe pump: routing again")
}

// logPass is one INFO line for a pass that did something. Memo ids, statuses
// and counts only — never transcript text or model output (REVIEW.md §8).
func (p *Pump) logPass(ctx context.Context, rep Report) {
	statuses := make([]string, 0, len(rep.Routed))
	for s := range rep.Routed {
		statuses = append(statuses, string(s))
	}
	sort.Strings(statuses)
	attrs := []any{"proposer", p.proposer}
	for _, s := range statuses {
		attrs = append(attrs, s, rep.Routed[scribe.Status(s)])
	}
	attrs = append(attrs, "failed", rep.Failed, "preempted", rep.Preempted, "dropped", rep.Dropped,
		"remaining", p.remaining(ctx))
	p.logger.Info("scribe pump pass", attrs...)
}

// remaining counts memos still owed a proposal, backoffs included: they are
// waiting too.
func (p *Pump) remaining(ctx context.Context) int {
	ms, err := p.store.UnroutedMemos(ctx, p.proposer, nil, remainingCap)
	if err != nil {
		return -1
	}
	return len(ms)
}
