package catalogue

import (
	"strings"
	"testing"
)

// CHRN-94 — the note half of "one fetch, two readers".

// COMPARED BY NUMBER, NOT BY STRING. The reference is lenient by design, so a
// string compare would resolve whichever spelling the catalogue happened to
// hold and clear the other two — a target the model wrote correctly, cleared
// because somebody typed the fixture differently.
func TestANoteResolvesWhicheverWayEitherSideSpellsIt(t *testing.T) {
	s := &Snapshot{Version: 1, Notes: []string{"CHR-0311"}}
	for _, spelling := range []string{"CHR-0311", "chr-0311", "CHR-311", "chr-311", "CHR-00311"} {
		if !s.HasNote(spelling) {
			t.Errorf("%q did not resolve against a catalogue holding CHR-0311", spelling)
		}
	}
	// And the fixture's own spelling does not privilege it either.
	loose := &Snapshot{Version: 1, Notes: []string{"chr-311"}}
	if !loose.HasNote("CHR-0311") {
		t.Error("a catalogue written chr-311 did not resolve CHR-0311; they are one note")
	}
}

func TestAnUnknownOrMalformedReferenceDoesNotResolve(t *testing.T) {
	s := &Snapshot{Version: 1, Notes: []string{"CHR-0311"}}
	for _, no := range []string{"CHR-0312", "CHR-0", "AMB-2291", "311", "", "the calendar note"} {
		if s.HasNote(no) {
			t.Errorf("%q resolved, and it should not", no)
		}
	}
}

// The same not-a-special-case as HasPage: an empty corpus answers false to
// everything, so every non-create verb clears at stage 2.
func TestAnEmptyCorpusResolvesNothing(t *testing.T) {
	if (&Snapshot{Version: 1}).HasNote("CHR-0311") {
		t.Fatal("an empty catalogue resolved a note")
	}
}

// The empty text names the CONSEQUENCE, not the absence. A model told only
// that the list is empty will still reach for `append` on a memo that plainly
// refers back to something.
func TestTheEmptyNoteListTellsTheModelWhatItMayAnswer(t *testing.T) {
	got := (&Snapshot{Version: 1}).RenderNotes()
	for _, want := range []string{"create", "target_note", "null"} {
		if !strings.Contains(got, want) {
			t.Errorf("the empty rendering does not mention %q, so the constraint is not stated: %q", want, got)
		}
	}
}

func TestAPopulatedNoteListRendersOneLineEach(t *testing.T) {
	got := (&Snapshot{Version: 1, Notes: []string{"CHR-0311", "CHR-0312"}}).RenderNotes()
	if got != "- CHR-0311\n- CHR-0312" {
		t.Fatalf("rendering:\n%s", got)
	}
}

// Shape is validated where a person edits it, on the projects' pattern: a
// malformed entry would be offered to the model and then cleared by stage 2,
// which reads as a hallucination the model did not commit.
func TestAMalformedOrRepeatedNoteIsRefusedAtParse(t *testing.T) {
	cases := map[string]string{
		"malformed": "version: 1\nprojects:\n  - {key: CHRN, name: Chronicle}\nnotes: [\"not-a-note\"]\n",
		"zero":      "version: 1\nprojects:\n  - {key: CHRN, name: Chronicle}\nnotes: [\"CHR-0\"]\n",
		"duplicate": "version: 1\nprojects:\n  - {key: CHRN, name: Chronicle}\nnotes: [\"CHR-0311\", \"chr-311\"]\n",
	}
	for name, doc := range cases {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: accepted, and it would reach the model", name)
		}
	}
}

func TestAWellFormedNoteListParses(t *testing.T) {
	s, err := Parse([]byte("version: 1\nprojects:\n  - {key: CHRN, name: Chronicle}\nnotes: [\"CHR-0311\", \"CHR-0312\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Notes) != 2 || !s.HasNote("CHR-311") {
		t.Fatalf("notes did not survive parsing: %+v", s.Notes)
	}
}
