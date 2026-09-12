package amber

import (
	"context"
	"errors"
	"go/build"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
)

// ---------------------------------------------------------------------------
// Fixtures. The bodies and statuses below are Amber's own, taken from
// internal/cite/format.go and internal/api/api.go's citeResponse rather than
// invented: a transport tested against a fabricated upstream proves that the
// fabrication was consistent with itself.
// ---------------------------------------------------------------------------

// A real-shaped reference: two uuids and a block index.
const citation = "amber1.9d654a7c-1ef5-49c8-bbe5-076164725a9d.00e6ecbc-2d27-485d-bbb4-c96c5f24ab74.2"

func ref(token string) markdown.Reference {
	return markdown.Reference{System: markdown.SystemAmber, Token: token}
}

// heldBody is what a resolved citation actually comes back as, INCLUDING the
// archive content Chronicle must never keep. The marker string is what
// TestNoArchiveContentSurvivesTheCall looks for on the way out.
const archiveText = "SENTINEL-the-operator-typed-this-into-a-session"

func heldBody() string {
	return `{"ref":"` + citation + `","outcome":"held",` +
		`"explain":"the cited block is in the archive and is returned, redacted",` +
		`"recoverable":false,"vocabulary_version":"v1",` +
		`"event":{"kind":"user_prompt","text":"` + archiveText + `","session_id":"9d654a7c-1ef5-49c8-bbe5-076164725a9d"},` +
		`"session":{"project_slug":"chronicle","last_ingested_at":"2026-09-12T04:00:00Z"},` +
		`"objects_read":3}`
}

// recorder is what the fake Amber was asked; serve answers every request with
// one status and body.
type recorder struct {
	path   string // decoded, as a handler reads it
	raw    string // as it went over the wire, escaping intact
	method string
	auth   string
	query  string
	calls  int
}

func serve(t *testing.T, status int, body string) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.calls++
		rec.path = r.URL.Path
		rec.raw = r.URL.EscapedPath()
		rec.method = r.Method
		rec.auth = r.Header.Get("Authorization")
		rec.query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c, err := New(srv.URL, "the-shared-archive-token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, rec
}

func fetch(t *testing.T, c *Client, token string) resolve.Answer {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.Fetch(ctx, ref(token))
}

// ---------------------------------------------------------------------------

func TestFetchAsksAmberForTheCitationAndPresentsTheCredential(t *testing.T) {
	c, rec := serve(t, http.StatusOK, heldBody())
	ans := fetch(t, c, citation)

	if ans.Err != nil {
		t.Fatalf("Fetch: %v", ans.Err)
	}
	if rec.method != http.MethodGet {
		t.Errorf("method = %s, want GET", rec.method)
	}
	// The reference is one path segment, byte-identical to what was written.
	// AMBR-11 chose a dot separator precisely so no encoding step sits between
	// a note and a link, and a token that arrived re-spelled would resolve to
	// a different record or to nothing.
	if want := "/v1/cite/" + citation; rec.path != want {
		t.Errorf("path = %q, want %q", rec.path, want)
	}
	if rec.auth != "Bearer the-shared-archive-token" {
		t.Errorf("Authorization = %q", rec.auth)
	}
	// NO ?context=. Chronicle renders an outcome, a sentence and a flag, and
	// asking for surrounding transcript it will not render would pull archive
	// content across the network for nothing.
	if rec.query != "" {
		t.Errorf("query = %q, want none", rec.query)
	}
}

