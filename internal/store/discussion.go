package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CHRN-43 — threads, turns, participants, and the rule that an agent may not
// speak twice in a row. Decided in Mode B: the Switchyard plan on CHRN-43,
// revision 2, approved 2026-09-07 with all six rulings picked.
// 0015_discussions.up.sql carries the argument.
//
// THE THREE PROPERTIES THIS FILE DOES NOT ENFORCE, because the store does:
// a turn is immutable (CH090), an agent turn requires a person's turn
// immediately before it (CH091), and a resolved thread takes no more turns
// (CH093). CHRN-67's MCP write tools are a second caller by design, so a rule
// that lived here would be one they do not inherit. What this file owns is the
// ORDERING — seq is allocated under the thread's row lock — and the mapping
// from those SQLSTATEs onto errors a handler can act on.

// ErrTurnImmutable is returned when something tries to rewrite a turn. It
// should be unreachable through this package, which never issues such a
// statement; it exists so that the day something does, the error names the
// rule rather than a SQLSTATE.
var ErrTurnImmutable = errors.New("store: a discussion turn is insert-only")

// ErrAgentMayNotFollowAgent is CH091 — ruling 3. An agent turn requires the
// immediately preceding turn to be a person's, which also means an agent
// cannot open a thread. Reachable through this package by design: CHRN-47
// posts an agent turn and has to handle the refusal.
var ErrAgentMayNotFollowAgent = errors.New("store: an agent turn must follow a person's turn")

// ErrAuthorKindDerived is CH092. author_kind is set from the author's account
// by the trigger and is never a caller's to supply; this package does not send
// the column at all, so it exists to name the rule for a direct writer.
var ErrAuthorKindDerived = errors.New("store: a turn's author kind is derived, not supplied")

// ErrDiscussionResolved is CH093 — ruling 6. A resolved thread takes no more
// turns. Continuing means opening a new thread that cites it, because CH080
// makes un-resolving impossible by design.
//
// CHRN-47 has to handle this: an agent that started thinking about a reply
// before the thread resolved will arrive after it did.
var ErrDiscussionResolved = errors.New("store: that discussion is resolved")

// ErrResolutionFixed is CH080's once-from-NULL clause. A recorded resolution is
// never rewritten or withdrawn — only completed, by linking a note to a thread
// that resolved without one.
var ErrResolutionFixed = errors.New("store: a resolution is recorded once and is not rewritten")

// ErrDiscussionColumnFrozen is CH080's allow list. Unreachable through this
// package, which updates only the columns it names; it exists so that the day a
// new column is added and written, the error says which.
var ErrDiscussionColumnFrozen = errors.New("store: that column of a discussion is not writable")

// ErrParticipantColumnFrozen is CH100's allow list — added_at and added_by mean
// FIRST added, and a re-add clears the removed pair without reattributing the
// original invitation. Unreachable through this package for the same reason.
var ErrParticipantColumnFrozen = errors.New("store: that column of a participant row is not writable")

// ErrInvalidDiscussionRef is returned by ParseDiscussionRef for anything that
// is not a discussion reference.
var ErrInvalidDiscussionRef = errors.New("store: not a discussion reference")

// ErrMemoAlreadySpoke is returned when a second turn claims a memo that already
// authored one. A memo lands exactly once — the cardinality tier2.memo_links
// asserts about the decision, said here about the turn that landing produced.
var ErrMemoAlreadySpoke = errors.New("store: that memo has already produced a turn")

// Discussion-guard SQLSTATEs, one block per table — see 0015 PART 4.
const (
	pgDiscussionGuard    = "CH080"
	pgTurnInsertOnly     = "CH090"
	pgAgentAfterAgent    = "CH091"
	pgAuthorKindSupplied = "CH092"
	pgTurnOnResolved     = "CH093"
	pgParticipantGuard   = "CH100"
)

// CH080 and CH100 each cover two rules, because the plan's error table gives
// one code per TABLE and criterion 18 asserts it. "You named an agent" and
// "that is already recorded" are different answers a handler owes a caller, so
// the guards name the arm in CONSTRAINT and this package reads it — rather than
// matching on the message, which is a string comparison against a sentence.
const (
	conDiscussionAllowList  = "discussions_update_allow_list"
	conResolutionOnce       = "discussions_resolution_once"
	conResolverIsAPerson    = "discussions_resolver_is_a_person"
	conParticipantAllowList = "discussion_participants_update_allow_list"
	conParticipantIsAPerson = "discussion_participants_actor_is_a_person"
)

