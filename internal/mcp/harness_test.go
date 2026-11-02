package mcp_test

// The harness the read tools are proven read-only with (CHRN-66).
//
// A tool's Mutates flag says what its author DECLARED, and readOnlyHint is
// derived from it. This records what a tool DID: every request a session's API
// client makes passes through a RoundTripper that logs it and refuses any
// method but GET, so a read tool that quietly POSTs fails here whatever it
// declared. Tools added later drive themselves through session() and end with
// rec.requireGETOnly(t).

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Einlanzerous/chronicle/internal/mcp"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// errNotGET is what the recorder answers a request that is not a GET.
var errNotGET = errors.New("recorder: a read tool made a request that is not a GET")

// recorder is an http.RoundTripper that logs each request and refuses any
// method but GET. It never reaches the network for a refused request.
type recorder struct {
	next http.RoundTripper

	mu         sync.Mutex
	requests   []string // "METHOD /path?query", in order
	violations []string // the subset that was not a GET
}

func newRecorder() *recorder { return &recorder{next: http.DefaultTransport} }

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	line := req.Method + " " + req.URL.RequestURI()
	r.mu.Lock()
	r.requests = append(r.requests, line)
	if req.Method != http.MethodGet {
		r.violations = append(r.violations, line)
		r.mu.Unlock()
		return nil, errNotGET
	}
	r.mu.Unlock()
	return r.next.RoundTrip(req)
}

// paths returns the recorded requests as "METHOD /path", query dropped, so an
// assertion can name a route without spelling out its parameters.
func (r *recorder) paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.requests))
	for i, line := range r.requests {
		out[i], _, _ = strings.Cut(line, "?")
	}
	return out
}

// reset forgets what has been recorded, so a test can look at one call alone.
func (r *recorder) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests, r.violations = nil, nil
}

// requireGETOnly fails the test if any request since the last reset was not a GET.
func (r *recorder) requireGETOnly(t *testing.T) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.violations) > 0 {
		t.Fatalf("a read tool made requests that are not GETs: %v", r.violations)
	}
}

// session starts an MCP session for user on the given transport label, whose
// API client goes through the returned recorder. It builds the server with
// mcp.BuildServer, the one place a tool is attached, so what it is offered is
// what a real session of that identity is offered; only the HTTP client
// differs. The hosted transport's own exchange and revoke are POST and DELETE
// by design and are not tool calls, so a hosted session is represented by its
// Identity over a session token and not by running the exchange.
func (w *world) session(t *testing.T, user store.User, transport mcp.Transport) (*sdk.ClientSession, *recorder) {
	t.Helper()
	rec := newRecorder()
	api, err := mcp.NewAPI(w.apiURL, w.sessionFor(t, user, "mcp-harness"),
		&http.Client{Transport: rec, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("NewAPI: %v", err)
	}
	sess := mcp.Session{
		Identity: mcp.Identity{Transport: transport, Kind: string(user.Kind), UserID: user.ID, Name: user.DisplayName},
		API:      api,
	}
	server := mcp.BuildServer(mcp.Build{Version: "test"}, sess, nil)

	ctx, cancel := context.WithCancel(context.Background())
	serverSide, clientSide := sdk.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, serverSide) }()
	t.Cleanup(func() { cancel(); <-done })

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, rec
}

// The harness itself, with the only tool there is: whoami, driven as each kind
// of session, makes exactly one GET and nothing else.
func TestHarnessRecordsWhatAToolDid(t *testing.T) {
	w := newWorld(t)
	bot, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for _, tc := range []struct {
		name      string
		user      store.User
		transport mcp.Transport
	}{
		{"a hosted person", w.owner, mcp.TransportHosted},
		{"a stdio person", w.owner, mcp.TransportStdio},
		{"a stdio agent", bot, mcp.TransportStdio},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, rec := w.session(t, tc.user, tc.transport)
			rec.reset()
			if who := callWhoAmI(t, cs); who.Email != tc.user.Email {
				t.Fatalf("whoami = %+v", who)
			}
			rec.requireGETOnly(t)
			if got := rec.paths(); len(got) != 1 || got[0] != "GET /auth/me" {
				t.Errorf("recorded %v, want exactly GET /auth/me", got)
			}
		})
	}
}

// The failure path, which is the point of the harness: a request that is not a
// GET is refused before it reaches the API, and recorded as a violation.
func TestHarnessRefusesAnythingButGET(t *testing.T) {
	rec := newRecorder()
	reached := false
	rec.next = roundTripFunc(func(*http.Request) (*http.Response, error) {
		reached = true
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	client := &http.Client{Transport: rec}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req, _ := http.NewRequest(method, "http://api.test/memos", nil)
		if _, err := client.Do(req); !errors.Is(err, errNotGET) {
			t.Errorf("%s: err = %v, want errNotGET", method, err)
		}
	}
	if reached {
		t.Error("a refused request reached the API")
	}
	if len(rec.violations) != 4 {
		t.Errorf("violations = %v, want all four recorded", rec.violations)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://api.test/notes/search?q=x", nil)
	if _, err := client.Do(req); err != nil {
		t.Errorf("GET: %v", err)
	}
	if got := rec.paths(); got[len(got)-1] != "GET /notes/search" {
		t.Errorf("paths = %v, want the query dropped", got)
	}
}

// The world serves the three families the read tools will call, so a later
// tool's integration test has a real handler behind it and not a 503: pages,
// the threads under a page, and the triage batch. None is empty-checked here;
// the point is that none answers "not configured".
func TestWorldServesTheStoresTheReadToolsReach(t *testing.T) {
	w := newWorld(t)
	token := w.sessionFor(t, w.owner, "mcp-harness")
	for _, path := range []string{"/pages", "/notes/search?q=anything", "/triage/batch", "/discussions?page=nowhere"} {
		req, _ := http.NewRequest(http.MethodGet, w.apiURL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusUnauthorized {
			t.Errorf("GET %s = %d: the world is not wired for it", path, resp.StatusCode)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The dependencies triage needs to be constructed and nothing more. Chronicle's
// triage is wired from a Scribe, a tier-1 store and a ticket client; a read of
// the batch uses none of them beyond the proposal lookup, which here finds no
// proposal for any memo (status `absent`). A later test that needs a proposal
// replaces noProposals; this ticket does not.
type noProposals struct{}

func (noProposals) ProposalsForMemos(context.Context, []uuid.UUID, string) (map[uuid.UUID]store.Proposal, error) {
	return map[uuid.UUID]store.Proposal{}, nil
}

func (noProposals) GetProposal(context.Context, uuid.UUID, string) (store.Proposal, error) {
	return store.Proposal{}, store.ErrNotFound
}

func (noProposals) BumpProposalGeneration(context.Context, uuid.UUID, string, *scribe.Proposal,
	[]scribe.ClearedField, scribe.Status) (store.Proposal, error) {
	return store.Proposal{}, errors.New("noProposals: the harness never writes a proposal")
}

type noTickets struct{}

func (noTickets) CreateTicket(context.Context, switchyard.NewTicket) (switchyard.Ticket, error) {
	return switchyard.Ticket{}, errors.New("noTickets: a read tool never creates a ticket")
}

func (noTickets) TicketsByMemo(context.Context, uuid.UUID) ([]switchyard.Ticket, error) {
	return nil, nil
}

func (noTickets) TicketURL(key string) string { return key }

type noCatalogue struct{}

func (noCatalogue) Fetch(context.Context) (*catalogue.Snapshot, error) {
	return nil, errors.New("noCatalogue: a read tool never fetches the catalogue")
}
