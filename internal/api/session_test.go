package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// fakeAccounts implements only what a given test touches. The embedded nil
// interface makes any unexpected call panic with the method name, which is a
// better failure than a zero value quietly satisfying an assertion.
type fakeAccounts struct {
	Accounts
	sessions       map[string]store.User // plaintext -> account
	invites        map[string]store.User // plaintext -> account, single use
	byEmail        map[string]store.User
	createErr      error
	minted         string
	mintedSessions []string
	revoked        []string
}

func newFakeAccounts() *fakeAccounts {
	return &fakeAccounts{
		sessions: map[string]store.User{},
		invites:  map[string]store.User{},
		byEmail:  map[string]store.User{},
	}
}

func (f *fakeAccounts) UserByToken(_ context.Context, plaintext string) (store.User, error) {
	u, ok := f.sessions[plaintext]
	if !ok || plaintext == "" {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeAccounts) RedeemInvite(_ context.Context, plaintext, label string) (store.User, string, error) {
	u, ok := f.invites[plaintext]
	if !ok || plaintext == "" {
		return store.User{}, "", store.ErrNotFound
	}
	delete(f.invites, plaintext) // single use
	session := "chr_session_" + label
	f.sessions[session] = u
	return u, session, nil
}

func (f *fakeAccounts) RevokeToken(_ context.Context, plaintext string) error {
	delete(f.sessions, plaintext)
	f.revoked = append(f.revoked, plaintext)
	return nil
}

func (f *fakeAccounts) MintInvite(_ context.Context, _ uuid.UUID, _ string) (string, error) {
	f.minted = "chr_invite_new"
	return f.minted, nil
}

func (f *fakeAccounts) ReplaceDeviceInvite(context.Context, uuid.UUID) (string, error) {
	f.minted = "chr_invite_new"
	return f.minted, nil
}

func (f *fakeAccounts) MintToken(_ context.Context, _ uuid.UUID, _, label string, _ *time.Time) (string, error) {
	tok := "chr_minted_" + label
	f.mintedSessions = append(f.mintedSessions, tok)
	return tok, nil
}

func (f *fakeAccounts) GetUserByEmail(_ context.Context, email string) (store.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return store.User{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeAccounts) ListMembers(context.Context) ([]store.Member, error) { return nil, nil }

func (f *fakeAccounts) CreateUser(_ context.Context, email, name, kind string) (store.User, error) {
	if f.createErr != nil {
		return store.User{}, f.createErr
	}
	return store.User{ID: uuid.New(), Email: email, DisplayName: name, Kind: kind}, nil
}

func (f *fakeAccounts) ListSessions(context.Context, uuid.UUID, string) ([]store.Session, error) {
	return nil, nil
}

// jsonReq builds a request the API will accept. Every JSON endpoint now
// requires the correct Content-Type, which is what closes the login-CSRF hole;
// tests have to send it like a real client does.
func jsonReq(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func person(email string, owner bool) store.User {
	return store.User{ID: uuid.New(), Email: email, DisplayName: email, Kind: store.KindPerson, IsOwner: owner}
}

func testRouter(f *fakeAccounts) http.Handler {
	return NewRouter(Deps{
		DB:            fakePinger{},
		Accounts:      f,
		Logger:        discardLogger(),
		Version:       "test",
		MobileBaseURL: "https://chronicle.example.com",
		SecureCookies: true,
	})
}

// Done when #4: an unauthenticated request to a non-health route gets nothing.
// There is no unauthenticated read surface, and no CHRONICLE_AUTH to switch
// this off.
func TestUnauthenticatedRequestsAreRefused(t *testing.T) {
	h := testRouter(newFakeAccounts())

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/auth/me"},
		{http.MethodPatch, "/auth/me"},
		{http.MethodDelete, "/auth/session"},
		{http.MethodPost, "/auth/invite"},
		{http.MethodGet, "/auth/sessions"},
		{http.MethodDelete, "/auth/sessions/" + uuid.New().String()},
		{http.MethodGet, "/admin/users"},
		{http.MethodPost, "/admin/users"},
		{http.MethodPost, "/admin/users/" + uuid.New().String() + "/invite"},
		{http.MethodDelete, "/admin/users/" + uuid.New().String()},
	} {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, jsonReq(route.method, route.path, "{}"))
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
			if got := rec.Header().Get("WWW-Authenticate"); got == "" {
				t.Error("no WWW-Authenticate header on a 401")
			}
		})
	}

	// A garbage token is refused the same way an absent one is.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req.Header.Set("Authorization", "Bearer chr_not-a-real-token")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bogus token = %d, want 401", rec.Code)
	}
}

