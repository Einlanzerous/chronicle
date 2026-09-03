package triage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// ============================================================================
// Recovery — the crash between the outward call and the confirm.
// ============================================================================

// HOOKED AFTER `CreateTicket` RETURNS. A ticket exists and the row does not
// know. The sweep finds it by `cf.chronicle_memo_id` — the metadata CHRN-35
// stamps — and confirms, which is why the recovery has no expiry: the memo id
// is a property of the ticket, not a cache entry with a 24-hour TTL.
//
// AN INJECTED HOOK RATHER THAN A PROCESS KILL. Killing a process mid-test
// leaves nothing to assert against; this reproduces the exact window.
func TestACrashAfterTheCreateIsRecoveredBySearchingForTheTicket(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo whose confirm never landed")
	h.propose(m.ID, ticketProposal("CHRN"))

	h.crashAfterCreate()
	res := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, res[0], StatusFailed)

	// The world after the crash: one ticket, one PENDING row, memo untriaged.
	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets created, want the one the crash left behind", got)
	}
	if !h.link(m.ID).Pending() {
		t.Fatal("the crash did not leave a pending row, so there is nothing to recover from")
	}
	if got := h.state(m.ID); got != store.StateTranscribed {
		t.Fatalf("memo is %q, want it not yet advanced", got)
	}

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Confirmed != 1 || rep.Created != 0 {
		t.Fatalf("report = %+v, want one confirmed and nothing created", rep)
	}

	// ONE TICKET, ONE CONFIRMED LINK, MEMO TRIAGED.
	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets, want exactly one", got)
	}
	l := h.link(m.ID)
	if !l.Confirmed() || l.TicketKey == nil {
		t.Fatalf("link = %+v, want confirmed with a key", l)
	}
	if got := h.state(m.ID); got != store.StateTriaged {
		t.Fatalf("memo is %q, want triaged", got)
	}
	if l.SweptAt == nil || len(l.CandidateKeys) != 1 {
		t.Fatalf("link = %+v, want swept_at and the candidate it found recorded", l)
	}
}

// HOOKED BEFORE THE OUTWARD CALL. Nothing was created. The sweep finds nothing,
// creates with the STORED DECISION and the STORED KEY, and confirms — and the
// stored key is what makes that create a replay rather than a second ticket if
// the original call did in fact get through.
func TestACrashBeforeTheCreateIsRecoveredByCreating(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo whose create never happened")
	h.propose(m.ID, ticketProposal("CHRN"))

	h.crashBeforeCreate()
	res := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, res[0], StatusFailed)

	if h.tracker.createdCount() != 0 {
		t.Fatal("something was created before the injected failure")
	}
	pending := h.link(m.ID)
	if !pending.Pending() {
		t.Fatal("no pending row to recover from")
	}
	storedKey := pending.SentIdempotencyKey

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Created != 1 || rep.Confirmed != 0 {
		t.Fatalf("report = %+v, want one created", rep)
	}

	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets, want exactly one", got)
	}
	// THE STORED KEY, not a fresh one. A sweep that minted its own would turn
	// every recovery into a duplicate whenever the original call had in fact
	// reached Switchyard.
	var sentStored bool
	for _, k := range h.tracker.sentKeys {
		if k == storedKey {
			sentStored = true
		}
	}
	if !sentStored {
		t.Fatalf("the sweep sent %v, want the stored key %q", h.tracker.sentKeys, storedKey)
	}
	// AND THE STORED DECISION. The row's own sent_* fields, not anything the
	// sweep composed.
	l := h.link(m.ID)
	if !l.Confirmed() || l.SentProjectKey != "CHRN" || l.SentTitle != "Do the thing" {
		t.Fatalf("link = %+v, want it confirmed against the decision it stored", l)
	}
	if got := h.state(m.ID); got != store.StateTriaged {
		t.Fatalf("memo is %q, want triaged", got)
	}
}

// ============================================================================
// SKIP LOCKED — the sweep must not race a live T2.
// ============================================================================

