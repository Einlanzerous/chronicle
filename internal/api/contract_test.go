package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
)

// CHRN-97's guards, tested where they can actually fail.
//
// Four of the five are structural and need no test to hold -- the byte-compare
// in verify.sh, the ServerInterface assertion in router.go, the generated
// registration, the construction-time panic. What they need is a test that
// proves the STRUCTURE is still wired up: that the panic can fire, that the
// table and the document cannot disagree, and that the responses are the ones
// the document describes.

// The document declares the credential in x-chronicle-policy, and policy.go
// declares it in Go. This is the test that makes them one fact instead of two.
//
// It fails in both directions on purpose. A document operation missing from the
// table is a route that would panic at boot -- better to learn it here. A table
// entry with no operation is a route the generator will never register, so the
// wrap it names is dead code that reads like a live credential.
func TestPolicyTableMatchesTheDocument(t *testing.T) {
	documented := map[string]policy{}

	for path, item := range apitest.Doc(t).Paths.Map() {
		for method, op := range item.Operations() {
			pattern := method + " " + path

			raw, ok := op.Extensions["x-chronicle-policy"]
			if !ok {
				t.Errorf("%s (%s) declares no x-chronicle-policy in openapi.yaml", pattern, op.OperationID)
				continue
			}
			name, ok := raw.(string)
			if !ok {
				t.Errorf("%s: x-chronicle-policy is %T, want a string", pattern, raw)
				continue
			}
			documented[pattern] = policy(name)

			switch got, ok := routePolicy[pattern]; {
			case !ok:
				t.Errorf("%s is in openapi.yaml but not in routePolicy — it would panic at construction", pattern)
			case got != policy(name):
				t.Errorf("%s: openapi.yaml says %q, routePolicy says %q", pattern, name, got)
			}
		}
	}

	for pattern := range routePolicy {
		if _, ok := documented[pattern]; !ok {
			t.Errorf("%s is in routePolicy but in no operation in openapi.yaml", pattern)
		}
	}
}

// The document says who may call an operation twice over: `security: []` means
// no credential at all, and x-chronicle-policy says which credential otherwise.
// They have to agree, or the document contradicts itself and the generated
// clients believe the half this package does not read.
func TestSecurityAndPolicyAgreeOnWhatIsPublic(t *testing.T) {
	for path, item := range apitest.Doc(t).Paths.Map() {
		for method, op := range item.Operations() {
			pattern := method + " " + path

			// A nil Security inherits the document's top-level requirement; an
			// empty one overrides it to "no credential".
			public := op.Security != nil && len(*op.Security) == 0
			pol, _ := op.Extensions["x-chronicle-policy"].(string)

			// signin is credential-free too, and deliberately a different
			// policy: it is rate-limited because it MINTS a credential.
			credentialFree := pol == string(policyPublic) || pol == string(policySignIn)

			if public != credentialFree {
				t.Errorf("%s: security declares public=%v but x-chronicle-policy is %q",
					pattern, public, pol)
			}
		}
	}
}

// A route the document grows without a declared credential must stop the
// binary, not default to something. This is that panic, fired on purpose.
//
// Tested through the shim rather than by mutating the document, because the
// failure is a property of the shim: it is what happens on the next operation
// anybody adds.
func TestAnUndeclaredRoutePanicsAtConstruction(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering a route with no policy did not panic; " +
				"an undeclared route would reach production with no credential")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "routePolicy") {
			t.Errorf("panic did not name routePolicy, so nobody will know what to fix: %v", r)
		}
	}()

	routed := newPolicyRouter(http.NewServeMux(), &api{})
	routed.HandleFunc("GET /a-route-nobody-declared", func(http.ResponseWriter, *http.Request) {})
}

