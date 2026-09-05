package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CHRN-33's read side: what the triage screen shows, and what the admin report
// can see that no plain SELECT can.
//
// The tier split is visible in what is NOT here. A triage item is half tier 2
// (the memo, its transcript, the decision row) and half tier 1 (the proposal
// Scribe wrote), and the two halves are fetched by two stores over two pools
// and joined in Go. That is not an inefficiency to be optimised away later: the
// pools connect as different roles, so there is no transaction that could span
// them and no SQL join that could exist. The awkwardness is the boundary
// working.

// UntriagedMemo is one row of the triage screen, tier-2 half only.
type UntriagedMemo struct {
	Memo Memo

	// Excerpt is the opening of the memo's transcript, for a screen that has to
	// label a recording before anybody has listened to it — and the fallback
	// label for a DISCARD proposal, which carries no title by design.
	//
	// BOUNDED IN SQL rather than in Go. A triage batch is twelve memos and a
	// transcript can be forty minutes of speech; selecting all of it to keep
	// the first line would move megabytes to discard them.
	Excerpt string

	// Link is the decision already recorded against this memo, or nil.
	//
	// A memo appearing here WITH a link row is not a contradiction. A confirmed
	// decision advances the memo out of `transcribed` and out of this list; a
	// pending one has not landed yet, and a REFUSED one never will — and that
	// last case is exactly why this field exists. Without it the memo simply
	// reappears in the morning list with no account of what happened to
	// yesterday's decision.
	Link *MemoLink
}

// excerptBudget is how much transcript the screen gets. Enough for an opening
// sentence and no more: this is a label, and the whole transcript is one tap
// away on the memo itself.
const excerptBudget = 400

// UntriagedMemos returns the memos awaiting a decision, OLDEST FIRST, with
// whatever decision has already been attempted against each.
//
// Oldest first because the screen is an evening pass over a day's backlog and
// the thing most likely to be forgotten is the memo from Tuesday. Newest-first
// would put the recording somebody made four minutes ago at the top of a list
// they are working through to reach the ones they have forgotten.
//
// A memo with NO DURABLE TRANSCRIPT IS OMITTED, and that is the durable floor
// doing its job rather than a filter for tidiness. Routing from a partial
// transcript, or from one written by a model this deployment has never
// measured, produces a proposal about half a sentence — and an operator has no
// way to see that from a triage card. The memo is not lost: it is in
// GET /admin/transcription, which is where a transcription problem belongs.
//
// authorID scopes the read; uuid.Nil returns every author's memos, which is the
// owner's view. THE POST SCOPES SEPARATELY AND MUST: hiding a memo from a list
// is not access control, and a client that names a memo id directly has never
// been through this function.
func (s *Store) UntriagedMemos(ctx context.Context, authorID uuid.UUID, limit int) ([]UntriagedMemo, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	}
	var author *uuid.UUID
	if authorID != uuid.Nil {
		author = &authorID
	}

	rows, err := s.pool.Query(ctx,
		`SELECT `+prefixed(memoColumns, "m")+`, t.excerpt
		   FROM tier2.memos m
		   JOIN LATERAL (
		         SELECT left(text, $4) AS excerpt
		           FROM tier2.transcripts
		          WHERE memo_id = m.id AND `+DurableClause+`
		          ORDER BY transcribed_at DESC
		          LIMIT 1
		        ) t ON TRUE
		  WHERE m.state = $5
		    AND ($1::uuid IS NULL OR m.author_id = $1)
		    -- CHRN-34's HOLD, and it is filtered HERE rather than in the
		    -- service for one reason: the limit has to keep meaning "a screen".
		    -- Fetching 25 and dropping the deferred ones in Go would hand back
		    -- 15 cards on an evening when 10 were parked, and the operator
		    -- would have to page to reach work that was never hidden from them.
		    --
		    -- A tier-2 read filtering on a tier-1 table is not the doctrine's
		    -- line being crossed. The line is about WRITES (0007: "SELECT is
		    -- not a write path"), and this statement runs on the main role,
		    -- which owns both schemas. What the doctrine forbids is a foreign
		    -- key, and there is none — see 0009.
		    AND NOT EXISTS (SELECT 1 FROM tier1.triage_holds h WHERE h.memo_id = m.id)
		  ORDER BY m.captured_at
		  LIMIT $6`,
		author, SufficientRunners, SufficientModels, excerptBudget, StateTranscribed, limit)
	if err != nil {
		return nil, fmt.Errorf("store: untriaged memos: %w", err)
	}
	defer rows.Close()

	var out []UntriagedMemo
	var ids []uuid.UUID
	for rows.Next() {
		var it UntriagedMemo
		var m Memo
		if err := rows.Scan(&m.ID, &m.AuthorID, &m.ContentHash, &m.ByteSize, &m.CapturedAt,
			&m.State, &m.StateReason, &m.Retention, &m.AudioPrunedAt,
			&m.DurationMS, &m.Codec, &m.SampleRateHz, &m.OriginalFilename,
			&m.CreatedAt, &m.UpdatedAt, &it.Excerpt); err != nil {
			return nil, fmt.Errorf("store: untriaged memos: %w", err)
		}
		it.Memo = m
		it.Excerpt = firstLine(it.Excerpt)
		out = append(out, it)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: untriaged memos: %w", err)
	}

	links, err := s.MemoLinksFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if l, ok := links[out[i].Memo.ID]; ok {
			out[i].Link = &l
		}
	}
	return out, nil
}

