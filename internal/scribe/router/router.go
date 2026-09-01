// Package router is CHRN-30's half of Scribe: the thing that actually asks the
// local model, wrapped around the contract in internal/scribe and the prompt in
// internal/scribe/prompt.
//
// It knows nothing about the eval harness. `eval.Router` wants
// `Route(ctx, Item)`; this offers `Route(ctx, transcript)` and the composition
// root adapts between them, so a production caller (CHRN-33's batch path) does
// not inherit a dependency on the thing that grades it.
//
// The plan is CHRN-30's, approved 2026-08-31.
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/prompt"
)

// DefaultTimeout is the per-call budget.
//
// The salvage used 120 s and it is kept. Cold-loading a 19.9 GB Q4_K_M model
// and then generating a long description is comfortably past 30 s, and a
// timeout that fires on a cold start would make the first memo of every run
// fail for a reason that has nothing to do with the prompt.
const DefaultTimeout = 120 * time.Second

// Catalogue is what the router routes against, and it is READ TWICE: rendered
// into the prompt, then handed to scribe.Reconcile as the thing the answer is
// validated against. One snapshot, two readers — the salvage's pattern, kept
// because re-fetching between the two opens a window where a project is in the
// prompt and gone by the time the answer is checked.
type Catalogue interface {
	scribe.Catalogue
	RenderProjects() string
	RenderPages() string
}

// Options constructs a Router.
type Options struct {
	// BaseURL is CHRONICLE_SCRIBE_OLLAMA_URL. The router will reach this host
	// and no other, enforced by its own transport.
	BaseURL string
	// Model is CHRONICLE_SCRIBE_MODEL, e.g. `gemma4:31b`.
	Model     string
	Catalogue Catalogue
	// MaxAttempts is CHRN-32 §7's ceiling. Zero means the contract's default.
	MaxAttempts int
	Timeout     time.Duration
}

// Router asks one model, over one host, with one pinned request shape.
type Router struct {
	base        *url.URL
	model       string
	cat         Catalogue
	maxAttempts int
	http        *http.Client
	req         prompt.Request
	numCtx      int
	proposer    string

	digestOnce sync.Once
	digest     string
	digestErr  error
}

