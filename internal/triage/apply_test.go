package triage

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// ============================================================================
// The ticket, in one test: eleven land, one does not, and the client is told
// precisely which.
// ============================================================================

// THE `Done when`, VERBATIM: "a 12-item batch with one failing item produces 11
// durable outcomes and one clear error, and replaying the same batch changes
// nothing."
//
// The failure is injected at the item that is hardest to get right — one in the
// MIDDLE — because a batch that rolled back on failure would still pass a test
// that failed the last item.
func TestATwelveItemBatchSurvivesOneFailure(t *testing.T) {
	h := newHarness(t)

	memos := make([]store.Memo, 12)
	items := make([]Item, 12)
	for i := range memos {
		memos[i] = h.ownMemo("memo number " + string(rune('a'+i)))
		h.propose(memos[i].ID, ticketProposal("CHRN"))
		items[i] = h.accept(memos[i].ID)
	}

	// Item 7 (index 6) is refused by Switchyard and nothing else is.
	const failing = 6
	h.tracker.createErr = nil
	h.svc.beforeCreate = func(memoID uuid.UUID) error { return nil }
	h.tracker.onCreate = nil

	// The refusal is targeted by swapping the error in for exactly one item,
	// which is what a real archived project would do.
	var mu sync.Mutex
	h.svc.beforeCreate = func(memoID uuid.UUID) error {
		mu.Lock()
		defer mu.Unlock()
		if memoID == memos[failing].ID {
			h.tracker.createErr = httpError(404, "project ARGY is archived")
		} else {
			h.tracker.createErr = nil
		}
		return nil
	}

	res := h.apply(h.owner, items...)
	if len(res) != 12 {
		t.Fatalf("got %d results, want 12", len(res))
	}

	for i, r := range res {
		if i == failing {
			wantStatus(t, r, StatusRefused)
			// A CLEAR ERROR, AND IT NAMES THE MEMO. Positionally and by id, so
			// a client can re-show exactly the one card.
			if r.MemoID != memos[failing].ID {
				t.Fatalf("the error names memo %s, want %s", r.MemoID, memos[failing].ID)
			}
			if !strings.Contains(r.Reason, "archived") {
				t.Fatalf("reason = %q, want Switchyard's own explanation", r.Reason)
			}
			continue
		}
		wantStatus(t, r, StatusApplied)
		if r.TicketKey == "" {
			t.Fatalf("item %d applied with no ticket key", i)
		}
	}

	// ELEVEN DURABLE OUTCOMES, read back from the database rather than from the
	// response — a response is a claim and the row is the fact.
	for i, m := range memos {
		if i == failing {
			// The failing memo is STILL UNTRIAGED and can be decided again.
			if got := h.state(m.ID); got != store.StateTranscribed {
				t.Fatalf("the failed memo is %q, want it still awaiting a decision", got)
			}
			l := h.link(m.ID)
			if !l.Refused() {
				t.Fatal("the failed decision left no refusal for the operator to read")
			}
			continue
		}
		if got := h.state(m.ID); got != store.StateTriaged {
			t.Fatalf("memo %d is %q, want triaged", i, got)
		}
		if !h.link(m.ID).Confirmed() {
			t.Fatalf("memo %d has no confirmed link", i)
		}
	}
	if got := h.tracker.createdCount(); got != 11 {
		t.Fatalf("%d tickets created, want 11", got)
	}
}

// REPLAYING THE SAME BATCH CHANGES NOTHING. Eleven replay as `applied` with the
// keys they already have and no second ticket exists; the refused one still
// refuses, because an identical resend of a refused decision would replay
// Switchyard's own cached refusal.
func TestReplayingABatchCreatesNothing(t *testing.T) {
	h := newHarness(t)

	var memos []store.Memo
	var items []Item
	for i := 0; i < 3; i++ {
		m := h.ownMemo("replay memo " + string(rune('a'+i)))
		h.propose(m.ID, ticketProposal("CHRN"))
		memos = append(memos, m)
		items = append(items, h.accept(m.ID))
	}

	first := h.apply(h.owner, items...)
	keys := make([]string, len(first))
	for i, r := range first {
		wantStatus(t, r, StatusApplied)
		keys[i] = r.TicketKey
	}
	created := h.tracker.createdCount()

	// The identical batch again. Note the items still carry the generations the
	// client saw, which is the point — nothing about the proposals moved.
	second := h.apply(h.owner, items...)
	for i, r := range second {
		wantStatus(t, r, StatusApplied)
		if r.TicketKey != keys[i] {
			t.Fatalf("item %d replayed as %s, want the original %s", i, r.TicketKey, keys[i])
		}
	}
	if got := h.tracker.createdCount(); got != created {
		t.Fatalf("the replay created %d more tickets", got-created)
	}
	// AND NOT ONE OUTWARD CALL. The link row answered, under lock, before
	// anything could reach the wire.
	if h.tracker.calls != created {
		t.Fatalf("%d create calls for %d tickets — the replay went to the wire", h.tracker.calls, created)
	}
	// Nothing destructive happened either: every memo is still exactly where
	// the first batch left it.
	for i, m := range memos {
		if got := h.state(m.ID); got != store.StateTriaged {
			t.Fatalf("memo %d is %q after the replay, want triaged", i, got)
		}
		if !h.link(m.ID).Confirmed() {
			t.Fatalf("memo %d lost its confirmation to the replay", i)
		}
	}
}

