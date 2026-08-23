package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestStore returns a store on a freshly migrated database, or skips.
func newTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := testDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	// Start from empty so the owner seed and the tests are not order-dependent.
	if err := MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(pool), ctx
}

func TestMigrationSeedsExactlyOneOwner(t *testing.T) {
	s, ctx := newTestStore(t)

	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if !owner.IsOwner || owner.Kind != KindPerson {
		t.Errorf("owner = %+v, want is_owner with kind person", owner)
	}
	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 1 {
		t.Errorf("accounts after migrate = %d, want 1", n)
	}
}

// Done when #1: an invite redeems into a per-device session.
func TestRedeemInviteYieldsASession(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	inviteTok, err := s.MintInvite(ctx, owner.ID, "test")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if !strings.HasPrefix(inviteTok, tokenPrefix) {
		t.Errorf("invite %q lacks the %q prefix", inviteTok, tokenPrefix)
	}

	// An invite is not a session: presenting it directly must authenticate
	// nothing, or the redemption step is decorative.
	if _, err := s.UserByToken(ctx, inviteTok); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByToken(invite) = %v, want ErrNotFound", err)
	}

	u, session, err := s.RedeemInvite(ctx, inviteTok, "Pixel 8")
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	if u.ID != owner.ID {
		t.Errorf("redeemed as %v, want %v", u.ID, owner.ID)
	}
	if session == inviteTok {
		t.Error("the session token is the invite token; they must be distinct credentials")
	}

	back, err := s.UserByToken(ctx, session)
	if err != nil {
		t.Fatalf("UserByToken(session): %v", err)
	}
	if back.ID != owner.ID {
		t.Errorf("session resolves to %v, want %v", back.ID, owner.ID)
	}

	sessions, err := s.ListSessions(ctx, owner.ID, session)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].Label != "Pixel 8" || !sessions[0].Current {
		t.Errorf("session = %+v, want the current device labelled Pixel 8", sessions[0])
	}
	// Redeeming an invite IS signing in; a device that just paired must not
	// read as "never signed in".
	if sessions[0].LastUsedAt == nil {
		t.Error("last_used_at is nil on a session that was just redeemed")
	}
}

// Done when #2: a second device needs a second invite — and the single-use
// guarantee has to hold against two devices racing the same one, which is the
// case a non-transactional check-then-update would lose.
func TestInviteIsSingleUseUnderARace(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	inviteTok, err := s.MintInvite(ctx, owner.ID, "test")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	const racers = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		wins     int
		sessions []string
	)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, session, err := s.RedeemInvite(ctx, inviteTok, "racer")
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
				sessions = append(sessions, session)
			} else if !errors.Is(err, ErrNotFound) {
				t.Errorf("loser got %v, want ErrNotFound", err)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("%d of %d racers redeemed the same invite; want exactly 1", wins, racers)
	}

	// And the second device genuinely needs its own invite.
	if _, _, err := s.RedeemInvite(ctx, inviteTok, "second device"); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-redeeming a spent invite = %v, want ErrNotFound", err)
	}
	second, err := s.MintInvite(ctx, owner.ID, "test")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if _, _, err := s.RedeemInvite(ctx, second, "second device"); err != nil {
		t.Fatalf("second invite: %v", err)
	}
	list, err := s.ListSessions(ctx, owner.ID, sessions[0])
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("sessions = %d, want 2", len(list))
	}
}

// Done when #3: signing out one device leaves the others working.
func TestSignOutIsPerDevice(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	phone, laptop := redeemFresh(t, s, ctx, owner.ID, "phone"), redeemFresh(t, s, ctx, owner.ID, "laptop")

	if err := s.RevokeToken(ctx, phone); err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if _, err := s.UserByToken(ctx, phone); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoked token still resolves: %v", err)
	}
	if _, err := s.UserByToken(ctx, laptop); err != nil {
		t.Errorf("the other device stopped working after one signed out: %v", err)
	}
}

