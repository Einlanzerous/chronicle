package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CHRN-33 — the memo link: what a PERSON decided a memo becomes.
//
// This is the file where derived state becomes authored state. Everything
// upstream of it is tier 1 and regenerable; a row here is not, and migration
// 0008 says so at length. What that file cannot say is the shape of the code,
// so this one says it:
//
//	T1  ClaimMemoLink    — insert the pending row. COMMIT. This is the lock.
//	T2  ResolveMemoLink  — lock link, then memo; call outward; confirm; advance.
//	    SweepMemoLink    — the same T2, for rows whose T2 never finished.
//
// THE OUTWARD CALL IS A CALLBACK INSIDE THE TRANSACTION, and that is the whole
// reason ResolveMemoLink exists rather than three exported statements a caller
// sequences. AdvanceMemoState runs on the pool, so a caller assembling this
// from existing methods would create the ticket, then advance the memo in a
// second connection with no lock between them — and a memo moved to `held` in
// that gap gets a real ticket and a failed advance, leaving the ticket behind
// with nothing pointing at it.
//
// Postgres error codes raised by tier2.memo_links_guard.
const (
	pgLinkReattributed  = "CH020"
	pgLinkConfirmedEdit = "CH021"
	pgLinkStaleKey      = "CH022"
)

// Link destinations. The same four the proposal contract carries — HOLD is an
// operator action on the memo (CHRN-34) and is never something a memo becomes.
const (
	LinkNote       = "NOTE"
	LinkTicket     = "TICKET"
	LinkDiscussion = "DISCUSSION"
	LinkDiscard    = "DISCARD"
)

// linkLockTimeout bounds how long one caller waits for another's row lock.
//
// T2 pins a pool connection across a 15-second outward call, which is the point
// — the lock is what makes the call safe. Without a deadline on the WAITING
// side, one stuck Switchyard call would queue every retry of that memo behind
// it and each waiter would hold a connection of its own while it waited. Five
// seconds is under CHRN-35's own timeout: a waiter gives up before the holder
// does, and answers `failed` for a client to retry rather than joining a queue.
// A var rather than a const ONLY so that the waiter's own deadline is testable:
// asserting it at five seconds would mean a five-second test, and an untested
// timeout is one nobody notices has stopped firing. Never changed in production.
var linkLockTimeout = "5s"

// ErrLinkLocked is returned when the lock wait expired — another batch, or the
// sweep, is mid-flight on this memo. Transient by construction: the holder is
// bounded by CHRN-35's timeout, so a retry shortly after will find it resolved.
var ErrLinkLocked = errors.New("store: memo link is locked by another decision in flight")

// ErrLinkKeyReused is the guard refusing to re-arm a refused row under the key
// that was refused. Switchyard cached that refusal; re-sending the key would
// replay it. See migration 0008 and switchyard.NewTicket.IdempotencyKey.
var ErrLinkKeyReused = errors.New("store: re-arming a refused memo link needs a fresh idempotency key")

