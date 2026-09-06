package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/google/uuid"
)

// CHRN-42 — note-to-note links, resolved both ways, plus tags.
//
// THE TWO HALVES SIT ON OPPOSITE SIDES OF THE TIER LINE, which is the design
// and not an accident. Links are DERIVED from what a note says, so they live
// in tier1.note_links and can be thrown away and rebuilt. Tags are AUTHORED —
// somebody decided this note is about hardware — so they live in
// tier2.note_tags and nothing regenerates them. 0013 has the argument.
//
// Links are extracted INSIDE the transaction that writes the revision, so the
// graph can never be stale with respect to the text it came from. That matters
// more than it sounds: a stale link graph is not a slow backlink list, it is a
// WRONG one, showing a note as pointing somewhere it no longer mentions.

// ErrInvalidTag is returned for a tag the schema's CHECK would refuse.
var ErrInvalidTag = errors.New("store: a tag must be lowercase alphanumeric words joined by single hyphens")

// Backlink is one note pointing at another, resolved.
type Backlink struct {
	NoteID uuid.UUID
	Number int64
	Title  string
	PageID uuid.UUID
}

// Ref renders the source note's permanent handle.
func (b Backlink) Ref() string { return FormatNoteRef(b.Number) }

// OutboundLink is a reference this note makes. Target is nil when the
// reference names a note that does not exist — while drafting, or because
// somebody misremembered a number. That is recorded rather than dropped, and
// it starts resolving by itself the moment the target is created.
type OutboundLink struct {
	ToNumber int64
	Target   *Backlink
}

// Ref renders the target's handle as it was written.
func (o OutboundLink) Ref() string { return FormatNoteRef(o.ToNumber) }

// reindexLinks replaces a note's outbound links from one revision's text.
//
// Runs on the caller's transaction, so the extraction and the revision land
// together or not at all.
func reindexLinks(ctx context.Context, q querier, noteID, revisionID uuid.UUID, selfNumber int64, title, body string) error {
	if _, err := q.Exec(ctx,
		`DELETE FROM tier1.note_links WHERE from_note_id = $1`, noteID); err != nil {
		return fmt.Errorf("store: reindex links: clear: %w", err)
	}

	// Title and body both, because a reference is a reference wherever a
	// person put it. markdown.References skips code spans and fenced blocks,
	// so a note quoting CHR-0311 in backticks does not acquire an edge.
	seen := map[int64]bool{}
	for _, r := range markdown.References([]byte(title + "\n\n" + body)) {
		// FILTERED HERE AND NOT BY THE CHECK CONSTRAINT. markdown's refPattern
		// matches any run of digits, so prose containing "CHR-0" yields
		// Number 0 — and 0013's `CHECK (to_number > 0)` would then refuse the
		// insert, fail reindexLinks, and roll back THE WHOLE NOTE. A sentence
		// mentioning CHR-0 would be a sentence that cannot be saved. The rule
		// is store.ParseNoteRef's, stated where user input meets it: note
		// numbers start at 1.
		//
		// A note does not link to itself either. Self-references happen — a
		// note quoting its own number while explaining what it is about — and
		// an edge for one puts every such note in its own backlink list.
		if r.Kind != markdown.KindNote || r.Number <= 0 ||
			r.Number == selfNumber || seen[r.Number] {
			continue
		}
		seen[r.Number] = true
		if _, err := q.Exec(ctx, `
			INSERT INTO tier1.note_links (from_note_id, from_revision_id, to_number)
			VALUES ($1, $2, $3)
			ON CONFLICT (from_note_id, to_number) DO UPDATE SET from_revision_id = EXCLUDED.from_revision_id`,
			noteID, revisionID, r.Number); err != nil {
			return fmt.Errorf("store: reindex links: insert: %w", err)
		}
	}
	return nil
}

// Backlinks lists the notes that point at this number, oldest note first.
//
// RESOLVED AT READ TIME against tier2.notes.number, which is what makes a
// reference written before its target existed start working on its own.
func (s *Store) Backlinks(ctx context.Context, number int64) ([]Backlink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT n.id, n.number, r.title, n.page_id
		  FROM tier1.note_links l
		  JOIN tier2.notes n ON n.id = l.from_note_id
		  JOIN tier2.note_revisions r ON r.id = n.current_revision_id
		 WHERE l.to_number = $1 AND n.deleted_at IS NULL
		 ORDER BY n.number`, number)
	if err != nil {
		return nil, fmt.Errorf("store: backlinks: %w", err)
	}
	defer rows.Close()

	out := []Backlink{}
	for rows.Next() {
		var b Backlink
		if err := rows.Scan(&b.NoteID, &b.Number, &b.Title, &b.PageID); err != nil {
			return nil, fmt.Errorf("store: backlinks: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: backlinks: %w", err)
	}
	return out, nil
}

// OutboundLinks lists what a note points at, resolved where it can be.
//
// A REFERENCE TO A DELETED NOTE RESOLVES AS DANGLING, exactly as a reference
// to a note that was never created does — the condition is on the JOIN, not in
// a WHERE, so the row survives with a nil Target. Dropping the row instead
// would silently edit what the author wrote: they typed CHR-0311 and the
// rendering would stop admitting it.
func (s *Store) OutboundLinks(ctx context.Context, noteID uuid.UUID) ([]OutboundLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT l.to_number, n.id, n.number, r.title, n.page_id
		  FROM tier1.note_links l
		  LEFT JOIN tier2.notes n ON n.number = l.to_number AND n.deleted_at IS NULL
		  LEFT JOIN tier2.note_revisions r ON r.id = n.current_revision_id
		 WHERE l.from_note_id = $1
		 ORDER BY l.to_number`, noteID)
	if err != nil {
		return nil, fmt.Errorf("store: outbound links: %w", err)
	}
	defer rows.Close()

	out := []OutboundLink{}
	for rows.Next() {
		var o OutboundLink
		var id *uuid.UUID
		var num *int64
		var title *string
		var page *uuid.UUID
		if err := rows.Scan(&o.ToNumber, &id, &num, &title, &page); err != nil {
			return nil, fmt.Errorf("store: outbound links: %w", err)
		}
		if id != nil && num != nil && title != nil && page != nil {
			o.Target = &Backlink{NoteID: *id, Number: *num, Title: *title, PageID: *page}
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: outbound links: %w", err)
	}
	return out, nil
}

