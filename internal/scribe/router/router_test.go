package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
)

func testCatalogue(t *testing.T) *catalogue.Snapshot {
	t.Helper()
	c, err := catalogue.Parse([]byte(`
version: 1
projects:
  - key: CHRN
    name: Chronicle
    description: voice notes
pages: []
`))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ollama stands in for the box. Each call returns the next canned response and
// records the prompt it was sent, so a test can assert what the retry actually
// asked.
type ollama struct {
	t        *testing.T
	replies  []map[string]any
	prompts  []string
	numCalls int
}

func (o *ollama) start(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/generate", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		o.prompts = append(o.prompts, req["prompt"].(string))
		i := o.numCalls
		o.numCalls++
		if i >= len(o.replies) {
			i = len(o.replies) - 1
		}
		_ = json.NewEncoder(w).Encode(o.replies[i])
	})
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"name":"m","digest":"sha256:deadbeefcafe"}]}`))
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func reply(body string) map[string]any {
	return map[string]any{"response": body, "done_reason": "stop", "prompt_eval_count": 100, "eval_count": 50}
}

func validProposal(dest, project string) string {
	p := map[string]any{
		"reason": "because it says so", "destination": dest, "confidence": 0.85,
		"title": "Do the thing", "nearest_page": nil, "project_key": project,
		"ticket_type": "task", "description": "## Summary\nx", "body": "x", "opening_post": "x",
	}
	b, _ := json.Marshal(p)
	return string(b)
}

func newTestRouter(t *testing.T, base string, attempts int) *Router {
	t.Helper()
	r, err := New(Options{BaseURL: base, Model: "m", Catalogue: testCatalogue(t), MaxAttempts: attempts})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// "Runs entirely on the local box" is one of the ticket's three Done-when
// clauses, and this is what makes it a property of the shipped binary rather
// than of a diff somebody read.
func TestTheClientRefusesAnyHostButTheConfiguredOne(t *testing.T) {
	srv := (&ollama{t: t, replies: []map[string]any{reply("{}")}}).start(t)
	r := newTestRouter(t, srv.URL, 1)

	// The configured host is reachable.
	if _, err := r.Digest(context.Background()); err != nil {
		t.Fatalf("the configured host was refused: %v", err)
	}
	// Nothing else is, even on a request the router itself constructs.
	req, err := http.NewRequest(http.MethodGet, "http://198.51.100.7:11434/api/tags", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.http.Do(req)
	if err == nil {
		t.Fatal("the client dialled a host other than the configured one")
	}
	if !strings.Contains(err.Error(), "refusing to dial") {
		t.Fatalf("err = %v, want the transport's refusal", err)
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    Options
		want string
	}{
		{"no url", Options{Model: "m", Catalogue: testCatalogue(t)}, "CHRONICLE_SCRIBE_OLLAMA_URL"},
		{"not a url", Options{BaseURL: "::nope", Model: "m", Catalogue: testCatalogue(t)}, "absolute http(s) URL"},
		{"wrong scheme", Options{BaseURL: "ftp://x", Model: "m", Catalogue: testCatalogue(t)}, "absolute http(s) URL"},
		{"no model", Options{BaseURL: "http://x:1", Catalogue: testCatalogue(t)}, "CHRONICLE_SCRIBE_MODEL"},
		{"no catalogue", Options{BaseURL: "http://x:1", Model: "m"}, "no catalogue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.o)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Ollama truncates from the FRONT when the prompt exceeds num_ctx — dropping
// the instructions, keeping the transcript — and says nothing. Without this
// assertion the router silently becomes a summariser and scores badly for a
// reason no report could show.
func TestAFilledContextWindowIsAnErrorRatherThanASilentTruncation(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{{
		"response": validProposal("NOTE", ""), "done_reason": "stop",
		"prompt_eval_count": 16384, "eval_count": 10,
	}}}
	r := newTestRouter(t, o.start(t).URL, 1)

	_, err := r.Route(context.Background(), "a memo")
	if err == nil {
		t.Fatal("a full context window was accepted")
	}
	if !strings.Contains(err.Error(), "truncates from the front") {
		t.Fatalf("err = %v, want it to name the truncation", err)
	}
}

// A cut-off answer is unparseable JSON, and would otherwise be retried as if
// the model had written something malformed. It did not; it was interrupted.
func TestALengthCutoffIsNamedRatherThanReadAsMalformedJSON(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{{
		"response": `{"reason":"it goes on and`, "done_reason": "length",
		"prompt_eval_count": 100, "eval_count": 1536,
	}}}
	r := newTestRouter(t, o.start(t).URL, 3)

	_, err := r.Route(context.Background(), "a memo")
	if err == nil || !strings.Contains(err.Error(), "num_predict") {
		t.Fatalf("err = %v, want it to name num_predict", err)
	}
	if o.numCalls != 1 {
		t.Errorf("made %d calls; a cut-off answer is not something a retry fixes", o.numCalls)
	}
}

