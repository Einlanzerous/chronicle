// Package triage applies a batch of routing decisions, and sweeps up what a
// batch that died halfway left behind.
//
// CHRN-33, and this is the ticket where DERIVED STATE BECOMES AUTHORED STATE.
// Everything upstream — the transcript's routing, the proposal, its confidence
// — is tier 1 and regenerable. An accept writes a decision that nothing can
// reconstruct, and creates a ticket in another service that nothing here can
// un-create. So the interesting behaviour is not the happy path; it is what
// item 7 of 12 failing does to the other eleven.
//
// THREE RULES CARRY THE DESIGN, and each of them exists because the obvious
// alternative is broken rather than merely worse:
//
//  1. THE BATCH IS NOT A TRANSACTION. Each item is its own unit. One failure
//     must not roll back eleven decisions a person made deliberately, and a
//     batch-wide error status gives a client nothing to re-show.
//
//  2. THE PENDING LINK ROW IS THE LOCK, AND IT IS TAKEN BEFORE THE OUTWARD
//     CALL. Switchyard's Idempotency-Key header replays a RESPONSE; it does not
//     serialise a SIDE EFFECT. Two overlapping requests for one memo both reach
//     the create handler and two tickets exist. Only a `UNIQUE (memo_id)` row
//     on this side prevents it. See migration 0008.
//
//  3. AN ITEM RUNS TO COMPLETION ONCE IT HAS STARTED. The decision became
//     durable at T1; finishing it must not depend on the phone staying
//     connected. Items are run on a detached context, and "the rest were not
//     attempted" stays true by checking the request between items rather than
//     inside one.
//
// The plan is CHRN-33 revision 4, approved 2026-09-03.
package triage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// Per-item outcome. FIVE, AND THEY ARE NOT THREE: the difference between
// `refused` and `failed` is the difference between a client that must stop and
// a client that must retry, and collapsing them produces either an infinite
// retry loop or a lost decision.
const (
	// StatusApplied — it landed. Carries the ticket key for a TICKET.
	StatusApplied = "applied"

	// StatusNeedsInput — stage 2 cleared a target that no longer resolves.
	// Carries what was cleared AND the post-bump generation, so the operator's
	// completed resend is not `stale` without an intervening GET.
	StatusNeedsInput = "needs_input"

	// StatusStale — the payload moved under the client. Carries the server's
	// generation. Re-show this item; do not decide it blind.
	StatusStale = "stale"

	// StatusRefused — it will refuse identically on every replay. The reason
	// names the rule, the ticket or the status. DO NOT RETRY.
	StatusRefused = "refused"

	// StatusFailed — transient. Retry.
	StatusFailed = "failed"
)

// DefaultLimit is how many memos one screen and one batch hold.
//
// The POST is capped at the GET's limit and the number is here once, because
// they are the same number for a reason: the batch is the screen, and a client
// that could POST more items than it could GET would be composing decisions
// about memos it never displayed.
const DefaultLimit = 25

// MaxLimit bounds both. Each item may make one outward call bounded at
// switchyard.DefaultTimeout, so this is also the worst-case wall clock of one
// request — twenty-five is a little over six minutes if every single call times
// out, and a screen holding more than twenty-five cards is not an evening pass.
const MaxLimit = 25

// itemBudget bounds one detached item: a lock wait, one outward call, and the
// database work either side. Deliberately longer than switchyard.DefaultTimeout
// rather than equal to it — an item whose create succeeded at 14.9 seconds must
// still have time to confirm the row, or the ticket exists and nothing points
// at it until the sweep runs.
const itemBudget = switchyard.DefaultTimeout + 15*time.Second

