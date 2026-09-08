package scribe

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// CHRN-94 — the verb and the target, which CHRN-32 deliberately left out
// because the values were not knowable until CHRN-39 defined them.

// note builds a valid NOTE with the given extra fields spliced in, so each
// test below can break exactly one thing.
func note(extra string) []byte {
	base := `{"destination":"NOTE","confidence":0.8,"reason":"argues a principle",
	          "title":"What disposable means","nearest_page":null,"body":"text"`
	if extra != "" {
		base += "," + extra
	}
	return []byte(base + "}")
}

// THE DEFAULT IS SAFE IN THE ONE DIRECTION THAT MATTERS. The prompt does not
// ask for a verb yet (CHRN-87 does that), so silence has to mean something —
// and it means the verb that acts on nothing already written. The three verbs
// that can change authored text are exactly the three a model must name.
func TestAnAbsentVerbMeansCreateRatherThanNothing(t *testing.T) {
	p, err := Parse(note(""))
	if err != nil {
		t.Fatalf("a NOTE with no verb was rejected, which would burn every attempt: %v", err)
	}
	if p.Verb != VerbCreate {
		t.Fatalf("verb %q, want %q — a landing site must not have to re-derive the default", p.Verb, VerbCreate)
	}
	if p.TargetNote != nil {
		t.Fatalf("target_note %q, want nil", *p.TargetNote)
	}
}

// An explicit empty string is the same silence in a different spelling: a
// model told to emit every key answers "" rather than omitting one.
func TestAnEmptyVerbIsTheSameSilence(t *testing.T) {
	p, err := Parse(note(`"verb":"","target_note":null`))
	if err != nil {
		t.Fatalf(`verb:"" was rejected: %v`, err)
	}
	if p.Verb != VerbCreate {
		t.Fatalf("verb %q, want %q", p.Verb, VerbCreate)
	}
}

// Silence is defaulted; INVENTION IS NOT. A verb the model made up is a shape
// error and therefore retryable, which is the whole difference between stage 1
// and stage 2 — the model can fix this and the world has not moved.
func TestAnInventedVerbIsAShapeErrorAndNotADefault(t *testing.T) {
	_, err := Parse(note(`"verb":"merge","target_note":"CHR-0311"`))
	if err == nil {
		t.Fatal(`verb "merge" was accepted; only silence may be defaulted`)
	}
	if !strings.Contains(err.Error(), "merge") || !strings.Contains(err.Error(), "verb") {
		t.Fatalf("the retry feedback does not tell the model what it did wrong: %v", err)
	}
}

// THREE OF FOUR CARRY A TARGET, and page_path cannot stand in for it: a page
// holds many notes, which is why NotesOnPage exists.
func TestTheThreeVerbsThatActOnAnExistingNoteRequireOne(t *testing.T) {
	for _, v := range []Verb{VerbAppend, VerbSupersede, VerbRelate} {
		if !v.NeedsTarget() {
			t.Errorf("%s should need a target", v)
		}
		_, err := Parse(note(`"verb":"` + string(v) + `","target_note":null`))
		if err == nil {
			t.Errorf("%s was accepted with no target_note", v)
			continue
		}
		if !strings.Contains(err.Error(), "target_note") {
			t.Errorf("%s: the error does not name the missing field: %v", v, err)
		}
	}
	if VerbCreate.NeedsTarget() {
		t.Error("create must not need a target — it acts on nothing that exists")
	}
}

// create acting on an existing note is a contradiction, and letting it through
// would put a target on a row whose verb says there is none.
func TestCreateMayNotCarryATarget(t *testing.T) {
	_, err := Parse(note(`"verb":"create","target_note":"CHR-0311"`))
	if err == nil {
		t.Fatal("create was accepted with a target_note")
	}
	if !strings.Contains(err.Error(), "target_note") {
		t.Fatalf("error does not name the field: %v", err)
	}
}

// A RETRYABLE SHAPE ERROR ON PURPOSE. "the smart calendar note" is something a
// person understands and the contract cannot use — exactly what feeding the
// error back to the model is for.
func TestATargetThatIsNotANoteReferenceIsRetryable(t *testing.T) {
	for _, bad := range []string{"the smart calendar note", "CHR-", "311", "CHR-0", "AMB-2291"} {
		_, err := Parse(note(`"verb":"append","target_note":"` + bad + `"`))
		if err == nil {
			t.Errorf("%q was accepted as a note reference", bad)
			continue
		}
		if !strings.Contains(err.Error(), "CHR-0311") {
			t.Errorf("%q: the feedback does not show the model the shape it wants: %v", bad, err)
		}
	}
}

