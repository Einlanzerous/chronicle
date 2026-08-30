package scribe

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Validation happens in TWO STAGES that fail differently, because one is about
// shape and the other is about the world.
//
//	Stage 1 · Parse      — deterministic, needs nothing external. Enum
//	                       membership, required fields, ranges, lengths. A
//	                       failure here is retried with the error fed back to
//	                       the model (§7).
//	Stage 2 · Reconcile  — referential, against CHRN-31's live catalogue. A
//	                       failure here is never a retry: the model answered as
//	                       well as it could and the world moved.
//
// Ollama is already asked for `format: "json"`, which constrains decoding to
// valid JSON — the salvage note records that this is why the downstream parser
// needs one fenced-block fallback rather than a general repair pass. So stage 1
// is mostly catching VALID JSON THAT IS THE WRONG SHAPE, which is the far more
// common failure.

// ShapeError is a stage-1 failure. It names the field, because the message goes
// back to the model on the next attempt and "confidence must be between 0 and 1,
// got 1.5" is a thing a model can act on where "invalid proposal" is not.
type ShapeError struct {
	Field   string
	Message string
}

func (e *ShapeError) Error() string { return e.Field + ": " + e.Message }

// ShapeErrors is every stage-1 failure at once rather than the first.
//
// All of them, deliberately: a retry that fixes one field and trips over the
// next spends an attempt per field, and §7 only allows three.
type ShapeErrors []*ShapeError

func (e ShapeErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, err := range e {
		parts = append(parts, err.Error())
	}
	return "scribe: invalid proposal — " + strings.Join(parts, "; ")
}

// Prompt renders the errors for the retry attempt, one per line.
func (e ShapeErrors) Prompt() string {
	parts := make([]string, 0, len(e))
	for _, err := range e {
		parts = append(parts, "- "+err.Field+": "+err.Message)
	}
	return strings.Join(parts, "\n")
}

// Parse is stage 1: raw model output in, a validated Proposal out.
//
// It takes BYTES rather than a decoded struct so that the raw output and the
// payload it produced stay a pair — the row stores both, and CHRN-36 diffs
// them against each other. A caller that decoded first would have nothing
// truthful to put in raw_output.
func Parse(raw []byte) (*Proposal, error) {
	var errs ShapeErrors
	bad := func(field, format string, args ...any) {
		errs = append(errs, &ShapeError{Field: field, Message: fmt.Sprintf(format, args...)})
	}

	// Presence is checked separately from value, because for two fields the
	// difference matters: `project_key: ""` is a real answer and an absent
	// project_key is a model that did not follow the contract.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, ShapeErrors{{Field: "(document)", Message: "not a JSON object: " + err.Error()}}
	}

	var p Proposal
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, ShapeErrors{{Field: "(document)", Message: "does not match the proposal shape: " + err.Error()}}
	}

	if !slices.Contains(Destinations, p.Destination) {
		bad("destination", "must be one of %v, got %q (HOLD is an operator action and never a proposal)",
			Destinations, p.Destination)
		// Every remaining check is destination-conditional, so there is
		// nothing truthful left to say.
		return nil, errs
	}

	if p.Confidence < 0 || p.Confidence > 1 {
		bad("confidence", "must be between 0 and 1 inclusive, got %v", p.Confidence)
	}
	if _, ok := keys["confidence"]; !ok {
		bad("confidence", "is required")
	}

	if strings.TrimSpace(p.Reason) == "" {
		bad("reason", "is required and may not be empty — it is what makes a wrong proposal rejectable at a glance")
	} else if len(p.Reason) > MaxReasonLen {
		bad("reason", "must be at most %d characters, got %d", MaxReasonLen, len(p.Reason))
	}

	// Required everywhere except DISCARD, where an imperative title for
	// something being thrown away is a fabrication with no reader.
	if p.Destination != DestDiscard {
		if strings.TrimSpace(p.Title) == "" {
			bad("title", "is required for %s", p.Destination)
		} else if len(p.Title) > MaxTitleLen {
			bad("title", "must be at most %d characters, got %d", MaxTitleLen, len(p.Title))
		}
	}

	// Required-but-nullable: the key must appear, its value may be null.
	if _, ok := keys["nearest_page"]; !ok {
		bad("nearest_page", "is required — use null when no existing page is near")
	}

	switch p.Destination {
	case DestTicket:
		if _, ok := keys["project_key"]; !ok {
			bad("project_key", "is required — use \"\" when the project cannot be told from the transcript, and never guess")
		} else if p.ProjectKey != nil && *p.ProjectKey != "" {
			if k := *p.ProjectKey; k != strings.ToUpper(k) {
				bad("project_key", "must be uppercase, got %q", k)
			}
		}
		if !slices.Contains(TicketTypes, p.TicketType) {
			bad("ticket_type", "must be one of %v, got %q", TicketTypes, p.TicketType)
		}
		if strings.TrimSpace(p.Description) == "" {
			bad("description", "is required for TICKET")
		}
	case DestNote:
		if strings.TrimSpace(p.Body) == "" {
			bad("body", "is required for NOTE")
		}
	case DestDiscussion:
		if strings.TrimSpace(p.OpeningPost) == "" {
			bad("opening_post", "is required for DISCUSSION")
		}
	case DestDiscard:
		// Nothing beyond the common fields. `reason` is doing the work, and it
		// is what the operator reads before agreeing to throw something away.
	}

	if len(errs) > 0 {
		return nil, errs
	}
	return &p, nil
}