// An item whose payload moved between the GET and the replay is `stale`, and
// the memo is left alone. Distinct from the applied case in the same batch,
// which is what a client needs in order to re-show exactly one card.
func TestAnItemWhosePayloadMovedReplaysAsStale(t *testing.T) {
	h := newHarness(t)
	moved := h.ownMemo("this one changes")
	h.propose(moved.ID, ticketProposal("CHRN"))
	item := h.accept(moved.ID)

	// A second Scribe run supersedes the payload and advances the generation.
	h.propose(moved.ID, ticketProposal("SWY"))

	res := h.apply(h.owner, item)
	wantStatus(t, res[0], StatusStale)
	if res[0].Generation == nil || *res[0].Generation != 2 {
		t.Fatalf("generation = %v, want the server's 2", res[0].Generation)
	}
	if h.tracker.createdCount() != 0 {
		t.Fatal("a stale item reached Switchyard")
	}
	if _, err := h.store.MemoLinkFor(h.ctx, moved.ID); err == nil {
		t.Fatal("a stale item wrote a link row")
	}
	if got := h.state(moved.ID); got != store.StateTranscribed {
		t.Fatalf("memo is %q, want it untouched", got)
	}
}

// A memo already triaged answers `applied` FROM THE LINK ROW, before any
// outward call and whatever the proposal has done since. Checking the
// generation first would report `stale` for a memo that is already decided —
// true about the payload and useless about the memo.
func TestAnAlreadyTriagedMemoAnswersFromTheLinkRow(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("decided yesterday")
	h.propose(m.ID, ticketProposal("CHRN"))

	first := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, first[0], StatusApplied)
	key := first[0].TicketKey
	before := h.tracker.calls

	// The proposal moves AFTER the decision landed, so a generation-first
	// implementation would answer `stale` here.
	h.propose(m.ID, ticketProposal("SWY"))

	again := h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer})
	wantStatus(t, again[0], StatusApplied)
	if again[0].TicketKey != key {
		t.Fatalf("key = %q, want the original %q", again[0].TicketKey, key)
	}
	if again[0].TicketURL == "" {
		t.Fatal("a replayed applied result carries no deep link")
	}
	if h.tracker.calls != before {
		t.Fatal("answering from the link row still went to the wire")
	}
}

// ============================================================================
// The generation echo — what stops an operator committing something they never
// saw.
// ============================================================================

func TestAGenerationMismatchIsStaleAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo")
	h.propose(m.ID, ticketProposal("CHRN"))

	wrong := 99
	res := h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer, Generation: &wrong})
	wantStatus(t, res[0], StatusStale)
	if res[0].Generation == nil || *res[0].Generation != 1 {
		t.Fatalf("generation = %v, want the server's 1", res[0].Generation)
	}
	if h.tracker.createdCount() != 0 {
		t.Fatal("a stale item created a ticket")
	}
}

// EVERY ITEM NAMES ITS PROPOSER, because identity is (memo_id, proposer). An
// item that omitted it would be checking a generation against nothing.
func TestAnItemWithoutAProposerIsRefused(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo")
	h.propose(m.ID, ticketProposal("CHRN"))

	g := 1
	res := h.apply(h.owner, Item{MemoID: m.ID, Generation: &g})
	wantStatus(t, res[0], StatusRefused)
	if !strings.Contains(res[0].Reason, "proposer") {
		t.Fatalf("reason = %q, want it to name the proposer", res[0].Reason)
	}
}

// A memo Scribe has never routed echoes NULL, and null is an ASSERTION rather
// than a missing value: "I saw no proposal". An override against it is honoured.
func TestAMemoWithNoProposalEchoesNull(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("never routed")

	res := h.apply(h.owner, Item{
		MemoID: m.ID, Proposer: testProposer, Generation: nil,
		Override: &Override{Destination: "TICKET", ProjectKey: "CHRN",
			TicketType: "task", Title: "By hand", Description: "typed by a person"},
	})
	wantStatus(t, res[0], StatusApplied)
}

// ...and that assertion is CHECKED. A proposal that appeared between the GET
// and the POST makes "I saw no proposal" false, so the item is `stale` and the
// operator is shown what the model said before deciding.
func TestAProposalThatAppearedSinceTheGetIsStale(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("routed while you were reading")

	// The client's item was composed before this ran.
	h.propose(m.ID, ticketProposal("CHRN"))

	res := h.apply(h.owner, Item{
		MemoID: m.ID, Proposer: testProposer, Generation: nil,
		Override: &Override{Destination: "TICKET", ProjectKey: "CHRN",
			TicketType: "task", Title: "By hand", Description: "typed by a person"},
	})
	wantStatus(t, res[0], StatusStale)
	if res[0].Generation == nil || *res[0].Generation != 1 {
		t.Fatalf("generation = %v, want 1", res[0].Generation)
	}
}

