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

// CHRN-38 — the note, its permanent CHR-#### number, and the revision that
// holds its text. Decided in Mode B: the Switchyard plan on CHRN-38, revision
// 2, approved 2026-09-05 with all four rulings picked.
//
// THE NOTE ROW CARRIES NO AUTHORED TEXT. Not the body and not the title; both
// are on tier2.note_revisions, so a rename is a revision exactly as an edit
// is. 0011_notes.up.sql has the argument. The consequence for this file is
// that every read of a note's text is a join, and that CHRN-39's "no code path
// issues an UPDATE that loses text" is not a rule this package has to remember
// — there is no column here it could break.

// ErrRevisionImmutable is returned when something tries to rewrite history.
// It should be unreachable through this package, which never issues such a
// statement; it exists so that the day something does, the error names the
// rule rather than a SQLSTATE.
var ErrRevisionImmutable = errors.New("store: a note revision is append-only")

// ErrMemoAlreadyLanded is returned when a second revision claims a memo that
// already authored one. A memo lands exactly once — the cardinality
// tier2.memo_links already asserts about the decision, said here about the
// text that decision produced.
var ErrMemoAlreadyLanded = errors.New("store: that memo has already produced a revision")

// ErrInvalidNoteRef is returned by ParseNoteRef for anything that is not a
// note reference.
var ErrInvalidNoteRef = errors.New("store: not a note reference")

// Note-guard SQLSTATEs raised by 0011.
const (
	pgNoteIdentity       = "CH030"
	pgRevisionAppendOnly = "CH040"
)

// noteRefPattern parses LENIENTLY and Format renders STRICTLY. People quote
// these by hand — in a discussion, in a Switchyard comment, in a memo they
// dictate while driving — so `CHR-311`, `chr-0311` and `CHR-00311` all have to
// reach note 311. A prefix is still required: accepting a bare number would
// make every integer anybody writes a note reference.
var noteRefPattern = regexp.MustCompile(`^(?i:chr)-([0-9]+)$`)

// noteRefWidth is the MINIMUM rendered width, not a cap. CHR-0311 at 311 and
// CHR-10000 at 10000 — a four-digit ceiling would stop the corpus at 9999 and
// make raising it a migration on the identity column.
const noteRefWidth = 4

// Note is identity, address and provenance. Its text is a NoteRevision.
type Note struct {
	ID                uuid.UUID
	Number            int64
	PageID            uuid.UUID
	CurrentRevisionID uuid.UUID
	AuthorID          uuid.UUID
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Ref renders the note's permanent handle, CHR-0311.
func (n Note) Ref() string { return FormatNoteRef(n.Number) }

// NoteRevision is one version of a note's authored text.
type NoteRevision struct {
	ID     uuid.UUID
	NoteID uuid.UUID
	Seq    int
	Title  string
	Body   string
	// MemoID is the memo this TEXT came from, or nil when it was authored
	// directly. It is not the decision to file that memo as a note — that is a
	// tier2.memo_links row, and the two answer different questions.
	MemoID    *uuid.UUID
	AuthorID  uuid.UUID
	CreatedAt time.Time
}

// NewNote is the input to CreateNote.
type NewNote struct {
	PageID   uuid.UUID
	AuthorID uuid.UUID
	Title    string
	Body     string
	MemoID   *uuid.UUID
}

// NewRevision is the input to AppendRevision.
type NewRevision struct {
	AuthorID uuid.UUID
	Title    string
	Body     string
	MemoID   *uuid.UUID
}

// FormatNoteRef renders a note number as it is written and spoken.
func FormatNoteRef(number int64) string {
	return fmt.Sprintf("CHR-%0*d", noteRefWidth, number)
}

// ParseNoteRef reads a note reference, leniently. See noteRefPattern.
func ParseNoteRef(s string) (int64, error) {
	m := noteRefPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("%w: %q", ErrInvalidNoteRef, s)
	}
	// Leading zeros are ignored by base-10 parsing, so CHR-00311 and CHR-311
	// are the same note.
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q: %v", ErrInvalidNoteRef, s, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%w: %q: note numbers start at 1", ErrInvalidNoteRef, s)
	}
	return n, nil
}

