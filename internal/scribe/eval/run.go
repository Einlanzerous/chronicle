package eval

import (
	"context"
	"fmt"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// Router is the seam CHRN-30 fills, and it is the only thing this package does
// not know how to do.
//
// THE PROMPT IS NOT THIS TICKET'S. internal/scribe says the same thing about
// itself — "this package never talks to Ollama, because the prompt is that
// ticket's and the contract is this one's" — and the harness inherits the line
// for the same reason: an eval that carried its own prompt would grade a router
// nobody ships. A Router wraps a generator, hands each attempt's output to
// scribe.Run, and reports what came back.
//
// Proposer is on the interface rather than passed alongside it because a report
// that cannot say WHICH ROUTER produced it is not a run log entry, and §2's log
// is what keeps run N comparable to run N-1. It is scribe.Proposer's
// three-part string — runner, model AND prompt version — since a prompt
// revision changes the answer as surely as a model swap does.
type Router interface {
	Proposer() string
	Route(ctx context.Context, item Item) (scribe.Outcome, error)
}

// Result pairs one item with what the router said about it.
type Result struct {
	Item    Item
	Outcome scribe.Outcome

	// Err is a TRANSPORT failure — Ollama unreachable, context cancelled —
	// and is distinct from an Outcome that carries its own Err because no
	// attempt validated. The first means the run is broken; the second is a
	// fact about the prompt and is scored.
	Err error
}

// Proposal returns the payload if one validated, or nil.
func (r Result) Proposal() *scribe.Proposal {
	if r.Err != nil {
		return nil
	}
	return r.Outcome.Proposal
}

// Run routes every item, in order, and never stops early.
//
// SEQUENTIAL, DELIBERATELY. A routing eval and memo transcription both want the
// R9700 and nothing arbitrates between them — CHRN-26's decision named this
// exact gap, since asrd can single-flight transcription but not the device,
// because Ollama does not go through it. Fanning out would turn one contended
// run into several.
//
// A transport failure does not end the run either: the remaining items are
// still worth routing, and a report that covers eighteen of twenty-one and says
// which three failed is more useful than an error.
func Run(ctx context.Context, r Router, items []Item) ([]Result, error) {
	if r == nil {
		return nil, fmt.Errorf("eval: no router")
	}
	out := make([]Result, 0, len(items))
	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		outcome, err := r.Route(ctx, it)
		out = append(out, Result{Item: it, Outcome: outcome, Err: err})
	}
	return out, nil
}
