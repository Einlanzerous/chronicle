package triage

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// ============================================================================
// The read side.
// ============================================================================

// OLDEST FIRST, because the screen is an evening pass over a day's backlog and
// the memo most likely to be forgotten is the one from Tuesday. Newest-first
// would put the recording made four minutes ago on top of a list somebody is
// working through to reach the ones they have forgotten.
func TestTheBatchIsOldestFirst(t *testing.T) {
	h := newHarness(t)
	var want []uuid.UUID
	for i := 0; i < 4; i++ {
		m := h.ownMemo("memo " + string(rune('a'+i)))
		h.propose(m.ID, ticketProposal("CHRN"))
		want = append(want, m.ID)
	}

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d", len(items), len(want))
	}
	for i, it := range items {
		if it.MemoID != want[i] {
			t.Fatalf("item %d is %s, want %s — the list is not oldest first", i, it.MemoID, want[i])
		}
	}
}

// A MEMO WITH NO DURABLE TRANSCRIPT IS OMITTED. Not lost: it is in
// GET /admin/transcription, which is where a transcription problem belongs.
func TestAMemoWithNoDurableTranscriptIsNotOffered(t *testing.T) {
	h := newHarness(t)
	good := h.ownMemo("a complete recording")
	h.propose(good.ID, ticketProposal("CHRN"))
	partial := h.partialMemo("half a sen")

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	for _, it := range items {
		if it.MemoID == partial.ID {
			t.Fatal("a memo whose only transcript is partial was offered for triage")
		}
	}
	if len(items) != 1 || items[0].MemoID != good.ID {
		t.Fatalf("got %d items, want only the durable one", len(items))
	}
}

// ABSENT, INVALID AND needs_input ARE THREE DIFFERENT FACTS and the screen must
// not merge any pair — CHRN-32 §7 says so about the last two, and each asks for
// a different action from the operator.
func TestAbsentInvalidAndNeedsInputAreThreeDifferentThings(t *testing.T) {
	h := newHarness(t)

	absent := h.ownMemo("scribe never looked at this one")

	broken := h.ownMemo("the model produced junk")
	h.propose(broken.ID, nil)

	incomplete := h.ownMemo("no project can be told from this")
	p := ticketProposal("CHRN")
	empty := ""
	p.ProjectKey = &empty
	h.propose(incomplete.ID, p)
	if _, err := h.tier1.BumpProposalGeneration(h.ctx, incomplete.ID, testProposer,
		p, nil, scribe.StatusNeedsInput); err != nil {
		t.Fatalf("bump: %v", err)
	}

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	got := map[uuid.UUID]BatchItem{}
	for _, it := range items {
		got[it.MemoID] = it
	}
	if len(got) != 3 {
		t.Fatalf("got %d items, want all three offered", len(got))
	}

	if s := got[absent.ID].Status; s != ProposalAbsent {
		t.Fatalf("a memo Scribe never routed has status %q, want %q", s, ProposalAbsent)
	}
	if got[absent.ID].Generation != nil {
		t.Fatal("a memo with no proposal row carries a generation")
	}

	inv := got[broken.ID]
	if inv.Status != ProposalInvalid {
		t.Fatalf("status = %q, want %q", inv.Status, ProposalInvalid)
	}
	// THE RECORDED ERROR IS SHOWN, or the card says a memo could not be routed
	// and cannot say why.
	if !strings.Contains(inv.Error, "destination") {
		t.Fatalf("error = %q, want the recorded failure", inv.Error)
	}
	if inv.Generation == nil || *inv.Generation != 1 {
		t.Fatalf("generation = %v, want 1 — an invalid row has one like any other", inv.Generation)
	}

	if s := got[incomplete.ID].Status; s != ProposalNeedsInput {
		t.Fatalf("status = %q, want %q — needs_input must never read as invalid", s, ProposalNeedsInput)
	}
}

// Every item carries what the POST will check it against, plus the one field
// that licences ACCEPT ALL.
func TestEachItemCarriesWhatThePostWillCheck(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a routable memo")
	stored := h.propose(m.ID, ticketProposal("CHRN"))

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	it := items[0]
	switch {
	case it.Proposer != testProposer:
		t.Fatalf("proposer = %q, want %q", it.Proposer, testProposer)
	case it.Generation == nil || *it.Generation != stored.Generation:
		t.Fatalf("generation = %v, want %d", it.Generation, stored.Generation)
	case it.Status != ProposalValid:
		t.Fatalf("status = %q", it.Status)
	case it.Proposal == nil || it.Proposal.Title != "Do the thing":
		t.Fatalf("proposal = %+v", it.Proposal)
	case it.Excerpt != "a routable memo":
		t.Fatalf("excerpt = %q, want the transcript's opening", it.Excerpt)
	}
	// 0.9 clears the harness's 0.8 floor.
	if !it.PreAcceptable {
		t.Fatal("a valid 0.9 proposal is not pre-acceptable at a 0.8 floor")
	}
}