const noteColumns = `id, number, page_id, current_revision_id, author_id, created_at, updated_at`

func scanNote(row pgx.Row) (Note, error) {
	var n Note
	err := row.Scan(&n.ID, &n.Number, &n.PageID, &n.CurrentRevisionID,
		&n.AuthorID, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, noteError(err)
	}
	return n, nil
}

const revisionColumns = `id, note_id, seq, title, body, memo_id, author_id, created_at`

func scanRevision(row pgx.Row) (NoteRevision, error) {
	var r NoteRevision
	err := row.Scan(&r.ID, &r.NoteID, &r.Seq, &r.Title, &r.Body,
		&r.MemoID, &r.AuthorID, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NoteRevision{}, ErrNotFound
	}
	if err != nil {
		return NoteRevision{}, noteError(err)
	}
	return r, nil
}

// CreateNote writes a note and its first revision in one transaction.
//
// THE REVISION IS INSERTED FIRST, and that order is the reason
// notes.current_revision_id can be NOT NULL. NOT NULL is not deferrable in
// Postgres, so inserting the note first would force the column to allow NULL
// forever and make every reader handle a state that cannot survive a commit.
// Generating both ids here and leaning on the two DEFERRABLE foreign keys
// makes "a note with no current revision" unrepresentable instead.
func (s *Store) CreateNote(ctx context.Context, in NewNote) (Note, NoteRevision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Note{}, NoteRevision{}, fmt.Errorf("store: create note: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	noteID := uuid.New()
	revID := uuid.New()

	rev, err := scanRevision(tx.QueryRow(ctx, `
		INSERT INTO tier2.note_revisions (id, note_id, seq, title, body, memo_id, author_id)
		VALUES ($1, $2, 1, $3, $4, $5, $6)
		RETURNING `+revisionColumns,
		revID, noteID, in.Title, in.Body, in.MemoID, in.AuthorID))
	if err != nil {
		return Note{}, NoteRevision{}, err
	}

	note, err := scanNote(tx.QueryRow(ctx, `
		INSERT INTO tier2.notes (id, page_id, current_revision_id, author_id)
		VALUES ($1, $2, $3, $4)
		RETURNING `+noteColumns,
		noteID, in.PageID, revID, in.AuthorID))
	if err != nil {
		return Note{}, NoteRevision{}, err
	}

	// Both deferred foreign keys are checked here, not above.
	if err := tx.Commit(ctx); err != nil {
		return Note{}, NoteRevision{}, noteError(err)
	}
	return note, rev, nil
}

// AppendRevision adds the next revision and makes it current.
//
// THE ROW LOCK IS NOT OPTIONAL. Two concurrent appends would otherwise read
// the same max(seq): one violates UNIQUE (note_id, seq) and fails, and which
// of the two ends up in current_revision_id is whichever committed second.
// E10 is exactly that case — an agent and a person editing one note in the
// same minute. Locking the note serialises the seq allocation and the pointer
// move together, on the row that owns both.
//
// This is the MECHANICAL primitive. CHRN-39 owns the verbs (create / append /
// supersede / relate), what each does to history, and the rule that nothing
// appends to authored text unattended.
func (s *Store) AppendRevision(ctx context.Context, noteID uuid.UUID, in NewRevision) (NoteRevision, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: append revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM tier2.notes WHERE id = $1 FOR UPDATE`, noteID).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return NoteRevision{}, ErrNotFound
	}
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: append revision: lock: %w", err)
	}

	rev, err := scanRevision(tx.QueryRow(ctx, `
		INSERT INTO tier2.note_revisions (note_id, seq, title, body, memo_id, author_id)
		VALUES ($1,
		        (SELECT COALESCE(MAX(seq), 0) + 1 FROM tier2.note_revisions WHERE note_id = $1),
		        $2, $3, $4, $5)
		RETURNING `+revisionColumns,
		noteID, in.Title, in.Body, in.MemoID, in.AuthorID))
	if err != nil {
		return NoteRevision{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tier2.notes SET current_revision_id = $2 WHERE id = $1`,
		noteID, rev.ID); err != nil {
		return NoteRevision{}, noteError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return NoteRevision{}, noteError(err)
	}
	return rev, nil
}

