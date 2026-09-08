package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CHRN-45 — read markers per participant per thread, and the counts derived
// from them. Decided in Mode B: the Switchyard plan on CHRN-45, revision 2,
// approved 2026-09-08 with all eight rulings picked.
// 0016_read_markers.up.sql carries the argument.
//
// UNREAD IS max(seq) − last_read_seq, EXACTLY, FOREVER. Not a count(*), and
// the difference is a property CHRN-43 bought rather than an optimisation:
// AppendTurn allocates seq as COALESCE(MAX(seq), 0) + 1 under the thread's row
// lock, so a rolled-back append leaves no gap, and CH090 refuses DELETE so no
// gap can be made later. There is nothing for the arithmetic to be wrong about.

// ErrNotAParticipant is returned by MarkRead when the caller has no participant
// row on that thread — ruling 8.
//
// Reading is not joining. Under ruling 3 POSTING is what puts somebody on a
// thread; letting a read do it too would turn tier2.discussion_participants
// from "who is expected to read this" into "who has looked", and everybody who
// glanced at a thread would then appear in Participants for everyone else.
var ErrNotAParticipant = errors.New("store: not a participant of that discussion")

// ErrAgentHasNoMarker is CH101 — ruling 4. Unread answers "what should I look
// at" and an agent is never asked that; CHRN-47 keeps its trigger explicit
// precisely so an agent does not reply to everything, and an agent that scanned
// unread threads would be that failure by another route.
var ErrAgentHasNoMarker = errors.New("store: an agent carries no read marker")

// ErrMarkerRewind is CH100's marker-forward arm — a decrease, including being
// cleared back to "never read", which is the largest decrease there is.
// Unreachable through this package, which wraps every write in GREATEST; it
// exists so a caller can tell this refusal from the allow list's, which shares
// the SQLSTATE.
var ErrMarkerRewind = errors.New("store: a read marker only moves forward")

// ErrMarkerPastTheThread is CH102. Unreachable through this package, which
// clamps to the thread's last turn; it names the rule for a direct writer.
var ErrMarkerPastTheThread = errors.New("store: a read marker cannot run past the thread")

// Read-marker SQLSTATEs — 0016. CH100's own codes are in discussion.go.
const (
	pgAgentHasNoMarker    = "CH101"
	pgMarkerPastTheThread = "CH102"
)

// Constraint names 0016 raises, so the two arms of CH100 that concern the
// marker are distinguishable from its allow list — discussion.go's reasoning.
const conMarkerForward = "discussion_participants_marker_forward"

// markerClamp is the value a marker is set to: never backwards, never past the
// thread's last turn.
//
// BOTH BOUNDS ARE HERE, and the upper one is the less obvious. Ruling 5 forbids
// a DECREASE, so GREATEST alone would let MarkRead(999) on a five-turn thread
// sit at 999 forever and report 0 unread until the thread had a thousand turns
// — this ticket's own "the web says none", silent and never self-correcting.
// One off-by-one against a stale turn list produces it.
//
// The LEAST is evaluated against the thread's head at write time, so a
// concurrent append can only make the clamp conservative: the caller is marked
// read to where the thread was when they reported, which is what "clients
// report position" means.
const markerClamp = `GREATEST(
	COALESCE(tier2.discussion_participants.last_read_seq, 0),
	LEAST($3::int, (SELECT COALESCE(MAX(seq), 0)
	                  FROM tier2.discussion_turns WHERE discussion_id = $1)))`

