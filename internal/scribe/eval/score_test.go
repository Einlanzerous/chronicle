package eval

import (
	"testing"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

// lbl builds a label. Real labels get a pin so they look like the committed
// ones; nothing in scoring reads it except the run log.
func lbl(id string, s Stratum, d scribe.Destination, tt string, confident *bool, alts ...Alternative) Label {
	l := Label{
		Stratum: s, Destination: d, TicketType: tt,
		Confident: confident, Reason: "because", AlsoDefensible: alts,
	}
	if s == StratumReal {
		l.Hash = id
		l.LabelledAgainst = "whisper.cpp/small.en"
	} else {
		l.File = "synthetic/" + id + ".md"
	}
	return l
}

// routed builds the result of a router that answered with one destination at
// one confidence.
func routed(l Label, d scribe.Destination, tt string, conf float64) Result {
	return Result{
		Item: Item{Label: l, Text: "…"},
		Outcome: scribe.Outcome{
			Proposal: &scribe.Proposal{Destination: d, TicketType: tt, Confidence: conf, Reason: "r"},
			Status:   scribe.StatusValid,
		},
	}
}

// silent builds the result of a router that produced nothing valid — CHRN-32
// §7's "a proposal that silently disappears from a batch".
func silent(l Label) Result {
	return Result{Item: Item{Label: l}, Outcome: scribe.Outcome{Status: scribe.StatusInvalid}}
}

func hash(n byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = '0'
	}
	b[63] = "0123456789abcdef"[n%16]
	return string(b)
}

// §2's rule, checked where it can be checked: the report holds one accuracy per
// stratum and there is no field on it that could hold a blended one. A single
// number would hide the only comparison that matters.
func TestAccuracyIsReportedPerStratumAndNeverAveraged(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl(hash(1), StratumReal, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.9),
		routed(lbl(hash(2), StratumReal, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.9),
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
	})

	if len(rep.Strata) != 2 {
		t.Fatalf("strata = %d, want 2", len(rep.Strata))
	}
	if got := rep.Accuracy(StratumReal).StrictRate(); got != 0 {
		t.Errorf("real strict = %v, want 0", got)
	}
	if got := rep.Accuracy(StratumSynthetic).StrictRate(); got != 1 {
		t.Errorf("synthetic strict = %v, want 1", got)
	}
	// The blended number would be 50%, and it is nowhere: the honest reading
	// is that the router is perfect on written fixtures and useless on speech.
	if rep.Accuracy(StratumReal).N != 2 || rep.Accuracy(StratumSynthetic).N != 2 {
		t.Errorf("strata were pooled")
	}
}

// §4: where two labels are both defensible, the router picking the other one is
// the ceiling on what any router can score — not a failure.
func TestADefensibleAlternativeIsLenientRatherThanWrong(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestNote, "", no(),
		Alternative{Destination: scribe.DestTicket, TicketType: "epic"})
	rep := Score("p", []Result{routed(l, scribe.DestTicket, "epic", 0.7)})

	a := rep.Accuracy(StratumReal)
	if a.Strict != 0 || a.Lenient != 1 {
		t.Fatalf("strict=%d lenient=%d, want 0 and 1", a.Strict, a.Lenient)
	}
	if a.AmbiguityTax() != 1 {
		t.Errorf("ambiguity tax = %v, want 1 — the gap is a property of the corpus", a.AmbiguityTax())
	}
	if rep.Items[0].Verdict != VerdictLenient {
		t.Errorf("verdict = %v, want lenient", rep.Items[0].Verdict)
	}
}

// An alternative that differs from the label ONLY in ticket type is already
// strict on destination. Counting it leniently would invent an ambiguity tax
// the corpus does not have.
func TestAnAlternativeThatDiffersOnlyInTypeIsStillStrict(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestTicket, "spike", no(),
		Alternative{Destination: scribe.DestTicket, TicketType: "task"})
	rep := Score("p", []Result{routed(l, scribe.DestTicket, "task", 0.7)})
	if got := rep.Accuracy(StratumReal).Strict; got != 1 {
		t.Fatalf("strict = %d, want 1", got)
	}
	if tax := rep.Accuracy(StratumReal).AmbiguityTax(); tax != 0 {
		t.Errorf("ambiguity tax = %v, want 0", tax)
	}
}