// NoteByID reads one note.
func (s *Store) NoteByID(ctx context.Context, id uuid.UUID) (Note, error) {
	return scanNote(s.pool.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM tier2.notes WHERE id = $1`, id))
}

// NoteByNumber resolves CHR-####. This is the lookup the ticket's "CHR-0311
// resolves to exactly one note forever" is about, and the sequence behind
// notes.number is what makes "forever" true.
func (s *Store) NoteByNumber(ctx context.Context, number int64) (Note, error) {
	return scanNote(s.pool.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM tier2.notes WHERE number = $1`, number))
}

// NoteByRef resolves a written reference — CHR-0311, chr-311, CHR-00311.
func (s *Store) NoteByRef(ctx context.Context, ref string) (Note, error) {
	n, err := ParseNoteRef(ref)
	if err != nil {
		return Note{}, err
	}
	return s.NoteByNumber(ctx, n)
}

// CurrentRevision reads the live text of a note.
func (s *Store) CurrentRevision(ctx context.Context, noteID uuid.UUID) (NoteRevision, error) {
	return scanRevision(s.pool.QueryRow(ctx, `
		SELECT r.id, r.note_id, r.seq, r.title, r.body, r.memo_id, r.author_id, r.created_at
		  FROM tier2.notes n JOIN tier2.note_revisions r ON r.id = n.current_revision_id
		 WHERE n.id = $1`, noteID))
}

// NoteRevisions returns a note's full history, oldest first.
func (s *Store) NoteRevisions(ctx context.Context, noteID uuid.UUID) ([]NoteRevision, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+revisionColumns+` FROM tier2.note_revisions WHERE note_id = $1 ORDER BY seq`, noteID)
	if err != nil {
		return nil, fmt.Errorf("store: note revisions: %w", err)
	}
	defer rows.Close()
	out := []NoteRevision{}
	for rows.Next() {
		var r NoteRevision
		if err := rows.Scan(&r.ID, &r.NoteID, &r.Seq, &r.Title, &r.Body,
			&r.MemoID, &r.AuthorID, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: note revisions: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: note revisions: %w", err)
	}
	return out, nil
}

// NotesOnPage lists the notes filed on a page, oldest number first.
func (s *Store) NotesOnPage(ctx context.Context, pageID uuid.UUID) ([]Note, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+noteColumns+` FROM tier2.notes WHERE page_id = $1 ORDER BY number`, pageID)
	if err != nil {
		return nil, fmt.Errorf("store: notes on page: %w", err)
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Number, &n.PageID, &n.CurrentRevisionID,
			&n.AuthorID, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: notes on page: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: notes on page: %w", err)
	}
	return out, nil
}

// MoveNote refiles a note on another page. Its id and number do not change,
// which is the whole of "permanent IDs that survive moves".
func (s *Store) MoveNote(ctx context.Context, id, pageID uuid.UUID) (Note, error) {
	return scanNote(s.pool.QueryRow(ctx,
		`UPDATE tier2.notes SET page_id = $2 WHERE id = $1 RETURNING `+noteColumns, id, pageID))
}

// noteError maps the guards' SQLSTATEs onto this package's sentinels.
func noteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgRevisionAppendOnly:
			return fmt.Errorf("%w: %v", ErrRevisionImmutable, err)
		case pgNoteIdentity:
			return fmt.Errorf("store: note identity is immutable: %w", err)
		case pgUniqueViolation:
			if pgErr.ConstraintName == "note_revisions_memo" {
				return fmt.Errorf("%w: %v", ErrMemoAlreadyLanded, err)
			}
		case pgForeignKeyViolation:
			// A page, author or memo that is not there. transcript.go:179 and
			// memolink.go:327 both map this the same way, and the reason is
			// what a handler has to do with it: a caller naming something that
			// does not exist is a 404, not a 500, and without this it arrives
			// as an opaque wrap that leaves a handler choosing between the
			// wrong status and a string match.
			return fmt.Errorf("%w: %v", ErrNotFound, err)
		}
	}
	return fmt.Errorf("store: note: %w", err)
}
