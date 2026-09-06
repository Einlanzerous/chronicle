// Package scribe holds the proposal contract: the structured thing Scribe must
// produce for a memo, and the two validations that stand between a model's
// output and a triage screen.
//
// It is a package rather than a file in internal/api because CHRN-67 exposes
// routing as an MCP tool. A contract that exists only inside HTTP handlers is
// one that gets reimplemented for the agent surface, and two implementations of
// a contract are two contracts.
//
// The decision is docs/decisions/chrn-32-proposal-contract.md; section numbers
// in these comments refer to it.
package scribe

import (
	"fmt"
	"strings"
)

// Destination is what Scribe says a memo should become.
//
// FOUR VALUES, NOT FIVE. The epic lists NOTE / TICKET / DISCUSSION with HOLD
// and DISCARD as escapes, which invites giving the model all five. HOLD is
// missing here on purpose (§4).
//
// DISCARD is a judgement about CONTENT — "this is somebody testing their
// microphone" is a claim about the transcript, the model is positioned to make
// it, and Reason makes it checkable at a glance.
//
// HOLD is a claim about THE OPERATOR'S OWN STATE OF MIND — "I will decide this
// later" — which a model cannot hold. A model that can answer "I'd rather not"
// will learn to, and HOLD would become the safe default that swallows the
// ambiguous half of every batch: forty proposals, eighteen of them HOLD, which
// is the screen you would have had with no router at all except that it cost
// forty completions. Low confidence is already how the model says it does not
// know, and it is strictly better, because it still commits to something
// CHRN-36 can score. A router that abstains cannot be measured, and E4's exit
// criterion is a measurement.
//
// HOLD is an operator action on every item (CHRN-34), regardless of what was
// proposed.
type Destination string

const (
	DestNote       Destination = "NOTE"
	DestTicket     Destination = "TICKET"
	DestDiscussion Destination = "DISCUSSION"
	DestDiscard    Destination = "DISCARD"
)

// Destinations is the set a proposal may carry, in the order a screen shows
// them. Iterated by the validator so adding one here is the only edit needed.
var Destinations = []Destination{DestNote, DestTicket, DestDiscussion, DestDiscard}

// TicketTypes is Switchyard's enum, carried over from the salvaged vox-dictate
// prompt unchanged (docs/salvage/vox-dictate/routing-prompt.md).
var TicketTypes = []string{"spike", "task", "bug", "epic"}

// Verb is what a NOTE proposal says should happen to the corpus.
//
// CHRN-39'S SET, NOT A NEW ONE. These are exactly the four values migration
// 0014's `note_revisions_verb` CHECK admits, and they are declared here as
// well as in internal/store because **store imports this package** — the
// dependency runs one way and cannot be reversed for a constant.
//
// Two copies of an enum is the shape this repo distrusts, so it is guarded
// rather than trusted: `TestScribeVerbsMatchTheColumn` in internal/store
// compares the two sets and fails if either grows alone. That is CHRN-84's
// pattern — the test loops over the exported set itself, so the advertised
// vocabulary and the storable one cannot drift.
//
// What each one means is CHRN-39's plan and is not re-decided here:
//
//	create     — a new note on page_path. One revision, seq 1.
//	append     — add to an existing note. Title carried forward unchanged.
//	supersede  — replace an existing note's body. The old body is seq n-1.
//	relate     — a DISTINCT idea belonging near an existing note; a new note
//	             whose body references the target.
type Verb string

const (
	VerbCreate    Verb = "create"
	VerbAppend    Verb = "append"
	VerbSupersede Verb = "supersede"
	VerbRelate    Verb = "relate"
)

// Verbs is the set a NOTE proposal may carry, in the order the prompt teaches
// them. Iterated by the validator, so adding one here is the only edit needed.
var Verbs = []Verb{VerbCreate, VerbAppend, VerbSupersede, VerbRelate}