// Lenient in, because people dictate these while driving and the store parses
// them the same way. CHR-0 is absent from this list deliberately — see the
// guard test in internal/store.
func TestTheSpellingsAPersonActuallyWritesAreAccepted(t *testing.T) {
	for _, ok := range []string{"CHR-0311", "chr-311", "CHR-00311", "Chr-1"} {
		p, err := Parse(note(`"verb":"supersede","target_note":"` + ok + `"`))
		if err != nil {
			t.Errorf("%q was rejected: %v", ok, err)
			continue
		}
		if p.TargetNote == nil || *p.TargetNote != ok {
			t.Errorf("%q did not survive parsing unchanged", ok)
		}
	}
}

// Stage 2, and it blocks the same way page_path does. Every non-create verb
// clears while the corpus is empty, which is the correct answer and not a gap:
// there is no note to act on.
func TestAnUnresolvableTargetBlocksAndIsRecorded(t *testing.T) {
	p, err := Parse(note(`"verb":"append","target_note":"CHR-0311"`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{})
	if status != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input — there is nothing to append to", status)
	}
	if p.TargetNote != nil {
		t.Fatal("an unresolvable target survived and could have been accepted")
	}
	if len(cleared) != 1 || cleared[0].Field != "target_note" || cleared[0].Value != "CHR-0311" {
		t.Fatalf("the clearing was not recorded, so CHRN-36 cannot count it: %+v", cleared)
	}
}

// THE VERB IS LEFT ALONE WHEN THE TARGET CLEARS. Rewriting a cleared `append`
// into a `create` would turn a proposal to CHANGE authored text into one to
// WRITE new text — a different act, chosen by a validator rather than by the
// person CHRN-39 requires to confirm it.
func TestClearingATargetDoesNotSilentlyRewriteTheVerb(t *testing.T) {
	p, err := Parse(note(`"verb":"supersede","target_note":"CHR-0311"`))
	if err != nil {
		t.Fatal(err)
	}
	if _, status := Reconcile(p, fakeCatalogue{}); status != StatusNeedsInput {
		t.Fatalf("status %q", status)
	}
	if p.Verb != VerbSupersede {
		t.Fatalf("verb became %q — the proposal now says something the model did not", p.Verb)
	}
}