// Store is the tier-2 surface: the corpus, and the decisions written about it.
type Store interface {
	GetMemo(ctx context.Context, id uuid.UUID) (store.Memo, error)
	UntriagedMemos(ctx context.Context, authorID uuid.UUID, limit int) ([]store.UntriagedMemo, error)
	MemoLinkFor(ctx context.Context, memoID uuid.UUID) (store.MemoLink, error)
	ClaimMemoLink(ctx context.Context, d store.Decision) (store.MemoLink, store.LinkClaim, error)
	ResolveMemoLink(ctx context.Context, memoID uuid.UUID, claim store.LinkClaim,
		decide func(context.Context, store.LinkAttempt) (store.LinkResolution, error)) (store.MemoLink, error)
	SweepMemoLink(ctx context.Context, skip []uuid.UUID,
		decide func(context.Context, store.LinkAttempt) (store.LinkResolution, error)) (store.MemoLink, error)
	LinksInFlight(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	TriageLinkStates(ctx context.Context, limit int) (store.TriageLinks, error)
	TriageBacklog(ctx context.Context) (store.TriageBacklog, error)
}

// Tier1 is the DERIVED surface, and it is a separate interface for the reason
// store.Tier1Store is a separate type: there is no method here that writes tier
// 2, so no amount of wiring can make this half author a decision.
type Tier1 interface {
	ProposalsForMemos(ctx context.Context, memoIDs []uuid.UUID, proposer string) (map[uuid.UUID]store.Proposal, error)
	GetProposal(ctx context.Context, memoID uuid.UUID, proposer string) (store.Proposal, error)
	BumpProposalGeneration(ctx context.Context, memoID uuid.UUID, proposer string,
		p *scribe.Proposal, cleared []scribe.ClearedField, status scribe.Status) (store.Proposal, error)
}

// Tickets is Switchyard, narrowed to what triage does: create one, and find the
// ones already created for a memo. Nothing that returns a title or a status,
// because there is no such method to narrow to.
type Tickets interface {
	CreateTicket(ctx context.Context, in switchyard.NewTicket) (switchyard.Ticket, error)
	TicketsByMemo(ctx context.Context, memoID uuid.UUID) ([]switchyard.Ticket, error)

	// TicketURL resolves a stored key into a deep link. It is on this interface
	// rather than a string on Options because the alternative is a second
	// spelling of Switchyard's routing living in this package, and the whole
	// argument for one client was that two spellings become two contracts.
	TicketURL(key string) string
}

// Catalogue supplies the live destination list. Fetched ONCE PER BATCH and
// never held between batches — CHRN-31's rule, and the reason is that a stale
// catalogue produces the worst kind of error, where the project it named does
// exist and is merely the wrong one.
type Catalogue interface {
	Fetch(ctx context.Context) (*catalogue.Snapshot, error)
}

// Options configures a Service.
type Options struct {
	Store     Store
	Tier1     Tier1
	Tickets   Tickets
	Catalogue Catalogue
	Logger    *slog.Logger

	// Proposer is the runner/model@prompt triple whose proposals this surface
	// reads. One proposer, because the identity of a proposal is
	// (memo_id, proposer) and a screen fetching across proposers would show one
	// memo twice.
	Proposer string

	// PreacceptMin is CHRONICLE_SCRIBE_PREACCEPT_MIN, and this package is THE
	// ONLY PLACE THIS API READS IT. It decides which cards a client may
	// pre-select for ACCEPT ALL and decides nothing else — in particular it is
	// never consulted when applying a decision, because by then a person has
	// looked at the card.
	PreacceptMin float64
}

// Service applies decisions and sweeps what they leave behind.
type Service struct {
	store   Store
	tier1   Tier1
	tickets Tickets
	cat     Catalogue
	logger  *slog.Logger

	proposer     string
	preacceptMin float64

	// newKey mints one decision's idempotency key. A field so tests can make it
	// deterministic; never a derivation from the memo, for the reason
	// switchyard.NewTicket.IdempotencyKey gives at length.
	newKey func() string

	// beforeCreate and afterCreate are TEST SEAMS, and they exist because the
	// failure they simulate cannot be produced any other way.
	//
	// The recovery this package is built around is "the process died between
	// the outward call and the confirm". Killing a process mid-test leaves
	// nothing to assert against; these inject the failure at the two moments
	// that matter — before the call, where nothing was created, and after it,
	// where a ticket exists and the row does not know. Nil in production, and
	// unexported so there is no way to set one from outside this package.
	beforeCreate func(memoID uuid.UUID) error
	afterCreate  func(memoID uuid.UUID) error
}

// New validates the wiring and returns a Service.
func New(o Options) (*Service, error) {
	switch {
	case o.Store == nil:
		return nil, fmt.Errorf("triage: no store")
	case o.Tier1 == nil:
		return nil, fmt.Errorf("triage: no tier-1 store — proposals are read through it and never through Store")
	case o.Tickets == nil:
		return nil, fmt.Errorf("triage: no ticket client")
	case o.Catalogue == nil:
		return nil, fmt.Errorf("triage: no catalogue — stage 2 re-runs at acceptance and has nothing to check against")
	case strings.TrimSpace(o.Proposer) == "":
		return nil, fmt.Errorf("triage: no proposer")
	}
	logger := o.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Service{
		store: o.Store, tier1: o.Tier1, tickets: o.Tickets, cat: o.Catalogue,
		logger: logger, proposer: o.Proposer, preacceptMin: o.PreacceptMin,
		newKey: switchyard.NewIdempotencyKey,
	}, nil
}

// Proposer is the proposal identity this surface reads and writes against.
func (s *Service) Proposer() string { return s.proposer }

// ============================================================================
// The request
// ============================================================================

// Item is one decision. THERE IS NO VERB FIELD, and its absence is deliberate.
//
// An item with no Override accepts the proposal AS SHOWN; an item with one
// carries a decision the operator authored. Append-versus-supersede is CHRN-39's
// question and CHRN-32's contract cannot express it, so there is nothing here
// to carry it and nothing to mistakenly default. The decoder rejects unknown
// fields, so a client that invents one is told rather than silently ignored.
type Item struct {
	MemoID uuid.UUID `json:"memo_id"`

	// Proposer names which proposal this decision is about. Required on every
	// item, including overrides: identity is (memo_id, proposer), and an
	// override still echoes a generation that has to be checked against
	// something.
	Proposer string `json:"proposer"`

	// Generation is the payload version the client saw. NULLABLE, AND NULL IS
	// AN ASSERTION rather than a missing value: it means "I saw no proposal for
	// this memo". If a proposal has appeared since the GET, that assertion is
	// false and the item is `stale`.
	Generation *int `json:"generation"`

	// Override is the operator's own decision. Nil means accept as shown.
	Override *Override `json:"override"`
}

// Override is a decision a person authored, bounded by the same limits stage 1
// puts on a model's.
type Override struct {
	Destination string `json:"destination"`

	// TICKET only.
	ProjectKey  string `json:"project_key"`
	TicketType  string `json:"ticket_type"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Result is one item's outcome. Every batch answers 200 with one of these per
// item, in request order — a batch-wide status could not say which of twelve
// memos to re-show.
type Result struct {
	MemoID uuid.UUID `json:"memo_id"`
	Status string    `json:"status"`

	Destination string `json:"destination,omitempty"`
	TicketKey   string `json:"ticket_key,omitempty"`
	TicketURL   string `json:"ticket_url,omitempty"`

	// Generation is the server's, and it is present for exactly two statuses:
	// `stale`, where it is what the client should have echoed, and
	// `needs_input`, where it is the POST-BUMP value the operator's completed
	// resend must carry.
	Generation *int `json:"generation,omitempty"`

	// Cleared is what stage 2 removed at acceptance, with the reason.
	Cleared []scribe.ClearedField `json:"cleared,omitempty"`

	// Reason names the rule, ticket or status behind a refusal, or the error
	// behind a failure. It is the only part of a non-applied result a person
	// reads, so it is never a bare error code.
	Reason string `json:"reason,omitempty"`
}

func (r Result) applied() bool { return r.Status == StatusApplied }

// ============================================================================
// Apply
// ============================================================================

// Apply runs a batch of decisions and returns one result per item, IN REQUEST
// ORDER so a client can pair them positionally without matching on ids.
//
// It returns an error only for a request that could not be attempted at all —
// too many items. Everything else is a per-item status, including a catalogue
// that could not be fetched: a batch-wide 502 would make a client retry twelve
// decisions when eleven of them were fine.
func (s *Service) Apply(ctx context.Context, actor store.User, items []Item) ([]Result, error) {
	if len(items) > MaxLimit {
		return nil, fmt.Errorf("%w: a batch is at most %d items, got %d", ErrTooLarge, MaxLimit, len(items))
	}

	// WHAT AN EARLIER BATCH LEFT BEHIND IS RESOLVED FIRST. A transiently failed
	// item left a pending row, and T2 never creates for a row it does not own —
	// so without this, the replay of that batch would answer `failed` for the
	// same item forever. See sweepBeforeBatch.
	s.sweepBeforeBatch(ctx)

	// ONE CATALOGUE FOR THE WHOLE BATCH. The same snapshot every item is
	// reconciled against, so twelve memos accepted in one pass cannot disagree
	// about whether a project is live. Fetched lazily: a batch of DISCARD
	// overrides needs no catalogue and must not fail because Switchyard's
	// project list was briefly unreachable.
	cat := &lazyCatalogue{src: s.cat}

	results := make([]Result, len(items))
	counts := map[string]int{}
	attempted := 0

	for i, it := range items {
		// BETWEEN ITEMS, NOT INSIDE ONE. A client that hung up mid-batch stops
		// the batch here; the item already running finishes on its own context.
		if err := ctx.Err(); err != nil {
			s.logger.WarnContext(ctx, "triage batch abandoned by its client",
				"attempted", attempted, "unattempted", len(items)-attempted)
			results = results[:attempted]
			break
		}
		attempted++

		// DETACHED, AND THEN BOUNDED. The decision becomes durable at T1 and
		// finishing it must not depend on the phone staying connected — on the
		// request context a client dropping would cancel a create Switchyard
		// may already have performed, manufacturing a sweep case that need not
		// exist. The budget replaces the cancellation the item just gave up.
		itemCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), itemBudget)
		results[i] = s.applyOne(itemCtx, actor, it, cat)
		cancel()
		counts[results[i].Status]++
	}

	// ONE LINE PER BATCH, counted by status. All refusals is a client bug; all
	// failures is an outage; and neither is visible from a per-item log.
	//
	// NOTHING AUTHORED IS IN IT. No transcript, no note body, no ticket
	// description and NO TICKET TITLE — the title is the field most likely to
	// be logged out of habit, and it is authored-derived text about somebody's
	// unfinished thinking.
	s.logger.InfoContext(ctx, "triage batch applied",
		"items", len(items), "attempted", attempted,
		"applied", counts[StatusApplied], "needs_input", counts[StatusNeedsInput],
		"stale", counts[StatusStale], "refused", counts[StatusRefused],
		"failed", counts[StatusFailed])
	return results, nil
}

// ErrTooLarge is a batch bigger than one screen.
var ErrTooLarge = errors.New("triage: batch too large")

// applyOne is one decision, start to finish. It never returns an error: every
// outcome is a Result, because the caller's job is to report twelve of them and
// an error would have to be turned back into one anyway.
func (s *Service) applyOne(ctx context.Context, actor store.User, it Item, cat *lazyCatalogue) Result {
	res := Result{MemoID: it.MemoID, Status: StatusFailed}

	if strings.TrimSpace(it.Proposer) == "" {
		return refuse(res, "every item must name the proposer it is deciding about — a proposal's identity is (memo_id, proposer)")
	}

	// ---- 1 · The memo, and whether this actor may decide it. ----
	//
	// THE POST SCOPES INDEPENDENTLY OF THE GET. Hiding a memo from a list is
	// not access control: a client that names an id directly never went through
	// the list. A member reaching for another member's memo gets the same
	// answer as one reaching for an id that does not exist, on the pattern the
	// credential lookups already set — a probe must not be able to tell a memo
	// it may not see from one that was never there.
	memo, err := s.store.GetMemo(ctx, it.MemoID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return refuse(res, "no such memo, or it belongs to another author")
	case err != nil:
		return s.fail(ctx, res, "read memo", err)
	case !canDecide(actor, memo):
		return refuse(res, "no such memo, or it belongs to another author")
	}

	// ---- 2 · A decision already recorded is the answer, before anything else.
	//
	// Consulted FIRST and WITHOUT AN OUTWARD CALL, which is what makes a
	// replayed batch safe: an item that landed yesterday answers `applied` with
	// the key it landed as, whatever the proposal has done since. Checking the
	// generation first would report `stale` for a memo that is already decided,
	// which is true about the payload and useless about the memo.
	//
	// The authoritative version of this check is inside T2, under the row lock.
	// This one is an early-out, and it agrees with that one by construction.
	switch link, err := s.store.MemoLinkFor(ctx, it.MemoID); {
	case err == nil && link.Confirmed():
		return s.applyLink(res, link)
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return s.fail(ctx, res, "read memo link", err)
	}

	// ---- 3 · The generation echo. ----
	stored, err := s.tier1.GetProposal(ctx, it.MemoID, it.Proposer)
	switch {
	case errors.Is(err, store.ErrNotFound):
		// No proposal row. The client must have said so.
		if it.Generation != nil {
			return stale(res, nil)
		}
	case err != nil:
		return s.fail(ctx, res, "read proposal", err)
	case it.Generation == nil || *it.Generation != stored.Generation:
		// Includes the edge the plan names: an `invalid` row HAS a generation
		// (saveFailedProposal inserts 1), so it echoes like any other and needs
		// no special case here.
		g := stored.Generation
		return stale(res, &g)
	}
	hasProposal := err == nil

	// ---- 4 · What is actually being decided. ----
	// `res` and not a second variable: decisionFor fills in the destination,
	// and binding it elsewhere would leave every non-refused result unlabelled
	// — so a client could not name the card it has to re-show.
	decided, res, ok := s.decisionFor(res, it, stored, hasProposal)
	if !ok {
		return res
	}

	// ---- 5 · Stage 2, at acceptance, against the batch's one snapshot. ----
	//
	// Not belt-and-braces. A proposal made on Tuesday evening and accepted on
	// Thursday morning has had two days for a project to be archived, and
	// validating only at write time would create a ticket in a project that no
	// longer takes them. It runs for OVERRIDES TOO: an operator can name an
	// archived project as easily as a model can.
	if needsCatalogue(decided) {
		snap, err := cat.get(ctx)
		if err != nil {
			// PER ITEM, NOT BATCH-WIDE. A non-2xx here would make a client
			// retry a batch it cannot see into.
			return s.fail(ctx, res, "fetch catalogue", err)
		}
		cleared, status := scribe.Reconcile(decided, snap)
		if len(cleared) > 0 {
			res.Cleared = cleared
		}
		if status == scribe.StatusNeedsInput {
			return s.needsInput(ctx, res, it, decided, cleared, stored, hasProposal)
		}
	}

	// ---- 6 · T1: the pending row. THE LOCK. ----
	d := store.Decision{
		MemoID:         it.MemoID,
		Destination:    string(decided.Destination),
		Title:          decided.Title,
		Description:    decided.Description,
		IdempotencyKey: s.newKey(),
	}
	if decided.Destination == scribe.DestTicket {
		d.Type = decided.TicketType
		if decided.ProjectKey != nil {
			d.ProjectKey = *decided.ProjectKey
		}
	}
	link, claim, err := s.store.ClaimMemoLink(ctx, d)
	switch {
	case errors.Is(err, store.ErrNotFound):
		return refuse(res, "no such memo, or it belongs to another author")
	case errors.Is(err, store.ErrLinkLocked):
		// T1 conflicts on UNIQUE (memo_id), so it WAITS HERE when another
		// batch's T2 is mid-call on the same memo — one statement before the
		// lock this package takes itself. Same answer as the T2 waiter, and it
		// has to be the same: from a client's side these are one situation.
		return failed(res, "another decision for this memo is in flight; retry shortly")
	case errors.Is(err, store.ErrLinkKeyReused):
		return refuse(res, "this decision was already refused under its own key; change the decision "+
			"and it will be attempted afresh")
	case err != nil:
		return s.fail(ctx, res, "claim memo link", err)
	}

	// ---- 7 · T2: lock, call, confirm, advance — one transaction. ----
	return s.resolve(ctx, res, link.MemoID, claim)
}

// decisionFor produces the proposal that will actually be committed, and
// refuses the destinations that cannot be committed at all.
func (s *Service) decisionFor(res Result, it Item, stored store.Proposal, hasProposal bool) (*scribe.Proposal, Result, bool) {
	var decided *scribe.Proposal

	if it.Override != nil {
		p, err := overrideProposal(*it.Override)
		if err != nil {
			return nil, refuse(res, err.Error()), false
		}
		decided = p
	} else {
		switch {
		case !hasProposal:
			return nil, refuse(res, "there is no proposal to accept for this memo — decide it with an override"), false
		case stored.Payload == nil:
			// `invalid`: no run has ever produced a valid proposal here. The
			// recorded error is on the card; there is nothing to accept.
			return nil, refuse(res, "this memo has no valid proposal to accept — decide it with an override"), false
		case stored.Status != scribe.StatusValid:
			// `needs_input` is a proposal a person can finish in one tap, and
			// finishing it is an override. Accepting it AS SHOWN would commit
			// the blank the model deliberately left.
			return nil, refuse(res, "this proposal needs input before it can be accepted as shown — supply the missing field as an override"), false
		}
		// COPIED, NOT ALIASED. Reconcile mutates what it is given, and the
		// stored payload must not move because an accept was attempted.
		clone := *stored.Payload
		decided = &clone

		// DISCARD IS NEVER ACCEPTED AS SHOWN, at any confidence and whatever
		// the client asserts about its batch. `discarded` is terminal in
		// tier2.memos_guard, so it is the one decision that cannot be walked
		// back, and a confident wrong DISCARD is exactly the case that clears
		// any threshold. It costs a deliberate tap: an override.
		if decided.Destination == scribe.DestDiscard {
			return nil, refuse(res, "a DISCARD is never accepted as shown — discarding is terminal, so it takes a deliberate override"), false
		}
	}

	res.Destination = string(decided.Destination)

	// NOT YET LANDABLE. Both need E5's page tree (CHRN-37), and until it exists
	// there is nowhere for a note or a discussion to go. REFUSED and not
	// `failed`: a client that retried would get the same answer every evening
	// until that ticket ships.
	switch decided.Destination {
	case scribe.DestNote, scribe.DestDiscussion:
		return nil, refuse(res, fmt.Sprintf(
			"%s cannot land yet — it needs the page tree from CHRN-37", decided.Destination)), false
	}
	return decided, res, true
}

// overrideProposal turns an operator's decision into a proposal, bounded by the
// same limits stage 1 applies to a model's.
//
// A PERSON'S DECISION IS VALIDATED LIKE A MODEL'S, and the reason is not
// symmetry: `project_key` is immutable after ticket creation in Switchyard, so
// a typo an operator makes at nine in the evening is as permanent as one a
// model makes.
func overrideProposal(o Override) (*scribe.Proposal, error) {
	dest := scribe.Destination(strings.TrimSpace(o.Destination))
	switch dest {
	case scribe.DestNote, scribe.DestTicket, scribe.DestDiscussion, scribe.DestDiscard:
	default:
		return nil, fmt.Errorf("override destination must be one of %v, got %q "+
			"(HOLD is an operator action on the memo, not a destination)", scribe.Destinations, o.Destination)
	}

	p := &scribe.Proposal{
		Destination: dest,
		// An override IS the reason. There is no model claim to argue with, and
		// a required free-text justification for a decision a person made by
		// hand is a field they would type "x" into.
		Reason:      "decided by the operator",
		Confidence:  1,
		Title:       strings.TrimSpace(o.Title),
		Description: o.Description,
	}
	if dest != scribe.DestDiscard {
		switch {
		case p.Title == "":
			return nil, fmt.Errorf("an override to %s needs a title", dest)
		case len(p.Title) > scribe.MaxTitleLen:
			return nil, fmt.Errorf("title must be at most %d characters, got %d", scribe.MaxTitleLen, len(p.Title))
		}
	}
	if dest == scribe.DestTicket {
		key := strings.TrimSpace(o.ProjectKey)
		switch {
		case key == "":
			return nil, fmt.Errorf("an override to TICKET needs a project key — Switchyard cannot move a ticket between projects afterwards")
		case key != strings.ToUpper(key):
			return nil, fmt.Errorf("project_key must be uppercase, got %q", key)
		}
		p.ProjectKey = &key
		p.TicketType = strings.TrimSpace(o.TicketType)
		if !containsString(scribe.TicketTypes, p.TicketType) {
			return nil, fmt.Errorf("ticket_type must be one of %v, got %q", scribe.TicketTypes, o.TicketType)
		}
		if strings.TrimSpace(p.Description) == "" {
			return nil, fmt.Errorf("an override to TICKET needs a description")
		}
	}
	return p, nil
}

func containsString(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

// needsCatalogue reports whether stage 2 has anything to check for this
// proposal. A DISCARD carrying no advisory page has no catalogue reference at
// all, and must not fail because the project list was briefly unreachable.
func needsCatalogue(p *scribe.Proposal) bool {
	if p.NearestPage != nil && *p.NearestPage != "" {
		return true
	}
	switch p.Destination {
	case scribe.DestTicket, scribe.DestNote:
		return true
	}
	return false
}

// needsInput answers with the generation the operator's completed resend must
// echo, bumping the stored proposal FIRST IF AND ONLY IF STAGE 2 MUTATED IT.
//
// THE BUMP BELONGS TO THE PAYLOAD, NOT TO THE REQUEST, and that distinction is
// the whole of why the two paths differ here:
//
//   - ACCEPT AS SHOWN · `decided` is a clone of the stored payload that
//     Reconcile has just mutated, so the stored row genuinely moved and must
//     say so. Generation 1 becomes 2 and the clearing is recorded.
//   - OVERRIDE · stage 2 cleared a field of THE OPERATOR'S OWN proposal. The
//     stored row was never touched. Writing `decided` back would replace the
//     model's proposal with the person's typed text under the model's
//     proposer — so the triage screen, whose entire job is to keep those two
//     apart, would attribute the second to the first, and the model's proposal
//     could never be accepted as shown again. It would also count an
//     operator's typo as a hallucination in the rate CHRN-36 reports per
//     proposer. Nothing is bumped and the stored generation is echoed, which
//     is still not stale: step 3 compares against whatever is stored.
//
// A tier-2 accept path writing authored text into a tier-1 table is invariant 1
// in the direction the structural tests do not cover — they check that tier 1
// never writes tier 2 — so it is named here at length.
//
// THE BUMP IS A TIER-1 WRITE ON THE TIER-1 POOL, and it is the only one in the
// accept flow. It cannot share a transaction with anything tier 2 — different
// pools, different roles — and it does not need to: this path writes nothing
// tier 2 at all. Reconcile → bump → answer, or reconcile → tier-2 transaction.
// Never both.
func (s *Service) needsInput(ctx context.Context, res Result, it Item,
	decided *scribe.Proposal, cleared []scribe.ClearedField,
	stored store.Proposal, hasProposal bool) Result {
	res.Status = StatusNeedsInput
	res.Reason = "a target no longer resolves in the live catalogue; supply it and resend as an override"

	if !hasProposal {
		// An override against a memo Scribe never routed. There is no row to
		// bump and no generation to echo — `null` remains the truthful answer,
		// and the operator's corrected resend echoes null again.
		return res
	}
	if it.Override != nil {
		// The stored payload did not move, so neither does its generation.
		g := stored.Generation
		res.Generation = &g
		return res
	}
	p, err := s.tier1.BumpProposalGeneration(ctx, it.MemoID, it.Proposer, decided, cleared, scribe.StatusNeedsInput)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return res
		}
		// The clearing is real whether or not it was recorded, so the answer
		// stands; what is lost is the generation echo, and saying so beats
		// returning a stale one.
		s.logger.ErrorContext(ctx, "triage could not record a stage-2 clearing",
			"memo_id", it.MemoID, "error", err)
		return res
	}
	g := p.Generation
	res.Generation = &g
	return res
}

// canDecide reports whether actor may decide this memo.
//
// The owner sees the whole corpus; a member sees their own. Same rule on both
// endpoints — see applyOne, which applies it per item rather than trusting that
// the client only names memos the GET showed it.
func canDecide(actor store.User, memo store.Memo) bool {
	return actor.IsAdmin() || actor.ID == memo.AuthorID
}

// ============================================================================
// Result constructors — one place each, so a status never gets a field it has
// no business carrying.
// ============================================================================

func applied(res Result, link store.MemoLink) Result {
	res.Status = StatusApplied
	res.Destination = link.Destination
	if link.TicketKey != nil {
		res.TicketKey = *link.TicketKey
	}
	return res
}

func refuse(res Result, reason string) Result {
	res.Status = StatusRefused
	res.Reason = reason
	return res
}

func stale(res Result, serverGeneration *int) Result {
	res.Status = StatusStale
	res.Generation = serverGeneration
	res.Reason = "this memo's proposal has changed since you fetched it"
	return res
}

// fail logs the cause and answers transiently.
//
// THE MEMO ID AND THE ERROR, AND NOTHING ELSE. No transcript, no note body, no
// ticket description, and no ticket title.
func (s *Service) fail(ctx context.Context, res Result, op string, err error) Result {
	s.logger.ErrorContext(ctx, "triage item failed", "op", op, "memo_id", res.MemoID, "error", err)
	res.Status = StatusFailed
	res.Reason = op + ": " + err.Error()
	return res
}

// lazyCatalogue fetches once per batch, at most, and remembers the failure too
// — twelve items must not make twelve attempts at an unreachable Switchyard.
type lazyCatalogue struct {
	src  Catalogue
	snap *catalogue.Snapshot
	err  error
	done bool
}

func (l *lazyCatalogue) get(ctx context.Context) (*catalogue.Snapshot, error) {
	if !l.done {
		l.snap, l.err = l.src.Fetch(ctx)
		l.done = true
	}
	return l.snap, l.err
}
