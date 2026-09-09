package triage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-95 — NOTE and DISCUSSION land through triage.

// withCorpus gives the harness a catalogue that has a page tree and, if asked,
// notes. liveCatalogue deliberately has neither: before this ticket nothing
// could land, so an empty tree was the correct state rather than a gap.
func withCorpus(t *testing.T, h *harness, pages []string, notes []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1\nprojects:\n  - key: CHRN\n    name: Project CHRN\n    description: the CHRN project\n")
	b.WriteString("pages:\n")
	for _, p := range pages {
		fmt.Fprintf(&b, "  - %s\n", p)
	}
	if len(notes) > 0 {
		b.WriteString("notes:\n")
		for _, n := range notes {
			fmt.Fprintf(&b, "  - %s\n", n)
		}
	}
	snap, err := catalogue.Parse([]byte(b.String()))
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	h.cat.snap = snap
}

func noteProposal(verb scribe.Verb, page string, target string) *scribe.Proposal {
	p := &scribe.Proposal{
		Destination: scribe.DestNote, Confidence: 0.95,
		Reason: "it argues a principle", Title: "A landed note",
		Verb: verb, Body: "the body of the note",
	}
	if page != "" {
		p.PagePath = &page
	}
	if target != "" {
		p.TargetNote = &target
	}
	return p
}

// The refusal this ticket deletes named a ticket that had already shipped. A
// NOTE now lands, and the result says what it became rather than only that it
// was decided.
func TestANoteLandsAndTheResultCarriesItsHandle(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)
	memo := h.ownMemo("a thought about disposability")
	h.propose(memo.ID, noteProposal(scribe.VerbCreate, "estate", ""))

	res := h.apply(h.owner, h.accept(memo.ID))[0]
	if res.Status != StatusApplied {
		t.Fatalf("status %q (%s), want applied", res.Status, res.Reason)
	}
	if res.NoteRef == "" {
		t.Error("an applied NOTE carried no handle — the operator is told it was decided and not what it became")
	}
	if !strings.HasPrefix(res.NoteRef, "CHR-") {
		t.Errorf("note_ref %q", res.NoteRef)
	}
	if res.TicketKey != "" {
		t.Errorf("a NOTE result carried a ticket key %q", res.TicketKey)
	}
	if h.state(memo.ID) != store.StateTriaged {
		t.Errorf("memo state %q, want triaged", h.state(memo.ID))
	}
	if l := h.link(memo.ID); l.NoteID == nil {
		t.Error("the link records no note")
	}
	if h.tracker.calls != 0 {
		t.Errorf("a NOTE landing made %d Switchyard calls", h.tracker.calls)
	}
}

// RULINGS 1 AND 2 — append and supersede are never part of ACCEPT ALL, and the
// server hears the deliberate tap rather than trusting the client not to
// pre-select them.
func TestAnAppendIsNeverPartOfAcceptAll(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, []string{"CHR-0001"})

	author := h.owner.ID
	page := h.mkPage(t, "estate")
	target := h.mkNote(t, page, author, "The original", "first pass")
	ref := store.FormatNoteRef(target.Number)
	withCorpus(t, h, []string{"estate"}, []string{ref})

	t.Run("as shown, it is refused and the reason names the verb", func(t *testing.T) {
		memo := h.ownMemo("more on the original")
		h.propose(memo.ID, noteProposal(scribe.VerbAppend, "", ref))

		res := h.apply(h.owner, h.accept(memo.ID))[0]
		if res.Status != StatusRefused {
			t.Fatalf("status %q, want refused", res.Status)
		}
		if !strings.Contains(res.Reason, "append") {
			t.Errorf("reason %q does not name the verb", res.Reason)
		}
		if h.state(memo.ID) == store.StateTriaged {
			t.Error("a refused append advanced the memo")
		}
	})

	t.Run("with the affirmative, the same item lands AS SHOWN", func(t *testing.T) {
		memo := h.ownMemo("more on the original, confirmed")
		h.propose(memo.ID, noteProposal(scribe.VerbAppend, "", ref))

		it := h.accept(memo.ID)
		it.ConfirmEdit = true
		res := h.apply(h.owner, it)[0]
		if res.Status != StatusApplied {
			t.Fatalf("status %q (%s), want applied", res.Status, res.Reason)
		}
		// AS SHOWN is the point: the model's text landed, not the operator's
		// retyping of it.
		rev, err := h.store.CurrentRevision(h.ctx, target.ID)
		if err != nil {
			t.Fatalf("CurrentRevision: %v", err)
		}
		if !strings.Contains(rev.Body, "the body of the note") {
			t.Errorf("body %q does not carry the proposal's text", rev.Body)
		}
	})

	t.Run("the affirmative is legal on nothing else", func(t *testing.T) {
		memo := h.ownMemo("a brand new thought")
		h.propose(memo.ID, noteProposal(scribe.VerbCreate, "estate", ""))

		it := h.accept(memo.ID)
		it.ConfirmEdit = true
		res := h.apply(h.owner, it)[0]
		if res.Status != StatusRefused {
			t.Fatalf("status %q, want refused — confirm_edit confirms nothing on a create", res.Status)
		}
		if !strings.Contains(res.Reason, "confirm_edit") {
			t.Errorf("reason %q", res.Reason)
		}
	})
}

