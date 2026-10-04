package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CHRN-41 — Postgres FTS across authored notes and memo transcripts.
//
// WHY TRANSCRIPTS ARE IN HERE AT ALL. CHRN-22 prunes audio at thirty days, so
// from day thirty-one a transcript is the only account of what somebody said.
// A search over notes alone would find the small fraction of the corpus that
// got triaged and lose the rest, which is the opposite of what "a phrase
// spoken into a memo in March is findable in September" asks for.
//
// THE INDEX IS TWO EXPRESSION INDEXES AND NO TABLE. See 0012_search.up.sql for
// why that dissolves rather than answers the tier-1 grant question CHRN-32
// §1.1 handed to this ticket.

// Search result kinds. AUTHORED VERSUS TRANSCRIBED IS THE DISTINCTION THE
// TICKET ASKS RESULTS TO CARRY, and it is not cosmetic: one is what a person
// decided to write down, the other is what they happened to say into a phone.
const (
	HitNote       = "note"
	HitTranscript = "transcript"
)

// ErrEmptyQuery is returned when a query has no searchable terms in it. A
// query of pure punctuation matches nothing, and answering "no results" would
// read as an empty corpus rather than as an empty question.
var ErrEmptyQuery = errors.New("store: search query has no terms")

// defaultSearchLimit bounds a result set that nobody asked to bound.
const defaultSearchLimit = 50

// SearchHit is one result. Note and transcript fields are mutually exclusive;
// Kind says which set is populated.
type SearchHit struct {
	Kind string

	// Notes.
	NoteID *uuid.UUID
	Number *int64 // render with FormatNoteRef
	Title  string
	PageID *uuid.UUID

	// Transcripts.
	MemoID *uuid.UUID
	Model  string // runner-qualified, e.g. whisper.cpp/small.en

	// Both.
	Snippet   string
	Rank      float32
	CreatedAt time.Time
}

// Ref renders a note hit's permanent handle, or "" for a transcript.
func (h SearchHit) Ref() string {
	if h.Number == nil {
		return ""
	}
	return FormatNoteRef(*h.Number)
}

// searchSQL is one statement, and the tsvector expressions in it are
// BYTE-IDENTICAL to the ones 0012 indexes. A difference of a single character
// — 'english' spelled differently, a missing setweight, the one-argument
// to_tsvector — costs the index silently: the query still returns the right
// rows, by sequential scan, and nothing fails.
//
// DISTINCT ON (memo_id) for transcripts, because tier2.transcripts is unique
// on (memo_id, model) and a memo transcribed by both small.en and medium.en
// would otherwise appear twice for one thing somebody said. The best-ranked
// one wins and carries its model, so the operator can see which decode
// matched.
const searchSQL = `
WITH q AS (SELECT websearch_to_tsquery('english', $1) AS tsq)
SELECT kind, note_id, number, title, page_id, memo_id, model, snippet, rank, created_at
FROM (
    SELECT 'note'::text                       AS kind,
           n.id                               AS note_id,
           n.number                           AS number,
           r.title                            AS title,
           n.page_id                          AS page_id,
           NULL::uuid                         AS memo_id,
           ''::text                           AS model,
           ts_headline('english', r.body, q.tsq,
                       'MaxFragments=2,MinWords=6,MaxWords=20,FragmentDelimiter= … ') AS snippet,
           ts_rank_cd(setweight(to_tsvector('english', r.title), 'A') ||
                      setweight(to_tsvector('english', r.body),  'B'), q.tsq) AS rank,
           r.created_at                       AS created_at
      FROM tier2.notes n
      JOIN tier2.note_revisions r ON r.id = n.current_revision_id
     CROSS JOIN q
     WHERE n.deleted_at IS NULL
       AND (setweight(to_tsvector('english', r.title), 'A') ||
            setweight(to_tsvector('english', r.body),  'B')) @@ q.tsq

    UNION ALL

    SELECT * FROM (
        SELECT DISTINCT ON (t.memo_id)
               'transcript'::text, NULL::uuid, NULL::bigint, ''::text, NULL::uuid,
               t.memo_id, t.model,
               ts_headline('english', t.text, q.tsq,
                           'MaxFragments=2,MinWords=6,MaxWords=20,FragmentDelimiter= … '),
               ts_rank_cd(to_tsvector('english', t.text), q.tsq),
               t.created_at
          FROM tier2.transcripts t
         CROSS JOIN q
         WHERE to_tsvector('english', t.text) @@ q.tsq
         ORDER BY t.memo_id, ts_rank_cd(to_tsvector('english', t.text), q.tsq) DESC
    ) best_per_memo
) hits
ORDER BY rank DESC, created_at DESC
LIMIT $2`

