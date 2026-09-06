// Package catalogue is what Scribe is allowed to route to, as ONE SNAPSHOT
// read twice.
//
// That shape is the salvage's, and it is the piece CHRN-30 reuses verbatim:
// *one fetch, two readers*. The same snapshot is rendered into the prompt and
// then validated against, so the list the model saw IS the list its answer is
// checked against. Re-fetching for validation would open a window where a
// project appears in the prompt and is gone by the time the answer is checked.
//
// WHY THIS EXISTS ALONGSIDE scribe.Catalogue. That interface is
// `HasProject(key) bool` / `HasPage(path) bool` — membership, which is all
// stage 2 needs. A prompt needs to RENDER the list, and no amount of
// membership testing produces `- ARGY — Argosy: …`. The memos are why it
// matters: they name services and never keys. "Argosy", "Signet", "Purser" —
// so with no list in the prompt the model cannot answer project_key at all,
// and the only honest answer for every TICKET is "".
//
// A Snapshot satisfies scribe.Catalogue, so the same value is both readers.
package catalogue

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// Project is one routable destination, as the prompt renders it.
//
// Description is carried because a key and a name do not tell a model whether
// a memo about "the tablet in the kitchen" belongs to EIDO or SERV. It is the
// field that does the routing work, and it is also the field that grows the
// prompt, which is why the fixture file writes one line per project rather
// than truncating a paragraph.
type Project struct {
	Key         string `yaml:"key" json:"key"`
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
}

// Snapshot is one reading of the world, and it does not change under its
// readers. Never cached across runs — CHRN-31's reason: a stale catalogue
// produces the worst kind of error, where the destination it picked does exist
// and is merely the wrong one, and nothing in the proposal looks wrong.
type Snapshot struct {
	Version  int       `yaml:"version" json:"version"`
	Projects []Project `yaml:"projects" json:"projects"`
	Pages    []string  `yaml:"pages" json:"pages"`

	// Notes are the CHR-#### references a NOTE proposal's `target_note` may
	// name — CHRN-94's half of "one fetch, two readers".
	//
	// A LIST, AND THEREFORE A BOUNDED ONE. Pages are a tree and stay small;
	// notes grow without limit, so rendering every note in the corpus into
	// every prompt does not scale and this field is not where that is solved.
	// Choosing WHICH notes are candidates for a given memo is retrieval over
	// the corpus, which CHRN-36 §7 assigns to CHRN-41 and which no ticket has
	// yet picked up. Until then this is empty and every non-`create` verb
	// clears at stage 2, which is the correct answer rather than a gap.
	Notes []string `yaml:"notes" json:"notes"`

	// sha is the hash of the bytes this snapshot was read from, or computed
	// over its content when it came from a live fetch.
	sha string
}

// LoadFixture reads the committed catalogue for the synthetic stratum.
//
// SYNTHETIC ONLY, and the plan says why: a live fetch would tie the synthetic
// score to Switchyard's state on the day, which is the property CHRN-36 §1
// bought by committing that stratum in the first place.
func LoadFixture(fsys fs.FS, path string) (*Snapshot, error) {
	b, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	return Parse(b)
}

// LoadFixtureFile is LoadFixture against the real filesystem.
func LoadFixtureFile(path string) (*Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("catalogue: %w", err)
	}
	s, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("catalogue: %s: %w", path, err)
	}
	return s, nil
}

// Parse decodes and validates a catalogue, and records the hash of the bytes
// it came from.
//
// KnownFields is on for the same reason the label file has it on: a misspelled
// key must not become a field that silently keeps its zero value.
func Parse(b []byte) (*Snapshot, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	var s Snapshot
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("catalogue: decode: %w", err)
	}
	if s.Version != 1 {
		return nil, fmt.Errorf("catalogue: version must be 1, got %d", s.Version)
	}
	if len(s.Projects) == 0 {
		return nil, fmt.Errorf("catalogue: no projects — a router with no destinations cannot answer project_key")
	}
	seen := map[string]bool{}
	for i, p := range s.Projects {
		switch {
		case p.Key == "":
			return nil, fmt.Errorf("catalogue: projects[%d] has no key", i)
		case p.Key != strings.ToUpper(p.Key):
			// The contract requires an uppercase project_key and validates it,
			// so a lowercase key here would be a catalogue that can only
			// produce answers stage 1 rejects.
			return nil, fmt.Errorf("catalogue: projects[%d] key %q must be uppercase", i, p.Key)
		case p.Name == "":
			return nil, fmt.Errorf("catalogue: project %s has no name", p.Key)
		case seen[p.Key]:
			return nil, fmt.Errorf("catalogue: project %s appears twice", p.Key)
		}
		seen[p.Key] = true
	}
	// Notes are validated for SHAPE only, on the projects' pattern: a
	// malformed entry here would be offered to the model and then cleared by
	// stage 2, which reads as a hallucination the model did not commit.
	seenNote := map[int64]bool{}
	for i, n := range s.Notes {
		num, err := store.ParseNoteRef(n)
		if err != nil {
			return nil, fmt.Errorf("catalogue: notes[%d]: %w", i, err)
		}
		if seenNote[num] {
			return nil, fmt.Errorf("catalogue: note %s appears twice", store.FormatNoteRef(num))
		}
		seenNote[num] = true
	}

	sum := sha256.Sum256(b)
	s.sha = hex.EncodeToString(sum[:])
	return &s, nil
}

