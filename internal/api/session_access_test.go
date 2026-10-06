package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/apitest"
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/cfaccess/cfaccesstest"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-65 — the Access exchange tells a browser from the hosted MCP endpoint by
// the application the assertion was issued for, and admits only people.
//
// These run the real verifier against a key the test generated, because the
// thing under test is what happens AFTER an assertion verifies, and until
// CHRN-65 no test reached that far through the router.

const (
	testWebAUD = "web-application-tag"
	testMCPAUD = "mcp-application-tag"
)

// accessRouter serves the exchange for a verifier that accepts the web
// audience and, when mcpAudience is non-empty, that one as well -- the two
// shapes cmd/chronicle builds from config.
func accessRouter(t *testing.T, f *fakeAccounts, mcpAudience string) (http.Handler, *cfaccesstest.Issuer) {
	t.Helper()
	iss := cfaccesstest.New(t)
	auds := []string{testWebAUD}
	if mcpAudience != "" {
		auds = append(auds, mcpAudience)
	}
	return NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test", SecureCookies: true,
		CFAccess: iss.Verifier(auds...), MCPAudience: mcpAudience,
	}), iss
}

func exchange(h http.Handler, assertion string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := jsonReq(http.MethodPost, "/auth/sso/cloudflare", "")
	req.Header.Set("Cf-Access-Jwt-Assertion", assertion)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func agent(email string) store.User {
	return store.User{ID: uuid.New(), Email: email, DisplayName: email, Kind: store.KindAgent}
}

// The web application's assertion is a browser's, and gets what it always got.
func TestAccessExchangeForTheWebAudienceIsADurableSession(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("magos@example.com", true), "chr_unused")
	h, iss := accessRouter(t, f, testMCPAUD)

	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: testWebAUD}))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	apitest.Conform(t, "createSessionFromAccess", rec)

	var got wire.Session
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ExpiresAt != nil {
		t.Errorf("expires_at = %v; a browser's session does not expire", got.ExpiresAt)
	}
	if len(f.mintedSessions) != 1 || len(f.mintedHosted) != 0 {
		t.Errorf("minted durable=%v hosted=%v, want one durable session and no hosted one", f.mintedSessions, f.mintedHosted)
	}
	if rec.Header().Get("Set-Cookie") == "" {
		t.Error("no cookie set; a browser needs one for sub-resources")
	}
}

// The MCP application's assertion is one conversation: bounded, no cookie, and
// never the session the caller happened to present.
func TestAccessExchangeForTheMCPAudienceIsABoundedSession(t *testing.T) {
	f := newFakeAccounts()
	magos := f.signIn(person("magos@example.com", true), "chr_existing")
	h, iss := accessRouter(t, f, testMCPAUD)

	// Presenting a session of the same account's is what makes the web path
	// reuse rather than mint. It must not do that here.
	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: magos.Email, Audience: testMCPAUD}),
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
		t.Error("the presented session was reused; a conversation's token must be its own")
	}
	if len(f.mintedHosted) != 1 || len(f.mintedSessions) != 0 {
		t.Errorf("minted durable=%v hosted=%v, want one hosted session and no durable one", f.mintedSessions, f.mintedHosted)
	}
	if got.ExpiresAt == nil {
		t.Fatal("no expires_at; the transport cannot retire a session it cannot date")
	}
	if d := time.Until(*got.ExpiresAt); d < store.HostedSessionTTL-time.Minute || d > store.HostedSessionTTL+time.Minute {
		t.Errorf("expires in %v, want about %v", d, store.HostedSessionTTL)
	}
	if c := rec.Header().Get("Set-Cookie"); c != "" {
		t.Errorf("Set-Cookie = %q; the caller is a server-side process, not a browser", c)
	}
}

// With no MCP application configured the verifier was never told its tag, so
// its assertions are refused like any other unknown audience -- the state every
// deployment is in until the variable is set.
func TestAccessExchangeRefusesTheMCPAudienceWhenNoneIsConfigured(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("magos@example.com", true), "chr_unused")
	h, iss := accessRouter(t, f, "")

	rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: testMCPAUD}))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	apitest.Conform(t, "createSessionFromAccess", rec)
	if len(f.mintedSessions)+len(f.mintedHosted) != 0 {
		t.Errorf("minted durable=%v hosted=%v for a refused assertion", f.mintedSessions, f.mintedHosted)
	}
}

// Only a person comes in through Access, whichever application vouched.
func TestAccessExchangeRefusesAnAgentAccount(t *testing.T) {
	for name, aud := range map[string]string{"web audience": testWebAUD, "MCP audience": testMCPAUD} {
		t.Run(name, func(t *testing.T) {
			f := newFakeAccounts()
			f.signIn(agent("bot@example.com"), "chr_unused")
			h, iss := accessRouter(t, f, testMCPAUD)

			rec := exchange(h, iss.Sign(t, cfaccesstest.Claims{Email: "bot@example.com", Audience: aud}))
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body)
			}
			apitest.Conform(t, "createSessionFromAccess", rec)

			var body wire.SsoError
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Code != "sso_agent_account" {
				t.Errorf("code = %q, want sso_agent_account", body.Code)
			}
			if len(f.mintedSessions)+len(f.mintedHosted) != 0 {
				t.Errorf("minted durable=%v hosted=%v for an agent", f.mintedSessions, f.mintedHosted)
			}
			if c := rec.Header().Get("Set-Cookie"); c != "" {
				t.Errorf("Set-Cookie = %q on a refusal", c)
			}
		})
	}
}