// Revoking by row id is scoped to the caller: guessing another account's
// session id must not cut off their device.
func TestRevokeSessionIsScopedToItsOwner(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	other, err := s.CreateUser(ctx, "Other@Example.com", "Other", KindPerson)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Normalized on the way in, so two casings cannot become two accounts.
	if other.Email != "other@example.com" {
		t.Errorf("email = %q, want it lowercased", other.Email)
	}

	victim := redeemFresh(t, s, ctx, owner.ID, "owner phone")
	list, err := s.ListSessions(ctx, owner.ID, victim)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	if _, err := s.RevokeSession(ctx, other.ID, list[0].ID, ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("cross-account revoke = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByToken(ctx, victim); err != nil {
		t.Errorf("the owner's device was revoked by another account: %v", err)
	}

	// Revoking your own device reports whether it was the one you are holding,
	// which is what the handler uses to decide about the cookie.
	wasCurrent, err := s.RevokeSession(ctx, owner.ID, list[0].ID, victim)
	if err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if !wasCurrent {
		t.Error("revoking the calling device did not report itself as current")
	}
}

// The retire-and-mint pair is one transaction, so concurrent taps cannot leave
// two live device keys — the failure the retirement exists to prevent.
func TestReplaceDeviceInviteIsAtomicUnderConcurrency(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	const taps = 6
	var wg sync.WaitGroup
	tokens := make([]string, taps)
	for i := 0; i < taps; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tok, err := s.ReplaceDeviceInvite(ctx, owner.ID)
			if err != nil {
				t.Errorf("tap %d: %v", i, err)
				return
			}
			tokens[i] = tok
		}(i)
	}
	wg.Wait()

	var live int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tier2.user_tokens
		  WHERE user_id = $1 AND kind = 'invite' AND label = $2 AND used_at IS NULL`,
		owner.ID, InviteLabelDevice).Scan(&live); err != nil {
		t.Fatalf("count: %v", err)
	}
	if live != 1 {
		t.Errorf("%d live device invites after %d concurrent taps; want exactly 1", live, taps)
	}

	// And exactly one of the handed-out tokens still redeems.
	var redeemable int
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if _, _, err := s.RedeemInvite(ctx, tok, "device"); err == nil {
			redeemable++
		}
	}
	if redeemable != 1 {
		t.Errorf("%d of the issued device invites still redeem; want exactly 1", redeemable)
	}
}

// last_used_at is throttled, so the auth middleware is not a write per request,
// but it still moves when it should.
func TestUserByTokenThrottlesTheLastUsedWrite(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	session := redeemFresh(t, s, ctx, owner.ID, "phone")

	read := func() time.Time {
		t.Helper()
		var ts time.Time
		if err := s.Pool().QueryRow(ctx,
			`SELECT last_used_at FROM tier2.user_tokens WHERE token_hash = $1`,
			hashToken(session)).Scan(&ts); err != nil {
			t.Fatalf("read last_used_at: %v", err)
		}
		return ts
	}

	first := read()
	for i := 0; i < 3; i++ {
		if _, err := s.UserByToken(ctx, session); err != nil {
			t.Fatalf("UserByToken: %v", err)
		}
	}
	if got := read(); !got.Equal(first) {
		t.Errorf("last_used_at moved on a request inside the throttle window: %v -> %v", first, got)
	}

	// Age the row past the window; the next authenticated request stamps it.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.user_tokens SET last_used_at = now() - $2::interval * 2
		  WHERE token_hash = $1`, hashToken(session), sessionTouchInterval.String()); err != nil {
		t.Fatalf("age the row: %v", err)
	}
	aged := read()
	if _, err := s.UserByToken(ctx, session); err != nil {
		t.Fatalf("UserByToken: %v", err)
	}
	if got := read(); !got.After(aged) {
		t.Errorf("last_used_at did not move once past the throttle window: %v -> %v", aged, got)
	}
}

// ReconcileOwner with no configured name must land the EMAIL, not migration
// 0002's "Owner" placeholder — the decision doc documents the email as the
// default and this is the case that actually happens.
func TestReconcileOwnerDefaultsTheNameToTheEmail(t *testing.T) {
	s, ctx := newTestStore(t)

	// Fresh install, CHRONICLE_OWNER_NAME unset: the placeholder must go.
	owner, err := s.ReconcileOwner(ctx, "magos@example.com", "")
	if err != nil {
		t.Fatalf("ReconcileOwner: %v", err)
	}
	if owner.DisplayName == OwnerPlaceholderName {
		t.Error("the owner kept migration 0002's placeholder display name")
	}
	if owner.DisplayName != "magos@example.com" {
		t.Errorf("DisplayName = %q, want the email", owner.DisplayName)
	}

	// A name the person chose survives every subsequent boot. Resetting it to
	// the email each restart would silently undo PATCH /auth/me.
	if _, err := s.UpdateDisplayName(ctx, owner.ID, "Magos"); err != nil {
		t.Fatalf("UpdateDisplayName: %v", err)
	}
	again, err := s.ReconcileOwner(ctx, "magos@example.com", "")
	if err != nil {
		t.Fatalf("ReconcileOwner: %v", err)
	}
	if again.DisplayName != "Magos" {
		t.Errorf("DisplayName = %q after a restart; the chosen name was overwritten", again.DisplayName)
	}
}