// MemoLink is one decision, and the row that stops that memo being decided
// twice at once.
//
// INVARIANT 2 lives in what is absent. There is no Title, no Status and no
// UpdatedAt-from-Switchyard on this struct, because there is no such column:
// TicketKey is a HANDLE that a card resolves from at render time, and the
// Sent* fields are provenance of Chronicle's own output rather than a copy of
// anything upstream owns.
type MemoLink struct {
	ID     uuid.UUID
	MemoID uuid.UUID

	Destination string

	// What Chronicle put on the wire.
	SentProjectKey     string
	SentType           string
	SentTitle          string
	SentDescription    string
	SentIdempotencyKey string

	// What came back. Nil until confirmed, and TicketKey is nil forever for
	// every destination but TICKET.
	TicketKey   *string
	ConfirmedAt *time.Time

	// What the sweep found. SweptAt with no candidates is "looked, found
	// nothing" and is a different fact from "never looked".
	SweptAt       *time.Time
	CandidateKeys []string

	// Why this decision will never land.
	RefusedAt     *time.Time
	RefusedStatus *int
	RefusedReason string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Confirmed reports that the decision landed and the row is now terminal.
func (l MemoLink) Confirmed() bool { return l.ConfirmedAt != nil }

// Refused reports a decision that will never land, whatever is retried.
func (l MemoLink) Refused() bool { return l.RefusedAt != nil }

// Pending reports a decision that has neither landed nor been refused. A
// pending row is NOT A LINK — nothing renders it and nothing resolves it — so
// invariant 2 has no quarrel with one existing.
func (l MemoLink) Pending() bool { return l.ConfirmedAt == nil && l.RefusedAt == nil }

// Ambiguous reports that a sweep found more than one ticket claiming this memo.
// Confirming either would turn the other into an orphan, so it needs a person.
func (l MemoLink) Ambiguous() bool { return len(l.CandidateKeys) > 1 }

// Decision is what a person chose, as Chronicle will send it.
type Decision struct {
	MemoID      uuid.UUID
	Destination string

	// TICKET only. Empty everywhere else.
	ProjectKey  string
	Type        string
	Title       string
	Description string

	// IdempotencyKey belongs to THIS DECISION and is minted fresh for it. It
	// must never be derived from the memo — switchyard.NewTicket.IdempotencyKey
	// and migration 0008 both carry the reason at length, and the guard refuses
	// a re-arm that reuses one.
	IdempotencyKey string
}

const memoLinkColumns = `id, memo_id, destination,
	sent_project_key, sent_type, sent_title, sent_description, sent_idempotency_key,
	ticket_key, confirmed_at, swept_at, candidate_keys,
	refused_at, refused_status, refused_reason, created_at, updated_at`

func scanMemoLink(row pgx.Row) (MemoLink, error) { return scanMemoLinkWith(nil, row) }

// scanMemoLinkWith scans a link row, optionally trailing one extra boolean —
// the upsert's insert-or-update discriminator, which only ClaimMemoLink selects.
func scanMemoLinkWith(extra *bool, row pgx.Row) (MemoLink, error) {
	var l MemoLink
	var projectKey, typ, title, desc, refusedReason *string
	var refusedStatus *int32
	dst := []any{&l.ID, &l.MemoID, &l.Destination,
		&projectKey, &typ, &title, &desc, &l.SentIdempotencyKey,
		&l.TicketKey, &l.ConfirmedAt, &l.SweptAt, &l.CandidateKeys,
		&l.RefusedAt, &refusedStatus, &refusedReason, &l.CreatedAt, &l.UpdatedAt}
	if extra != nil {
		dst = append(dst, extra)
	}
	err := row.Scan(dst...)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoLink{}, ErrNotFound
	}
	if err != nil {
		return MemoLink{}, err
	}
	for dst, src := range map[*string]*string{
		&l.SentProjectKey: projectKey, &l.SentType: typ,
		&l.SentTitle: title, &l.SentDescription: desc, &l.RefusedReason: refusedReason,
	} {
		if src != nil {
			*dst = *src
		}
	}
	if refusedStatus != nil {
		n := int(*refusedStatus)
		l.RefusedStatus = &n
	}
	return l, nil
}

// LinkClaim is what one call to ClaimMemoLink actually did, and it is
// THREE-VALUED RATHER THAN TWO because the three cases do not collapse.
//
// "Did we put the decision there" is what T2 branches on for the outward call,
// and two of these answer yes. But "is there a decision worth keeping a record
// of" is a DIFFERENT question with a different answer, and a two-valued claim
// forces one of them to be wrong — a re-armed row that then has to be abandoned
// would be deleted along with the refusal it was correcting, and the operator
// would lose the account of why their first decision evaporated.
type LinkClaim string

const (
	// ClaimInserted — the row did not exist. Nothing has ever been attempted
	// for this memo, so abandoning it can leave no trace.
	ClaimInserted LinkClaim = "inserted"

	// ClaimRearmed — a REFUSED row reclaimed by a DIFFERENT decision, with a
	// fresh key. This is how an operator corrects a misfile without waiting out
	// Switchyard's 24-hour idempotency cache. The row carries history.
	ClaimRearmed LinkClaim = "rearmed"

	// ClaimExisting — somebody else's row, in whatever state. T2 must NEVER
	// create for one of these.
	ClaimExisting LinkClaim = "existing"
)

// Ours reports whether this call put the decision on the row, and therefore
// whether T2 may make the outward call for it.
func (c LinkClaim) Ours() bool { return c == ClaimInserted || c == ClaimRearmed }