// MarkRead moves a participant's marker to throughSeq.
//
// IT TAKES A POSITION, not "mark everything read" — the ticket's "clients
// report position rather than computing it locally", which is what makes the
// call idempotent and safe to retry. Marking a thread fully read is
// MarkRead(ctx, d, u, len(turns)) at the caller, which reads honestly there and
// is safe under the clamp above even when len(turns) was read a moment ago.
//
// A STALE REPORT IS A NO-OP, NOT AN ERROR (ruling 5). A phone showing turn 4
// while the web already marked 9 is being slow, not wrong, and answering it
// with a refusal turns ordinary lag into a case every client special-cases.
func (s *Store) MarkRead(ctx context.Context, discussionID, userID uuid.UUID, throughSeq int) error {
	if err := requireActor(userID); err != nil {
		return err
	}
	if throughSeq < 0 {
		return fmt.Errorf("%w: a read position cannot be negative", ErrInvalidInput)
	}

	// REFUSED HERE AS A MESSAGE; CH101 is the enforcement. requireActor's
	// comment (note.go:64) makes the same split for the same reason: a caller
	// deserves to be told which mistake it made.
	kind, err := s.userKind(ctx, userID)
	if err != nil {
		return err
	}
	if kind == KindAgent {
		return fmt.Errorf("%w: %s", ErrAgentHasNoMarker, userID)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE tier2.discussion_participants
		   SET last_read_seq = `+markerClamp+`,
		       last_read_at  = now()
		 WHERE discussion_id = $1 AND user_id = $2`,
		discussionID, userID, throughSeq)
	if err != nil {
		return discussionError(err)
	}
	if tag.RowsAffected() == 0 {
		// Ruling 8 — reading is not joining. Told apart from "no such thread",
		// because sending a caller to look for a missing discussion when the
		// discussion is fine is an afternoon lost.
		if _, err := s.DiscussionByID(ctx, discussionID); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", ErrNotAParticipant, userID)
	}
	return nil
}

// userKind reads an account's kind, or ErrNotFound.
func (s *Store) userKind(ctx context.Context, id uuid.UUID) (string, error) {
	var kind string
	err := s.pool.QueryRow(ctx, `SELECT kind FROM tier2.users WHERE id = $1`, id).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: user kind: %w", err)
	}
	return kind, nil
}

// unreadExpr is the ARITHMETIC, shared by the single-thread read and the
// aggregate so the two cannot disagree about how unread is computed — the bug
// ruling 6 exists to prevent, which is a second hand-written subtraction
// drifting from this one.
//
// IT IS NOT THE WHOLE ANSWER, and the difference matters: WHICH ROWS each
// query asks about lives in its WHERE clause, and the two deliberately differ
// there. The aggregate filters removed participants; UnreadCount does not. See
// UnreadCount for why.
const unreadExpr = `GREATEST(0,
	(SELECT COALESCE(MAX(t.seq), 0) FROM tier2.discussion_turns t
	  WHERE t.discussion_id = p.discussion_id)
	- COALESCE(p.last_read_seq, 0))`

// UnreadCount reports how many turns this participant has not read.
//
// A NEVER-READ PARTICIPANT IS BEHIND BY THE WHOLE THREAD, because COALESCE
// treats their NULL as 0 — while the column stays distinguishably NULL for
// anything that wants to ask whether they ever opened it.
//
// AN AGENT IS ALWAYS 0, which is ruling 4 rather than a convenience. Without
// the test an agent's NULL marker would COALESCE to 0 and report the whole
// thread unread — a number for something the ruling says has no unread, and
// one that disagreed with UnreadByDiscussion's empty map for the same account.
//
// A REMOVED PARTICIPANT IS *NOT* FILTERED HERE, and that asymmetry with the
// aggregate is deliberate rather than an oversight. They are different
// questions: the badge asks "what should I look at", and removal is exactly a
// statement that this thread is not that; asking directly about one thread is
// answered about the thread, not about the roster. CHRN-99 inherits both and
// should pick per surface.
func (s *Store) UnreadCount(ctx context.Context, discussionID, userID uuid.UUID) (int, error) {
	kind, err := s.userKind(ctx, userID)
	if err != nil {
		return 0, err
	}
	if kind == KindAgent {
		return 0, nil
	}
	var n int
	err = s.pool.QueryRow(ctx, `
		SELECT `+unreadExpr+`
		  FROM tier2.discussion_participants p
		 WHERE p.discussion_id = $1 AND p.user_id = $2`, discussionID, userID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrNotAParticipant, userID)
	}
	if err != nil {
		return 0, fmt.Errorf("store: unread count: %w", err)
	}
	return n, nil
}

// UnreadByDiscussion is the badge: every thread this person has something
// unread in (ruling 6).
//
// THREE FILTERS, and each is a decision rather than a detail:
//
//   - REMOVED PARTICIPANTS ARE EXCLUDED. Being taken off a thread is exactly a
//     statement that you are no longer expected to read it, so counting it
//     would make removal do nothing a person can see.
//   - THREADS WITH NOTHING UNREAD ARE OMITTED rather than returned as 0, so the
//     map's length is the number of threads wanting attention and a caller does
//     not have to filter it again to get that.
//   - AGENTS GET AN EMPTY MAP, from the explicit kind test below and not from
//     the marker. Worth being exact: an agent's marker is NULL, which COALESCEs
//     to 0, so without the test the arithmetic would report every thread it is
//     on at full count. Ruling 4 says an agent has no unread; the test is what
//     makes that true here, as CH101 makes it true in the store.
//
// A RESOLVED THREAD CAN STILL BE UNREAD, deliberately. CH093 means the count
// cannot move again, so what is left is a fixed number of turns somebody has
// not read yet — including, usually, the one that concluded it.
func (s *Store) UnreadByDiscussion(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]int, error) {
	if err := requireActor(userID); err != nil {
		return nil, err
	}
	kind, err := s.userKind(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]int{}
	if kind == KindAgent {
		return out, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT p.discussion_id, `+unreadExpr+`
		  FROM tier2.discussion_participants p
		 WHERE p.user_id = $1 AND p.removed_at IS NULL`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: unread by discussion: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("store: unread by discussion: %w", err)
		}
		if n > 0 {
			out[id] = n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: unread by discussion: %w", err)
	}
	return out, nil
}

