package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Einlanzerous/chronicle/internal/apiclient"
	"github.com/Einlanzerous/chronicle/internal/cfaccess"
)

// The hosted transport: Streamable HTTP behind Cloudflare Access, on the shape
// SERV-98 shipped for Switchyard (switchyard/mcp/src/http.ts), restated in Go.
//
// IT TAKES NO TOKEN. Identity is each request's Access assertion, verified
// here against THIS endpoint's own audience on every request, and exchanged
// once per session at POST /auth/sso/cloudflare for a session as that person.
// A process-wide credential here would be one token acting for everybody who
// connects, which is the thing the design exists to not have; HostedConfig has
// no field to put one in.
//
// Fail closed: no valid assertion is a 401 carrying
// `WWW-Authenticate: Bearer resource_metadata="…"` (RFC 9728, the shape a
// hosted MCP client's authorization handshake starts from) -- never a 200 and
// never a redirect. That is not only the handshake. The estate's internal
// entrypoint is reachable by every container on its network, so this check is
// also what stops a neighbour driving the endpoint with a forged Host header.
//
// A SESSION'S TOKEN DOES NOT OUTLIVE THE SESSION. It was minted for exactly
// one conversation and this process is the only holder of its plaintext, so
// every way a session ends -- the client's DELETE, the idle sweep, retirement
// ahead of the token's expiry, shutdown, an initialize the protocol layer
// refused after the exchange had already run -- revokes it, once. Switchyard's
// transport left 118 live tokens behind before it paired the two (SWY-444).

const (
	// HostedIdleTimeout drops a session that has gone this long without a
	// request. The client's next call gets a 404 and reinitializes, which is
	// the documented Streamable HTTP recovery. It must be shorter than the
	// token's lifetime (store.HostedSessionTTL, 12 h, set by the API) and is.
	HostedIdleTimeout = 60 * time.Minute
	// HostedExpiryMargin retires a session this long BEFORE its token's stated
	// expiry, so a call in flight when the deadline passes still finishes on a
	// live credential. Longer than any single API call this transport makes.
	HostedExpiryMargin = 5 * time.Minute

	hostedSweepInterval   = 5 * time.Minute
	hostedExchangeTimeout = 10 * time.Second
	// hostedRevokeTimeout must stay under the container's stop grace (Docker's
	// default is 10 s): shutdown waits for every session's revoke, and an
	// unreachable API must not eat the whole of it.
	hostedRevokeTimeout = 5 * time.Second

	// hostedMaxInitBody bounds the one body this transport reads itself, to
	// decide whether a request may open a session.
	hostedMaxInitBody = 1 << 20

	accessAssertionHeader = "Cf-Access-Jwt-Assertion"
	mcpSessionHeader      = "Mcp-Session-Id"
	prmWellKnown          = "/.well-known/oauth-protected-resource"
)

// HostedConfig is what the hosted transport needs. There is deliberately no
// credential in it.
type HostedConfig struct {
	// APIURL is where Chronicle's API is, for the exchange and for tool calls.
	APIURL string
	// PublicURL is the MCP endpoint as clients reach it, e.g.
	// "https://chronicle-mcp.example.com/mcp". Configured rather than derived
	// from the Host header, which is attacker-controlled on the internal
	// entrypoint and would otherwise feed the resource_metadata pointer.
	PublicURL string
	// TeamDomain is the Zero Trust team domain; it is the authorization server
	// the protected-resource metadata names.
	TeamDomain string
	// Verifier checks each request's assertion. It must have been built for
	// THIS endpoint's audience alone: an assertion issued for the web
	// application is somebody's browser session and must not open an MCP one.
	Verifier *cfaccess.Verifier
	Build    Build
	Logger   *slog.Logger

	// HTTPClient is used for calls to the API. Nil means a default with a
	// timeout.
	HTTPClient *http.Client
}

// Hosted is the hosted transport's HTTP handler.
type Hosted struct {
	cfg      HostedConfig
	mcpPath  string
	prmPath  string
	metadata string // the resource_metadata URL
	sdk      *sdk.StreamableHTTPHandler
	now      func() time.Time

	mu       sync.Mutex
	sessions map[string]*hostedSession
}