// An `invalid` row HAS a generation — saveFailedProposal inserts 1 — so it
// echoes like any other and needs no special case. Asserted because the
// tempting implementation treats "no payload" as "no proposal" and lets a null
// echo through.
func TestAnInvalidProposalEchoesItsGenerationLikeAnyOther(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("the model produced junk")
	p := h.propose(m.ID, nil)
	if p.Status != scribe.StatusInvalid || p.Generation != 1 {
		t.Fatalf("setup: status=%s generation=%d", p.Status, p.Generation)
	}

	// Echoing null against a row that exists is stale...
	res := h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer, Generation: nil,
		Override: &Override{Destination: "DISCARD"}})
	wantStatus(t, res[0], StatusStale)

	// ...and echoing 1 is honoured.
	one := 1
	res = h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer, Generation: &one,
		Override: &Override{Destination: "DISCARD"}})
	wantStatus(t, res[0], StatusApplied)
}

// There is nothing to accept AS SHOWN for an `invalid` proposal, and saying so
// is not the same as `failed`: no retry will produce one.
func TestAnInvalidProposalCannotBeAcceptedAsShown(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("junk again")
	h.propose(m.ID, nil)

	one := 1
	res := h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer, Generation: &one})
	wantStatus(t, res[0], StatusRefused)
	if !strings.Contains(res[0].Reason, "override") {
		t.Fatalf("reason = %q, want it to name the way out", res[0].Reason)
	}
}

// ============================================================================
// Stage 2, at acceptance.
// ============================================================================

// A proposal made on Tuesday and accepted on Thursday has had two days for its
// project to be archived. Validating only at write time would file a ticket in
// a project that no longer takes them.
func TestAProjectArchivedSinceTheProposalIsNeedsInput(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("about argosy")
	h.propose(m.ID, ticketProposal("ARGY"))
	item := h.accept(m.ID)

	// ARGY is not in the live catalogue.
	res := h.apply(h.owner, item)
	wantStatus(t, res[0], StatusNeedsInput)
	if h.tracker.createdCount() != 0 {
		t.Fatal("a ticket was created in a project that is not live")
	}

	var clearedKey bool
	for _, c := range res[0].Cleared {
		if c.Field == "project_key" && c.Value == "ARGY" {
			clearedKey = true
		}
	}
	if !clearedKey {
		t.Fatalf("cleared = %+v, want project_key ARGY named", res[0].Cleared)
	}

	// THE POST-BUMP GENERATION. The bump has just happened and the operator's
	// next action is an override echoing one — without this, every completion
	// would be `stale` until the client re-ran the GET.
	if res[0].Generation == nil || *res[0].Generation != 2 {
		t.Fatalf("generation = %v, want the post-bump 2", res[0].Generation)
	}

	// And the completion, using exactly what came back, is not stale.
	done := h.apply(h.owner, Item{
		MemoID: m.ID, Proposer: testProposer, Generation: res[0].Generation,
		Override: &Override{Destination: "TICKET", ProjectKey: "CHRN",
			TicketType: "task", Title: "Do the thing", Description: "## Summary"},
	})
	wantStatus(t, done[0], StatusApplied)
}

// STAGE 2 RUNS FOR OVERRIDES TOO. An operator can name an archived project as
// easily as a model can, and `project_key` is immutable after creation.
func TestAnOverrideIsReconciledLikeAnAccept(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("about argosy")
	h.propose(m.ID, ticketProposal("CHRN"))

	res := h.apply(h.owner, h.override(m.ID, Override{
		Destination: "TICKET", ProjectKey: "ARGY", TicketType: "task",
		Title: "Do the thing", Description: "## Summary"}))
	wantStatus(t, res[0], StatusNeedsInput)
	if h.tracker.createdCount() != 0 {
		t.Fatal("an override reached a project that is not live")
	}
	var clearedKey bool
	for _, c := range res[0].Cleared {
		if c.Field == "project_key" && c.Value == "ARGY" {
			clearedKey = true
		}
	}
	if !clearedKey {
		t.Fatalf("cleared = %+v, want the operator's own key named", res[0].Cleared)
	}
}

// AN OVERRIDE MUST NOT OVERWRITE THE MODEL'S PROPOSAL WITH THE OPERATOR'S TEXT.
//
// Stage 2 at acceptance clears a field of whatever it was given, and on the
// override path that is the OPERATOR'S proposal — the stored row was never
// touched. Bumping it with the person's text would replace the model's proposal
// under the model's proposer, so the one screen whose job is to keep those two
// apart would attribute the second to the first, the model's proposal could
// never be accepted as shown again, and CHRN-36 would count an operator's typo
// as a hallucination.
//
// This is invariant 1 in the direction the structural tests do not cover: they
// check that tier 1 never writes tier 2, and this is a tier-2 accept path
// writing authored text into a tier-1 table that is never hand-edited.
func TestAnOverrideNeverOverwritesTheModelsProposal(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("about argosy")
	before := h.propose(m.ID, ticketProposal("CHRN"))

	res := h.apply(h.owner, h.override(m.ID, Override{
		Destination: "TICKET", ProjectKey: "ARGY", TicketType: "task",
		Title: "OPERATOR TYPED THIS", Description: "and this"}))
	wantStatus(t, res[0], StatusNeedsInput)

	after, err := h.tier1.GetProposal(h.ctx, m.ID, testProposer)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	switch {
	case after.Payload == nil:
		t.Fatal("the model's proposal was replaced by one with no payload")
	case after.Payload.Title == "OPERATOR TYPED THIS":
		t.Fatal("the operator's text was written into the model's proposal row")
	case after.Payload.Title != before.Payload.Title:
		t.Fatalf("payload title = %q, want the model's %q", after.Payload.Title, before.Payload.Title)
	case after.Generation != before.Generation:
		t.Fatalf("generation moved %d -> %d for a payload that did not change",
			before.Generation, after.Generation)
	case after.Status != scribe.StatusValid:
		t.Fatalf("status = %q, want the model's proposal still acceptable as shown", after.Status)
	}

	// The echoed generation is the stored one, so the completed resend is still
	// not stale without an intervening GET — which is what the bump bought on
	// the accept path and is true here for free.
	if res[0].Generation == nil || *res[0].Generation != before.Generation {
		t.Fatalf("generation = %v, want the stored %d", res[0].Generation, before.Generation)
	}
	done := h.apply(h.owner, Item{
		MemoID: m.ID, Proposer: testProposer, Generation: res[0].Generation,
		Override: &Override{Destination: "TICKET", ProjectKey: "CHRN",
			TicketType: "task", Title: "Do the thing", Description: "## Summary"},
	})
	wantStatus(t, done[0], StatusApplied)

	// And the model's proposal is still there to accept as shown, had the
	// operator gone that way instead.
	if !h.link(m.ID).Confirmed() {
		t.Fatal("the completed override did not land")
	}
}

