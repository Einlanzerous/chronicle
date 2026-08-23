package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CHRN-71 — accounts, invites and per-device sessions. The model is Lyceum's
// (LYCM-801), adopted deliberately rather than reinvented: two auth models on
// one estate means two sets of edge cases to debug at 1 a.m. The decision, and
// the seven places Chronicle diverges, are in
// docs/decisions/chrn-71-accounts-and-sessions.md.

// ErrDuplicateEmail is returned by CreateUser when the address is taken.
var ErrDuplicateEmail = errors.New("store: email already registered")

// ErrOwnerImmutable is returned when a caller tries to delete the owner.
var ErrOwnerImmutable = errors.New("store: the owner account cannot be removed")

// pgUniqueViolation is Postgres SQLSTATE 23505.
const pgUniqueViolation = "23505"

// Token kinds. An invite is single-use and is what gets handed out; redeeming
// it yields a session, the long-lived per-device credential clients carry.
const (
	TokenInvite  = "invite"
	TokenSession = "session"
)

// Account kinds. 'agent' is an authorship fact, not a permission level: the
// Scribe participates in discussions, so its turns need an author that is not
// the owner. It is refused ownership by a table constraint.
const (
	KindPerson = "person"
	KindAgent  = "agent"
)

// InviteLabelDevice marks an invite an account minted for its own next device
// (POST /auth/invite) rather than one the owner issued to somebody else. The
// label is what lets minting a new device key retire the previous one without
// touching an invite a third party is waiting to redeem.
const InviteLabelDevice = "device"

// tokenPrefix marks a Chronicle credential, so one is recognizable in a paste
// or a log line and is not confused with Lyceum's lyc_ tokens.
const tokenPrefix = "chr_"

// InviteTTL bounds how long an unredeemed invite stays usable. Redeeming one
// yields a session that does not expire, so an invite left in a chat log or a
// terminal scrollback would otherwise be a standing way in.
const InviteTTL = 7 * 24 * time.Hour

// User is a person or an agent with an account. Exactly one user is the owner,
// seeded by migration 0002 and reconciled from the environment at boot.
type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Kind        string
	IsOwner     bool
	CreatedAt   time.Time
}

// IsAdmin reports whether this account may reach the /admin surface. Ownership
// alone is not the test: the table forbids an agent owner today, and this keeps
// that true in code if the constraint is ever relaxed.
func (u User) IsAdmin() bool { return u.IsOwner && u.Kind == KindPerson }

// Session is one signed-in device — a row the account holder can see and
// revoke. It never carries token material.
type Session struct {
	ID         uuid.UUID
	Label      string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	Current    bool
}

// Member is an account plus the household metadata the admin list renders.
type Member struct {
	User
	LastSeenAt      *time.Time // nil when no device has ever signed in
	InviteExpiresAt *time.Time // non-nil when an unredeemed invite is outstanding
	SessionCount    int
}

const userColumns = `id, email, display_name, kind, is_owner, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Kind, &u.IsOwner, &u.CreatedAt)
	return u, err
}

// normalizeEmail lowercases and trims so lookups are case-insensitive. The
// Cloudflare Access JWT and whatever provisions an account may not agree on
// casing, and neither should be able to create a second account for one person.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// hashToken returns the hex SHA-256 of a plaintext credential. Lookups hash the
// presented token and query by hash, so the comparison is an index hit rather
// than a byte-by-byte compare of a secret.
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// newToken generates a fresh 256-bit credential as a prefixed, URL-safe string.
func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: generate token: %w", err)
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CreateUser adds an account. New accounts are never owners — the owner is
// seeded by migration 0002 and there can only be one.
func (s *Store) CreateUser(ctx context.Context, email, displayName, kind string) (User, error) {
	email = normalizeEmail(email)
	if email == "" {
		return User{}, errors.New("store: email is required")
	}
	if displayName == "" {
		displayName = email
	}
	if kind == "" {
		kind = KindPerson
	}
	if kind != KindPerson && kind != KindAgent {
		return User{}, fmt.Errorf("store: unknown account kind %q", kind)
	}

	u, err := scanUser(s.pool.QueryRow(ctx,
		`INSERT INTO tier2.users (email, display_name, kind) VALUES ($1, $2, $3)
		 RETURNING `+userColumns, email, displayName, kind))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return User{}, ErrDuplicateEmail
	}
	if err != nil {
		return User{}, fmt.Errorf("store: create user: %w", err)
	}
	return u, nil
}

// GetUser returns the account with id, or ErrNotFound.
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM tier2.users WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user: %w", err)
	}
	return u, nil
}

// GetUserByEmail returns the account with the given address, case-insensitively.
// This is the join the Cloudflare Access path makes against a verified email.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM tier2.users WHERE email = $1`, normalizeEmail(email)))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get user by email: %w", err)
	}
	return u, nil
}

// GetOwner returns the owner account, or ErrNotFound if 0002 has not run.
func (s *Store) GetOwner(ctx context.Context) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userColumns+` FROM tier2.users WHERE is_owner`))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: get owner: %w", err)
	}
	return u, nil
}