// The retry loop is scribe.Run's. What this package owes it is a prompt that
// CHANGED — /api/generate has no conversation, and at temperature 0 an
// unchanged prompt is the same answer, so a loop that re-asks identically
// burns attempts to no purpose.
func TestARetryReRendersThePromptWithTheValidationErrors(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{
		reply(`{"destination":"WHATEVER"}`),
		reply(validProposal("NOTE", "")),
	}}
	r := newTestRouter(t, o.start(t).URL, 3)

	out, err := r.Route(context.Background(), "a memo about a principle")
	if err != nil {
		t.Fatal(err)
	}
	if out.Proposal == nil || out.Proposal.Destination != scribe.DestNote {
		t.Fatalf("outcome = %+v, want the second attempt's NOTE", out)
	}
	if o.numCalls != 2 {
		t.Fatalf("made %d calls, want 2", o.numCalls)
	}
	if o.prompts[0] == o.prompts[1] {
		t.Fatal("the retry sent an identical prompt")
	}
	if !strings.Contains(o.prompts[1], "previous attempt was rejected") ||
		!strings.Contains(o.prompts[1], "destination") {
		t.Errorf("the retry did not carry the validation errors:\n%s", o.prompts[1])
	}
}

// A memo that never validates still produces an Outcome carrying its error —
// CHRN-32 §7, and the sharpest sentence in it is why: the operator will not
// notice which memo went missing.
func TestAMemoThatNeverValidatesStillProducesAnOutcome(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{reply(`{"destination":"NONSENSE"}`)}}
	r := newTestRouter(t, o.start(t).URL, 3)

	out, err := r.Route(context.Background(), "a memo")
	if err != nil {
		t.Fatalf("a contract failure is not a transport error: %v", err)
	}
	if out.Proposal != nil || out.Status != scribe.StatusInvalid || out.Err == nil {
		t.Fatalf("outcome = %+v, want invalid with an error", out)
	}
	if o.numCalls != 3 {
		t.Errorf("made %d calls, want the 3 attempts the contract allows", o.numCalls)
	}
}

// Stage 2 is never retried: the model answered as well as it could and the
// world moved. A catalogue miss must cost one completion, not three.
func TestACatalogueMissCostsOneCompletion(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{reply(validProposal("TICKET", "NOSUCH"))}}
	r := newTestRouter(t, o.start(t).URL, 3)

	out, err := r.Route(context.Background(), "a memo about work")
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != scribe.StatusNeedsInput {
		t.Fatalf("status = %q, want needs_input", out.Status)
	}
	if o.numCalls != 1 {
		t.Fatalf("made %d calls; a stage-2 clearing is not the model's fault", o.numCalls)
	}
	if len(out.Cleared) != 1 || out.Cleared[0].Field != "project_key" {
		t.Errorf("cleared = %+v, want the project key", out.Cleared)
	}
}

func TestTheProposerNamesRunnerModelAndPromptVersion(t *testing.T) {
	r := newTestRouter(t, "http://127.0.0.1:11434", 1)
	got := r.Proposer()
	if !strings.HasPrefix(got, "ollama/m@") {
		t.Fatalf("proposer = %q, want ollama/<model>@<promptversion>", got)
	}
	if strings.Count(got, "/") != 1 || strings.Count(got, "@") != 1 {
		t.Fatalf("proposer = %q is not the three-part form", got)
	}
}

// The tag is mutable; the digest is not. Re-pull `gemma4:31b` and the proposer
// string is unchanged while the weights are not.
func TestTheDigestIsReadOnceAndMemoised(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{reply("{}")}}
	r := newTestRouter(t, o.start(t).URL, 1)

	first, err := r.Digest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Digest(context.Background())
	if err != nil || first != second || first != "sha256:deadbeefcafe" {
		t.Fatalf("digest = %q / %q, err %v", first, second, err)
	}
}

func TestAModelThatIsNotPulledIsNamed(t *testing.T) {
	o := &ollama{t: t, replies: []map[string]any{reply("{}")}}
	r, err := New(Options{BaseURL: o.start(t).URL, Model: "absent", Catalogue: testCatalogue(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Digest(context.Background()); err == nil || !strings.Contains(err.Error(), "not pulled") {
		t.Fatalf("err = %v, want it to say the model is not pulled", err)
	}
}
