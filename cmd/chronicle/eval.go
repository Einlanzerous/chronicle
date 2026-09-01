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
	"strings"
	"syscall"
	"time"

	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/scribe/eval"
	"github.com/Einlanzerous/chronicle/internal/scribe/router"
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

const (
	defaultLabelsPath = "docs/eval/routing-v1.yaml"
	// catalogueFile sits beside the label file and travels with it.
	catalogueFile = "catalogue-v1.yaml"
)

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

	// REFUSED RATHER THAN IGNORED. The dry run returns before the JSON block,
	// so the pair used to write no file and say nothing — REVIEW.md §8's
	// cautionary tale exactly. And it is refused rather than made to work: the
	// resolution deliberately has no JSON form, because an Item carries the
	// transcript TEXT, and writing that to a file is the one thing §1 forbids.
	if *dryRun && *jsonOut != "" {
		return fmt.Errorf("eval: --json writes a scored report and --dry-run produces no scores, so the pair does nothing.\n" +
			"Drop one. The resolution check has no JSON form on purpose: it holds the transcripts,\n" +
			"and those do not leave the corpus (§1)")
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

	rt, err := newRouter(filepath.Dir(*labelsPath), want)
	if err != nil {
		return err
	}
	// Read once per run, before any routing, so the report can say WHICH
	// weights answered. A tag is mutable; the digest is not.
	digest, err := rt.Digest(ctx)
	if err != nil {
		return err
	}

	results, err := eval.Run(ctx, rt, resolution.Items)
	if err != nil {
		return err
	}

	rep := eval.Score(rt.Proposer(), results)
	rep.RunAt = time.Now()
	rep.LabelsPath = *labelsPath
	rep.LabelsSHA256 = fileSHA256(*labelsPath)
	rep.ModelDigest = digest
	rep.CatalogueSHA256 = rt.cat.SHA256()
	rep.Render(os.Stdout)

	if *jsonOut != "" {
		// The report carries the model's reasoning about each memo, and a
		// reason quotes the transcript it is about — so a scored `real` run
		// writes something corpus-adjacent. `.gitignore` covers `*.eval.json`
		// and cannot cover an arbitrary path, so the mismatch is said out loud
		// here rather than left to the one filename somebody guessed.
		if !strings.HasSuffix(*jsonOut, ".eval.json") {
			fmt.Fprintf(os.Stderr,
				"NOTE: %s is not named *.eval.json, which is what .gitignore covers.\n"+
					"      This file holds the model's reasoning about each memo, and a reason\n"+
					"      quotes the transcript — do not commit it (CHRN-36 §1).\n\n", *jsonOut)
		}
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

// evalRouter adapts the production router to the harness's interface.
//
// The adaptation exists so that internal/scribe/router does NOT import
// internal/scribe/eval: CHRN-33's batch path will want the router and has no
// business depending on the thing that grades it. Route is the whole of the
// difference — the harness passes an Item, the router wants the text.
type evalRouter struct {
	*router.Router
	cat *catalogue.Snapshot
}

func (e evalRouter) Route(ctx context.Context, it eval.Item) (scribe.Outcome, error) {
	return e.Router.Route(ctx, it.Text)
}

// newRouter builds the router from the SCRIBE VARIABLES ALONE.
//
// Never through config.Load(), which refuses the moment CHRONICLE_DATABASE_URL
// is unset. runEval deliberately opens the corpus only when a real label needs
// one, so `--stratum synthetic` works on a machine with no database and no
// environment — and a router built from the full config would undo exactly
// that, failing a synthetic run by naming the database instead of the model.
func newRouter(labelsDir string, want eval.Stratum) (evalRouter, error) {
	cfg, err := config.LoadScribe()
	if err != nil {
		return evalRouter{}, err
	}
	if !cfg.Enabled() {
		return evalRouter{}, fmt.Errorf(
			"eval: CHRONICLE_SCRIBE_OLLAMA_URL is not set, so there is no model to ask.\n" +
				"Set it and CHRONICLE_SCRIBE_MODEL, or use --dry-run to check the labels resolve")
	}

	// THE CATALOGUE IS THE STRATUM'S, by the ruling on CHRN-30's plan
	// (2026-08-31). The synthetic stratum routes against a committed fixture
	// so a run is reproducible from the repo alone — which is the property
	// CHRN-36 §1 bought by committing that stratum, and what keeps this half
	// free of a token and a network. The real stratum needs the live list, and
	// that is CHRN-31's project half.
	if want != eval.StratumSynthetic {
		return evalRouter{}, fmt.Errorf(
			"eval: only --stratum synthetic can be scored today.\n" +
				"The real stratum must route against the LIVE Switchyard project list, which is\n" +
				"CHRN-31's project half and does not exist yet. Routing it against the committed\n" +
				"fixture catalogue would score a router nobody will run — and routing it against\n" +
				"no catalogue at all would put every TICKET in needs_input by construction")
	}

	cat, err := catalogue.LoadFixtureFile(filepath.Join(labelsDir, catalogueFile))
	if err != nil {
		return evalRouter{}, err
	}
	r, err := router.New(router.Options{
		BaseURL:     cfg.OllamaURL,
		Model:       cfg.Model,
		Catalogue:   cat,
		MaxAttempts: cfg.MaxAttempts,
	})
	if err != nil {
		return evalRouter{}, err
	}
	return evalRouter{Router: r, cat: cat}, nil
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