// THE CENTRAL GUARD OF THIS TICKET.
//
// Amber answers five of its six outcomes as non-2xx WITH the cite body.
// internal/switchyard's client discards the body on anything >= 300, so a
// Chronicle that reused it would see five broken outcomes as five transport
// failures and never read the vocabulary at all. This test fails the moment
// somebody "simplifies" this transport into that one.
func TestTheBodySurvivesEveryStatusAmberAnswersWith(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"held", http.StatusOK, `{"outcome":"held","recoverable":false}`},
		{"not_captured", http.StatusNotFound, `{"outcome":"not_captured","recoverable":false}`},
		{"block_out_of_range", http.StatusNotFound, `{"outcome":"block_out_of_range","recoverable":false}`},
		{"prompt_only", http.StatusGone, `{"outcome":"prompt_only","recoverable":false}`},
		{"not_in_capture", http.StatusGone, `{"outcome":"not_in_capture","recoverable":true}`},
		{"ambiguous", http.StatusConflict, `{"outcome":"ambiguous","recoverable":true}`},
		{"malformed reference", http.StatusBadRequest, `{"error":"malformed citation: \"x\" has 1 components"}`},
		{"refused credential", http.StatusUnauthorized, `{"error":"missing or invalid bearer token"}`},
		{"no archive open", http.StatusServiceUnavailable, `{"error":"citation resolution unavailable: no archive is open"}`},
		{"internal", http.StatusInternalServerError, `{"error":"cannot resolve citation"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := serve(t, tc.status, tc.body)
			ans := fetch(t, c, citation)

			if ans.Err != nil {
				t.Fatalf("a %d is an ANSWER, not a transport failure: %v", tc.status, ans.Err)
			}
			if ans.Status != tc.status {
				t.Errorf("status = %d, want %d", ans.Status, tc.status)
			}
			if string(ans.Body) != tc.body {
				t.Errorf("body = %q, want it carried through verbatim", ans.Body)
			}
		})
	}
}

// An answer over the cap is dropped, and the drop is a fact about ONE citation.
// It must not look like a transport failure: that trips the resolver's breaker
// and stops the rest of the page from being checked at all, on the strength of
// one enormous block.
func TestAnOversizeAnswerIsDroppedRatherThanTruncated(t *testing.T) {
	big := `{"outcome":"held","event":{"text":"` + strings.Repeat("x", MaxBody) + `"}}`
	c, _ := serve(t, http.StatusOK, big)
	ans := fetch(t, c, citation)

	if ans.Err != nil {
		t.Fatalf("an oversize body is not a transport failure: %v", ans.Err)
	}
	if ans.Status != http.StatusOK {
		t.Errorf("status = %d, want the status kept: it is a fact and it is true", ans.Status)
	}
	if len(ans.Body) != 0 {
		t.Fatalf("body kept %d bytes; an unreadable answer is dropped, not truncated", len(ans.Body))
	}
	// And it reaches a reader as a per-reference unreachable rather than as a
	// claim about the archive.
	res := resolveOne(t, c, citation)
	if res.State != resolve.StateUnreachable {
		t.Errorf("state = %q, want unreachable", res.State)
	}
	// Pinned rather than merely non-empty, because this is the sentence the
	// cap actually produces and it is the one documented on MaxBody. If a
	// classifier change ever gives the drop its own words, this is where that
	// shows up rather than in somebody's surprise.
	if want := "the capture archive returned a non-JSON response"; res.Explain != want {
		t.Errorf("explain = %q, want %q", res.Explain, want)
	}
}

func TestAnAnswerExactlyAtTheCapIsStillRead(t *testing.T) {
	// The boundary matters because the read asks for MaxBody+1 to tell "at the
	// cap" from "over it"; an off-by-one here silently drops a legal answer.
	pad := `{"outcome":"held","event":{"text":"`
	body := pad + strings.Repeat("x", MaxBody-len(pad)-len(`"}}`)) + `"}}`
	if len(body) != MaxBody {
		t.Fatalf("fixture is %d bytes, want exactly %d", len(body), MaxBody)
	}
	c, _ := serve(t, http.StatusOK, body)
	ans := fetch(t, c, citation)
	if len(ans.Body) != MaxBody {
		t.Fatalf("body = %d bytes, want the whole %d", len(ans.Body), MaxBody)
	}
}

func TestATransportFailureIsReportedAsOneAndKeepsItsKind(t *testing.T) {
	t.Run("nothing listening", func(t *testing.T) {
		// A port nothing is on: the archive is down, or the name does not
		// resolve. There is no answer at all, which is the one thing no
		// upstream can ever report about itself.
		c, err := New("http://127.0.0.1:1", "a-token")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ans := fetch(t, c, citation)
		if ans.Err == nil {
			t.Fatal("want a transport failure")
		}
		if ans.Status != 0 || ans.Body != nil {
			t.Errorf("status/body set on a call that never answered: %d %q", ans.Status, ans.Body)
		}
		if strings.Contains(ans.Err.Error(), "a-token") {
			t.Fatal("the credential is in the error, and therefore in every log that scrapes it")
		}
	})

	t.Run("a timeout is still recognisable as one", func(t *testing.T) {
		// internal/resolve reads this with errors.As to tell "did not answer in
		// the time allowed" from "could not be reached" — two sentences with
		// different remedies. Wrapping must not flatten that.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer srv.Close()
		c, err := New(srv.URL, "a-token")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		ans := c.Fetch(ctx, ref(citation))
		if ans.Err == nil {
			t.Fatal("want a transport failure")
		}
		var ne net.Error
		if !errors.As(ans.Err, &ne) || !ne.Timeout() {
			t.Fatalf("error = %v; it no longer reads as a timeout through errors.As", ans.Err)
		}
	})
}

// The resolver keys transports by system, so this cannot fire through Resolve.
// It fires when a composition root registers this client under the wrong key —
// and the alternative to refusing is asking Amber to resolve `SWY-389`, getting
// a 400 for a reference it cannot parse, and rendering a live ticket as a
// malformed citation.
func TestAReferenceThatIsNotACitationIsRefusedWithoutDialling(t *testing.T) {
	c, rec := serve(t, http.StatusOK, heldBody())
	ans := c.Fetch(context.Background(), markdown.Reference{
		System: markdown.SystemSwitchyard, Key: "SWY", Token: "SWY-389", Number: 389,
	})
	if ans.Err == nil {
		t.Fatal("a ticket reference was accepted as a citation")
	}
	if rec.calls != 0 {
		t.Errorf("dialled %d times for a reference that is not Amber's", rec.calls)
	}
}

// Defence in depth over internal/markdown's alphabet, which already excludes
// `/`. What matters is that a token can never select a different route: it is
// one segment or it is refused by Amber as malformed, and never a request for
// something else.
func TestATokenCannotEscapeItsPathSegment(t *testing.T) {
	c, rec := serve(t, http.StatusBadRequest, `{"error":"malformed citation"}`)
	fetch(t, c, "amber1.a/../../v1/prompts.b")

	// The WIRE form is what selects a route. Escaped, the whole token is one
	// segment; Amber unescapes it back into the reference, cite.Parse refuses
	// it, and the card says malformed — which is the truth about it.
	if strings.Count(strings.TrimPrefix(rec.raw, "/v1/cite/"), "/") != 0 {
		t.Fatalf("escaped path = %q: the token spans more than one segment", rec.raw)
	}
	if !strings.HasPrefix(rec.raw, "/v1/cite/") {
		t.Fatalf("escaped path = %q, want it under /v1/cite/", rec.raw)
	}
}

func TestNewRefusesAHalfConfiguredClient(t *testing.T) {
	for _, tc := range []struct{ name, url, token, want string }{
		{"no url", "", "a-token", "CHRONICLE_AMBER_URL"},
		{"no token", "http://amber:4008", "", "CHRONICLE_AMBER_TOKEN"},
		{"not a url", "amber:4008", "a-token", "CHRONICLE_AMBER_URL"},
		{"wrong scheme", "ftp://amber:4008", "a-token", "CHRONICLE_AMBER_URL"},

		// A base carrying either of these parses, boots, reports itself
		// configured — and then asks Amber about `/` for every citation,
		// because concatenating a path onto it leaves the path EMPTY:
		//
		//	http://amber:4008#frag  +  /v1/cite/<ref>
		//	  -> path="" fragment="frag/v1/cite/<ref>"
		//
		// Which is the configured-and-unusable shape the parse exists to
		// prevent, reached through the one malformation it was not checking.
		{"a query string", "http://amber:4008?x=1", "a-token", "query"},
		{"a forced query", "http://amber:4008?", "a-token", "query"},
		{"a fragment", "http://amber:4008#frag", "a-token", "fragment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.url, tc.token)
			if err == nil {
				t.Fatal("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to name %s", err, tc.want)
			}
		})
	}

	// A PATH IS NOT A MALFORMATION. Amber behind a reverse proxy under a prefix
	// concatenates correctly, and refusing it would reject a real deployment.
	t.Run("a path prefix is allowed and reaches the right endpoint", func(t *testing.T) {
		c, rec := serve(t, http.StatusOK, heldBody())
		prefixed, err := New(c.BaseURL()+"/archive", "a-token")
		if err != nil {
			t.Fatalf("New with a path prefix: %v", err)
		}
		fetch(t, prefixed, citation)
		if want := "/archive/v1/cite/" + citation; rec.path != want {
			t.Fatalf("path = %q, want %q", rec.path, want)
		}
	})

	t.Run("a trailing slash is trimmed", func(t *testing.T) {
		c, err := New("http://amber:4008/", "a-token")
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if c.BaseURL() != "http://amber:4008" {
			t.Fatalf("BaseURL = %q", c.BaseURL())
		}
	})
}

// Measured rather than assumed, and re-measurable: Amber serves no HTML
// anywhere, so there is nowhere for a reader's browser to go. An arrow onto a
// bearer-token 401 is a worse link than no arrow.
func TestURLForIsEmptyBecauseAmberServesNoHTML(t *testing.T) {
	c, _ := serve(t, http.StatusOK, heldBody())
	if got := c.URLFor(citation); got != "" {
		t.Fatalf("URLFor = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// "No Amber content is stored in Chronicle's tables" — the ticket's third
// clause, held as a property of the import graph rather than as a promise in a
// comment. This package cannot write archive content to a table because it
// cannot reach a table.
// ---------------------------------------------------------------------------

func TestNothingHereCanReachTheStore(t *testing.T) {
	const module = "github.com/Einlanzerous/chronicle/"
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("module root: %v", err)
	}

	// Anything that could put a row in a table, by any route.
	forbidden := []string{module + "internal/store", "jackc/pgx", "database/sql"}

	seen := map[string]bool{}
	var walk func(pkg, dir string)
	walk = func(pkg, dir string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true

		// Non-test imports only: a test may reach for anything, and it is the
		// shipped package whose reach is the claim.
		p, err := build.ImportDir(dir, 0)
		if err != nil {
			t.Fatalf("reading %s: %v", pkg, err)
		}
		for _, imp := range p.Imports {
			for _, bad := range forbidden {
				if strings.Contains(imp, bad) {
					t.Errorf("%s imports %s: internal/amber must not be able to reach a table", pkg, imp)
				}
			}
			if strings.HasPrefix(imp, module) {
				walk(imp, filepath.Join(root, strings.TrimPrefix(imp, module)))
			}
		}
	}
	walk(module+"internal/amber", ".")

	// A walk that visited only this package would pass vacuously.
	if len(seen) < 3 {
		t.Fatalf("walked %d packages (%v); the import graph was not actually followed", len(seen), keys(seen))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// resolveOne runs one citation through the real resolver, which is how the
// transport is exercised the way the render path will use it.
func resolveOne(t *testing.T, c *Client, token string) resolve.Resolution {
	t.Helper()
	r, err := resolve.New(resolve.Options{
		Transports: map[string]resolve.Transport{markdown.SystemAmber: c},
	})
	if err != nil {
		t.Fatalf("resolve.New: %v", err)
	}
	out := r.Resolve(context.Background(), []markdown.Reference{ref(token)})
	if len(out) != 1 {
		t.Fatalf("got %d resolutions for one reference", len(out))
	}
	return out[0]
}
