package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// CHRN-75. Keying on RemoteAddr alone collapses everything behind Traefik into
// one bucket, so one attacker locks out every real user. X-Forwarded-For is
// believed only when the request carries the secret Traefik stamps -- because
// on construct_net the PEER ADDRESS cannot tell Traefik from a neighbour, which
// is what the retired CHRONICLE_TRUSTED_PROXIES tried and failed to do.
func TestClientIPTrustsForwardedOnlyWithTheProxySecret(t *testing.T) {
	const secret = "chrn75-test-fixture-not-a-real-value"
	a := &api{proxySecret: secret, logger: discardLogger(), mismatch: &mismatchWarner{now: time.Now}}

	withSecret := func(remote, presented, fwd string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = remote
		if presented != "" {
			r.Header.Set(ProxySecretHeader, presented)
		}
		if fwd != "" {
			r.Header.Set("X-Forwarded-For", fwd)
		}
		return r
	}

	t.Run("through Traefik", func(t *testing.T) {
		if got := a.clientIP(withSecret("172.19.0.16:40000", secret, "203.0.113.44")); got != "203.0.113.44" {
			t.Errorf("clientIP = %q, want the forwarded client", got)
		}
	})

	// The case the whole ticket exists for. This peer is inside the RETIRED
	// default 172.16.0.0/12, so the old code trusted it and it could pick any
	// bucket it liked by writing a header.
	t.Run("a neighbour on construct_net spoofing the header", func(t *testing.T) {
		if got := a.clientIP(withSecret("172.19.0.20:40000", "", "203.0.113.44")); got != "172.19.0.20" {
			t.Errorf("clientIP = %q, want the real peer — it presented no secret", got)
		}
	})

	// And presence is not the test. A neighbour going direct arrives with the
	// header PRESENT AND WRONG, which is lab case C from the decision.
	t.Run("a neighbour presenting a wrong secret", func(t *testing.T) {
		if got := a.clientIP(withSecret("172.19.0.20:40000", "FORGED-BY-CLIENT", "203.0.113.44")); got != "172.19.0.20" {
			t.Errorf("clientIP = %q, want the real peer — the secret did not match", got)
		}
	})

	// A prefix of the real secret must not pass either: ConstantTimeCompare is
	// length-sensitive, and this is the shape a truncated env var takes.
	t.Run("a truncated secret", func(t *testing.T) {
		if got := a.clientIP(withSecret("172.19.0.20:40000", secret[:8], "203.0.113.44")); got != "172.19.0.20" {
			t.Errorf("clientIP = %q, want the real peer", got)
		}
	})

	t.Run("a chain takes the last hop", func(t *testing.T) {
		if got := a.clientIP(withSecret("172.19.0.16:40000", secret, "1.2.3.4, 203.0.113.44")); got != "203.0.113.44" {
			t.Errorf("clientIP = %q, want the rightmost hop", got)
		}
	})

	t.Run("no secret configured", func(t *testing.T) {
		bare := &api{}
		if got := bare.clientIP(withSecret("172.19.0.16:40000", secret, "203.0.113.44")); got != "172.19.0.16" {
			t.Errorf("clientIP = %q, want the peer when nothing is believed", got)
		}
	})

	// Two clients behind Traefik get their own budgets — the property the whole
	// dance exists to buy, and the one "just use RemoteAddr" throws away.
	h := NewRouter(Deps{
		DB: fakePinger{}, Accounts: newFakeAccounts(), Logger: discardLogger(),
		Version: "test", ProxySecret: secret,
	})
	exhaust := func(client string) int {
		var last int
		for i := 0; i < signInRateBurst+2; i++ {
			req := jsonReq(http.MethodPost, "/auth/session", `{"token":"chr_no"}`)
			req.RemoteAddr = "172.19.0.16:40000"
			req.Header.Set(ProxySecretHeader, secret)
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
	req.RemoteAddr = "172.19.0.16:40000"
	req.Header.Set(ProxySecretHeader, secret)
	req.Header.Set("X-Forwarded-For", "203.0.113.2")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusTooManyRequests {
		t.Error("a second client behind Traefik was locked out by the first")
	}
}

// The defect, as a bucket count rather than as a string comparison: a neighbour
// rotating X-Forwarded-For must not get a fresh 20-request budget per value.
//
// This is the assertion that failed before CHRN-75. It cannot reference the
// retired CHRONICLE_TRUSTED_PROXIES any more — the field is gone — so what it
// pins is the behaviour: whatever the neighbour writes, it is keyed on its own
// address and the 21st request is refused.
func TestANeighbourCannotBuyBucketsWithAHeader(t *testing.T) {
	h := NewRouter(Deps{
		DB: fakePinger{}, Accounts: newFakeAccounts(), Logger: discardLogger(),
		Version: "test", ProxySecret: "chrn75-test-fixture-not-a-real-value",
	})

	var last int
	for i := 0; i < signInRateBurst+1; i++ {
		req := jsonReq(http.MethodPost, "/auth/session", `{"token":"chr_no"}`)
		// A neighbour on construct_net -- inside the retired default -- with a
		// different spoofed client every time.
		req.RemoteAddr = "172.19.0.20:40000"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i+1))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("request %d = %d, want 429: a neighbour bought %d buckets with a header",
			signInRateBurst+1, last, signInRateBurst+1)
	}
}