// The probes stay open: a liveness check cannot require a credential, and
// CHRN-59's QR onboarding probes /healthz before any credential exists.
func TestHealthProbesStayOpen(t *testing.T) {
	h := testRouter(newFakeAccounts())
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200 without a credential", path, rec.Code)
		}
	}
}

// Done when #5: a spent invite is indistinguishable from one that never
// existed — same status, same headers, same bytes.
func TestSpentAndUnknownInvitesAreIndistinguishable(t *testing.T) {
	f := newFakeAccounts()
	owner := person("owner@example.com", true)
	f.invites["chr_will_be_spent"] = owner
	h := testRouter(f)

	signIn := func(token string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"token": token, "device_label": "probe"})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(http.MethodPost, "/auth/session", string(body)))
		return rec
	}

	if rec := signIn("chr_will_be_spent"); rec.Code != http.StatusOK {
		t.Fatalf("first redemption = %d, want 200", rec.Code)
	}

	spent := signIn("chr_will_be_spent")
	unknown := signIn("chr_never_existed")

	if spent.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
		t.Fatalf("spent = %d, unknown = %d; want both 401", spent.Code, unknown.Code)
	}
	if !bytes.Equal(spent.Body.Bytes(), unknown.Body.Bytes()) {
		t.Errorf("bodies differ:\n spent:   %q\n unknown: %q", spent.Body.String(), unknown.Body.String())
	}
	if spent.Header().Get("Content-Type") != unknown.Header().Get("Content-Type") {
		t.Error("content types differ between a spent and an unknown invite")
	}
	// And neither hands back a cookie.
	if len(spent.Result().Cookies()) != 0 || len(unknown.Result().Cookies()) != 0 {
		t.Error("a refused sign-in set a cookie")
	}
}

func TestSignInIssuesBothCarriers(t *testing.T) {
	f := newFakeAccounts()
	f.invites["chr_good"] = person("owner@example.com", true)
	h := testRouter(f)

	body, _ := json.Marshal(map[string]string{"token": "chr_good", "device_label": "Pixel 8"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPost, "/auth/session", string(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionToken == "" {
		t.Error("no session token in the response")
	}

	// The cookie exists so a browser sub-resource — a cover image, an audio
	// element streaming a memo — can authenticate without a header it cannot
	// set.
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie is readable by page scripts")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Value != got.SessionToken {
		t.Error("the cookie and the returned token disagree")
	}
}

func TestSessionArrivesByHeaderOrCookieAndHeaderWins(t *testing.T) {
	f := newFakeAccounts()
	holder := person("owner@example.com", true)
	f.sessions["chr_live"] = holder
	h := testRouter(f)

	t.Run("cookie", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "chr_live"})
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})

	t.Run("header beats a stale cookie", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "chr_stale"})
		req.Header.Set("Authorization", "Bearer chr_live")
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200 — the header should override a stale cookie", rec.Code)
		}
	})
}

// Done when #3 at the HTTP layer: signing out clears this device and leaves the
// account's others alone.
func TestSignOutRevokesOnlyTheCallingDevice(t *testing.T) {
	f := newFakeAccounts()
	holder := person("owner@example.com", true)
	f.sessions["chr_phone"] = holder
	f.sessions["chr_laptop"] = holder
	h := testRouter(f)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/auth/session", nil)
	req.Header.Set("Authorization", "Bearer chr_phone")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(f.revoked) != 1 || f.revoked[0] != "chr_phone" {
		t.Errorf("revoked = %v, want just the calling device", f.revoked)
	}
	if _, ok := f.sessions["chr_laptop"]; !ok {
		t.Error("the other device was signed out too")
	}

	// The cookie is cleared, or a browser keeps sending a token that resolves
	// to nothing.
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("the session cookie was not cleared on sign-out")
	}
}

