package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/invite"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-71 — accounts and per-device sessions.
//
// No passwords. A one-time invite redeems into a durable per-device session,
// and two sign-in paths mint the same kind of session: POST /auth/session for
// the app and MCP on the direct host, POST /auth/sso/cloudflare for a browser
// that already cleared Access on the tunneled host.
//
// The principle, from Lyceum's router comment: Cloudflare Access decides
// whether the request is served at all, this decides who it is served as. They
// are complementary, not alternatives — which is why the same account is
// reachable both ways, and why CHRN-65 can put MCP behind Access or not
// without changing anything here.
//
// There is no mode in which Chronicle serves an unauthenticated caller
// anything but a health probe. Lyceum carries a LYCEUM_AUTH flag because it had
// a running single-user install to keep working; Chronicle has none, so the
// flag would exist only to be forgotten, and the serve-every-request-as-owner
// branch it guards is the bug LYCM-116 exists to catch.

// sessionCookie carries the session token for requests a client cannot put a
// header on.
//
// The bearer header alone is not enough. A browser fetching a sub-resource — an
// <img>, an <audio> element streaming a memo — sends no Authorization header,
// so gating those routes on the header would break every one of them. A cookie
// rides along automatically. Native clients send the header and ignore this.
const sessionCookie = "chronicle_session"

// ctxKey is this package's private context-key type, so nothing outside can
// collide with (or forge) the authenticated user.
type ctxKey int

const userCtxKey ctxKey = iota

func withUser(ctx context.Context, u store.User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// userFrom returns the authenticated account for a request handled behind
// requireUser or requireOwner. Anything reached another way gets the zero
// value, whose ID is the nil UUID and therefore matches no row.
func userFrom(ctx context.Context) store.User {
	u, _ := ctx.Value(userCtxKey).(store.User)
	return u
}

// bearerToken extracts the credential from an "Authorization: Bearer <token>"
// header, returning "" when the header is absent or is not a Bearer credential.
// The scheme match is case-insensitive per RFC 7235.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const scheme = "bearer "
	if len(h) < len(scheme) || !strings.EqualFold(h[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(h[len(scheme):])
}

// sessionToken pulls the caller's session credential from the Authorization
// header, falling back to the cookie. The header wins, so a native client can
// override a stale cookie.
func sessionToken(r *http.Request) string {
	if token := bearerToken(r); token != "" {
		return token
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// setSessionCookie issues the session cookie. HttpOnly keeps it away from page
// scripts, so an XSS in the web UI cannot exfiltrate it. Lax is enough because
// every mutating route is JSON-only and none is reachable by a cross-site form
// post. Secure is set only when the request already arrived over TLS, so a LAN
// install on plain HTTP still works.
func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
}

// requireUser wraps next so it runs only for a request carrying a valid session
// token, with the resolved account available via userFrom.
func (a *api) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.accounts.UserByToken(r.Context(), sessionToken(r))
		if errors.Is(err, store.ErrNotFound) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="chronicle"`)
			http.Error(w, "missing or invalid session token", http.StatusUnauthorized)
			return
		}
		if err != nil {
			a.serverError(w, r, "resolve session token", err)
			return
		}
		next(w, r.WithContext(withUser(r.Context(), u)))
	}
}

// requireOwner is requireUser plus an ownership check: a valid session
// belonging to anyone else gets 403. It guards /admin — adding and removing
// accounts is the owner's call alone, and an agent account never qualifies.
func (a *api) requireOwner(next http.HandlerFunc) http.HandlerFunc {
	return a.requireUser(func(w http.ResponseWriter, r *http.Request) {
		if !userFrom(r.Context()).IsAdmin() {
			http.Error(w, "owner only", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// userJSON is the wire shape of an account. It deliberately carries no token
// material: a credential is returned exactly once, by the call that mints it.
type userJSON struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Kind        string    `json:"kind"`
	IsOwner     bool      `json:"is_owner"`
}

func toUserJSON(u store.User) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Kind: u.Kind, IsOwner: u.IsOwner}
}

type sessionResponse struct {
	User         userJSON `json:"user"`
	SessionToken string   `json:"session_token"`
}

// handleAuthSession redeems a single-use invite for a long-lived session bound
// to this device. This is how the app and MCP sign in.
//
//	Body: {"token": "chr_...", "device_label": "Pixel 8"}
//	200:  {"user": {...}, "session_token": "chr_..."}
//
// The session token is shown here and never again; the client stores it and
// sends it as Authorization: Bearer. A spent, expired or unknown invite is 401
// with an identical body — indistinguishable on purpose, so probing cannot tell
// a used invite from one that never existed.
func (a *api) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	if !a.signInLimiter.allow(clientIP(r)) {
		http.Error(w, "too many attempts, slow down", http.StatusTooManyRequests)
		return
	}

	var req struct {
		Token       string `json:"token"`
		DeviceLabel string `json:"device_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	u, session, err := a.accounts.RedeemInvite(r.Context(), strings.TrimSpace(req.Token), req.DeviceLabel)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "invalid or already-used invite", http.StatusUnauthorized)
		return
	}
	if err != nil {
		a.serverError(w, r, "redeem invite", err)
		return
	}

	setSessionCookie(w, r, session)
	writeJSON(w, http.StatusOK, sessionResponse{toUserJSON(u), session})
}

// ssoErrorBody is the wire shape of a Cloudflare SSO refusal. A client switches
// on Error: sso_disabled → fall back to the invite flow silently;
// sso_no_account → show the named email so the person knows what to ask for.
type ssoErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Email   string `json:"email,omitempty"`
}

