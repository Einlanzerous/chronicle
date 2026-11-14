package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/cfaccess/cfaccesstest"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// SERV-266 — the estate MCP gateway forwards its callers' Access assertions
// unchanged (ruling 4 of SERV-257's plan), so what reaches the exchange carries
// the GATEWAY's audience. It is an MCP-only audience: it gets what the MCP
// endpoint's gets, and nothing a browser gets.

const testGatewayAUD = "gateway-application-tag"

// A gateway assertion is one MCP conversation, exactly as the MCP endpoint's
// is: bounded, no cookie, and never the session the caller happened to present.
func TestAccessExchangeForTheGatewayAudienceIsABoundedSession(t *testing.T) {
	f := newFakeAccounts()
	magos := f.signIn(person("magos@example.com", true), "chr_existing")
	h, iss := accessRouter(t, f, testMCPAUD, testGatewayAUD)

	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: magos.Email, Audience: testGatewayAUD}),
		&http.Cookie{Name: sessionCookie, Value: "chr_existing"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	apitest.Conform(t, "createSessionFromAccess", rec)

	var got wire.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionToken == "chr_existing" {
		t.Error("the presented browser session was reused for a gateway assertion")
	}
	if len(f.mintedHosted) != 1 || len(f.mintedSessions) != 0 {
		t.Errorf("minted durable=%v hosted=%v, want one hosted session and no durable one", f.mintedSessions, f.mintedHosted)
	}
	if got.ExpiresAt == nil {
		t.Fatal("no expires_at; a gateway assertion must never be given a non-expiring session")
	}
	if d := time.Until(*got.ExpiresAt); d > store.HostedSessionTTL+time.Minute {
		t.Errorf("expires in %v, longer than a hosted session", d)
	}
	if c := rec.Header().Get("Set-Cookie"); c != "" {
		t.Errorf("Set-Cookie = %q; a gateway assertion is never a browser", c)
	}
}

// The gateway audience works without the MCP endpoint's being configured: the
// two are separate applications and either may exist alone.
func TestAccessExchangeAcceptsTheGatewayAudienceAlone(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("magos@example.com", true), "chr_unused")
	h, iss := accessRouter(t, f, testGatewayAUD)

	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: testGatewayAUD}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if len(f.mintedHosted) != 1 || len(f.mintedSessions) != 0 {
		t.Errorf("minted durable=%v hosted=%v, want one hosted session", f.mintedSessions, f.mintedHosted)
	}
	if c := rec.Header().Get("Set-Cookie"); c != "" {
		t.Errorf("Set-Cookie = %q for a gateway assertion", c)
	}
}

// Configuring the gateway changes nothing for a browser.
func TestAccessExchangeForTheWebAudienceIsUnchangedByTheGateway(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("magos@example.com", true), "chr_unused")
	h, iss := accessRouter(t, f, testMCPAUD, testGatewayAUD)

	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: testWebAUD}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if len(f.mintedSessions) != 1 || len(f.mintedHosted) != 0 {
		t.Errorf("minted durable=%v hosted=%v, want one durable session", f.mintedSessions, f.mintedHosted)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("the browser lost its cookie once a gateway audience was configured")
	}
}

// Every way a gateway-shaped assertion can be wrong is a 401 that mints
// nothing: the knob unset (the default), an audience nobody configured, an
// expired assertion, another team's issuer, and a key the team does not publish.
func TestAccessExchangeRefusesBadGatewayAssertions(t *testing.T) {
	good := cfaccesstest.Claims{Email: "magos@example.com", Audience: testGatewayAUD}
	with := func(f func(*cfaccesstest.Claims)) cfaccesstest.Claims { c := good; f(&c); return c }

	cases := []struct {
		name       string
		configured []string // MCP-only audiences the server is configured with
		claims     cfaccesstest.Claims
		unknownKey bool
	}{
		{name: "gateway audience not configured", configured: []string{testMCPAUD}, claims: good},
		{name: "nothing MCP-only configured", configured: nil, claims: good},
		{name: "an unknown audience", configured: []string{testMCPAUD, testGatewayAUD},
			claims: with(func(c *cfaccesstest.Claims) { c.Audience = "some-other-application" })},
		{name: "expired", configured: []string{testMCPAUD, testGatewayAUD},
			claims: with(func(c *cfaccesstest.Claims) { c.Expires = -time.Minute })},
		{name: "wrong issuer", configured: []string{testMCPAUD, testGatewayAUD},
			claims: with(func(c *cfaccesstest.Claims) { c.Issuer = "https://elsewhere.cloudflareaccess.com" })},
		{name: "unpublished key", configured: []string{testMCPAUD, testGatewayAUD}, claims: good, unknownKey: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAccounts()
			f.signIn(person("magos@example.com", true), "chr_unused")
			h, iss := accessRouter(t, f, tc.configured...)

			assertion := iss.Sign(t, tc.claims)
			if tc.unknownKey {
				assertion = iss.SignWithUnknownKey(t, tc.claims)
			}
			rec := exchange(h, assertion)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
			apitest.Conform(t, "createSessionFromAccess", rec)
			if len(f.mintedSessions)+len(f.mintedHosted) != 0 {
				t.Errorf("minted durable=%v hosted=%v for a refused assertion", f.mintedSessions, f.mintedHosted)
			}
			if c := rec.Header().Get("Set-Cookie"); c != "" {
				t.Errorf("Set-Cookie = %q on a refusal", c)
			}
		})
	}
}

// MCP-only means MCP-only. A gateway assertion is something the sign-in
// exchange may trade; on every other route it is not a credential at all, so
// presenting one where a session belongs is the same 401 as presenting
// nothing. The routes are the session-authenticated ones a browser uses.
func TestGatewayAssertionIsNotACredentialOnSessionRoutes(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("magos@example.com", true), "chr_unused")
	h, iss := accessRouter(t, f, testMCPAUD, testGatewayAUD)
	assertion := iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: testGatewayAUD})

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/auth/me"},
		{http.MethodGet, "/auth/sessions"},
		{http.MethodGet, "/discussions"},
		{http.MethodGet, "/notes"},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.Header.Set("Cf-Access-Jwt-Assertion", assertion)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: a gateway assertion opened a session route", rec.Code)
			}
			if len(f.mintedSessions)+len(f.mintedHosted) != 0 {
				t.Errorf("minted durable=%v hosted=%v on a session route", f.mintedSessions, f.mintedHosted)
			}
		})
	}
}