// Search runs one query across the authored corpus and the transcribed one.
//
// The query language is websearch_to_tsquery's: bare words are ANDed, "quoted
// phrases" are phrases, OR is OR, and a leading - excludes. That is the syntax
// people already type into search boxes, and it CANNOT RAISE on malformed
// input the way plainto_tsquery's stricter cousins can — which matters because
// the input is whatever somebody typed.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: %q", ErrEmptyQuery, query)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	// A query of pure punctuation parses to an empty tsquery, which matches
	// nothing. Reported as the empty question it is rather than as an empty
	// corpus.
	var empty bool
	if err := s.pool.QueryRow(ctx,
		`SELECT websearch_to_tsquery('english', $1)::text = ''`, query).Scan(&empty); err != nil {
		return nil, fmt.Errorf("store: search: parse query: %w", err)
	}
	if empty {
		return nil, fmt.Errorf("%w: %q", ErrEmptyQuery, query)
	}

	rows, err := s.pool.Query(ctx, searchSQL, query, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	defer rows.Close()

	out := []SearchHit{}
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Kind, &h.NoteID, &h.Number, &h.Title, &h.PageID,
			&h.MemoID, &h.Model, &h.Snippet, &h.Rank, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: search: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	return out, nil
}

// ── CHRN-116 — the notes-only search a non-owner account may call ───────────
//
// Search above is owner-only because its transcript arm has no author
// predicate. SearchNotes is the half of it that was always safe to share:
// notes are one corpus every member already reads by ref.
//
// IT IS A SECOND STATEMENT AND NOT A FILTER. Dropping transcript hits after
// the union would make "no transcript reaches a non-owner" depend on a branch
// somebody can edit. Here there is nothing to drop: the statement reads two
// tables and neither of them holds a transcript, and NoteHit has nowhere to
// put one. What enforces that is the statement text and the type, each under
// a test — NOT a grant: both statements run as the same role, which may read
// transcripts.

// NoteHit is one notes-search result. It has no memo and no model: the type
// cannot carry a transcript.
type NoteHit struct {
	NoteID    uuid.UUID
	Number    int64 // render with FormatNoteRef
	Title     string
	PageID    uuid.UUID
	Snippet   string
	Rank      float32
	CreatedAt time.Time
}

// Ref renders the note's permanent handle.
func (h NoteHit) Ref() string { return FormatNoteRef(h.Number) }

// searchNotesSQL is the notes arm of searchSQL as a statement of its own. The
// tsvector expression is COPIED from searchSQL rather than factored out of
// it, so the owner path's statement stays byte-identical; the warning above
// searchSQL applies to this copy exactly as it does to the original.
// TestSearchNotesSharesTheIndexedExpression holds the copy to the original
// without a database, and TestSearchNotesUsesTheIndexAtScale holds it to 0012
// with one.
//
// The only relations it names are tier2.notes and tier2.note_revisions.
// TestSearchNotesStatementNamesNoTranscriptTable keeps it that way.
const searchNotesSQL = `
WITH q AS (SELECT websearch_to_tsquery('english', $1) AS tsq)
SELECT n.id, n.number, r.title, n.page_id,
       ts_headline('english', r.body, q.tsq,
                   'MaxFragments=2,MinWords=6,MaxWords=20,FragmentDelimiter= … ') AS snippet,
       ts_rank_cd(setweight(to_tsvector('english', r.title), 'A') ||
                  setweight(to_tsvector('english', r.body),  'B'), q.tsq) AS rank,
       r.created_at
  FROM tier2.notes n
  JOIN tier2.note_revisions r ON r.id = n.current_revision_id
 CROSS JOIN q
 WHERE n.deleted_at IS NULL
   AND (setweight(to_tsvector('english', r.title), 'A') ||
        setweight(to_tsvector('english', r.body),  'B')) @@ q.tsq
 ORDER BY rank DESC, r.created_at DESC
 LIMIT $2`

// SearchNotes searches the live text of live notes and nothing else. Same
// query language, same ranking and same empty-question answer as Search.
func (s *Store) SearchNotes(ctx context.Context, query string, limit int) ([]NoteHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("%w: %q", ErrEmptyQuery, query)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}

	// The same probe Search makes, copied so that no line inside Search
	// changes: pure punctuation parses to an empty tsquery.
	var empty bool
	if err := s.pool.QueryRow(ctx,
		`SELECT websearch_to_tsquery('english', $1)::text = ''`, query).Scan(&empty); err != nil {
		return nil, fmt.Errorf("store: search notes: parse query: %w", err)
	}
	if empty {
		return nil, fmt.Errorf("%w: %q", ErrEmptyQuery, query)
	}

	rows, err := s.pool.Query(ctx, searchNotesSQL, query, limit)
	if err != nil {
		return nil, fmt.Errorf("store: search notes: %w", err)
	}
	defer rows.Close()

	out := []NoteHit{}
	for rows.Next() {
		var h NoteHit
		if err := rows.Scan(&h.NoteID, &h.Number, &h.Title, &h.PageID,
			&h.Snippet, &h.Rank, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: search notes: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: search notes: %w", err)
	}
	return out, nil
}
