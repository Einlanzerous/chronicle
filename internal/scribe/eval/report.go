package eval

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// Rendering. The report is written as markdown-flavoured plain text on purpose:
// §2 requires EVERY scoring run against the held-out stratum to be recorded in
// the decision document, and a report that has to be reformatted before it can
// be pasted is a report that gets summarised from memory instead.
//
// One renderer, therefore, and not a terminal format plus a document format
// that drift apart.

// Render writes the whole report.
func (r *Report) Render(w io.Writer) {
	pf(w, "# Routing eval — %s\n\n", r.RunAt.Format("2006-01-02 15:04"))
	pf(w, "- proposer: `%s`\n", or(r.Proposer, "(none)"))
	pf(w, "- labels:   `%s`", r.LabelsPath)
	if r.LabelsSHA256 != "" {
		pf(w, " (sha256 %s)", r.LabelsSHA256[:12])
	}
	nl(w)
	pf(w, "- items:    %d scored, %d unhandled, %d failed\n\n",
		len(r.Items)-len(r.Unhandled), len(r.Unhandled), len(r.Failures))

	r.renderMovedPins(w)
	r.renderAccuracy(w)
	r.renderCalibration(w)
	r.renderThresholds(w)
	r.renderUnhandled(w)
	r.renderRunLog(w)
	r.renderFailures(w)
}

// renderMovedPins is first in the report and not last, because everything below
// it means something different if the input text moved (§1).
func (r *Report) renderMovedPins(w io.Writer) {
	if len(r.MovedPins) == 0 {
		return
	}
	pf(w, "## ⚠ THE TRANSCRIPT PIN MOVED ON %d ITEM(S)\n\n", len(r.MovedPins))
	pf(w, "%s\n\n", strings.Join(r.MovedPins, ", "))
	ps(w, "These were scored from a transcript other than the one the labeller read.\n"+
		"The content hash still matches — it identifies the AUDIO — but the words changed,\n"+
		"so this run is not comparable to the ones before it and the labels' reasons were\n"+
		"written about different text. Re-label, or read this as a new baseline.\n\n")
}

func (r *Report) renderAccuracy(w io.Writer) {
	ps(w, "## Destination accuracy\n\n")
	if len(r.Strata) == 0 {
		ps(w, "Nothing scored.\n\n")
		return
	}
	// PER STRATUM, NEVER AVERAGED (§2). There is no total row here because
	// there is no total in the type.
	for _, a := range r.Strata {
		pf(w, "### `%s` — n=%d\n\n", a.Stratum, a.N)
		if a.N == 0 {
			pf(w, "No scoreable items (%d unhandled).\n\n", a.Unhandled)
			continue
		}
		pf(w, "- strict:  %d/%d  (%s)\n", a.Strict, a.N, pct(a.StrictRate()))
		pf(w, "- lenient: %d/%d  (%s)\n", a.Lenient, a.N, pct(a.LenientRate()))
		pf(w, "- ambiguity tax: %s — a property of the corpus, not the prompt\n", pct(a.AmbiguityTax()))
		pf(w, "- no proposal: %d · needs_input: %d · fields cleared: %d · unhandled (set apart): %d\n\n",
			a.NoProposal, a.NeedsInput, a.Cleared, a.Unhandled)

		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		psln(tw, "  label\tn\tstrict\tlenient\tno proposal")
		for _, d := range scribe.Destinations {
			s := a.ByLabel[d]
			if s == nil {
				continue
			}
			pf(tw, "  %s\t%d\t%d\t%d\t%d\n", d, s.N, s.Strict, s.Lenient, s.NoProposal)
		}
		flush(tw)
		nl(w)

		if a.TicketTyped > 0 {
			pf(w, "Ticket type (subordinate — destination is the score): %d/%d of the TICKETs\n"+
				"routed as TICKET carried a type the labeller accepts.\n\n", a.TicketMatched, a.TicketTyped)
		}
		r.renderConfusion(w, a)
	}
}

// renderConfusion prints where the wrong answers went. §5: the breakdown
// matters more than the headline, because at this n an overall 88% could be one
// router that never gets DISCARD right.
func (r *Report) renderConfusion(w io.Writer, a *Accuracy) {
	rows := 0
	for d, row := range a.Confusion {
		for proposed := range row {
			if proposed != string(d) {
				rows++
			}
		}
	}
	if rows == 0 {
		return
	}
	ps(w, "Where the misses went:\n\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, d := range scribe.Destinations {
		row := a.Confusion[d]
		if row == nil {
			continue
		}
		var proposed []string
		for k := range row {
			if k != string(d) {
				proposed = append(proposed, k)
			}
		}
		if len(proposed) == 0 {
			continue
		}
		sort.Strings(proposed)
		var parts []string
		for _, p := range proposed {
			parts = append(parts, fmt.Sprintf("%s×%d", p, row[p]))
		}
		pf(tw, "  %s\t→\t%s\n", d, strings.Join(parts, ", "))
	}
	flush(tw)
	nl(w)
}