// EVERY RESULT NAMES ITS DESTINATION, not only the refusals. A client re-showing
// a `needs_input` or a `stale` card has to be able to label it.
func TestEveryResultCarriesTheDestinationItWasAbout(t *testing.T) {
	h := newHarness(t)

	applied := h.ownMemo("this one lands")
	h.propose(applied.ID, ticketProposal("CHRN"))

	incomplete := h.ownMemo("about argosy")
	h.propose(incomplete.ID, ticketProposal("ARGY"))

	res := h.apply(h.owner, h.accept(applied.ID), h.accept(incomplete.ID))
	wantStatus(t, res[0], StatusApplied)
	wantStatus(t, res[1], StatusNeedsInput)
	for i, r := range res {
		if r.Destination != string(scribe.DestTicket) {
			t.Fatalf("result %d (%s) has destination %q, want TICKET", i, r.Status, r.Destination)
		}
	}
}

// ONE SNAPSHOT FOR THE WHOLE BATCH. Twelve memos accepted in one pass cannot
// disagree about whether a project is live.
func TestTheCatalogueIsFetchedOncePerBatch(t *testing.T) {
	h := newHarness(t)
	var items []Item
	for i := 0; i < 4; i++ {
		m := h.ownMemo("memo " + string(rune('a'+i)))
		h.propose(m.ID, ticketProposal("CHRN"))
		items = append(items, h.accept(m.ID))
	}
	h.apply(h.owner, items...)
	if h.cat.calls != 1 {
		t.Fatalf("the catalogue was fetched %d times for one batch", h.cat.calls)
	}
}

// A catalogue that cannot be read is a PER-ITEM failure. A batch-wide non-2xx
// would make a client retry a set it cannot see into.
func TestAnUnreachableCatalogueFailsPerItem(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo")
	h.propose(m.ID, ticketProposal("CHRN"))
	discard := h.ownMemo("noise")

	h.cat.snap, h.cat.err = nil, context.DeadlineExceeded

	res := h.apply(h.owner,
		h.accept(m.ID),
		// An override to DISCARD carries no catalogue reference at all and must
		// not fail because the project list was briefly unreachable.
		Item{MemoID: discard.ID, Proposer: testProposer, Override: &Override{Destination: "DISCARD"}})

	wantStatus(t, res[0], StatusFailed)
	wantStatus(t, res[1], StatusApplied)
	if h.cat.calls != 1 {
		t.Fatalf("the catalogue was attempted %d times; one failure per batch is enough", h.cat.calls)
	}
}

// ============================================================================
// DISCARD — the one decision that cannot be walked back.
// ============================================================================

// NEVER ACCEPTED AS SHOWN, at any confidence. `discarded` is terminal in
// tier2.memos_guard, and a confident wrong DISCARD is precisely the case that
// clears any threshold.
func TestADiscardIsNeverAcceptedAsShown(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("testing testing one two")
	h.propose(m.ID, &scribe.Proposal{
		Destination: scribe.DestDiscard, Confidence: 1.0,
		Reason: "somebody testing their microphone"})

	res := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, res[0], StatusRefused)
	if !strings.Contains(res[0].Reason, "override") {
		t.Fatalf("reason = %q, want it to name the deliberate tap", res[0].Reason)
	}
	if got := h.state(m.ID); got != store.StateTranscribed {
		t.Fatalf("memo is %q, want it untouched", got)
	}
}