// firstLine trims the excerpt to its opening line. A transcript is usually one
// unbroken paragraph, in which case this is the whole budget and correct.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// prefixed qualifies a column list with a table alias, so memoColumns stays
// the one definition of what a memo row is even in a join.
func prefixed(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

// MemoLinksFor returns the decision rows for a set of memos, keyed by memo.
// Memos with no decision are simply absent.
func (s *Store) MemoLinksFor(ctx context.Context, memoIDs []uuid.UUID) (map[uuid.UUID]MemoLink, error) {
	out := map[uuid.UUID]MemoLink{}
	if len(memoIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+memoLinkColumns+` FROM tier2.memo_links WHERE memo_id = ANY($1)`, memoIDs)
	if err != nil {
		return nil, fmt.Errorf("store: memo links: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		l, err := scanMemoLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: memo links: %w", err)
		}
		out[l.MemoID] = l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: memo links: %w", err)
	}
	return out, nil
}

// TriageLinks splits the decision rows four ways, which is the whole of what
// the admin report has to say about them.
//
// The three column-derived states are ordinary reads. IN FLIGHT IS NOT, and the
// difference is the point of this type — see TriageLinkStates.
type TriageLinks struct {
	// InFlight — some transaction holds this row's lock right now, which means
	// its outward call has not returned. Nothing to do; it will resolve or the
	// sweep will find it.
	InFlight []MemoLink

	// Unresolved — pending, unlocked, and a sweep has looked without finding a
	// ticket. Either Switchyard was unreachable or the create never happened.
	Unresolved []MemoLink

	// Ambiguous — more than one ticket claims this memo. NEEDS A PERSON:
	// confirming either would orphan the other, and nothing available to the
	// sweep says which is right.
	Ambiguous []MemoLink

	// Refused — a decision that will never land. NEEDS THE OPERATOR, and the
	// triage screen tells them why on the memo itself.
	Refused []MemoLink
}

// TriageLinkStates reports the four states, observing "in flight" by taking
// row locks rather than by guessing from a timestamp.
//
// THERE IS NO COLUMN FOR IN FLIGHT, AND THERE MUST NOT BE. A process that
// crashed between writing `started_at` and finishing would leave a row claiming
// to be in flight forever, and a sweep that trusted the column would never
// touch it — which is precisely the stuck state this design keeps arriving at
// and refusing. The lock IS the fact: a row held by a live transaction is being
// worked on, and a row whose holder died is unlocked the moment the connection
// drops, with no cleanup and no timeout to tune.
//
// The probe: inside a transaction, take `FOR UPDATE SKIP LOCKED` over the
// pending set, then ROLL BACK. The rows that came back were free; THE PENDING
// ROWS IT DID NOT RETURN ARE THE IN-FLIGHT ONES. It holds each lock for
// microseconds and blocks nothing a T2 would notice.
//
// Stated here at length so that the next person to read this does not reach for
// pg_locks — which cannot be joined back to a row without its ctid — or quietly
// fall back to an age predicate, which is the same bug the sweep refuses.
func (s *Store) TriageLinkStates(ctx context.Context, limit int) (TriageLinks, error) {
	var out TriageLinks
	if limit <= 0 {
		return out, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
	}

	pending, err := s.linksWhere(ctx,
		`confirmed_at IS NULL AND refused_at IS NULL`, `created_at`, limit)
	if err != nil {
		return out, err
	}
	out.Refused, err = s.linksWhere(ctx, `refused_at IS NOT NULL`, `refused_at DESC`, limit)
	if err != nil {
		return out, err
	}
	if len(pending) == 0 {
		return out, nil
	}

	ids := make([]uuid.UUID, len(pending))
	for i, l := range pending {
		ids[i] = l.ID
	}

	flight, err := s.LinksInFlight(ctx, ids)
	if err != nil {
		return out, err
	}
	for _, l := range pending {
		switch {
		case flight[l.ID]:
			out.InFlight = append(out.InFlight, l)
		case l.Ambiguous():
			out.Ambiguous = append(out.Ambiguous, l)
		default:
			out.Unresolved = append(out.Unresolved, l)
		}
	}
	return out, nil
}