// §7: excluded from the headline accuracy AND from calibration, because their
// correctness is not unknown but undefined — the right label depends on a
// capability that does not exist.
func TestUnhandledItemsAreSetApartFromBothScores(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestTicket, "task", yes())
	l.Unhandled = []Gap{GapPageVerb}
	ok := lbl(hash(2), StratumReal, scribe.DestNote, "", yes())

	rep := Score("p", []Result{
		routed(l, scribe.DestDiscard, "", 0.95),
		routed(ok, scribe.DestNote, "", 0.95),
	})

	a := rep.Accuracy(StratumReal)
	if a.N != 1 {
		t.Errorf("scoreable n = %d, want 1 — the unhandled item is not in the denominator", a.N)
	}
	if a.Unhandled != 1 {
		t.Errorf("unhandled = %d, want 1", a.Unhandled)
	}
	if a.StrictRate() != 1 {
		t.Errorf("strict = %v, want 1", a.StrictRate())
	}
	if rep.Calibration.Scored != 1 {
		t.Errorf("calibration scored = %d, want 1 — an unhandled item has no correctness to bucket",
			rep.Calibration.Scored)
	}
	if len(rep.Unhandled) != 1 {
		t.Errorf("the finding is the list, and it is empty")
	}
}

// CHRN-32 §7's sharpest sentence: a malformed proposal is one that silently
// disappears from a batch. It is not a wrong destination — it is no
// destination — but leaving it out of the denominator would let a prompt that
// answers only the easy two thirds score a hundred percent.
func TestAMemoWithNoProposalCountsWrongAndIsReportedApart(t *testing.T) {
	rep := Score("p", []Result{
		silent(lbl(hash(1), StratumReal, scribe.DestNote, "", yes())),
		routed(lbl(hash(2), StratumReal, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
	})
	a := rep.Accuracy(StratumReal)
	if a.N != 2 || a.Strict != 1 {
		t.Fatalf("n=%d strict=%d, want 2 and 1", a.N, a.Strict)
	}
	if a.NoProposal != 1 {
		t.Errorf("no_proposal = %d, want 1", a.NoProposal)
	}
	if rep.Calibration.Skipped != 1 {
		t.Errorf("skipped = %d, want 1 — there is no confidence to band", rep.Calibration.Skipped)
	}
}

// A proposal whose project key is missing still has a destination, and
// destination accuracy does not care that a person has to supply the project.
func TestNeedsInputIsScoredOnItsDestination(t *testing.T) {
	r := routed(lbl(hash(1), StratumReal, scribe.DestTicket, "task", yes()), scribe.DestTicket, "task", 0.9)
	r.Outcome.Status = scribe.StatusNeedsInput
	rep := Score("p", []Result{r})
	a := rep.Accuracy(StratumReal)
	if a.Strict != 1 || a.NeedsInput != 1 {
		t.Fatalf("strict=%d needs_input=%d, want 1 and 1", a.Strict, a.NeedsInput)
	}
}

// §5's [rev]: calibration pools both strata to reach its n, which §2 forbids
// for accuracy. What is pooled is the relationship and not a score, and each
// band reports where its items came from so the pooling stays inspectable.
func TestCalibrationPoolsStrataButKeepsThemVisible(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl(hash(1), StratumReal, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.85),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.3),
	})
	top := rep.Calibration.Buckets[2]
	if top.N != 2 || top.Correct != 2 {
		t.Fatalf("top band n=%d correct=%d, want 2 and 2", top.N, top.Correct)
	}
	if top.ByStratum[StratumReal] != 1 || top.ByStratum[StratumSynthetic] != 1 {
		t.Errorf("per-stratum n is not visible: %v", top.ByStratum)
	}
	if low := rep.Calibration.Buckets[0]; low.N != 1 || low.Correct != 0 {
		t.Errorf("low band n=%d correct=%d, want 1 and 0", low.N, low.Correct)
	}
}

