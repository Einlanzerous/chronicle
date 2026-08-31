// Package eval is CHRN-36's harness: the labelled set, the resolution that
// turns a label back into the text Scribe would actually see, and the scoring
// that says whether the router can be trusted.
//
// The decision is docs/decisions/chrn-36-routing-eval-set.md; section numbers
// in these comments refer to it.
//
// THE LABELS ARE IN THE REPO AND THE REAL TRANSCRIPTS ARE NOT (§1). A label
// names a memo by `tier2.memos.content_hash` and nothing else, so the set can
// live in a public repository while the authored text stays in tier 2, behind
// the role boundary migration 0007 draws. The synthetic stratum is committed in
// full, and the test that settles why is one question: did a person say this
// into a recorder?
//
// The consequence for anyone reading this package: a real label resolves only
// on a machine with the corpus. That is a real narrowing and it is deliberate.
package eval

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// Stratum is which half of the set a label belongs to, and it is the field
// that decides who may look at it (§2).
//
// ACCURACY IS REPORTED PER STRATUM AND NEVER AVERAGED. A blended number hides
// the only comparison that matters — whether the router does worse on real
// speech than on text somebody wrote to be routed — so nothing in this package
// returns a combined score, and Report has no field that could hold one.
type Stratum string

const (
	// StratumReal is held out. CHRN-30 may not develop against it; every
	// scoring run over it is logged, because a held-out set degrades each time
	// it is looked at.
	StratumReal Stratum = "real"
	// StratumSynthetic is development material. Fixtures written to test a
	// router, so they are not tier 2 and have no other home.
	StratumSynthetic Stratum = "synthetic"
)

// Strata is the closed set, in report order.
var Strata = []Stratum{StratumReal, StratumSynthetic}

// Gap names a routing case THE CONTRACT CANNOT EXPRESS (§7), recorded on the
// label so that scoring does not mark the router wrong for doing the only thing
// it can.
//
// A CLOSED SET, not free text. An unhandled tag anyone may spell produces three
// spellings of one finding, and the count is the useful output — "three of
// twenty-one real memos want a capability the contract does not have" is a
// finding for E4 and E5, not a score. Adding a value here is a deliberate edit
// and should arrive with the ticket that closes it.
type Gap string

const (
	// GapPageVerb is an idea arriving in more than one pass: the memo refers
	// back to earlier thinking and wants a VERB against a page — append,
	// supersede, or stand beside — which CHRN-32's contract does not carry.
	// The verb belongs to CHRN-39 and resolving the target needs CHRN-41.
	GapPageVerb Gap = "page_verb"
	// GapDedup is a memo asking whether a ticket already exists before one is
	// created. Nothing in the contract checks Switchyard.
	GapDedup Gap = "dedup"
)

// Gaps is the closed set, in the order §7 introduces them.
var Gaps = []Gap{GapPageVerb, GapDedup}

// Alternative is a second label a reasonable person could have assigned.
//
// It carries a ticket type as well as a destination because §3's ambiguity is
// sometimes WITHIN a destination — City Maps is "spike or note", which is one
// alternative, and a memo that could be a task or an epic is another.
type Alternative struct {
	Destination scribe.Destination `yaml:"destination" json:"destination"`
	TicketType  string             `yaml:"ticket_type,omitempty" json:"ticket_type,omitempty"`
}

func (a Alternative) String() string {
	if a.TicketType != "" {
		return string(a.Destination) + "/" + a.TicketType
	}
	return string(a.Destination)
}