// LinksInFlight reports which of ids another transaction is holding right now —
// which is to say, whose outward call has not returned.
//
// It answers by TAKING AND IMMEDIATELY RELEASING the locks: the rows a
// `FOR UPDATE SKIP LOCKED` returns were free, so THE ONES IT DID NOT RETURN ARE
// THE IN-FLIGHT ONES. Absent ids are absent from the result, which reads as
// false, and that is right — a row that no longer exists is not in flight.
func (s *Store) LinksInFlight(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	flight := map[uuid.UUID]bool{}
	if len(ids) == 0 {
		return flight, nil
	}
	free, err := s.unlocked(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !free[id] {
			flight[id] = true
		}
	}
	return flight, nil
}

// unlocked reports which of ids no other transaction is holding, by taking and
// immediately releasing their locks.
func (s *Store) unlocked(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("store: triage link states: %w", err)
	}
	// ROLLED BACK, ALWAYS. This transaction writes nothing and exists only to
	// scope the locks it takes; committing it would be the same thing more
	// expensively.
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx,
		`SELECT id FROM tier2.memo_links WHERE id = ANY($1) FOR UPDATE SKIP LOCKED`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: triage link states: %w", err)
	}
	defer rows.Close()

	free := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: triage link states: %w", err)
		}
		free[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: triage link states: %w", err)
	}
	return free, nil
}

func (s *Store) linksWhere(ctx context.Context, where, order string, limit int) ([]MemoLink, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+memoLinkColumns+` FROM tier2.memo_links
		  WHERE `+where+` ORDER BY `+order+` LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: memo links: %w", err)
	}
	defer rows.Close()
	var out []MemoLink
	for rows.Next() {
		l, err := scanMemoLink(rows)
		if err != nil {
			return nil, fmt.Errorf("store: memo links: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: memo links: %w", err)
	}
	return out, nil
}

// TriageBacklog is how much is waiting, and how long it has waited.
//
// By AGE and not merely by count, because the two failures look completely
// different and a bare number cannot tell them apart: forty memos captured this
// evening is a normal day before the evening pass, and four memos from three
// weeks ago is a screen nobody is opening.
type TriageBacklog struct {
	Total int

	// Today, ThisWeek and Older partition Total by capture age.
	Today    int
	ThisWeek int
	Older    int

	// OldestCapturedAt is nil when nothing is waiting.
	OldestCapturedAt *time.Time
}

// TriageBacklog counts the memos awaiting a decision. It applies the same
// durable-transcript floor UntriagedMemos does, so the number an operator reads
// is the number of cards the screen would show.
func (s *Store) TriageBacklog(ctx context.Context) (TriageBacklog, error) {
	var b TriageBacklog
	err := s.pool.QueryRow(ctx,
		`SELECT count(*),
		        count(*) FILTER (WHERE m.captured_at >  now() - interval '1 day'),
		        count(*) FILTER (WHERE m.captured_at <= now() - interval '1 day'
		                           AND m.captured_at >  now() - interval '7 days'),
		        count(*) FILTER (WHERE m.captured_at <= now() - interval '7 days'),
		        min(m.captured_at)
		   FROM tier2.memos m
		  WHERE m.state = $1
		    AND EXISTS (SELECT 1 FROM tier2.transcripts
		                 WHERE memo_id = m.id AND `+DurableClause+`)
		    -- Deferred memos are counted by CountTriageHolds and must not also
		    -- appear here, or the backlog an operator is trying to drive to
		    -- zero includes the memos they deliberately set aside.
		    AND NOT EXISTS (SELECT 1 FROM tier1.triage_holds h WHERE h.memo_id = m.id)`,
		StateTranscribed, SufficientRunners, SufficientModels).
		Scan(&b.Total, &b.Today, &b.ThisWeek, &b.Older, &b.OldestCapturedAt)
	if err != nil {
		return TriageBacklog{}, fmt.Errorf("store: triage backlog: %w", err)
	}
	return b, nil
}
