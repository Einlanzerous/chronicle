package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/scribe/eval"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// `chronicle eval` — CHRN-36's harness at a terminal.
//
// The decision is docs/decisions/chrn-36-routing-eval-set.md.
//
// TWO MODES, AND ONE OF THEM NEEDS NO MODEL. --dry-run resolves every label
// against the corpus and reports what it found, which is the half of this that
// runs today and the half that answers "is a run still reproducible". The
// scored run needs a router, and there is not one yet: CHRN-30 owns the prompt,
// and internal/scribe/eval owns everything except the prompt.
//
// It cannot run in CI whatever is committed here (§8). A scored run needs
// Ollama and gemma4:31b, which exist on one machine; CHRN-83 is what would make
// it possible at all, and even then a 42-item eval on every PR would contend
// with the transcription pump for the R9700 with nothing arbitrating.

const defaultLabelsPath = "docs/eval/routing-v1.yaml"

func runEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	labelsPath := fs.String("labels", defaultLabelsPath, "the label file")
	stratum := fs.String("stratum", "all", "which stratum to run: all, real, or synthetic")
	dryRun := fs.Bool("dry-run", false, "resolve every label and report, without routing anything")
	jsonOut := fs.String("json", "", "also write the report as JSON to this path")
	allowMovedPins := fs.Bool("allow-moved-pins", false,
		"score even where the transcript is not the one the labeller read")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var want eval.Stratum
	switch *stratum {
	case "all", "":
		want = ""
	case string(eval.StratumReal):
		want = eval.StratumReal
	case string(eval.StratumSynthetic):
		want = eval.StratumSynthetic
	default:
		return fmt.Errorf("eval: --stratum must be all, real or synthetic, got %q", *stratum)
	}

	set, err := eval.Load(*labelsPath)
	if err != nil {
		return err
	}
	labels := set.Select(want)
	if len(labels) == 0 {
		return fmt.Errorf("eval: no labels in stratum %q", *stratum)
	}

	// A HELD-OUT SET DEGRADES EVERY TIME IT IS LOOKED AT (§2), so looking at it
	// is announced rather than silent. The run log the report ends with is the
	// other half of that; this is the half that fires before any work happens,
	// while there is still time to notice it was not what was meant.
	if hasReal(labels) {
		fmt.Fprint(os.Stderr,
			"NOTE: this run touches the HELD-OUT `real` stratum. Record it in §2's run log\n"+
				"      of docs/decisions/chrn-36-routing-eval-set.md — the report ends with the\n"+
				"      entry to paste. CHRN-30 develops against `synthetic`.\n\n")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The fixture root is the LABEL FILE'S OWN DIRECTORY, so a label set moved
	// somewhere else takes its fixtures with it and nothing resolves against a
	// path that happens to exist relative to the shell.
	res := eval.Resolver{Files: os.DirFS(filepath.Dir(*labelsPath))}

	// The corpus is opened only when a real label needs it, which is what lets
	// `--stratum synthetic` run on a machine with no database and no
	// environment at all — including a CI runner.
	if hasReal(labels) {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		dsn, role := cfg.DatabaseURL, "the main role"
		if cfg.Tier1IsSeparate() {
			dsn, role = cfg.Tier1DatabaseURL, "chronicle_tier1"
		}
		pool, err := store.ConnectWithRetry(ctx, dsn, 30*time.Second)
		if err != nil {
			return err
		}
		defer pool.Close()
		tier1 := store.NewTier1(pool)
		// THE EVAL READS THE CORPUS AS DERIVED WORK DOES, and says which role
		// it got rather than which variable was set — the same correction the
		// boot warning carries, for the same reason.
		actual, err := tier1.Role(ctx)
		if err != nil {
			return err
		}
		if actual != "chronicle_tier1" {
			fmt.Fprintf(os.Stderr, "NOTE: reading the corpus as %q (wanted chronicle_tier1, configured %s).\n"+
				"      The score is unaffected; the boundary is not being exercised.\n\n", actual, role)
		}
		res.Corpus = tier1
	}

	resolution := res.ResolveAll(ctx, labels)

	if *dryRun {
		eval.RenderResolution(os.Stdout, resolution, labels)
		return dryRunVerdict(resolution, *allowMovedPins)
	}

	// EVERY LABEL OR NONE. A scored run that quietly covered eighteen of
	// twenty-one would report an accuracy over a set nobody chose, and the
	// number would be compared against runs over the full set. --dry-run is
	// how you look at a partial resolution on purpose.
	if len(resolution.Failures) > 0 {
		eval.RenderResolution(os.Stderr, resolution, labels)
		return fmt.Errorf("eval: %d label(s) did not resolve, so this run would score a different set than the last one",
			len(resolution.Failures))
	}
	if moved := resolution.Moved(); len(moved) > 0 && !*allowMovedPins {
		return fmt.Errorf("eval: the transcript pin moved on %d item(s) — they would be scored from text the\n"+
			"labeller never read, and the run would not be comparable to the last one. Re-label,\n"+
			"or pass --allow-moved-pins to take this run as a new baseline", len(moved))
	}

	router, err := newRouter()
	if err != nil {
		return err
	}
	results, err := eval.Run(ctx, router, resolution.Items)
	if err != nil {
		return err
	}

	rep := eval.Score(router.Proposer(), results)
	rep.RunAt = time.Now()
	rep.LabelsPath = *labelsPath
	rep.LabelsSHA256 = fileSHA256(*labelsPath)
	rep.Render(os.Stdout)

	if *jsonOut != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s\n", *jsonOut)
	}
	return nil
}

// dryRunVerdict decides the exit status of a resolution check.
//
// NON-ZERO ON A MOVED PIN, and that is the point of having the check at all.
// A content hash proves the AUDIO did not move; it says nothing about the
// words, because TranscriptForScribe returns the best-ranked transcript at
// query time and a re-transcription changes the text under every label while
// every hash still matches (§1). Silence there is how a re-transcription gets
// read as prompt drift six weeks later.
func dryRunVerdict(res eval.Resolution, allowMoved bool) error {
	moved := res.Moved()
	switch {
	case len(res.Failures) > 0:
		return fmt.Errorf("eval: %d label(s) did not resolve", len(res.Failures))
	case len(moved) > 0 && !allowMoved:
		return fmt.Errorf("eval: the transcript pin moved on %d item(s): the labels record what was read, "+
			"and it is not what would be scored", len(moved))
	}
	return nil
}

// newRouter is the seam CHRN-30 fills, and it is the whole of what is missing.
//
// It is a function returning an error rather than a stub returning canned
// output on purpose: a harness that scored a fake router would print a
// plausible report, and the estate's cautionary tale is an integration that was
// a silent no-op and looked like a working feature. Everything else here runs.
func newRouter() (eval.Router, error) {
	return nil, fmt.Errorf(
		"eval: no router is wired yet, so there is nothing to score.\n" +
			"CHRN-30 owns the prompt and supplies it: a type with Proposer() and Route(),\n" +
			"calling scribe.Run with a generator over Ollama. Until then:\n" +
			"  chronicle eval --dry-run              check every label still resolves\n" +
			"  chronicle eval --stratum synthetic --dry-run   the same, with no database")
}

func hasReal(labels []eval.Label) bool {
	for _, l := range labels {
		if l.Stratum == eval.StratumReal {
			return true
		}
	}
	return false
}

func fileSHA256(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