// PRE-ACCEPTABILITY IS READ FROM CHRONICLE_SCRIBE_PREACCEPT_MIN AND NOWHERE
// ELSE IN THIS API. Two gates, and the first is not a threshold: DISCARD is
// never pre-acceptable at any confidence, because it is the one accept that
// cannot be walked back.
func TestPreAcceptabilityHasTwoGatesAndOneIsNotAThreshold(t *testing.T) {
	h := newHarness(t)

	low := h.ownMemo("the model was unsure")
	p := ticketProposal("CHRN")
	p.Confidence = 0.4
	h.propose(low.ID, p)

	confidentDiscard := h.ownMemo("testing testing")
	h.propose(confidentDiscard.ID, &scribe.Proposal{
		Destination: scribe.DestDiscard, Confidence: 1.0,
		Reason: "somebody testing their microphone"})

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	for _, it := range items {
		if it.PreAcceptable {
			t.Fatalf("memo %s is pre-acceptable; neither should be", it.MemoID)
		}
	}
}

// A REFUSED DECISION IS SHOWN ON THE MEMO, with the status that refused it.
// Without this the memo simply reappears in the morning list and the operator
// has no account of what happened to yesterday's decision.
func TestARefusedDecisionIsShownOnTheMemoThatReappears(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("this one was refused")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.tracker.createErr = httpError(404, "project CHRN is archived")
	h.apply(h.owner, h.accept(m.ID))

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if len(items) != 1 || items[0].MemoID != m.ID {
		t.Fatalf("the refused memo is not back in the list: %+v", items)
	}
	l := items[0].Link
	switch {
	case l == nil:
		t.Fatal("the memo reappeared with no account of the refusal")
	case l.State != LinkStateRefused:
		t.Fatalf("state = %q, want %q", l.State, LinkStateRefused)
	case l.RefusedStatus == nil || *l.RefusedStatus != 404:
		t.Fatalf("refused_status = %v, want 404", l.RefusedStatus)
	case !strings.Contains(l.RefusedReason, "archived"):
		t.Fatalf("refused_reason = %q", l.RefusedReason)
	}
}

// A pending decision reads as IN FLIGHT while its T2 holds the row, and the
// screen uses the same lock probe the admin report does — so the two cannot
// disagree about what a memo is doing.
func TestAMemoBeingDecidedReadsAsInFlight(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("being decided right now")
	h.propose(m.ID, ticketProposal("CHRN"))

	inT2 := make(chan struct{})
	release := make(chan struct{})
	h.svc.beforeCreate = func(uuid.UUID) error {
		close(inT2)
		<-release
		return nil
	}
	go func() { _, _ = h.svc.Apply(h.ctx, h.owner, []Item{h.accept(m.ID)}) }()
	<-inT2

	reader := h.sweeper()
	items, err := reader.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if len(items) != 1 || items[0].Link == nil {
		t.Fatalf("items = %+v, want the memo with its pending decision", items)
	}
	if got := items[0].Link.State; got != LinkStateInFlight {
		t.Fatalf("state = %q, want %q", got, LinkStateInFlight)
	}
	close(release)
}

// ============================================================================
// GET /admin/triage
// ============================================================================