// /admin is the owner's alone, and an agent account never qualifies — the
// Scribe holds a session so it can author, not so it can administer.
func TestAdminIsOwnerOnly(t *testing.T) {
	f := newFakeAccounts()
	f.sessions["chr_owner"] = person("owner@example.com", true)
	f.sessions["chr_member"] = person("member@example.com", false)
	f.sessions["chr_scribe"] = store.User{
		ID: uuid.New(), Email: "scribe@chronicle.local", DisplayName: "Scribe",
		Kind: store.KindAgent, IsOwner: true, // even if it somehow held the flag
	}
	h := testRouter(f)

	for _, tc := range []struct {
		name, token string
		want        int
	}{
		{"owner", "chr_owner", http.StatusOK},
		{"member", "chr_member", http.StatusForbidden},
		{"agent holding the owner flag", "chr_scribe", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// Adding your own next device is not administration: the new session can do
// nothing the one requesting it cannot already do.
func TestSelfInviteIsNotAnAdminRoute(t *testing.T) {
	f := newFakeAccounts()
	f.sessions["chr_member"] = person("member@example.com", false)
	h := testRouter(f)

	rec := httptest.NewRecorder()
	req := jsonReq(http.MethodPost, "/auth/invite", "")
	req.Header.Set("Authorization", "Bearer chr_member")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	var got inviteJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.InviteToken == "" {
		t.Error("no invite token in the reveal")
	}
	// The scannable link is built server-side, because only the server knows
	// which of its origins a phone can reach.
	if got.SignInURL != "https://chronicle.example.com/sign-in?token="+got.InviteToken {
		t.Errorf("SignInURL = %q", got.SignInURL)
	}
}

// SSO answers sso_disabled rather than 404 when it is not configured, so a
// client can tell "not set up here" from "wrong URL".
func TestSSODisabledWhenUnconfigured(t *testing.T) {
	h := testRouter(newFakeAccounts())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, jsonReq(http.MethodPost, "/auth/sso/cloudflare", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body ssoErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "sso_disabled" {
		t.Errorf("error = %q, want sso_disabled", body.Error)
	}
}

// A forged Cf-Access-Jwt-Assertion authenticates nobody: the header is verified
// against Cloudflare's signing keys, never trusted because it is present.
func TestForgedAccessHeaderIsRefused(t *testing.T) {
	f := newFakeAccounts()
	h := NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test",
		CFAccess: NewCFAccessVerifier("team.invalid", "aud"),
	})

	rec := httptest.NewRecorder()
	req := jsonReq(http.MethodPost, "/auth/sso/cloudflare", "")
	req.Header.Set("Cf-Access-Jwt-Assertion", "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6Im1hZ29zQGV4YW1wbGUuY29tIn0.")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var body ssoErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error != "unauthorized" {
		t.Errorf("error = %q, want unauthorized", body.Error)
	}
}

// The credential endpoint is bounded even for a caller already inside the
// Docker network, where Traefik's limiter never sees the request.
func TestSignInIsRateLimited(t *testing.T) {
	h := testRouter(newFakeAccounts())

	var lastCode int
	for i := 0; i < signInRateBurst+5; i++ {
		body, _ := json.Marshal(map[string]string{"token": "chr_wrong"})
		req := jsonReq(http.MethodPost, "/auth/session", string(body))
		req.RemoteAddr = "203.0.113.7:51000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		lastCode = rec.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Errorf("after %d attempts the last status was %d, want 429", signInRateBurst+5, lastCode)
	}
}

// The access log must not carry credentials. A sign-in link puts an invite in
// the query string, so the logger records the path only.
func TestAccessLogOmitsTheQueryString(t *testing.T) {
	var buf bytes.Buffer
	h := NewRouter(Deps{DB: fakePinger{}, Accounts: newFakeAccounts(), Logger: jsonLogger(&buf), Version: "test"})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/me?token=chr_secret_in_a_url", nil))

	if strings.Contains(buf.String(), "chr_secret_in_a_url") {
		t.Errorf("the access log carried a credential from the query string:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"http.url":"/auth/me"`) {
		t.Errorf("expected the bare path in the log, got:\n%s", buf.String())
	}
}

func TestRateLimiterWindowResets(t *testing.T) {
	now := time.Now()
	l := newIPRateLimiter(time.Minute, 2)
	l.now = func() time.Time { return now }

	for i := 1; i <= 2; i++ {
		if !l.allow("a") {
			t.Fatalf("attempt %d was refused inside a burst of 2", i)
		}
	}
	if l.allow("a") {
		t.Error("a third attempt inside the window was allowed")
	}
	// A different key is unaffected.
	if !l.allow("b") {
		t.Error("one key's exhaustion blocked another")
	}
	now = now.Add(time.Minute + time.Second)
	if !l.allow("a") {
		t.Error("the window did not reset")
	}
}

// The cookie's Secure flag comes from configuration, not from r.TLS. Behind
// Traefik r.TLS is nil on every request — including on the WAN entrypoint —
// so deriving it from the request shipped a durable credential without the
// flag in exactly the deployment that needs it.
func TestSessionCookieIsSecureBehindATLSTerminatingProxy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		secure bool
	}{
		{"configured secure", true},
		{"plain-HTTP LAN install", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeAccounts()
			f.invites["chr_good"] = person("owner@example.com", true)
			h := NewRouter(Deps{
				DB: fakePinger{}, Accounts: f, Logger: discardLogger(),
				Version: "test", SecureCookies: tc.secure,
			})

			rec := httptest.NewRecorder()
			req := jsonReq(http.MethodPost, "/auth/session", `{"token":"chr_good"}`)
			req.TLS = nil // what every request actually looks like behind Traefik
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			for _, c := range rec.Result().Cookies() {
				if c.Name == sessionCookie && c.Secure != tc.secure {
					t.Errorf("cookie Secure = %v on a request with no TLS, want %v", c.Secure, tc.secure)
				}
			}
		})
	}
}