// NeedsTarget reports whether this verb names an existing note.
//
// THREE OF FOUR, AND create IS THE ODD ONE OUT. CHRN-39's plan corrects an
// earlier reading that only `relate` carried a target: `append` and
// `supersede` both act on a note that already exists, and `page_path` cannot
// identify one because `NotesOnPage` exists precisely for a page holding many.
//
// THE ZERO VALUE ANSWERS false, because an absent verb means create and a
// Proposal does not always come through Parse. `store.Proposal.Payload` is
// decoded with a plain json.Unmarshal, so every row written before CHRN-94
// deserialises with `Verb("")` and never passes the normalisation in Parse. A
// bare `v != VerbCreate` would tell CHRN-95 that those rows need a target they
// were never able to carry.
func (v Verb) NeedsTarget() bool { return v != VerbCreate && v != "" }

// Status is the proposal row's state, and it is not the memo's.
//
//   - Valid      — usable as it stands.
//   - NeedsInput — the model answered honestly and a person must complete it:
//     an unguessable project key (§3), or a target the catalogue no longer has
//     (§5). Distinct from Invalid, and CHRN-33 must not merge them: one is a
//     memo an operator can finish in a tap, the other is a memo with no
//     proposal at all.
//   - Invalid    — no run has EVER produced a valid proposal for this memo
//     under this proposer. Not "the last attempt failed": a failed re-run keeps
//     the payload that validated and records the failure beside it (§7),
//     because a prompt regression on Tuesday should not cost the operator a
//     good proposal from Monday.
type Status string

const (
	StatusValid      Status = "valid"
	StatusNeedsInput Status = "needs_input"
	StatusInvalid    Status = "invalid"
)

// MaxReasonLen and MaxTitleLen bound the two free-text fields a screen has to
// lay out. The title bound is the salvaged prompt's own.
const (
	MaxReasonLen = 400
	MaxTitleLen  = 100
)

// Proposal is the payload, as a discriminated union on Destination.
//
// One JSONB column rather than fifteen nullable ones: the
// destination-conditional fields differ in kind, and modelling them as columns
// produces a table where two thirds of every row is NULL and no constraint can
// say which two thirds.
type Proposal struct {
	Destination Destination `json:"destination"`

	// Confidence is carried and NOT interpreted (§8). The contract checks it
	// is a number in [0,1] and attaches no meaning to any particular value —
	// the threshold that licences ACCEPT ALL is CHRN-36's to measure, because
	// CHRN-36 is the only thing that will ever know whether confidence
	// predicts correctness.
	Confidence float64 `json:"confidence"`

	// Reason is load-bearing and required. The canvas gives the shape:
	//
	//   "argues a principle, names no owner and no due date — reads as
	//    doctrine, not work. Nearest existing page is `storage/amber`."
	//
	// A destination without a reason is something the operator has to verify
	// from scratch, which is SLOWER THAN DECIDING UNAIDED. A router that makes
	// triage slower has negative value however accurate it is.
	Reason string `json:"reason"`

	// Title is required except on DISCARD, where the model would otherwise
	// have to invent an imperative title for something being thrown away.
	Title string `json:"title,omitempty"`

	// NearestPage is ADVISORY. It feeds the reason line and is never a landing
	// target, which is why a bad one is cleared without changing the status
	// (§5) — an otherwise acceptable TICKET should not become work because the
	// model invented a page name in a sentence.
	NearestPage *string `json:"nearest_page"`

	// --- TICKET ---

	// ProjectKey may be empty, and empty is a real answer rather than a
	// failure. The salvaged prompt's best rule, carried over because the
	// reason still holds: project_key is IMMUTABLE after ticket creation in
	// Switchyard, so a guessed project is a permanent wrong answer. The
	// recovery lives in code and in the operator, never in the model.
	ProjectKey  *string `json:"project_key,omitempty"`
	TicketType  string  `json:"ticket_type,omitempty"`
	Description string  `json:"description,omitempty"`

	// --- NOTE ---

	// PagePath is the target, and it MAY name a page that does not exist —
	// provided its nearest existing ancestor is live, which is what stops
	// `a/b/c/d/e` being invented whole. Distinct from NearestPage, which is
	// advisory; requiring PagePath to already exist would pre-decide that a
	// note can only ever land on a page somebody made earlier, and that is
	// E5's call and not this contract's.
	PagePath *string `json:"page_path,omitempty"`

	// Verb is what should happen to the corpus, and it is REQUIRED for NOTE.
	//
	// CHRN-32 shipped this contract carrying page_path and no verb, on the
	// stated grounds that the values were not knowable in advance and guessing
	// would pre-decide E5. CHRN-39 defined them, so this is the field that
	// refusal was holding open.
	Verb Verb `json:"verb,omitempty"`

	// TargetNote is the existing note the verb acts on, written CHR-0311.
	//
	// REQUIRED UNLESS THE VERB IS create, which is what NeedsTarget says. It
	// is deliberately NOT a general `{kind, ref}` target: this struct is a
	// discriminated union whose fields are grouped by destination and it
	// carries no polymorphic field, so a general one would be the first, and
	// `validate.go` would grow a kind switch with a branch no test can reach
	// until threads exist. If E6 wants a memo routed into an existing
	// discussion, the field is `target_thread` in the DISCUSSION block below.
	// Decided by magos 2026-09-06; the argument is on CHRN-94.
	//
	// Syntax is checked here and RESOLUTION IS NOT: stage 2 asks the
	// catalogue, exactly as page_path does. A note that was soft-deleted
	// between proposal and acceptance is a stage-2 clearing, not a parse
	// error.
	TargetNote *string `json:"target_note,omitempty"`

	Body string `json:"body,omitempty"`

	// --- DISCUSSION ---
	//
	// No target field here yet, and that is E6's call rather than an omission:
	// whether a thread gets a public ID namespace at all is CHRN-43's, and
	// `target_thread` is the name reserved for it if it does.

	OpeningPost string `json:"opening_post,omitempty"`
}

