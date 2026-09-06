package scribe

import (
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

// A create has no target, so stage 2 has nothing to ask the catalogue and must
// not invent a reason to block.
func TestACreateIsNotBlockedByAnEmptyCorpus(t *testing.T) {
	p, err := Parse(note(`"verb":"create","target_note":null,"page_path":null`))
	if err != nil {
		t.Fatal(err)
	}
	cleared, status := Reconcile(p, fakeCatalogue{})
	if status != StatusValid {
		t.Fatalf("status %q, want valid — a new note needs no existing one", status)
	}
	if len(cleared) != 0 {
		t.Fatalf("nothing should have been cleared: %+v", cleared)
	}
}