// ClaimMemoLink is T1: the pending row goes in FIRST, and it is the only thing
// in the whole path that stops one memo becoming two tickets.
//
// The re-arm is an ON CONFLICT DO UPDATE with a WHERE, so it is one statement
// and one lock rather than a delete racing an insert. An IDENTICAL resend of a
// refused decision does not match that WHERE, so nothing is written and the
// caller is told the row is not theirs — which is the honest answer, because
// re-sending an unchanged decision would be refused again for the same reason.
func (s *Store) ClaimMemoLink(ctx context.Context, d Decision) (MemoLink, LinkClaim, error) {
	switch {
	case d.MemoID == uuid.Nil:
		return MemoLink{}, ClaimExisting, fmt.Errorf("%w: decision has no memo", ErrInvalidInput)
	case d.IdempotencyKey == "":
		return MemoLink{}, ClaimExisting, fmt.Errorf("%w: decision has no idempotency key", ErrInvalidInput)
	}

	// T1 TAKES THE SAME DEADLINE T2'S WAITERS DO, and it needs one for the same
	// reason one statement earlier.
	//
	// This INSERT conflicts on `UNIQUE (memo_id)`, so when another batch's T2
	// holds that row across a 15-second outward call, this statement WAITS ON
	// THE UNIQUE INDEX — not on any lock this package took, and with no
	// timeout, because a statement run on the pool inherits none. A stuck
	// Switchyard call would therefore queue every other batch's T1 for that
	// memo behind it, each holding a pool connection while it waited, and the
	// deadline in inLinkTx would never be reached to save them.
	//
	// A transaction purely to scope `SET LOCAL`. Setting it on the session
	// instead would leave it on a pooled connection for whatever ran next.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MemoLink{}, ClaimExisting, fmt.Errorf("store: claim memo link: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+linkLockTimeout+`'`); err != nil {
		return MemoLink{}, ClaimExisting, fmt.Errorf("store: claim memo link: %w", err)
	}

	var inserted bool
	l, err := scanMemoLinkWith(&inserted, tx.QueryRow(ctx,
		`INSERT INTO tier2.memo_links
		     (memo_id, destination, sent_project_key, sent_type, sent_title,
		      sent_description, sent_idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (memo_id) DO UPDATE SET
		     destination          = EXCLUDED.destination,
		     sent_project_key     = EXCLUDED.sent_project_key,
		     sent_type            = EXCLUDED.sent_type,
		     sent_title           = EXCLUDED.sent_title,
		     sent_description     = EXCLUDED.sent_description,
		     sent_idempotency_key = EXCLUDED.sent_idempotency_key,
		     -- The refusal is cleared because this is a NEW decision, and the
		     -- sweep's findings go with it: they were about the decision that
		     -- was refused, and reading them against this one would report a
		     -- candidate for a ticket nobody asked for.
		     refused_at           = NULL,
		     refused_status       = NULL,
		     refused_reason       = NULL,
		     swept_at             = NULL,
		     candidate_keys       = NULL
		 WHERE tier2.memo_links.refused_at IS NOT NULL
		   AND (tier2.memo_links.destination      IS DISTINCT FROM EXCLUDED.destination
		     OR tier2.memo_links.sent_project_key IS DISTINCT FROM EXCLUDED.sent_project_key
		     OR tier2.memo_links.sent_type        IS DISTINCT FROM EXCLUDED.sent_type
		     OR tier2.memo_links.sent_title       IS DISTINCT FROM EXCLUDED.sent_title
		     OR tier2.memo_links.sent_description IS DISTINCT FROM EXCLUDED.sent_description)
		 -- INSERT OR RE-ARM? xmax = 0 is true for a tuple this statement
		 -- inserted and false for one it updated, which is the only way to tell
		 -- the two apart from inside a single upsert: RETURNING sees the new
		 -- row, and there is no OLD to compare it against.
		 --
		 -- Its known imprecision runs in the SAFE DIRECTION. A concurrent
		 -- aborted insert can leave a non-zero xmax on a row this statement did
		 -- create, which reads here as a re-arm — and a re-arm that has to be
		 -- abandoned is MARKED rather than deleted, so the worst case is a
		 -- refusal row that could have been dropped. The reverse, deleting a
		 -- row that carried history, cannot happen.
		 RETURNING `+memoLinkColumns+`, (xmax = 0)`,
		d.MemoID, d.Destination, nullable(d.ProjectKey), nullable(d.Type),
		nullable(d.Title), nullable(d.Description), d.IdempotencyKey))

	switch {
	case err == nil:
		if err := tx.Commit(ctx); err != nil {
			return MemoLink{}, ClaimExisting, translateLinkError(err)
		}
		if inserted {
			return l, ClaimInserted, nil
		}
		return l, ClaimRearmed, nil

	case errors.Is(err, ErrNotFound):
		// The conflict fired and the WHERE did not match: somebody else's row,
		// or our own identical resend of a refused one. Read it and report it
		// as not ours.
		//
		// Read INSIDE this transaction, so the row cannot change between the
		// upsert that declined it and the read that describes it.
		existing, err := scanMemoLink(tx.QueryRow(ctx,
			`SELECT `+memoLinkColumns+` FROM tier2.memo_links WHERE memo_id = $1`, d.MemoID))
		if err != nil {
			return MemoLink{}, ClaimExisting, translateLinkError(err)
		}
		if err := tx.Commit(ctx); err != nil {
			return MemoLink{}, ClaimExisting, translateLinkError(err)
		}
		return existing, ClaimExisting, nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
		// The memo is gone, which for a caller is the same answer as a memo
		// that was never theirs to decide.
		return MemoLink{}, ClaimExisting, ErrNotFound
	}
	return MemoLink{}, ClaimExisting, translateLinkError(err)
}