// CountUsers reports how many accounts exist.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tier2.users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

// ReconcileOwner brings the seeded owner row in line with the configured
// identity. Migration 0002 writes a placeholder; this is where the operator's
// CHRONICLE_OWNER_EMAIL / CHRONICLE_OWNER_NAME actually land.
func (s *Store) ReconcileOwner(ctx context.Context, email, displayName string) (User, error) {
	owner, err := s.GetOwner(ctx)
	if err != nil {
		return User{}, err
	}
	email = normalizeEmail(email)
	if email == "" {
		email = owner.Email
	}
	if displayName == "" {
		displayName = owner.DisplayName
	}
	if email == owner.Email && displayName == owner.DisplayName {
		return owner, nil
	}

	updated, err := scanUser(s.pool.QueryRow(ctx,
		`UPDATE tier2.users SET email = $2, display_name = $3 WHERE id = $1
		 RETURNING `+userColumns, owner.ID, email, displayName))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return owner, ErrDuplicateEmail
	}
	if err != nil {
		return User{}, fmt.Errorf("store: reconcile owner: %w", err)
	}
	return updated, nil
}

// UpdateDisplayName renames an account.
func (s *Store) UpdateDisplayName(ctx context.Context, id uuid.UUID, name string) (User, error) {
	u, err := scanUser(s.pool.QueryRow(ctx,
		`UPDATE tier2.users SET display_name = $2 WHERE id = $1
		 RETURNING `+userColumns, id, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: update display name: %w", err)
	}
	return u, nil
}

// DeleteUser removes an account and, by cascade, its credentials. The owner
// cannot be removed.
func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	var isOwner bool
	err := s.pool.QueryRow(ctx, `SELECT is_owner FROM tier2.users WHERE id = $1`, id).Scan(&isOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	if isOwner {
		return ErrOwnerImmutable
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM tier2.users WHERE id = $1`, id); err != nil {
		return fmt.Errorf("store: delete user: %w", err)
	}
	return nil
}

// ListMembers returns every account with the metadata the admin list renders:
// enough to tell an active account from one that was invited and never showed up.
func (s *Store) ListMembers(ctx context.Context) ([]Member, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.email, u.display_name, u.kind, u.is_owner, u.created_at,
		       max(t.last_used_at) FILTER (WHERE t.kind = 'session'),
		       max(t.expires_at)   FILTER (WHERE t.kind = 'invite'
		                                     AND t.used_at IS NULL
		                                     AND (t.expires_at IS NULL OR t.expires_at > now())),
		       count(t.id)         FILTER (WHERE t.kind = 'session')
		  FROM tier2.users u
		  LEFT JOIN tier2.user_tokens t ON t.user_id = u.id
		 GROUP BY u.id
		 ORDER BY u.is_owner DESC, u.created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: list members: %w", err)
	}
	defer rows.Close()

	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.ID, &m.Email, &m.DisplayName, &m.Kind, &m.IsOwner, &m.CreatedAt,
			&m.LastSeenAt, &m.InviteExpiresAt, &m.SessionCount); err != nil {
			return nil, fmt.Errorf("store: scan member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MintToken issues a credential and returns its plaintext. The plaintext is not
// recoverable afterwards: only its hash is stored, so a caller that loses it
// must mint another. expiresAt may be nil for a credential that does not expire,
// which is the normal case for a session.
func (s *Store) MintToken(ctx context.Context, userID uuid.UUID, kind, label string, expiresAt *time.Time) (string, error) {
	if kind != TokenInvite && kind != TokenSession {
		return "", fmt.Errorf("store: unknown token kind %q", kind)
	}
	plaintext, err := newToken()
	if err != nil {
		return "", err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO tier2.user_tokens (user_id, kind, token_hash, label, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		userID, kind, hashToken(plaintext), nullString(label), expiresAt); err != nil {
		return "", fmt.Errorf("store: mint token: %w", err)
	}
	return plaintext, nil
}

// MintInvite issues a single-use invite carrying the standard TTL.
func (s *Store) MintInvite(ctx context.Context, userID uuid.UUID, label string) (string, error) {
	expires := time.Now().Add(InviteTTL)
	return s.MintToken(ctx, userID, TokenInvite, label, &expires)
}

// UserByToken resolves a presented session token to its account, or ErrNotFound
// when the token is unknown, expired, or is an invite that has not been redeemed
// yet. It touches last_used_at as a side effect, which is what makes the device
// list say when a device was last seen.
func (s *Store) UserByToken(ctx context.Context, plaintext string) (User, error) {
	if plaintext == "" {
		return User{}, ErrNotFound
	}
	u, err := scanUser(s.pool.QueryRow(ctx,
		`WITH touched AS (
		     UPDATE tier2.user_tokens SET last_used_at = now()
		      WHERE token_hash = $1
		        AND kind = 'session'
		        AND (expires_at IS NULL OR expires_at > now())
		     RETURNING user_id
		 )
		 SELECT `+userColumns+` FROM tier2.users WHERE id = (SELECT user_id FROM touched)`,
		hashToken(plaintext)))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("store: user by token: %w", err)
	}
	return u, nil
}

// RedeemInvite exchanges a single-use invite for a long-lived session bound to
// the given device label, returning the account and the new session's plaintext.
//
// The claim and the mint happen in one transaction, and the claim is a
// conditional UPDATE on used_at, so two devices racing the same invite cannot
// both win. A spent, expired or unknown invite yields ErrNotFound — all three
// alike, so the caller cannot tell them apart.
func (s *Store) RedeemInvite(ctx context.Context, plaintext, deviceLabel string) (User, string, error) {
	if plaintext == "" {
		return User{}, "", ErrNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, "", fmt.Errorf("store: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userID uuid.UUID
	err = tx.QueryRow(ctx,
		`UPDATE tier2.user_tokens SET used_at = now()
		  WHERE token_hash = $1
		    AND kind = 'invite'
		    AND used_at IS NULL
		    AND (expires_at IS NULL OR expires_at > now())
		 RETURNING user_id`, hashToken(plaintext)).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("store: claim invite: %w", err)
	}

	session, err := newToken()
	if err != nil {
		return User{}, "", err
	}
	// last_used_at is stamped now rather than left NULL until the first
	// authenticated request: redeeming an invite IS signing in. Without this a
	// device that just paired reads as "never signed in" until it happens to
	// make another call.
	if _, err := tx.Exec(ctx,
		`INSERT INTO tier2.user_tokens (user_id, kind, token_hash, label, last_used_at)
		 VALUES ($1, 'session', $2, $3, now())`,
		userID, hashToken(session), nullString(deviceLabel)); err != nil {
		return User{}, "", fmt.Errorf("store: mint session: %w", err)
	}

	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM tier2.users WHERE id = $1`, userID))
	if err != nil {
		return User{}, "", fmt.Errorf("store: load redeeming user: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, "", fmt.Errorf("store: commit redeem: %w", err)
	}
	return u, session, nil
}

// RevokeToken deletes the credential with the given plaintext, whoever holds it.
// This is how a client signs out: the token it carries stops working
// immediately and other devices are untouched. Revoking an unknown token is a
// no-op rather than an error — the caller's intent is satisfied either way.
func (s *Store) RevokeToken(ctx context.Context, plaintext string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM tier2.user_tokens WHERE token_hash = $1`, hashToken(plaintext)); err != nil {
		return fmt.Errorf("store: revoke token: %w", err)
	}
	return nil
}

// RevokeUnredeemedInvites deletes an account's outstanding, still-redeemable
// invites carrying the given label and reports how many went. Redeemed ones are
// left alone: they are spent, and the sessions they became are revoked from the
// device list rather than from here.
//
// This is what keeps self-minted device keys to one live credential per account.
// Minting is otherwise insert-only, so tapping "add a device" three times would
// leave three seven-day keys valid with nothing able to show or revoke them —
// the device list holds redeemed sessions, not outstanding invites.
func (s *Store) RevokeUnredeemedInvites(ctx context.Context, userID uuid.UUID, label string) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tier2.user_tokens
		  WHERE user_id = $1 AND kind = 'invite' AND label = $2 AND used_at IS NULL`,
		userID, label)
	if err != nil {
		return 0, fmt.Errorf("store: revoke unredeemed invites: %w", err)
	}
	return tag.RowsAffected(), nil
}

