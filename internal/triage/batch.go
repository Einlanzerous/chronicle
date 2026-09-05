package triage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// The read side: the day's untriaged memos, with everything the screen needs to
// show a card and everything the POST will check the card against.
//
// IT IS TWO STORES AND A JOIN IN GO, and that is the tier boundary rather than
// an unoptimised query. The memo, its transcript and any decision already made
// are tier 2; the proposal is tier 1. They are reached through different types
// over pools that connect as different roles, so there is no transaction that
// could span them and no SQL join that could exist.

// Proposal statuses as the screen sees them. THE FIRST FOUR ARE FOUR DIFFERENT
// FACTS and CHRN-32 §7 is explicit that two of them must never be merged:
// `needs_input` is a memo an operator finishes in a tap, `invalid` is a memo
// with no proposal at all, and `absent` is a memo Scribe has never looked at.
// Collapsing any pair produces a screen that asks for the wrong action.
const (
	ProposalValid      = "valid"
	ProposalNeedsInput = "needs_input"
	ProposalInvalid    = "invalid"
	ProposalAbsent     = "absent"
)

// Link states as the screen and the admin report see them.
const (
	LinkStateConfirmed  = "confirmed"
	LinkStateInFlight   = "in_flight"
	LinkStateUnresolved = "unresolved"
	LinkStateAmbiguous  = "ambiguous"
	LinkStateRefused    = "refused"
)

// BatchItem is one card.
type BatchItem struct {
	MemoID     uuid.UUID `json:"memo_id"`
	CapturedAt time.Time `json:"captured_at"`
	DurationMS *int32    `json:"duration_ms,omitempty"`

	// Excerpt is the transcript's opening. It is also the LABEL FOR A DISCARD,
	// which carries no title by design — the model is not asked to invent an
	// imperative title for something being thrown away, so the screen falls
	// back to what was actually said.
	Excerpt string `json:"excerpt"`

	// Proposer is on every item, and the POST requires it back, because a
	// proposal's identity is (memo_id, proposer).
	Proposer string `json:"proposer"`

	// Generation is what the POST must echo. NULL MEANS "NO PROPOSAL ROW", and
	// echoing null is an assertion the server checks: if a row has appeared
	// since this GET, the item comes back `stale`.
	Generation *int   `json:"generation"`
	Status     string `json:"status"`

	Proposal *scribe.Proposal      `json:"proposal,omitempty"`
	Cleared  []scribe.ClearedField `json:"cleared_fields,omitempty"`

	// Error is the recorded failure behind an `invalid` proposal. Shown,
	// because the alternative is a card that says a memo could not be routed
	// and cannot say why.
	Error string `json:"error,omitempty"`

	// PreAcceptable says whether this card may be pre-selected for ACCEPT ALL.
	// COMPUTED HERE AND NOWHERE ELSE: it is the only place this API reads
	// CHRONICLE_SCRIBE_PREACCEPT_MIN, and a second reader would be a second
	// threshold to keep in step.
	PreAcceptable bool `json:"pre_acceptable"`

	// Link is a decision already recorded against this memo. A memo appearing
	// here with a REFUSED link is the case this field exists for: without it,
	// the memo just reappears in the morning with no account of what happened.
	Link *LinkState `json:"link,omitempty"`
}

// LinkState is a decision's standing, as the screen and the admin report
// describe it. A HANDLE AND A STATE — never a title or a status as Switchyard
// holds them, which is invariant 2 enforced by there being nothing else here.
type LinkState struct {
	Destination string    `json:"destination"`
	State       string    `json:"state"`
	DecidedAt   time.Time `json:"decided_at"`

	TicketKey string `json:"ticket_key,omitempty"`
	TicketURL string `json:"ticket_url,omitempty"`

	// CandidateKeys is populated only for `ambiguous`, and it is the whole of
	// what a person needs to resolve one.
	CandidateKeys []string `json:"candidate_keys,omitempty"`

	SweptAt       *time.Time `json:"swept_at,omitempty"`
	RefusedStatus *int       `json:"refused_status,omitempty"`
	RefusedReason string     `json:"refused_reason,omitempty"`
	RefusedAtTime *time.Time `json:"refused_at,omitempty"`
}