// A mismatched secret is the likeliest first-deploy state and used to be
// completely silent -- the same coarse behaviour as "unset", with none of the
// warning. It must say so, and must not say so twenty-one times.
func TestAMismatchedSecretWarnsOnceAndNeverCarriesEitherValue(t *testing.T) {
	const secret = "chrn75-test-fixture-not-a-real-value"
	var buf bytes.Buffer
	a := &api{
		proxySecret: secret,
		logger:      slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
		mismatch:    &mismatchWarner{now: time.Now},
	}

	for i := 0; i < 21; i++ {
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = "172.19.0.20:40000"
		r.Header.Set(ProxySecretHeader, "FORGED-BY-CLIENT")
		a.clientIP(r)
	}

	out := buf.String()
	if n := strings.Count(out, "did not match"); n != 1 {
		t.Errorf("warned %d times across 21 mismatches, want 1 — a neighbour can trip this deliberately", n)
	}
	if !strings.Contains(out, "172.19.0.20") {
		t.Error("the warning does not carry the peer, which is the whole diagnostic")
	}
	// Neither the configured value nor the presented one may reach a log.
	if strings.Contains(out, secret) {
		t.Error("the warning leaked the configured secret")
	}
	if strings.Contains(out, "FORGED-BY-CLIENT") {
		t.Error("the warning leaked the presented value")
	}

	// A request with NO header is the ordinary lateral case and must stay quiet.
	buf.Reset()
	quiet := &api{proxySecret: secret, logger: a.logger, mismatch: &mismatchWarner{now: time.Now}}
	r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
	r.RemoteAddr = "172.19.0.20:40000"
	quiet.clientIP(r)
	if buf.Len() != 0 {
		t.Errorf("an absent header warned: %s", buf.String())
	}
}

func TestMismatchWarnerOpensAgainAfterItsWindow(t *testing.T) {
	now := time.Now()
	w := &mismatchWarner{now: func() time.Time { return now }}

	if !w.allow() {
		t.Fatal("the first warning was suppressed")
	}
	if w.allow() {
		t.Error("a second warning inside the window was emitted")
	}
	now = now.Add(mismatchWarnEvery + time.Second)
	if !w.allow() {
		t.Error("the warner never reopened — a persistent misconfiguration would go quiet")
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

// The state the reviewer found, which the signed-off decision had left out: an
// ABSENT header and a MISSING MIDDLEWARE are indistinguishable at the request
// level, and the second is a silent shared bucket on exactly the sign-in path.
//
// SERV-148 attaches the middleware to three routers and its own text warns
// "all three, not only the /auth/ one" — miss chronicle-public-auth and every
// WAN sign-in shares one bucket with nothing saying so.
func TestAConfiguredSecretNoRequestCarriesIsReported(t *testing.T) {
	const secret = "chrn75-test-fixture-not-a-real-value"
	newAPI := func(buf *bytes.Buffer) *api {
		return &api{
			proxySecret: secret,
			logger:      slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
			mismatch:    &mismatchWarner{now: time.Now},
			absent:      &mismatchWarner{now: time.Now},
			proxySeen:   &proxySeen{},
		}
	}
	bare := func(remote string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		r.RemoteAddr = remote
		return r
	}

	t.Run("reported until a request proves the middleware is attached", func(t *testing.T) {
		var buf bytes.Buffer
		a := newAPI(&buf)

		for i := 0; i < 5; i++ {
			a.clientIP(bare("172.19.0.16:40000"))
		}
		out := buf.String()
		if n := strings.Count(out, "no request has ever carried it"); n != 1 {
			t.Fatalf("warned %d times across 5 header-less requests, want 1", n)
		}
		if !strings.Contains(out, "SERV-148") {
			t.Error("the warning does not name the ticket that fixes it")
		}
		if strings.Contains(out, secret) {
			t.Error("the warning leaked the configured secret")
		}
	})

	// One good request silences it for the life of the process: past that
	// point an absent header really is the ordinary lateral case.
	t.Run("silent once the middleware has proved itself", func(t *testing.T) {
		var buf bytes.Buffer
		a := newAPI(&buf)

		through := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		through.RemoteAddr = "172.19.0.16:40000"
		through.Header.Set(ProxySecretHeader, secret)
		through.Header.Set("X-Forwarded-For", "203.0.113.44")
		if got := a.clientIP(through); got != "203.0.113.44" {
			t.Fatalf("clientIP = %q, want the forwarded client", got)
		}

		buf.Reset()
		a.absent = &mismatchWarner{now: time.Now} // a fresh window, so only the flag can quiet it
		for i := 0; i < 5; i++ {
			a.clientIP(bare("172.19.0.20:40000"))
		}
		if buf.Len() != 0 {
			t.Errorf("still warning after a request carried the secret: %s", buf.String())
		}
	})

	// A neighbour cannot suppress the warning, because it cannot produce a
	// match — which is what stops this being a signal an attacker can turn off.
	t.Run("a wrong secret does not count as proof", func(t *testing.T) {
		var buf bytes.Buffer
		a := newAPI(&buf)

		forged := httptest.NewRequest(http.MethodPost, "/auth/session", nil)
		forged.RemoteAddr = "172.19.0.20:40000"
		forged.Header.Set(ProxySecretHeader, "FORGED-BY-CLIENT")
		a.clientIP(forged)

		if a.proxySeen.yes() {
			t.Fatal("a forged secret was accepted as proof the middleware is attached")
		}
		buf.Reset()
		a.clientIP(bare("172.19.0.16:40000"))
		if !strings.Contains(buf.String(), "no request has ever carried it") {
			t.Error("the warning was suppressed by a request that did not match")
		}
	})

	// And with no secret configured at all, boot already warns — this path must
	// stay quiet rather than doubling the noise on a LAN install.
	t.Run("quiet when no secret is configured", func(t *testing.T) {
		var buf bytes.Buffer
		a := newAPI(&buf)
		a.proxySecret = ""
		a.clientIP(bare("172.19.0.20:40000"))
		if buf.Len() != 0 {
			t.Errorf("warned with no secret configured: %s", buf.String())
		}
	})
}