// An OVERRIDE to DISCARD is honoured, writes a link row confirmed with no
// outward call, and replays as `applied` through the same path as every other
// destination — which is what keeps replay one code path.
func TestAnOverrideToDiscardIsHonouredAndReplays(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("testing testing one two")
	h.propose(m.ID, ticketProposal("CHRN"))

	res := h.apply(h.owner, h.override(m.ID, Override{Destination: "DISCARD"}))
	wantStatus(t, res[0], StatusApplied)
	if got := h.state(m.ID); got != store.StateDiscarded {
		t.Fatalf("memo is %q, want discarded", got)
	}
	l := h.link(m.ID)
	if !l.Confirmed() || l.Destination != store.LinkDiscard {
		t.Fatalf("link = %+v, want a confirmed DISCARD", l)
	}
	if l.TicketKey != nil {
		t.Fatal("a DISCARD carries a ticket key")
	}
	if h.tracker.calls != 0 {
		t.Fatal("a DISCARD made an outward call")
	}

	again := h.apply(h.owner, Item{MemoID: m.ID, Proposer: testProposer,
		Override: &Override{Destination: "DISCARD"}})
	wantStatus(t, again[0], StatusApplied)
	if h.tracker.calls != 0 {
		t.Fatal("replaying a DISCARD made an outward call")
	}
}

// ============================================================================
// What cannot land yet.
// ============================================================================

// NOTE and DISCUSSION need E5's page tree. REFUSED, not `failed`: a client that
// retried would get the same answer every evening until CHRN-37 ships.
func TestNoteAndDiscussionAreRefusedUntilTheyHaveSomewhereToLand(t *testing.T) {
	h := newHarness(t)
	for _, dest := range []string{"NOTE", "DISCUSSION"} {
		m := h.ownMemo("a thought about " + dest)
		h.propose(m.ID, ticketProposal("CHRN"))

		o := Override{Destination: dest, Title: "A thought"}
		res := h.apply(h.owner, h.override(m.ID, o))
		wantStatus(t, res[0], StatusRefused)
		if !strings.Contains(res[0].Reason, "CHRN-37") {
			t.Fatalf("%s: reason = %q, want it to name the ticket that unblocks it", dest, res[0].Reason)
		}
		if got := h.state(m.ID); got != store.StateTranscribed {
			t.Fatalf("%s: memo is %q, want it untouched", dest, got)
		}
		if _, err := h.store.MemoLinkFor(h.ctx, m.ID); err == nil {
			t.Fatalf("%s: a refusal before T1 left a row behind", dest)
		}
	}
}

// ============================================================================
// The memo moved.
// ============================================================================

// A memo put on hold between the GET and the POST is REFUSED, checked under
// lock BEFORE the outward call, and it leaves NO PENDING ROW.
//
// The ordering is the whole point: checked afterwards, the memo would get a
// real ticket and then fail the advance — there is no `held > triaged` edge —
// and T2 would roll back with the ticket standing behind it.
func TestAMemoHeldBetweenTheGetAndThePostIsRefused(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("second thoughts")
	h.propose(m.ID, ticketProposal("CHRN"))
	item := h.accept(m.ID)

	h.hold(m.ID)

	res := h.apply(h.owner, item)
	wantStatus(t, res[0], StatusRefused)
	if !strings.Contains(res[0].Reason, "CHRN-34") {
		t.Fatalf("reason = %q, want it to name the hold inbox", res[0].Reason)
	}
	if h.tracker.calls != 0 {
		t.Fatal("a held memo reached Switchyard")
	}
	if _, err := h.store.MemoLinkFor(h.ctx, m.ID); err == nil {
		t.Fatal("a held memo left a pending row behind")
	}
	if got := h.state(m.ID); got != store.StateHeld {
		t.Fatalf("memo is %q, want it still held", got)
	}
}

// ============================================================================
// Refused is not failed.
// ============================================================================

// A NON-RETRYABLE OUTWARD ERROR MARKS THE ROW. It does not delete it: the
// operator is owed an account of why their decision evaporated, and the row is
// the only thing that can give them one.
func TestANonRetryableRefusalMarksTheRowRatherThanDeletingIt(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("about argosy")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.tracker.createErr = httpError(404, "project CHRN is archived")

	res := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, res[0], StatusRefused)

	l := h.link(m.ID)
	switch {
	case !l.Refused():
		t.Fatal("the row was not marked refused")
	case l.RefusedStatus == nil || *l.RefusedStatus != 404:
		t.Fatalf("refused_status = %v, want 404", l.RefusedStatus)
	case !strings.Contains(l.RefusedReason, "archived"):
		t.Fatalf("refused_reason = %q, want Switchyard's explanation", l.RefusedReason)
	}
	if got := h.state(m.ID); got != store.StateTranscribed {
		t.Fatalf("memo is %q, want it still decidable", got)
	}

	// AND THE SWEEP SKIPS IT. A refused row is not pending, so nothing retries
	// a decision that will refuse identically forever.
	h.tracker.createErr = nil
	rep, err := h.svc.Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Examined != 0 {
		t.Fatalf("the sweep examined %d refused rows", rep.Examined)
	}
	if h.tracker.createdCount() != 0 {
		t.Fatal("the sweep created a ticket for a refused decision")
	}
}

// A RETRYABLE error keeps the row pending and answers `failed`, because a
// ticket may or may not exist behind it and only the sweep can find out.
// Deleting it would be this path asserting there is no ticket.
func TestARetryableErrorKeepsTheRowPending(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.tracker.createErr = httpError(503, "upstream unavailable")

	res := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, res[0], StatusFailed)

	l := h.link(m.ID)
	if !l.Pending() {
		t.Fatalf("link = %+v, want it still pending", l)
	}
	if got := h.state(m.ID); got != store.StateTranscribed {
		t.Fatalf("memo is %q, want it untouched", got)
	}
}

