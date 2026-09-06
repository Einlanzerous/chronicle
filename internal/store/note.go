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

// ErrNoteDeleted is returned when something tries to write to a note that has
// been soft-deleted. Undelete first — which is itself recorded.
var ErrNoteDeleted = errors.New("store: that note is deleted")

// ErrRewind is returned when something tries to move a note back to an earlier
// revision. CHRN-39 ruling 1 restores by APPENDING, so the pointer only ever
// moves forward and this should be unreachable through this package.
var ErrRewind = errors.New("store: a note cannot be moved back to an earlier revision")

// ErrConfirmerRequired is returned when text would land in tier 2 without a
// person agreeing to it. CHRN-39 ruling 4 — nothing lands in authored text
// unattended, and an agent is never a valid confirmer.
var ErrConfirmerRequired = errors.New("store: authored text needs a confirming person")

// ErrDeletionUnjournaled is returned by UndeleteNote when tier2.notes says a
// note is deleted and tier2.note_deletions has no open record of it. The store
// writes both in one transaction, so this is only reachable when something
// else wrote the note row — and undeleting it silently would close nothing and
// leave the journal claiming the note was never gone.
var ErrDeletionUnjournaled = errors.New("store: a note is deleted with no open deletion record")

// requireActor is a message, not the enforcement. The triggers refuse a nil or
// agent actor regardless (CH041); this exists because a zero uuid.UUID is a
// caller that supplied NOBODY, and without it that reaches the trigger as
// 00000000-… and is reported as "not a person" — true, and misleading. CHRN-67
// will be wiring these actors and should be told which mistake it made.
func requireActor(id uuid.UUID) error {
	if id == uuid.Nil {
		return fmt.Errorf("%w: no actor was supplied", ErrConfirmerRequired)
	}
	return nil
}

// Note-guard SQLSTATEs. CH030 and CH040 are 0011's; the rest are 0014's.
const (
	pgNoteIdentity        = "CH030"
	pgNoteColumnFrozen    = "CH031"
	pgNoteRewind          = "CH032"
	pgNoteDeleted         = "CH033"
	pgRevisionAppendOnly  = "CH040"
	pgConfirmerRequired   = "CH041"
	pgTagOnDeletedNote    = "CH060"
	pgDeletionRecordFixed = "CH070"
)

// The verb set — CHRN-39 ruling 3, four peer verbs. What a person confirmed
// about a Scribe proposal, recorded on the revision as provenance.
//
// create and relate both produce a NEW note and are therefore seq 1; append
// and supersede act on text that already exists and cannot be. 0014's
// note_revisions_verb_seq constraint enforces that rather than trusting it.
const (
	VerbCreate    = "create"
	VerbAppend    = "append"
	VerbSupersede = "supersede"
	VerbRelate    = "relate"
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

	// DeletedAt and DeletedBy are set together or not at all (CHRN-39
	// ruling 2). They are the CURRENT state; tier2.note_deletions is the
	// history, because undelete clears these and would otherwise erase that
	// the deletion ever happened.
	DeletedAt *time.Time
	DeletedBy *uuid.UUID
}

// Ref renders the note's permanent handle, CHR-0311.
func (n Note) Ref() string { return FormatNoteRef(n.Number) }

// Deleted reports whether the note has been soft-deleted. Reads that should
// not show it filter in SQL; this is for callers holding a Note already.
func (n Note) Deleted() bool { return n.DeletedAt != nil }

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

	// ConfirmedBy is who agreed to this text landing, and is never an agent
	// (CHRN-39 ruling 4). It answers a different question from AuthorID,
	// which is whose words these are and may legitimately be an agent for a
	// Scribe-routed memo. NULL only on rows written before 0014.
	ConfirmedBy *uuid.UUID

	// Verb is what the person confirmed about a proposal. Nil means authored
	// directly — someone typed it — mirroring MemoID.
	Verb *string

	// RestoredFrom names the revision this one reproduces. A restore APPENDS
	// rather than rewinding, so this is the only thing distinguishing it from
	// an ordinary edit that happens to reproduce old text.
	RestoredFrom *uuid.UUID
}

