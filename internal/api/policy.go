package api

import (
	"fmt"
	"net/http"
)

// The credential every route requires, declared once and enforced at
// construction (CHRN-97 ruling 1).
//
// ============================================================================
// WHY THIS EXISTS AT ALL
// ============================================================================
//
// Route registration is GENERATED from openapi.yaml, and oapi-codegen's
// std-http-server has one global middleware list with no per-operation hook --
// while this API has four different credential requirements across its routes.
// The seam is the BaseRouter: the generated code calls HandleFunc(pattern, h)
// on whatever ServeMux-shaped value it is handed, so this package hands it one
// that looks the pattern up here and wraps the handler before registering it.
//
// The wrap happens BEFORE registration, so the credential check runs ahead of
// the generated parameter binding. A 401 never costs a UUID parse, and a
// malformed id on a route you may not call answers 401 rather than telling you
// the id was malformed.
//
// ============================================================================
// A ROUTE WITH NO ENTRY HERE PANICS AT CONSTRUCTION.
// ============================================================================
//
// Not a 403, not a log line: the binary does not start. NewRouter runs at boot
// and in every router test, so an operation added to the document without a
// declared credential cannot reach production and cannot pass CI. Fail-closed
// in the shape CHRN-52 already uses for the tier-1 DSN -- a boundary that is
// merely usually enforced is not one.
//
// ============================================================================
// AND IT IS NOT A SECOND SOURCE OF TRUTH.
// ============================================================================
//
// Every operation in openapi.yaml carries `x-chronicle-policy`, and
// TestPolicyTableMatchesTheDocument compares this map against it operation by
// operation. The document is where a reviewer reads who may call what, and a
// widening is a diff there -- which is the whole point, because a widening
// hidden in Go is a widening nobody reviews.
type policy string

const (
	// policyPublic -- no credential. Exactly the two probes.
	policyPublic policy = "public"

	// policySignIn -- no credential, but rate-limited: these mint one. Both
	// unauthenticated minting endpoints are limited, because limiting one of
	// them just moves the target.
	policySignIn policy = "signin"

	// policyMember -- any account. The author is taken from the session and
	// never from anything the request carries.
	policyMember policy = "member"

	// policyOwner -- the owner account, and never an agent.
	policyOwner policy = "owner"
)

// routePolicy is keyed by the exact pattern the generated registration emits:
// "METHOD /path". Adding a route to openapi.yaml without adding it here is a
// panic at construction; adding it here without the document is a test failure.
var routePolicy = map[string]policy{
	"GET /healthz": policyPublic,
	"GET /readyz":  policyPublic,

	// The two unauthenticated endpoints that MINT a credential. Limiting only
	// one of them just moves the target: /auth/sso/cloudflare drives a JWKS
	// fetch and a database write per call.
	"POST /auth/session":        policySignIn,
	"POST /auth/sso/cloudflare": policySignIn,

	"DELETE /auth/session":       policyMember,
	"GET /auth/me":               policyMember,
	"PATCH /auth/me":             policyMember,
	"POST /auth/invite":          policyMember,
	"GET /auth/sessions":         policyMember,
	"DELETE /auth/sessions/{id}": policyMember,

	// Account administration. Owner only, and never an agent.
	"POST /admin/users":             policyOwner,
	"GET /admin/users":              policyOwner,
	"POST /admin/users/{id}/invite": policyOwner,
	"DELETE /admin/users/{id}":      policyOwner,

	// Memo ingest (CHRN-20). requireUser and not requireOwner: every account
	// records its own memos, and the author comes from the session rather than
	//anything the request carries.
	//
	// Deliberately NOT policySignIn. That bucket exists for unauthenticated
	// endpoints that mint a credential; a 20-per-minute cap on a chunked upload
	// would throttle ingest and nothing else. What bounds this surface instead
	// is the per-account cap on open sessions and the declared-size limit, both
	// in internal/upload.
	"POST /memos/uploads":        policyMember,
	"GET /memos/uploads/{id}":    policyMember,
	"PATCH /memos/uploads/{id}":  policyMember,
	"DELETE /memos/uploads/{id}": policyMember,

	// TRIAGE (CHRN-33) — the one place derived state becomes authored state.
	//
	// requireUser and not requireOwner: every account triages its own memos.
	// The author scoping is applied inside the service, PER ITEM on the POST as
	// well as on the GET, because a list that merely hides a memo is not access
	// control — a client naming an id directly never went through the list.
	"GET /triage/batch":    policyMember,
	"POST /triage/accept":  policyMember,
	"POST /triage/hold":    policyMember,
	"POST /triage/release": policyMember,
	"GET /triage/deferred": policyMember,

	"GET /admin/storage":       policyOwner,
	"GET /admin/transcription": policyOwner,
	// Spans every author's corpus, so owner rather than member.
	"GET /admin/triage": policyOwner,
}

