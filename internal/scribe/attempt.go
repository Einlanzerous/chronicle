package scribe

import (
	"context"
	"fmt"
)

// Generate produces one completion. CHRN-30 supplies it; this package never
// talks to Ollama, because the prompt is that ticket's and the contract is
// this one's.
//
// feedback is empty on the first attempt and carries the previous attempt's
// validation errors afterwards.
type Generate func(ctx context.Context, attempt int, feedback string) (raw []byte, err error)

// Outcome is one memo's trip through both stages.
//
// Every field is meant to reach the row, and the pairing matters: Raw is the
// text Proposal was parsed from, ALWAYS. When Proposal is nil, Raw is the last
// attempt's output and belongs in last_attempt_raw rather than raw_output --
// see Store.SaveProposal, which is where the distinction is enforced.
type Outcome struct {
	Proposal *Proposal
	Raw      []byte
	Status   Status
	Cleared  []ClearedField

	// Err is set when no attempt produced a schema-valid proposal. It is the
	// last attempt's errors, and it is written to the row rather than logged
	// and dropped.
	Err error
}

// Run asks for a proposal, validating each attempt and feeding failures back,
// then reconciles the winner against the catalogue.
//
// THERE IS NO PATH WHERE A MEMO PRODUCES NOTHING. That is the whole of §7, and
// the ticket's sharpest sentence is why: "a malformed proposal is a proposal
// that silently disappears from a batch, and the operator will not notice which
// memo went missing." A failure comes back as an Outcome carrying Err and the
// raw output, which the caller records; the memo then appears in the batch with
// its error instead of a proposal, and can be routed by hand.
//
// Retries are STAGE 1 ONLY. A stage-2 failure is not the model's fault — it
// answered as well as it could and the world moved — so asking again would
// spend a completion to be told the same thing.
func Run(ctx context.Context, gen Generate, cat Catalogue, maxAttempts int) Outcome {
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastRaw []byte
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		feedback := ""
		if errs, ok := lastErr.(ShapeErrors); ok {
			// The error goes back to the model. A model told "confidence must
			// be between 0 and 1, got 1.5" usually fixes it, and one more
			// completion is cheaper than an operator's attention.
			feedback = errs.Prompt()
		}

		raw, err := gen(ctx, attempt, feedback)
		if err != nil {
			// Ollama unreachable, context cancelled, and so on. Not a contract
			// failure and not something a differently-worded prompt fixes, so
			// it ends the run rather than burning the remaining attempts.
			return Outcome{Raw: raw, Status: StatusInvalid, Err: fmt.Errorf("scribe: generate: %w", err)}
		}
		lastRaw = raw

		p, err := Parse(raw)
		if err != nil {
			lastErr = err
			continue
		}

		cleared, status := Reconcile(p, cat)
		return Outcome{Proposal: p, Raw: raw, Status: status, Cleared: cleared}
	}

	return Outcome{Raw: lastRaw, Status: StatusInvalid, Err: lastErr}
}
