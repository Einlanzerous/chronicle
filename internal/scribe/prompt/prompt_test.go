package prompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// hashV1 is the identity of v1: prompt.md, schema.json and options.json
// together. IF THIS TEST FAILS, one of those three changed.
//
// The guard exists because the version is half the identity of every proposal
// ever attributed. verify.sh already applies the same rule to the generated
// ASR client and to the schema, for the same reason: a generated artefact with
// no guard is one somebody hand-edits.
//
// WHEN TO BUMP Version, AND WHEN ONLY TO UPDATE THIS HASH. The failure the
// version prevents needs recorded results attributed to the old one: CHRN-36
// compares run N to run N−1 through the proposer string, and cannot do that if
// the string sat still while the prompt moved. So the line is whether a
// version has been USED to attribute something durable — a run in §2's log, or
// a row in tier1.memo_proposals.
//
//   - Before that: update this hash alone. Bumping on every edit during
//     development would make the version a change counter, which is a version
//     nobody can reason about.
//   - After it: bump Version and this hash together, in one commit.
//
// v1 has attributed nothing yet, so CHRN-30's own iterations move the hash.
const hashV1 = "6e7417b52f8d931372ede8a93d632887656724c5dcf1cc8c3a32eb99456d2c98"

func TestThePromptCannotChangeWithoutBumpingItsVersion(t *testing.T) {
	got := Hash()
	if got != hashV1 {
		t.Fatalf(`the prompt, schema or options changed.

  was: %s
  now: %s

This is not a bug — it is the guard.

If NOTHING has been recorded under %q yet (no run in CHRN-36 §2's log, no row
in tier1.memo_proposals), set hashV1 to the "now" value and carry on.

Otherwise do BOTH in the same commit: bump Version, and set hashV1 — because
the proposer string is what CHRN-36 uses to tell a prompt regression from a
model regression, and a string that sits still while the prompt moves makes
that comparison a lie.

If you did not mean to change the prompt at all, revert instead.`, hashV1, got, Version)
	}
}

// The hash covers all three files, so a change to the decoding options is as
// loud as a change to a word of the text. `think: false → true` changes the
// answer; so does num_predict.
func TestTheHashCoversTheOptionsAndNotOnlyTheText(t *testing.T) {
	before := Hash()
	saved := optionsJSON
	t.Cleanup(func() { optionsJSON = saved })

	optionsJSON = []byte(`{"think":true,"options":{"temperature":0}}`)
	if Hash() == before {
		t.Fatal("the options changed and the hash did not")
	}
}

func TestFillSubstitutesEverySlot(t *testing.T) {
	out := Fill("- ARGY — Argosy", "(none)", "so the thing is", "")
	for _, want := range []string{"- ARGY — Argosy", "(none)", "so the thing is"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "{{") {
		t.Errorf("an unfilled slot survived:\n%s", out)
	}
}

// /api/generate has no conversation, so a retry is the same prompt re-rendered
// with the errors spliced in. At temperature 0 that is what makes a retry a
// different question rather than the same one asked twice.
func TestFeedbackIsSplicedIntoThePromptRatherThanAppended(t *testing.T) {
	first := Fill("p", "pg", "t", "")
	retry := Fill("p", "pg", "t", "- confidence: must be between 0 and 1, got 1.5")

	if strings.Contains(first, "previous attempt") {
		t.Error("the first attempt carries retry text")
	}
	if !strings.Contains(retry, "confidence: must be between 0 and 1") {
		t.Fatal("the validation error did not reach the prompt")
	}
	if retry == first {
		t.Fatal("a retry produced an identical prompt, which at temperature 0 is the same answer")
	}
	// The transcript must still be last: an instruction after the memo reads
	// as part of the memo.
	if strings.LastIndex(retry, "# Transcript") < strings.Index(retry, "previous attempt") {
		t.Error("the feedback block landed after the transcript")
	}
}

func TestAnOverlongTranscriptIsCappedRatherThanOverflowing(t *testing.T) {
	out := Fill("p", "pg", strings.Repeat("a", MaxTranscriptBytes+5_000), "")
	if n := strings.Count(out, "a") - strings.Count(text, "a"); n != MaxTranscriptBytes {
		t.Fatalf("transcript rendered %d bytes, want the %d cap", n, MaxTranscriptBytes)
	}
}

// The cap is in bytes, so a transcript of multi-byte runes can be cut mid-rune
// — whisper emits em dashes and curly quotes, so this is reachable. Invalid
// UTF-8 in a prompt is a decode the model was never asked for.
func TestCappingNeverSplitsARune(t *testing.T) {
	// 2 bytes each, so the byte cap lands exactly between two runes only if it
	// is even; make it odd by prefixing one ASCII byte.
	long := "a" + strings.Repeat("\u03c9", MaxTranscriptBytes)
	out := Fill("p", "pg", long, "")
	if !utf8.ValidString(out) {
		t.Fatal("the rendered prompt is not valid UTF-8")
	}
	body := out[strings.Index(out, "# Transcript"):]
	if n := strings.Count(body, "\u03c9"); n != (MaxTranscriptBytes-1)/2 {
		t.Fatalf("rendered %d runes, want %d whole ones", n, (MaxTranscriptBytes-1)/2)
	}
}