func writeSSOError(w http.ResponseWriter, status int, code, message, email string) {
	writeJSON(w, status, ssoErrorBody{Error: code, Message: message, Email: email})
}

// handleAuthCFAccess exchanges a verified Cloudflare Access identity for a
// Chronicle session. The browser calls it on load: the tunnel injects a
// Cf-Access-Jwt-Assertion header, cfaccess.go verifies it, and a verified email
// matched to an account mints the same kind of session the invite path does.
//
// It never auto-provisions. An email that cleared the Access gate but has no
// Chronicle account gets 403, not a new account — Access-group membership is
// not the same question as whether somebody has a place in this corpus.
//
//	401 sso_disabled   — SSO unconfigured, or no header (not through the tunnel)
//	401 unauthorized   — the JWT failed verification
//	403 sso_no_account — verified email, no matching account
//	200                — {"user": {...}, "session_token": "..."} plus the cookie
func (a *api) handleAuthCFAccess(w http.ResponseWriter, r *http.Request) {
	if a.cfAccess == nil {
		writeSSOError(w, http.StatusUnauthorized, "sso_disabled",
			"Cloudflare Access SSO is not configured on this server", "")
		return
	}
	jwt := r.Header.Get("Cf-Access-Jwt-Assertion")
	if jwt == "" {
		writeSSOError(w, http.StatusUnauthorized, "sso_disabled",
			"no Cloudflare Access identity on this request", "")
		return
	}

	email, err := a.cfAccess.Verify(r.Context(), jwt)
	if err != nil {
		writeSSOError(w, http.StatusUnauthorized, "unauthorized",
			"invalid Cloudflare Access token", "")
		return
	}

	u, err := a.accounts.GetUserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		writeSSOError(w, http.StatusForbidden, "sso_no_account",
			"signed in to Cloudflare as "+email+", but no Chronicle account has that email — "+
				"ask the owner for an invite", email)
		return
	}
	if err != nil {
		a.serverError(w, r, "cf access: lookup user", err)
		return
	}

	// A fresh session per SSO sign-in, durable like an invite-redeemed one.
	// Signing in on the web does not disturb the person's other devices.
	session, err := a.accounts.MintToken(r.Context(), u.ID, store.TokenSession, "Cloudflare Access", nil)
	if err != nil {
		a.serverError(w, r, "cf access: mint session", err)
		return
	}

	setSessionCookie(w, r, session)
	writeJSON(w, http.StatusOK, sessionResponse{toUserJSON(u), session})
}