// discussionRefPattern parses LENIENTLY and Format renders STRICTLY, as
// noteRefPattern does and for the same reason: people quote handles by hand, so
// `DSC-7`, `dsc-0007` and `DSC-00007` all have to reach discussion 7. A prefix
// is still required — a bare number would make every integer anybody writes a
// discussion reference.
var discussionRefPattern = regexp.MustCompile(`^(?i:dsc)-([0-9]+)$`)

// discussionRefWidth is the MINIMUM rendered width, not a cap — noteRefWidth's
// rule, so the corpus does not stop at 9999.
const discussionRefWidth = 4

// Discussion is one threaded conversation. Its content is its DiscussionTurns.
//
// THERE IS NO opened_by AND NO memo_id ON THIS TYPE, and their absence is the
// decision rather than an omission: the memo that opened a thread is the memo
// of turn 1 and the opener is turn 1's author, so a column for either would be
// the same fact written twice. Worse, an unconstrained opened_by could name an
// agent while a person authored turn 1 — making "an agent cannot open a
// thread" true of the turns and false of the column.
type Discussion struct {
	ID     uuid.UUID
	Number int64
	// PageID is nullable, unlike a note's. A note is ADDRESSED by its page; a
	// thread is not addressed at all until it resolves.
	PageID    *uuid.UUID
	Title     string
	CreatedAt time.Time

	// ResolvedAt and ResolvedBy are set together or not at all, and
	// ResolvedNoteID is optional even when they are: some threads just end,
	// and CHRN-46 wants that to be a deliberate choice rather than an error.
	ResolvedAt     *time.Time
	ResolvedBy     *uuid.UUID
	ResolvedNoteID *uuid.UUID
}

// Ref renders the thread's permanent handle, DSC-0007.
func (d Discussion) Ref() string { return FormatDiscussionRef(d.Number) }

// Resolved reports whether the thread has concluded.
func (d Discussion) Resolved() bool { return d.ResolvedAt != nil }

// DiscussionTurn is one thing said in a thread.
type DiscussionTurn struct {
	ID           uuid.UUID
	DiscussionID uuid.UUID

	// Seq is server-assigned under the thread's row lock and is the ONLY
	// ordering. It is also what makes CHRN-45 cheap: a read marker is one
	// integer and an unread count is one comparison.
	Seq int

	AuthorID uuid.UUID

	// AuthorKind is the author's kind AT THE TIME THE TURN WAS WRITTEN, set by
	// the trigger from tier2.users.kind (ruling 5). Denormalised on purpose:
	// CH091 is a safety rule and users.kind is mutable, so reading it live
	// would let an account edit rewrite the verdict on turns already written.
	AuthorKind string

	Body      string
	CreatedAt time.Time

	// ComposedAt is the client's claim about when this was written — the phone
	// that was offline for six hours. ADVISORY: unverifiable, nullable, and
	// never sorted on.
	ComposedAt *time.Time

	MemoID *uuid.UUID
}

// ByAgent reports whether an agent wrote this turn. Read from the row, with no
// join to a mutable column — which is what CHRN-44's "the distinction survives
// export" asks for.
func (t DiscussionTurn) ByAgent() bool { return t.AuthorKind == KindAgent }

// DiscussionParticipant is who is expected to READ a thread — CHRN-45's
// question, not "who may write". Nothing requires a turn's author to be a
// participant.
type DiscussionParticipant struct {
	DiscussionID uuid.UUID
	UserID       uuid.UUID

	// AddedAt and AddedBy mean FIRST added and are frozen by CH100: a re-add
	// after a removal clears the removed pair and does not reattribute the
	// original invitation.
	AddedAt time.Time
	AddedBy uuid.UUID

	RemovedAt *time.Time
	RemovedBy *uuid.UUID
}

// Active reports whether the participant is currently on the thread.
func (p DiscussionParticipant) Active() bool { return p.RemovedAt == nil }

