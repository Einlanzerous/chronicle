// Package prompt is the routing prompt v1, its output schema, and the decoding
// options — the three things that together decide what the model answers.
//
// ALL THREE ARE ONE ARTEFACT, and the version names all three. `scribe.Proposer`
// makes the identity of a proposal `runner/model@promptVersion`, and its comment
// is explicit about why the third part is not optional: without it CHRN-36
// cannot tell a prompt regression from a model regression, which is the one
// comparison the eval set exists to make. `think: false → true`, or num_predict
// 512 → 2048, changes the answer as surely as a word in the text does, so the
// guard hashes the prompt AND the schema AND the options.
//
// EMBEDDED, never read from a path at runtime — the argument the house style
// makes for migrations, that the binary and what it depends on ship together.
// A prompt loaded from disk would let the proposer string lie about what
// actually ran.
//
// The plan is CHRN-30's, approved 2026-08-31; section names in these comments
// refer to it.
package prompt

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

//go:embed prompt.md
var text string

//go:embed schema.json
var schemaJSON []byte

//go:embed options.json
var optionsJSON []byte

// Version is the third part of the proposer string. BUMP IT whenever any of
// the three embedded files changes — Guard fails the build until you do.
const Version = "v1"

// MaxTranscriptBytes caps what is pasted into the prompt. BYTES rather than
// runes, because the thing being budgeted is context window, and bytes track
// tokens more closely than rune counts do.
//
// The salvage used 20 000 and it is kept, with the reason now written down:
// num_ctx is sized around it. The longest transcript in the real corpus is
// 5 801 characters, so this is headroom rather than a limit anybody meets —
// but a memo that did exceed it would otherwise push the instructions out of
// the context window from the front, silently (see router's num_ctx check).
const MaxTranscriptBytes = 20_000

// Request is the shape options.json carries: everything about the call that is
// not the prompt text or the schema.
type Request struct {
	// Think is stated rather than inherited. gemma4:31b advertises a
	// `thinking` capability, so the default is whatever the next Ollama
	// release decides — and if thinking were on, "one completion, not two"
	// would quietly be two passes inside one call.
	Think   bool           `json:"think"`
	Options map[string]any `json:"options"`
}

// Options returns the decoding options, decoded fresh so a caller cannot
// mutate the package's copy.
func Options() (Request, error) {
	var r Request
	if err := json.Unmarshal(optionsJSON, &r); err != nil {
		return Request{}, fmt.Errorf("prompt: options.json: %w", err)
	}
	return r, nil
}

// NumCtx is the context window the options ask for, which the router asserts
// the request actually fit inside.
func NumCtx() (int, error) {
	r, err := Options()
	if err != nil {
		return 0, err
	}
	v, ok := r.Options["num_ctx"].(float64)
	if !ok {
		return 0, fmt.Errorf("prompt: options.json has no numeric num_ctx — Ollama would fall back to a small default and truncate from the front")
	}
	return int(v), nil
}

// Schema is the JSON schema passed as Ollama's `format`.
//
// NOT `format: "json"`, which constrains decoding to valid JSON and nothing
// more. A schema gets llama.cpp's grammar, and the grammar is what makes the
// field order in prompt.md a property of the decode rather than a convention
// the example encourages: the model emits `reason` before `destination` and
// cannot go back and edit it once it has committed.
//
// Range and length checks are NOT expressible here and stay in stage 1, which
// is why scribe.Parse keeps every one of its tests.
func Schema() json.RawMessage { return json.RawMessage(schemaJSON) }

// Fill renders the prompt for one memo.
//
// feedback is empty on the first attempt. On a retry it carries the previous
// raw output and the validation errors — and it is spliced into the PROMPT
// rather than appended as a turn, because /api/generate has no conversation.
// At temperature 0 this is load-bearing rather than stylistic: a retry with an
// unchanged prompt is the same answer, so a loop that does not change its
// input burns attempts to no purpose.
func Fill(projects, pages, transcript, feedback string) string {
	if len(transcript) > MaxTranscriptBytes {
		transcript = transcript[:MaxTranscriptBytes]
		// BACK OFF TO A RUNE BOUNDARY. Slicing bytes can cut a multi-byte rune
		// in half and put invalid UTF-8 into the prompt — whisper emits em
		// dashes and curly quotes, so this is reachable rather than theoretical.
		for len(transcript) > 0 && !utf8.ValidString(transcript) {
			transcript = transcript[:len(transcript)-1]
		}
	}
	block := ""
	if strings.TrimSpace(feedback) != "" {
		block = "\n# Your previous attempt was rejected\n\n" +
			"Fix exactly these problems and return the whole object again:\n\n" +
			feedback + "\n"
	}
	r := strings.NewReplacer(
		"{{PROJECT_LIST}}", projects,
		"{{PAGE_LIST}}", pages,
		"{{TRANSCRIPT}}", transcript,
		"{{FEEDBACK}}", block,
	)
	return r.Replace(text)
}

// Hash is the identity of the whole request shape: text, schema and options.
//
// Length-prefixed rather than concatenated, so that moving a byte from the end
// of one file to the start of the next cannot leave the hash unchanged.
func Hash() string {
	h := sha256.New()
	for _, part := range [][]byte{[]byte(text), schemaJSON, optionsJSON} {
		_, _ = fmt.Fprintf(h, "%d:", len(part))
		h.Write(part)
	}
	return hex.EncodeToString(h.Sum(nil))
}