// RULING 6 — relate produces an actual edge, and the reference is appended only
// when the body does not already carry it.
func TestARelateProducesTheLinkItsNameClaims(t *testing.T) {
	h := newHarness(t)
	page := h.mkPage(t, "estate")
	target := h.mkNote(t, page, h.owner.ID, "The smart calendar", "the first pass")
	ref := store.FormatNoteRef(target.Number)
	withCorpus(t, h, []string{"estate"}, []string{ref})

	t.Run("a body that does not mention the target gains the footer", func(t *testing.T) {
		memo := h.ownMemo("an add-on to the calendar thing")
		p := noteProposal(scribe.VerbRelate, "estate", ref)
		p.Body = "a distinct idea that belongs near the other one"
		h.propose(memo.ID, p)

		res := h.apply(h.owner, h.accept(memo.ID))[0]
		if res.Status != StatusApplied {
			t.Fatalf("status %q (%s)", res.Status, res.Reason)
		}

		// ASSERTED THROUGH Backlinks, not by reading the body: the link is what
		// the reference line exists for, and the body is only how it gets there.
		back, err := h.store.Backlinks(h.ctx, target.Number)
		if err != nil {
			t.Fatalf("Backlinks: %v", err)
		}
		if len(back) != 1 {
			t.Fatalf("%d backlinks, want 1 — relate produced a note that relates to nothing", len(back))
		}
	})

	t.Run("a body that already mentions it is left alone", func(t *testing.T) {
		memo := h.ownMemo("an add-on, and the model said so")
		p := noteProposal(scribe.VerbRelate, "estate", ref)
		p.Body = "this builds on " + ref + " and goes further"
		h.propose(memo.ID, p)

		res := h.apply(h.owner, h.accept(memo.ID))[0]
		if res.Status != StatusApplied {
			t.Fatalf("status %q (%s)", res.Status, res.Reason)
		}

		n, err := h.store.NoteByNumber(h.ctx, mustNumber(t, res.NoteRef))
		if err != nil {
			t.Fatalf("NoteByNumber: %v", err)
		}
		rev, err := h.store.CurrentRevision(h.ctx, n.ID)
		if err != nil {
			t.Fatalf("CurrentRevision: %v", err)
		}
		if got := strings.Count(rev.Body, ref); got != 1 {
			t.Errorf("the reference appears %d times in %q, want 1", got, rev.Body)
		}
		back, err := h.store.Backlinks(h.ctx, target.Number)
		if err != nil {
			t.Fatalf("Backlinks: %v", err)
		}
		if len(back) != 2 {
			t.Fatalf("%d backlinks after two relates, want 2", len(back))
		}
	})
}

// RULING 7 — a create with no page is cleared into needs_input, and the
// operator can finish it. Both halves matter: before this ticket the override
// had no field to carry a page in, so the needs_input loop was a dead end for
// exactly the field stage 2 clears most.
func TestANoteWithNoPageIsCompletableRatherThanRefused(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)
	memo := h.ownMemo("a thought with nowhere to go")
	h.propose(memo.ID, noteProposal(scribe.VerbCreate, "", ""))

	res := h.apply(h.owner, h.accept(memo.ID))[0]
	if res.Status != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input", res.Status)
	}
	var named bool
	for _, c := range res.Cleared {
		if c.Field == "page_path" {
			named = true
		}
	}
	if !named {
		t.Errorf("nothing named page_path: %+v", res.Cleared)
	}
	if res.Generation == nil {
		t.Error("no generation to echo on the resend")
	}
	if l, err := h.store.MemoLinkFor(h.ctx, memo.ID); err == nil {
		t.Errorf("a proposal that needs input claimed a link row: %+v", l)
	}

	// The operator supplies it, and the same memo lands.
	it := h.override(memo.ID, Override{
		Destination: "NOTE", Title: "A thought", Verb: "create",
		PagePath: "estate", Body: "the body",
	})
	if res.Generation != nil {
		g := *res.Generation
		it.Generation = &g
	}
	out := h.apply(h.owner, it)[0]
	if out.Status != StatusApplied {
		t.Fatalf("override status %q (%s), want applied", out.Status, out.Reason)
	}
	if out.NoteRef == "" {
		t.Error("the completed override reported no handle")
	}
}