// NewDiscussion opens a thread and its first turn together.
type NewDiscussion struct {
	Title  string
	PageID *uuid.UUID

	// AuthorID authors turn 1, and it is where "an agent cannot open a thread"
	// is enforced: CH091 refuses an agent at seq 1 because there is no
	// preceding person.
	AuthorID uuid.UUID
	Body     string

	MemoID     *uuid.UUID
	ComposedAt *time.Time
}

// NewTurn is the input to AppendTurn.
//
// THERE IS NO Seq FIELD. The server allocates it under the thread's row lock,
// which is what makes a thread read in the same order everywhere.
//
// THERE IS NO AuthorKind FIELD EITHER — CH092 refuses a supplied one.
type NewTurn struct {
	DiscussionID uuid.UUID
	AuthorID     uuid.UUID
	Body         string

	MemoID *uuid.UUID

	// ComposedAt is advisory and orders nothing. A turn composed six hours ago
	// still lands at the end of the thread, because where it belongs is where
	// it arrived — see 0015's `ordering`.
	ComposedAt *time.Time
}

// FormatDiscussionRef renders a discussion number as it is written and spoken.
func FormatDiscussionRef(number int64) string {
	return fmt.Sprintf("DSC-%0*d", discussionRefWidth, number)
}

// ParseDiscussionRef reads a discussion reference, leniently.
//
// IT PARSES RATHER THAN PATTERN-MATCHES, and that is CHRN-94's lesson rather
// than a style choice: a value that passes a regex and fails ParseInt is
// well-formed in one place and unresolvable in the other. DSC-99999999999999999999
// matches the pattern and overflows an int64, and DSC-0 matches and names a
// number the sequence never issues.
func ParseDiscussionRef(s string) (int64, error) {
	m := discussionRefPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidDiscussionRef, s)
	}
	// Leading zeros are ignored by base-10 parsing, so DSC-00007 and DSC-7 are
	// the same thread.
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %v", ErrInvalidDiscussionRef, s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %q: discussion numbers start at 1", ErrInvalidDiscussionRef, s)
	}
	return n, nil
}

const discussionColumns = `id, number, page_id, title, created_at, resolved_at, resolved_by, resolved_note_id`

// discussionDest is the single place discussionColumns is unpacked — noteDest's
// reason: a column added to the list cannot be picked up by one reader and
// missed by another.
func discussionDest(d *Discussion) []any {
	return []any{&d.ID, &d.Number, &d.PageID, &d.Title, &d.CreatedAt,
		&d.ResolvedAt, &d.ResolvedBy, &d.ResolvedNoteID}
}

func scanDiscussion(row pgx.Row) (Discussion, error) {
	var d Discussion
	err := row.Scan(discussionDest(&d)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Discussion{}, ErrNotFound
	}
	if err != nil {
		return Discussion{}, discussionError(err)
	}
	return d, nil
}

const turnColumns = `id, discussion_id, seq, author_id, author_kind, body, created_at, composed_at, memo_id`

func turnDest(t *DiscussionTurn) []any {
	return []any{&t.ID, &t.DiscussionID, &t.Seq, &t.AuthorID, &t.AuthorKind,
		&t.Body, &t.CreatedAt, &t.ComposedAt, &t.MemoID}
}

func scanTurn(row pgx.Row) (DiscussionTurn, error) {
	var t DiscussionTurn
	err := row.Scan(turnDest(&t)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return DiscussionTurn{}, ErrNotFound
	}
	if err != nil {
		return DiscussionTurn{}, discussionError(err)
	}
	return t, nil
}

const participantColumns = `discussion_id, user_id, added_at, added_by, removed_at, removed_by`

func participantDest(p *DiscussionParticipant) []any {
	return []any{&p.DiscussionID, &p.UserID, &p.AddedAt, &p.AddedBy,
		&p.RemovedAt, &p.RemovedBy}
}