// Ruling R4. A router whose high-confidence answers are no better than its
// low-confidence ones has a confidence field that is decoration, and no
// threshold can rescue it.
func TestAFlatConfidenceLicensesNoThreshold(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.95),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.95),
		routed(lbl("c", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.2),
		routed(lbl("d", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.2),
	})
	if rep.Calibration.Licenses() {
		t.Fatal("a flat confidence licensed a threshold")
	}
	if !rep.Calibration.Monotonic {
		t.Error("50% then 50% is not a fall")
	}
	if rep.Calibration.Rising {
		t.Error("nothing rose")
	}
	if n := len(rep.Calibration.ConfidentWrong); n != 1 {
		t.Errorf("confident and wrong = %d, want 1 — that is what ACCEPT ALL would ship", n)
	}
}

func TestARisingConfidenceLicensesAThreshold(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.95),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
		routed(lbl("c", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.2),
		routed(lbl("d", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.2),
	})
	if !rep.Calibration.Licenses() {
		t.Fatalf("a rising confidence did not license a threshold: %+v", rep.Calibration)
	}
}

// One occupied band is not a trend, and calling it monotonic would be a claim
// the data does not carry.
func TestOneOccupiedBandCannotShowATrend(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.95),
	})
	if rep.Calibration.Determinable {
		t.Fatal("a single band was treated as a trend")
	}
	if rep.Calibration.Licenses() {
		t.Fatal("an undeterminable trend licensed a threshold")
	}
}

// The sweep asks scribe.Proposal.PreAcceptable rather than re-deriving
// "confidence >= min", so the two gates that are NOT a threshold still hold: a
// confident DISCARD is the one accept that cannot be undone, and it is never
// pre-acceptable at any value.
func TestTheThresholdSweepAsksTheProductionGate(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestDiscard, "", yes()), scribe.DestDiscard, "", 1.0),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 1.0),
	})
	for _, row := range rep.Thresholds {
		switch {
		case row.Min > 1 && row.PreAccepted != 0:
			t.Errorf("min %.2f pre-accepted %d, want 0 — the default admits nothing", row.Min, row.PreAccepted)
		case row.Min <= 1 && row.PreAccepted != 1:
			t.Errorf("min %.2f pre-accepted %d, want 1 — the DISCARD is never in ACCEPT ALL", row.Min, row.PreAccepted)
		}
	}
}

func TestTheSweepNamesWhatAThresholdWouldShipWrongly(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.9),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.6),
	})
	var at80, at50 ThresholdRow
	for _, row := range rep.Thresholds {
		switch row.Min {
		case 0.80:
			at80 = row
		case 0.50:
			at50 = row
		}
	}
	if at80.PreAccepted != 1 || len(at80.Wrong) != 1 || at80.Wrong[0] != "a" {
		t.Errorf("at 0.80: %+v, want one pre-accepted and it is the wrong one", at80)
	}
	if at50.PreAccepted != 2 || len(at50.Wrong) != 1 {
		t.Errorf("at 0.50: %+v, want two pre-accepted of which one is wrong", at50)
	}
	if at80.Held != 1 {
		t.Errorf("held at 0.80 = %d, want 1", at80.Held)
	}
}

// The type diagnostic is subordinate on purpose — §5 names destination
// accuracy and calibration as the two scores — and it is counted only where a
// destination miss cannot be charged twice.
func TestTicketTypeIsCountedOnlyWhenBothSidesAreTickets(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestTicket, "spike", yes()), scribe.DestTicket, "spike", 0.9),
		routed(lbl("b", StratumSynthetic, scribe.DestTicket, "spike", yes()), scribe.DestTicket, "task", 0.9),
		routed(lbl("c", StratumSynthetic, scribe.DestTicket, "task", yes()), scribe.DestNote, "", 0.9),
	})
	a := rep.Accuracy(StratumSynthetic)
	if a.TicketTyped != 2 || a.TicketMatched != 1 {
		t.Fatalf("typed=%d matched=%d, want 2 and 1", a.TicketTyped, a.TicketMatched)
	}
}