// NewNote is the input to CreateNote.
type NewNote struct {
	PageID   uuid.UUID
	AuthorID uuid.UUID
	Title    string
	Body     string
	MemoID   *uuid.UUID

	// ConfirmedBy is REQUIRED, and seq 1 is not exempt (CHRN-39 ruling 4).
	// CreateNote writes the first revision itself, so exempting it would put
	// the hole exactly where a Scribe-routed note arrives. An exemption for
	// agent-created notes is CHRN-67's argument to make, not this package's to
	// grant by omission.
	ConfirmedBy uuid.UUID

	// Verb is VerbCreate or VerbRelate on a new note, or nil when a person
	// typed it directly. Anything else is refused by note_revisions_verb_seq.
	Verb *string
}

// NewRevision is the input to AppendRevision.
type NewRevision struct {
	AuthorID uuid.UUID
	Title    string
	Body     string
	MemoID   *uuid.UUID

	// ConfirmedBy is REQUIRED — see NewNote.
	ConfirmedBy uuid.UUID

	// Verb is VerbAppend or VerbSupersede on an existing note, or nil for a
	// direct edit.
	Verb *string
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

const noteColumns = `id, number, page_id, current_revision_id, author_id, created_at, updated_at, deleted_at, deleted_by`

// noteDest is the single place noteColumns is unpacked. Every reader goes
// through it — scanNote for one row, the row loops for many — so a column
// added to noteColumns cannot be picked up by one caller and missed by
// another, which is exactly how a soft-delete filter goes half-applied.
func noteDest(n *Note) []any {
	return []any{&n.ID, &n.Number, &n.PageID, &n.CurrentRevisionID,
		&n.AuthorID, &n.CreatedAt, &n.UpdatedAt, &n.DeletedAt, &n.DeletedBy}
}

func scanNote(row pgx.Row) (Note, error) {
	var n Note
	err := row.Scan(noteDest(&n)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, noteError(err)
	}
	return n, nil
}

const revisionColumns = `id, note_id, seq, title, body, memo_id, author_id, created_at, confirmed_by, verb, restored_from`

// revisionDest is revisionColumns' single unpacking point — see noteDest.
func revisionDest(r *NoteRevision) []any {
	return []any{&r.ID, &r.NoteID, &r.Seq, &r.Title, &r.Body,
		&r.MemoID, &r.AuthorID, &r.CreatedAt,
		&r.ConfirmedBy, &r.Verb, &r.RestoredFrom}
}

func scanRevision(row pgx.Row) (NoteRevision, error) {
	var r NoteRevision
	err := row.Scan(revisionDest(&r)...)
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
	if err := requireActor(in.ConfirmedBy); err != nil {
		return Note{}, NoteRevision{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Note{}, NoteRevision{}, fmt.Errorf("store: create note: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	noteID := uuid.New()
	revID := uuid.New()

	rev, err := scanRevision(tx.QueryRow(ctx, `
		INSERT INTO tier2.note_revisions
		            (id, note_id, seq, title, body, memo_id, author_id, confirmed_by, verb)
		VALUES ($1, $2, 1, $3, $4, $5, $6, $7, $8)
		RETURNING `+revisionColumns,
		revID, noteID, in.Title, in.Body, in.MemoID, in.AuthorID, in.ConfirmedBy, in.Verb))
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

	// CHRN-42's link graph is derived from this text, and it is derived HERE so
	// that the graph can never be stale with respect to the revision it came
	// from. A stale link graph is not a slow backlink list, it is a wrong one.
	if err := reindexLinks(ctx, tx, noteID, revID, note.Number, in.Title, in.Body); err != nil {
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
	if err := requireActor(in.ConfirmedBy); err != nil {
		return NoteRevision{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: append revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked uuid.UUID
	var number int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT id, number, deleted_at FROM tier2.notes WHERE id = $1 FOR UPDATE`,
		noteID).Scan(&locked, &number, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NoteRevision{}, ErrNotFound
	}
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: append revision: lock: %w", err)
	}

	// CH033 would catch this on the pointer move below, but only after a
	// revision row had been written and rolled back. Refusing on the locked
	// row says the same thing without burning a seq.
	if deletedAt != nil {
		return NoteRevision{}, fmt.Errorf("%w: %s", ErrNoteDeleted, FormatNoteRef(number))
	}

	rev, err := scanRevision(tx.QueryRow(ctx, `
		INSERT INTO tier2.note_revisions
		            (note_id, seq, title, body, memo_id, author_id, confirmed_by, verb)
		VALUES ($1,
		        (SELECT COALESCE(MAX(seq), 0) + 1 FROM tier2.note_revisions WHERE note_id = $1),
		        $2, $3, $4, $5, $6, $7)
		RETURNING `+revisionColumns,
		noteID, in.Title, in.Body, in.MemoID, in.AuthorID, in.ConfirmedBy, in.Verb))
	if err != nil {
		return NoteRevision{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tier2.notes SET current_revision_id = $2 WHERE id = $1`,
		noteID, rev.ID); err != nil {
		return NoteRevision{}, noteError(err)
	}

	// Re-derived from the text that is now current — see CreateNote.
	if err := reindexLinks(ctx, tx, noteID, rev.ID, number, in.Title, in.Body); err != nil {
		return NoteRevision{}, err
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
		SELECT `+prefixed(revisionColumns, "r")+`
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
		if err := rows.Scan(revisionDest(&r)...); err != nil {
			return nil, fmt.Errorf("store: note revisions: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: note revisions: %w", err)
	}
	return out, nil
}

// NotesOnPage lists the LIVE notes filed on a page, oldest number first.
//
// Soft-deleted notes are excluded here rather than by the caller, and the same
// is true of Search, Backlinks and NotesByTag. A delete that one read surface
// forgets about is not a delete.
func (s *Store) NotesOnPage(ctx context.Context, pageID uuid.UUID) ([]Note, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+noteColumns+`
		   FROM tier2.notes WHERE page_id = $1 AND deleted_at IS NULL
		  ORDER BY number`, pageID)
	if err != nil {
		return nil, fmt.Errorf("store: notes on page: %w", err)
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(noteDest(&n)...); err != nil {
			return nil, fmt.Errorf("store: notes on page: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: notes on page: %w", err)
	}
	return out, nil
}

// RestoreRevision brings back the text of an earlier revision by APPENDING it
// as a new one (CHRN-39 ruling 1).
//
// IT DOES NOT MOVE THE POINTER BACKWARDS, and 0014's CH032 refuses that even
// if some future caller tries. The reason is the question "what did this note
// say on 12 August": under a rewind, current_revision_id has moved and nothing
// records that it moved or when, so seq 4 and 5 sit there indistinguishable
// from revisions that were never current. Appending keeps the log a complete
// account of what was displayed and when, and RestoredFrom is what makes the
// restore legible as an event rather than as an edit that happens to reproduce
// old text.
//
// THE NEW ROW CARRIES memo_id NULL. Copying the source's memo_id would collide
// with 0011's UNIQUE note_revisions_memo — a memo authors exactly one revision
// — and would be the wrong claim anyway: this text came from a revision, not
// from a memo. Provenance travels through RestoredFrom instead.
//
// author_id is the SOURCE revision's author, because author_id answers "whose
// words are these" and the words are unchanged. confirmedBy answers who agreed
// to them landing again, which is the part that is new.
func (s *Store) RestoreRevision(ctx context.Context, noteID, revisionID, confirmedBy uuid.UUID) (NoteRevision, error) {
	if err := requireActor(confirmedBy); err != nil {
		return NoteRevision{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: restore revision: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Same lock as AppendRevision, and for the same reason: this allocates a
	// seq and moves the pointer, and those must not race.
	var number int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT number, deleted_at FROM tier2.notes WHERE id = $1 FOR UPDATE`,
		noteID).Scan(&number, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return NoteRevision{}, ErrNotFound
	}
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: restore revision: lock: %w", err)
	}
	if deletedAt != nil {
		return NoteRevision{}, fmt.Errorf("%w: %s", ErrNoteDeleted, FormatNoteRef(number))
	}

	// note_id in the WHERE is not redundant with the composite foreign key —
	// it decides whether the caller gets ErrNotFound or a constraint violation
	// for naming another note's revision, and ErrNotFound is the honest answer.
	var srcTitle, srcBody string
	var srcAuthor uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT title, body, author_id FROM tier2.note_revisions WHERE id = $1 AND note_id = $2`,
		revisionID, noteID).Scan(&srcTitle, &srcBody, &srcAuthor)
	if errors.Is(err, pgx.ErrNoRows) {
		return NoteRevision{}, ErrNotFound
	}
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: restore revision: source: %w", err)
	}

	rev, err := scanRevision(tx.QueryRow(ctx, `
		INSERT INTO tier2.note_revisions
		            (note_id, seq, title, body, memo_id, author_id, confirmed_by, verb, restored_from)
		VALUES ($1,
		        (SELECT COALESCE(MAX(seq), 0) + 1 FROM tier2.note_revisions WHERE note_id = $1),
		        $2, $3, NULL, $4, $5, NULL, $6)
		RETURNING `+revisionColumns,
		noteID, srcTitle, srcBody, srcAuthor, confirmedBy, revisionID))
	if err != nil {
		return NoteRevision{}, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tier2.notes SET current_revision_id = $2 WHERE id = $1`,
		noteID, rev.ID); err != nil {
		return NoteRevision{}, noteError(err)
	}

	// The restored text is what the links must now reflect — see CreateNote.
	if err := reindexLinks(ctx, tx, noteID, rev.ID, number, srcTitle, srcBody); err != nil {
		return NoteRevision{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return NoteRevision{}, noteError(err)
	}
	return rev, nil
}

// SoftDeleteNote takes a note out of every read surface without losing a byte
// of it (CHRN-39 ruling 2), and journals the act (ruling 5).
//
// IDEMPOTENT, AND IT DOES NOT OVERWRITE. Deleting an already-deleted note is a
// no-op rather than an error — the house pattern TagNote sets — but crucially
// it leaves the original deleted_at and deleted_by alone, so a second caller
// cannot quietly become the recorded deleter. That is a property of the
// database rather than a promise this function keeps, on both tables: 0014's
// note_deletions_open partial unique index refuses a second open journal row,
// and notes_guard's CH033 refuses rewriting the pair on the note row while it
// is set.
func (s *Store) SoftDeleteNote(ctx context.Context, noteID, deletedBy uuid.UUID) error {
	if err := requireActor(deletedBy); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: soft delete note: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var deletedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT deleted_at FROM tier2.notes WHERE id = $1 FOR UPDATE`, noteID).Scan(&deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: soft delete note: lock: %w", err)
	}
	if deletedAt != nil {
		return nil
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tier2.notes SET deleted_at = now(), deleted_by = $2 WHERE id = $1`,
		noteID, deletedBy); err != nil {
		return noteError(err)
	}

	// The journal and the predicate are written in one transaction, which is
	// what keeps notes.deleted_at and this table from disagreeing.
	if _, err := tx.Exec(ctx,
		`INSERT INTO tier2.note_deletions (note_id, deleted_by) VALUES ($1, $2)`,
		noteID, deletedBy); err != nil {
		return noteError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return noteError(err)
	}
	return nil
}

// UndeleteNote brings a note back and closes out its deletion record.
//
// IT TAKES AN ACTOR because clearing deleted_by is the whole problem: without
// tier2.note_deletions and without this argument, a delete followed by an
// undelete would leave the database holding no trace that either happened —
// exactly the property RestoreRevision refuses for revisions. Idempotent for
// the same reason SoftDeleteNote is.
func (s *Store) UndeleteNote(ctx context.Context, noteID, undeletedBy uuid.UUID) error {
	if err := requireActor(undeletedBy); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: undelete note: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var number int64
	var deletedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT number, deleted_at FROM tier2.notes WHERE id = $1 FOR UPDATE`,
		noteID).Scan(&number, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: undelete note: lock: %w", err)
	}
	if deletedAt == nil {
		return nil
	}

	// Closed FIRST, so the person test on undeleted_by (CH041, in
	// note_deletions_guard) refuses the whole transaction before the note is
	// visible again. notes_guard cannot make that check — the undelete sets
	// notes.deleted_by to NULL, so there is no new value there to test.
	closed, err := tx.Exec(ctx, `
		UPDATE tier2.note_deletions
		   SET undeleted_at = now(), undeleted_by = $2
		 WHERE note_id = $1 AND undeleted_at IS NULL`,
		noteID, undeletedBy)
	if err != nil {
		return noteError(err)
	}
	// EXACTLY ONE, and it is checked. note_deletions_open guarantees at most
	// one open record; this guarantees at least one. A note row that says
	// deleted with nothing open in the journal was not written by this store,
	// and undeleting it anyway would clear the row while the journal goes on
	// saying the note was never gone.
	if closed.RowsAffected() != 1 {
		return fmt.Errorf("%w: %s", ErrDeletionUnjournaled, FormatNoteRef(number))
	}

	if _, err := tx.Exec(ctx,
		`UPDATE tier2.notes SET deleted_at = NULL, deleted_by = NULL WHERE id = $1`,
		noteID); err != nil {
		return noteError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return noteError(err)
	}
	return nil
}

// NoteDeletion is one entry in a note's deletion history.
type NoteDeletion struct {
	ID          uuid.UUID
	NoteID      uuid.UUID
	DeletedAt   time.Time
	DeletedBy   uuid.UUID
	UndeletedAt *time.Time
	UndeletedBy *uuid.UUID
}

// NoteDeletions returns a note's deletion history, newest first. This is the
// answer to "was this ever deleted, and by whom" — which notes.deleted_at
// cannot give once the note has been brought back.
func (s *Store) NoteDeletions(ctx context.Context, noteID uuid.UUID) ([]NoteDeletion, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, note_id, deleted_at, deleted_by, undeleted_at, undeleted_by
		  FROM tier2.note_deletions WHERE note_id = $1 ORDER BY deleted_at DESC`, noteID)
	if err != nil {
		return nil, fmt.Errorf("store: note deletions: %w", err)
	}
	defer rows.Close()
	out := []NoteDeletion{}
	for rows.Next() {
		var d NoteDeletion
		if err := rows.Scan(&d.ID, &d.NoteID, &d.DeletedAt, &d.DeletedBy,
			&d.UndeletedAt, &d.UndeletedBy); err != nil {
			return nil, fmt.Errorf("store: note deletions: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: note deletions: %w", err)
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
		case pgNoteColumnFrozen:
			// 0014's allow list. Unreachable through this package, which
			// updates only the columns it names; it exists so that the day a
			// new column is added and written, the error says which.
			return fmt.Errorf("store: that column of a note is not writable: %w", err)
		case pgNoteRewind:
			return fmt.Errorf("%w: %v", ErrRewind, err)
		case pgNoteDeleted, pgTagOnDeletedNote:
			return fmt.Errorf("%w: %v", ErrNoteDeleted, err)
		case pgConfirmerRequired:
			return fmt.Errorf("%w: %v", ErrConfirmerRequired, err)
		case pgDeletionRecordFixed:
			return fmt.Errorf("store: a deletion record is not rewritable: %w", err)
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