// hostedSession is one conversation.
type hostedSession struct {
	id string
	// email is the identity that opened the session, lowercased. Every later
	// request must present an assertion for the SAME identity: a session id is
	// routing state, not a credential, and must not let one person ride
	// another's token.
	email  string
	server *sdk.Server
	api    *apiclient.ClientWithResponses
	// expiresAt is when the API stops honouring this session's token. Zero
	// means the API did not say, and the session ends on idleness or close.
	expiresAt time.Time

	mu       sync.Mutex
	lastSeen time.Time

	// retire runs at most once however many paths reach it -- the sweep, a
	// DELETE and shutdown can all arrive for one ending.
	retire sync.Once
	done   chan struct{}
}

type sessionCtxKey struct{}

// NewHosted builds the hosted transport. It fails on a configuration that
// could not serve, so the process refuses to start rather than answering its
// first request wrongly.
func NewHosted(cfg HostedConfig) (*Hosted, error) {
	if cfg.Verifier == nil {
		return nil, errors.New("mcp: hosted transport needs an Access verifier")
	}
	if cfg.APIURL == "" || cfg.TeamDomain == "" {
		return nil, errors.New("mcp: hosted transport needs the API URL and the Access team domain")
	}
	pub, err := url.Parse(cfg.PublicURL)
	if err != nil || pub.Scheme == "" || pub.Host == "" {
		return nil, fmt.Errorf("mcp: public URL %q is not an absolute URL", cfg.PublicURL)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	cfg.APIURL = strings.TrimRight(cfg.APIURL, "/")
	cfg.TeamDomain = cfaccess.NormalizeTeamDomain(cfg.TeamDomain)

	h := &Hosted{cfg: cfg, now: time.Now, sessions: map[string]*hostedSession{}}
	h.mcpPath = strings.TrimRight(pub.Path, "/")
	if h.mcpPath == "" {
		h.mcpPath = "/"
	}
	// RFC 9728 path insertion: metadata for "https://host/mcp" lives at
	// "/.well-known/oauth-protected-resource/mcp". The bare path is served too,
	// for a client that skips the insertion step.
	h.prmPath = prmWellKnown
	if h.mcpPath != "/" {
		h.prmPath = prmWellKnown + h.mcpPath
	}
	h.metadata = pub.Scheme + "://" + pub.Host + h.prmPath

	// The SDK asks for a server on EVERY request, not only the one that opens
	// a session, so this must have no side effects: it hands back the server
	// already built for the session this handler put in the request context.
	// The exchange has happened, or been refused, before the SDK is reached.
	h.sdk = sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		if s, ok := r.Context().Value(sessionCtxKey{}).(*hostedSession); ok {
			return s.server
		}
		return nil
	}, &sdk.StreamableHTTPOptions{Logger: cfg.Logger})
	return h, nil
}

// ServeHTTP routes the three things this process serves.
func (h *Hosted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	// Liveness for the container healthcheck, and the version contract the
	// delivery ledger polls. Unauthenticated, and bare of anything but the two
	// identifiers the image already carries as labels.
	case r.Method == http.MethodGet && r.URL.Path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok", "version": h.version(), "sha": nullable(h.cfg.Build.Commit),
		})
	// Protected-resource metadata: the document the 401's pointer names.
	// Unauthenticated by design -- a client reads it precisely when it has no
	// credentials yet.
	case r.Method == http.MethodGet && (r.URL.Path == h.prmPath || r.URL.Path == prmWellKnown):
		writeJSON(w, http.StatusOK, map[string]any{
			"resource":                 h.cfg.PublicURL,
			"authorization_servers":    []string{"https://" + h.cfg.TeamDomain},
			"bearer_methods_supported": []string{"header"},
		})
	case r.URL.Path == h.mcpPath:
		h.serveMCP(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "no route for "+r.URL.Path)
	}
}

func (h *Hosted) serveMCP(w http.ResponseWriter, r *http.Request) {
	// Every request, not just the first: an expired or forged assertion must
	// stop working mid-session too.
	assertion := strings.TrimSpace(r.Header.Get(accessAssertionHeader))
	if assertion == "" {
		h.unauthorized(w, "Cf-Access-Jwt-Assertion header missing", false)
		return
	}
	id, err := h.cfg.Verifier.Verify(r.Context(), assertion)
	if err != nil {
		h.unauthorized(w, "invalid Cloudflare Access token", true)
		return
	}
	email := strings.ToLower(id.Email)

	if sid := strings.TrimSpace(r.Header.Get(mcpSessionHeader)); sid != "" {
		h.serveSession(w, r, sid, email)
		return
	}
	h.openSession(w, r, assertion, email)
}