// policyRouter is the ServeMux the generated registration writes into.
//
// It satisfies wire.ServeMux (HandleFunc + ServeHTTP) and delegates to a real
// *http.ServeMux. Since CHRN-97's third PR there is no second way in: every
// route this service serves is registered through here, so there is no longer a
// path by which one can be added without a credential.
type policyRouter struct {
	mux *http.ServeMux
	api *api

	// seen is what was actually registered through here, so a test can assert
	// the generated route set rather than trusting this file's map to describe
	// it.
	seen map[string]policy
}

func newPolicyRouter(mux *http.ServeMux, a *api) *policyRouter {
	return &policyRouter{mux: mux, api: a, seen: map[string]policy{}}
}

// HandleFunc wraps by declared policy, then registers.
func (p *policyRouter) HandleFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	pol, ok := routePolicy[pattern]
	if !ok {
		// The document grew a route and nobody said who may call it. Refusing
		// to start is the only answer that cannot be ignored: a default of
		// "public" ships an open route, and a default of "owner" ships a route
		// nobody can reach and that somebody then widens in a hurry.
		panic(fmt.Sprintf(
			"api: %q is in openapi.yaml but has no entry in routePolicy. "+
				"Every route declares its credential in both places; see internal/api/policy.go.",
			pattern))
	}

	p.seen[pattern] = pol
	p.mux.HandleFunc(pattern, p.wrap(pol, h))
}

func (p *policyRouter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mux.ServeHTTP(w, r)
}

// wrap applies the policy's middleware.
func (p *policyRouter) wrap(pol policy, h http.HandlerFunc) http.HandlerFunc {
	switch pol {
	case policyPublic:
		return h
	case policySignIn:
		return p.api.limitSignIn(h)
	case policyMember:
		return p.guarded(p.api.requireUser, h)
	case policyOwner:
		return p.guarded(p.api.requireOwner, h)
	default:
		// Unreachable while policy is a closed set, and a panic rather than a
		// fallthrough for the reason above: there is no safe default.
		panic(fmt.Sprintf("api: unknown policy %q", pol))
	}
}

// guarded applies a credential wrapper, or refuses the route when there is no
// credential surface to apply it with.
//
// Deps.Accounts is nil only in a test that builds a probes-only router; setup()
// always supplies it. Before the generated registration, such a router simply
// had no /admin routes and they answered 404. Now every operation in the
// document is registered, so the honest answer is 503 naming the reason -- the
// same shape an absent audio store and an absent transcription store already
// use, and for the same stated reason: "not configured here" and "wrong URL"
// are different facts.
//
// What it must never be is unguarded. Registering the bare handler when there
// is nothing to check the credential with would serve owner-only reports to
// anybody, which is why this returns a refusal rather than skipping the wrap.
func (p *policyRouter) guarded(wrapper func(http.HandlerFunc) http.HandlerFunc, h http.HandlerFunc) http.HandlerFunc {
	if p.api.accounts == nil {
		return func(w http.ResponseWriter, r *http.Request) {
			writeError(w, http.StatusServiceUnavailable, codeAccountsUnconfigured,
				"this deployment has no credential surface, so no authenticated route can be served")
		}
	}
	return wrapper(h)
}
