package mcp_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/cfaccess/cfaccesstest"
)

// SERV-266 — the estate MCP gateway (SERV-257, ruling 4) verifies a caller's
// Access assertion against its own audience and forwards it unchanged, so the
// assertion reaching this transport carries the GATEWAY's tag. The transport
// accepts that tag as one more MCP-only audience when, and only when, it is
// configured, and the API's exchange must agree, or every session fails.

const gatewayAUD = "gateway-application-tag"

// A gateway-forwarded assertion opens the same session a direct one does: the
// person, on the hosted transport, not writing, with no write tool offered.
func TestHostedAcceptsTheGatewayAudienceWhenConfigured(t *testing.T) {
	w := buildWorld(t, true, gatewayAUD)
	_, endpoint := w.hostedFor(t, mcpAUD, gatewayAUD)

	cs := connect(t, endpoint, w.assertion(t, w.owner.Email, gatewayAUD))
	defer func() { _ = cs.Close() }()

	if names, want := toolNames(t, cs), registeredReadTools(); !slices.Equal(names, want) {
		t.Fatalf("tools = %v, want exactly the registry's read tools %v", names, want)
	}
	who := callWhoAmI(t, cs)
	if who.Kind != "person" || who.Transport != "hosted" || who.MayWrite || who.Email != w.owner.Email {
		t.Fatalf("whoami = %+v, want the owner as a person on hosted, not writing", who)
	}
	if n := w.liveHosted(t); n != 1 {
		t.Errorf("live hosted sessions = %d, want 1 bounded hosted session", n)
	}
}

// Criterion 6 of SERV-257's plan, the backend's half: when the gateway ends
// its session it DELETEs each backend session it opened, and the backend must
// close that session and revoke the token it minted for it. The gateway's own
// half (sending the DELETE) is SERV-267.
func TestAGatewaySessionsDeleteClosesItAndRevokesItsToken(t *testing.T) {
	w := buildWorld(t, true, gatewayAUD)
	h, endpoint := w.hostedFor(t, mcpAUD, gatewayAUD)
	assertion := w.assertion(t, w.owner.Email, gatewayAUD)

	first := raw(t, http.MethodPost, endpoint, initializeBody, map[string]string{"Cf-Access-Jwt-Assertion": assertion})
	sid := first.Header.Get("Mcp-Session-Id")
	if first.StatusCode != http.StatusOK || sid == "" || w.liveHosted(t) != 1 {
		t.Fatalf("initialize = %d, session %q, %d live hosted rows; want a session and one row",
			first.StatusCode, sid, w.liveHosted(t))
	}

	resp := raw(t, http.MethodDelete, endpoint, "", map[string]string{"Cf-Access-Jwt-Assertion": assertion, "Mcp-Session-Id": sid})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204", resp.StatusCode)
	}

	waitFor(t, "the gateway session's token to be revoked", func() bool { return w.liveHosted(t) == 0 })
	if h.SessionCount() != 0 {
		t.Errorf("SessionCount = %d after the gateway closed its session", h.SessionCount())
	}
	if _, revokes := w.counts(); revokes != 1 {
		t.Errorf("revokes = %d, want exactly one", revokes)
	}
	after := raw(t, http.MethodPost, endpoint, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		map[string]string{"Cf-Access-Jwt-Assertion": assertion, "Mcp-Session-Id": sid})
	if after.StatusCode != http.StatusNotFound {
		t.Errorf("the closed session's id = %d, want 404", after.StatusCode)
	}
}