// Every route this API serves, and what an anonymous caller gets from it.
//
// THE EXPECTATIONS ARE WRITTEN OUT, NOT DERIVED. Deriving them from routePolicy
// would make this test agree with the table by construction and prove nothing;
// these are the statuses the surface answered before CHRN-97 moved any of it,
// enumerated route by route. The plan's inventory table is this list.
//
// It covers the routes the generator now registers AND the ones still
// hand-registered, which is the point: the second half of this ticket moves
// them, and this is what says the move changed nothing.
func TestAnonymousGetsTheSameAnswerFromEveryRoute(t *testing.T) {
	h := testRouter(newFakeAccounts())

	// 401: a session is required. 429 is not reachable anonymously here
	// (the sign-in limiter allows a burst), so the two sign-in routes answer
	// on their own merits and are listed with what they actually return.
	cases := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/readyz", http.StatusOK},

		{http.MethodDelete, "/auth/session", http.StatusUnauthorized},
		{http.MethodGet, "/auth/me", http.StatusUnauthorized},
		{http.MethodPatch, "/auth/me", http.StatusUnauthorized},
		{http.MethodPost, "/auth/invite", http.StatusUnauthorized},
		{http.MethodGet, "/auth/sessions", http.StatusUnauthorized},
		{http.MethodDelete, "/auth/sessions/" + someUUID, http.StatusUnauthorized},

		{http.MethodPost, "/admin/users", http.StatusUnauthorized},
		{http.MethodGet, "/admin/users", http.StatusUnauthorized},
		{http.MethodPost, "/admin/users/" + someUUID + "/invite", http.StatusUnauthorized},
		{http.MethodDelete, "/admin/users/" + someUUID, http.StatusUnauthorized},
		{http.MethodGet, "/admin/storage", http.StatusUnauthorized},
		{http.MethodGet, "/admin/triage", http.StatusUnauthorized},
		{http.MethodGet, "/admin/transcription", http.StatusUnauthorized},

		{http.MethodPost, "/memos/uploads", http.StatusUnauthorized},
		{http.MethodGet, "/memos/uploads/" + someUUID, http.StatusUnauthorized},
		{http.MethodPatch, "/memos/uploads/" + someUUID, http.StatusUnauthorized},
		{http.MethodDelete, "/memos/uploads/" + someUUID, http.StatusUnauthorized},

		{http.MethodGet, "/triage/batch", http.StatusUnauthorized},
		{http.MethodPost, "/triage/accept", http.StatusUnauthorized},
		{http.MethodPost, "/triage/hold", http.StatusUnauthorized},
		{http.MethodPost, "/triage/release", http.StatusUnauthorized},
		{http.MethodGet, "/triage/deferred", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}

	// The two sign-in routes are credential-free by design, and the only two
	// that are besides the probes.
	//
	// The claim is NOT "they do not answer 401" -- a sign-in with no invite
	// legitimately does, and that is the handler doing its job. The claim is
	// that the request REACHED the handler: requireUser stamps
	// WWW-Authenticate on its refusal and the sign-in handlers never do, so an
	// empty header is the observable that separates "your credential was
	// rejected" from "you needed a credential to try".
	for _, path := range []string{"/auth/session", "/auth/sso/cloudflare"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(http.MethodPost, path, `{}`))
		if got := rec.Header().Get("WWW-Authenticate"); got != "" {
			t.Errorf("POST %s was refused by the credential wrapper (WWW-Authenticate: %q): "+
				"signing in now requires a session", path, got)
		}
	}

	// And the route set is closed. A path nobody declared is a 404, which is
	// what says the generated registration added no surface of its own.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/notes/CHR-0311", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /notes/CHR-0311 = %d, want 404 — that route belongs to CHRN-98", rec.Code)
	}
}

// The owner-only routes answer 403 to an ordinary member, not 200 and not 404.
func TestAMemberIsRefusedTheOwnerRoutes(t *testing.T) {
	f := newFakeAccounts()
	member := person("member@example.com", false)
	f.sessions["member-token"] = member
	h := testRouter(f)

	owned := []struct{ method, path string }{
		{http.MethodPost, "/admin/users"},
		{http.MethodGet, "/admin/users"},
		{http.MethodPost, "/admin/users/" + someUUID + "/invite"},
		{http.MethodDelete, "/admin/users/" + someUUID},
		{http.MethodGet, "/admin/storage"},
		{http.MethodGet, "/admin/triage"},
		{http.MethodGet, "/admin/transcription"},
	}

	for _, route := range owned {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(route.method, route.path, nil)
		r.Header.Set("Authorization", "Bearer member-token")
		h.ServeHTTP(rec, r)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a member = %d, want 403", route.method, route.path, rec.Code)
		}
	}
}

