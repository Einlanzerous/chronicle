package scribe

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// No database, no model, no network. Everything here is the contract itself.

// fakeCatalogue is CHRN-31's shape, stubbed. The zero value is the catalogue as
// it actually is until E5: no pages at all.
type fakeCatalogue struct {
	projects map[string]bool
	pages    map[string]bool
}

func (c fakeCatalogue) HasProject(k string) bool { return c.projects[k] }
func (c fakeCatalogue) HasPage(p string) bool    { return c.pages[p] }

// A minimal valid proposal of each kind, as JSON, so the tests below can break
// one field at a time.
func validTicket() string {
	return `{"destination":"TICKET","confidence":0.86,"reason":"names an owner and a due date",
	         "title":"Fix the decode collision","nearest_page":null,
	         "project_key":"CHRN","ticket_type":"bug","description":"## Summary\nA thing."}`
}

// THE LOAD-BEARING RULE OF §4. A model that can answer "I'd rather not" will
// learn to, and HOLD would become the abstention default that swallows the
// ambiguous half of every batch — leaving CHRN-36 nothing to score, and a
// router that cannot be measured is one E4's exit criterion cannot accept.
func TestHOLDIsNotAProposableDestination(t *testing.T) {
	_, err := Parse([]byte(`{"destination":"HOLD","confidence":0.4,"reason":"decide later","title":"x","nearest_page":null}`))
	if err == nil {
		t.Fatal("HOLD was accepted as a destination; it is an operator action and never a proposal")
	}
	if !strings.Contains(err.Error(), "HOLD") {
		t.Fatalf("the error does not tell the model what it did wrong: %v", err)
	}
}

// DISCARD is the other half of the same decision, and it IS proposable: a
// judgement about content, which the model is positioned to make and `reason`
// makes checkable at a glance.
func TestDISCARDIsProposableAndNeedsNoTitle(t *testing.T) {
	p, err := Parse([]byte(`{"destination":"DISCARD","confidence":0.95,
	    "reason":"eleven seconds of microphone testing, no content","nearest_page":null}`))
	if err != nil {
		t.Fatalf("DISCARD with no title was rejected: %v", err)
	}
	if p.Destination != DestDiscard {
		t.Fatalf("destination %q", p.Destination)
	}
}

// A title on a memo being thrown away is a fabrication with no reader, but
// every other destination needs one.
func TestTitleIsRequiredExceptOnDiscard(t *testing.T) {
	_, err := Parse([]byte(`{"destination":"NOTE","confidence":0.5,"reason":"doctrine",
	    "nearest_page":null,"body":"text"}`))
	if err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("a NOTE with no title was accepted, or the error did not name the field: %v", err)
	}
}

// The reason is what makes a wrong proposal rejectable at a glance. Without it
// the operator has to verify the destination from scratch, which is slower than
// deciding unaided — so a router that omits it has negative value.
func TestReasonIsRequiredAndNotJustWhitespace(t *testing.T) {
	for _, reason := range []string{`""`, `"   "`} {
		_, err := Parse([]byte(`{"destination":"DISCARD","confidence":0.5,"reason":` + reason + `,"nearest_page":null}`))
		if err == nil || !strings.Contains(err.Error(), "reason") {
			t.Fatalf("reason %s was accepted: %v", reason, err)
		}
	}
}

// PRESENCE, not just value. `project_key: ""` is a real answer — the salvaged
// prompt forbids defaulting because the key is immutable once the ticket exists
// — but a model that omits the key entirely has not followed the contract, and
// the two must not look the same.
func TestTicketProjectKeyMustBePresentButMayBeEmpty(t *testing.T) {
	missing := `{"destination":"TICKET","confidence":0.5,"reason":"work","title":"Do it",
	             "nearest_page":null,"ticket_type":"task","description":"x"}`
	if _, err := Parse([]byte(missing)); err == nil || !strings.Contains(err.Error(), "project_key") {
		t.Fatalf("an absent project_key was accepted: %v", err)
	}

	empty := `{"destination":"TICKET","confidence":0.5,"reason":"work","title":"Do it",
	           "nearest_page":null,"project_key":"","ticket_type":"task","description":"x"}`
	if _, err := Parse([]byte(empty)); err != nil {
		t.Fatalf("an empty project_key was rejected, which would teach the model to guess: %v", err)
	}
}

