// Package amber resolves one estate citation against the capture archive.
//
// ============================================================================
// THE NAMESPACE THIS TICKET WAS WRITTEN AGAINST DOES NOT EXIST.
// ============================================================================
//
// CHRN-50 asks for `AMB-2291 · SEALED`. Measured over ~/projects/amber on
// 2026-09-09 and again here: `grep -rn 'AMB-'` returns nothing and
// `grep -rni sealed` returns nothing. Both came from the canvas, where they are
// illustrations, and both were read as identifiers. CHRN-48 took `AMB` out of
// the grammar and put in what Amber actually publishes:
//
//	amber1.<session-uuid>.<record-uuid>[.<block-seq>]        the address
//	held | not_captured | prompt_only | not_in_capture |     the state
//	block_out_of_range | ambiguous
//
// So `HELD` is the state that replaces the imagined `SEALED`, and the ticket
// got SMALLER: "Amber's read surface may need a small addition to support
// this" — it needs none. `GET /v1/cite/{ref}` has been live since AMBR-11 and
// already publishes an outcome vocabulary, an explanatory sentence and a
// `recoverable` flag, which is the broken-versus-unreachable distinction this
// epic would otherwise have had to invent.
//
// ============================================================================
// WHY THIS IS ITS OWN CLIENT AND NOT internal/switchyard's.
// ============================================================================
//
// AMBER ANSWERS ITS BROKEN OUTCOMES AS NON-2xx *WITH THE CITE BODY*.
// cite.Outcome.Status() maps held to 200, not_captured and block_out_of_range
// to 404, prompt_only and not_in_capture to 410, ambiguous to 409, and
// handleCite writes the whole citeResponse at that status.
//
// internal/switchyard's `do` discards the body on anything >= 300 and returns a
// bare *switchyard.Error. Reused here, all five broken outcomes would arrive as
// "the archive is in trouble" and the vocabulary would never be seen at all —
// the confident falsehood about somebody's evidence that CHRN-51's five states
// exist to prevent. Hence a transport that reads the body at every status and
// decides nothing.
//
// ============================================================================
// NOTHING FROM AMBER IS STORED, AND THIS PACKAGE COULD NOT STORE IT.
// ============================================================================
//
// Invariant 2: an Amber item resolves at render time and is never written into
// Chronicle's tables. Amber holds the durable archive as its source of truth,
// and a copy of an archive entry in a notes wiki is a second archive that will
// diverge. This package therefore has NO DATABASE DEPENDENCY AT ALL — not a
// store, not a pool, not pgx — and TestNothingHereCanReachTheStore walks the
// import graph and fails if that ever stops being true.
//
// The answer's bytes live as long as one classification. Chronicle reads an
// outcome, a sentence and a flag off them; the event text, the session
// evidence and the context Amber also sends are never decoded, never returned
// and never logged.
//
// ============================================================================
// THE CREDENTIAL IS AMBER'S OWN, AND THAT IS A KNOWN DEVIATION.
// ============================================================================
//
// Amber gates all of /v1 behind a single AMBER_API_TOKEN rather than minting
// one per consumer. Switchyard already presents it (SWY-209 / AMBR-9), and the
// compose comment there records the cost rather than hiding it: "a deliberate
// deviation from PRINCIPLES.md §4 (never share a token across components; mint
// per-consumer, scope minimally)" — the same token also reads GET /v1/prompts,
// so a consumer's compromise reads the archived prompt corpus.
//
// Chronicle is the THIRD consumer presenting it. That deviation is inherited by
// reference and not re-litigated here; narrowing it is Amber's to do, and there
// is no scoped credential to hand over today. Stated out loud rather than
// arrived at silently.
package amber

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
)

// citePath is the archive's citation endpoint. The reference is one path
// segment, which is a property AMBR-11 chose the separator for: a dot is
// "unreserved by RFC 3986, so a reference is safe verbatim in a path segment, a
// query parameter, a fragment and a markdown link target".
const citePath = "/v1/cite/"

// MaxBody bounds what one answer is allowed to cost.
//
// A cite answer carries the cited block's text, and a block can be a tool
// result of arbitrary size — Amber caps the CONTEXT window at fifty events and
// caps nothing about the event itself. Chronicle reads four fields off this
// body and would hold a multi-megabyte one in memory to do it, once per
// reference on the page.
//
// FOUR MEBIBYTES IS A CEILING, NOT A BUDGET: a JSON object around one block
// does not approach it, and anything that does is pathological rather than
// large. An answer over the cap is dropped rather than truncated into the
// classifier, which reads it as an answer Chronicle could not understand:
// unreachable for THAT REFERENCE, not a claim about the service, retried on the
// next render. That is the right shape — one enormous block says nothing about
// the next citation — and it is why the overflow is not reported as a transport
// failure, which would trip the breaker and silence the rest of the page.
const MaxBody = 4 << 20

// Client is Chronicle's read-only reach into the capture archive.
//
// It is a resolve.Transport and nothing else: one call, no retries, no
// classification, no clock. Everything about what an answer MEANS was settled
// by CHRN-51 in internal/resolve/classify.go, deliberately, so that the two
// transport tickets could not invent two mappings of it.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