// The refusals the shared wrappers produce are responses like any other, and
// the document describes them. Conform is what says so.
func TestRefusalsConformToTheDocument(t *testing.T) {
	f := newFakeAccounts()
	f.sessions["member-token"] = person("member@example.com", false)
	h := testRouter(f)

	for _, tc := range []struct {
		name, operationID, token string
		want                     int
	}{
		{"anonymous", "getStorageReport", "", http.StatusUnauthorized},
		{"member", "getStorageReport", "member-token", http.StatusForbidden},
		{"anonymous", "getTranscriptionReport", "", http.StatusUnauthorized},
		{"member", "getTranscriptionReport", "member-token", http.StatusForbidden},
	} {
		t.Run(tc.operationID+"/"+tc.name, func(t *testing.T) {
			path := "/admin/storage"
			if tc.operationID == "getTranscriptionReport" {
				path = "/admin/transcription"
			}
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, path, nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			h.ServeHTTP(rec, r)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			apitest.Conform(t, tc.operationID, rec)

			var body wire.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not the documented envelope: %v", err)
			}
			if body.Code == "" {
				t.Error("the envelope carries no code, so a client has nothing to branch on")
			}
		})
	}
}

// The probes conform too, including the unready branch -- which is the one a
// deploy actually reads.
func TestProbesConformToTheDocument(t *testing.T) {
	h := testRouter(newFakeAccounts())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	apitest.Conform(t, "getHealthz", rec)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	apitest.Conform(t, "getReadyz", rec)

	// Unready: the probe answers 503 naming the check, and that shape is
	// declared. A deploy that cannot tell ready from unready is worse than one
	// with no probe.
	down := NewRouter(Deps{
		DB:       fakePinger{err: errors.New("db is down")},
		Accounts: newFakeAccounts(),
		Logger:   discardLogger(),
		Version:  "test",
	})
	rec = httptest.NewRecorder()
	down.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz with a dead database = %d, want 503", rec.Code)
	}
	apitest.Conform(t, "getReadyz", rec)
}

// bindError is the generated wrapper's error handler, and the reason it is
// supplied at all: its default answers text/plain with the parser's own words.
//
// Nothing in this half of the ticket binds a parameter -- the four migrated
// operations take none -- so this exercises it directly. The second half brings
// the {id} routes, and Conform covers a malformed one per operation there.
func TestBindErrorAnswersTheDocumentedEnvelopeAndLeaksNothing(t *testing.T) {
	handle := bindError(discardLogger())

	leaky := &wire.InvalidParamFormatError{
		ParamName: "id",
		Err:       errors.New("invalid UUID length: 5"),
	}

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"malformed", leaky},
		{"missing", &wire.RequiredParamError{ParamName: "id"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handle(rec, httptest.NewRequest(http.MethodGet, "/admin/storage", nil), tc.err)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want application/json — the default answers text/plain", ct)
			}

			var body wire.Error
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not the documented envelope: %v", err)
			}
			if body.Code != codeInvalidParameter {
				t.Errorf("code = %q, want %q", body.Code, codeInvalidParameter)
			}

			// The regression this replaces: pathUUID answers a flat message
			// "rather than leaking the shape of the id space through a lookup",
			// while the generated text says "invalid UUID length: 5".
			if strings.Contains(body.Message, "UUID") || strings.Contains(body.Message, "length") {
				t.Errorf("message %q relays the parser's own words", body.Message)
			}
		})
	}
}

// Not a guard so much as a readable record: what the document currently
// describes. A route joining it without a policy fails the test above; this one
// makes the set itself visible in a diff.
func TestDocumentedOperations(t *testing.T) {
	var got []string
	for name := range apitest.Operations(t) {
		got = append(got, name)
	}
	sort.Strings(got)

	want := []string{"getHealthz", "getReadyz", "getStorageReport", "getTranscriptionReport"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("operations = %v, want %v.\nIf this is the second half of CHRN-97 landing routes, update the list.", got, want)
	}
}

const someUUID = "6f1b2c3d-4e5f-4a7b-8c9d-0e1f2a3b4c5d"