// PreAcceptable reports whether a proposal may be pre-selected for ACCEPT ALL.
//
// TWO GATES, AND THE FIRST IS NOT A THRESHOLD. DISCARD is never pre-acceptable
// at any confidence (§4): `discarded` is terminal in the memo state machine —
// migration 0003 lists it only ever as a transition target and never as a
// source — and one confidence floor cannot express "this destination is never
// in ACCEPT ALL". A confident wrong DISCARD is precisely the case that clears
// any threshold, and it is the one accept that cannot be undone. Not data
// destruction, since the transcript is tier 2 and survives, but it should cost
// a deliberate tap.
//
// The second gate is min, which comes from configuration and is CHRN-36's to
// set.
func (p Proposal) PreAcceptable(status Status, min float64) bool {
	if status != StatusValid {
		return false
	}
	if p.Destination == DestDiscard {
		return false
	}
	return p.Confidence >= min
}

// Proposer is the row's identity alongside the memo, and it is
// RUNNER-QUALIFIED — on tier2.transcripts.model's pattern, which holds
// `whisper.cpp/small.en` rather than `small.en` because a bare model name says
// nothing about what ran it. CHRN-22's model floor had to be two-axis for the
// same reason.
//
// Three parts here, because a proposal has three inputs and not two:
//
//	ollama/gemma4:31b@v1
//	└runner┘└─model──┘└─┘prompt version
//
// The PROMPT VERSION is not optional. A proposal is the output of a model AND a
// prompt, and a prompt revision changes the answer as surely as a model swap
// does — without it CHRN-36 cannot tell a prompt regression from a model
// regression, which is the one comparison the eval set exists to make.
//
// (The third input, which transcript it was derived from, is recorded on the
// row rather than in the key: keying on it would let two transcripts of one
// memo each carry a proposal, which is a triage screen showing the same memo
// twice.)
func Proposer(runner, model, promptVersion string) (string, error) {
	runner, model, promptVersion = strings.TrimSpace(runner), strings.TrimSpace(model), strings.TrimSpace(promptVersion)
	for name, part := range map[string]string{"runner": runner, "model": model, "prompt version": promptVersion} {
		if part == "" {
			return "", fmt.Errorf("scribe: proposer %s is empty", name)
		}
		// The separators are the parse, exactly as split_part is the parse in
		// store.DurableClause. A part carrying one would make the string
		// ambiguous, and an ambiguous proposer is one CHRN-36 attributes to
		// the wrong thing.
		if strings.ContainsAny(part, "/@") {
			return "", fmt.Errorf("scribe: proposer %s %q may not contain '/' or '@'", name, part)
		}
	}
	return runner + "/" + model + "@" + promptVersion, nil
}
