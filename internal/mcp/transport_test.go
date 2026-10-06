package mcp_test

// The two transports, run against the REAL API handler on the test database.
//
// These tests live in an external test package and import internal/api and
// internal/store, which the code under test may not: verify.sh's `mcp boundary`
// step checks the non-test graph, deliberately, because a test of "this is a
// client of the API" needs an API to be a client of. The transport reaches
// that API over HTTP, through an httptest server, exactly as it does in
// production; nothing here hands it a Go value from the other side.
//
// They reset the shared chronicle_test like internal/store and internal/api
// do, which is why verify.sh runs one test binary at a time.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Einlanzerous/chronicle/internal/api"
	"github.com/Einlanzerous/chronicle/internal/cfaccess/cfaccesstest"
	"github.com/Einlanzerous/chronicle/internal/mcp"
	"github.com/Einlanzerous/chronicle/internal/store"
)

const (
	webAUD    = "web-application-tag"
	mcpAUD    = "mcp-application-tag"
	publicURL = "https://chronicle-mcp.test/mcp"
	prmURL    = "https://chronicle-mcp.test/.well-known/oauth-protected-resource/mcp"
)

// world is a Chronicle API with a database behind it, an Access issuer, and a
// count of the two calls the hosted transport's guarantees are stated in.
type world struct {
	st     *store.Store
	ctx    context.Context
	apiURL string
	iss    *cfaccesstest.Issuer
	owner  store.User

	mu        sync.Mutex
	exchanges int
	revokes   int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	w := &world{st: store.New(pool), ctx: ctx, iss: cfaccesstest.New(t)}
	if w.owner, err = w.st.GetOwner(ctx); err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	// The API as serve builds it for a deployment with both Access
	// applications configured.
	handler := api.NewRouter(api.Deps{
		DB: w.st, Accounts: w.st, Version: "test", SecureCookies: true,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		CFAccess: w.iss.Verifier(webAUD, mcpAUD), MCPAudience: mcpAUD,
	})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		w.mu.Lock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/auth/sso/cloudflare":
			w.exchanges++
		case r.Method == http.MethodDelete && r.URL.Path == "/auth/session":
			w.revokes++
		}
		w.mu.Unlock()
		handler.ServeHTTP(rw, r)
	}))
	t.Cleanup(srv.Close)
	w.apiURL = srv.URL
	return w
}

func (w *world) counts() (exchanges, revokes int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exchanges, w.revokes
}

// liveHosted counts the owner's hosted session rows the API would still honour.
func (w *world) liveHosted(t *testing.T) int {
	t.Helper()
	sessions, err := w.st.ListSessions(w.ctx, w.owner.ID, "")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	n := 0
	for _, s := range sessions {
		if s.Label == store.HostedSessionLabel {
			n++
		}
	}
	return n
}

func (w *world) assertion(t *testing.T, email, aud string) string {
	t.Helper()
	return w.iss.Sign(t, cfaccesstest.Claims{Email: email, Audience: aud})
}