// advanceAuthorsMarker is ruling 3: posting is reading.
//
// WHY IT EXISTS AT ALL. A person opens a thread and the Scribe answers. The
// thread has two turns and the person has read one of them — their own. Without
// this they are shown 2 unread, one of which is a sentence they dictated, which
// is the `Done when`'s "two" and the default behaviour of every implementation
// that does not think about it.
//
// IT RUNS IN THE APPEND'S OWN TRANSACTION, so an append refused by CH091 or
// CH093 leaves the marker where it was. A refused reply that silently marked a
// thread read would be worse than the bug it was trying to avoid.
//
// IT SKIPS AGENTS, AND THAT IS NOT AN OPTIMISATION. CH100 requires added_by to
// be a person, and a BEFORE INSERT trigger fires on INSERT … ON CONFLICT DO
// UPDATE *even when the row already exists and only the update path can run* —
// verified against Postgres 16. So an agent's self-upsert is refused and, being
// in the same transaction, takes the turn down with it: the Scribe could not
// reply at all. The branch reads author_kind off the turn the insert just
// returned (CH092 sets it), so it costs no extra query.
//
// ON CONFLICT TOUCHES ONLY THE MARKER (ruling 7). A person removed from a
// thread may still post — 0015 requires no membership to write — and their
// removal STANDS. Clearing it here would let anybody a person removed put
// themselves back by speaking, with the remover neither consulted nor told.
// AddParticipant does clear the pair, and the asymmetry is the decision.
func advanceAuthorsMarker(ctx context.Context, tx pgx.Tx, turn DiscussionTurn) error {
	if turn.ByAgent() {
		return nil
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO tier2.discussion_participants
		            (discussion_id, user_id, added_by, last_read_seq, last_read_at)
		VALUES ($1, $2, $2, $3, now())
		ON CONFLICT (discussion_id, user_id) DO UPDATE
		   SET last_read_seq = GREATEST(
		           COALESCE(tier2.discussion_participants.last_read_seq, 0),
		           EXCLUDED.last_read_seq),
		       last_read_at  = now()`,
		turn.DiscussionID, turn.AuthorID, turn.Seq)
	if err != nil {
		return discussionError(err)
	}
	return nil
}