// handleAuthSignOut revokes the credential this request carried, so this device
// stops working while the account's other devices are untouched.
func (a *api) handleAuthSignOut(w http.ResponseWriter, r *http.Request) {
	if token := sessionToken(r); token != "" {
		if err := a.accounts.RevokeToken(r.Context(), token); err != nil {
			a.serverError(w, r, "revoke session", err)
			return
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleAuthMe returns the signed-in account.
func (a *api) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toUserJSON(userFrom(r.Context())))
}

// handleAuthUpdateMe renames the signed-in account.
func (a *api) handleAuthUpdateMe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		http.Error(w, "display_name is required", http.StatusBadRequest)
		return
	}

	u, err := a.accounts.UpdateDisplayName(r.Context(), userFrom(r.Context()).ID, name)
	if err != nil {
		a.serverError(w, r, "update display name", err)
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

// inviteJSON is the one-time reveal. Every field is shown exactly once.
//
// SignInURL is the same invite as a scannable link. The server builds it
// because only the server knows which of its origins a phone can reach: the
// browser minting an invite is typically on the Access-gated hostname, and a QR
// built from that origin walks the phone into an SSO wall a bearer token cannot
// open. Omitted when CHRONICLE_MOBILE_BASE_URL is unset.
type inviteJSON struct {
	User        userJSON `json:"user"`
	InviteToken string   `json:"invite_token"`
	SignInURL   string   `json:"sign_in_url,omitempty"`
	ExpiresIn   string   `json:"expires_in"`
}

func (a *api) writeInvite(w http.ResponseWriter, status int, u store.User, token string) {
	writeJSON(w, status, inviteJSON{
		User:        toUserJSON(u),
		InviteToken: token,
		SignInURL:   invite.SignInURL(a.mobileBaseURL, token),
		ExpiresIn:   store.InviteTTL.String(),
	})
}

// handleSelfInvite mints a one-time invite for the caller — the "add my next
// device" path.
//
// Deliberately not an /admin route. Adding your own second device is not
// administration: it hands the caller no authority they do not already hold,
// since the session making the request can already do everything the new one
// will. Routing it through requireOwner would leave every non-owner unable to
// pair a phone without asking, and the owner unable to do it at all.
//
// It is weaker than the session that authorises it: single-use, and expired
// after store.InviteTTL.
func (a *api) handleSelfInvite(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())

	// Retire any device invite this account minted earlier and never redeemed,
	// so "add a device" leaves exactly one live key rather than a seven-day
	// credential per tap. Nothing in the product can show or revoke a dangling
	// one — the device list holds redeemed sessions, not outstanding invites —
	// so bounding it here is all that stands between an impatient double-tap
	// and a handful of valid keys nobody can account for.
	if _, err := a.accounts.RevokeUnredeemedInvites(r.Context(), u.ID, store.InviteLabelDevice); err != nil {
		a.serverError(w, r, "revoke previous device invites", err)
		return
	}

	token, err := a.accounts.MintInvite(r.Context(), u.ID, store.InviteLabelDevice)
	if err != nil {
		a.serverError(w, r, "mint device invite", err)
		return
	}
	a.writeInvite(w, http.StatusCreated, u, token)
}

// memberJSON is one row of the account list: the account plus enough to tell an
// active one from one that was invited and never showed up.
type memberJSON struct {
	userJSON
	LastSeenAt      *time.Time `json:"last_seen_at"`
	InviteExpiresAt *time.Time `json:"invite_expires_at"`
	SessionCount    int        `json:"session_count"`
}