// A PERSON'S DECISION IS VALIDATED BY THE MODEL'S OWN VALIDATOR — literally the
// same code, so the two cannot drift as the contract grows.
func TestAnOverrideIsHeldToTheSameContractAsTheModel(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)

	t.Run("a NOTE override with an append and no target", func(t *testing.T) {
		memo := h.ownMemo("an override with a hole in it")
		h.propose(memo.ID, noteProposal(scribe.VerbCreate, "estate", ""))

		res := h.apply(h.owner, h.override(memo.ID, Override{
			Destination: "NOTE", Title: "Appending to nothing",
			Verb: "append", Body: "text", PagePath: "estate",
		}))[0]
		if res.Status != StatusRefused {
			t.Fatalf("status %q, want refused", res.Status)
		}

		// THE IDENTICAL MESSAGE. Compared against what Parse says to a model
		// rather than matched on a substring, because the whole claim is that
		// there is one validator and not two.
		_, want := scribe.Parse([]byte(`{"destination":"NOTE","confidence":1,"reason":"decided by the operator",
			"title":"Appending to nothing","nearest_page":null,"page_path":"estate",
			"verb":"append","body":"text"}`))
		if want == nil {
			t.Fatal("the model's validator accepted an append with no target")
		}
		if res.Reason != want.Error() {
			t.Errorf("override said\n  %q\nthe model is told\n  %q", res.Reason, want.Error())
		}
	})

	t.Run("a DISCUSSION override with no opening post", func(t *testing.T) {
		memo := h.ownMemo("a thread with nothing in it")
		h.propose(memo.ID, noteProposal(scribe.VerbCreate, "estate", ""))

		res := h.apply(h.owner, h.override(memo.ID, Override{
			Destination: "DISCUSSION", Title: "An empty thread",
		}))[0]
		if res.Status != StatusRefused {
			t.Fatalf("status %q, want refused — a thread cannot open with an empty turn 1", res.Status)
		}
		if !strings.Contains(res.Reason, "opening_post") {
			t.Errorf("reason %q does not name the missing field", res.Reason)
		}
	})
}

// A DISCUSSION opens a NEW thread whose turn 1 is the memo's author.
func TestADiscussionLandsAsAThreadTheMemosAuthorOpened(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)
	member := h.user("member@example.com")
	memo := h.memo(member.ID, "something worth arguing about")

	p := &scribe.Proposal{
		Destination: scribe.DestDiscussion, Confidence: 0.9,
		Reason: "it is a question rather than a claim", Title: "Worth discussing",
		OpeningPost: "here is the question",
	}
	h.propose(memo.ID, p)

	// The OWNER accepts a MEMBER's memo, so the confirmer and the author are
	// demonstrably different people.
	res := h.apply(h.owner, h.accept(memo.ID))[0]
	if res.Status != StatusApplied {
		t.Fatalf("status %q (%s)", res.Status, res.Reason)
	}
	if res.DiscussionRef == "" {
		t.Error("an applied DISCUSSION carried no handle")
	}

	l := h.link(memo.ID)
	if l.DiscussionID == nil {
		t.Fatal("the link records no thread")
	}
	turns, err := h.store.Turns(h.ctx, *l.DiscussionID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(turns) != 1 || turns[0].Seq != 1 {
		t.Fatalf("%d turns, want one at seq 1", len(turns))
	}
	if turns[0].AuthorID != member.ID {
		t.Errorf("turn 1 authored by %v, want the MEMO'S author %v", turns[0].AuthorID, member.ID)
	}
	if turns[0].AuthorID == h.owner.ID {
		t.Error("the deciding actor authored the opening post")
	}
	if l.ConfirmedBy == nil || *l.ConfirmedBy != h.owner.ID {
		t.Errorf("link confirmed_by %v, want the deciding actor", l.ConfirmedBy)
	}
}