// Done when #5, at the store layer: unknown, spent and expired invites are one
// error, so nothing above can accidentally tell them apart.
func TestBadInvitesAreIndistinguishable(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	spent, err := s.MintInvite(ctx, owner.ID, "test")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if _, _, err := s.RedeemInvite(ctx, spent, "first"); err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}

	past := time.Now().Add(-time.Hour)
	expired, err := s.MintToken(ctx, owner.ID, TokenInvite, "stale", &past)
	if err != nil {
		t.Fatalf("MintToken: %v", err)
	}

	for name, token := range map[string]string{
		"unknown": tokenPrefix + "nothingHereAtAll",
		"spent":   spent,
		"expired": expired,
		"empty":   "",
	} {
		if _, _, err := s.RedeemInvite(ctx, token, "probe"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s invite = %v, want ErrNotFound like every other bad invite", name, err)
		}
	}
}

// An account's own device key is bounded to one live invite, so an impatient
// double-tap cannot leave a handful of seven-day credentials nothing can show
// or revoke.
func TestSelfInviteRetiresThePreviousUnredeemedOne(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	first, err := s.MintInvite(ctx, owner.ID, InviteLabelDevice)
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	// An invite issued to somebody else must survive: revoking one out from
	// under a person mid-redemption would be its own bug.
	guest, err := s.CreateUser(ctx, "guest@example.com", "Guest", KindPerson)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	guestInvite, err := s.MintInvite(ctx, guest.ID, InviteLabelIssued)
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	second, err := s.ReplaceDeviceInvite(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ReplaceDeviceInvite: %v", err)
	}

	if _, _, err := s.RedeemInvite(ctx, first, "stale"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the retired invite still redeems: %v", err)
	}
	if _, _, err := s.RedeemInvite(ctx, second, "current"); err != nil {
		t.Errorf("the current device invite does not redeem: %v", err)
	}
	if _, _, err := s.RedeemInvite(ctx, guestInvite, "guest"); err != nil {
		t.Errorf("another account's invite was revoked as collateral: %v", err)
	}
}