// handleAdminUserList returns every account. Owner only.
func (a *api) handleAdminUserList(w http.ResponseWriter, r *http.Request) {
	members, err := a.accounts.ListMembers(r.Context())
	if err != nil {
		a.serverError(w, r, "list members", err)
		return
	}
	out := make([]memberJSON, 0, len(members))
	for _, m := range members {
		out = append(out, memberJSON{
			userJSON:        toUserJSON(m.User),
			LastSeenAt:      m.LastSeenAt,
			InviteExpiresAt: m.InviteExpiresAt,
			SessionCount:    m.SessionCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdminUserCreate adds an account and returns its one-time invite. Owner
// only. `kind` may be "agent" — this is how the Scribe gets an identity of its
// own, so a discussion turn can be attributed to it rather than to the owner.
func (a *api) handleAdminUserCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Kind        string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}

	u, err := a.accounts.CreateUser(r.Context(), req.Email, strings.TrimSpace(req.DisplayName), req.Kind)
	if errors.Is(err, store.ErrDuplicateEmail) {
		http.Error(w, "email is already registered", http.StatusConflict)
		return
	}
	if err != nil {
		a.serverError(w, r, "create user", err)
		return
	}

	token, err := a.accounts.MintInvite(r.Context(), u.ID, store.TokenInvite)
	if err != nil {
		a.serverError(w, r, "mint invite", err)
		return
	}
	a.writeInvite(w, http.StatusCreated, u, token)
}

// handleAdminUserInvite mints a fresh invite for an existing account — a second
// device, or a replacement for one never redeemed. Owner only.
func (a *api) handleAdminUserInvite(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id", "user")
	if !ok {
		return
	}
	u, err := a.accounts.GetUser(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if err != nil {
		a.serverError(w, r, "get user", err)
		return
	}

	token, err := a.accounts.MintInvite(r.Context(), u.ID, store.TokenInvite)
	if err != nil {
		a.serverError(w, r, "mint invite", err)
		return
	}
	a.writeInvite(w, http.StatusCreated, u, token)
}

// handleAdminUserDelete removes an account with its credentials (FK cascade).
// The owner cannot be removed. Owner only.
func (a *api) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id", "user")
	if !ok {
		return
	}
	switch err := a.accounts.DeleteUser(r.Context(), id); {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "user not found", http.StatusNotFound)
	case errors.Is(err, store.ErrOwnerImmutable):
		http.Error(w, "the owner account cannot be removed", http.StatusForbidden)
	case err != nil:
		a.serverError(w, r, "delete user", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// sessionJSON is one signed-in device. It never carries token material — a
// session is revoked by row id, not by presenting the secret again.
type sessionJSON struct {
	ID         uuid.UUID  `json:"id"`
	DeviceName string     `json:"device_label"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	Current    bool       `json:"current"`
}

// handleSessionList returns the caller's own signed-in devices.
//
// This is the one real risk in a password-free model: a session does not
// expire, so a lost device stays signed in forever unless its holder can see it
// and cut it off. Hence the list. Sessions stay durable on purpose — CHRN-20's
// Android upload queue has to be able to drain a memo recorded weeks ago, and a
// rolling expiry would turn a backlogged upload into a silent 401.
func (a *api) handleSessionList(w http.ResponseWriter, r *http.Request) {
	sessions, err := a.accounts.ListSessions(r.Context(), userFrom(r.Context()).ID, sessionToken(r))
	if err != nil {
		a.serverError(w, r, "list sessions", err)
		return
	}
	out := make([]sessionJSON, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, sessionJSON{
			ID: s.ID, DeviceName: s.Label, CreatedAt: s.CreatedAt,
			LastSeenAt: s.LastUsedAt, Current: s.Current,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSessionRevoke signs one of the caller's own devices out. The store
// scopes the delete to the caller, so guessing another account's id reports 404
// rather than cutting off somebody else's device.
func (a *api) handleSessionRevoke(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id", "session")
	if !ok {
		return
	}
	current := sessionToken(r)
	switch err := a.accounts.RevokeSession(r.Context(), userFrom(r.Context()).ID, id); {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "session not found", http.StatusNotFound)
		return
	case err != nil:
		a.serverError(w, r, "revoke session", err)
		return
	}
	// Revoking the credential this very request rode in on is a sign-out; drop
	// the cookie too, or the browser keeps sending a token that resolves to
	// nothing.
	if current != "" {
		if _, err := a.accounts.UserByToken(r.Context(), current); errors.Is(err, store.ErrNotFound) {
			clearSessionCookie(w, r)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// pathUUID parses a {…} path value, answering 400 rather than leaking the shape
// of the id space through a lookup.
func pathUUID(w http.ResponseWriter, r *http.Request, name, what string) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		http.Error(w, "invalid "+what+" id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