// RebuildNoteLinks throws the whole graph away and re-derives it from the
// current revision of every note.
//
// THIS IS THE PROOF THAT tier1.note_links IS TIER 1. "Disposable because it is
// regenerable" is a claim, and a claim about a derived table needs something
// that actually regenerates it — otherwise the table drifts into being the
// only copy of something, which is the failure the tier split exists to
// prevent. It is also the repair for the orphan rows the missing foreign key
// permits, per 0013's header.
func (s *Store) RebuildNoteLinks(ctx context.Context) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("store: rebuild links: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM tier1.note_links`); err != nil {
		return 0, fmt.Errorf("store: rebuild links: clear: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT n.id, r.id, n.number, r.title, r.body
		  FROM tier2.notes n
		  JOIN tier2.note_revisions r ON r.id = n.current_revision_id`)
	if err != nil {
		return 0, fmt.Errorf("store: rebuild links: read: %w", err)
	}
	type row struct {
		noteID, revID uuid.UUID
		number        int64
		title, body   string
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.noteID, &r.revID, &r.number, &r.title, &r.body); err != nil {
			rows.Close()
			return 0, fmt.Errorf("store: rebuild links: read: %w", err)
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: rebuild links: read: %w", err)
	}

	for _, r := range all {
		if err := reindexLinks(ctx, tx, r.noteID, r.revID, r.number, r.title, r.body); err != nil {
			return 0, err
		}
	}

	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tier1.note_links`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: rebuild links: count: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("store: rebuild links: commit: %w", err)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Tags — authored, tier 2, and a filter rather than a hierarchy.
// ---------------------------------------------------------------------------

// NormaliseTag lowercases and trims a tag. Two tags differing only in case are
// two filters nobody can tell apart, so the difference is removed before it
// can be stored rather than papered over at query time.
func NormaliseTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// TagNote adds a tag. Tagging twice is not an error — it is the same claim.
//
// Tagging a DELETED note is refused, by note_tags_guard (CH060) rather than
// here. It cannot be notes_guard's CH033: this writes tier2.note_tags and
// never touches the note row, so the guard that fires on UPDATE of tier2.notes
// would never see it and the rule would read as enforced while doing nothing.
func (s *Store) TagNote(ctx context.Context, noteID uuid.UUID, tag string) error {
	t := NormaliseTag(tag)
	if !slugPattern.MatchString(t) {
		return fmt.Errorf("%w: %q", ErrInvalidTag, tag)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tier2.note_tags (note_id, tag) VALUES ($1, $2)
		ON CONFLICT (note_id, tag) DO NOTHING`, noteID, t)
	if err != nil {
		return noteError(err)
	}
	return nil
}

// UntagNote removes a tag. Removing one that is not there is not an error.
func (s *Store) UntagNote(ctx context.Context, noteID uuid.UUID, tag string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM tier2.note_tags WHERE note_id = $1 AND tag = $2`,
		noteID, NormaliseTag(tag))
	if err != nil {
		return fmt.Errorf("store: untag note: %w", err)
	}
	return nil
}

// NoteTags lists a note's tags, alphabetically.
func (s *Store) NoteTags(ctx context.Context, noteID uuid.UUID) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tag FROM tier2.note_tags WHERE note_id = $1 ORDER BY tag`, noteID)
	if err != nil {
		return nil, fmt.Errorf("store: note tags: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("store: note tags: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: note tags: %w", err)
	}
	return out, nil
}

// NotesByTag lists every note carrying a tag, ACROSS THE WHOLE TREE.
//
// That is what "a filter on the tree rather than a second hierarchy" means in
// practice: the result is not scoped to a page and has no order derived from
// one. A tag cuts across the tree; it does not reorganise it.
func (s *Store) NotesByTag(ctx context.Context, tag string) ([]Note, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+prefixed(noteColumns, "n")+`
		  FROM tier2.note_tags t JOIN tier2.notes n ON n.id = t.note_id
		 WHERE t.tag = $1 AND n.deleted_at IS NULL
		 ORDER BY n.number`, NormaliseTag(tag))
	if err != nil {
		return nil, fmt.Errorf("store: notes by tag: %w", err)
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(noteDest(&n)...); err != nil {
			return nil, fmt.Errorf("store: notes by tag: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: notes by tag: %w", err)
	}
	return out, nil
}

// AllTags lists every tag in use with how many notes carry it — the vocabulary
// a filter UI offers.
func (s *Store) AllTags(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tag, count(*) FROM tier2.note_tags GROUP BY tag ORDER BY tag`)
	if err != nil {
		return nil, fmt.Errorf("store: all tags: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var t string
		var n int
		if err := rows.Scan(&t, &n); err != nil {
			return nil, fmt.Errorf("store: all tags: %w", err)
		}
		out[t] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: all tags: %w", err)
	}
	return out, nil
}