// serveSession handles a request that names a session.
func (h *Hosted) serveSession(w http.ResponseWriter, r *http.Request, sid, email string) {
	h.mu.Lock()
	s := h.sessions[sid]
	h.mu.Unlock()
	// 404 tells a spec-following client to reinitialize. It is also the answer
	// after a restart, when the table is empty and the client's id means
	// nothing here.
	if s == nil {
		sessionNotFound(w)
		return
	}
	if s.email != email {
		h.unauthorized(w, "Access identity does not match this MCP session", true)
		return
	}
	// The token this session runs as stops working at its expiry and nothing
	// here can renew it. Left alone the session would carry on with a dead
	// credential: every tool call would fail, and because a failing call still
	// counts as activity it would never idle out either. So retire it just
	// ahead of the deadline and answer as for an unknown session -- the client
	// reinitializes, which exchanges afresh.
	if !s.expiresAt.IsZero() && !h.now().Before(s.expiresAt.Add(-HostedExpiryMargin)) {
		h.drop(s)
		go h.retire(s)
		sessionNotFound(w)
		return
	}
	s.touch(h.now())

	r = r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, s))
	if r.Method == http.MethodDelete {
		// Out of the table first, so no request can find a session whose
		// token is about to be revoked; then the SDK closes its own half and
		// answers the client; then the token goes.
		h.drop(s)
		h.sdk.ServeHTTP(w, r)
		h.retire(s)
		return
	}
	h.sdk.ServeHTTP(w, r)
}

// openSession handles a request with no session id: only an initialize may
// open one.
func (h *Hosted) openSession(w http.ResponseWriter, r *http.Request, assertion, email string) {
	if r.Method != http.MethodPost {
		rpcError(w, http.StatusBadRequest, -32000, "Bad Request: no valid session ID provided")
		return
	}
	// The body is read and checked BEFORE the exchange, so a garbage request
	// cannot mint a token.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, hostedMaxInitBody))
	if err != nil {
		rpcError(w, http.StatusBadRequest, -32700, "Parse error: request body could not be read")
		return
	}
	isInit, ok := isInitialize(body)
	if !ok {
		rpcError(w, http.StatusBadRequest, -32700, "Parse error: request body is not valid JSON")
		return
	}
	if !isInit {
		rpcError(w, http.StatusBadRequest, -32000, "Bad Request: no valid session ID provided")
		return
	}

	token, expiresAt, refusal := h.exchange(r.Context(), assertion)
	if refusal != nil {
		refusal(w)
		return
	}
	api, err := NewAPI(h.cfg.APIURL, token, h.cfg.HTTPClient)
	if err != nil {
		writeError(w, http.StatusBadGateway, "bad_gateway", "could not build an API client")
		return
	}

	// The id is chosen HERE, before the SDK is reached, and the session's
	// server is told to use it. Left to itself the SDK assigns one only after
	// this handler has already minted a token, and the token would have no key
	// to be held -- and later revoked -- under.
	s := &hostedSession{
		id: uuid.NewString(), email: email, api: api, expiresAt: expiresAt,
		lastSeen: h.now(), done: make(chan struct{}),
	}

	ident, err := resolve(r.Context(), TransportHosted, api)
	if err != nil {
		go h.retire(s)
		h.cfg.Logger.Error("mcp: resolve identity after exchange", "err", err)
		writeError(w, http.StatusBadGateway, "bad_gateway", "could not establish who this session runs as")
		return
	}
	s.server = BuildServer(h.cfg.Build, Session{Identity: ident, API: api},
		&sdk.ServerOptions{GetSessionID: func() string { return s.id }})

	h.mu.Lock()
	h.sessions[s.id] = s
	h.mu.Unlock()

	r.Body = io.NopCloser(bytes.NewReader(body))
	rec := &statusRecorder{ResponseWriter: w}
	h.sdk.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), sessionCtxKey{}, s)))

	// The exchange ran before the protocol layer had looked at this request,
	// and it refuses an initialize for reasons this handler does not check --
	// a wrong Accept, a wrong Content-Type, an unsupported version. Then no
	// session opened, and nothing else will ever find the token just minted
	// for it. Nobody can revoke it but us, here.
	if rec.status >= 300 || rec.Header().Get(mcpSessionHeader) != s.id {
		h.drop(s)
		go h.retire(s)
		return
	}
	h.cfg.Logger.Info("mcp: session opened", "session", s.id, "kind", ident.Kind)
}