func (r *Report) renderCalibration(w io.Writer) {
	c := r.Calibration
	ps(w, "## Calibration — does confidence predict correctness\n\n")
	ps(w, "Pooled across strata to reach its n, which §2 forbids for accuracy and §5\n"+
		"allows here: what is pooled is the confidence–correctness relationship, not a\n"+
		"score. Each band reports where its items came from. Correctness is LENIENT.\n\n")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	psln(tw, "  confidence\tn\tcorrect\taccuracy\tby stratum")
	for _, b := range c.Buckets {
		acc := "—"
		if b.N > 0 {
			acc = pct(b.Rate())
		}
		pf(tw, "  %s\t%d\t%d\t%s\t%s\n", b.Name, b.N, b.Correct, acc, strata(b.ByStratum))
	}
	flush(tw)
	nl(w)
	if c.Skipped > 0 {
		pf(w, "%d item(s) had no proposal and so no confidence to band.\n\n", c.Skipped)
	}

	switch {
	case !c.Determinable:
		ps(w, "**Trend: cannot be determined.** Fewer than two bands are occupied, so\n"+
			"nothing here says whether accuracy rises with confidence.\n\n")
	case c.Monotonic && c.Rising:
		ps(w, "**Trend: accuracy rises with confidence.** The confidence field carries signal.\n\n")
	case c.Monotonic:
		ps(w, "**Trend: FLAT.** Accuracy never falls, but the top band is no better than the\n"+
			"bottom. A confidence field whose high answers are no better than its low ones is\n"+
			"decoration, and no threshold can rescue it.\n\n")
	default:
		ps(w, "**Trend: NOT MONOTONIC.** Accuracy falls somewhere as confidence rises.\n\n")
	}

	pf(w, "Confident and wrong (top band): **%d**", len(c.ConfidentWrong))
	if len(c.ConfidentWrong) > 0 {
		pf(w, " — %s", strings.Join(c.ConfidentWrong, ", "))
	}
	nl(w)
	if n := len(c.ConfidentWrongRefused); n > 0 {
		pf(w, "Of those, %d cannot be shipped at ANY threshold (a DISCARD, or not `valid`): %s.\n"+
			"A confidence threshold alone would admit the remaining %d.\n",
			n, strings.Join(c.ConfidentWrongRefused, ", "), len(c.ConfidentWrong)-n)
	}
	ps(w, "This is a calibration number rather than a shipping number. What ACCEPT ALL\n"+
		"would actually ship at each candidate value is the sweep below.\n\n")
}