// Required-but-nullable: the key must appear so that "no nearby page" is stated
// rather than inferred from a model that forgot the field.
func TestNearestPageMustBePresentEvenWhenNull(t *testing.T) {
	_, err := Parse([]byte(`{"destination":"DISCARD","confidence":0.5,"reason":"noise"}`))
	if err == nil || !strings.Contains(err.Error(), "nearest_page") {
		t.Fatalf("an absent nearest_page was accepted: %v", err)
	}
}

func TestConfidenceMustBeInRange(t *testing.T) {
	for _, c := range []string{"1.5", "-0.1"} {
		_, err := Parse([]byte(`{"destination":"DISCARD","confidence":` + c + `,"reason":"x","nearest_page":null}`))
		if err == nil || !strings.Contains(err.Error(), "confidence") {
			t.Fatalf("confidence %s was accepted: %v", c, err)
		}
	}
}

// EVERY error, not the first. §7 allows three attempts, and a retry that fixes
// one field only to trip over the next spends one attempt per field.
func TestParseReportsEveryFailureAtOnce(t *testing.T) {
	_, err := Parse([]byte(`{"destination":"TICKET","confidence":9,"reason":"",
	    "nearest_page":null,"project_key":"chrn","ticket_type":"nonsense","description":""}`))
	var errs ShapeErrors
	if !errors.As(err, &errs) {
		t.Fatalf("got %T, want ShapeErrors", err)
	}
	if len(errs) < 5 {
		t.Fatalf("reported %d failures, want every one of them: %v", len(errs), errs)
	}
	// The feedback is what goes back to the model on the next attempt.
	if !strings.Contains(errs.Prompt(), "confidence") || !strings.Contains(errs.Prompt(), "ticket_type") {
		t.Fatalf("the retry feedback does not name the fields:\n%s", errs.Prompt())
	}
}

// §5's split: a bad TARGET blocks, a bad ADVISORY field does not. An otherwise
// acceptable TICKET must not become work for the operator because the model
// invented a page name in a sentence.
func TestAHallucinatedNearestPageIsClearedWithoutBlocking(t *testing.T) {
	cat := fakeCatalogue{projects: map[string]bool{"CHRN": true}}
	p, err := Parse([]byte(`{"destination":"TICKET","confidence":0.9,"reason":"work","title":"Do it",
	    "nearest_page":"storage/amber","project_key":"CHRN","ticket_type":"task","description":"x"}`))
	if err != nil {
		t.Fatal(err)
	}

	cleared, status := Reconcile(p, cat)
	if status != StatusValid {
		t.Fatalf("status %q — an advisory field must not block a proposal that can land", status)
	}
	if p.NearestPage != nil {
		t.Fatalf("the hallucinated page survived as %q; it cannot be accepted if it is not there", *p.NearestPage)
	}
	if len(cleared) != 1 || cleared[0].Field != "nearest_page" || cleared[0].Value != "storage/amber" {
		t.Fatalf("the clearing was not recorded, so CHRN-36 cannot count it: %+v", cleared)
	}
}

// A target, so it blocks — and with no page tree at all (before CHRN-37) every
// non-null page_path clears, which is the correct answer rather than a gap.
func TestAPagePathWithNoLiveAncestorBlocks(t *testing.T) {
	p, err := Parse([]byte(`{"destination":"NOTE","confidence":0.8,"reason":"doctrine","title":"A note",
	    "nearest_page":null,"page_path":"storage/amber/retention","body":"text"}`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{})
	if status != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input — the note has nowhere to land", status)
	}
	if p.PagePath != nil {
		t.Fatal("a page path with no live ancestor survived")
	}
	if len(cleared) != 1 || cleared[0].Field != "page_path" {
		t.Fatalf("clearing not recorded: %+v", cleared)
	}
}