// New validates everything that can be validated without a network.
func New(o Options) (*Router, error) {
	if strings.TrimSpace(o.BaseURL) == "" {
		return nil, fmt.Errorf("router: CHRONICLE_SCRIBE_OLLAMA_URL is not set, so there is no model to ask")
	}
	u, err := url.Parse(strings.TrimRight(o.BaseURL, "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("router: CHRONICLE_SCRIBE_OLLAMA_URL %q is not an absolute http(s) URL", o.BaseURL)
	}
	if strings.TrimSpace(o.Model) == "" {
		return nil, fmt.Errorf("router: CHRONICLE_SCRIBE_MODEL is not set")
	}
	if o.Catalogue == nil {
		return nil, fmt.Errorf("router: no catalogue — the prompt cannot render a project list, so every TICKET would answer with an empty project_key")
	}

	// The proposer is built HERE, at construction, so a bad model name is a
	// startup error rather than something discovered on the twentieth memo.
	proposer, err := scribe.Proposer("ollama", o.Model, prompt.Version)
	if err != nil {
		return nil, fmt.Errorf("router: %w", err)
	}
	req, err := prompt.Options()
	if err != nil {
		return nil, err
	}
	numCtx, err := prompt.NumCtx()
	if err != nil {
		return nil, err
	}
	attempts := o.MaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Router{
		base: u, model: o.Model, cat: o.Catalogue, maxAttempts: attempts,
		http: pinnedClient(u, timeout), req: req, numCtx: numCtx, proposer: proposer,
	}, nil
}

// pinnedClient refuses to dial anything but the configured host.
//
// THE POINT IS THAT IT HOLDS IN THE SHIPPED BINARY, not only in a diff someone
// read. "Runs entirely on the local box" is one of this ticket's three
// Done-when clauses, and a comment saying so is not a mechanism: a future
// caller who passes a different URL, or a redirect to somewhere else, meets a
// refusal rather than a reviewer's memory.
func pinnedClient(u *url.URL, timeout time.Duration) *http.Client {
	want := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" {
			want = u.Hostname() + ":443"
		} else {
			want = u.Hostname() + ":80"
		}
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if addr != want {
					return nil, fmt.Errorf("router: refusing to dial %s — this client may reach %s and nothing else", addr, want)
				}
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

// Proposer is the row's identity: runner, model and prompt version.
func (r *Router) Proposer() string { return r.proposer }

// Model is the configured model tag. The tag is not the weights — see Digest.
func (r *Router) Model() string { return r.model }

// Digest is the model's content hash, read once per run and memoised.
//
// A TAG IS NOT THE WEIGHTS. `gemma4:31b` is mutable; re-pull it and the
// proposer string is unchanged while the model is not, which is exactly the
// prompt-versus-model confusion the proposer's third part exists to prevent.
// It is reported beside the proposer rather than inside it, because
// scribe.Proposer rightly refuses a part carrying extra separators.
//
// Read from /api/tags rather than /api/show: the plan said /api/show, and the
// digest is not in that response.
func (r *Router) Digest(ctx context.Context) (string, error) {
	r.digestOnce.Do(func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.base.String()+"/api/tags", nil)
		if err != nil {
			r.digestErr = err
			return
		}
		resp, err := r.http.Do(req)
		if err != nil {
			r.digestErr = fmt.Errorf("router: /api/tags: %w", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		var body struct {
			Models []struct {
				Name   string `json:"name"`
				Digest string `json:"digest"`
			} `json:"models"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			r.digestErr = fmt.Errorf("router: /api/tags: %w", err)
			return
		}
		for _, m := range body.Models {
			if m.Name == r.model {
				r.digest = m.Digest
				return
			}
		}
		r.digestErr = fmt.Errorf("router: %s is not pulled on this Ollama", r.model)
	})
	return r.digest, r.digestErr
}

// Route runs one transcript through the contract.
//
// The retry loop is scribe.Run's, not this package's: the router supplies
// Generate and nothing else. Said aloud because reimplementing the loop one
// layer down is the obvious mistake, and it would lose §7's guarantee that a
// memo always produces an Outcome.
//
// The returned error is a TRANSPORT failure — Ollama unreachable, a truncated
// context, a response that could not be decoded. It is distinct from
// Outcome.Err, which means no attempt produced a schema-valid proposal and is
// a fact about the prompt rather than about the box.
func (r *Router) Route(ctx context.Context, transcript string) (scribe.Outcome, error) {
	var transportErr error
	gen := func(ctx context.Context, attempt int, feedback string) ([]byte, error) {
		p := prompt.Fill(r.cat.RenderProjects(), r.cat.RenderPages(), transcript, feedback)
		raw, err := r.generate(ctx, p)
		if err != nil {
			transportErr = err
		}
		return raw, err
	}
	out := scribe.Run(ctx, gen, r.cat, r.maxAttempts)
	return out, transportErr
}

type generateRequest struct {
	Model   string          `json:"model"`
	Prompt  string          `json:"prompt"`
	Stream  bool            `json:"stream"`
	Format  json.RawMessage `json:"format"`
	Think   bool            `json:"think"`
	Options map[string]any  `json:"options"`
}

type generateResponse struct {
	Response        string `json:"response"`
	DoneReason      string `json:"done_reason"`
	PromptEvalCount int    `json:"prompt_eval_count"`
	EvalCount       int    `json:"eval_count"`
}

// generate is one completion.
func (r *Router) generate(ctx context.Context, p string) ([]byte, error) {
	body, err := json.Marshal(generateRequest{
		Model: r.model, Prompt: p, Stream: false,
		Format: prompt.Schema(), Think: r.req.Think, Options: r.req.Options,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base.String()+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("router: generate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("router: generate: %s", resp.Status)
	}
	var out generateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("router: generate: decode: %w", err)
	}

	// TRUNCATION IS AN ERROR, NOT A QUIETLY WORSE ROUTER.
	//
	// When prompt tokens exceed num_ctx, Ollama truncates FROM THE FRONT —
	// dropping these instructions and keeping the transcript — and says
	// nothing about it. The router would then be a summariser scoring badly
	// for a reason no report could show. num_ctx is pinned in options.json and
	// this is the assertion that it was actually big enough.
	if out.PromptEvalCount >= r.numCtx {
		return nil, fmt.Errorf("router: the prompt filled the context window (%d of num_ctx %d): "+
			"Ollama truncates from the front, so the instructions were dropped and only the "+
			"transcript survived — raise num_ctx in options.json (and bump the prompt version)",
			out.PromptEvalCount, r.numCtx)
	}
	// The other end: num_predict cut the answer off mid-object, which reaches
	// stage 1 as unparseable JSON and would be retried as if the model had
	// written something malformed. It did not; it was interrupted.
	if out.DoneReason == "length" {
		return []byte(out.Response), fmt.Errorf("router: the answer hit num_predict (%d tokens generated) and was cut off mid-object; "+
			"raise num_predict in options.json (and bump the prompt version)", out.EvalCount)
	}
	return []byte(out.Response), nil
}