// A SWEEP RUNNING WHILE ANOTHER BATCH'S T2 HOLDS ITS ROW PRODUCES ONE TICKET.
//
// This is the test that fails silently under an age-based sweep. A pending row
// is committed the instant T1 returns, so a sweep that picked rows by age would
// see this one mid-flight, search, find nothing yet, and create — a duplicate
// manufactured by the thing meant to prevent one. `FOR UPDATE SKIP LOCKED`
// reads the T2's own lock as "in flight", which it is by definition.
func TestASweepSkipsARowWhoseOwnT2IsStillRunning(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("decided and swept at once")
	h.propose(m.ID, ticketProposal("CHRN"))

	inT2 := make(chan struct{})
	release := make(chan struct{})
	h.svc.beforeCreate = func(uuid.UUID) error {
		close(inT2)
		<-release
		return nil
	}

	done := make(chan []Result, 1)
	go func() {
		r, err := h.svc.Apply(h.ctx, h.owner, []Item{h.accept(m.ID)})
		if err != nil {
			t.Errorf("Apply: %v", err)
		}
		done <- r
	}()

	<-inT2 // T2 now holds the link row and the memo row.

	// A DIFFERENT service, as the background sweep is a different goroutine.
	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Examined != 0 {
		t.Fatalf("the sweep examined %d rows, want it to skip the locked one", rep.Examined)
	}
	if h.tracker.searches != 0 {
		t.Fatal("the sweep searched Switchyard for a row that was in flight")
	}

	close(release)
	res := <-done
	wantStatus(t, res[0], StatusApplied)

	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets created, want exactly one", got)
	}
}

// The other half: a pending row that nothing holds IS claimed. Without this,
// the test above would pass against a sweep that never claims anything.
func TestASweepClaimsAnUnlockedPendingRow(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("abandoned")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.crashBeforeCreate()
	h.apply(h.owner, h.accept(m.ID))

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Examined != 1 {
		t.Fatalf("report = %+v, want the abandoned row claimed", rep)
	}
}

// ============================================================================
// The memo-state rule, at the sweep's own two doors.
// ============================================================================

// NO MATCH + A HELD MEMO: DO NOT CREATE. The operator stepped back from this
// memo after the decision was written; creating now would file a ticket they
// have already changed their mind about, and then fail the advance forever.
func TestTheSweepDoesNotCreateForAHeldMemo(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("held after deciding")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.crashBeforeCreate()
	h.apply(h.owner, h.accept(m.ID))

	h.hold(m.ID)

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Refused != 1 || rep.Created != 0 {
		t.Fatalf("report = %+v, want one refusal and nothing created", rep)
	}
	if h.tracker.createdCount() != 0 {
		t.Fatal("the sweep created a ticket for a held memo")
	}
	l := h.link(m.ID)
	if !l.Refused() || !strings.Contains(l.RefusedReason, "CHRN-34") {
		t.Fatalf("link = %+v, want a refusal naming the hold inbox", l)
	}
	if got := h.state(m.ID); got != store.StateHeld {
		t.Fatalf("memo is %q, want it still held", got)
	}
}

// ONE MATCH + A HELD MEMO: CONFIRM THE LINK, LEAVE THE MEMO ALONE. The ticket
// is real and the trail back to the recording matters more than the state;
// whether a held memo may become triaged is CHRN-34's question.
//
// And there must be NO ADVANCE ERROR ON ANY LATER BATCH, which is the half that
// would be missed by confirming and advancing anyway.
func TestTheSweepConfirmsAHeldMemosTicketWithoutMovingIt(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("held after the ticket existed")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.crashAfterCreate()
	h.apply(h.owner, h.accept(m.ID))

	h.hold(m.ID)

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Confirmed != 1 {
		t.Fatalf("report = %+v, want the link confirmed", rep)
	}
	l := h.link(m.ID)
	if !l.Confirmed() || l.TicketKey == nil {
		t.Fatalf("link = %+v, want it confirmed", l)
	}
	if got := h.state(m.ID); got != store.StateHeld {
		t.Fatalf("memo is %q, want the hold left standing", got)
	}

	// A later batch answers from the row and does not try to advance again.
	res := h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer})
	wantStatus(t, res[0], StatusApplied)
	if res[0].TicketKey != *l.TicketKey {
		t.Fatalf("key = %q, want the confirmed %q", res[0].TicketKey, *l.TicketKey)
	}
	// A second sweep has nothing to do, because the row is confirmed.
	rep, err = h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Examined != 0 {
		t.Fatalf("report = %+v, want nothing left to sweep", rep)
	}
}

// ============================================================================
// Ambiguity — the one outcome no amount of retrying resolves.
// ============================================================================