// CountTokens reports how many live credentials of a kind an account holds.
// Boot uses it to decide whether the owner still needs a first sign-in invite.
func (s *Store) CountTokens(ctx context.Context, userID uuid.UUID, kind string) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tier2.user_tokens
		  WHERE user_id = $1 AND kind = $2 AND used_at IS NULL
		    AND (expires_at IS NULL OR expires_at > now())`,
		userID, kind).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count tokens: %w", err)
	}
	return n, nil
}

// ListSessions returns an account's signed-in devices, marking whichever one
// carries the presented token as current.
func (s *Store) ListSessions(ctx context.Context, userID uuid.UUID, currentPlaintext string) ([]Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, coalesce(label, ''), created_at, last_used_at, token_hash = $2
		   FROM tier2.user_tokens
		  WHERE user_id = $1 AND kind = 'session'
		  ORDER BY created_at`,
		userID, hashToken(currentPlaintext))
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Label, &sess.CreatedAt, &sess.LastUsedAt, &sess.Current); err != nil {
			return nil, fmt.Errorf("store: scan session: %w", err)
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// RevokeSession signs one of an account's own devices out. The delete is scoped
// to the caller, so guessing another account's session id reports ErrNotFound
// rather than cutting off somebody else's device.
func (s *Store) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tier2.user_tokens
		  WHERE id = $1 AND user_id = $2 AND kind = 'session'`, sessionID, userID)
	if err != nil {
		return fmt.Errorf("store: revoke session: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
