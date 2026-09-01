package catalogue

import (
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// One snapshot is both readers: rendered into the prompt, and handed to
// scribe.Reconcile as the thing the answer is validated against.
var _ scribe.Catalogue = (*Snapshot)(nil)

const two = `
version: 1
projects:
  - key: ARGY
    name: Argosy
    description: media streaming
  - key: SGNT
    name: Signet
pages: []
`

func TestASnapshotIsBothReaders(t *testing.T) {
	s, err := Parse([]byte(two))
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasProject("ARGY") || s.HasProject("NOPE") {
		t.Error("membership is wrong")
	}
	want := "- ARGY — Argosy: media streaming\n- SGNT — Signet"
	if got := s.RenderProjects(); got != want {
		t.Errorf("rendered:\n%s\nwant:\n%s", got, want)
	}
}

// A prompt that simply omits the page list invites the model to invent a path;
// stage 2 clears it, and CHRN-36's hallucination rate then measures a prompt
// that asked for the hallucination. Saying "there are none" is the difference
// between no options and no information.
func TestAnEmptyPageTreeSaysSoRatherThanRenderingNothing(t *testing.T) {
	s, err := Parse([]byte(two))
	if err != nil {
		t.Fatal(err)
	}
	got := s.RenderPages()
	if !strings.Contains(got, "none") || !strings.Contains(got, "null") {
		t.Fatalf("rendered %q, want it to say there are no pages and what that means", got)
	}
	if s.HasPage("anything") {
		t.Error("an empty tree matched a page")
	}
}

func TestParseRefusesACatalogueThatCannotBeAnswered(t *testing.T) {
	for _, tc := range []struct{ name, yaml, want string }{
		{"wrong version", "version: 2\nprojects: [{key: A, name: a}]\n", "version must be 1"},
		{"no projects", "version: 1\nprojects: []\n", "no projects"},
		{"lowercase key", "version: 1\nprojects: [{key: argy, name: a}]\n", "must be uppercase"},
		{"no name", "version: 1\nprojects: [{key: ARGY}]\n", "has no name"},
		{"duplicate", "version: 1\nprojects: [{key: A, name: a}, {key: A, name: b}]\n", "appears twice"},
		{"unknown field", "version: 1\nprojects: [{key: A, name: a, colour: red}]\n", "field colour not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// A lowercase key would be a catalogue that can only produce answers stage 1
// rejects: scribe.Parse requires an uppercase project_key.
func TestAnUppercaseKeyIsWhatTheContractValidates(t *testing.T) {
	s, err := Parse([]byte(two))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range s.Projects {
		if p.Key != strings.ToUpper(p.Key) {
			t.Errorf("%q is not uppercase", p.Key)
		}
	}
}

// The hash is over the BYTES, so a comment change moves it. That is correct:
// the report records it to say which catalogue a run saw, and a run that saw a
// different file saw a different world even if the project list is identical.
func TestTheHashIdentifiesTheBytes(t *testing.T) {
	a, err := Parse([]byte(two))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse([]byte(two + "\n# a comment\n"))
	if err != nil {
		t.Fatal(err)
	}
	if a.SHA256() == "" || a.SHA256() == b.SHA256() {
		t.Fatalf("hashes %q and %q should differ", a.SHA256(), b.SHA256())
	}
}

// The committed catalogue is an input to every synthetic run, so it is guarded
// like the label file: it must load, and it must contain the projects the
// fixtures actually talk about.
func TestTheCommittedCatalogueLoads(t *testing.T) {
	s, err := LoadFixtureFile("../../../docs/eval/catalogue-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pages) != 0 {
		t.Errorf("pages = %d; there is no page tree until CHRN-37", len(s.Pages))
	}
	// Every service a synthetic fixture names by name must be answerable, or
	// the fixture asks for a project key that cannot be given.
	for _, key := range []string{"ARGY", "LYCM", "SGNT", "CTFG", "SWY", "PRSR", "CTAW"} {
		if !s.HasProject(key) {
			t.Errorf("%s is missing, and a fixture names it", key)
		}
	}
	if s.SHA256() == "" {
		t.Error("no hash")
	}
}