// hosted starts a hosted transport pointed at the world's API, verifying
// against the MCP application's audience alone.
func (w *world) hosted(t *testing.T) (*mcp.Hosted, string) {
	t.Helper()
	h, err := mcp.NewHosted(mcp.HostedConfig{
		APIURL: w.apiURL, PublicURL: publicURL, TeamDomain: cfaccesstest.Team,
		Verifier: w.iss.Verifier(mcpAUD), Build: mcp.Build{Version: "1.2.3", Commit: "abc123"},
	})
	if err != nil {
		t.Fatalf("NewHosted: %v", err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	return h, srv.URL + "/mcp"
}

// withAssertion is an HTTP client that presents one Access assertion, the way
// Cloudflare's edge does for a client that has signed in.
type withAssertion struct{ assertion string }

func (a withAssertion) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Cf-Access-Jwt-Assertion", a.assertion)
	return http.DefaultTransport.RoundTrip(r)
}

func connect(t *testing.T, endpoint, assertion string) *sdk.ClientSession {
	t.Helper()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(context.Background(),
		&sdk.StreamableClientTransport{
			Endpoint: endpoint, HTTPClient: &http.Client{Transport: withAssertion{assertion}},
			DisableStandaloneSSE: true, MaxRetries: -1,
		}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return cs
}

func callWhoAmI(t *testing.T, cs *sdk.ClientSession) mcp.WhoAmI {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if res.IsError {
		t.Fatalf("whoami answered an error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out mcp.WhoAmI
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode whoami: %v", err)
	}
	return out
}

func toolNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"raw","version":"0"}}}`

// raw sends one request by hand, for the cases an MCP client would never send.
func raw(t *testing.T, method, endpoint, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// waitFor polls until cond holds: a session's revoke after a sweep or an
// expiry runs off the request path.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// --- Done when 3: an unauthenticated caller gets nothing --------------------

func TestHostedRefusesEveryCallerItCannotIdentify(t *testing.T) {
	w := newWorld(t)
	_, endpoint := w.hosted(t)
	good := cfaccesstest.Claims{Email: w.owner.Email, Audience: mcpAUD}
	expired := good
	expired.Expires = -time.Minute

	for name, assertion := range map[string]string{
		"no assertion":                        "",
		"signed by an unknown key":            w.iss.SignWithUnknownKey(t, good),
		"issued for the web application":      w.assertion(t, w.owner.Email, webAUD),
		"expired":                             w.iss.Sign(t, expired),
		"not an assertion at all":             "nonsense",
		"alg none, the email simply asserted": "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6Im1hZ29zQGV4YW1wbGUuY29tIn0.",
	} {
		t.Run(name, func(t *testing.T) {
			resp := raw(t, http.MethodPost, endpoint, initializeBody, map[string]string{"Cf-Access-Jwt-Assertion": assertion})
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			challenge := resp.Header.Get("WWW-Authenticate")
			if !strings.HasPrefix(challenge, "Bearer ") || !strings.Contains(challenge, `resource_metadata="`+prmURL+`"`) {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge naming %s", challenge, prmURL)
			}
			if resp.Header.Get("Mcp-Session-Id") != "" {
				t.Error("a refused request was given a session id")
			}
		})
	}
	if ex, _ := w.counts(); ex != 0 {
		t.Errorf("the API's exchange was called %d times for callers the transport should have refused itself", ex)
	}
	if n := w.liveHosted(t); n != 0 {
		t.Errorf("%d hosted sessions were minted for unidentified callers", n)
	}
}

func TestHostedServesItsMetadataAndHealthWithoutCredentials(t *testing.T) {
	w := newWorld(t)
	_, endpoint := w.hosted(t)
	base := strings.TrimSuffix(endpoint, "/mcp")

	for _, path := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		resp := raw(t, http.MethodGet, base+path, "", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
		}
		var doc struct {
			Resource             string   `json:"resource"`
			AuthorizationServers []string `json:"authorization_servers"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if doc.Resource != publicURL || len(doc.AuthorizationServers) != 1 || doc.AuthorizationServers[0] != "https://"+cfaccesstest.Team {
			t.Errorf("GET %s = %+v, want the public URL and the team domain", path, doc)
		}
	}

	resp := raw(t, http.MethodGet, base+"/healthz", "", nil)
	var health map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.StatusCode != http.StatusOK || health["status"] != "ok" || health["version"] != "1.2.3" || health["sha"] != "abc123" {
		t.Errorf("GET /healthz = %d %v, want ok with the build's version and sha", resp.StatusCode, health)
	}

	if resp := raw(t, http.MethodGet, base+"/auth/me", "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /auth/me on the MCP process = %d, want 404; it is not a proxy to the API", resp.StatusCode)
	}
}

// --- Done when 2: a hosted client through Access ----------------------------