// Batch returns the memos awaiting a decision, oldest first.
//
// limit is capped at MaxLimit — the same cap the POST enforces, because the
// batch is the screen.
func (s *Service) Batch(ctx context.Context, actor store.User, limit int) ([]BatchItem, error) {
	switch {
	case limit <= 0:
		limit = DefaultLimit
	case limit > MaxLimit:
		limit = MaxLimit
	}

	// The owner sees every author's memos; a member sees their own. The POST
	// applies the same rule per item — see applyOne — because a list that
	// merely hides a memo is not access control.
	scope := actor.ID
	if actor.IsAdmin() {
		scope = uuid.Nil
	}

	memos, err := s.store.UntriagedMemos(ctx, scope, limit)
	if err != nil {
		return nil, err
	}
	if len(memos) == 0 {
		return []BatchItem{}, nil
	}

	ids := make([]uuid.UUID, len(memos))
	var pendingLinks []uuid.UUID
	for i, m := range memos {
		ids[i] = m.Memo.ID
		if m.Link != nil && m.Link.Pending() {
			pendingLinks = append(pendingLinks, m.Link.ID)
		}
	}

	proposals, err := s.tier1.ProposalsForMemos(ctx, ids, s.proposer)
	if err != nil {
		return nil, err
	}

	// IN FLIGHT IS A LOCK, NOT A COLUMN AND NOT AN AGE. The same probe the
	// admin report uses, so the two never disagree about what a memo is doing.
	flight, err := s.store.LinksInFlight(ctx, pendingLinks)
	if err != nil {
		return nil, err
	}

	out := make([]BatchItem, 0, len(memos))
	for _, m := range memos {
		it := BatchItem{
			MemoID:     m.Memo.ID,
			CapturedAt: m.Memo.CapturedAt,
			DurationMS: m.Memo.DurationMS,
			Excerpt:    m.Excerpt,
			Proposer:   s.proposer,
			Status:     ProposalAbsent,
		}
		if p, ok := proposals[m.Memo.ID]; ok {
			g := p.Generation
			it.Generation = &g
			it.Status = string(p.Status)
			it.Proposal = p.Payload
			it.Cleared = p.Cleared
			it.Error = p.Error
			if p.Payload != nil {
				it.PreAcceptable = p.Payload.PreAcceptable(p.Status, s.preacceptMin)
			}
		}
		if m.Link != nil {
			it.Link = s.linkState(*m.Link, flight[m.Link.ID])
		}
		out = append(out, it)
	}
	return out, nil
}

// linkState describes one decision row. The four pending states come from the
// plan's own table, and only one of them is read from a lock.
func (s *Service) linkState(l store.MemoLink, inFlight bool) *LinkState {
	st := &LinkState{
		Destination:   l.Destination,
		DecidedAt:     l.CreatedAt,
		CandidateKeys: l.CandidateKeys,
		SweptAt:       l.SweptAt,
		RefusedStatus: l.RefusedStatus,
		RefusedReason: l.RefusedReason,
		RefusedAtTime: l.RefusedAt,
	}
	if l.TicketKey != nil {
		st.TicketKey = *l.TicketKey
		st.TicketURL = s.tickets.TicketURL(*l.TicketKey)
	}
	switch {
	case l.Confirmed():
		st.State = LinkStateConfirmed
	case l.Refused():
		st.State = LinkStateRefused
		// Candidates belonged to the decision that was refused; showing them
		// beside a refusal would offer a ticket nobody asked for.
		st.CandidateKeys = nil
	case inFlight:
		st.State = LinkStateInFlight
	case l.Ambiguous():
		st.State = LinkStateAmbiguous
	default:
		st.State = LinkStateUnresolved
	}
	return st
}