// §5: the breakdown matters more than the headline, because at this n an
// overall 88% could be one router that never gets DISCARD right.
func TestTheBreakdownShowsWhichDestinationFails(t *testing.T) {
	var results []Result
	for i := range 8 {
		l := lbl(string(rune('a'+i)), StratumSynthetic, scribe.DestNote, "", yes())
		results = append(results, routed(l, scribe.DestNote, "", 0.9))
	}
	results = append(results,
		routed(lbl("y", StratumSynthetic, scribe.DestDiscard, "", yes()), scribe.DestNote, "", 0.9))

	a := Score("p", results).Accuracy(StratumSynthetic)
	if got := a.ByLabel[scribe.DestDiscard]; got == nil || got.Strict != 0 || got.N != 1 {
		t.Fatalf("DISCARD row = %+v, want 0 of 1", got)
	}
	if got := a.Confusion[scribe.DestDiscard]["NOTE"]; got != 1 {
		t.Errorf("confusion DISCARD→NOTE = %d, want 1", got)
	}
}

// §2's run log needs the transcript each memo was scored FROM, so provenance
// survives scoring rather than being reconstructed afterwards.
func TestTheRecordCarriesTheTranscriptItWasScoredFrom(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestNote, "", yes())
	r := routed(l, scribe.DestNote, "", 0.9)
	r.Item.MemoID, r.Item.TranscriptID = uuid.New(), uuid.New()
	r.Item.Model = "whisper.cpp/small.en"

	rec := Score("p", []Result{r}).Items[0]
	if rec.TranscriptID != r.Item.TranscriptID.String() || rec.Model != "whisper.cpp/small.en" {
		t.Fatalf("record = %+v", rec)
	}
	if rec.PinMoved {
		t.Error("the pin matched and was reported moved")
	}
}

func TestAMovedPinIsCarriedIntoTheReport(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestNote, "", yes())
	r := routed(l, scribe.DestNote, "", 0.9)
	r.Item.Model = "whisper.cpp/large-v3"

	rep := Score("p", []Result{r})
	if len(rep.MovedPins) != 1 {
		t.Fatalf("moved pins = %v, want one", rep.MovedPins)
	}
}

// scribe.Parse validates ticket_type for TICKET and ignores it elsewhere, so a
// model may return one on a NOTE and still produce a valid proposal. The
// report must not then claim the router said "NOTE/task".
func TestATypeOnANonTicketIsNotReported(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "task", 0.9),
	})
	if got := rep.Items[0].Proposed.String(); got != "NOTE" {
		t.Fatalf("proposed = %q, want NOTE", got)
	}
}

// A confidently wrong DISCARD is a calibration failure AND is shipped by
// nothing, because PreAcceptable refuses DISCARD at any confidence. The report
// used to print it under "what ACCEPT ALL would actually ship" while the sweep
// four lines below correctly showed nothing being pre-accepted.
func TestAConfidentlyWrongDiscardIsCountedButNotShippable(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestDiscard, "", 0.95),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.95),
	})
	c := rep.Calibration
	if len(c.ConfidentWrong) != 2 {
		t.Fatalf("confident and wrong = %v, want both", c.ConfidentWrong)
	}
	if len(c.ConfidentWrongRefused) != 1 || c.ConfidentWrongRefused[0] != "a" {
		t.Fatalf("refused by the gate = %v, want just the DISCARD", c.ConfidentWrongRefused)
	}
	for _, row := range rep.Thresholds {
		if row.Min <= 0.95 && row.PreAccepted != 1 {
			t.Errorf("min %.2f pre-accepted %d, want 1 — the DISCARD is never in ACCEPT ALL",
				row.Min, row.PreAccepted)
		}
	}
}

// A proposal that never validated is refused by the gate too, whatever the
// confidence — except it has none, so it cannot reach the top band at all.
func TestAnInvalidProposalIsRefusedByTheGate(t *testing.T) {
	r := routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.95)
	r.Outcome.Status = scribe.StatusNeedsInput
	rep := Score("p", []Result{r})
	if len(rep.Calibration.ConfidentWrongRefused) != 1 {
		t.Fatalf("refused = %v, want the needs_input one", rep.Calibration.ConfidentWrongRefused)
	}
}