// SHA256 identifies the catalogue in the eval report, beside the labels hash.
//
// NOT DECORATION. The labels prove which answers a run was scored against; the
// catalogue decides which project keys were AVAILABLE to be answered. Change
// it and every TICKET proposal can move while the labels hash is unchanged,
// which is CHRN-36 §1's failure — a run that reads as prompt drift when the
// input actually moved — with a different noun.
func (s *Snapshot) SHA256() string { return s.sha }

// HasProject reports whether a key is live. Half of scribe.Catalogue.
func (s *Snapshot) HasProject(key string) bool {
	for _, p := range s.Projects {
		if p.Key == key {
			return true
		}
	}
	return false
}

// HasPage reports whether a page path exists exactly. Half of
// scribe.Catalogue.
//
// Answers false to everything while Pages is empty, which is the state until
// CHRN-37 and is the correct answer rather than a gap: there is nowhere for a
// note to hang.
func (s *Snapshot) HasPage(path string) bool {
	for _, p := range s.Pages {
		if p == path {
			return true
		}
	}
	return false
}

// HasNote reports whether a note reference resolves. Third of
// scribe.Catalogue.
//
// COMPARED BY NUMBER AND NOT BY STRING, because the reference is lenient by
// design: CHR-311, chr-0311 and CHR-00311 are one note, and a string compare
// would resolve whichever spelling the catalogue happened to hold and clear
// the other two. `store.ParseNoteRef` is the parse, and this package may call
// it because nothing in internal/store imports this one.
func (s *Snapshot) HasNote(ref string) bool {
	want, err := store.ParseNoteRef(ref)
	if err != nil {
		return false
	}
	for _, n := range s.Notes {
		if got, err := store.ParseNoteRef(n); err == nil && got == want {
			return true
		}
	}
	return false
}

// RenderProjects is the prompt's half of "one fetch, two readers".
//
// One line per project, no truncation. The salvage cut descriptions at 150
// characters mid-word with no ellipsis, which is what a budget looks like when
// nobody chose one; here the budget lives in the catalogue file, where a person
// writing a description can see it.
func (s *Snapshot) RenderProjects() string {
	var b strings.Builder
	for _, p := range s.Projects {
		b.WriteString("- " + p.Key + " — " + p.Name)
		if p.Description != "" {
			b.WriteString(": " + p.Description)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderPages is the other half, and it is deliberately explicit when empty.
//
// A prompt that simply omits the page list invites the model to invent a path;
// stage 2 then clears it, and CHRN-36's hallucination rate measures a prompt
// that asked for the hallucination. Saying "there are none" is the difference
// between a model with no options and a model with no information.
func (s *Snapshot) RenderPages() string {
	if len(s.Pages) == 0 {
		return "(none — the page tree does not exist yet, so `page_path` and `nearest_page` MUST be null)"
	}
	var b strings.Builder
	for _, p := range s.Pages {
		b.WriteString("- " + p + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderNotes is the third reader, and it is explicit when empty for exactly
// RenderPages' reason.
//
// The empty text names the CONSEQUENCE rather than the absence, because the
// consequence is what constrains the answer: with no note to act on, `create`
// is the only verb that can be satisfied, and a model told merely that the
// list is empty will still reach for `append` on a memo that plainly refers
// back to something.
func (s *Snapshot) RenderNotes() string {
	if len(s.Notes) == 0 {
		return "(none — no notes exist yet, so `verb` MUST be `create` and `target_note` MUST be null)"
	}
	var b strings.Builder
	for _, n := range s.Notes {
		b.WriteString("- " + n + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