func (r *Report) renderThresholds(w io.Writer) {
	ps(w, "## The threshold\n\n")

	if !r.Calibration.Licenses() {
		// RULING R4, stated by the harness rather than left for a human to
		// remember. If accuracy does not rise with confidence there is no
		// value at which pre-accepting beats not pre-accepting.
		ps(w, "**No threshold ships (R4).** Calibration does not show accuracy rising with\n"+
			"confidence, so leave `CHRONICLE_SCRIBE_PREACCEPT_MIN` at its 1.01 default, which\n"+
			"admits nothing. ACCEPT ALL waits for a prompt that calibrates — a CHRN-30 problem\n"+
			"and not a threshold problem. The sweep below is printed as evidence, not as a menu.\n\n")
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	psln(tw, "  min\tpre-accepted\tof those wrong\theld for a tap")
	for _, row := range r.Thresholds {
		note := ""
		if row.Min > 1 {
			note = "  ← the default: admits nothing"
		}
		pf(tw, "  %.2f\t%d\t%d\t%d%s\n", row.Min, row.PreAccepted, len(row.Wrong), row.Held, note)
	}
	flush(tw)
	ps(w, "\nDISCARD is absent from every row at every value: it is never pre-acceptable,\n"+
		"and the sweep asks scribe.Proposal.PreAcceptable rather than re-deriving the gate.\n\n")
}

func (r *Report) renderUnhandled(w io.Writer) {
	if len(r.Unhandled) == 0 {
		return
	}
	ps(w, "## Known-unhandled cases (§7) — a finding, not a score\n\n")
	ps(w, "These want a capability the contract does not have, so they are excluded from\n"+
		"the headline accuracy and from calibration. No percentage is computed over them:\n"+
		"tuning a prompt against them would be tuning against a missing feature.\n\n")

	byGap := map[Gap][]string{}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	psln(tw, "  item\tstratum\tgap\tlabelled\tproposed")
	for _, it := range r.Unhandled {
		var gaps []string
		for _, g := range it.Unhandled {
			gaps = append(gaps, string(g))
			byGap[g] = append(byGap[g], it.Short)
		}
		proposed := "(none)"
		if it.Proposed != nil {
			proposed = it.Proposed.String()
		}
		pf(tw, "  %s\t%s\t%s\t%s\t%s\n", it.Short, it.Stratum, strings.Join(gaps, "+"),
			it.Labelled, proposed)
	}
	flush(tw)
	nl(w)
	for _, g := range Gaps {
		if n := len(byGap[g]); n > 0 {
			pf(w, "- `%s`: %d item(s)\n", g, n)
		}
	}
	nl(w)
}

// renderRunLog is §2's requirement made mechanical: date, proposer, result, and
// THE TRANSCRIPT EACH MEMO WAS SCORED FROM. Real stratum only — the synthetic
// half is development material and does not degrade by being looked at.
func (r *Report) renderRunLog(w io.Writer) {
	var real []ItemRecord
	for _, it := range r.Items {
		if it.Stratum == StratumReal {
			real = append(real, it)
		}
	}
	if len(real) == 0 {
		return
	}
	ps(w, "## Run log entry (paste into the decision, §2)\n\n")
	ps(w, "A held-out set degrades every time it is looked at. This block is the record\n"+
		"that keeps run N comparable to run N−1.\n\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	psln(tw, "  memo\tmodel\ttranscript\tlabelled\tproposed\tconf\tverdict")
	for _, it := range real {
		proposed, conf := "(none)", "—"
		if it.Proposed != nil {
			proposed = it.Proposed.String()
			conf = fmt.Sprintf("%.2f", it.Confidence)
		}
		tid := it.TranscriptID
		if len(tid) > 8 {
			tid = tid[:8]
		}
		pf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			it.Short, or(it.Model, "—"), or(tid, "—"), it.Labelled, proposed, conf, it.Verdict)
	}
	flush(tw)
	nl(w)
}

func (r *Report) renderFailures(w io.Writer) {
	if len(r.Failures) == 0 {
		return
	}
	ps(w, "## Failures\n\n")
	for _, f := range r.Failures {
		pf(w, "- %s\n", f)
	}
	nl(w)
}

// RenderResolution is the --dry-run report: what resolves, from what, and
// whether the pins still hold. It needs no model, which is what makes it the
// half of this harness that can run today.
func RenderResolution(w io.Writer, res Resolution, labels []Label) {
	ps(w, "# Routing eval — resolution check\n\n")
	pf(w, "%d label(s): %d resolved, %d unresolved.\n\n",
		len(labels), len(res.Items), len(res.Failures))

	counts := map[Stratum]map[scribe.Destination]int{}
	for _, l := range labels {
		if counts[l.Stratum] == nil {
			counts[l.Stratum] = map[scribe.Destination]int{}
		}
		counts[l.Stratum][l.Destination]++
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	ps(tw, "  stratum\t")
	for _, d := range scribe.Destinations {
		pf(tw, "%s\t", d)
	}
	psln(tw, "total\tunsure\tunhandled")
	for _, s := range Strata {
		if counts[s] == nil {
			continue
		}
		total, unsure, unhandled := 0, 0, 0
		for _, l := range labels {
			if l.Stratum != s {
				continue
			}
			total++
			if !l.IsConfident() {
				unsure++
			}
			if len(l.Unhandled) > 0 {
				unhandled++
			}
		}
		pf(tw, "  %s\t", s)
		for _, d := range scribe.Destinations {
			pf(tw, "%d\t", counts[s][d])
		}
		pf(tw, "%d\t%d\t%d\n", total, unsure, unhandled)
	}
	flush(tw)
	nl(w)

	if items := res.Items; len(items) > 0 {
		ps(w, "## Resolved\n\n")
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		psln(tw, "  item\tstratum\tmodel\ttranscript\tchars\tpin")
		for _, it := range items {
			tid := it.TranscriptID.String()
			if it.Label.Stratum == StratumSynthetic {
				tid = "—"
			} else if len(tid) > 8 {
				tid = tid[:8]
			}
			pin := "ok"
			switch {
			case it.Label.Stratum == StratumSynthetic:
				pin = "n/a"
			case it.PinMoved():
				pin = "MOVED — labelled against " + it.Label.LabelledAgainst
			}
			pf(tw, "  %s\t%s\t%s\t%s\t%d\t%s\n",
				it.Label.Short(), it.Label.Stratum, or(it.Model, "—"), tid, len(it.Text), pin)
		}
		flush(tw)
		nl(w)
	}

	if len(res.Failures) > 0 {
		ps(w, "## Unresolved\n\n")
		for _, f := range res.Failures {
			pf(w, "- %s (%s): %v\n", f.Label.Short(), f.Label.Stratum, f.Err)
		}
		nl(w)
	}
}

// Rendering writes to a terminal or to a strings.Builder, and a short write to
// either is not something a report can do anything about. These make the
// discard explicit in one place rather than leaving fifty unchecked returns.
func ps(w io.Writer, s string)                { _, _ = io.WriteString(w, s) }
func psln(w io.Writer, s string)              { _, _ = io.WriteString(w, s+"\n") }
func pf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
func nl(w io.Writer)                          { _, _ = io.WriteString(w, "\n") }
func flush(tw *tabwriter.Writer)              { _ = tw.Flush() }

func pct(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func strata(m map[Stratum]int) string {
	var parts []string
	for _, s := range Strata {
		if n := m[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s, n))
		}
	}
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}
