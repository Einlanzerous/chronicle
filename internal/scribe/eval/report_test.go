package eval

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

func repoFS(t *testing.T) fs.FS {
	t.Helper()
	return os.DirFS("../../../docs/eval")
}

func render(t *testing.T, rep *Report) string {
	t.Helper()
	var b strings.Builder
	rep.Render(&b)
	return b.String()
}

// Ruling R4, said by the harness rather than left for a human to remember.
func TestTheReportRefusesAThresholdWhenCalibrationIsFlat(t *testing.T) {
	rep := Score("ollama/gemma4:31b@v1", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.95),
		routed(lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.95),
		routed(lbl("c", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.2),
		routed(lbl("d", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.2),
	})
	out := render(t, rep)
	for _, want := range []string{"No threshold ships (R4)", "1.01", "admits nothing"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not mention %q:\n%s", want, out)
		}
	}
}

// A moved pin changes what everything below it means, so it is reported first.
func TestAMovedPinIsTheFirstThingTheReportSays(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestNote, "", yes())
	r := routed(l, scribe.DestNote, "", 0.9)
	r.Item.Model = "whisper.cpp/large-v3"

	out := render(t, Score("p", []Result{r}))
	warn := strings.Index(out, "THE TRANSCRIPT PIN MOVED")
	acc := strings.Index(out, "Destination accuracy")
	if warn < 0 {
		t.Fatalf("no warning:\n%s", out)
	}
	if warn > acc {
		t.Error("the warning comes after the numbers it invalidates")
	}
}

// §2's log: date, proposer, result, and the transcript each memo was scored
// from — rendered for the real stratum, which is the half that degrades by
// being looked at.
func TestTheReportEndsWithTheRunLogEntry(t *testing.T) {
	out := render(t, Score("ollama/gemma4:31b@v1", []Result{
		routed(lbl(hash(1), StratumReal, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
	}))
	for _, want := range []string{"Run log entry", "ollama/gemma4:31b@v1", "memo", "transcript"} {
		if !strings.Contains(out, want) {
			t.Errorf("report does not mention %q:\n%s", want, out)
		}
	}
}

func TestASyntheticOnlyRunHasNoRunLogEntry(t *testing.T) {
	out := render(t, Score("p", []Result{
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
	}))
	if strings.Contains(out, "Run log entry") {
		t.Error("development material does not degrade by being looked at")
	}
}

// Two strata, two blocks, and no third number anywhere.
func TestTheReportShowsEachStratumSeparately(t *testing.T) {
	out := render(t, Score("p", []Result{
		routed(lbl(hash(1), StratumReal, scribe.DestNote, "", yes()), scribe.DestTicket, "task", 0.9),
		routed(lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), scribe.DestNote, "", 0.9),
	}))
	if !strings.Contains(out, "### `real` — n=1") || !strings.Contains(out, "### `synthetic` — n=1") {
		t.Fatalf("both strata are not reported separately:\n%s", out)
	}
	// The blended number would be 50%, and there is no heading it could sit
	// under: accuracy is rendered per stratum and the type holds no total.
	// Calibration DOES pool, and says so where it does.
	if strings.Contains(out, "### `all`") || strings.Contains(out, "overall") {
		t.Error("a combined accuracy block appeared in the report")
	}
	if !strings.Contains(out, "Pooled across strata") {
		t.Error("calibration pools and did not say so")
	}
}

func TestTheUnhandledSectionReportsACountAndNotAPercentage(t *testing.T) {
	l := lbl(hash(1), StratumReal, scribe.DestTicket, "task", yes())
	l.Unhandled = []Gap{GapDedup}
	out := render(t, Score("p", []Result{routed(l, scribe.DestNote, "", 0.9)}))
	if !strings.Contains(out, "a finding, not a score") || !strings.Contains(out, "`dedup`: 1 item") {
		t.Fatalf("unhandled section:\n%s", out)
	}
}

func TestTheResolutionCheckRendersWithoutAModel(t *testing.T) {
	set, err := Load("../../../docs/eval/routing-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	labels := set.Select(StratumSynthetic)
	res := Resolver{Files: repoFS(t)}.ResolveAll(t.Context(), labels)
	if len(res.Failures) > 0 {
		t.Fatalf("%d fixture(s) missing: %v", len(res.Failures), res.Failures[0].Err)
	}
	var b strings.Builder
	RenderResolution(&b, res, labels)
	if !strings.Contains(b.String(), "25 label(s): 25 resolved, 0 unresolved.") {
		t.Fatalf("resolution report:\n%s", b.String())
	}
}