// A REFUSAL DOES NOT POISON THE MEMO FOR TWENTY-FOUR HOURS.
//
// Switchyard caches every sub-500 JSON response — that 404 included — under the
// key it was sent with. A memo-derived key would replay it for every corrected
// decision the operator made for the rest of the day. This passes ONLY because
// the key belongs to the decision and a re-armed row gets a fresh one.
func TestACorrectedDecisionReachesSwitchyardAfterARefusal(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("about argosy")
	h.propose(m.ID, ticketProposal("ARGY"))

	// First: an override into a project that refuses.
	h.tracker.createErr = httpError(404, "project SWY is archived")
	first := h.apply(h.owner, h.override(m.ID, Override{
		Destination: "TICKET", ProjectKey: "SWY", TicketType: "task",
		Title: "Do the thing", Description: "## Summary"}))
	wantStatus(t, first[0], StatusRefused)
	refusedKey := h.link(m.ID).SentIdempotencyKey

	// The operator corrects the project. Switchyard works again for CHRN.
	h.tracker.createErr = nil
	second := h.apply(h.owner, h.override(m.ID, Override{
		Destination: "TICKET", ProjectKey: "CHRN", TicketType: "task",
		Title: "Do the thing", Description: "## Summary"}))
	wantStatus(t, second[0], StatusApplied)
	if second[0].TicketKey == "" {
		t.Fatal("the corrected decision produced no ticket")
	}
	if got := h.link(m.ID).SentIdempotencyKey; got == refusedKey {
		t.Fatalf("the corrected decision re-sent the refused key %q", got)
	}
	if got := h.state(m.ID); got != store.StateTriaged {
		t.Fatalf("memo is %q, want triaged", got)
	}
}

// An IDENTICAL resend of a refused decision is refused again, with the status
// that refused it — Switchyard has the same answer cached under the same key,
// and saying so beats replaying it.
func TestAnIdenticalResendOfARefusedDecisionIsRefusedWithItsStatus(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("a memo")
	h.propose(m.ID, ticketProposal("CHRN"))
	h.tracker.createErr = httpError(422, "title is too long")

	first := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, first[0], StatusRefused)
	calls := h.tracker.calls

	h.tracker.createErr = nil
	again := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, again[0], StatusRefused)
	if !strings.Contains(again[0].Reason, "422") {
		t.Fatalf("reason = %q, want the status that refused it", again[0].Reason)
	}
	if h.tracker.calls != calls {
		t.Fatal("an identical resend went back to the wire")
	}
}

// ============================================================================
// The lock.
// ============================================================================

// TWO CONCURRENT ACCEPTS OF ONE MEMO PRODUCE EXACTLY ONE TICKET, against a stub
// that does NOT serialise — because the real Switchyard does not. The header
// replays a response; it does not serialise a side effect. Only the pending row
// prevents this.
func TestTwoConcurrentAcceptsCreateOneTicket(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("decided twice at once")
	h.propose(m.ID, ticketProposal("CHRN"))

	// A real overlap: the handler is slow enough that the second caller is
	// genuinely inside the window the real middleware leaves open.
	h.tracker.handlerDelay = 200 * time.Millisecond

	var wg sync.WaitGroup
	out := make([][]Result, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := h.svc.Apply(h.ctx, h.owner, []Item{h.accept(m.ID)})
			if err != nil {
				t.Errorf("Apply: %v", err)
				return
			}
			out[i] = r
		}(i)
	}
	wg.Wait()

	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets created for one memo — the lock did not hold", got)
	}
	var applied int
	for _, r := range out {
		if len(r) == 1 && r[0].applied() {
			applied++
		}
	}
	if applied == 0 {
		t.Fatalf("neither caller was told the decision landed: %+v", out)
	}
	if !h.link(m.ID).Confirmed() {
		t.Fatal("no confirmed link after two concurrent accepts")
	}
}

// ============================================================================
// The batch is not one transaction.
// ============================================================================

// Failing item 7 of 12 leaves items 1–6 DURABLE. Asserted from the database
// after the call returns, because the failure mode this rules out is a
// batch-wide transaction that rolls six real decisions back.
func TestFailingItemSevenLeavesTheFirstSixDurable(t *testing.T) {
	h := newHarness(t)
	memos := make([]store.Memo, 12)
	items := make([]Item, 12)
	for i := range memos {
		memos[i] = h.ownMemo("batch memo " + string(rune('a'+i)))
		h.propose(memos[i].ID, ticketProposal("CHRN"))
		items[i] = h.accept(memos[i].ID)
	}
	h.svc.beforeCreate = func(memoID uuid.UUID) error {
		if memoID == memos[6].ID {
			h.tracker.createErr = httpError(400, "malformed")
		} else {
			h.tracker.createErr = nil
		}
		return nil
	}

	res := h.apply(h.owner, items...)
	wantStatus(t, res[6], StatusRefused)
	for i := 0; i < 6; i++ {
		if !h.link(memos[i].ID).Confirmed() {
			t.Fatalf("item %d was rolled back by item 7", i)
		}
		if got := h.state(memos[i].ID); got != store.StateTriaged {
			t.Fatalf("item %d is %q, want triaged", i, got)
		}
	}
}

// ============================================================================
// Author scoping — on BOTH endpoints.
// ============================================================================