// An agent is refused with a reason that names the rule, rather than reaching
// the trigger and being reported as a transient failure the client should
// retry.
func TestAnAgentMayNotConfirmALanding(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)
	scribeAcct, err := h.store.CreateUser(h.ctx, "scribe@example.com", "Scribe", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// THE MEMO IS THE AGENT'S OWN, so canDecide admits it and nothing but the
	// kind check can be doing the refusing. An agent can never be an admin —
	// IsAdmin requires kind 'person' — so this is the only shape in which an
	// agent reaches the landing at all, and CHRN-67 is the route that would
	// produce it.
	memo := h.memo(scribeAcct.ID, "a note an agent would like to accept")
	h.propose(memo.ID, noteProposal(scribe.VerbCreate, "estate", ""))

	res := h.apply(scribeAcct, h.accept(memo.ID))[0]
	if res.Status != StatusRefused {
		t.Fatalf("status %q, want refused (not failed — a retry can never succeed)", res.Status)
	}
	if !strings.Contains(res.Reason, "person") {
		t.Errorf("reason %q does not name the rule", res.Reason)
	}
}

// mustNumber parses CHR-0311 back to 311 for a test that has only the handle.
func mustNumber(t *testing.T, ref string) int64 {
	t.Helper()
	n, err := store.ParseNoteRef(ref)
	if err != nil {
		t.Fatalf("ParseNoteRef(%q): %v", ref, err)
	}
	return n
}

// mkPage and mkNote seed the corpus a landing acts on. The harness has no such
// helpers because before this ticket nothing here wrote notes.
func (h *harness) mkPage(t *testing.T, slug string) store.Page {
	t.Helper()
	p, err := h.store.CreatePage(h.ctx, nil, slug)
	if err != nil {
		t.Fatalf("CreatePage(%q): %v", slug, err)
	}
	return p
}