// exchange trades a verified assertion for that person's session token. On
// failure it returns the refusal to write, so the API's answer passes through
// with its own status.
func (h *Hosted) exchange(ctx context.Context, assertion string) (string, time.Time, func(http.ResponseWriter)) {
	// The exchange is the one call made with no session credential, so it
	// uses a client of its own. The contract deliberately does not declare the
	// assertion header as a parameter, so it is set by a request editor.
	anon, err := apiclient.NewClientWithResponses(h.cfg.APIURL, apiclient.WithHTTPClient(h.cfg.HTTPClient))
	if err != nil {
		return "", time.Time{}, func(w http.ResponseWriter) {
			writeError(w, http.StatusBadGateway, "bad_gateway", "could not build an API client")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, hostedExchangeTimeout)
	defer cancel()
	resp, err := anon.CreateSessionFromAccessWithResponse(ctx, func(_ context.Context, req *http.Request) error {
		req.Header.Set(accessAssertionHeader, assertion)
		return nil
	})
	if err != nil {
		h.cfg.Logger.Error("mcp: token exchange unreachable", "err", err)
		return "", time.Time{}, func(w http.ResponseWriter) {
			writeError(w, http.StatusBadGateway, "bad_gateway", "token exchange unreachable")
		}
	}

	switch status := resp.StatusCode(); {
	case status == http.StatusOK && resp.JSON200 != nil && resp.JSON200.SessionToken != "":
		var expiresAt time.Time
		if resp.JSON200.ExpiresAt != nil {
			expiresAt = *resp.JSON200.ExpiresAt
		}
		return resp.JSON200.SessionToken, expiresAt, nil
	// The API disagrees that this assertion is valid, or has no such audience
	// configured. Re-authenticating is the client's move.
	case status == http.StatusUnauthorized:
		return "", time.Time{}, func(w http.ResponseWriter) {
			h.unauthorized(w, ssoMessage(resp.JSON401, "token exchange refused"), true)
		}
	// A valid identity with no Chronicle account, or an agent's. Presenting
	// the same assertion again cannot help, so it is not a WWW-Authenticate
	// case.
	case status == http.StatusForbidden:
		code, msg := "forbidden", ssoMessage(resp.JSON403, "this identity has no Chronicle account")
		if resp.JSON403 != nil && resp.JSON403.Code != "" {
			code = resp.JSON403.Code
		}
		return "", time.Time{}, func(w http.ResponseWriter) { writeError(w, http.StatusForbidden, code, msg) }
	// The API's sign-in limiter. Passed through as itself rather than hidden
	// behind a 502: every hosted exchange shares one bucket (this process's
	// address), and the client should know to wait.
	case status == http.StatusTooManyRequests:
		return "", time.Time{}, func(w http.ResponseWriter) {
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many sign-ins, slow down")
		}
	default:
		h.cfg.Logger.Error("mcp: token exchange failed", "status", status)
		return "", time.Time{}, func(w http.ResponseWriter) {
			writeError(w, http.StatusBadGateway, "bad_gateway", fmt.Sprintf("token exchange failed with %d", status))
		}
	}
}

func ssoMessage(e *apiclient.SsoError, fallback string) string {
	if e != nil && e.Message != "" {
		return e.Message
	}
	return fallback
}

// drop takes a session out of the table. It does not end it; retire does.
func (h *Hosted) drop(s *hostedSession) {
	h.mu.Lock()
	if h.sessions[s.id] == s {
		delete(h.sessions, s.id)
	}
	h.mu.Unlock()
}

// retire is the one way a session ends: the protocol half is closed and the
// token is revoked, as a pair. It runs once per session and never fails -- by
// the time it runs there is nobody left to tell, and the API expires the token
// on its own if the revoke does not arrive.
//
// It revokes through the session's own client, so the secret stays where it
// already lives rather than being copied onto the session record.
func (h *Hosted) retire(s *hostedSession) {
	s.retire.Do(func() {
		defer close(s.done)
		if s.server != nil {
			for ss := range s.server.Sessions() {
				_ = ss.Close()
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), hostedRevokeTimeout)
		defer cancel()
		resp, err := s.api.DeleteSessionWithResponse(ctx)
		if err != nil {
			h.cfg.Logger.Warn("mcp: revoke session token", "session", s.id, "err", err)
			return
		}
		if resp.StatusCode() >= 300 && resp.StatusCode() != http.StatusUnauthorized {
			h.cfg.Logger.Warn("mcp: revoke session token", "session", s.id, "status", resp.StatusCode())
		}
	})
	<-s.done
}

// Sweep retires every session idle for longer than HostedIdleTimeout and
// reports how many. Run calls it on a ticker.
func (h *Hosted) Sweep() int {
	now := h.now()
	h.mu.Lock()
	var idle []*hostedSession
	for id, s := range h.sessions {
		if now.Sub(s.seen()) > HostedIdleTimeout {
			delete(h.sessions, id)
			idle = append(idle, s)
		}
	}
	h.mu.Unlock()
	for _, s := range idle {
		go h.retire(s)
	}
	return len(idle)
}

// Run sweeps idle sessions until ctx ends.
func (h *Hosted) Run(ctx context.Context) {
	t := time.NewTicker(hostedSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n := h.Sweep(); n > 0 {
				h.cfg.Logger.Info("mcp: idle sessions retired", "count", n)
			}
		}
	}
}

