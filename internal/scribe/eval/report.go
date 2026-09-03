package eval

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/Einlanzerous/chronicle/internal/config"
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
	if r.ModelDigest != "" {
		pf(w, "- model:     %s\n", short(r.ModelDigest))
	}
	if r.CatalogueSHA256 != "" {
		pf(w, "- catalogue: %s\n", short(r.CatalogueSHA256))
	}
	pf(w, "- items:    %d scored, %d unhandled, %d failed\n\n",
		len(r.Items)-len(r.Unhandled), len(r.Unhandled), len(r.Failures))

	r.renderMovedPins(w)
	r.renderAccuracy(w)
	r.renderMisses(w)
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
		pf(w, "- no proposal: %d · needs_input: %d · fields cleared: %d · unhandled (set apart): %d\n",
			a.NoProposal, a.NeedsInput, a.Cleared, a.Unhandled)
		// The check on the grammar. A schema passed as `format` should make
		// stage 1 nearly unreachable; if this rate is not near zero, the
		// grammar is not doing what the plan says it is, and every other
		// number here would look identical either way.
		pf(w, "- retried: %d of %d items, %d completions for %d memos (%s)\n\n",
			a.Retried, a.N, a.Attempts, a.N, retryVerdict(a))

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

// renderMisses is what prompt iteration is actually done from: every item the
// router got wrong, with ITS OWN ARGUMENT for the answer it gave.
//
// The report used to say an item was wrong and not why, which sent you back to
// re-run it by hand — the gap the CHRN-30 review found. A destination plus a
// reason is usually enough to see whether the prompt misled the model or the
// label is arguable.
func (r *Report) renderMisses(w io.Writer) {
	var bad []ItemRecord
	for _, it := range r.Items {
		if it.Verdict == VerdictWrong || it.Verdict == VerdictNoProposal {
			bad = append(bad, it)
		}
	}
	if len(bad) == 0 {
		return
	}
	pf(w, "## What went wrong (%d)\n\n", len(bad))
	for _, it := range bad {
		proposed := "(no proposal)"
		if it.Proposed != nil {
			proposed = fmt.Sprintf("%s at %.2f", it.Proposed, it.Confidence)
		}
		pf(w, "**%s** — labelled %s, answered %s\n", it.Short, it.Labelled, proposed)
		if it.RunnerUp != "" {
			pf(w, "- runner-up: %s\n", it.RunnerUp)
		}
		if it.Attempts > 1 {
			pf(w, "- took %d attempts\n", it.Attempts)
		}
		if it.Reason != "" {
			pf(w, "> %s\n", strings.ReplaceAll(strings.TrimSpace(it.Reason), "\n", " "))
		}
		for _, c := range it.Cleared {
			pf(w, "- cleared `%s` = %q: %s\n", c.Field, c.Value, c.Reason)
		}
		if it.Error != "" {
			pf(w, "- error: %s\n", it.Error)
		}
		nl(w)
	}
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

	r.renderLabellerProbe(w)
}

// renderLabellerProbe reports whether the router is least sure where a person
// was, which is calibration measured WITHOUT needing the router to be wrong.
//
// It exists because the trend above cannot be read on a set with no mistakes:
// Rising asserts accuracy VARIES with confidence, and a router that gets
// everything right makes every band 100% and Rising false. The corpus already
// records where a second labeller could reasonably disagree, so a calibrated
// model should be least sure exactly there — and that signal survives a
// perfect score.
func (r *Report) renderLabellerProbe(w io.Writer) {
	c := r.Calibration
	ok, determinable := c.TracksTheLabeller()
	ps(w, "### Does it hesitate where the labeller did?\n\n")
	if !determinable {
		pf(w, "Not determinable: %d labeller-unsure and %d confident items scored.\n\n",
			c.UnsureN, c.ConfidentN)
		return
	}
	pf(w, "- labeller unsure (n=%d): median confidence %.2f\n", c.UnsureN, c.UnsureMedian)
	pf(w, "- labeller confident (n=%d): median confidence %.2f\n", c.ConfidentN, c.ConfidentMedian)
	if len(c.UnsureInTopBand) > 0 {
		pf(w, "- **in the top band anyway: %s**\n", strings.Join(c.UnsureInTopBand, ", "))
	}
	if ok {
		ps(w, "\n**The router hesitates where a person did.**\n\n")
	} else {
		ps(w, "\n**It does not.** The model is no less sure on the memos a second labeller\n"+
			"could reasonably have labelled differently, so its confidence is not tracking\n"+
			"the thing that makes them hard.\n\n")
	}
}

func (r *Report) renderThresholds(w io.Writer) {
	ps(w, "## The threshold\n\n")

	if !r.Calibration.Licenses() {
		// RULING R4, stated by the harness rather than left for a human to
		// remember. If accuracy does not rise with confidence there is no
		// value at which pre-accepting beats not pre-accepting.
		ps(w, "**No threshold ships (R4).** Calibration does not show accuracy rising with\n"+
			"confidence, so raise `CHRONICLE_SCRIBE_PREACCEPT_MIN` back above 1, which admits\n"+
			"nothing. The compiled default is 0.80 as of CHRN-36's run on 2026-09-03, so a flat\n"+
			"run now means UNDOING that rather than leaving things alone. ACCEPT ALL waits for a\n"+
			"prompt that calibrates — a CHRN-30 problem and not a threshold problem. The sweep\n"+
			"below is printed as evidence, not as a menu.\n\n")
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	psln(tw, "  min\tpre-accepted\tof those wrong\theld for a tap")
	for _, row := range r.Thresholds {
		// Two different rows are worth pointing at, and conflating them is how
		// this table went stale once already: the row that admits nothing, and
		// whatever value is actually COMPILED IN today. They were the same row
		// until CHRN-36's run set the default to 0.80, and a reader who assumes
		// they still are will misread every line above.
		var note string
		switch {
		case row.Min == config.DefaultScribePreacceptMin:
			note = "  ← compiled default"
		case row.Min > 1:
			note = "  ← admits nothing"
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

// retryVerdict says what the retry count means, rather than leaving a reader
// to know that near-zero is the good answer.
func retryVerdict(a *Accuracy) string {
	switch {
	case a.N == 0:
		return "nothing scored"
	case a.Retried == 0:
		return "no retries — stage 1 never fired, which is what the grammar is for"
	case a.Retried*4 <= a.N:
		return "a few retries; the grammar is mostly holding"
	default:
		return "RETRIES ARE COMMON — the schema may not be constraining the model at all"
	}
}

// short trims a digest for a header line. The first twelve distinguish
// everything anyone will ever compare by hand.
func short(s string) string {
	s = strings.TrimPrefix(s, "sha256:")
	if len(s) > 12 {
		return s[:12]
	}
	if s == "" {
		return "—"
	}
	return s
}

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