// Assert the seam at compile time rather than at the composition root: this
// type exists to be registered as one, and a signature that drifts should fail
// here rather than wherever CHRN-97 builds the map.
var _ resolve.Transport = (*Client)(nil)

// New validates the configuration without calling anything.
//
// BOTH OR NEITHER, and the reason is Amber's own failure mode rather than
// symmetry with the other clients. Amber FAILS CLOSED: with no token set on its
// side every /v1 route answers 503, and with a token set it answers 401 to a
// request that carries none. So a Chronicle holding a URL and no token would
// dial, be refused, trip the breaker, and render every citation "the capture
// archive refused Chronicle's credential" — an outage report about a service
// that is perfectly healthy, which is exactly what resolve.StateUnconfigured
// exists to say instead. Unconfigured is a state; half-configured is a lie.
func New(baseURL, token string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("amber: CHRONICLE_AMBER_URL is not set")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("amber: CHRONICLE_AMBER_URL %q is not an absolute http(s) URL", baseURL)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("amber: CHRONICLE_AMBER_TOKEN is not set — every /v1 route answers 401 without it")
	}
	return &Client{
		base:  u,
		token: token,
		// NO CLIENT-SIDE TIMEOUT, on resolve.Transport's instruction: "ctx
		// already carries the per-call deadline; a Transport must not install
		// its own." Two deadlines means the shorter one wins invisibly, and
		// the resolver's is the one the page's budget is arithmetic over.
		http: &http.Client{},
	}, nil
}

// BaseURL is where this client points. The URL is loggable; the token is not,
// here or anywhere — a credential in a log line is a credential in every
// aggregator that scrapes them.
func (c *Client) BaseURL() string { return c.base.String() }

// Fetch makes exactly one call for one citation.
//
// It classifies nothing. A non-2xx is an ANSWER and is returned as one, with
// its body: that is the whole reason this transport exists.
func (c *Client) Fetch(ctx context.Context, ref markdown.Reference) resolve.Answer {
	// A WIRING GUARD, NOT A PARSER. The resolver keys transports by system, so
	// this cannot fire through Resolve — but a composition root that registered
	// this client under the wrong key would otherwise ask Amber to resolve
	// `SWY-389`, get a 400 for a reference it cannot parse, and render a ticket
	// as a malformed citation. A named error is a better failure than a card
	// that confidently says the wrong thing about somebody's work.
	if ref.System != markdown.SystemAmber {
		return resolve.Answer{Err: fmt.Errorf(
			"amber: %q is a %s reference, not an amber1 citation", ref.Token, ref.System)}
	}

	// PathEscape and not concatenation. Every token internal/markdown can
	// produce passes through it byte-identical — its component alphabet is
	// [A-Za-z0-9:_-] plus the dot separator, none of which is escaped — so this
	// costs nothing in the ordinary case. What it buys is that a token from any
	// other source can never leave its segment and select a different route:
	// `/` becomes %2F, which Amber unescapes back into the reference and
	// refuses as malformed, which is the truth about it.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base.String()+citePath+url.PathEscape(ref.Token), nil)
	if err != nil {
		return resolve.Answer{Err: fmt.Errorf("amber: %w", err)}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	// NO ?context= PARAMETER. Amber defaults to no context window, and
	// Chronicle wants the minimum an honest card needs: an outcome, a sentence
	// and a flag. Asking for surrounding transcript Chronicle will not render
	// would pull authored archive content across the network for nothing —
	// which is how a copy starts.
	resp, err := c.http.Do(req)
	if err != nil {
		// Wrapped, never replaced: internal/resolve reads this with errors.As
		// to tell a timeout ("did not answer in the time allowed") from a dial
		// failure ("could not be reached"), and the two have different
		// remedies. *url.Error carries Timeout() through %w.
		return resolve.Answer{Err: fmt.Errorf("amber: GET %s: %w", citePath, err)}
	}
	defer func() { _ = resp.Body.Close() }()

	// MaxBody+1, so that "exactly at the cap" and "over it" are distinguishable
	// rather than both reading as a full read.
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		// A connection that died mid-body IS a transport failure: there is no
		// usable response, whatever the status line promised.
		return resolve.Answer{Err: fmt.Errorf("amber: GET %s: read body: %w", citePath, err)}
	}
	if len(body) > MaxBody {
		// Dropped rather than truncated. Truncated JSON would decode as
		// nothing anyway, and carrying a partial copy of archive text into the
		// classifier to achieve the same outcome is worse for no gain. The
		// status is kept: it is a fact, and it is true.
		body = nil
	}
	return resolve.Answer{Status: resp.StatusCode, Body: body}
}

// URLFor is the outbound arrow's target, and for Amber there is none.
//
// MEASURED, NOT ASSUMED: Amber serves no HTML anywhere — no template, no
// embedded asset, no `text/html` response in the whole repository — and its
// only published port exists so Uptime Kuma can reach /readyz. Its /v1 surface
// is JSON behind a bearer token, so a link there lands a reader on a 401 in a
// browser they cannot authenticate in.
//
// Returning "" is CHRN-51's stated contract for exactly this case: "it returns
// ” when there is nowhere for a reader's browser to go — Amber's case, which
// serves no HTML at all". A card with no arrow is honest; an arrow onto a
// refusal is a worse link than none.
func (c *Client) URLFor(string) string { return "" }