// Every gateway-shaped assertion this transport cannot accept is refused by
// the transport itself, before the API's exchange is ever called: the
// audience unconfigured (the default), an audience nobody configured, an
// expired one, another team's issuer.
func TestHostedRefusesGatewayAssertionsItCannotAccept(t *testing.T) {
	w := buildWorld(t, true, gatewayAUD)
	good := cfaccesstest.Claims{Email: w.owner.Email, Audience: gatewayAUD}
	with := func(f func(*cfaccesstest.Claims)) cfaccesstest.Claims { c := good; f(&c); return c }

	_, unconfigured := w.hostedFor(t, mcpAUD)
	_, configured := w.hostedFor(t, mcpAUD, gatewayAUD)

	for name, tc := range map[string]struct {
		endpoint string
		claims   cfaccesstest.Claims
	}{
		"gateway audience not configured on the transport": {unconfigured, good},
		"an unknown audience":                              {configured, with(func(c *cfaccesstest.Claims) { c.Audience = "some-other-application" })},
		"the web application's audience":                   {configured, with(func(c *cfaccesstest.Claims) { c.Audience = webAUD })},
		"expired":                                          {configured, with(func(c *cfaccesstest.Claims) { c.Expires = -time.Minute })},
		"wrong issuer":                                     {configured, with(func(c *cfaccesstest.Claims) { c.Issuer = "https://elsewhere.cloudflareaccess.com" })},
	} {
		t.Run(name, func(t *testing.T) {
			resp := raw(t, http.MethodPost, tc.endpoint, initializeBody,
				map[string]string{"Cf-Access-Jwt-Assertion": w.iss.Sign(t, tc.claims)})
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", resp.StatusCode)
			}
			if resp.Header.Get("Mcp-Session-Id") != "" {
				t.Error("a refused request was given a session id")
			}
		})
	}
	if ex, _ := w.counts(); ex != 0 {
		t.Errorf("the API's exchange was called %d times for assertions the transport should have refused", ex)
	}
	if n := w.liveHosted(t); n != 0 {
		t.Errorf("%d hosted sessions were minted for refused assertions", n)
	}
}

// The two readers must agree (check-edge-auth.sh asserts it in
// construct-server). If the transport accepts the gateway's tag but the API
// was not told it, the exchange refuses, the transport passes the 401 on, and
// nothing is minted -- loud, never a session on the wrong terms.
func TestAGatewayAudienceTheAPIDoesNotKnowOpensNothing(t *testing.T) {
	w := newWorld(t) // the API accepts the web and MCP endpoint's tags only
	h, endpoint := w.hostedFor(t, mcpAUD, gatewayAUD)

	resp := raw(t, http.MethodPost, endpoint, initializeBody,
		map[string]string{"Cf-Access-Jwt-Assertion": w.assertion(t, w.owner.Email, gatewayAUD)})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 passed through from the exchange", resp.StatusCode)
	}
	if ex, _ := w.counts(); ex != 1 {
		t.Errorf("exchanges = %d, want the one refused attempt", ex)
	}
	if n := w.liveHosted(t); n != 0 {
		t.Errorf("%d hosted sessions minted for an audience the API does not accept", n)
	}
	if h.SessionCount() != 0 {
		t.Errorf("SessionCount = %d", h.SessionCount())
	}
}

// SERV-340 (CHRN-172) -- the gateway audience is a second way in, so it must
// not be a second way to get an account. An assertion on it whose email has no
// user is refused by the API's exchange with 403 sso_no_account, the transport
// passes that through without a session id, and no user is provisioned. This
// mirrors the stranger case of TestHostedPassesThroughTheExchangesRefusal, which
// covers the MCP audience only.
func TestHostedRefusesAnUnknownEmailOnTheGatewayAudience(t *testing.T) {
	w := buildWorld(t, true, gatewayAUD)
	h, endpoint := w.hostedFor(t, mcpAUD, gatewayAUD)

	before, err := w.st.CountUsers(w.ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}

	resp := raw(t, http.MethodPost, endpoint, initializeBody,
		map[string]string{"Cf-Access-Jwt-Assertion": w.assertion(t, "stranger@example.com", gatewayAUD)})
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if resp.StatusCode != http.StatusForbidden || body.Code != "sso_no_account" {
		t.Errorf("status = %d, code %q, want 403 sso_no_account", resp.StatusCode, body.Code)
	}
	if resp.Header.Get("Mcp-Session-Id") != "" {
		t.Error("an email with no account was given a session id")
	}
	if ex, _ := w.counts(); ex != 1 {
		t.Errorf("exchanges = %d, want the one refused attempt", ex)
	}
	if n := w.liveHosted(t); n != 0 {
		t.Errorf("%d hosted sessions minted for an email with no account", n)
	}
	if h.SessionCount() != 0 {
		t.Errorf("SessionCount = %d", h.SessionCount())
	}
	after, err := w.st.CountUsers(w.ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if after != before {
		t.Errorf("tier2.users = %d after the refusal, was %d: the gateway audience provisioned an account", after, before)
	}
	t.Logf("gateway audience, unknown email: status=%d code=%s session_id=none exchanges=1 tier2.users before=%d after=%d",
		resp.StatusCode, body.Code, before, after)
}