// MemoLinkFor returns a memo's link row, or ErrNotFound when it has none.
func (s *Store) MemoLinkFor(ctx context.Context, memoID uuid.UUID) (MemoLink, error) {
	l, err := scanMemoLink(s.pool.QueryRow(ctx,
		`SELECT `+memoLinkColumns+` FROM tier2.memo_links WHERE memo_id = $1`, memoID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return MemoLink{}, fmt.Errorf("store: memo link: %w", err)
	}
	return l, err
}

// LinkAttempt is what T2 found once it held both locks. The caller decides from
// this and nothing else — every field it needs to branch on is here, so no
// branch can be taken against a value read before the lock.
type LinkAttempt struct {
	Link MemoLink

	// MemoState is read under the memo's own row lock, which is why the
	// memo-state check can precede the outward call rather than discovering the
	// problem afterwards from a failed advance.
	MemoState string

	// Claim is ClaimMemoLink's verdict, carried through. A row that pre-existed
	// is the sweep's business and T2 must not create for it — and a row that
	// was RE-ARMED rather than inserted carries history that must survive being
	// abandoned.
	Claim LinkClaim
}

// LinkAction is what to do with the row, decided by the caller and performed
// here so that it happens inside the transaction that holds the locks.
type LinkAction string

const (
	// LinkConfirm records the outcome and, when AdvanceTo is set, moves the
	// memo in the same transaction. Terminal: the guard refuses every later
	// edit, because `applied` answered from a stored key is only honest while
	// that key cannot change afterwards.
	LinkConfirm LinkAction = "confirm"

	// LinkRefuse marks a decision that will never land, with why. It does NOT
	// delete: a refusal is an outcome of a decision, not a reason to forget it.
	LinkRefuse LinkAction = "refuse"

	// LinkDrop deletes the row, leaving no trace and no side effect. For the
	// one case that has neither: a memo that moved out of `transcribed` between
	// the GET and the POST, where nothing was sent and nothing happened.
	LinkDrop LinkAction = "drop"

	// LinkLeave writes only the sweep's bookkeeping and leaves the row pending
	// for the next pass. A transient failure is not an outcome.
	LinkLeave LinkAction = "leave"
)

// LinkResolution is the caller's verdict on one attempt.
type LinkResolution struct {
	Action LinkAction

	// TicketKey is what Switchyard returned. Empty for the destinations that
	// make no outward call.
	TicketKey string

	// AdvanceTo moves the memo, from whatever state the attempt read under
	// lock. EMPTY LEAVES THE MEMO ALONE, which is not a degenerate case: a
	// sweep that finds the ticket for a memo since put on hold confirms the
	// link and leaves the hold standing, because the trail back matters and
	// CHRN-34's state-machine question is not this ticket's to settle.
	AdvanceTo string
	Reason    string

	// Why it was refused. RefusedStatus is Switchyard's, and nil for a refusal
	// Chronicle reached on its own.
	RefusedStatus *int
	RefusedReason string

	// Swept records that this pass looked, whatever it found. Written on every
	// action but LinkDrop.
	Swept         bool
	CandidateKeys []string
}

// ResolveMemoLink is T2: lock, decide, act, commit — one unit.
//
// LOCK ORDER IS LINK ROW, THEN MEMO ROW, on every path in this file that takes
// both. Two orders deadlock on a busy evening and Postgres kills one of them at
// random, which surfaces as a triage batch that fails a different item every
// time and cannot be reproduced.
//
// decide MAKES THE OUTWARD CALL while both locks are held. That is deliberate
// and it is what the lock is for: a concurrent accept of the same memo blocks
// here rather than racing to Switchyard, and the sweep's SKIP LOCKED reads the
// same lock as "in flight". The cost is a pinned connection for the duration of
// the call, which is why waiters get linkLockTimeout.
func (s *Store) ResolveMemoLink(ctx context.Context, memoID uuid.UUID, claim LinkClaim,
	decide func(context.Context, LinkAttempt) (LinkResolution, error)) (MemoLink, error) {
	return s.inLinkTx(ctx, func(ctx context.Context, tx pgx.Tx) (MemoLink, error) {
		link, err := scanMemoLink(tx.QueryRow(ctx,
			`SELECT `+memoLinkColumns+` FROM tier2.memo_links
			  WHERE memo_id = $1 FOR UPDATE`, memoID))
		if err != nil {
			return MemoLink{}, err
		}
		return resolveLocked(ctx, tx, link, claim, decide)
	})
}

// SweepMemoLink claims ONE unresolved row and resolves it, skipping any row
// another transaction already holds.
//
// `FOR UPDATE SKIP LOCKED`, NEVER AN AGE PREDICATE, and the difference is a
// duplicate ticket rather than a preference. A pending row is committed the
// moment T1 returns, so an age-based sweep running while that item's T2 is
// mid-call sees a row it considers abandoned, searches Switchyard by memo,
// finds nothing yet, and creates. A row locked by its own T2 IS IN FLIGHT BY
// DEFINITION, and the lock is the only thing that knows it.
//
// skip carries the rows this pass has already looked at and left pending, so
// one unreachable Switchyard does not make the sweep spin on the oldest row.
// ErrNotFound means there is nothing left to claim.
func (s *Store) SweepMemoLink(ctx context.Context, skip []uuid.UUID,
	decide func(context.Context, LinkAttempt) (LinkResolution, error)) (MemoLink, error) {
	if skip == nil {
		skip = []uuid.UUID{}
	}
	return s.inLinkTx(ctx, func(ctx context.Context, tx pgx.Tx) (MemoLink, error) {
		link, err := scanMemoLink(tx.QueryRow(ctx,
			`SELECT `+memoLinkColumns+` FROM tier2.memo_links
			  WHERE confirmed_at IS NULL AND refused_at IS NULL
			    AND NOT (id = ANY($1))
			  ORDER BY created_at
			  FOR UPDATE SKIP LOCKED
			  LIMIT 1`, skip))
		if err != nil {
			return MemoLink{}, err
		}
		// The sweep never owns a decision. It re-sends one somebody else's T2
		// wrote and did not finish, which is why the claim is `existing` and
		// why case 2 creates from the STORED decision and the STORED key rather
		// than anything it composed itself.
		return resolveLocked(ctx, tx, link, ClaimExisting, decide)
	})
}

// resolveLocked takes the memo's row lock — second, always — and applies what
// the caller decides.
func resolveLocked(ctx context.Context, tx pgx.Tx, link MemoLink, claim LinkClaim,
	decide func(context.Context, LinkAttempt) (LinkResolution, error)) (MemoLink, error) {
	var state string
	err := tx.QueryRow(ctx, `SELECT state FROM tier2.memos WHERE id = $1 FOR UPDATE`,
		link.MemoID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemoLink{}, ErrNotFound
	}
	if err != nil {
		return MemoLink{}, err
	}

	res, err := decide(ctx, LinkAttempt{Link: link, MemoState: state, Claim: claim})
	if err != nil {
		return MemoLink{}, err
	}
	return applyResolution(ctx, tx, link, state, res)
}

func applyResolution(ctx context.Context, tx pgx.Tx, link MemoLink, state string, res LinkResolution) (MemoLink, error) {
	switch res.Action {
	case LinkDrop:
		if _, err := tx.Exec(ctx, `DELETE FROM tier2.memo_links WHERE id = $1`, link.ID); err != nil {
			return MemoLink{}, err
		}
		return link, nil

	case LinkLeave:
		if !res.Swept {
			return link, nil
		}
		return scanMemoLink(tx.QueryRow(ctx,
			`UPDATE tier2.memo_links
			    SET swept_at = now(), candidate_keys = $2
			  WHERE id = $1
			 RETURNING `+memoLinkColumns, link.ID, res.CandidateKeys))

	case LinkConfirm:
		out, err := scanMemoLink(tx.QueryRow(ctx,
			`UPDATE tier2.memo_links
			    SET ticket_key     = $2,
			        confirmed_at   = now(),
			        swept_at       = CASE WHEN $3 THEN now() ELSE swept_at END,
			        candidate_keys = CASE WHEN $3 THEN $4 ELSE candidate_keys END
			  WHERE id = $1
			 RETURNING `+memoLinkColumns,
			link.ID, nullable(res.TicketKey), res.Swept, res.CandidateKeys))
		if err != nil {
			return MemoLink{}, err
		}
		if res.AdvanceTo == "" {
			return out, nil
		}
		if _, err := advanceMemoState(ctx, tx, link.MemoID, state, res.AdvanceTo, res.Reason); err != nil {
			return MemoLink{}, err
		}
		return out, nil

	case LinkRefuse:
		var status *int32
		if res.RefusedStatus != nil {
			n := int32(*res.RefusedStatus)
			status = &n
		}
		return scanMemoLink(tx.QueryRow(ctx,
			`UPDATE tier2.memo_links
			    SET refused_at     = now(),
			        refused_status = $2,
			        refused_reason = $3,
			        swept_at       = CASE WHEN $4 THEN now() ELSE swept_at END,
			        candidate_keys = CASE WHEN $4 THEN $5 ELSE candidate_keys END
			  WHERE id = $1
			 RETURNING `+memoLinkColumns,
			link.ID, status, res.RefusedReason, res.Swept, res.CandidateKeys))
	}
	return MemoLink{}, fmt.Errorf("store: unknown link action %q", res.Action)
}

// inLinkTx runs fn in a transaction with a lock deadline, and translates the
// two failures a caller has to tell apart from a server error.
func (s *Store) inLinkTx(ctx context.Context, fn func(context.Context, pgx.Tx) (MemoLink, error)) (MemoLink, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MemoLink{}, fmt.Errorf("store: memo link: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+linkLockTimeout+`'`); err != nil {
		return MemoLink{}, fmt.Errorf("store: memo link: %w", err)
	}

	link, err := fn(ctx, tx)
	if err != nil {
		return MemoLink{}, translateLinkError(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MemoLink{}, translateLinkError(err)
	}
	return link, nil
}

func translateLinkError(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrIllegalTransition) ||
		errors.Is(err, ErrLinkLocked) || errors.Is(err, ErrLinkKeyReused) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgLockNotAvailable:
			return fmt.Errorf("%w: %s", ErrLinkLocked, pgErr.Message)
		case pgIllegalTransition:
			return fmt.Errorf("%w: %s", ErrIllegalTransition, pgErr.Message)
		case pgLinkStaleKey:
			return fmt.Errorf("%w: %s", ErrLinkKeyReused, pgErr.Message)
		case pgLinkReattributed, pgLinkConfirmedEdit:
			return fmt.Errorf("store: memo link guard: %s", pgErr.Message)
		}
	}
	return fmt.Errorf("store: memo link: %w", err)
}

// SetLinkLockTimeoutForTest shortens the waiter's lock deadline and returns a
// function restoring it.
//
// EXPORTED ONLY FOR TESTS IN OTHER PACKAGES, and it exists because the
// alternative is worse: asserting the deadline at its real five seconds means a
// five-second test, and a test nobody wants to run is how a timeout stops
// firing without anybody noticing. Never called outside a test.
func SetLinkLockTimeoutForTest(d string) func() {
	prev := linkLockTimeout
	linkLockTimeout = d
	return func() { linkLockTimeout = prev }
}