// A member naming another member's memo is refused BY THE POST, not merely
// hidden by the GET. Hiding a memo from a list is not access control: a client
// that names an id directly never went through the list.
func TestAMemberCannotDecideAnotherMembersMemo(t *testing.T) {
	h := newHarness(t)
	alice := h.user("alice@example.test")
	bob := h.user("bob@example.test")

	hers := h.memo(alice.ID, "alice's memo")
	h.propose(hers.ID, ticketProposal("CHRN"))

	// Bob's GET does not show it...
	items, err := h.svc.Batch(h.ctx, bob, DefaultLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	for _, it := range items {
		if it.MemoID == hers.ID {
			t.Fatal("the GET showed another member's memo")
		}
	}

	// ...and neither does naming it directly work.
	res := h.apply(bob, h.accept(hers.ID))
	wantStatus(t, res[0], StatusRefused)
	if h.tracker.createdCount() != 0 {
		t.Fatal("a member decided another member's memo")
	}

	// The refusal must not distinguish "not yours" from "does not exist", on
	// the pattern the credential lookups already set: a probe must not be able
	// to enumerate the corpus.
	missing := h.apply(bob, Item{MemoID: uuid.New(), Proposer: testProposer})
	if missing[0].Reason != res[0].Reason {
		t.Fatalf("a memo that exists refuses differently (%q) from one that does not (%q)",
			res[0].Reason, missing[0].Reason)
	}

	// The owner is unrestricted.
	owned := h.apply(h.owner, h.accept(hers.ID))
	wantStatus(t, owned[0], StatusApplied)
}

// ============================================================================
// Size, and a client that hangs up.
// ============================================================================

func TestABatchLargerThanAScreenIsRefused(t *testing.T) {
	h := newHarness(t)
	items := make([]Item, MaxLimit+1)
	_, err := h.svc.Apply(h.ctx, h.owner, items)
	if err == nil {
		t.Fatal("an oversized batch was accepted")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Fatalf("err = %v, want it to name the cap", err)
	}
}

// A CLIENT THAT DROPS MID-BATCH LOSES NOTHING THAT STARTED. The item in flight
// runs to completion on its own context; only items not yet started are
// skipped, and the results returned describe exactly what was attempted.
func TestAClientDroppingMidBatchStillFinishesTheItemInFlight(t *testing.T) {
	h := newHarness(t)
	var memos []store.Memo
	var items []Item
	for i := 0; i < 3; i++ {
		m := h.ownMemo("drop memo " + string(rune('a'+i)))
		h.propose(m.ID, ticketProposal("CHRN"))
		memos = append(memos, m)
		items = append(items, h.accept(m.ID))
	}

	ctx, cancel := context.WithCancel(h.ctx)
	// The client hangs up the instant the first ticket is created — while that
	// item is still mid-confirm.
	h.tracker.onCreate = func() { cancel() }

	res, err := h.svc.Apply(ctx, h.owner, items)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("got %d results, want only the attempted one", len(res))
	}
	wantStatus(t, res[0], StatusApplied)

	// The first item is fully durable despite the cancellation.
	if !h.link(memos[0].ID).Confirmed() {
		t.Fatal("the in-flight item was abandoned by the cancellation")
	}
	if got := h.state(memos[0].ID); got != store.StateTriaged {
		t.Fatalf("memo is %q, want triaged", got)
	}
	// And the rest were never touched.
	for _, m := range memos[1:] {
		if _, err := h.store.MemoLinkFor(h.ctx, m.ID); err == nil {
			t.Fatal("an unattempted item wrote a row")
		}
	}
}

// ============================================================================
// Logging.
// ============================================================================

// NO LOG LINE CARRIES AUTHORED TEXT. Not the transcript, not a note body, not a
// ticket description, and NOT A TICKET TITLE — the title is the field most
// likely to be logged out of habit, and it is authored-derived text about
// somebody's unfinished thinking.
func TestTheBatchPathNeverLogsAuthoredText(t *testing.T) {
	h := newHarness(t)

	const transcript = "SECRETTRANSCRIPT the thing I said out loud in the car"
	const title = "SECRETTITLE do the thing"
	const description = "SECRETDESCRIPTION the whole plan"

	var buf bytes.Buffer
	h.svc.logger = slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ok := h.ownMemo(transcript)
	p := ticketProposal("CHRN")
	p.Title, p.Description = title, description
	h.propose(ok.ID, p)

	// A failing item too, because the per-item failure line is the one most
	// likely to reach for context it should not have.
	bad := h.ownMemo(transcript + " again")
	badP := ticketProposal("CHRN")
	badP.Title, badP.Description = title, description
	h.propose(bad.ID, badP)
	h.svc.beforeCreate = func(memoID uuid.UUID) error {
		if memoID == bad.ID {
			h.tracker.createErr = httpError(500, "boom "+title)
		} else {
			h.tracker.createErr = nil
		}
		return nil
	}

	h.apply(h.owner, h.accept(ok.ID), h.accept(bad.ID))

	logged := buf.String()
	if logged == "" {
		t.Fatal("the batch logged nothing at all, so this proves nothing")
	}
	for _, secret := range []string{"SECRETTRANSCRIPT", "SECRETTITLE", "SECRETDESCRIPTION"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("a log line carries %s:\n%s", secret, logged)
		}
	}
	// It does say what happened, or it would be useless.
	var sawCounts bool
	for _, line := range strings.Split(strings.TrimSpace(logged), "\n") {
		var rec map[string]any
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec["msg"] == "triage batch applied" {
			sawCounts = true
		}
	}
	if !sawCounts {
		t.Fatalf("no per-batch counts were logged:\n%s", logged)
	}
}