// OpenDiscussion writes a thread and its first turn in one transaction.
//
// ONE TRANSACTION, on CreateNote's precedent and for the same reason: a thread
// with no turns is a state no reader should have to handle, and it is the only
// state in which "who opened this" has no answer.
//
// IT RETURNS THE TURN, and that is not a convenience. tier2.discussions has no
// opened_by and no memo_id, because both are turn 1's and a second copy would
// be a second source of truth — so the turn is where a caller reads the opener
// and the opening memo.
func (s *Store) OpenDiscussion(ctx context.Context, in NewDiscussion) (Discussion, DiscussionTurn, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Discussion{}, DiscussionTurn{}, fmt.Errorf("store: open discussion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	d, err := scanDiscussion(tx.QueryRow(ctx, `
		INSERT INTO tier2.discussions (page_id, title)
		VALUES ($1, $2)
		RETURNING `+discussionColumns,
		in.PageID, in.Title))
	if err != nil {
		return Discussion{}, DiscussionTurn{}, err
	}

	// seq 1 literally rather than through MAX+1: the thread was created one
	// statement ago and has no turns, and no lock can make that more true.
	t, err := scanTurn(tx.QueryRow(ctx, `
		INSERT INTO tier2.discussion_turns
		            (discussion_id, seq, author_id, body, composed_at, memo_id)
		VALUES ($1, 1, $2, $3, $4, $5)
		RETURNING `+turnColumns,
		d.ID, in.AuthorID, in.Body, in.ComposedAt, in.MemoID))
	if err != nil {
		return Discussion{}, DiscussionTurn{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Discussion{}, DiscussionTurn{}, discussionError(err)
	}
	return d, t, nil
}

// AppendTurn adds the next turn to a thread.
//
// THE ROW LOCK IS NOT OPTIONAL, and it is AppendRevision's lock rather than a
// retry loop. Two concurrent appends would otherwise read the same MAX(seq):
// one violates UNIQUE (discussion_id, seq) and fails, and a caller that
// retried on the unique violation would be waiting on an index entry with no
// deadline of its own. Locking the thread serialises the allocation on the row
// that owns it.
//
// TWO THINGS FALL OUT OF THE LOCK BEYOND ORDERING. CH093's resolved check
// reads a row already held, so ruling 6 costs nothing; and CH091's "the turn at
// seq - 1 is visible" becomes a fact rather than snapshot reasoning, because
// appends to one thread are serial.
//
// lock_timeout IS SET FOR memolink.go:251's REASON: a statement run without one
// waits forever, and a waiter that gives up with a legible error is better than
// a connection held until something else notices.
func (s *Store) AppendTurn(ctx context.Context, in NewTurn) (DiscussionTurn, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DiscussionTurn{}, fmt.Errorf("store: append turn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+linkLockTimeout+`'`); err != nil {
		return DiscussionTurn{}, fmt.Errorf("store: append turn: %w", err)
	}

	var number int64
	var resolvedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT number, resolved_at FROM tier2.discussions WHERE id = $1 FOR UPDATE`,
		in.DiscussionID).Scan(&number, &resolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return DiscussionTurn{}, ErrNotFound
	}
	if err != nil {
		return DiscussionTurn{}, discussionError(fmt.Errorf("append turn: lock: %w", err))
	}

	// CH093 would catch this on the insert below, but only after a seq had
	// been computed and rolled back. Refusing on the locked row says the same
	// thing sooner — AppendRevision:359's shape, for the deleted-note case.
	if resolvedAt != nil {
		return DiscussionTurn{}, fmt.Errorf("%w: %s", ErrDiscussionResolved, FormatDiscussionRef(number))
	}

	t, err := scanTurn(tx.QueryRow(ctx, `
		INSERT INTO tier2.discussion_turns
		            (discussion_id, seq, author_id, body, composed_at, memo_id)
		VALUES ($1,
		        (SELECT COALESCE(MAX(seq), 0) + 1
		           FROM tier2.discussion_turns WHERE discussion_id = $1),
		        $2, $3, $4, $5)
		RETURNING `+turnColumns,
		in.DiscussionID, in.AuthorID, in.Body, in.ComposedAt, in.MemoID))
	if err != nil {
		return DiscussionTurn{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return DiscussionTurn{}, discussionError(err)
	}
	return t, nil
}

// DiscussionByID reads one thread.
func (s *Store) DiscussionByID(ctx context.Context, id uuid.UUID) (Discussion, error) {
	return scanDiscussion(s.pool.QueryRow(ctx,
		`SELECT `+discussionColumns+` FROM tier2.discussions WHERE id = $1`, id))
}

// DiscussionByNumber resolves DSC-####.
func (s *Store) DiscussionByNumber(ctx context.Context, number int64) (Discussion, error) {
	return scanDiscussion(s.pool.QueryRow(ctx,
		`SELECT `+discussionColumns+` FROM tier2.discussions WHERE number = $1`, number))
}

// DiscussionByRef resolves a written reference — DSC-0007, dsc-7, DSC-00007.
func (s *Store) DiscussionByRef(ctx context.Context, ref string) (Discussion, error) {
	n, err := ParseDiscussionRef(ref)
	if err != nil {
		return Discussion{}, err
	}
	return s.DiscussionByNumber(ctx, n)
}

// Turns returns a thread in order.
//
// ORDER BY seq AND NOT BY A TIMESTAMP. created_at is arrival and composed_at is
// a claim; seq is the record, and it is the only thing two clients are
// guaranteed to agree on.
func (s *Store) Turns(ctx context.Context, discussionID uuid.UUID) ([]DiscussionTurn, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+turnColumns+`
		   FROM tier2.discussion_turns WHERE discussion_id = $1 ORDER BY seq`, discussionID)
	if err != nil {
		return nil, fmt.Errorf("store: turns: %w", err)
	}
	defer rows.Close()
	out := []DiscussionTurn{}
	for rows.Next() {
		var t DiscussionTurn
		if err := rows.Scan(turnDest(&t)...); err != nil {
			return nil, fmt.Errorf("store: turns: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: turns: %w", err)
	}
	return out, nil
}

// ResolveDiscussion records what a thread concluded.
//
// note IS OPTIONAL AND MAY NAME AN EXISTING NOTE. CHRN-46's own example —
// "Resolved into PRINCIPLES §6" — is a section of something already written, so
// the caller may have appended a revision rather than created a note; this
// records whichever it was. Resolving with NO note is accepted, because some
// threads just end.
//
// THIS FUNCTION DOES NOT WRITE THE NOTE. CHRN-46 owns the resolve action and
// the text it produces; what lands here is the link. Ruling 4 governs the
// revision CHRN-46 writes: it carries verb NULL, with resolved_note_id below as
// its provenance.
//
// WHAT A SECOND CALL DOES, which is the part worth reading:
//
//   - Resolved without a note, called again WITH one: the note is linked. The
//     original resolver and timestamp stand. This is the case CHRN-46's
//     "resolving without a note is allowed" implies, and it is why CH080's
//     once-from-NULL clause is per column rather than on the triple.
//   - Called again with the SAME note: nothing changes, no error.
//   - Called again with a DIFFERENT note, or with none where one is recorded:
//     ErrResolutionFixed. A conclusion is not quietly replaced.
//
// THE COALESCE IS ON THE FIRST TWO COLUMNS ONLY, and asymmetrically on purpose.
// Coalescing resolved_note_id too would make the third case a silent no-op —
// the caller's note dropped on the floor with nothing returned to say so —
// whereas sending it raw puts the question to CH080, which is the guard that
// owns it. resolved_at and resolved_by are coalesced because a later call
// linking a note would otherwise send its own now() and actor into that same
// clause and be refused for a rewrite it never intended.
func (s *Store) ResolveDiscussion(ctx context.Context, id, by uuid.UUID, note *uuid.UUID) error {
	if err := requireActor(by); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussions
		   SET resolved_at      = COALESCE(resolved_at, now()),
		       resolved_by      = COALESCE(resolved_by, $2),
		       resolved_note_id = $3
		 WHERE id = $1`, id, by, note)
	if err != nil {
		return discussionError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddParticipant puts somebody on a thread, or brings them back.
//
// RE-ADDING CLEARS THE REMOVED PAIR AND LEAVES added_at / added_by ALONE, which
// CH100 enforces rather than trusts. The row means "first added", so a re-add
// does not reattribute the original invitation to whoever happened to notice
// they were missing.
func (s *Store) AddParticipant(ctx context.Context, discussionID, userID, addedBy uuid.UUID) error {
	if err := requireActor(addedBy); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tier2.discussion_participants (discussion_id, user_id, added_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (discussion_id, user_id) DO UPDATE
		   SET removed_at = NULL, removed_by = NULL
		 WHERE tier2.discussion_participants.removed_at IS NOT NULL`,
		discussionID, userID, addedBy)
	if err != nil {
		return discussionError(err)
	}
	return nil
}

// RemoveParticipant takes somebody off a thread WITHOUT touching a word they
// said. A turn carries its own author_id and author_kind, and nothing reads
// membership to decide authorship — which is why this is a state change and not
// a deletion.
//
// IDEMPOTENT. Zero rows means they were never on the thread or are already off
// it; both mean the same thing to a caller and neither is an error, on
// SoftDeleteNote's precedent. The WHERE is what keeps a second remove from
// replacing the recorded remover.
func (s *Store) RemoveParticipant(ctx context.Context, discussionID, userID, removedBy uuid.UUID) error {
	if err := requireActor(removedBy); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussion_participants
		   SET removed_at = now(), removed_by = $3
		 WHERE discussion_id = $1 AND user_id = $2 AND removed_at IS NULL`,
		discussionID, userID, removedBy)
	if err != nil {
		return discussionError(err)
	}
	return nil
}

// Participants lists everybody ever on a thread, oldest first. Removed
// participants are INCLUDED and carry their removed pair: a reader rendering a
// thread needs them, because their turns are still in it.
func (s *Store) Participants(ctx context.Context, discussionID uuid.UUID) ([]DiscussionParticipant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+participantColumns+`
		   FROM tier2.discussion_participants WHERE discussion_id = $1 ORDER BY added_at`,
		discussionID)
	if err != nil {
		return nil, fmt.Errorf("store: participants: %w", err)
	}
	defer rows.Close()
	out := []DiscussionParticipant{}
	for rows.Next() {
		var p DiscussionParticipant
		if err := rows.Scan(participantDest(&p)...); err != nil {
			return nil, fmt.Errorf("store: participants: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: participants: %w", err)
	}
	return out, nil
}

// discussionError maps the guards' SQLSTATEs onto this package's sentinels.
func discussionError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgTurnInsertOnly:
			return fmt.Errorf("%w: %v", ErrTurnImmutable, err)
		case pgAgentAfterAgent:
			return fmt.Errorf("%w: %v", ErrAgentMayNotFollowAgent, err)
		case pgAuthorKindSupplied:
			return fmt.Errorf("%w: %v", ErrAuthorKindDerived, err)
		case pgTurnOnResolved:
			return fmt.Errorf("%w: %v", ErrDiscussionResolved, err)
		case pgDiscussionGuard:
			switch pgErr.ConstraintName {
			case conResolverIsAPerson:
				// CH041's sentinel, deliberately, on 0014:386-388's reasoning:
				// it is the same rule, and a caller that catches "a person is
				// required" should not have to know how many tables it can
				// come from.
				return fmt.Errorf("%w: %v", ErrConfirmerRequired, err)
			case conDiscussionAllowList:
				return fmt.Errorf("%w: %v", ErrDiscussionColumnFrozen, err)
			case conResolutionOnce:
				return fmt.Errorf("%w: %v", ErrResolutionFixed, err)
			}
		case pgParticipantGuard:
			switch pgErr.ConstraintName {
			case conParticipantIsAPerson:
				return fmt.Errorf("%w: %v", ErrConfirmerRequired, err)
			case conParticipantAllowList:
				return fmt.Errorf("%w: %v", ErrParticipantColumnFrozen, err)
			}
		case pgLockNotAvailable:
			return fmt.Errorf("%w: %v", ErrLinkLocked, err)
		case pgUniqueViolation:
			if pgErr.ConstraintName == "discussion_turns_memo" {
				return fmt.Errorf("%w: %v", ErrMemoAlreadySpoke, err)
			}
		case pgForeignKeyViolation:
			// A page, author, memo or note that is not there. note.go:755 maps
			// it the same way and for the same reason: a caller naming
			// something that does not exist is a 404, not a 500.
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}
	}
	return fmt.Errorf("store: discussion: %w", err)
}