func TestHostedSessionActsAsThePersonAndExchangesOnce(t *testing.T) {
	w := newWorld(t)
	_, endpoint := w.hosted(t)

	cs := connect(t, endpoint, w.assertion(t, w.owner.Email, mcpAUD))
	defer func() { _ = cs.Close() }()

	if names := toolNames(t, cs); len(names) != 1 || names[0] != "whoami" {
		t.Fatalf("tools = %v, want exactly whoami", names)
	}
	for i := 0; i < 3; i++ {
		who := callWhoAmI(t, cs)
		if who.Kind != "person" || who.Transport != "hosted" || who.MayWrite || who.Email != w.owner.Email || !who.IsOwner {
			t.Fatalf("whoami = %+v, want the owner as a person on hosted, not writing", who)
		}
	}
	if ex, _ := w.counts(); ex != 1 {
		t.Errorf("exchanges = %d, want exactly one for the session however many requests follow", ex)
	}
	if n := w.liveHosted(t); n != 1 {
		t.Errorf("live hosted sessions = %d, want 1", n)
	}
}

// The 404-then-reinitialize path, including across a restart, when the new
// process has never heard of the client's session.
func TestHostedSessionSurvivesAReconnect(t *testing.T) {
	w := newWorld(t)
	assertion := w.assertion(t, w.owner.Email, mcpAUD)
	_, endpoint := w.hosted(t)

	first := raw(t, http.MethodPost, endpoint, initializeBody, map[string]string{"Cf-Access-Jwt-Assertion": assertion})
	sid := first.Header.Get("Mcp-Session-Id")
	if first.StatusCode != http.StatusOK || sid == "" {
		t.Fatalf("initialize = %d with session %q, want 200 and an id", first.StatusCode, sid)
	}

	// A new process: same configuration, an empty table.
	_, restarted := w.hosted(t)
	stale := raw(t, http.MethodPost, restarted, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		map[string]string{"Cf-Access-Jwt-Assertion": assertion, "Mcp-Session-Id": sid})
	if stale.StatusCode != http.StatusNotFound {
		t.Fatalf("a session id the process does not hold = %d, want 404", stale.StatusCode)
	}

	cs := connect(t, restarted, assertion)
	defer func() { _ = cs.Close() }()
	if who := callWhoAmI(t, cs); who.Email != w.owner.Email {
		t.Errorf("after reinitializing, whoami = %+v", who)
	}
}