// The schema is what makes field order a property of the decode. If the order
// here drifts from the order prompt.md documents, the grammar and the
// instructions disagree and the reason stops preceding the destination.
func TestTheSchemaEmitsReasonBeforeDestination(t *testing.T) {
	var s struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(schemaJSON, &s); err != nil {
		t.Fatal(err)
	}
	// runner_up sits between the destination and the confidence on purpose:
	// the grammar makes the model name its second-best answer BEFORE it says
	// how sure it is, so the number is made of something rather than felt.
	want := []string{"reason", "destination", "runner_up", "confidence", "title",
		"nearest_page", "project_key", "ticket_type", "description", "body", "opening_post"}
	if len(s.Required) != len(want) {
		t.Fatalf("required = %v, want %v", s.Required, want)
	}
	for i := range want {
		if s.Required[i] != want[i] {
			t.Fatalf("required[%d] = %q, want %q — reason must be emitted first", i, s.Required[i], want[i])
		}
	}
	// Property order in the raw bytes is what llama.cpp's grammar follows.
	if ri, di := strings.Index(string(schemaJSON), `"reason"`), strings.Index(string(schemaJSON), `"destination"`); ri > di {
		t.Error("`reason` is declared after `destination` in properties")
	}
}

// Nothing is inherited from Ollama's defaults, because a default that changes
// between releases is a router that changes with it.
func TestEveryDecodingKnobIsPinned(t *testing.T) {
	r, err := Options()
	if err != nil {
		t.Fatal(err)
	}
	if r.Think {
		t.Error("think is on; v1 pins it off, and thinking would make `one completion, not two` two passes in one call")
	}
	for _, k := range []string{"temperature", "seed", "num_ctx", "num_predict", "repeat_penalty", "top_k", "top_p"} {
		if _, ok := r.Options[k]; !ok {
			t.Errorf("%s is not pinned", k)
		}
	}
	if v := r.Options["temperature"]; v != float64(0) {
		t.Errorf("temperature = %v, want 0 — the model's own default is 1", v)
	}
	n, err := NumCtx()
	if err != nil {
		t.Fatal(err)
	}
	// Prompt plus the transcript cap plus the output must fit, or Ollama
	// truncates from the front and drops these instructions.
	if min := (MaxTranscriptBytes / 4) + 4_000; n < min {
		t.Errorf("num_ctx = %d, too small for a %d-char transcript plus the prompt (want >= %d)",
			n, MaxTranscriptBytes, min)
	}
}

// CHRN-36 §2 holds the real stratum out so the score is not memorisation. The
// same argument applies one level down: an example lifted from the synthetic
// fixtures would turn the only stratum this ticket may look at into a
// memorisation test.
//
// Checked by shingle rather than by eye — a ten-word run shared with any
// fixture is either a quotation or a paraphrase close enough to matter.
func TestNoSynthenticFixtureIsQuotedInThePrompt(t *testing.T) {
	const dir = "../../../docs/eval/synthetic"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	promptShingles := shingles(text, 10)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for s := range shingles(string(b), 10) {
			if promptShingles[s] {
				t.Errorf("%s shares a ten-word run with the prompt: %q", e.Name(), s)
			}
		}
	}
}

func shingles(s string, n int) map[string]bool {
	words := strings.Fields(strings.ToLower(s))
	out := map[string]bool{}
	for i := 0; i+n <= len(words); i++ {
		out[strings.Join(words[i:i+n], " ")] = true
	}
	return out
}

// The backoff must inspect only the TAIL. An earlier version asked
// utf8.ValidString of the WHOLE string, so one bad byte anywhere before the cap
// could never be trimmed away by shortening the end — the loop ran to empty and
// the model routed a fragment, silently.
func TestOneBadByteEarlyDoesNotDiscardTheTranscript(t *testing.T) {
	body := "the thing I want is " + "\xff" + strings.Repeat("a", MaxTranscriptBytes)
	out := Fill("p", "pg", body, "")

	rendered := out[strings.Index(out, "# Transcript"):]
	if n := strings.Count(rendered, "a"); n < MaxTranscriptBytes/2 {
		t.Fatalf("only %d transcript bytes survived one invalid byte", n)
	}
	if !strings.Contains(rendered, "the thing I want is") {
		t.Error("the start of the transcript was thrown away")
	}
}

// At most three bytes come off, which is all a split rune can be.
func TestTheBackoffTrimsAtMostARune(t *testing.T) {
	// The cap lands mid-rune: one ASCII byte then 3-byte runes.
	body := "a" + strings.Repeat("世", MaxTranscriptBytes)
	out := Fill("p", "pg", body, "")
	if !utf8.ValidString(out) {
		t.Fatal("the rendered prompt is not valid UTF-8")
	}
	rendered := out[strings.Index(out, "# Transcript"):]
	got := strings.Count(rendered, "世")*3 + 1
	if MaxTranscriptBytes-got > 3 {
		t.Fatalf("trimmed %d bytes, want at most 3", MaxTranscriptBytes-got)
	}
}
