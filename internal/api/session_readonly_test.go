package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/cfaccess/cfaccesstest"
)

// SERV-307 (plan SERV-293, Row 2): a read-only credential is refused by the API
// itself on every route outside readOnlyRoutes. Until this, "a hosted session
// cannot write" was a property of which tools the MCP server offered.

const readOnlyToken = "chr_readonly_hosted"

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// readOnlyWorld has an owner holding one read-only token and one ordinary
// session, so a refusal can be told from "nobody was signed in".
func readOnlyWorld(t *testing.T) (http.Handler, *fakeAccounts) {
	t.Helper()
	f := newFakeAccounts()
	owner := person("magos@example.com", true)
	ro := owner
	ro.SessionReadOnly = true
	f.signIn(ro, readOnlyToken)
	f.signIn(owner, "chr_ordinary")
	return testRouter(f), f
}

func call(h http.Handler, token, method, pattern, body string) *httptest.ResponseRecorder {
	path := pathParam.ReplaceAllString(pattern, "00000000-0000-0000-0000-000000000001")
	req := jsonReq(method, path, body)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func splitPattern(p string) (string, string) {
	m, path, _ := strings.Cut(p, " ")
	return m, path
}

func requireReadOnlyRefusal(t *testing.T, pattern string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Fatalf("%s: status = %d, want 403 for a read-only token: %s", pattern, rec.Code, rec.Body)
	}
	var e wire.Error
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("%s: decode: %v", pattern, err)
	}
	if e.Code != codeReadOnlySession {
		t.Fatalf("%s: code = %q, want %q", pattern, e.Code, codeReadOnlySession)
	}
}

// The sweep iterates routePolicy, so a route added to the API is covered with no
// edit here: a credentialed route outside readOnlyRoutes must refuse.
func TestAReadOnlyTokenIsRefusedOnEveryCredentialedRouteOutsideTheAllowlist(t *testing.T) {
	h, _ := readOnlyWorld(t)
	ops := map[string]string{}
	for path, item := range apitest.Doc(t).Paths.Map() {
		for method, op := range item.Operations() {
			ops[method+" "+path] = op.OperationID
		}
	}

	swept := 0
	for pattern, pol := range routePolicy {
		if pol != policyMember && pol != policyOwner {
			continue
		}
		if readOnlyRoutes[pattern] {
			continue
		}
		swept++
		t.Run(pattern, func(t *testing.T) {
			method, _ := splitPattern(pattern)
			rec := call(h, readOnlyToken, method, strings.TrimPrefix(pattern, method+" "), `{}`)
			requireReadOnlyRefusal(t, pattern, rec)
			apitest.Conform(t, ops[pattern], rec)
		})
	}
	if swept == 0 {
		t.Fatal("the sweep covered no routes, so it proves nothing")
	}
}

// The six the plan names, spelled out so they cannot silently drop out of the
// sweep (for instance by being added to readOnlyRoutes).
func TestTheNamedWritesAreRefusedToAReadOnlyToken(t *testing.T) {
	h, _ := readOnlyWorld(t)
	for _, tc := range []struct{ pattern, body string }{
		{"POST /auth/invite", ``},
		{"POST /pages", `{"title":"x"}`},
		{"POST /notes", `{"title":"x","body":"y"}`},
		{"POST /discussions/{ref}/turns", `{"body":"hi"}`},
		{"POST /memos/uploads", `{}`},
		{"PATCH /auth/me", `{"display_name":"x"}`},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			if readOnlyRoutes[tc.pattern] {
				t.Fatalf("%s is on the read-only allowlist", tc.pattern)
			}
			method, path := splitPattern(tc.pattern)
			requireReadOnlyRefusal(t, tc.pattern, call(h, readOnlyToken, method, path, tc.body))
		})
	}
}

// The same routes, with an ordinary session: the refusal is the token's, not the
// route's. Cookie and invite-redeemed sessions are not read-only.
func TestAnOrdinarySessionIsNotRefusedOnTheSameRoutes(t *testing.T) {
	h, _ := readOnlyWorld(t)
	for _, pattern := range []string{"POST /auth/invite", "PATCH /auth/me", "POST /pages", "POST /memos/uploads"} {
		method, path := splitPattern(pattern)
		rec := call(h, "chr_ordinary", method, path, `{"display_name":"x","title":"x"}`)
		var e wire.Error
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Code == codeReadOnlySession {
			t.Errorf("%s: an ordinary session was refused as read-only", pattern)
		}
	}
}

// The allowlist is a subset of the credentialed routes, and every entry is
// spelled the way the router registers it.
func TestTheReadOnlyAllowlistNamesRealCredentialedRoutes(t *testing.T) {
	for pattern := range readOnlyRoutes {
		pol, ok := routePolicy[pattern]
		if !ok {
			t.Errorf("%s is on the read-only allowlist but is not a route", pattern)
			continue
		}
		if pol != policyMember && pol != policyOwner {
			t.Errorf("%s is on the read-only allowlist but is %q, which carries no credential to be read-only", pattern, pol)
		}
	}
}

// What a read-only token IS for: the routes the MCP read tools call, and the
// self-revoke. Not refused as read-only (they may answer 503 here, where the
// stores are not wired, which is a different fact).
func TestAReadOnlyTokenReachesTheAllowlistedRoutes(t *testing.T) {
	h, _ := readOnlyWorld(t)
	for pattern := range readOnlyRoutes {
		method, path := splitPattern(pattern)
		rec := call(h, readOnlyToken, method, path, ``)
		var e wire.Error
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		if e.Code == codeReadOnlySession {
			t.Errorf("%s: refused a read-only token though it is on the allowlist", pattern)
		}
	}
}

func TestAReadOnlyTokenCanStillRevokeItself(t *testing.T) {
	h, f := readOnlyWorld(t)
	rec := call(h, readOnlyToken, "DELETE", "/auth/session", ``)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body)
	}
	if len(f.revoked) != 1 || f.revoked[0] != readOnlyToken {
		t.Errorf("revoked = %v, want the read-only token", f.revoked)
	}
	if rec := call(h, readOnlyToken, "GET", "/auth/me", ``); rec.Code != http.StatusUnauthorized {
		t.Errorf("after revoke GET /auth/me = %d, want 401", rec.Code)
	}
}

func TestAReadOnlyTokenCanReadItsOwnAccount(t *testing.T) {
	h, _ := readOnlyWorld(t)
	rec := call(h, readOnlyToken, "GET", "/auth/me", ``)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
}

// Every hosted exchange asks the store for a hosted mint, which is the one
// place read_only is set. The browser's sign-in asks for a durable one.
func TestEveryHostedAudienceMintsTheReadOnlyKind(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("magos@example.com", true), "chr_unused")
	h, iss := accessRouter(t, f, testMCPAUD, testGatewayAUD)
	for _, aud := range []string{testMCPAUD, testGatewayAUD} {
		rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: aud}))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d: %s", aud, rec.Code, rec.Body)
		}
	}
	if len(f.mintedHosted) != 2 || len(f.mintedSessions) != 0 {
		t.Errorf("hosted=%v durable=%v, want two hosted mints and no durable one", f.mintedHosted, f.mintedSessions)
	}
	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: testWebAUD}))
	if rec.Code != http.StatusOK {
		t.Fatalf("web: status = %d: %s", rec.Code, rec.Body)
	}
	if len(f.mintedHosted) != 2 || len(f.mintedSessions) != 1 {
		t.Errorf("after a browser sign-in hosted=%v durable=%v, want it to add a durable one only", f.mintedHosted, f.mintedSessions)
	}
}