func TestOwnerCannotBeDeletedAndAgentsCannotOwn(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if err := s.DeleteUser(ctx, owner.ID); !errors.Is(err, ErrOwnerImmutable) {
		t.Errorf("DeleteUser(owner) = %v, want ErrOwnerImmutable", err)
	}

	// The Scribe's account: an authorship identity, never an administrator.
	scribe, err := s.CreateUser(ctx, "scribe@chronicle.local", "Scribe", KindAgent)
	if err != nil {
		t.Fatalf("CreateUser(agent): %v", err)
	}
	if scribe.IsAdmin() {
		t.Error("a fresh agent account reports as an administrator")
	}
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.users SET is_owner = true WHERE id = $1`, scribe.ID); err == nil {
		t.Error("an agent was made owner; the users_owner_is_a_person constraint is not holding")
	}

	// Deleting an account takes its credentials with it.
	session := redeemFresh(t, s, ctx, scribe.ID, "mcp")
	if err := s.DeleteUser(ctx, scribe.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if _, err := s.UserByToken(ctx, session); !errors.Is(err, ErrNotFound) {
		t.Errorf("a deleted account's session still resolves: %v", err)
	}
}

func TestListMembersReportsHouseholdState(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	redeemFresh(t, s, ctx, owner.ID, "laptop")

	pending, err := s.CreateUser(ctx, "pending@example.com", "Pending", KindPerson)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.MintInvite(ctx, pending.ID, InviteLabelIssued); err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	members, err := s.ListMembers(ctx)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2", len(members))
	}
	if !members[0].IsOwner {
		t.Error("the owner is not listed first")
	}
	if members[0].SessionCount != 1 || members[0].LastSeenAt == nil {
		t.Errorf("owner row = %+v, want one session and a last-seen time", members[0])
	}
	if members[1].SessionCount != 0 || members[1].LastSeenAt != nil {
		t.Errorf("pending row = %+v, want no sessions and never seen", members[1])
	}
	if members[1].InviteExpiresAt == nil {
		t.Error("an outstanding invite is not reported, so the row cannot show as pending")
	}
}

func TestReconcileOwnerAppliesConfiguredIdentity(t *testing.T) {
	s, ctx := newTestStore(t)

	owner, err := s.ReconcileOwner(ctx, "  Magos@Example.com ", "Magos")
	if err != nil {
		t.Fatalf("ReconcileOwner: %v", err)
	}
	if owner.Email != "magos@example.com" || owner.DisplayName != "Magos" {
		t.Errorf("owner = %+v, want the configured identity, normalized", owner)
	}

	// Empty values leave a chosen name alone rather than resetting it: boot
	// must not undo PATCH /auth/me on every restart.
	again, err := s.ReconcileOwner(ctx, "", "")
	if err != nil {
		t.Fatalf("ReconcileOwner: %v", err)
	}
	if again.Email != "magos@example.com" || again.DisplayName != "Magos" {
		t.Errorf("owner = %+v after an empty reconcile", again)
	}

	// A typo'd address that belongs to someone else is reported, not applied.
	if _, err := s.CreateUser(ctx, "taken@example.com", "Taken", KindPerson); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.ReconcileOwner(ctx, "taken@example.com", ""); !errors.Is(err, ErrDuplicateEmail) {
		t.Errorf("ReconcileOwner onto a taken address = %v, want ErrDuplicateEmail", err)
	}
}

// Done when #6, and the assertion CHRN-52 will own: the tier-1 role cannot read
// the credentials table. Doctrine says a tier-1 write path must not reach a
// tier-2 table, and a separate role is the enforcement mechanism — this is what
// proves the mechanism is actually wired, not merely intended.
//
// Needs a DSN for chronicle_tier1; skips without one rather than passing
// vacuously.
func TestTier1RoleCannotReachCredentials(t *testing.T) {
	s, ctx := newTestStore(t)
	_ = s

	dsn := os.Getenv("CHRONICLE_TEST_TIER1_DATABASE_URL")
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_TIER1_DATABASE_URL not set; skipping tier-isolation test")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as chronicle_tier1: %v", err)
	}
	defer pool.Close()

	// Positive control first. Without it every assertion below would also pass
	// on a connection that never worked at all, and a test that cannot fail is
	// worse here than no test — this is the one guarding the doctrine.
	var role string
	if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatalf("the tier-1 connection does not work, so nothing below proves anything: %v", err)
	}
	if role != "chronicle_tier1" {
		t.Fatalf("connected as %q, want chronicle_tier1", role)
	}
	var canReachTier1 bool
	if err := pool.QueryRow(ctx,
		`SELECT has_schema_privilege(current_user, 'tier1', 'USAGE')`).Scan(&canReachTier1); err != nil {
		t.Fatalf("check tier1 privilege: %v", err)
	}
	if !canReachTier1 {
		t.Fatal("chronicle_tier1 cannot reach its own schema; the grants in 0001 are not applied")
	}

	// Every probe scans into `any`. An earlier version scanned all three into an
	// int, which made the third one — `SELECT token_hash`, the query that most
	// directly expresses the invariant — unable to fail: scanning a TEXT column
	// into an int errors whatever the privileges are, so `err == nil` was
	// unreachable and the assertion passed for the wrong reason.
	for _, q := range []string{
		`SELECT count(*) FROM tier2.user_tokens`,
		`SELECT count(*) FROM tier2.users`,
		`SELECT token_hash FROM tier2.user_tokens LIMIT 1`,
		`SELECT email FROM tier2.users WHERE is_owner`,
	} {
		var got any
		err := pool.QueryRow(ctx, q).Scan(&got)
		if err == nil {
			t.Errorf("chronicle_tier1 executed %q; tier-2 credentials are reachable from a tier-1 path", q)
			continue
		}
		// The refusal has to be a permission refusal. "relation does not exist"
		// would also be a non-nil error, and would mean this test is passing
		// because the migration never ran.
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("chronicle_tier1 running %q failed with %v; want a permission denial", q, err)
		}
	}
}

// redeemFresh mints an invite and immediately redeems it, returning the session.
func redeemFresh(t *testing.T, s *Store, ctx context.Context, userID uuid.UUID, label string) string {
	t.Helper()
	inviteTok, err := s.MintInvite(ctx, userID, "test")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	_, session, err := s.RedeemInvite(ctx, inviteTok, label)
	if err != nil {
		t.Fatalf("RedeemInvite: %v", err)
	}
	return session
}