// The other side of the same rule: a target that DOES resolve leaves a
// proposal that can land.
func TestAResolvableTargetLeavesTheProposalValid(t *testing.T) {
	cat := fakeCatalogue{notes: map[string]bool{"CHR-0311": true}}
	p, err := Parse(note(`"verb":"append","target_note":"CHR-0311"`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, cat)
	if status != StatusValid {
		t.Fatalf("status %q, want valid — the note it names is right there", status)
	}
	if len(cleared) != 0 {
		t.Fatalf("nothing should have been cleared: %+v", cleared)
	}
	if p.TargetNote == nil || *p.TargetNote != "CHR-0311" {
		t.Fatal("the target did not survive reconciliation")
	}
}

// A create has no target, so an empty NOTE CORPUS must not block it. It still
// needs a page, which is a different question and CHRN-95 ruling 7's — the
// catalogue here has a live page and no notes at all, which is exactly the
// state this test is about.
func TestACreateIsNotBlockedByAnEmptyCorpus(t *testing.T) {
	p, err := Parse(note(`"verb":"create","target_note":null,"page_path":"ideas"`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{pages: map[string]bool{"ideas": true}})
	if status != StatusValid {
		t.Fatalf("status %q, want valid — a new note needs no existing one", status)
	}
	if len(cleared) != 0 {
		t.Fatalf("nothing should have been cleared: %+v", cleared)
	}
}

// STAGE 1 MUST STORE WHAT IT APPROVED. Validating a trimmed copy and keeping
// the padded original is the same hole IsNoteRef closes, entered from the
// other side: the reference passes stage 1, reaches HasNote with its space
// intact, fails to resolve, and is cleared with "no such live note" — about a
// note that is right there, and not as a retryable error the model could fix.
// Found by the reviewer of PR #67.
func TestAPaddedTargetIsNormalisedAndStillResolves(t *testing.T) {
	p, err := Parse(note(`"verb":"append","target_note":"  CHR-0311\t"`))
	if err != nil {
		t.Fatalf("a padded reference was rejected rather than normalised: %v", err)
	}
	if p.TargetNote == nil || *p.TargetNote != "CHR-0311" {
		t.Fatalf("target_note = %q, want the trimmed value written back", *p.TargetNote)
	}

	cat := fakeCatalogue{notes: map[string]bool{"CHR-0311": true}}
	cleared, status := Reconcile(p, cat)
	if status != StatusValid {
		t.Fatalf("status %q — the note it names is in the catalogue, and the padding was the only difference", status)
	}
	if len(cleared) != 0 {
		t.Fatalf("cleared %+v — a person would be told the note does not exist", cleared)
	}
}

// The overflow case, at the contract's own boundary rather than only in the
// cross-package guard: a number no int64 can hold cannot name a note.
func TestATargetTooLargeToBeANoteNumberIsRefused(t *testing.T) {
	if IsNoteRef("CHR-99999999999999999999") {
		t.Fatal("a reference that overflows int64 was accepted as well-formed")
	}
	if _, err := Parse(note(`"verb":"append","target_note":"CHR-99999999999999999999"`)); err == nil {
		t.Fatal("Parse accepted a note number no int64 can hold")
	}
}

// A Proposal does not always come through Parse: store.Proposal.Payload is
// decoded with a plain json.Unmarshal, so every row written before CHRN-94
// deserialises with the zero value and never sees the normalisation. If the
// zero value claimed to need a target, CHRN-95 would be told those rows want
// something they were never able to carry. Found by the reviewer of PR #67.
func TestTheZeroValueVerbNeedsNoTargetEither(t *testing.T) {
	if Verb("").NeedsTarget() {
		t.Fatal(`Verb("").NeedsTarget() is true, but an absent verb means create`)
	}
	var decoded Proposal
	if err := json.Unmarshal([]byte(`{"destination":"NOTE","body":"x"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Verb.NeedsTarget() {
		t.Fatal("a payload decoded outside Parse claims to need a target")
	}
}

// CHRN-95 RULING 1 — append and supersede are never pre-selected for ACCEPT
// ALL, at any confidence.
//
// Asserted well ABOVE the floor so the verb is demonstrably the reason: a test
// that used a low confidence would pass whether or not the gate existed.
func TestAppendAndSupersedeAreNeverPreAcceptable(t *testing.T) {
	const floor = 0.8
	for _, tc := range []struct {
		verb Verb
		want bool
	}{
		{VerbCreate, true},
		{VerbRelate, true},
		{VerbAppend, false},
		{VerbSupersede, false},
	} {
		p := &Proposal{
			Destination: DestNote, Confidence: 0.99, Verb: tc.verb,
			Title: "t", Body: "b", Reason: "r",
		}
		if got := p.PreAcceptable(StatusValid, floor); got != tc.want {
			t.Errorf("PreAcceptable(%s) at confidence 0.99 = %v, want %v", tc.verb, got, tc.want)
		}
	}
}

// RULING 7 — a create that names no page has nowhere to land, so it needs a
// person rather than a default. tier2.notes.page_id is NOT NULL and there is no
// inbox page to fall back on.
func TestACreateWithNoPageNeedsInput(t *testing.T) {
	p, err := Parse(note(`"verb":"create","page_path":null`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{pages: map[string]bool{"estate": true}})
	if status != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input", status)
	}
	if len(cleared) != 1 || cleared[0].Field != "page_path" {
		t.Fatalf("clearing does not name the field to supply: %+v", cleared)
	}

	// ONE CLEARING, NOT TWO. A path that exists but has no live ancestor is
	// cleared for that reason alone; telling an operator both would be the
	// first answer said twice.
	p2, err := Parse(note(`"verb":"create","page_path":"invented/branch"`))
	if err != nil {
		t.Fatal(err)
	}
	cleared2, status2 := Reconcile(p2, fakeCatalogue{pages: map[string]bool{"estate": true}})
	if status2 != StatusNeedsInput {
		t.Fatalf("status %q, want needs_input", status2)
	}
	if len(cleared2) != 1 {
		t.Errorf("%d clearings for one bad path: %+v", len(cleared2), cleared2)
	}
}

// RULING 8 — target_thread stays reserved, and this is what the code can
// actually assert.
//
// NOT STRICT REJECTION. scribe.Parse unmarshals into a map to check presence
// and then into Proposal with a plain json.Unmarshal; neither refuses unknown
// keys, and the only DisallowUnknownFields in the repo is on the HTTP decoder,
// which a model's output never passes through. So a model emitting
// target_thread has it silently dropped, exactly as it would any other unknown
// key. Making Parse strict is a change to CHRN-94's contract and is filed
// separately rather than smuggled in here.
func TestTheContractStillHasNoTargetThread(t *testing.T) {
	raw := []byte(`{"destination":"DISCUSSION","confidence":0.9,"reason":"a question",
	    "title":"Worth discussing","nearest_page":null,"opening_post":"the post",
	    "target_thread":"DSC-0007"}`)
	p, err := Parse(raw)
	if err != nil {
		t.Fatalf("an unknown key was rejected, which Parse does not do: %v", err)
	}
	if p.OpeningPost != "the post" {
		t.Errorf("opening_post %q — the rest of the payload must decode normally", p.OpeningPost)
	}
	for _, f := range structFields(p) {
		if strings.Contains(strings.ToLower(f), "targetthread") {
			t.Errorf("Proposal has a %s field; ruling 8 keeps target_thread reserved", f)
		}
	}
}

func structFields(p *Proposal) []string {
	rt := reflect.TypeOf(*p)
	out := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		out = append(out, rt.Field(i).Name)
	}
	return out
}