// Login CSRF: a cross-site form can only produce a handful of content types,
// none of them application/json. Requiring it is what stops an attacker's page
// swapping a victim's session for one of the attacker's accounts.
func TestSignInRefusesNonJSONContentTypes(t *testing.T) {
	f := newFakeAccounts()
	f.invites["chr_attacker"] = person("attacker@example.com", false)
	h := testRouter(f)

	for _, ct := range []string{
		"text/plain",                        // <form enctype="text/plain">
		"application/x-www-form-urlencoded", // the default form encoding
		"multipart/form-data",
		"", // no header at all
	} {
		t.Run("Content-Type: "+ct, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/session",
				strings.NewReader(`{"token":"chr_attacker"}`))
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Errorf("status = %d, want 415", rec.Code)
			}
			if len(rec.Result().Cookies()) != 0 {
				t.Error("a cross-site form post was handed a session cookie")
			}
		})
	}

	// The invite must still be unspent: a refused request cannot have redeemed.
	if _, ok := f.invites["chr_attacker"]; !ok {
		t.Error("the invite was redeemed by a request that was refused")
	}

	// A charset parameter is still JSON.
	req := httptest.NewRequest(http.MethodPost, "/auth/session",
		strings.NewReader(`{"token":"chr_attacker"}`))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("application/json; charset=utf-8 = %d, want 200", rec.Code)
	}
}

// Bodies and fields are bounded, so an unauthenticated caller cannot force
// unbounded allocation or write a 10 KB "device label" into a TEXT column.
func TestRequestBodiesAndFieldsAreBounded(t *testing.T) {
	f := newFakeAccounts()
	f.invites["chr_good"] = person("owner@example.com", true)
	h := testRouter(f)

	t.Run("oversized body", func(t *testing.T) {
		huge := `{"token":"` + strings.Repeat("A", maxBodyBytes*2) + `"}`
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(http.MethodPost, "/auth/session", huge))
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Errorf("status = %d, want 413", rec.Code)
		}
	})

	t.Run("oversized device label", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"token":        "chr_good",
			"device_label": strings.Repeat("x", maxDeviceLabelLen+1),
		})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, jsonReq(http.MethodPost, "/auth/session", string(body)))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", rec.Code)
		}
		if _, ok := f.invites["chr_good"]; !ok {
			t.Error("the invite was spent by a request that was rejected")
		}
	})
}

// The SPA calls the SSO endpoint on load. Minting per call would bury the
// device list — the only thing that makes a non-expiring session safe — under
// one identical row per page view.
func TestSSOReusesTheSessionTheBrowserAlreadyHolds(t *testing.T) {
	f := newFakeAccounts()
	magos := person("magos@example.com", true)
	f.byEmail["magos@example.com"] = magos
	f.sessions["chr_existing"] = magos

	a := &api{accounts: f, logger: discardLogger(), secureCookies: true,
		signInLimiter: newIPRateLimiter(signInRateWindow, signInRateBurst)}

	// Stand in for a verified Access identity, so the test is about session
	// reuse rather than about JWT verification (covered separately).
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/sso/cloudflare", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "chr_existing"})
	a.completeCFAccessSignIn(rec, req, magos)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if len(f.mintedSessions) != 0 {
		t.Errorf("minted %v; the browser's existing session should have been reused", f.mintedSessions)
	}

	var got sessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.SessionToken != "chr_existing" {
		t.Errorf("session_token = %q, want the one already held", got.SessionToken)
	}

	// A cookie belonging to somebody else does NOT get reused.
	f.sessions["chr_other"] = person("other@example.com", false)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/sso/cloudflare", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "chr_other"})
	a.completeCFAccessSignIn(rec, req, magos)
	if len(f.mintedSessions) != 1 {
		t.Errorf("minted %v; a session belonging to another account must not be reused", f.mintedSessions)
	}
}

