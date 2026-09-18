// Package api is Chronicle's HTTP surface: the router every later endpoint
// hangs off, the two health probes the deploy path needs, and the credential
// surface from CHRN-71.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/estatewiki"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/upload"
	"github.com/Einlanzerous/chronicle/web"
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

	// ProxySecret is the value Traefik stamps on X-Chronicle-Proxy-Secret, and
	// the whole of the trust decision for X-Forwarded-For when keying the
	// sign-in limiter (CHRN-75). Empty means believe nobody, which makes every
	// request through Traefik share one bucket.
	//
	// It is not authentication. Nothing outside clientIP may consult it.
	ProxySecret string

	// Audio is the on-disk store of recordings (CHRN-23). Nil until
	// CHRONICLE_AUDIO_DIR is set, and GET /admin/storage then answers 503
	// naming the variable rather than reporting a corpus of zero -- "no audio
	// configured" and "no audio yet" are different facts and must not render
	// the same.
	Audio *audio.Store

	// Corpus is the database side of that report.
	Corpus Corpus

	// Transcription backs GET /admin/transcription (CHRN-27). Nil answers 503
	// rather than reporting an empty corpus, for the reason Audio does.
	Transcription Transcription

	// Transcribing reports whether a transcription pump is actually running.
	// It is on the report because an operator reading a large `pending` count
	// otherwise cannot tell a backlog from a Chronicle that was never pointed
	// at an ASR service -- and those want completely different remedies.
	Transcribing bool

	// Triage is CHRN-33's batch surface. Nil answers 503 naming the variables
	// that turn it on, rather than 404 — "not configured here" and "wrong URL"
	// are different facts, and this is the same shape Audio and Transcription
	// already use.
	Triage Triage

	// Uploads is the direct ingest path (CHRN-20). Nil when there is no audio
	// store, in which case the four /memos/uploads routes answer 503 naming
	// CHRONICLE_AUDIO_DIR -- the same shape the storage report uses, and for
	// the same reason: "not configured here" and "wrong URL" are different
	// facts and a client should be able to tell them apart.
	Uploads *upload.Service

	// References resolves Switchyard and Amber references into cards
	// (CHRN-104). setup() builds it from whichever transports the
	// configuration has, and builds it even when it has none: a Chronicle
	// with neither upstream still resolves its own notes and answers
	// `unconfigured` for the rest, which is a true thing to say. Nil only on
	// a router assembled without one, and POST /references/resolve then
	// answers 503 rather than dereferencing it inside a render.
	References References

	// LocalReferences answers Chronicle's own CHR- and DSC- references, which
	// never go through a Transport: there is no classifier for Chronicle's
	// namespace, and nothing to cache or to go stale.
	LocalReferences LocalReferences

	// Wiki is E5's store, reachable (CHRN-98): pages, notes, revisions and
	// search, and a note's backlinks (CHRN-105). Nil only on a router
	// assembled without one, and the nine routes then answer 503 rather than
	// dereferencing it.
	Wiki Wiki

	// Threads is E6's store, reachable (CHRN-99). Nil only on a router
	// assembled without one, and the nine routes then answer 503.
	Threads Threads

	// Memos is the recording behind a note (CHRN-107): the memo row, its
	// retention status and its transcript.
	//
	// WIRED LIKE Wiki AND NOT LIKE Corpus, and that is what scopes the 503.
	// setup() sets deps.Corpus inside `if cfg.AudioDir != ""`, because the
	// storage report is a report about a disk; deps.Wiki is unconditional,
	// because a note is a database read. These are database reads too:
	// provenance and the transcript must answer on a host with no audio
	// directory at all, and only GET /audio/{memo_id} touches Audio.
	Memos Memos

	// EstateWiki is the tier-1 corpus mount (CHRN-100): SERV-101's generated
	// wiki, read from files. serve refuses to boot without one; nil only on a
	// router assembled without it, and the two /tier1 routes then answer 503.
	EstateWiki *estatewiki.Corpus

	// Keys is the live Switchyard project key set — the renderer's predicate
	// for which KEY-N tokens are references, and the miss feed's destination.
	// Nil when no tracker is configured: the renderer then runs the pure path
	// (CHR, DSC and amber1 mark; every ticket-shaped token stays prose) and
	// there is nothing to report a miss to. A *resolve.Keys, not the
	// interface, so that a nil pointer cannot arrive here as a non-nil
	// interface and be dereferenced inside a render.
	Keys *resolve.Keys

	// Now is the clock a local resolution stamps its fetched_at from. Nil is
	// time.Now. Injectable so a test can hold the local half of a batch to
	// the same instant the resolver's own clock gives the upstream half — a
	// response should not carry two clocks.
	Now func() time.Time

	// Web is the embedded SPA handler (CHRN-53). Nil uses the production
	// bundle, web.Handler(). A test substitutes a small fs.FS-backed fixture
	// (web.HandlerFS) here so the router's WIRING — the redirect, the /app/
	// mount, the API keeping the root — is provable without a real bun
	// build, which the CI Go job (build, vet, test) deliberately does not
	// run: the compiled-in dist/ there is only the checked-in placeholder.
	Web http.Handler
}

