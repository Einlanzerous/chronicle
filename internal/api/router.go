// Package api is Chronicle's HTTP surface: the router every later endpoint
// hangs off, the two health probes the deploy path needs, and the credential
// surface from CHRN-71.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// Pinger is the slice of the store the readiness probe needs. An interface so
// readiness can be tested without a database.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Accounts is the slice of the store the credential surface needs. Named here
// rather than taking *store.Store so the middleware can be tested against a
// fake — the assertion that an unauthenticated request reaches nothing should
// not need Postgres to run.
type Accounts interface {
	CreateUser(ctx context.Context, email, displayName, kind string) (store.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (store.User, error)
	GetUserByEmail(ctx context.Context, email string) (store.User, error)
	UpdateDisplayName(ctx context.Context, id uuid.UUID, name string) (store.User, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
	ListMembers(ctx context.Context) ([]store.Member, error)

	MintToken(ctx context.Context, userID uuid.UUID, kind, label string, expiresAt *time.Time) (string, error)
	MintInvite(ctx context.Context, userID uuid.UUID, label string) (string, error)
	ReplaceDeviceInvite(ctx context.Context, userID uuid.UUID) (string, error)
	UserByToken(ctx context.Context, plaintext string) (store.User, error)
	RedeemInvite(ctx context.Context, plaintext, deviceLabel string) (store.User, string, error)
	RevokeToken(ctx context.Context, plaintext string) error
	ListSessions(ctx context.Context, userID uuid.UUID, currentPlaintext string) ([]store.Session, error)
	RevokeSession(ctx context.Context, userID, sessionID uuid.UUID, currentPlaintext string) (bool, error)
}

// Deps is what the router needs to serve.
type Deps struct {
	DB       Pinger
	Accounts Accounts
	Logger   *slog.Logger
	Version  string
	Commit   string

	// CFAccess verifies Cloudflare Access JWTs. Nil disables SSO, and
	// POST /auth/sso/cloudflare then answers sso_disabled rather than 404 — a
	// client can tell "not configured here" from "wrong URL".
	CFAccess *CFAccessVerifier

	// MobileBaseURL is the origin baked into an invite's sign-in link. Empty
	// omits the link, leaving clients on their own fallback.
	MobileBaseURL string

	// SecureCookies sets the Secure flag on the session cookie. It is a config
	// value rather than something derived from the request because TLS
	// terminates at Traefik: r.TLS is nil for every request this service ever
	// sees in the deployment it ships, WAN entrypoint included.
	SecureCookies bool

	// TrustedProxies are the peers whose X-Forwarded-For may be believed when
	// keying the sign-in rate limiter. Empty means trust nobody, which makes
	// every request through Traefik share one bucket.
	TrustedProxies []netip.Prefix
}

// api holds what the handlers share.
type api struct {
	accounts       Accounts
	logger         *slog.Logger
	cfAccess       *CFAccessVerifier
	mobileBaseURL  string
	secureCookies  bool
	trustedProxies []netip.Prefix
	signInLimiter  *ipRateLimiter
}

// NewRouter builds the HTTP handler.
//
// The two probes answer different questions and must not be collapsed:
//
//	/healthz — is this process alive? No dependencies. If it answers, the
//	           binary is running, and a restart is the wrong remedy for a
//	           database that is merely down.
//	/readyz  — can this process serve traffic? Pings the database. A load
//	           balancer takes an unready instance out of rotation; it does
//	           not kill it.
//
// They are also the only two routes reachable without a credential, with the
// two sign-in endpoints. Everything else requires a session; there is no
// unauthenticated read surface. /healthz staying dependency-free is load
// bearing beyond liveness: CHRN-59's QR onboarding probes it to check a server
// address before committing to it, which happens before any credential exists.
func NewRouter(d Deps) http.Handler {
	a := &api{
		accounts:       d.Accounts,
		logger:         d.Logger,
		cfAccess:       d.CFAccess,
		mobileBaseURL:  d.MobileBaseURL,
		secureCookies:  d.SecureCookies,
		trustedProxies: d.TrustedProxies,
		signInLimiter:  newIPRateLimiter(signInRateWindow, signInRateBurst),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		body := map[string]string{"status": "ok", "version": d.Version}
		if d.Commit != "" {
			body["commit"] = d.Commit
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if d.DB != nil {
			if err := d.DB.Ping(ctx); err != nil {
				d.Logger.Warn("readiness probe failed", "check", "database", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": "unready",
					"check":  "database",
				})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	if d.Accounts == nil {
		return requestLogger(d.Logger, mux)
	}

	// Sign-in. Both paths mint the same kind of session, and both must stay
	// reachable without one.
	//
	// BOTH are rate-limited. They are the two unauthenticated endpoints that
	// mint a credential, and limiting only one of them just moves the target:
	// /auth/sso/cloudflare drives a JWKS fetch and a database write per call.
	mux.HandleFunc("POST /auth/session", a.limitSignIn(a.handleAuthSession))
	mux.HandleFunc("POST /auth/sso/cloudflare", a.limitSignIn(a.handleAuthCFAccess))

	mux.HandleFunc("DELETE /auth/session", a.requireUser(a.handleAuthSignOut))
	mux.HandleFunc("GET /auth/me", a.requireUser(a.handleAuthMe))
	mux.HandleFunc("PATCH /auth/me", a.requireUser(a.handleAuthUpdateMe))
	mux.HandleFunc("POST /auth/invite", a.requireUser(a.handleSelfInvite))
	mux.HandleFunc("GET /auth/sessions", a.requireUser(a.handleSessionList))
	mux.HandleFunc("DELETE /auth/sessions/{id}", a.requireUser(a.handleSessionRevoke))

	// Account administration. Owner only, and never an agent.
	mux.HandleFunc("POST /admin/users", a.requireOwner(a.handleAdminUserCreate))
	mux.HandleFunc("GET /admin/users", a.requireOwner(a.handleAdminUserList))
	mux.HandleFunc("POST /admin/users/{id}/invite", a.requireOwner(a.handleAdminUserInvite))
	mux.HandleFunc("DELETE /admin/users/{id}", a.requireOwner(a.handleAdminUserDelete))

	return requestLogger(d.Logger, mux)
}

// serverError logs the cause and answers with a generic message. The detail
// belongs in the log, not in a response body that may cross the WAN.
func (a *api) serverError(w http.ResponseWriter, r *http.Request, what string, err error) {
	a.logger.ErrorContext(r.Context(), "request failed", "op", what, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLogger emits one structured line per request. Health probes log at
// debug so a 10-second liveness check does not bury everything else at info.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		switch {
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
			level = slog.LevelDebug
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		// Datadog's standard log attributes, so these map without a custom
		// pipeline: http.method / http.status_code / http.url, and `duration`
		// in NANOSECONDS, which is what Datadog documents that field to be.
		// Dozzle shows the raw JSON, which stays readable either way.
		//
		// Note what is absent: no query string, no Authorization header, no
		// request body. A sign-in URL carries an invite in ?token=, and a log
		// line is exactly the wrong place for a live credential.
		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("http.method", r.Method),
			slog.String("http.url", r.URL.Path),
			slog.Int("http.status_code", rec.status),
			slog.Int64("duration", time.Since(start).Nanoseconds()),
		)
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
