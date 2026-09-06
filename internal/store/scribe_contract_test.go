package store

import (
	"slices"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// THE GUARD FOR TWO COPIES OF ONE FACT.
//
// internal/scribe declares the verb set and the note-reference syntax, and so
// does this package. That is not duplication anybody chose: **internal/store
// imports internal/scribe** (proposal.go), so the dependency runs one way and
// the contract package cannot import these constants back.
//
// Two copies of an enum is the shape this repo distrusts, so they are checked
// rather than trusted — CHRN-84's pattern, where the test loops over the
// exported set itself so the advertised vocabulary and the usable one cannot
// drift. These tests need no database: they compare constants.

// storeVerbs is this package's set, written out so that adding a constant
// without adding it here fails the count assertion below rather than passing
// silently.
var storeVerbs = []string{VerbCreate, VerbAppend, VerbSupersede, VerbRelate}

func TestScribeVerbsMatchTheColumn(t *testing.T) {
	// migration 0014's note_revisions_verb CHECK admits exactly these four. A
	// verb the contract can propose and the column refuses is a proposal that
	// validates, reaches a person, is confirmed, and then fails on INSERT with
	// a constraint violation — the worst place to find out.
	if len(scribe.Verbs) != len(storeVerbs) {
		t.Fatalf("verb sets differ in size: scribe has %d %v, store has %d %v",
			len(scribe.Verbs), scribe.Verbs, len(storeVerbs), storeVerbs)
	}
	for _, v := range scribe.Verbs {
		if !slices.Contains(storeVerbs, string(v)) {
			t.Errorf("scribe.Verbs has %q, which this package cannot store — "+
				"add it to the constants above AND to migration 0014's CHECK, or remove it there", v)
		}
	}
	for _, v := range storeVerbs {
		if !slices.Contains(scribe.Verbs, scribe.Verb(v)) {
			t.Errorf("store can hold verb %q, which scribe.Verbs does not offer — "+
				"a verb nothing can propose is a column value nothing writes", v)
		}
	}
}

// The one asymmetry the two sets are ALLOWED to have, asserted so it reads as
// deliberate: the column is nullable and the contract has no null verb.
//
// CHRN-39's plan settles what that means — a null verb is "authored directly",
// which is a revision nobody proposed. scribe.Verbs describes what Scribe may
// SAY, and "I am not proposing anything" is not something it can say.
func TestTheContractHasNoVerbForARevisionNobodyProposed(t *testing.T) {
	if slices.Contains(scribe.Verbs, scribe.Verb("")) {
		t.Fatal(`scribe.Verbs contains "", but null on the column means "authored directly" — ` +
			`a proposal always proposes something`)
	}
}

// Everything scribe.IsNoteRef admits, ParseNoteRef must resolve, and the other
// way round. A reference the contract accepts and the store cannot parse is a
// target_note that survives stage 1 and then cannot be looked up at all.
func TestTheNoteReferenceSyntaxIsTheSameOnBothSides(t *testing.T) {
	cases := []string{
		// Accepted by both: the lenient spellings a person actually writes.
		"CHR-0311", "chr-0311", "CHR-311", "chr-311", "CHR-00311", "Chr-1",
		"CHR-10000", "CHR-0001",
		// Rejected by both. `CHR-0` and the int64 overflow are here because a
		// pattern cannot see either without restating the store's arithmetic
		// in a second notation — which is why IsNoteRef parses rather than
		// matching. Both were found by the reviewer of PR #67 against a version
		// that only matched.
		"", "311", "CHR-", "CHR-0", "chr-0", "CHR-000", "CHR-abc", "CHR_311",
		"CHR-311x", " CHR-311", "CHR-311 ", "CHR--311", "CHR-3.11", "CHR--1",
		"AMB-311", "CHRN-311",
		"CHR-99999999999999999999", "CHR-9223372036854775808",
	}
	for _, c := range cases {
		_, err := ParseNoteRef(c)
		store, scribeOK := err == nil, scribe.IsNoteRef(c)
		if store != scribeOK {
			t.Errorf("%q: scribe.IsNoteRef=%v but ParseNoteRef succeeds=%v — "+
				"the two spellings of one rule have drifted", c, scribeOK, store)
		}
	}
}