// The ancestor rule is what lets a note propose a NEW leaf without letting it
// invent a whole branch. `storage/amber/retention` is admissible when
// `storage/amber` exists; it is not admissible on an empty tree.
func TestAPagePathMayBeNewWhenAnAncestorIsLive(t *testing.T) {
	cat := fakeCatalogue{pages: map[string]bool{"storage/amber": true}}
	p, err := Parse([]byte(`{"destination":"NOTE","confidence":0.8,"reason":"doctrine","title":"A note",
	    "nearest_page":null,"page_path":"storage/amber/retention","body":"text"}`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, cat)
	if status != StatusValid || p.PagePath == nil {
		t.Fatalf("a new leaf under a live ancestor was refused: status=%q cleared=%+v", status, cleared)
	}
}

// Not a clearing and not an error: the model was asked to answer empty rather
// than guess, and it did. It needs a person, which is what needs_input means.
func TestAnEmptyProjectKeyNeedsInputAndIsNotAnError(t *testing.T) {
	p, err := Parse([]byte(`{"destination":"TICKET","confidence":0.7,"reason":"work","title":"Do it",
	    "nearest_page":null,"project_key":"","ticket_type":"task","description":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{})
	if status != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input", status)
	}
	if len(cleared) != 0 {
		t.Fatalf("nothing was hallucinated, so nothing should be recorded as cleared: %+v", cleared)
	}
}

// The Thursday-morning case: the proposal was right on Tuesday and the project
// has been archived since. Stage 2 runs again at acceptance for exactly this.
func TestAnArchivedProjectIsClearedAndBlocks(t *testing.T) {
	p, err := Parse([]byte(`{"destination":"TICKET","confidence":0.9,"reason":"work","title":"Do it",
	    "nearest_page":null,"project_key":"GONE","ticket_type":"task","description":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{projects: map[string]bool{"CHRN": true}})
	if status != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input", status)
	}
	if len(cleared) != 1 || cleared[0].Field != "project_key" {
		t.Fatalf("clearing not recorded: %+v", cleared)
	}
	// Destination, confidence and reason are kept: it needs a new project, not
	// a re-route from scratch.
	if p.Destination != DestTicket || p.Confidence != 0.9 || p.Reason == "" {
		t.Fatalf("stage 2 discarded the model's honest answer: %+v", p)
	}
}

// R1's corollary, and it is a CONTRACT rule rather than a threshold. `discarded`
// is terminal in the memo state machine, and a confident wrong DISCARD is
// precisely the case that clears any floor. It is the one accept that cannot be
// undone and it should cost a deliberate tap.
func TestDiscardIsNeverPreAcceptableAtAnyConfidence(t *testing.T) {
	p := Proposal{Destination: DestDiscard, Confidence: 1.0}
	if p.PreAcceptable(StatusValid, 0.0) {
		t.Fatal("a DISCARD at 1.0 was pre-acceptable with the floor on the ground")
	}

	// The same confidence on a destination that can be walked back is fine.
	q := Proposal{Destination: DestNote, Confidence: 1.0}
	if !q.PreAcceptable(StatusValid, 0.9) {
		t.Fatal("a NOTE above the floor was not pre-acceptable")
	}
	// And status is the other gate.
	if q.PreAcceptable(StatusNeedsInput, 0.9) {
		t.Fatal("a proposal awaiting input was pre-acceptable")
	}
}

// The default admits nothing, so ACCEPT ALL cannot pre-select on a number
// nobody has measured. CHRN-36 sets the real one.
func TestTheDefaultThresholdPreAcceptsNothing(t *testing.T) {
	p := Proposal{Destination: DestNote, Confidence: 1.0}
	if p.PreAcceptable(StatusValid, 1.01) {
		t.Fatal("the default floor admitted a proposal; it must admit none until CHRN-36 sets one")
	}
}

func TestProposerIsRunnerQualifiedWithAPromptVersion(t *testing.T) {
	got, err := Proposer("ollama", "gemma4:31b", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "ollama/gemma4:31b@v1" {
		t.Fatalf("proposer %q", got)
	}
	// A part carrying a separator would make the string ambiguous, and an
	// ambiguous proposer is one CHRN-36 attributes to the wrong thing.
	if _, err := Proposer("ollama", "gemma4/31b", "v1"); err == nil {
		t.Fatal("a model name containing '/' was accepted")
	}
	if _, err := Proposer("ollama", "gemma4:31b", ""); err == nil {
		t.Fatal("an empty prompt version was accepted; without it a prompt regression is invisible")
	}
}

// §7: three attempts, with the validation error fed back. A model told what it
// got wrong usually fixes it, and one more completion is cheaper than an
// operator's attention.
func TestRunFeedsTheValidationErrorBackAndSucceeds(t *testing.T) {
	var sawFeedback string
	gen := func(_ context.Context, attempt int, feedback string) ([]byte, error) {
		if attempt == 1 {
			return []byte(`{"destination":"TICKET","confidence":7,"reason":"work","title":"Do it",
			    "nearest_page":null,"project_key":"CHRN","ticket_type":"task","description":"x"}`), nil
		}
		sawFeedback = feedback
		return []byte(validTicket()), nil
	}

	out := Run(context.Background(), gen, fakeCatalogue{projects: map[string]bool{"CHRN": true}}, 3)
	if out.Proposal == nil {
		t.Fatalf("no proposal after a recoverable first attempt: %v", out.Err)
	}
	if !strings.Contains(sawFeedback, "confidence") {
		t.Fatalf("the second attempt was not told what the first got wrong: %q", sawFeedback)
	}
	if out.Status != StatusValid {
		t.Fatalf("status %q", out.Status)
	}
}

// THE MEMO NEVER SILENTLY DISAPPEARS. After the ceiling, the failure comes back
// with the raw output attached so it can be recorded and the memo shown with
// its error — the operator can then route it by hand. A run that produced
// nothing at all is the thing this test exists to make impossible.
func TestRunRecordsAFailureRatherThanReturningNothing(t *testing.T) {
	calls := 0
	gen := func(_ context.Context, _ int, _ string) ([]byte, error) {
		calls++
		return []byte(`{"destination":"NONSENSE"}`), nil
	}

	out := Run(context.Background(), gen, fakeCatalogue{}, 3)
	if calls != 3 {
		t.Fatalf("made %d attempts, want the full ceiling of 3", calls)
	}
	if out.Proposal != nil {
		t.Fatal("a nonsense response produced a proposal")
	}
	if out.Status != StatusInvalid {
		t.Fatalf("status %q, want invalid", out.Status)
	}
	if out.Err == nil {
		t.Fatal("the failure was not carried back, so nothing could record it")
	}
	if len(out.Raw) == 0 {
		t.Fatal("the raw output was dropped; it is the only evidence of why the prompt failed")
	}
}

// A transport failure is not a contract failure and no reworded prompt fixes
// it, so it ends the run rather than burning the remaining attempts on a
// service that is down.
func TestRunStopsOnAGenerateError(t *testing.T) {
	calls := 0
	gen := func(_ context.Context, _ int, _ string) ([]byte, error) {
		calls++
		return nil, errors.New("connection refused")
	}
	out := Run(context.Background(), gen, fakeCatalogue{}, 3)
	if calls != 1 {
		t.Fatalf("made %d attempts against an unreachable model, want 1", calls)
	}
	if out.Err == nil {
		t.Fatal("the transport error was swallowed")
	}
}