// FOUR WAYS, and only three of them come from columns. In flight is observed by
// taking row locks; the alternatives — a column, or an age predicate — both
// produce a row that claims to be working forever after the process holding it
// died.
func TestTheAdminReportSplitsLinkRowsFourWays(t *testing.T) {
	h := newHarness(t)

	// The order below is not arbitrary. EVERY BATCH SWEEPS BEFORE IT STARTS, so
	// a row parked in one state is resolved by the next Apply unless the sweep
	// still cannot resolve it — which is why the unresolved row is set up last
	// and the search is left failing from that point on.

	// AMBIGUOUS · two tickets claim one memo.
	ambiguous := h.ownMemo("claimed twice")
	h.propose(ambiguous.ID, ticketProposal("CHRN"))
	h.crashAfterCreate()
	h.apply(h.owner, h.accept(ambiguous.ID))
	h.tracker.plant(ambiguous.ID, "CHRN-777")
	if _, err := h.sweeper().Sweep(h.ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	h.svc.afterCreate = nil

	// REFUSED · Switchyard said no, permanently.
	refused := h.ownMemo("refused permanently")
	h.propose(refused.ID, ticketProposal("CHRN"))
	h.tracker.createErr = httpError(422, "title is too long")
	h.apply(h.owner, h.accept(refused.ID))
	h.tracker.createErr = nil

	// UNRESOLVED · a crash before the call, and a Switchyard that cannot be
	// asked what happened.
	unresolved := h.ownMemo("nobody knows what happened to this")
	h.propose(unresolved.ID, ticketProposal("CHRN"))
	h.crashBeforeCreate()
	h.apply(h.owner, h.accept(unresolved.ID))
	h.tracker.searchErr = httpError(503, "unavailable")
	if _, err := h.sweeper().Sweep(h.ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	// IN FLIGHT · a T2 holding its row right now.
	live := h.ownMemo("being decided right now")
	h.propose(live.ID, ticketProposal("CHRN"))
	inT2 := make(chan struct{})
	release := make(chan struct{})
	h.svc.beforeCreate = func(uuid.UUID) error {
		close(inT2)
		<-release
		return nil
	}
	go func() { _, _ = h.svc.Apply(h.ctx, h.owner, []Item{h.accept(live.ID)}) }()
	<-inT2

	rep, err := h.sweeper().Admin(h.ctx)
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	close(release)

	switch {
	case len(rep.InFlight) != 1 || rep.InFlight[0].State != LinkStateInFlight:
		t.Fatalf("in_flight = %+v, want the live T2's row", rep.InFlight)
	case len(rep.Unresolved) != 1:
		t.Fatalf("unresolved = %+v, want one", rep.Unresolved)
	case len(rep.Ambiguous) != 1:
		t.Fatalf("ambiguous = %+v, want one", rep.Ambiguous)
	case len(rep.Refused) != 1:
		t.Fatalf("refused = %+v, want one", rep.Refused)
	}
	if len(rep.Ambiguous[0].CandidateKeys) != 2 {
		t.Fatalf("the ambiguous row does not carry both keys: %+v", rep.Ambiguous[0])
	}
	if rep.Refused[0].RefusedStatus == nil || *rep.Refused[0].RefusedStatus != 422 {
		t.Fatalf("the refused row does not carry its status: %+v", rep.Refused[0])
	}
	// The unresolved row LOOKED and found nothing, which is a different fact
	// from never having looked — and the only way to tell them apart.
	if rep.Unresolved[0].SweptAt == nil {
		t.Fatalf("the unresolved row does not record that a sweep looked: %+v", rep.Unresolved[0])
	}
	// IN FLIGHT IS NOT AGE. All four rows were created within milliseconds of
	// each other, so an age predicate would have called all four in flight, or
	// none of them.
	for _, l := range append(append([]LinkState{}, rep.Unresolved...), rep.Ambiguous...) {
		if l.State == LinkStateInFlight {
			t.Fatalf("an unlocked row reads as in flight: %+v", l)
		}
	}
}

// The backlog is reported BY AGE, because forty memos from this evening and
// four from three weeks ago want completely different remedies and one number
// cannot tell them apart.
func TestTheAdminReportCountsTheBacklogByAge(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < 3; i++ {
		m := h.ownMemo("waiting " + string(rune('a'+i)))
		h.propose(m.ID, ticketProposal("CHRN"))
	}
	// One decided memo, which must not be counted as waiting.
	done := h.ownMemo("already decided")
	h.propose(done.ID, ticketProposal("CHRN"))
	h.apply(h.owner, h.accept(done.ID))

	rep, err := h.svc.Admin(h.ctx)
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	if rep.Backlog.Total != 3 || rep.Backlog.Today != 3 {
		t.Fatalf("backlog = %+v, want three waiting and all captured today", rep.Backlog)
	}
	if rep.Backlog.OldestCapturedAt == nil {
		t.Fatal("the backlog has no oldest capture time")
	}
	// A partial memo is not a triage backlog: it is a transcription problem,
	// and counting it here would send the operator to the wrong screen.
	h.partialMemo("half a sen")
	rep, err = h.svc.Admin(h.ctx)
	if err != nil {
		t.Fatalf("Admin: %v", err)
	}
	if rep.Backlog.Total != 3 {
		t.Fatalf("total = %d, want the partial memo left out", rep.Backlog.Total)
	}
}

// The GET is capped at the same number the POST is, because the batch IS the
// screen: a client able to POST more items than it could GET would be composing
// decisions about memos it never displayed.
func TestTheBatchIsCappedAtTheSameNumberThePostIs(t *testing.T) {
	h := newHarness(t)
	items, err := h.svc.Batch(h.ctx, h.owner, MaxLimit*10)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if len(items) > MaxLimit {
		t.Fatalf("got %d items past the cap of %d", len(items), MaxLimit)
	}
	// The two caps are one number, asserted rather than assumed.
	if _, err := h.svc.Apply(h.ctx, h.owner, make([]Item, MaxLimit+1)); err == nil {
		t.Fatal("the POST accepted more than the GET's cap")
	}
}

// A memo whose decision landed leaves the list, because it is no longer
// `transcribed`. Asserted so that "the list shows what is waiting" stays a fact
// rather than a hope.
func TestADecidedMemoLeavesTheList(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("about to be decided")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.apply(h.owner, h.accept(m.ID))

	items, err := h.svc.Batch(h.ctx, h.owner, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %+v, want the decided memo gone", items)
	}
	if got := h.state(m.ID); got != store.StateTriaged {
		t.Fatalf("memo is %q", got)
	}
}