func (h *harness) mkNote(t *testing.T, page store.Page, author uuid.UUID, title, body string) store.Note {
	t.Helper()
	n, _, err := h.store.CreateNote(h.ctx, store.NewNote{
		PageID: page.ID, AuthorID: author, ConfirmedBy: author, Title: title, Body: body,
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return n
}

// A PENDING LOCAL ROW CANNOT COME FROM THE LANDING PATH ANY MORE, so the sweep
// arm that answers one is reachable only by producing the row the way a build
// before ruling 3 would have — claim it, and stop.
//
// The reason it gives has to change with the code: the old one named a ticket
// that has shipped, and it was the same string triage.go carried. The honest
// answer for a landing with no outward call is that nothing was written
// anywhere, so nothing is lost by deciding again.
func TestAPendingLocalRowIsSweptWithAnHonestReason(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)
	memo := h.ownMemo("a landing that died halfway")

	if _, _, err := h.store.ClaimMemoLink(h.ctx, store.Decision{
		MemoID: memo.ID, Destination: store.LinkNote, Title: "Half-landed",
		IdempotencyKey: "chronicle-decision-orphan",
	}); err != nil {
		t.Fatalf("ClaimMemoLink: %v", err)
	}

	rep, err := h.sweeper().Sweep(h.ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if rep.Refused != 1 {
		t.Fatalf("refused %d, want 1 (examined %d)", rep.Refused, rep.Examined)
	}

	l := h.link(memo.ID)
	if l.RefusedAt == nil {
		t.Fatal("the row is still pending")
	}
	if strings.Contains(l.RefusedReason, "CHRN-37") {
		t.Errorf("the sweep still cites a ticket that shipped: %q", l.RefusedReason)
	}
	if !strings.Contains(l.RefusedReason, "nothing was written") {
		t.Errorf("reason %q does not say what actually happened", l.RefusedReason)
	}
	if h.tracker.searches != 0 {
		t.Errorf("the sweep searched Switchyard for a NOTE %d times", h.tracker.searches)
	}
}

// The batch GET renders a landed note's handle. Before this a confirmed NOTE
// link showed a destination and nothing else.
func TestTheBatchGETRendersALandedNotesHandle(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)
	memo := h.ownMemo("a note to render")
	h.propose(memo.ID, noteProposal(scribe.VerbCreate, "estate", ""))

	res := h.apply(h.owner, h.accept(memo.ID))[0]
	if res.Status != StatusApplied {
		t.Fatalf("status %q (%s)", res.Status, res.Reason)
	}

	st := h.svc.linkState(h.ctx, h.link(memo.ID), false)
	if st.NoteRef != res.NoteRef {
		t.Errorf("the listing renders %q, the apply reported %q", st.NoteRef, res.NoteRef)
	}
	if st.NoteRef == "" {
		t.Error("a confirmed NOTE link renders with no handle at all")
	}
}

// STAGE 2 STILL RUNS FOR NOTE, and a target that no longer resolves is cleared
// into needs_input rather than failing at write time.
//
// The second half is the one this ticket made possible: before Override grew
// the NOTE block there was no field to supply target_note in, so the
// needs_input loop was a dead end for exactly the field stage 2 clears most.
func TestATargetThatNoLongerResolvesIsCompletableByTheOperator(t *testing.T) {
	h := newHarness(t)
	page := h.mkPage(t, "estate")
	live := h.mkNote(t, page, h.owner.ID, "A live note", "body")
	liveRef := store.FormatNoteRef(live.Number)

	// The model named a note that is not in the live catalogue — soft-deleted,
	// or never there.
	withCorpus(t, h, []string{"estate"}, []string{liveRef})
	memo := h.ownMemo("more on a note that went away")
	h.propose(memo.ID, noteProposal(scribe.VerbAppend, "", "CHR-9999"))

	it := h.accept(memo.ID)
	it.ConfirmEdit = true
	res := h.apply(h.owner, it)[0]
	if res.Status != StatusNeedsInput {
		t.Fatalf("status %q (%s), want needs_input", res.Status, res.Reason)
	}
	var named bool
	for _, c := range res.Cleared {
		if c.Field == "target_note" && c.Value == "CHR-9999" {
			named = true
		}
	}
	if !named {
		t.Errorf("the cleared field was not reported to the client: %+v", res.Cleared)
	}
	if _, err := h.store.MemoLinkFor(h.ctx, memo.ID); err == nil {
		t.Error("a proposal that needs input claimed a link row")
	}

	// The operator supplies the target they meant, and it lands.
	out := h.apply(h.owner, h.override(memo.ID, Override{
		Destination: "NOTE", Title: "More on it", Verb: "append",
		TargetNote: liveRef, Body: "the operator's own text",
	}))[0]
	if out.Status != StatusApplied {
		t.Fatalf("override status %q (%s), want applied", out.Status, out.Reason)
	}
	rev, err := h.store.CurrentRevision(h.ctx, live.ID)
	if err != nil {
		t.Fatalf("CurrentRevision: %v", err)
	}
	if !strings.Contains(rev.Body, "the operator's own text") {
		t.Errorf("body %q did not gain the override's text", rev.Body)
	}
}

// THE WIRE COLUMNS ARE FILLED ONLY BY A DECISION THAT GOES ON A WIRE, asserted
// THROUGH applyOne because that is where the Decision is built.
//
// The store-level test of this criterion constructs its own Decision, so it
// could never have caught the caller filling sent_title for every destination —
// which is what it did until the PR review found it. This is the half that
// fails if the caller regresses.
func TestALocalLandingFillsNoWireColumns(t *testing.T) {
	h := newHarness(t)
	withCorpus(t, h, []string{"estate"}, nil)

	note := h.ownMemo("a note whose title must not be recorded as sent")
	h.propose(note.ID, noteProposal(scribe.VerbCreate, "estate", ""))
	if res := h.apply(h.owner, h.accept(note.ID))[0]; res.Status != StatusApplied {
		t.Fatalf("NOTE status %q (%s)", res.Status, res.Reason)
	}
	l := h.link(note.ID)
	if l.SentTitle != "" || l.SentDescription != "" {
		t.Errorf("a NOTE landing recorded %q / %q as sent — nothing was sent anywhere",
			l.SentTitle, l.SentDescription)
	}

	// A DISCUSSION lands through the same branch and sends nothing either.
	disc := h.ownMemo("a thread whose title must not be recorded as sent")
	h.propose(disc.ID, &scribe.Proposal{
		Destination: scribe.DestDiscussion, Confidence: 0.9,
		Reason: "a question", Title: "Worth discussing", OpeningPost: "the post",
	})
	if res := h.apply(h.owner, h.accept(disc.ID))[0]; res.Status != StatusApplied {
		t.Fatalf("DISCUSSION status %q (%s)", res.Status, res.Reason)
	}
	if l := h.link(disc.ID); l.SentTitle != "" || l.SentDescription != "" {
		t.Errorf("a DISCUSSION landing recorded %q / %q as sent", l.SentTitle, l.SentDescription)
	}

	// AND THE TICKET ARM STILL FILLS THEM. They are what an operator is shown
	// beside a live card as "what Chronicle sent", so gating them on the wire
	// must not empty the one case that uses one.
	tkt := h.ownMemo("a ticket that really does go on a wire")
	h.propose(tkt.ID, ticketProposal("CHRN"))
	if res := h.apply(h.owner, h.accept(tkt.ID))[0]; res.Status != StatusApplied {
		t.Fatalf("TICKET status %q (%s)", res.Status, res.Reason)
	}
	if l := h.link(tkt.ID); l.SentTitle != "Do the thing" {
		t.Errorf("sent_title %q on a TICKET, want the title that was sent", l.SentTitle)
	}
}