func TestHostedSessionCannotBeRiddenByAnotherIdentity(t *testing.T) {
	w := newWorld(t)
	other, err := w.st.CreateUser(w.ctx, "other@example.com", "Other", store.KindPerson)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, endpoint := w.hosted(t)

	first := raw(t, http.MethodPost, endpoint, initializeBody,
		map[string]string{"Cf-Access-Jwt-Assertion": w.assertion(t, w.owner.Email, mcpAUD)})
	sid := first.Header.Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatalf("initialize = %d, no session id", first.StatusCode)
	}

	// A perfectly valid assertion, for somebody else, naming the owner's session.
	resp := raw(t, http.MethodPost, endpoint, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`,
		map[string]string{"Cf-Access-Jwt-Assertion": w.assertion(t, other.Email, mcpAUD), "Mcp-Session-Id": sid})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), w.owner.Email) {
		t.Errorf("the refusal carries the session owner's identity: %s", body)
	}
}

// Only a person comes in through Access; the API's refusal passes through.
func TestHostedPassesThroughTheExchangesRefusal(t *testing.T) {
	w := newWorld(t)
	if _, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, endpoint := w.hosted(t)

	for email, wantCode := range map[string]string{"bot@example.com": "sso_agent_account", "stranger@example.com": "sso_no_account"} {
		resp := raw(t, http.MethodPost, endpoint, initializeBody,
			map[string]string{"Cf-Access-Jwt-Assertion": w.assertion(t, email, mcpAUD)})
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if resp.StatusCode != http.StatusForbidden || body.Code != wantCode {
			t.Errorf("%s: %d %q, want 403 %s", email, resp.StatusCode, body.Code, wantCode)
		}
		if resp.Header.Get("Mcp-Session-Id") != "" {
			t.Errorf("%s was given a session", email)
		}
	}
}

// --- A session's token does not outlive the session --------------------------

func TestEveryWayAHostedSessionEndsRevokesItsTokenOnce(t *testing.T) {
	type ending func(t *testing.T, w *world, h *mcp.Hosted, endpoint, assertion, sid string)

	endings := map[string]ending{
		"the client's DELETE": func(t *testing.T, _ *world, _ *mcp.Hosted, endpoint, assertion, sid string) {
			resp := raw(t, http.MethodDelete, endpoint, "", map[string]string{"Cf-Access-Jwt-Assertion": assertion, "Mcp-Session-Id": sid})
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
			}
		},
		"the idle sweep": func(t *testing.T, _ *world, h *mcp.Hosted, _, _, _ string) {
			h.SetNow(func() time.Time { return time.Now().Add(mcp.HostedIdleTimeout + time.Minute) })
			if n := h.Sweep(); n != 1 {
				t.Fatalf("Sweep retired %d sessions, want 1", n)
			}
		},
		"retirement ahead of the token's expiry": func(t *testing.T, _ *world, h *mcp.Hosted, endpoint, assertion, sid string) {
			// Inside the margin: the token still works, and the session must
			// already be gone.
			h.SetNow(func() time.Time { return time.Now().Add(store.HostedSessionTTL - mcp.HostedExpiryMargin + time.Minute) })
			resp := raw(t, http.MethodPost, endpoint, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
				map[string]string{"Cf-Access-Jwt-Assertion": assertion, "Mcp-Session-Id": sid})
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("a request inside the expiry margin = %d, want 404", resp.StatusCode)
			}
		},
		"shutdown": func(_ *testing.T, _ *world, h *mcp.Hosted, _, _, _ string) {
			h.Close()
		},
	}

	for name, end := range endings {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			h, endpoint := w.hosted(t)
			assertion := w.assertion(t, w.owner.Email, mcpAUD)

			first := raw(t, http.MethodPost, endpoint, initializeBody, map[string]string{"Cf-Access-Jwt-Assertion": assertion})
			sid := first.Header.Get("Mcp-Session-Id")
			if sid == "" || w.liveHosted(t) != 1 {
				t.Fatalf("initialize = %d, session %q, %d live hosted rows; want a session and one row", first.StatusCode, sid, w.liveHosted(t))
			}

			end(t, w, h, endpoint, assertion, sid)

			waitFor(t, "the session's token to be revoked", func() bool { return w.liveHosted(t) == 0 })
			if h.SessionCount() != 0 {
				t.Errorf("SessionCount = %d after the session ended", h.SessionCount())
			}
			// Ending it again, by every other route, must not revoke twice.
			h.Sweep()
			h.Close()
			if _, revokes := w.counts(); revokes != 1 {
				t.Errorf("revokes = %d, want exactly one", revokes)
			}
			// And the id is dead.
			resp := raw(t, http.MethodPost, endpoint, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
				map[string]string{"Cf-Access-Jwt-Assertion": assertion, "Mcp-Session-Id": sid})
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("the ended session's id = %d, want 404", resp.StatusCode)
			}
		})
	}
}

// The exchange runs before the protocol layer has looked at the request. When
// that layer then refuses the initialize, no session opened -- and the token
// minted for it has nobody to revoke it but the handler that minted it.
func TestAnInitializeRefusedAfterTheExchangeLeavesNoToken(t *testing.T) {
	w := newWorld(t)
	h, endpoint := w.hosted(t)

	resp := raw(t, http.MethodPost, endpoint, initializeBody, map[string]string{
		"Cf-Access-Jwt-Assertion": w.assertion(t, w.owner.Email, mcpAUD),
		"Accept":                  "text/html", // the transport requires JSON and SSE
	})
	if resp.StatusCode < 400 {
		t.Fatalf("status = %d; the protocol layer was expected to refuse this Accept", resp.StatusCode)
	}

	ex, _ := w.counts()
	if ex != 1 {
		t.Fatalf("exchanges = %d; this test is only about a refusal AFTER an exchange", ex)
	}
	waitFor(t, "the orphaned token to be revoked", func() bool { return w.liveHosted(t) == 0 })
	if _, revokes := w.counts(); revokes != 1 {
		t.Errorf("revokes = %d, want exactly one", revokes)
	}
	if h.SessionCount() != 0 {
		t.Errorf("SessionCount = %d for a session that never opened", h.SessionCount())
	}
}

// A request that could not open a session must not mint a token on the way to
// being refused.
func TestOnlyAnInitializeMayOpenAHostedSession(t *testing.T) {
	w := newWorld(t)
	_, endpoint := w.hosted(t)
	auth := map[string]string{"Cf-Access-Jwt-Assertion": w.assertion(t, w.owner.Email, mcpAUD)}

	for name, send := range map[string]func() *http.Response{
		"a tool call with no session": func() *http.Response {
			return raw(t, http.MethodPost, endpoint, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, auth)
		},
		"not JSON": func() *http.Response { return raw(t, http.MethodPost, endpoint, `<html>`, auth) },
		"a GET":    func() *http.Response { return raw(t, http.MethodGet, endpoint, "", auth) },
	} {
		if resp := send(); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, resp.StatusCode)
		}
	}
	if ex, _ := w.counts(); ex != 0 {
		t.Errorf("exchanges = %d, want none", ex)
	}
}

// --- Done when 1: stdio, the agent-attributed path ---------------------------

// sessionFor gives an account a session token the way one is really obtained:
// an invite, redeemed.
func (w *world) sessionFor(t *testing.T, u store.User, label string) string {
	t.Helper()
	invite, err := w.st.MintInvite(w.ctx, u.ID, store.InviteLabelIssued)
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	_, session, err := w.st.RedeemInvite(w.ctx, invite, label)
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	return session
}

// stdio runs the stdio transport over a pipe and returns a client on the other
// end of it.
func stdio(t *testing.T, cfg mcp.StdioConfig) *sdk.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverSide, clientSide := sdk.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- mcp.RunStdioOn(ctx, cfg, serverSide) }()
	t.Cleanup(func() { cancel(); <-done })

	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func TestStdioRunsAsTheAccountItsTokenNames(t *testing.T) {
	w := newWorld(t)
	bot, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	for _, tc := range []struct {
		name string
		user store.User
		kind string
	}{
		{"an agent's token", bot, "agent"},
		// Ruling: a person's token runs, with read tools only.
		{"a person's token", w.owner, "person"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := stdio(t, mcp.StdioConfig{BaseURL: w.apiURL, Token: w.sessionFor(t, tc.user, "mcp-stdio")})

			if names := toolNames(t, cs); len(names) != 1 || names[0] != "whoami" {
				t.Fatalf("tools = %v, want exactly whoami", names)
			}
			who := callWhoAmI(t, cs)
			if who.Kind != tc.kind || who.Transport != "stdio" || who.Email != tc.user.Email {
				t.Errorf("whoami = %+v, want %s as a %s on stdio", who, tc.user.Email, tc.kind)
			}
			// No write tool exists yet, so the gate is checked where it lives
			// (TestBuildServerOffersMutatingToolsOnlyToAnAgentOnStdio); this is
			// what the session is told about itself.
			if who.MayWrite != (tc.kind == "agent") {
				t.Errorf("may_write = %v for a %s on stdio", who.MayWrite, tc.kind)
			}
		})
	}
	if ex, _ := w.counts(); ex != 0 {
		t.Errorf("stdio called the Access exchange %d times; it has a token and no assertion", ex)
	}
}

// It does not start with an identity it could not establish.
func TestStdioRefusesToStartWithoutAnIdentity(t *testing.T) {
	w := newWorld(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()

	for name, cfg := range map[string]mcp.StdioConfig{
		"no token":           {BaseURL: w.apiURL, Token: ""},
		"a token nobody has": {BaseURL: w.apiURL, Token: "chr_not_a_real_token"},
		"an unreachable API": {BaseURL: dead.URL, Token: "chr_anything"},
	} {
		t.Run(name, func(t *testing.T) {
			serverSide, _ := sdk.NewInMemoryTransports()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := mcp.RunStdioOn(ctx, cfg, serverSide); err == nil {
				t.Fatal("RunStdioOn started; want an error before it speaks MCP")
			}
		})
	}
}
