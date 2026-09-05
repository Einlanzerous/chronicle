package eval

import (
	"encoding/json"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// Verdict is what one scored item came to.
type Verdict string

const (
	// VerdictStrict is the label's own destination.
	VerdictStrict Verdict = "strict"
	// VerdictLenient is a destination the labeller marked also defensible.
	// Not a router failure: it is the ceiling on what any router can score
	// against a corpus a second labeller would disagree with (§4).
	VerdictLenient Verdict = "lenient"
	VerdictWrong   Verdict = "wrong"
	// VerdictNoProposal is a memo no attempt produced a valid proposal for.
	//
	// COUNTED WRONG, and reported on its own line. It is not a wrong
	// destination — it is no destination — but excluding it from the
	// denominator would let a prompt that answers only the easy two thirds
	// score a hundred percent, and CHRN-32 §7's whole argument is that a memo
	// which produces nothing is the failure the operator does not notice.
	VerdictNoProposal Verdict = "no_proposal"
	// VerdictUnhandled is §7's set: excluded from the headline entirely,
	// because tuning a prompt against them would be tuning against a missing
	// feature.
	VerdictUnhandled Verdict = "unhandled"
)

// Correct reports whether the verdict counts as right for calibration and for
// the threshold sweep.
//
// LENIENT, not strict. Confidence is being asked to predict whether the answer
// is ACCEPTABLE, and a defensible alternative shipped by ACCEPT ALL is not a
// bad ship. Scoring calibration strictly would charge the router for the
// corpus's own ambiguity twice.
func (v Verdict) Correct() bool { return v == VerdictStrict || v == VerdictLenient }

// ItemRecord is one row of the run log (§2): what was scored, what it was
// scored FROM, and what came back.
type ItemRecord struct {
	ID      string  `json:"id"`
	Short   string  `json:"short"`
	Stratum Stratum `json:"stratum"`

	// Provenance. §2 requires the transcript each memo was scored from —
	// model and id — because a ranking-driven input change would otherwise
	// read as prompt drift.
	MemoID       string `json:"memo_id,omitempty"`
	TranscriptID string `json:"transcript_id,omitempty"`
	Model        string `json:"model,omitempty"`
	PinMoved     bool   `json:"pin_moved,omitempty"`

	Labelled       Alternative   `json:"labelled"`
	Confident      bool          `json:"labeller_confident"`
	AlsoDefensible []Alternative `json:"also_defensible,omitempty"`
	Unhandled      []Gap         `json:"unhandled,omitempty"`

	Proposed   *Alternative  `json:"proposed"`
	Confidence float64       `json:"confidence"`
	Status     scribe.Status `json:"status"`

	// Attempts is how many completions this item cost, and RunnerUp is the
	// destination the model named as its second choice.
	//
	// Attempts is the check on the grammar (see scribe.Outcome.Attempts): a
	// schema that stopped being applied would show up here and NOWHERE ELSE in
	// the report. RunnerUp is what the confidence rubric is built on — the
	// prompt makes the model name its second choice before it says how sure it
	// is — so a report that could not show it could not explain its own
	// calibration. It is read out of the raw output rather than the payload,
	// because it is a routing diagnostic and not part of CHRN-32's contract.
	Attempts int    `json:"attempts"`
	RunnerUp string `json:"runner_up,omitempty"`

	// Reason is the model's own argument for the destination.
	//
	// CARRIED BECAUSE PROMPT WORK IS IMPOSSIBLE WITHOUT IT: a report that says
	// an item was wrong and not why sends you back to re-run it by hand.
	//
	// It also changes a property this record used to have, and the change is
	// deliberate rather than overlooked. Every other field here is a
	// destination, a status or a count; a reason is MODEL OUTPUT ABOUT A
	// TRANSCRIPT and will quote it. So a `--json` from the `real` stratum is
	// corpus-adjacent in a way it was not before — §1 is a rule about git and
	// this file is not in git, but it should not be pasted into a PR either,
	// and the repo ignores `*.eval.json` so the accident needs a -f.
	Reason string `json:"reason,omitempty"`

	// ClearedCount and Cleared are how many fields stage 2 removed, and which.
	// 0007 states the purpose as the hallucination rate this harness reports;
	// the count aggregates and the values are what you read to find out what
	// the model invented.
	ClearedCount int                   `json:"cleared_count"`
	Cleared      []scribe.ClearedField `json:"cleared_fields,omitempty"`

	Error string `json:"error,omitempty"`

	Verdict Verdict `json:"verdict"`
}

// DestStat is one labelled destination's row in the breakdown.
type DestStat struct {
	N          int `json:"n"`
	Strict     int `json:"strict"`
	Lenient    int `json:"lenient"`
	NoProposal int `json:"no_proposal"`
}

// Accuracy is ONE STRATUM's destination score. There is deliberately no type in
// this package that holds a score over more than one (§2).
type Accuracy struct {
	Stratum Stratum `json:"stratum"`

	// N is the scoreable count: everything in the stratum except §7's
	// unhandled items.
	N       int `json:"n"`
	Strict  int `json:"strict"`
	Lenient int `json:"lenient"`

	NoProposal int `json:"no_proposal"`
	NeedsInput int `json:"needs_input"`
	Cleared    int `json:"cleared_fields"`
	Unhandled  int `json:"unhandled"`

	// Retried is how many items cost more than one completion, and Attempts is
	// the total spent. Reported because a retry rate at or near zero is the
	// only evidence that the JSON schema passed as `format` is actually
	// constraining the model — if it silently stopped, stage 1 would start
	// firing again and every other number here would be unchanged.
	Retried  int `json:"retried"`
	Attempts int `json:"attempts"`

	// ByLabel is the per-destination breakdown, keyed by the LABEL's
	// destination. §5: it matters more than the headline, because at this n an
	// overall 88% could be one router that never gets DISCARD right.
	ByLabel map[scribe.Destination]*DestStat `json:"by_label"`

	// Confusion is labelled destination -> what was proposed instead, counted.
	// "(none)" is the no-proposal case, kept in the matrix so a router that
	// fails on exactly one destination is visible rather than averaged away.
	Confusion map[scribe.Destination]map[string]int `json:"confusion"`

	// The ticket-type diagnostic, subordinate on purpose: §5 names destination
	// accuracy and calibration as the two scores, and this is neither. It is
	// counted over items whose label AND proposal are both TICKET.
	TicketTyped   int `json:"ticket_typed"`
	TicketMatched int `json:"ticket_type_matched"`
}

// StrictRate and LenientRate are proportions, or zero on an empty stratum.
func (a *Accuracy) StrictRate() float64  { return rate(a.Strict, a.N) }
func (a *Accuracy) LenientRate() float64 { return rate(a.Lenient, a.N) }

// AmbiguityTax is the gap between lenient and strict: a property of the corpus
// rather than of the prompt. A prompt change that moves strict without moving
// lenient has learned the labeller's preferences, not the task.
func (a *Accuracy) AmbiguityTax() float64 { return a.LenientRate() - a.StrictRate() }

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// Bucket is one confidence band.
type Bucket struct {
	Name string  `json:"name"`
	Lo   float64 `json:"lo"`
	Hi   float64 `json:"hi"`

	N       int `json:"n"`
	Correct int `json:"correct"`

	// ByStratum keeps the pooling inspectable (§5's [rev]): calibration pools
	// both strata to reach its n, so each bucket reports where its items came
	// from beside the total. What is pooled is the confidence-correctness
	// relationship and not a score — no blended accuracy falls out of it.
	ByStratum map[Stratum]int `json:"by_stratum"`
}

func (b Bucket) Rate() float64 { return rate(b.Correct, b.N) }

// bucketBounds is §5's three bands, chosen because the usual instruments are
// noise at n~46. Expected calibration error over ten bins on forty items
// measures binning, not calibration.
var bucketBounds = []Bucket{
	{Name: "< 0.50", Lo: 0, Hi: 0.5},
	{Name: "0.50 - 0.80", Lo: 0.5, Hi: 0.8},
	{Name: ">= 0.80", Lo: 0.8, Hi: 1.0001},
}

// Calibration is the second score, and it is the one that licenses ACCEPT ALL.
type Calibration struct {
	Buckets []Bucket `json:"buckets"`

	Scored int `json:"scored"`
	// Skipped is items with no proposal: there is no confidence to bucket.
	Skipped int `json:"skipped"`

	// Determinable is false with fewer than two occupied buckets — one band
	// cannot show a trend, and saying "monotonic" about it would be a claim
	// the data does not carry.
	Determinable bool `json:"determinable"`
	// Monotonic is non-decreasing accuracy across occupied buckets.
	Monotonic bool `json:"monotonic"`
	// Rising is the strict version: the top occupied bucket beats the bottom.
	// A router whose high-confidence answers are no better than its
	// low-confidence ones has a confidence field that is decoration, and no
	// threshold can rescue it.
	Rising bool `json:"rising"`

	// ConfidentWrong is items in the top band that were wrong. Reported as a
	// count AND a list, because at this n the identities are more useful than
	// the proportion.
	//
	// IT IS A CALIBRATION NUMBER AND NOT A SHIPPING NUMBER, and the two are
	// not the same set: a confidently wrong DISCARD is a calibration failure
	// and is shipped by nothing, because PreAcceptable refuses DISCARD at any
	// confidence. Conflating them would report the irreversible accept as
	// something ACCEPT ALL admits, which is the one thing CHRN-32 §4 exists to
	// say it does not.
	ConfidentWrong []string `json:"confident_wrong"`
	// The LABELLER-UNCERTAINTY PROBE, and the reason it exists is that Rising
	// cannot be measured on a set with no mistakes.
	//
	// `Licenses()` asserts accuracy VARIES with confidence, which structurally
	// needs wrong answers to demonstrate. On the synthetic stratum a good
	// router plausibly gets everything right, every occupied band sits at
	// 100%, and Rising is false — the claim fails because the router made no
	// mistakes. These three measure the same thing from the other side: the
	// corpus already records where a person was unsure (§4's `confident:
	// false` plus `also_defensible`), and a calibrated model should be least
	// sure exactly there. That signal survives a perfect score.
	//
	// Medians rather than means: at n=4 unsure items one outlier moves a mean
	// and moves nothing that matters.
	UnsureN         int     `json:"unsure_n"`
	UnsureMedian    float64 `json:"unsure_median"`
	ConfidentN      int     `json:"confident_n"`
	ConfidentMedian float64 `json:"confident_median"`
	// UnsureInTopBand names labeller-unsure items the router was most sure
	// about — the sharpest single failure of this probe.
	UnsureInTopBand []string `json:"unsure_in_top_band"`

	// ConfidentWrongRefused is the subset the production gate declines whatever
	// the threshold — a DISCARD, or a proposal that is not `valid`. Asking
	// PreAcceptable at a minimum of 0 answers exactly that, since every
	// confidence clears 0 and only the two non-threshold gates can still
	// refuse. What is left is what a confidence threshold alone would admit.
	ConfidentWrongRefused []string `json:"confident_wrong_refused"`
}

// Licenses reports R4's question: does the evidence support shipping a
// threshold at all?
//
// If accuracy does not rise with confidence there is no value at which
// pre-accepting beats not pre-accepting, and shipping a number anyway spends
// trust the router has not earned. The answer then is to RAISE
// CHRONICLE_SCRIBE_PREACCEPT_MIN back above 1, which admits nothing, and wait
// for a prompt that calibrates.
//
// "Back above" rather than "leave at": the compiled default was 1.01 until
// CHRN-36's run of 2026-09-03 measured a rising calibration and set it to 0.80.
// A later run that goes flat is therefore a REGRESSION to undo, not a state to
// preserve, and this sentence has to say so or it will read as reassurance.
func (c Calibration) Licenses() bool { return c.Determinable && c.Monotonic && c.Rising }

// TracksTheLabeller reports the probe: is the router less sure where a person
// was unsure, and did it stay out of the top band on all of them?
//
// Undeterminable with either group empty, which is honest rather than a pass:
// a set with no arguable labels cannot say anything about this.
func (c Calibration) TracksTheLabeller() (ok, determinable bool) {
	if c.UnsureN == 0 || c.ConfidentN == 0 {
		return false, false
	}
	return c.UnsureMedian < c.ConfidentMedian && len(c.UnsureInTopBand) == 0, true
}

// ThresholdRow is one candidate value of CHRONICLE_SCRIBE_PREACCEPT_MIN and
// what it would have shipped over this run.
type ThresholdRow struct {
	Min float64 `json:"min"`
	// PreAccepted counts items scribe.Proposal.PreAcceptable admits.
	PreAccepted int `json:"pre_accepted"`
	// Wrong is how many of those were not an answer the labeller accepts. This
	// is the number the threshold is chosen against.
	Wrong []string `json:"wrong"`
	// Held is scoreable items the threshold would leave for a manual tap.
	Held int `json:"held"`
}

// thresholdLadder is the sweep. It ends at 1.01 — a value no confidence can
// reach — so R4's answer is a row in the same table as every alternative to it,
// rather than a footnote.
//
// That row is NOT the compiled default any more; it was until CHRN-36's run set
// the default to 0.80, and report.go annotates the two separately for exactly
// that reason. The ladder must also CONTAIN the compiled default, or the
// sweep cannot show what is actually shipping — asserted in score_test.go,
// because the coupling is otherwise implicit and silently breakable.
var thresholdLadder = []float64{0.50, 0.60, 0.70, 0.75, 0.80, 0.85, 0.90, 0.95, 1.01}

// Report is one scored run, and it IS §2's run log entry: date, proposer,
// result, and the transcript each memo was scored from.
//
// Strata is a slice and there is no Overall field. That is the enforcement of
// "never averaged" (§2) at the only place it can be enforced — a number that
// does not exist cannot be quoted.
type Report struct {
	RunAt        time.Time `json:"run_at"`
	Proposer     string    `json:"proposer"`
	LabelsPath   string    `json:"labels_path"`
	LabelsSHA256 string    `json:"labels_sha256"`

	// ModelDigest and CatalogueSHA256 are the other two inputs a run is a
	// function of, recorded for the same reason the labels hash and the
	// transcript pin are. The proposer names the model TAG, which is mutable:
	// re-pull and the string is unchanged while the weights are not. And the
	// catalogue decides which project keys were available to be answered, so
	// changing it moves every TICKET proposal while the labels hash sits still.
	ModelDigest     string `json:"model_digest,omitempty"`
	CatalogueSHA256 string `json:"catalogue_sha256,omitempty"`

	Strata []*Accuracy `json:"strata"`

	Calibration Calibration    `json:"calibration"`
	Thresholds  []ThresholdRow `json:"thresholds"`

	Items []ItemRecord `json:"items"`

	// Unhandled is §7's list, carried whole: the count is the useful output.
	Unhandled []ItemRecord `json:"unhandled_items"`

	// Failures are labels that never became items — a hash the corpus does not
	// have, a fixture that is missing — plus routers that could not be reached.
	Failures []string `json:"failures,omitempty"`
	// MovedPins names items whose transcript is not the one the labeller read.
	MovedPins []string `json:"moved_pins,omitempty"`
}

// Accuracy returns one stratum's score, or nil. There is no argument that
// returns a combined one.
func (r *Report) Accuracy(s Stratum) *Accuracy {
	for _, a := range r.Strata {
		if a.Stratum == s {
			return a
		}
	}
	return nil
}

// Score turns routed results into a report.
//
// The ORDER of the two exclusions matters and is §7's: an unhandled item is set
// aside BEFORE anything else looks at it, so it reaches neither the headline
// accuracy nor the calibration buckets. Its correctness is not merely unknown,
// it is undefined — the right label depends on a capability that does not
// exist.
func Score(proposer string, results []Result) *Report {
	rep := &Report{Proposer: proposer}

	byStratum := map[Stratum]*Accuracy{}
	for _, s := range Strata {
		byStratum[s] = &Accuracy{
			Stratum:   s,
			ByLabel:   map[scribe.Destination]*DestStat{},
			Confusion: map[scribe.Destination]map[string]int{},
		}
	}

	buckets := slices.Clone(bucketBounds)
	for i := range buckets {
		buckets[i].ByStratum = map[Stratum]int{}
	}

	// Kept for the threshold sweep, which needs the payload and status
	// together and must ask scribe.Proposal.PreAcceptable rather than
	// re-deriving the gate.
	type shipped struct {
		rec    ItemRecord
		p      *scribe.Proposal
		status scribe.Status
	}
	var shippable []shipped

	// The labeller-uncertainty probe's raw material, gathered over scored
	// items only: an unhandled item has no correctness and a memo with no
	// proposal has no confidence to compare.
	var unsureConf, confidentConf []float64
	var unsureTop []string

	for _, res := range results {
		rec := record(res)
		acc := byStratum[rec.Stratum]

		if rec.Verdict == VerdictUnhandled {
			if acc != nil {
				acc.Unhandled++
			}
			rep.Unhandled = append(rep.Unhandled, rec)
			rep.Items = append(rep.Items, rec)
			if rec.PinMoved {
				rep.MovedPins = append(rep.MovedPins, rec.Short)
			}
			continue
		}

		if acc != nil {
			acc.N++
			acc.Cleared += rec.ClearedCount
			acc.Attempts += rec.Attempts
			if rec.Attempts > 1 {
				acc.Retried++
			}
			if rec.Status == scribe.StatusNeedsInput {
				acc.NeedsInput++
			}
			d := acc.ByLabel[rec.Labelled.Destination]
			if d == nil {
				d = &DestStat{}
				acc.ByLabel[rec.Labelled.Destination] = d
			}
			d.N++

			// ONLY REAL MISSES. A lenient answer is off-diagonal too — the
			// label said NOTE, also_defensible said TICKET, the router said
			// TICKET — and §4 is explicit that this IS NOT A ROUTER FAILURE.
			// Listing it under "where the misses went" would charge the router
			// for the corpus's ambiguity in the one place the rest of this
			// report is careful not to. The ambiguity tax already reports it.
			if rec.Verdict == VerdictWrong || rec.Verdict == VerdictNoProposal {
				proposed := "(none)"
				if rec.Proposed != nil {
					proposed = string(rec.Proposed.Destination)
				}
				row := acc.Confusion[rec.Labelled.Destination]
				if row == nil {
					row = map[string]int{}
					acc.Confusion[rec.Labelled.Destination] = row
				}
				row[proposed]++
			}

			switch rec.Verdict {
			case VerdictStrict:
				acc.Strict++
				acc.Lenient++
				d.Strict++
				d.Lenient++
			case VerdictLenient:
				acc.Lenient++
				d.Lenient++
			case VerdictNoProposal:
				acc.NoProposal++
				d.NoProposal++
			}

			// The subordinate type diagnostic: only where both sides are
			// TICKET, so a destination miss is never counted twice.
			if rec.Labelled.Destination == scribe.DestTicket && rec.Proposed != nil &&
				rec.Proposed.Destination == scribe.DestTicket {
				acc.TicketTyped++
				if typeAccepted(res.Item.Label, rec.Proposed.TicketType) {
					acc.TicketMatched++
				}
			}
		}

		if p := res.Proposal(); p != nil {
			for i := range buckets {
				if rec.Confidence >= buckets[i].Lo && rec.Confidence < buckets[i].Hi {
					buckets[i].N++
					buckets[i].ByStratum[rec.Stratum]++
					if rec.Verdict.Correct() {
						buckets[i].Correct++
					} else if i == len(buckets)-1 {
						rep.Calibration.ConfidentWrong = append(rep.Calibration.ConfidentWrong, rec.Short)
						if !p.PreAcceptable(rec.Status, 0) {
							rep.Calibration.ConfidentWrongRefused =
								append(rep.Calibration.ConfidentWrongRefused, rec.Short)
						}
					}
					break
				}
			}
			rep.Calibration.Scored++
			shippable = append(shippable, shipped{rec: rec, p: p, status: rec.Status})

			topLo := buckets[len(buckets)-1].Lo
			if rec.Confident {
				confidentConf = append(confidentConf, rec.Confidence)
			} else {
				unsureConf = append(unsureConf, rec.Confidence)
				if rec.Confidence >= topLo {
					unsureTop = append(unsureTop, rec.Short)
				}
			}
		} else {
			rep.Calibration.Skipped++
		}

		rep.Items = append(rep.Items, rec)
		if rec.PinMoved {
			rep.MovedPins = append(rep.MovedPins, rec.Short)
		}
		if res.Err != nil {
			rep.Failures = append(rep.Failures, rec.Short+": "+res.Err.Error())
		}
	}

	for _, s := range Strata {
		if a := byStratum[s]; a.N > 0 || a.Unhandled > 0 {
			rep.Strata = append(rep.Strata, a)
		}
	}

	rep.Calibration.Buckets = buckets
	rep.Calibration.Determinable, rep.Calibration.Monotonic, rep.Calibration.Rising = trend(buckets)
	rep.Calibration.UnsureN, rep.Calibration.UnsureMedian = len(unsureConf), median(unsureConf)
	rep.Calibration.ConfidentN, rep.Calibration.ConfidentMedian = len(confidentConf), median(confidentConf)
	rep.Calibration.UnsureInTopBand = unsureTop

	scoreable := 0
	for _, a := range rep.Strata {
		scoreable += a.N
	}
	for _, min := range thresholdLadder {
		row := ThresholdRow{Min: min}
		for _, s := range shippable {
			// THE PRODUCTION GATE, ASKED RATHER THAN COPIED. PreAcceptable
			// also refuses DISCARD at any confidence and refuses anything not
			// `valid`, and an eval that re-derived "confidence >= min" would
			// report a threshold for a rule nobody ships.
			if s.p.PreAcceptable(s.status, min) {
				row.PreAccepted++
				if !s.rec.Verdict.Correct() {
					row.Wrong = append(row.Wrong, s.rec.Short)
				}
			}
		}
		row.Held = scoreable - row.PreAccepted
		rep.Thresholds = append(rep.Thresholds, row)
	}

	return rep
}

// record flattens one result into its row.
func record(res Result) ItemRecord {
	l := res.Item.Label
	rec := ItemRecord{
		ID:             l.ID(),
		Short:          l.Short(),
		Stratum:        l.Stratum,
		Model:          res.Item.Model,
		PinMoved:       res.Item.PinMoved(),
		Labelled:       Alternative{Destination: l.Destination, TicketType: l.TicketType},
		Confident:      l.IsConfident(),
		AlsoDefensible: l.AlsoDefensible,
		Unhandled:      l.Unhandled,
		Status:         res.Outcome.Status,
		ClearedCount:   len(res.Outcome.Cleared),
		Cleared:        res.Outcome.Cleared,
		Attempts:       res.Outcome.Attempts,
		RunnerUp:       runnerUp(res.Outcome.Raw),
	}
	if res.Item.MemoID != uuid.Nil {
		rec.MemoID = res.Item.MemoID.String()
		rec.TranscriptID = res.Item.TranscriptID.String()
	}
	switch {
	case res.Err != nil:
		rec.Error = res.Err.Error()
	case res.Outcome.Err != nil:
		rec.Error = res.Outcome.Err.Error()
	}

	if p := res.Proposal(); p != nil {
		// The type is carried only where it means something. scribe.Parse
		// validates ticket_type for TICKET and ignores it elsewhere, so a
		// model that answers `{"destination":"NOTE","ticket_type":"task"}`
		// produces a valid proposal — and rendering that as `NOTE/task` would
		// report a field the contract does not read.
		alt := Alternative{Destination: p.Destination}
		if p.Destination == scribe.DestTicket {
			alt.TicketType = p.TicketType
		}
		rec.Proposed = &alt
		rec.Confidence = p.Confidence
		rec.Reason = p.Reason
	}

	switch {
	case len(l.Unhandled) > 0:
		rec.Verdict = VerdictUnhandled
	case rec.Proposed == nil:
		rec.Verdict = VerdictNoProposal
	case rec.Proposed.Destination == l.Destination:
		rec.Verdict = VerdictStrict
	case defensible(l, rec.Proposed.Destination):
		rec.Verdict = VerdictLenient
	default:
		rec.Verdict = VerdictWrong
	}
	return rec
}

// runnerUp reads the model's second choice out of its raw answer.
//
// From the RAW rather than the payload, deliberately: `runner_up` shapes the
// confidence and is a diagnostic this harness reports, but it is not part of
// CHRN-32's proposal contract and this package does not get to extend that
// contract by the back door. A missing or unparseable value is simply absent.
func runnerUp(raw []byte) string {
	var v struct {
		RunnerUp string `json:"runner_up"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return v.RunnerUp
}

// defensible reports whether the labeller marked this destination acceptable.
//
// Destination only: an alternative that differs from the label ONLY in ticket
// type — "spike or task" — is already strict on destination, and charging it
// as lenient would invent an ambiguity tax the corpus does not have.
func defensible(l Label, d scribe.Destination) bool {
	for _, a := range l.AlsoDefensible {
		if a.Destination == d {
			return true
		}
	}
	return false
}

// typeAccepted reports whether a proposed ticket type is one the labeller would
// accept: the label's own, or an alternative that is also a TICKET.
func typeAccepted(l Label, t string) bool {
	for _, a := range l.Answers() {
		if a.Destination == scribe.DestTicket && a.TicketType == t {
			return true
		}
	}
	return false
}

// median of a small sample. Sorts a copy: the caller's slice order is not this
// function's to change.
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	c := slices.Clone(xs)
	slices.Sort(c)
	if n := len(c); n%2 == 1 {
		return c[n/2]
	}
	return (c[len(c)/2-1] + c[len(c)/2]) / 2
}

// trend answers §5's actual question — does accuracy rise with confidence at
// all — over the occupied buckets only.
//
// An empty bucket is not evidence of anything, so it is skipped rather than
// treated as zero accuracy; treating it as zero would manufacture a trend out
// of a band nothing landed in.
func trend(buckets []Bucket) (determinable, monotonic, rising bool) {
	var rates []float64
	for _, b := range buckets {
		if b.N > 0 {
			rates = append(rates, b.Rate())
		}
	}
	if len(rates) < 2 {
		return false, false, false
	}
	monotonic = true
	for i := 1; i < len(rates); i++ {
		if rates[i] < rates[i-1] {
			monotonic = false
			break
		}
	}
	return true, monotonic, rates[len(rates)-1] > rates[0]
}