// Close retires every session and waits for the revokes. They run
// concurrently, so the wait is one revoke timeout however many sessions are
// open: the process exits right after this, and a token whose revoke never
// left is one only its own expiry will end.
func (h *Hosted) Close() {
	h.mu.Lock()
	all := make([]*hostedSession, 0, len(h.sessions))
	for id, s := range h.sessions {
		delete(h.sessions, id)
		all = append(all, s)
	}
	h.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range all {
		wg.Add(1)
		go func() { defer wg.Done(); h.retire(s) }()
	}
	wg.Wait()
}

// SessionCount reports how many sessions are open.
func (h *Hosted) SessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

func (s *hostedSession) touch(t time.Time) {
	s.mu.Lock()
	s.lastSeen = t
	s.mu.Unlock()
}

func (s *hostedSession) seen() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen
}

func (h *Hosted) version() string {
	if h.cfg.Build.Version == "" {
		return "dev"
	}
	return h.cfg.Build.Version
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// unauthorized answers 401 with the RFC 9728 pointer.
func (h *Hosted) unauthorized(w http.ResponseWriter, message string, invalidToken bool) {
	params := `resource_metadata="` + h.metadata + `"`
	if invalidToken {
		params = `error="invalid_token", ` + params
	}
	w.Header().Set("WWW-Authenticate", "Bearer "+params)
	writeError(w, http.StatusUnauthorized, "unauthorized", message)
}

// isInitialize reports whether body is an MCP initialize request, alone or in
// a batch. The second result is false when body is not JSON at all.
func isInitialize(body []byte) (isInit, valid bool) {
	type envelope struct {
		Method string `json:"method"`
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var batch []envelope
		if err := json.Unmarshal(trimmed, &batch); err != nil {
			return false, false
		}
		for _, m := range batch {
			if m.Method == "initialize" {
				return true, true
			}
		}
		return false, true
	}
	var one envelope
	if err := json.Unmarshal(trimmed, &one); err != nil {
		return false, false
	}
	return one.Method == "initialize", true
}

// statusRecorder notes the status a handler answered with.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Flush keeps streamed responses streaming through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError answers in the API's own error envelope, so a client sees one
// vocabulary whichever process refused it.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}

// rpcError answers in the shape the Streamable HTTP transport itself uses for
// a refusal before dispatch.
func rpcError(w http.ResponseWriter, status, code int, message string) {
	writeJSON(w, status, map[string]any{
		"jsonrpc": "2.0", "error": map[string]any{"code": code, "message": message}, "id": nil,
	})
}

func sessionNotFound(w http.ResponseWriter) {
	rpcError(w, http.StatusNotFound, -32001, "Session not found")
}