// AdminReport is what an operator reads to answer "is triage healthy".
type AdminReport struct {
	Backlog BacklogReport `json:"backlog"`

	// Deferred is CHRN-34's inbox as a NUMBER, beside the backlog rather than
	// inside it. The backlog counts what is waiting for a decision nobody has
	// made; this counts what somebody decided to decide later. Adding them
	// would produce one figure that cannot be driven to zero, because half of
	// it is deliberate.
	Deferred int `json:"deferred"`

	// The four link states. Three are read from columns; IN FLIGHT IS OBSERVED
	// BY TAKING ROW LOCKS — see store.TriageLinkStates for why it cannot be a
	// column or a timestamp.
	InFlight   []LinkState `json:"in_flight"`
	Unresolved []LinkState `json:"unresolved"`
	Ambiguous  []LinkState `json:"ambiguous"`
	Refused    []LinkState `json:"refused"`
}

// BacklogReport is how much is waiting, BY AGE. Forty memos captured this
// evening is a normal day; four from three weeks ago is a screen nobody opens,
// and one number cannot tell them apart.
type BacklogReport struct {
	Total            int        `json:"total"`
	Today            int        `json:"today"`
	ThisWeek         int        `json:"this_week"`
	Older            int        `json:"older"`
	OldestCapturedAt *time.Time `json:"oldest_captured_at,omitempty"`
}

// Admin builds the report behind GET /admin/triage.
func (s *Service) Admin(ctx context.Context) (AdminReport, error) {
	var rep AdminReport

	b, err := s.store.TriageBacklog(ctx)
	if err != nil {
		return rep, err
	}
	rep.Backlog = BacklogReport{
		Total: b.Total, Today: b.Today, ThisWeek: b.ThisWeek, Older: b.Older,
		OldestCapturedAt: b.OldestCapturedAt,
	}

	if rep.Deferred, err = s.store.CountTriageHolds(ctx); err != nil {
		return rep, err
	}

	links, err := s.store.TriageLinkStates(ctx, MaxLimit*4)
	if err != nil {
		return rep, err
	}
	rep.InFlight = s.linkStates(links.InFlight, true)
	rep.Unresolved = s.linkStates(links.Unresolved, false)
	rep.Ambiguous = s.linkStates(links.Ambiguous, false)
	rep.Refused = s.linkStates(links.Refused, false)
	return rep, nil
}

func (s *Service) linkStates(ls []store.MemoLink, inFlight bool) []LinkState {
	out := make([]LinkState, 0, len(ls))
	for _, l := range ls {
		out = append(out, *s.linkState(l, inFlight))
	}
	return out
}

// sweepBeforeBatch resolves what an earlier batch left behind, so that a replay
// of that batch finds its rows already answered.
//
// AT THE START OF EVERY BATCH, and this is the mechanism behind the replay
// criterion rather than a convenience. An item that failed transiently left a
// pending row; T2 will not create for a row it does not own, so on the replay
// that item would answer `failed` forever if nothing else ever resolved it.
// Sweeping first means the row is confirmed or refused by the time the replay's
// T2 looks at it, and the replay reports the real outcome.
//
// Errors are logged and swallowed: a sweep that cannot run is not a reason to
// refuse a batch of decisions a person is waiting on.
func (s *Service) sweepBeforeBatch(ctx context.Context) {
	// DETACHED, for the reason the items are: the sweep makes outward calls and
	// confirms rows, and a client hanging up mid-sweep would abandon one
	// halfway rather than let it finish.
	sweepCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), itemBudget)
	defer cancel()

	rep, err := s.Sweep(sweepCtx)
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		s.logger.WarnContext(ctx, "triage sweep ran out of time before the batch; the background pass will finish it")
	case err != nil:
		s.logger.ErrorContext(ctx, "triage sweep before batch failed", "error", err)
	case !rep.empty():
		s.logSweep(ctx, rep)
	}
}

// ErrBadRequest is a request this API will never accept as written. Distinct
// from a per-item refusal: this one is about the batch itself.
var ErrBadRequest = errors.New("triage: bad request")