// api holds what the handlers share.
//
// It implements wire.ServerInterface, which is the compile-time half of
// CHRN-97's drift guard: an operation added to openapi.yaml with no method here
// fails `go build`, and a method whose operation is deleted fails it too.
var _ wire.ServerInterface = (*api)(nil)

type api struct {
	accounts      Accounts
	db            Pinger
	version       string
	commit        string
	logger        *slog.Logger
	cfAccess      *CFAccessVerifier
	mobileBaseURL string
	secureCookies bool
	proxySecret   string
	mismatch      *mismatchWarner
	absent        *mismatchWarner
	proxySeen     *proxySeen
	signInLimiter *ipRateLimiter
	audio         *audio.Store
	corpus        Corpus
	uploads       *upload.Service
	transcription Transcription
	transcribing  bool
	triage        Triage
	references    References
	localRefs     LocalReferences
	now           func() time.Time
	wiki          Wiki
	threads       Threads
	memos         Memos
	estate        *estatewiki.Corpus
	keys          *resolve.Keys
	renderer      *markdown.Renderer
}

// NewRouter builds the HTTP handler.
//
// EVERY route is registered by the generator from openapi.yaml, onto a
// policyRouter that wraps each one with the credential policy.go declares for
// it. Nobody writes a registration here, which is what makes the route set
// unable to drift from the document; a route the document grows without a
// declared credential panics at construction rather than starting.
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
// two sign-in endpoints — which is now a property policy.go states and a test
// proves against the document, rather than one this comment asserts. Everything
// else requires a session; there is no unauthenticated read surface. /healthz
// staying dependency-free is load bearing beyond liveness: CHRN-59's QR
// onboarding probes it to check a server address before committing to it, which
// happens before any credential exists.
func NewRouter(d Deps) http.Handler {
	a := &api{
		accounts:      d.Accounts,
		db:            d.DB,
		version:       d.Version,
		commit:        d.Commit,
		logger:        d.Logger,
		cfAccess:      d.CFAccess,
		mobileBaseURL: d.MobileBaseURL,
		secureCookies: d.SecureCookies,
		proxySecret:   d.ProxySecret,
		mismatch:      &mismatchWarner{now: time.Now},
		absent:        &mismatchWarner{now: time.Now},
		proxySeen:     &proxySeen{},
		signInLimiter: newIPRateLimiter(signInRateWindow, signInRateBurst),
		audio:         d.Audio,
		corpus:        d.Corpus,
		uploads:       d.Uploads,
		transcription: d.Transcription,
		transcribing:  d.Transcribing,
		triage:        d.Triage,
		references:    d.References,
		localRefs:     d.LocalReferences,
		now:           d.Now,
		wiki:          d.Wiki,
		threads:       d.Threads,
		memos:         d.Memos,
		estate:        d.EstateWiki,
		keys:          d.Keys,
	}
	if a.now == nil {
		a.now = time.Now
	}
	// THE PREDICATE IS NIL WHEN THE KEY SET IS, and the two nils must not be
	// confused: a nil *resolve.Keys stored in a markdown.ProjectKeys is a
	// non-nil interface whose method dereferences nil on the first render.
	var predicate markdown.ProjectKeys
	if d.Keys != nil {
		predicate = d.Keys
	}
	a.renderer = newRenderer(predicate)

	mux := http.NewServeMux()

	// EVERY ROUTE IN openapi.yaml, REGISTERED BY THE GENERATOR (CHRN-97).
	//
	// Nobody writes these registrations, which is what makes the route set
	// unable to drift from the document. They land on a policyRouter rather
	// than on the mux directly, so each one is wrapped with the credential
	// policy.go declares for it -- and a route the document has and policy.go
	// does not is a panic here, at construction, rather than an open endpoint.
	//
	// ErrorHandlerFunc is supplied because its DEFAULT is
	// `http.Error(w, err.Error(), 400)`: text/plain, and leakier than the
	// handlers it would be answering for. See bindError.
	routed := newPolicyRouter(mux, a)
	wire.HandlerWithOptions(a, wire.StdHTTPServerOptions{
		BaseRouter:       routed,
		ErrorHandlerFunc: bindError(d.Logger),
	})

	// The web app (CHRN-53), mounted directly on the mux — NOT on routed, i.e.
	// outside policyRouter. Every route above carries a credential
	// policy.go declares for it (see "NOTHING IS HAND-REGISTERED ANY MORE"
	// below); this one is the deliberate exception, because it is not an
	// OPERATION in the sense that comment means. It serves files, takes no
	// credential and returns no API payload, so there is nothing for the
	// policy table to say about it. The API still owns the root — GET
	// /notes/{ref} is JSON — so the app's own client routes live under
	// /app/, and GET / only redirects there. Go 1.22's mux prefers the more
	// specific pattern, so a documented route under a different prefix is
	// never shadowed by this catch-all.
	webHandler := d.Web
	if webHandler == nil {
		webHandler = web.Handler()
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/app/", http.StatusFound)
	})
	mux.Handle("/app/", webHandler)

	if d.Accounts == nil {
		return requestLogger(d.Logger, mux)
	}

	// NOTHING IS HAND-REGISTERED ANY MORE, WITH ONE EXCEPTION ABOVE. Every
	// API route this service serves is in openapi.yaml, was registered above
	// by the generator, and carries the credential policy.go declares for it
	// — which is what CHRN-97 set out to make true and what the four guards
	// keep true. The SPA mount above is not a second source of hand-written
	// routes in that sense: it is a static catch-all, not an operation, and
	// CHRN-53's decision comment is where that distinction is written down.
	//
	// The mux is still built here rather than by wire.Handler so that
	// requestLogger wraps it, and so that a route added to the document
	// without a policy panics at construction rather than starting.

	return requestLogger(d.Logger, mux)
}