// Catalogue is what Scribe is allowed to route to, assembled freshly per run by
// CHRN-31. Two methods, so that ticket implements a small thing.
//
// Read LIVE and never cached across runs, for the reason CHRN-31 gives: a stale
// catalogue produces the worst kind of error, where the destination it picked
// does exist and is merely the wrong one, and nothing in the proposal looks
// wrong.
type Catalogue interface {
	// HasProject reports whether a Switchyard project key is live.
	HasProject(key string) bool
	// HasPage reports whether a Chronicle page path exists exactly.
	//
	// Until CHRN-37 lands in E5 there is no page tree, so a correct
	// implementation answers false to everything — which is not a special case
	// here, just an empty catalogue.
	HasPage(path string) bool
}

// ClearedField records something stage 2 removed, and why.
//
// Never a silent drop. A model that invents a page in one proposal out of three
// is a fact about the prompt, and it is only visible if the clearing leaves a
// trace — which is what lets CHRN-36 report a hallucination rate per proposer.
type ClearedField struct {
	Field  string `json:"field"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

// Reconcile is stage 2: check the proposal's references against the live
// catalogue, clear what it cannot resolve, and report the resulting status.
//
// It MUTATES p, which is the point — the row stores what survived.
//
// A BAD TARGET BLOCKS; A BAD ADVISORY FIELD DOES NOT. The distinction is by
// what the field does, not by which check failed:
//
//   - Targets (project_key, page_path) — clearing one leaves the proposal
//     unable to land, so the status becomes needs_input and a person supplies
//     the missing target. Destination, confidence and reason are kept: a
//     proposal whose project was archived needs a new project, not a re-route
//     from scratch.
//   - Advisory (nearest_page) — feeds the reason line and nothing else. It is
//     cleared and the status does NOT change.
//
// Either way "a hallucinated page path cannot be accepted" holds strictly, and
// holds by the general mechanism rather than a special case: a cleared field is
// not there to accept.
//
// RUN AT WRITE AND AGAIN AT ACCEPTANCE. Not belt-and-braces: a proposal made on
// Tuesday evening and accepted on Thursday morning has had two days for a
// project to be archived or a page to be renamed, and validating only at write
// time would let CHRN-33 create a ticket in a project that no longer takes
// them. The catalogue is already assembled for the batch, so the second check
// is nearly free.
func Reconcile(p *Proposal, cat Catalogue) (cleared []ClearedField, status Status) {
	status = StatusValid

	clear := func(field, value, reason string, blocking bool) {
		cleared = append(cleared, ClearedField{Field: field, Value: value, Reason: reason})
		if blocking {
			status = StatusNeedsInput
		}
	}

	// Advisory. Cleared without changing the status.
	if p.NearestPage != nil && *p.NearestPage != "" && !cat.HasPage(*p.NearestPage) {
		clear("nearest_page", *p.NearestPage, "no such page in the live catalogue", false)
		p.NearestPage = nil
	}

	switch p.Destination {
	case DestTicket:
		switch {
		case p.ProjectKey == nil || *p.ProjectKey == "":
			// NOT a clearing and NOT an error: the model was asked to answer
			// empty rather than guess, and it did. It needs a person, which is
			// exactly what needs_input means. Marking this invalid would teach
			// the model to guess, and a guessed project key is permanent.
			status = StatusNeedsInput
		case !cat.HasProject(*p.ProjectKey):
			clear("project_key", *p.ProjectKey, "no such live Switchyard project", true)
			empty := ""
			p.ProjectKey = &empty
		}

	case DestNote:
		// A target, so it blocks. It may name a page that does not exist,
		// provided its nearest existing ancestor is live -- which is what
		// stops a whole invented branch while still letting a note propose a
		// new leaf. With no page tree at all (before CHRN-37) nothing has a
		// live ancestor, so every non-null page_path clears here.
		if p.PagePath != nil && *p.PagePath != "" && !hasLiveAncestor(*p.PagePath, cat) {
			clear("page_path", *p.PagePath, "neither the page nor any ancestor of it is in the live catalogue", true)
			p.PagePath = nil
		}

	case DestDiscussion, DestDiscard:
		// Neither carries a catalogue reference beyond nearest_page, handled
		// above.
	}

	return cleared, status
}

// hasLiveAncestor reports whether any proper ancestor of path -- or path itself
// -- exists in the catalogue.
//
// `a/b/c` is admissible when `a/b` or `a` is live. It is not admissible on an
// empty tree, which is the state until CHRN-37, and that is the correct answer
// rather than a gap: there is nowhere for the note to hang.
func hasLiveAncestor(path string, cat Catalogue) bool {
	p := strings.Trim(path, "/")
	for p != "" {
		if cat.HasPage(p) {
			return true
		}
		i := strings.LastIndex(p, "/")
		if i < 0 {
			return false
		}
		p = p[:i]
	}
	return false
}