// A CORRECTION ABANDONED BY A HOLD IS MARKED, NOT DELETED.
//
// The memo was refused, the operator corrected it, and the memo was held in
// between. The fresh-claim case drops its row — nothing was ever attempted for
// that memo — but this row carries the record of the decision the correction
// was correcting. Dropping it would leave the memo waiting in the morning with
// no account of either attempt, which is the exact failure "a refusal marks, it
// does not delete" exists to prevent.
func TestACorrectionAbandonedByAHoldKeepsItsRecord(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("refused, corrected, then held")
	h.propose(m.ID, ticketProposal("CHRN"))

	h.tracker.createErr = httpError(404, "project CHRN is archived")
	first := h.apply(h.owner, h.accept(m.ID))
	wantStatus(t, first[0], StatusRefused)
	h.tracker.createErr = nil

	// The operator changes their mind and holds the memo before the correction
	// reaches the server.
	h.hold(m.ID)

	corrected := h.apply(h.owner, h.override(m.ID, Override{
		Destination: "TICKET", ProjectKey: "SWY", TicketType: "task",
		Title: "Do the thing", Description: "## Summary"}))
	wantStatus(t, corrected[0], StatusRefused)
	if !strings.Contains(corrected[0].Reason, "CHRN-34") {
		t.Fatalf("reason = %q, want it to name the hold inbox", corrected[0].Reason)
	}
	if h.tracker.createdCount() != 0 {
		t.Fatal("a held memo reached Switchyard")
	}

	// THE ROW SURVIVES, marked. A dropped row would leave nothing to show.
	l, err := h.store.MemoLinkFor(h.ctx, m.ID)
	if err != nil {
		t.Fatalf("the correction's row was deleted along with the history it carried: %v", err)
	}
	if !l.Refused() {
		t.Fatalf("link = %+v, want it marked refused rather than left pending", l)
	}
	if !strings.Contains(l.RefusedReason, "CHRN-34") {
		t.Fatalf("refused_reason = %q", l.RefusedReason)
	}

	// And the sweep leaves it alone rather than creating for a held memo.
	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Examined != 0 || h.tracker.createdCount() != 0 {
		t.Fatalf("report = %+v, want the refused row skipped", rep)
	}
	if got := h.state(m.ID); got != store.StateHeld {
		t.Fatalf("memo is %q, want it still held", got)
	}
}

// A FIRST DECISION ABANDONED BY A HOLD LEAVES NOTHING BEHIND. The other half of
// the case above, asserted beside it so the asymmetry is deliberate rather than
// accidental.
func TestAFirstDecisionAbandonedByAHoldLeavesNoRow(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("held before anything was attempted")
	h.propose(m.ID, ticketProposal("CHRN"))
	item := h.accept(m.ID)
	h.hold(m.ID)

	res := h.apply(h.owner, item)
	wantStatus(t, res[0], StatusRefused)
	if _, err := h.store.MemoLinkFor(h.ctx, m.ID); err == nil {
		t.Fatal("a decision that never reached the wire left a row behind")
	}
}

// THE WAITER HAS ITS OWN DEADLINE. T2 pins a pool connection across a
// 15-second outward call, and without a lock_timeout on the waiting side one
// stuck Switchyard call would queue every retry of that memo behind it, each
// holding a connection while it waited. The waiter gives up and answers
// `failed` — transient by construction, because the holder is bounded.
func TestAWaiterGivesUpRatherThanQueueingBehindAStuckCall(t *testing.T) {
	h := newHarness(t)
	m := h.ownMemo("held open by somebody else")
	h.propose(m.ID, ticketProposal("CHRN"))

	// Shortened so this is a fast test rather than a five-second one; the
	// behaviour under test is the deadline existing at all.
	restore := store.SetLinkLockTimeoutForTest("100ms")
	defer restore()

	inT2 := make(chan struct{})
	release := make(chan struct{})
	h.svc.beforeCreate = func(uuid.UUID) error {
		close(inT2)
		<-release
		return nil
	}
	holder := make(chan []Result, 1)
	go func() {
		r, err := h.svc.Apply(h.ctx, h.owner, []Item{h.accept(m.ID)})
		if err != nil {
			t.Errorf("holder Apply: %v", err)
		}
		holder <- r
	}()
	<-inT2

	waiter := h.sweeper()
	res, err := waiter.Apply(h.ctx, h.owner, []Item{h.accept(m.ID)})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantStatus(t, res[0], StatusFailed)
	if !strings.Contains(res[0].Reason, "in flight") {
		t.Fatalf("reason = %q, want it to say another decision holds the memo", res[0].Reason)
	}

	// The holder was never disturbed: it finishes normally, and its decision is
	// the only one that landed.
	close(release)
	held := <-holder
	wantStatus(t, held[0], StatusApplied)
	if got := h.tracker.createdCount(); got != 1 {
		t.Fatalf("%d tickets created, want exactly one", got)
	}
}