// §4: the label said NOTE, the labeller said TICKET was also defensible, the
// router said TICKET. That is the ceiling on what any router can score here,
// and it does not belong under "where the misses went".
func TestADefensibleAnswerIsNotAMiss(t *testing.T) {
	l := lbl("a", StratumSynthetic, scribe.DestNote, "", no(),
		Alternative{Destination: scribe.DestTicket, TicketType: "task"})
	wrong := lbl("b", StratumSynthetic, scribe.DestNote, "", yes())

	a := Score("p", []Result{
		routed(l, scribe.DestTicket, "task", 0.9),
		routed(wrong, scribe.DestDiscard, "", 0.9),
	}).Accuracy(StratumSynthetic)

	if got := a.Confusion[scribe.DestNote]["TICKET"]; got != 0 {
		t.Errorf("a lenient answer appears as a miss %d time(s)", got)
	}
	if got := a.Confusion[scribe.DestNote]["DISCARD"]; got != 1 {
		t.Errorf("the genuine miss = %d, want 1", got)
	}
}

// The probe exists because Rising cannot be read on a set with no mistakes: it
// asserts accuracy VARIES with confidence, which needs wrong answers. This
// measures the same thing from the other side, and survives a perfect score.
func TestTheProbeReadsCalibrationWithoutNeedingAMistake(t *testing.T) {
	unsure := func(id string, conf float64) Result {
		l := lbl(id, StratumSynthetic, scribe.DestNote, "", no(),
			Alternative{Destination: scribe.DestDiscussion})
		return routed(l, scribe.DestNote, "", conf)
	}
	sure := func(id string, conf float64) Result {
		return routed(lbl(id, StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", conf)
	}

	// Everything correct — the case that breaks Rising entirely.
	rep := Score("p", []Result{
		unsure("a", 0.65), unsure("b", 0.65),
		sure("c", 0.95), sure("d", 0.85), sure("e", 0.95),
	})
	if rep.Calibration.Rising {
		t.Fatal("a flawless run reported a rising trend")
	}
	if rep.Calibration.Licenses() {
		t.Fatal("a flawless run licensed a threshold — that is the trap this replaces")
	}

	ok, determinable := rep.Calibration.TracksTheLabeller()
	if !determinable {
		t.Fatal("the probe could not be determined on 2 unsure and 3 confident items")
	}
	if !ok {
		t.Fatalf("the probe failed on a router that hesitated exactly where the labeller did: %+v", rep.Calibration)
	}
	if rep.Calibration.UnsureMedian != 0.65 || rep.Calibration.ConfidentMedian != 0.95 {
		t.Errorf("medians = %v / %v, want 0.65 / 0.95",
			rep.Calibration.UnsureMedian, rep.Calibration.ConfidentMedian)
	}
}

// The sharpest single failure of the probe: most sure about the memo a person
// found hardest.
func TestAnUnsureFixtureInTheTopBandFailsTheProbe(t *testing.T) {
	l := lbl("a", StratumSynthetic, scribe.DestNote, "", no(),
		Alternative{Destination: scribe.DestDiscussion})
	rep := Score("p", []Result{
		routed(l, scribe.DestNote, "", 0.95),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.95),
	})
	if got := rep.Calibration.UnsureInTopBand; len(got) != 1 || got[0] != "a" {
		t.Fatalf("unsure in top band = %v, want [a]", got)
	}
	if ok, _ := rep.Calibration.TracksTheLabeller(); ok {
		t.Fatal("the probe passed with an unsure item in the top band")
	}
}

func TestTheProbeIsUndeterminableWithoutBothGroups(t *testing.T) {
	rep := Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
	})
	if _, determinable := rep.Calibration.TracksTheLabeller(); determinable {
		t.Fatal("a set with no arguable labels claimed to measure the probe")
	}
}
