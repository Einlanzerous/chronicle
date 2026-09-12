package switchyard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// resolver starts a Switchyard that records what FetchTicket asked for and
// answers whatever the test wants.
type resolver struct {
	// mu guards what the handler goroutine records. The delayed case answers
	// after its caller's deadline has already fired, so the two really can be
	// running at once.
	mu     sync.Mutex
	paths  []string
	auth   []string
	status int
	body   string
	delay  time.Duration
}

func (rv *resolver) asked() []string {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	return append([]string(nil), rv.paths...)
}

func (rv *resolver) credentials() []string {
	rv.mu.Lock()
	defer rv.mu.Unlock()
	return append([]string(nil), rv.auth...)
}

func (rv *resolver) start(t *testing.T) *Client {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rv.mu.Lock()
		rv.paths = append(rv.paths, r.URL.EscapedPath())
		rv.auth = append(rv.auth, r.Header.Get("Authorization"))
		delay, st, body := rv.delay, rv.status, rv.body
		rv.mu.Unlock()

		if delay > 0 {
			time.Sleep(delay)
		}
		if st == 0 {
			st = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(st)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)
	c, err := New(s.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestAFetchedTicketComesBackAsStatusAndBytes is the whole contract: the answer
// is not interpreted on the way out.
func TestAFetchedTicketComesBackAsStatusAndBytes(t *testing.T) {
	rv := &resolver{body: `{"key":"SWY-389","title":"Plan rulings","status":{"category":"in_progress","display_name":"In Progress"}}`}
	c := rv.start(t)

	status, body, err := c.FetchTicket(context.Background(), "SWY-389")
	if err != nil {
		t.Fatalf("a ticket that exists is not an error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if string(body) != rv.body {
		t.Fatalf("the body was not carried verbatim:\n got %s\nwant %s", body, rv.body)
	}
	if got := rv.asked()[0]; got != "/v1/tickets/SWY-389" {
		t.Fatalf("path = %q, want /v1/tickets/SWY-389", got)
	}
	if got := rv.credentials()[0]; got != "Bearer tok" {
		t.Fatalf("the credential was not sent: %q", got)
	}
}

// TestAMissingTicketIsAnAnswerAndNotAnError is the difference from do(), and it
// is the one this ticket's Done-when rests on: a deleted ticket must arrive as
// a fact about the referent. Switchyard soft-deletes and resolveTicket excludes
// deleted rows, so a deletion IS this 404.
func TestAMissingTicketIsAnAnswerAndNotAnError(t *testing.T) {
	rv := &resolver{status: http.StatusNotFound, body: `{"error":"ticket not found"}`}
	c := rv.start(t)

	status, body, err := c.FetchTicket(context.Background(), "SWY-9999")
	if err != nil {
		t.Fatalf("a 404 must not be an error here — it is the answer: %v", err)
	}
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
	if !strings.Contains(string(body), "ticket not found") {
		t.Fatalf("the body was thrown away: %q", body)
	}
	// The contrast, stated rather than assumed: the same status through do() is
	// an *Error with no body at all, which is right for a create and wrong here.
	var t2 Ticket
	if err := c.do(context.Background(), "GET", "/v1/tickets/SWY-9999", nil, nil, &t2); err == nil {
		t.Fatal("do() is supposed to turn a 404 into an error; if it no longer does, FetchTicket has lost its reason to exist")
	}
}

// TestARefusedCredentialIsAlsoAnAnswer — 401 and 403 are statuses the caller
// must be able to see, because they have a remedy no generic outage has.
func TestARefusedCredentialIsAlsoAnAnswer(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		rv := &resolver{status: code, body: `{"error":"nope"}`}
		c := rv.start(t)
		status, _, err := c.FetchTicket(context.Background(), "SWY-1")
		if err != nil {
			t.Fatalf("%d: must not be an error: %v", code, err)
		}
		if status != code {
			t.Fatalf("status = %d, want %d", status, code)
		}
	}
}

// TestAnUnreachableTrackerIsATransportError — nobody answered, so the caller
// gets an error and not a status of zero dressed up as one.
func TestAnUnreachableTrackerIsATransportError(t *testing.T) {
	// A port nothing is listening on: the dial is refused before any status
	// exists, which is the case no upstream can report about itself.
	dead, err := New("http://127.0.0.1:1", "tok")
	if err != nil {
		t.Fatal(err)
	}

	status, body, err := dead.FetchTicket(context.Background(), "SWY-1")
	if err == nil {
		t.Fatal("a refused dial must be an error")
	}
	if status != 0 || body != nil {
		t.Fatalf("nothing answered, so there is no status and no body: %d %q", status, body)
	}
	if strings.Contains(err.Error(), "tok") {
		t.Fatalf("the credential is in the error: %v", err)
	}
}

// TestATimeoutSurvivesAsANetError is what lets internal/resolve say "did not
// answer in the time allowed" rather than "could not be reached". The two have
// different remedies and only the wrapping keeps them apart.
func TestATimeoutSurvivesAsANetError(t *testing.T) {
	rv := &resolver{delay: 200 * time.Millisecond, body: `{"key":"SWY-1"}`}
	c := rv.start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _, err := c.FetchTicket(ctx, "SWY-1")
	if err == nil {
		t.Fatal("the deadline was supposed to fire")
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("a timeout must arrive as a net.Error whose Timeout() is true, got %#v", err)
	}
}

// TestTheRendersDeadlineBeatsTheClientsOwn — the client's 15s is a triage
// batch's patience, and a render must not inherit it.
func TestTheRendersDeadlineBeatsTheClientsOwn(t *testing.T) {
	rv := &resolver{delay: 150 * time.Millisecond, body: `{"key":"SWY-1"}`}
	c := rv.start(t)
	if c.http.Timeout != DefaultTimeout {
		t.Fatalf("the client's own timeout changed: %v", c.http.Timeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, _, err := c.FetchTicket(ctx, "SWY-1"); err == nil {
		t.Fatal("want a deadline error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waited %v — the caller's deadline was ignored", elapsed)
	}
}

// TestAnOversizedAnswerIsDroppedRatherThanTruncated. The full ticket detail
// carries every comment, so the ceiling is real; what matters is that going
// over it produces neither a fabricated outage nor a half-body passed off as
// the answer.
func TestAnOversizedAnswerIsDroppedRatherThanTruncated(t *testing.T) {
	rv := &resolver{body: `{"key":"SWY-1","title":"` + strings.Repeat("x", maxTicketBody) + `"}`}
	c := rv.start(t)

	status, body, err := c.FetchTicket(context.Background(), "SWY-1")
	if err != nil {
		t.Fatalf("Switchyard answered, so this is not a transport failure: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want the status it actually answered", status)
	}
	if body != nil {
		t.Fatalf("an unusable body must be dropped, not truncated and passed on (%d bytes)", len(body))
	}
}

// TestAnAnswerJustUnderTheCeilingIsKept guards the boundary from the other
// side: the limit must not eat a large but legitimate thread.
func TestAnAnswerJustUnderTheCeilingIsKept(t *testing.T) {
	pad := maxTicketBody - len(`{"key":"SWY-1","title":""}`)
	rv := &resolver{body: `{"key":"SWY-1","title":"` + strings.Repeat("x", pad) + `"}`}
	c := rv.start(t)

	_, body, err := c.FetchTicket(context.Background(), "SWY-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != maxTicketBody {
		t.Fatalf("a body of exactly the ceiling was not kept: %d", len(body))
	}
}

// TestAKeyIsEscapedIntoThePath — the token came out of a note body, and the
// safety must not rest on today's grammar.
func TestAKeyIsEscapedIntoThePath(t *testing.T) {
	rv := &resolver{body: `{}`}
	c := rv.start(t)

	if _, _, err := c.FetchTicket(context.Background(), "SWY-1/../../v1/projects"); err != nil {
		t.Fatal(err)
	}
	got := rv.asked()[0]
	if strings.Contains(got, "..") && !strings.Contains(got, "%2F") {
		t.Fatalf("the key escaped its path segment: %q", got)
	}
	if !strings.HasPrefix(got, "/v1/tickets/") {
		t.Fatalf("path = %q, want it to stay under /v1/tickets/", got)
	}
}

// TestAnEmptyKeyIsRefusedBeforeDialling — GET /v1/tickets/ is the list route,
// and asking it for one ticket would answer 200 with a page of them.
func TestAnEmptyKeyIsRefusedBeforeDialling(t *testing.T) {
	rv := &resolver{body: `{}`}
	c := rv.start(t)

	if _, _, err := c.FetchTicket(context.Background(), "  "); err == nil {
		t.Fatal("an empty key must be refused")
	}
	if asked := rv.asked(); len(asked) != 0 {
		t.Fatalf("it dialled anyway: %v", asked)
	}
}