// GetHealthz answers the liveness probe. No dependencies, by design: a database
// that is down is not a reason to restart the binary, and CHRN-59's QR
// onboarding probes this before any credential exists.
func (a *api) GetHealthz(w http.ResponseWriter, r *http.Request) {
	body := wire.Health{Status: wire.Ok, Version: a.version}
	if a.commit != "" {
		body.Commit = &a.commit
	}
	writeJSON(w, http.StatusOK, body)
}

// GetReadyz answers the readiness probe. Pings the database, because a load
// balancer takes an unready instance out of rotation rather than killing it --
// which is the question liveness does not answer.
func (a *api) GetReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if a.db != nil {
		if err := a.db.Ping(ctx); err != nil {
			a.logger.Warn("readiness probe failed", "check", "database", "error", err)
			check := "database"
			writeJSON(w, http.StatusServiceUnavailable, wire.Readiness{
				Status: wire.Unready,
				Check:  &check,
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, wire.Readiness{Status: wire.Ready})
}

// serverError logs the cause and answers with a generic message. The detail
// belongs in the log, not in a response body that may cross the WAN.
func (a *api) serverError(w http.ResponseWriter, r *http.Request, what string, err error) {
	a.logger.ErrorContext(r.Context(), "request failed", "op", what, "error", err)
	writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
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