// Label is one hand-assigned answer, and it carries its own ambiguity.
//
// `Confident` and `AlsoDefensible` are the load-bearing fields (§4). Where two
// labels are both defensible, the router picking the other one IS NOT A ROUTER
// FAILURE — it is the ceiling on what any router can score against this set.
// Without recording that, prompt work tunes against noise and the number drifts
// upward while nothing improves.
type Label struct {
	// Hash is `tier2.memos.content_hash` and is the identity of a REAL memo.
	// Exactly one of Hash and File is set.
	Hash string `yaml:"hash,omitempty" json:"hash,omitempty"`
	// File is a path under the label file's own directory, and is the identity
	// of a SYNTHETIC fixture.
	File string `yaml:"file,omitempty" json:"file,omitempty"`

	Stratum Stratum `yaml:"stratum" json:"stratum"`

	// LabelledAgainst is the transcript the labeller actually read, as
	// `tier2.transcripts.model` holds it — runner-qualified, so
	// `whisper.cpp/small.en` and never `small.en`.
	//
	// IT IS NOT REDUNDANT WITH Hash, and §1's [rev] is the whole reason it
	// exists: the hash identifies the AUDIO. A memo may carry several
	// transcripts, they are unique on (memo_id, model), `chronicle
	// retranscribe` makes that ordinary, and TranscriptForScribe returns the
	// best-ranked one AT QUERY TIME. A large-v3 pass would change the text
	// every memo is scored from while every hash still matched, and the run
	// would read as prompt drift. Real stratum only: a fixture has no ASR
	// between the labeller and the text.
	LabelledAgainst string `yaml:"labelled_against,omitempty" json:"labelled_against,omitempty"`

	Destination scribe.Destination `yaml:"destination" json:"destination"`
	// TicketType is required exactly when Destination is TICKET, which is the
	// same rule scribe.Parse holds a proposal to. A label is a well-formed
	// answer or it is not an answer.
	TicketType string `yaml:"ticket_type,omitempty" json:"ticket_type,omitempty"`

	// Confident is a POINTER because absent is not false. A label file that
	// omits the field would otherwise silently claim the labeller was unsure,
	// which is the same distinction scribe.Parse draws for `confidence`.
	Confident *bool `yaml:"confident" json:"confident"`

	Reason string `yaml:"reason" json:"reason"`

	AlsoDefensible []Alternative `yaml:"also_defensible" json:"also_defensible"`
	Unhandled      []Gap         `yaml:"unhandled" json:"unhandled"`
}

// ID is the label's identity in a report: its hash or its file, whichever it
// carries.
func (l Label) ID() string {
	if l.Hash != "" {
		return l.Hash
	}
	return l.File
}

// Short is the identity trimmed for a terminal table. A 64-character hash makes
// every row wrap and the first eight distinguish twenty-one memos comfortably.
//
// LENGTH-GUARDED, and not defensively. Set.Validate calls this to build the
// prefix for its OWN error messages, before problems() has checked the hash is
// a sha256 — so an unguarded slice crashes on precisely the malformed input the
// next check has a written diagnostic for, and the file that would have said
// `hash "abc123" is not a lowercase sha256` says nothing at all.
func (l Label) Short() string {
	if l.Hash != "" {
		if len(l.Hash) >= 8 {
			return l.Hash[:8]
		}
		return l.Hash
	}
	return strings.TrimSuffix(path.Base(l.File), ".md")
}

// IsConfident reports the field, treating an unset pointer as not confident.
// Validate refuses an unset one, so this only ever answers for a valid label.
func (l Label) IsConfident() bool { return l.Confident != nil && *l.Confident }

// Answers reports every destination a scorer accepts leniently: the label plus
// anything the labeller marked defensible.
func (l Label) Answers() []Alternative {
	out := make([]Alternative, 0, 1+len(l.AlsoDefensible))
	out = append(out, Alternative{Destination: l.Destination, TicketType: l.TicketType})
	return append(out, l.AlsoDefensible...)
}

// Set is the whole label file.
type Set struct {
	// Version is the file format's, and it is checked rather than ignored: a
	// harness that silently accepts a shape it does not understand scores
	// something nobody meant.
	Version int     `yaml:"version" json:"version"`
	Labels  []Label `yaml:"labels" json:"labels"`
}

// hashPattern is the shape tier2.memos.content_hash holds: lowercase sha256.
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Load reads and validates a label file.
func Load(p string) (Set, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return Set{}, fmt.Errorf("eval: open labels: %w", err)
	}
	s, err := Read(bytes.NewReader(b))
	if err != nil {
		return Set{}, fmt.Errorf("eval: %s: %w", p, err)
	}
	return s, nil
}