// MORE THAN ONE TICKET CLAIMS ONE MEMO: CONFIRM NOTHING. Picking one turns the
// other into an orphan nobody will ever find, and there is no evidence here
// that says which is right. Both keys are recorded and a person decides.
func TestASweepFindingTwoTicketsConfirmsNeither(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("claimed twice")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.crashAfterCreate()
	h.apply(h.owner, h.accept(m.ID))

	// A second ticket carrying the same memo id — a race from before the
	// pending row existed, or somebody who copied the metadata by hand.
	h.tracker.plant(m.ID, "CHRN-777")

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Ambiguous != 1 || rep.Confirmed != 0 || rep.Created != 0 {
		t.Fatalf("report = %+v, want one ambiguity and nothing resolved", rep)
	}

	l := h.link(m.ID)
	switch {
	case !l.Pending():
		t.Fatalf("link = %+v, want it left pending", l)
	case len(l.CandidateKeys) != 2:
		t.Fatalf("candidate_keys = %v, want both", l.CandidateKeys)
	case l.SweptAt == nil:
		t.Fatal("the sweep did not record that it looked")
	case !l.Ambiguous():
		t.Fatal("the row does not read as ambiguous")
	}
	if got := h.state(m.ID); got != store.StateTranscribed {
		t.Fatalf("memo is %q, want it untouched", got)
	}

	// RE-ACCEPTING MAKES NO CREATE AND NO CONFIRM. The accept path never
	// creates for a row it does not own — otherwise it would be the accept
	// silently picking the winner the sweep refused to pick.
	created, calls := h.tracker.createdCount(), h.tracker.calls
	res := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, res[0], StatusRefused)
	if !strings.Contains(res[0].Reason, "CHRN-777") {
		t.Fatalf("reason = %q, want it to name what was found", res[0].Reason)
	}
	if h.tracker.createdCount() != created || h.tracker.calls != calls {
		t.Fatal("re-accepting an ambiguous memo went to the wire")
	}
	if h.link(m.ID).Confirmed() {
		t.Fatal("re-accepting confirmed one of the two")
	}
}

// ============================================================================
// Unreachable.
// ============================================================================

