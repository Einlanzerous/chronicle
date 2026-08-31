package eval

import (
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// The committed set is a build artefact of this ticket, so it is guarded like
// one. This is the half of CHRN-36 that CAN run in CI: it needs no corpus, no
// GPU and no Ollama, only the file.
func TestTheCommittedLabelSetIsValid(t *testing.T) {
	set, err := Load("../../../docs/eval/routing-v1.yaml")
	if err != nil {
		t.Fatalf("the committed label set does not load: %v", err)
	}

	real, synth := set.Select(StratumReal), set.Select(StratumSynthetic)
	if len(real) != 21 {
		t.Errorf("real stratum = %d labels, want 21 (§3, after R2's four)", len(real))
	}
	if len(synth) != 25 {
		t.Errorf("synthetic stratum = %d labels, want 25", len(synth))
	}

	// §3's tally, counted rather than described — the decision's own first
	// draft got this wrong by describing it.
	want := map[scribe.Destination]int{
		scribe.DestTicket: 12, scribe.DestNote: 3,
		scribe.DestDiscussion: 3, scribe.DestDiscard: 3,
	}
	got := map[scribe.Destination]int{}
	unsure, unhandled := 0, 0
	for _, l := range real {
		got[l.Destination]++
		if !l.IsConfident() {
			unsure++
		}
		if len(l.Unhandled) > 0 {
			unhandled++
		}
	}
	for d, n := range want {
		if got[d] != n {
			t.Errorf("real %s = %d, want %d", d, got[d], n)
		}
	}
	if unsure != 5 {
		t.Errorf("arguable real labels = %d, want 5 (§3)", unsure)
	}
	if unhandled != 3 {
		t.Errorf("unhandled real labels = %d, want 3 (§7)", unhandled)
	}

	// Every real label pins the transcript it was assigned against, and the
	// pin is runner-qualified. Validate enforces both; this is the assertion
	// that the committed file actually carries them.
	for _, l := range real {
		if l.LabelledAgainst != "whisper.cpp/small.en" {
			t.Errorf("%s: labelled_against = %q, want whisper.cpp/small.en", l.Short(), l.LabelledAgainst)
		}
	}
}

// The corpus is tier 2 and this repository is public. Nothing under docs/eval
// may carry authored text, and this checks the whole directory rather than only
// the labels — the failure §1 guards against is a transcript DROPPED BESIDE the
// set, which no amount of label validation would notice.
//
// So: every file under docs/eval is either the label file itself or a synthetic
// fixture some label references, and no real label names a file at all.
func TestNoRealTranscriptIsCommitted(t *testing.T) {
	const labels = "routing-v1.yaml"
	set, err := Load("../../../docs/eval/" + labels)
	if err != nil {
		t.Fatal(err)
	}

	accounted := map[string]bool{labels: true}
	for _, l := range set.Labels {
		if l.Stratum == StratumReal && l.File != "" {
			t.Errorf("%s: a real label names a file, which would put authored text in git (§1)", l.Short())
		}
		if l.File != "" {
			accounted[l.File] = true
		}
	}

	err = fs.WalkDir(os.DirFS("../../../docs/eval"), ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		case !accounted[p]:
			t.Errorf("docs/eval/%s is committed and no label accounts for it. The only text that "+
				"belongs here is a fixture nobody said into a recorder (§1)", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Set.Validate builds its error prefix from Short() before problems() has
// checked the hash is a sha256, so a short hash used to crash the process at
// the exact point the code has a written diagnostic for it.
func TestAMalformedHashIsReportedRatherThanFatal(t *testing.T) {
	wantErr(t, strings.Replace(oneReal,
		"0000000000000000000000000000000000000000000000000000000000000001", "abc123", 1),
		"is not a lowercase sha256")
}

func mustParse(t *testing.T, body string) Set {
	t.Helper()
	s, err := Read(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return s
}

func wantErr(t *testing.T, body, substr string) {
	t.Helper()
	_, err := Read(strings.NewReader(body))
	if err == nil {
		t.Fatalf("want an error mentioning %q, got none", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error = %v\nwant it to mention %q", err, substr)
	}
}

const oneReal = `
version: 1
labels:
  - hash: 0000000000000000000000000000000000000000000000000000000000000001
    stratum: real
    labelled_against: whisper.cpp/small.en
    destination: NOTE
    confident: true
    reason: argues a principle
`

func TestAValidLabelParses(t *testing.T) {
	set := mustParse(t, oneReal)
	if len(set.Labels) != 1 || set.Labels[0].Destination != scribe.DestNote {
		t.Fatalf("got %+v", set.Labels)
	}
}

// §4's pair, from both ends. An unsure label that names no alternative records
// a shrug the scorer cannot use, and a confident one that names an alternative
// is not confident.
func TestAnUnsureLabelMustNameAnAlternative(t *testing.T) {
	wantErr(t, strings.Replace(oneReal, "confident: true", "confident: false", 1),
		"also_defensible")
}

func TestAConfidentLabelMayNotNameOne(t *testing.T) {
	wantErr(t, oneReal+`    also_defensible:
      - destination: DISCUSSION
`, "naming another defensible label is what")
}

// Absent is not false, for the same reason scribe.Parse checks the presence of
// `confidence` separately from its value.
func TestConfidentIsRequiredBecauseAbsentIsNotFalse(t *testing.T) {
	wantErr(t, strings.Replace(oneReal, "    confident: true\n", "", 1), "`confident` is required")
}

// §1's [rev]: the hash identifies the audio, not the words.
func TestARealLabelMustPinItsTranscript(t *testing.T) {
	wantErr(t, strings.Replace(oneReal, "    labelled_against: whisper.cpp/small.en\n", "", 1),
		"labelled_against")
}

func TestTheTranscriptPinIsRunnerQualified(t *testing.T) {
	wantErr(t, strings.Replace(oneReal, "whisper.cpp/small.en", "small.en", 1), "runner-qualified")
}

func TestARealLabelIsNamedByHashAndNotByFile(t *testing.T) {
	wantErr(t, `
version: 1
labels:
  - file: synthetic/x.md
    stratum: real
    labelled_against: whisper.cpp/small.en
    destination: NOTE
    confident: true
    reason: r
`, "is not in this repo")
}

func TestASyntheticLabelIsNamedByFileAndHasNoPin(t *testing.T) {
	wantErr(t, `
version: 1
labels:
  - file: synthetic/x.md
    stratum: synthetic
    labelled_against: whisper.cpp/small.en
    destination: NOTE
    confident: true
    reason: r
`, "no ASR between")
}

func TestAFixturePathMayNotEscapeTheLabelFile(t *testing.T) {
	wantErr(t, `
version: 1
labels:
  - file: ../../../etc/passwd.md
    stratum: synthetic
    destination: NOTE
    confident: true
    reason: r
`, "escapes")
}

// A label is held to the contract's own rule, so that a label is a well-formed
// answer rather than a note about one.
func TestTicketTypeIsRequiredExactlyForTickets(t *testing.T) {
	wantErr(t, strings.Replace(oneReal, "destination: NOTE", "destination: TICKET", 1),
		"`ticket_type` must be one of")
	wantErr(t, oneReal+"    ticket_type: task\n", "which has no type")
}

func TestUnhandledIsAClosedSet(t *testing.T) {
	wantErr(t, oneReal+`    unhandled:
      - cross_reference
`, "`unhandled` must be one of")
}

func TestOneMemoGetsOneLabel(t *testing.T) {
	wantErr(t, oneReal+strings.TrimPrefix(oneReal, "\nversion: 1\nlabels:\n"), "already labelled")
}

// A misspelled key must not become a field that silently keeps its zero value:
// `confidnet: false` would otherwise be a label quietly claiming certainty.
func TestAMisspelledKeyIsAnError(t *testing.T) {
	wantErr(t, oneReal+"    confidnet: false\n", "field confidnet not found")
}

func TestTheFileFormatVersionIsChecked(t *testing.T) {
	wantErr(t, strings.Replace(oneReal, "version: 1", "version: 2", 1), "version must be 1")
}

// Every problem at once, so a person editing the file gets one round trip
// rather than one per line.
func TestValidationReportsEveryProblemAtOnce(t *testing.T) {
	_, err := Read(strings.NewReader(`
version: 3
labels:
  - stratum: real
    destination: WHATEVER
    reason: ""
`))
	if err == nil {
		t.Fatal("want errors")
	}
	for _, want := range []string{"version must be 1", "neither `hash` nor `file`",
		"`destination` must be one of", "`reason` is required", "`confident` is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q from:\n%v", want, err)
		}
	}
}

func TestSelectFiltersByStratum(t *testing.T) {
	set := mustParse(t, oneReal+`  - file: synthetic/x.md
    stratum: synthetic
    destination: DISCARD
    confident: true
    reason: r
`)
	if got := len(set.Select(StratumReal)); got != 1 {
		t.Errorf("real = %d, want 1", got)
	}
	if got := len(set.Select("")); got != 2 {
		t.Errorf("all = %d, want 2", got)
	}
}