// Read decodes and validates a label file from any reader.
//
// KnownFields is on, so a misspelled key is an error rather than a field that
// silently keeps its zero value. `confidnet: false` must not be a label that
// quietly claims certainty.
func Read(r io.Reader) (Set, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var s Set
	if err := dec.Decode(&s); err != nil {
		return Set{}, fmt.Errorf("decode labels: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Set{}, err
	}
	return s, nil
}

// Validate reports EVERY problem with the set at once rather than the first.
//
// The same reason ShapeErrors gives for a proposal: a file fixed one line at a
// time costs a round trip per line, and a label file is edited by a person
// rather than retried by a model.
func (s Set) Validate() error {
	var errs []string
	bad := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if s.Version != 1 {
		bad("version must be 1, got %d", s.Version)
	}
	if len(s.Labels) == 0 {
		bad("the set is empty")
	}

	seen := map[string]int{}
	for i, l := range s.Labels {
		where := fmt.Sprintf("labels[%d]", i)
		if id := l.ID(); id != "" {
			if first, dup := seen[id]; dup {
				bad("%s: %q is already labelled at labels[%d] — one memo, one label", where, id, first)
			} else {
				seen[id] = i
			}
			where = fmt.Sprintf("labels[%d] (%s)", i, l.Short())
		}
		for _, msg := range l.problems() {
			bad("%s: %s", where, msg)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid label set:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// problems is one label's validation, and the rules it enforces are the
// decision's rather than a schema's convenience.
func (l Label) problems() []string {
	var out []string
	bad := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	// IDENTITY. A real label names a memo by content hash; a synthetic one
	// names a committed fixture. Both, or neither, means the harness cannot
	// tell what it is meant to route.
	switch {
	case l.Hash != "" && l.File != "":
		bad("has both `hash` and `file` — a real memo is named by hash, a fixture by file")
	case l.Hash == "" && l.File == "":
		bad("has neither `hash` nor `file`")
	}

	switch l.Stratum {
	case StratumReal:
		if l.File != "" {
			bad("stratum `real` is named by `hash`, not `file` — a real transcript is tier 2 and is not in this repo (§1)")
		}
		if l.Hash != "" && !hashPattern.MatchString(l.Hash) {
			bad("`hash` %q is not a lowercase sha256 — it is tier2.memos.content_hash verbatim", l.Hash)
		}
		// §1's [rev]: the pin, without which a re-transcription reads as
		// prompt drift.
		switch {
		case l.LabelledAgainst == "":
			bad("`labelled_against` is required on the real stratum — record the transcript model the labeller read, or a re-transcription will read as prompt drift (§1)")
		case !strings.Contains(l.LabelledAgainst, "/"):
			bad("`labelled_against` %q is not runner-qualified — tier2.transcripts.model holds `whisper.cpp/small.en`, never `small.en`", l.LabelledAgainst)
		}
	case StratumSynthetic:
		if l.Hash != "" {
			bad("stratum `synthetic` is named by `file`, not `hash` — nobody said it, so there is no memo")
		}
		if l.LabelledAgainst != "" {
			bad("`labelled_against` is meaningless on the synthetic stratum: the fixture IS the text, with no ASR between")
		}
		if l.File != "" {
			if err := checkFixturePath(l.File); err != nil {
				bad("`file` %q %s", l.File, err)
			}
		}
	case "":
		bad("`stratum` is required — it decides who may look at this label (§2)")
	default:
		bad("`stratum` must be one of %v, got %q", Strata, l.Stratum)
	}

	// THE ANSWER, held to the same shape a proposal is held to.
	if !slices.Contains(scribe.Destinations, l.Destination) {
		bad("`destination` must be one of %v, got %q", scribe.Destinations, l.Destination)
	}
	if err := checkTicketType(l.Destination, l.TicketType); err != nil {
		bad("%s", err)
	}

	if strings.TrimSpace(l.Reason) == "" {
		bad("`reason` is required — a label nobody can argue with is a label nobody checked")
	}

	// AMBIGUITY, and the pair that makes it mean something (§4).
	//
	// The two rules are one rule read from both ends: `confident` and
	// `also_defensible` must agree. An unsure label that names no alternative
	// records a shrug the scorer cannot use, and a confident label that names
	// one is not confident.
	switch {
	case l.Confident == nil:
		bad("`confident` is required — absent is not false, and five of twenty-one real memos are genuinely arguable (§4)")
	case !*l.Confident && len(l.AlsoDefensible) == 0:
		bad("`confident: false` with an empty `also_defensible` — name the other defensible label, or the ambiguity tax cannot be measured (§4)")
	case *l.Confident && len(l.AlsoDefensible) > 0:
		bad("`confident: true` with %d alternative(s) — naming another defensible label is what `confident: false` means", len(l.AlsoDefensible))
	}

	primary := Alternative{Destination: l.Destination, TicketType: l.TicketType}
	altSeen := map[string]bool{primary.String(): true}
	for _, a := range l.AlsoDefensible {
		if !slices.Contains(scribe.Destinations, a.Destination) {
			bad("`also_defensible` destination must be one of %v, got %q", scribe.Destinations, a.Destination)
			continue
		}
		if err := checkTicketType(a.Destination, a.TicketType); err != nil {
			bad("`also_defensible`: %s", err)
			continue
		}
		if altSeen[a.String()] {
			bad("`also_defensible` repeats %s — it is either the label or an alternative, not both", a)
			continue
		}
		altSeen[a.String()] = true
	}

	// §7's known-unhandled cases.
	gapSeen := map[Gap]bool{}
	for _, g := range l.Unhandled {
		if !slices.Contains(Gaps, g) {
			bad("`unhandled` must be one of %v, got %q", Gaps, g)
			continue
		}
		if gapSeen[g] {
			bad("`unhandled` repeats %q", g)
			continue
		}
		gapSeen[g] = true
	}

	return out
}

// checkTicketType holds a label to the contract's own rule: a ticket type is
// required exactly when the destination is TICKET, and is one of Switchyard's
// four. scribe.Parse says the same thing to a model.
func checkTicketType(d scribe.Destination, t string) error {
	if d == scribe.DestTicket {
		if !slices.Contains(scribe.TicketTypes, t) {
			return fmt.Errorf("`ticket_type` must be one of %v for TICKET, got %q", scribe.TicketTypes, t)
		}
		return nil
	}
	if t != "" {
		return fmt.Errorf("`ticket_type` %q is set on %s, which has no type", t, d)
	}
	return nil
}

// checkFixturePath keeps a synthetic label pointing at something committed
// beside the label file.
//
// Absolute paths and `..` are refused because the path is opened relative to
// the label file's directory: a fixture that escaped it would make the set
// depend on a machine rather than on the repo, which is the one property §1
// buys by committing the synthetic stratum in full.
func checkFixturePath(p string) error {
	switch {
	case path.IsAbs(p), strings.HasPrefix(p, "/"), strings.Contains(p, `\`):
		return fmt.Errorf("must be relative to the label file")
	case p != path.Clean(p):
		return fmt.Errorf("is not a clean path (want %q)", path.Clean(p))
	case strings.HasPrefix(p, "../"), p == "..":
		return fmt.Errorf("escapes the label file's directory")
	case !strings.HasSuffix(p, ".md"):
		return fmt.Errorf("must be a .md fixture")
	}
	return nil
}

// Select returns the labels in one stratum, or all of them.
//
// It exists so that "CHRN-30 may look at exactly one stratum" is something a
// caller expresses rather than something it remembers to filter.
func (s Set) Select(st Stratum) []Label {
	if st == "" {
		return slices.Clone(s.Labels)
	}
	var out []Label
	for _, l := range s.Labels {
		if l.Stratum == st {
			out = append(out, l)
		}
	}
	return out
}