// A pass that could not look LEAVES THE ROW PENDING and CARRIES THE CANDIDATES
// FORWARD. Clearing them would silently downgrade an ambiguity to "unresolved"
// on the first network blip, and the next pass would create a third ticket.
func TestAnUnreachableSweepKeepsWhatAnEarlierPassFound(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("claimed twice, then a blip")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.crashAfterCreate()
	h.apply(h.owner, h.accept(m.ID))
	h.tracker.plant(m.ID, "CHRN-778")

	sw := h.sweeper()
	if _, err := sw.Sweep(h.ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(h.link(m.ID).CandidateKeys) != 2 {
		t.Fatal("setup: the first pass did not record both")
	}

	h.tracker.searchErr = httpError(503, "unavailable")
	rep, err := sw.Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Unresolved != 1 && rep.Ambiguous != 1 {
		t.Fatalf("report = %+v, want the row left pending", rep)
	}
	l := h.link(m.ID)
	if !l.Pending() {
		t.Fatalf("link = %+v, want it still pending", l)
	}
	if len(l.CandidateKeys) != 2 {
		t.Fatalf("candidate_keys = %v, want the earlier pass's findings kept", l.CandidateKeys)
	}
}

// A SEARCH THAT WAS REFUSED IS NOT A DECISION THAT WAS REFUSED, and the row
// must survive it — WITH A TICKET ALREADY BEHIND IT, which is the case that
// makes the difference visible.
//
// A 4xx on the CREATE means nothing was created, so the decision will never
// land and marking it refused is right. A 4xx on the SEARCH means the lookup
// failed and says nothing about whether a ticket exists. Marking that refused
// used to be terminal — `refused_at` is exactly what stops the sweep claiming
// the row again — so the ticket was stranded, and worse: the operator, told to
// change their decision, re-armed the row, T2 never searches, and a SECOND
// ticket was created for one memo. The recovery mechanism manufacturing the
// duplicate the whole design exists to prevent.
//
// The earlier version of this test used crashBeforeCreate, so no ticket existed
// behind the row and the loss was invisible.
func TestARefusedSearchDoesNotStrandATicketThatExists(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo whose confirm never landed")
	h.propose(m.ID, ticketProposal("CHRN"))

	// A ticket EXISTS; only the confirm is missing.
	h.crashAfterCreate()
	h.apply(h.owner, h.accept(m.ID))
	if h.tracker.createdCount() != 1 {
		t.Fatal("setup: no ticket behind the pending row")
	}

	// The token was rotated and this deployment still holds the old one. 401 is
	// non-retryable, which is what used to end the row.
	h.tracker.searchErr = httpError(401, "unauthorized")
	sw := h.sweeper()
	rep, err := sw.Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Refused != 0 {
		t.Fatalf("report = %+v, want the row left pending rather than refused", rep)
	}
	if l := h.link(m.ID); !l.Pending() {
		t.Fatalf("link = %+v, want it still claimable once the credential is fixed", l)
	}

	// And once it is fixed, the very next pass finds the ticket and confirms
	// it. One ticket, one confirmed link, memo triaged.
	h.tracker.searchErr = nil
	if _, err := sw.Sweep(h.ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets, want the original and only the original", got)
	}
	l := h.link(m.ID)
	if !l.Confirmed() || l.TicketKey == nil {
		t.Fatalf("link = %+v, want it confirmed against the ticket that existed all along", l)
	}
	if got := h.state(m.ID); got != store.StateTriaged {
		t.Fatalf("memo is %q, want triaged", got)
	}
}

// The other half, so the two are not confused: a refused CREATE still marks the
// row. Nothing was created, so the decision will never land whatever is retried.
func TestARefusedCreateStillMarksTheRow(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.crashBeforeCreate()
	h.apply(h.owner, h.accept(m.ID))

	h.svc.beforeCreate = nil
	h.tracker.createErr = httpError(404, "project CHRN is archived")
	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Refused != 1 {
		t.Fatalf("report = %+v, want a refusal", rep)
	}
	l := h.link(m.ID)
	if l.RefusedStatus == nil || *l.RefusedStatus != 404 {
		t.Fatalf("refused_status = %v, want 404", l.RefusedStatus)
	}
}

// ============================================================================
// The sweep before every batch.
// ============================================================================

// A TRANSIENTLY FAILED ITEM IS RE-ATTEMPTED BY THE SWEEP AT THE REPLAY'S BATCH
// START, and T2 then makes NO OUTWARD CALL for the pre-existing row.
//
// Without the batch-start sweep, that item would answer `failed` forever: T2
// never creates for a row it does not own, so nothing would ever resolve it.
func TestAReplayedBatchSweepsTheFailedItemFirst(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("failed, then replayed")
	h.propose(m.ID, ticketProposal("CHRN"))

	// A crash after the create: the ticket exists and the row does not know.
	h.crashAfterCreate()
	first := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, first[0], StatusFailed)
	created := h.tracker.createdCount()

	// The replay. The hook is gone — the process restarted — and the batch
	// sweeps before it starts.
	h.svc.afterCreate = nil
	callsBefore := h.tracker.calls

	second := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, second[0], StatusApplied)

	if got := h.tracker.createdCount(); got != created {
		t.Fatalf("the replay created %d more tickets", got-created)
	}
	// T2 MADE NO OUTWARD CALL. The sweep confirmed the row before the batch
	// looked at it, so the item answered from a confirmed link.
	if h.tracker.calls != callsBefore {
		t.Fatalf("%d create calls during the replay, want none", h.tracker.calls-callsBefore)
	}
	if got := h.state(m.ID); got != store.StateTriaged {
		t.Fatalf("memo is %q, want triaged", got)
	}
}

// A pending DISCARD has nothing to search for and nothing to create. The sweep
// finishes it rather than asking Switchyard about a decision that never left
// this process.
func TestTheSweepFinishesAPendingDiscardWithoutAskingSwitchyard(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("throw this away")
	h.propose(m.ID, ticketProposal("CHRN"))

	// Write a pending DISCARD row directly: the accept path confirms one in the
	// same call, so the only way it can be pending is a T2 that died.
	if _, _, err := h.store.ClaimMemoLink(h.ctx, store.Decision{
		MemoID: m.ID, Destination: store.LinkDiscard, IdempotencyKey: "chronicle-orphan-1",
	}); err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Confirmed != 1 {
		t.Fatalf("report = %+v, want the discard finished", rep)
	}
	if h.tracker.searches != 0 {
		t.Fatal("the sweep asked Switchyard about a DISCARD")
	}
	if got := h.state(m.ID); got != store.StateDiscarded {
		t.Fatalf("memo is %q, want discarded", got)
	}
}

// The background loop stops with its context, like every other worker here.
func TestTheSweepLoopStopsWithItsContext(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(h.ctx)
	done := make(chan error, 1)
	go func() { done <- h.sweeper().Run(ctx, 10*time.Millisecond) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the sweep loop did not stop")
	}
}