// Both unauthenticated credential-minting endpoints are limited, not just one.
func TestBothSignInEndpointsAreRateLimited(t *testing.T) {
	for _, path := range []string{"/auth/session", "/auth/sso/cloudflare"} {
		t.Run(path, func(t *testing.T) {
			h := testRouter(newFakeAccounts())
			var last int
			for i := 0; i < signInRateBurst+2; i++ {
				req := jsonReq(http.MethodPost, path, `{"token":"chr_no"}`)
				req.RemoteAddr = "198.51.100.9:4000"
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				last = rec.Code
			}
			if last != http.StatusTooManyRequests {
				t.Errorf("last status = %d, want 429", last)
			}
		})
	}
}

// Keying on RemoteAddr alone collapses everything behind Traefik into one
// bucket, so one attacker locks out every real user. X-Forwarded-For is
// believed only when the peer is a configured proxy.
func TestClientIPTrustsForwardedOnlyFromATrustedProxy(t *testing.T) {
	proxy := netip.MustParsePrefix("172.19.0.0/16")
	a := &api{trustedProxies: []netip.Prefix{proxy}}

	t.Run("through the proxy", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = "172.19.0.5:40000"
		r.Header.Set("X-Forwarded-For", "203.0.113.44")
		if got := a.clientIP(r); got != "203.0.113.44" {
			t.Errorf("clientIP = %q, want the forwarded client", got)
		}
	})

	t.Run("a neighbour container spoofing the header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = "10.42.0.9:40000" // not a trusted proxy
		r.Header.Set("X-Forwarded-For", "203.0.113.44")
		if got := a.clientIP(r); got != "10.42.0.9" {
			t.Errorf("clientIP = %q, want the real peer — the header is not trustworthy here", got)
		}
	})

	t.Run("a chain takes the last hop", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = "172.19.0.5:40000"
		r.Header.Set("X-Forwarded-For", "1.2.3.4, 203.0.113.44")
		if got := a.clientIP(r); got != "203.0.113.44" {
			t.Errorf("clientIP = %q, want the rightmost hop", got)
		}
	})

	t.Run("no trusted proxies configured", func(t *testing.T) {
		bare := &api{}
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = "172.19.0.5:40000"
		r.Header.Set("X-Forwarded-For", "203.0.113.44")
		if got := bare.clientIP(r); got != "172.19.0.5" {
			t.Errorf("clientIP = %q, want the peer when nothing is trusted", got)
		}
	})

	// Two clients behind the same proxy get their own budgets — the property
	// the whole trusted-proxy dance exists to buy.
	h := NewRouter(Deps{
		DB: fakePinger{}, Accounts: newFakeAccounts(), Logger: discardLogger(),
		Version: "test", TrustedProxies: []netip.Prefix{proxy},
	})
	exhaust := func(client string) int {
		var last int
		for i := 0; i < signInRateBurst+2; i++ {
			req := jsonReq(http.MethodPost, "/auth/session", `{"token":"chr_no"}`)
			req.RemoteAddr = "172.19.0.5:40000"
			req.Header.Set("X-Forwarded-For", client)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			last = rec.Code
		}
		return last
	}
	if got := exhaust("203.0.113.1"); got != http.StatusTooManyRequests {
		t.Errorf("first client not limited: %d", got)
	}
	req := jsonReq(http.MethodPost, "/auth/session", `{"token":"chr_no"}`)
	req.RemoteAddr = "172.19.0.5:40000"
	req.Header.Set("X-Forwarded-For", "203.0.113.2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Error("a second client behind the same proxy was locked out by the first")
	}
}

// The map is bounded even when every window is live, which is exactly the case
// an address spray produces.
func TestRateLimiterMapIsBoundedUnderASpray(t *testing.T) {
	now := time.Now()
	l := newIPRateLimiter(time.Hour, 5) // long window: nothing expires on its own
	l.now = func() time.Time { return now }

	for i := 0; i < maxRateKeys*2; i++ {
		l.allow(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}).String())
	}
	if len(l.hits) > maxRateKeys {
		t.Errorf("limiter holds %d keys, above the %d cap", len(l.hits), maxRateKeys)
	}
}

// An invalid `kind` is the caller's mistake. A 500 would tell them to retry
// something that can never succeed.
func TestInvalidAccountKindIsABadRequest(t *testing.T) {
	f := newFakeAccounts()
	f.sessions["chr_owner"] = person("owner@example.com", true)
	f.createErr = store.ErrInvalidInput
	h := testRouter(f)

	req := jsonReq(http.MethodPost, "/admin/users",
		`{"email":"x@example.com","kind":"wizard"}`)
	req.Header.Set("Authorization", "Bearer chr_owner")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
