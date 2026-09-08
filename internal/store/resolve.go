package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// CHRN-46 — closing a discussion produces something durable, and the link runs
// both ways.
//
// CHRN-43 built the mechanical half: tier2.discussions.resolved_at /
// resolved_by / resolved_note_id, CH080 recording a resolution once, and
// ResolveDiscussion to write it. This file is the ACT — the note a thread
// becomes, written in the same transaction as the link that says so.
//
// WHY ONE TRANSACTION, and it is the whole reason this file exists rather than
// two calls at the call site: a note created without the link is a conclusion
// nothing points at, and a link to a note whose insert failed is a thread
// claiming a product it does not have. Both are the "second, worse archive"
// the ticket exists to prevent, arrived at from opposite directions.
//
// LOCK ORDER IS DISCUSSION THEN NOTE, here and in ResolveDiscussion, and both
// entry points take the discussion's row lock FIRST for that reason alone.
//
// The order is not free to choose, because one half of it is taken by the
// database rather than by this code. `resolved_note_id` is a non-deferrable
// foreign key to tier2.notes, so the resolving UPDATE's referential check takes
// a row lock on the NOTE — verified: with a concurrent `SELECT … FROM
// tier2.notes … FOR UPDATE` held, that UPDATE blocks and hits its lock_timeout
// with 55P03. ResolveDiscussion therefore locks the discussion, then the note,
// and nothing can make it do otherwise.
//
// An earlier version of this file did the reverse — appendRevisionTx took the
// note first, then resolveTx took the discussion — which put a genuine cycle
// between two legitimate callers resolving the same thread into the same note.
// Taking the discussion up front REMOVES the cycle rather than bounding it; the
// lock_timeout below is for ordinary contention, not for that.

// Resolution is the note a thread concluded into, when that note is new.
//
// THERE IS NO Verb FIELD, AND THAT IS RULING 4 MADE UNREPRESENTABLE rather than
// merely documented. The revision a resolution writes carries verb NULL, with
// tier2.discussions.resolved_note_id as its provenance — 0014:84-87's shape for
// a restore, where a NULL verb's sibling column says where the text came from.
// A Verb field here would be a field whose only correct value is the zero one.
//
// THERE IS NO AuthorID EITHER. The resolver authors the summary and confirms
// it: these are the same person by construction, because writing down what a
// conversation concluded IS the act of resolving it. CH080 and CH041 both
// require that person to be a person.
type Resolution struct {
	PageID uuid.UUID
	Title  string
	Body   string
}

// ResolveIntoNewNote closes a thread by writing the note it produced.
//
// The note and the resolution land together or not at all.
func (s *Store) ResolveIntoNewNote(ctx context.Context, discussionID, by uuid.UUID, in Resolution) (Note, NoteRevision, error) {
	if err := requireActor(by); err != nil {
		return Note{}, NoteRevision{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Note{}, NoteRevision{}, fmt.Errorf("store: resolve into a new note: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockDiscussionForResolve(ctx, tx, discussionID); err != nil {
		return Note{}, NoteRevision{}, err
	}

	note, rev, err := createNoteTx(ctx, tx, NewNote{
		PageID:      in.PageID,
		AuthorID:    by,
		ConfirmedBy: by,
		Title:       in.Title,
		Body:        in.Body,
		// Verb and MemoID stay nil — see Resolution.
	})
	if err != nil {
		return Note{}, NoteRevision{}, err
	}

	if err := resolveTx(ctx, tx, discussionID, by, &note.ID); err != nil {
		return Note{}, NoteRevision{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Note{}, NoteRevision{}, discussionError(err)
	}
	return note, rev, nil
}

// ResolveIntoExistingNote closes a thread into a note that is already written,
// by APPENDING a revision to it.
//
// THIS IS THE ORDINARY CASE AND NOT THE EXOTIC ONE. CHRN-46's own example —
// "Resolved into PRINCIPLES §6" — is a section of a long-lived document, and a
// page like that collects several threads' worth over time. That is also why
// tier2.discussions.resolved_note_id carries no UNIQUE: two threads resolving
// into one note is correct here, not a conflict.
//
// It APPENDS rather than replacing, so the note's earlier text survives as a
// revision — CHRN-39's rule, inherited rather than restated.
func (s *Store) ResolveIntoExistingNote(ctx context.Context, discussionID, noteID, by uuid.UUID, title, body string) (NoteRevision, error) {
	if err := requireActor(by); err != nil {
		return NoteRevision{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return NoteRevision{}, fmt.Errorf("store: resolve into an existing note: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockDiscussionForResolve(ctx, tx, discussionID); err != nil {
		return NoteRevision{}, err
	}

	rev, err := appendRevisionTx(ctx, tx, noteID, NewRevision{
		AuthorID:    by,
		ConfirmedBy: by,
		Title:       title,
		Body:        body,
	})
	if err != nil {
		return NoteRevision{}, err
	}

	if err := resolveTx(ctx, tx, discussionID, by, &noteID); err != nil {
		return NoteRevision{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return NoteRevision{}, discussionError(err)
	}
	return rev, nil
}

// ResolveWithoutNote closes a thread that produced nothing durable.
//
// IT IS A NAMED CALL RATHER THAN A NIL ARGUMENT, and that is the ticket's
// wording rather than style: "resolving without a note is allowed (some threads
// just end), but it should be a DELIBERATE CHOICE rather than the default path
// of least resistance." A nilable parameter makes the default the easiest thing
// to type. A caller reaching this function has said what it means.
//
// The thread stays readable — see DiscussionsOnPage.
func (s *Store) ResolveWithoutNote(ctx context.Context, discussionID, by uuid.UUID) error {
	return s.ResolveDiscussion(ctx, discussionID, by, nil)
}

// lockDiscussionForResolve takes the FIRST of the two row locks a resolution
// needs, before any note is touched — see the lock-order note at the top.
//
// IT ALSO SETS lock_timeout, which these transactions previously did not.
// AppendTurn sets one on the argument that "a statement run without one waits
// forever" (memolink.go:49), and a resolution holds a transaction across TWO
// row locks, so the argument applies here more than where it was written down.
//
// AND IT ANSWERS "no such thread" BEFORE A NOTE IS WRITTEN. Without it the
// note is inserted, the resolving UPDATE matches nothing, and the whole thing
// rolls back — correct, but it burns a note number to say ErrNotFound.
// AppendTurn:388 refuses on the locked row for the same reason.
func lockDiscussionForResolve(ctx context.Context, tx pgx.Tx, discussionID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+linkLockTimeout+`'`); err != nil {
		return fmt.Errorf("store: resolve: %w", err)
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT id FROM tier2.discussions WHERE id = $1 FOR UPDATE`, discussionID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return discussionError(err)
	}
	return nil
}

// resolveTx records the resolution inside a caller's transaction.
//
// IT RUNS ResolveDiscussion'S OWN STATEMENT, verbatim, from the shared
// constant. That is not tidiness: the statement carries the actor test that
// CH080 cannot do on a completion (see ResolveDiscussion), and a hand-copied
// second version is exactly how that test ends up present on one resolve path
// and missing on the other.
//
// The asymmetric COALESCE comes with it: resolved_at and resolved_by are kept
// if already set, so linking a note to a thread that resolved without one
// COMPLETES the record rather than being refused as a rewrite — while
// resolved_note_id is sent raw, so relinking to a DIFFERENT note reaches CH080
// instead of being silently dropped.
func resolveTx(ctx context.Context, tx pgx.Tx, discussionID, by uuid.UUID, note *uuid.UUID) error {
	// Both callers check first, and it is repeated here rather than trusted:
	// a divergence between this and ResolveDiscussion is exactly how the actor
	// test went missing on one path once already.
	if err := requireActor(by); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, resolveStatement, discussionID, by, note)
	if err != nil {
		return discussionError(err)
	}
	if tag.RowsAffected() == 0 {
		// Inside the caller's transaction, so the "which clause missed" read
		// sees this transaction's own writes — which is what makes it correct
		// here rather than merely convenient.
		var exists bool
		if err := tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM tier2.discussions WHERE id = $1)`,
			discussionID).Scan(&exists); err != nil {
			return discussionError(err)
		}
		if !exists {
			return ErrNotFound
		}
		return fmt.Errorf("%w: a discussion is resolved by a person, not by an agent", ErrConfirmerRequired)
	}
	return nil
}

// DiscussionsResolvedInto returns the threads that produced this note — the
// link read backwards, and the half of "linked both ways" a forward column
// cannot answer.
//
// IT IS A QUERY RATHER THAN A COLUMN ON tier2.notes, and that is the tier rule
// rather than laziness. A notes.discussion_id would be a second copy of a fact
// tier2.discussions already holds, and the two would drift the moment one was
// written without the other. It also could not represent the truth: a note may
// be what SEVERAL threads concluded, so the reverse link is a list. 0015's
// partial index discussions_resolved_note is what makes reading it cheap.
func (s *Store) DiscussionsResolvedInto(ctx context.Context, noteID uuid.UUID) ([]Discussion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+discussionColumns+`
		   FROM tier2.discussions WHERE resolved_note_id = $1 ORDER BY resolved_at`, noteID)
	if err != nil {
		return nil, fmt.Errorf("store: discussions resolved into: %w", err)
	}
	defer rows.Close()
	out := []Discussion{}
	for rows.Next() {
		var d Discussion
		if err := rows.Scan(discussionDest(&d)...); err != nil {
			return nil, fmt.Errorf("store: discussions resolved into: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: discussions resolved into: %w", err)
	}
	return out, nil
}

// DiscussionsOnPage lists the threads filed against a page, oldest first.
//
// RESOLVED THREADS ARE INCLUDED, and their inclusion is this ticket's third
// `Done when` rather than an oversight: "a resolved thread is still readable
// rather than hidden." This is the read surface where hiding would happen, so
// it is the one that has to say it — and it is the exact opposite of
// NotesOnPage, which filters soft-deleted notes because a delete one read
// surface forgets about is not a delete.
//
// The difference is what the two states MEAN. A deleted note was taken out of
// view on purpose. A resolved thread concluded, which is the most interesting
// thing a thread can do.
func (s *Store) DiscussionsOnPage(ctx context.Context, pageID uuid.UUID) ([]Discussion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+discussionColumns+`
		   FROM tier2.discussions WHERE page_id = $1 ORDER BY number`, pageID)
	if err != nil {
		return nil, fmt.Errorf("store: discussions on page: %w", err)
	}
	defer rows.Close()
	out := []Discussion{}
	for rows.Next() {
		var d Discussion
		if err := rows.Scan(discussionDest(&d)...); err != nil {
			return nil, fmt.Errorf("store: discussions on page: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: discussions on page: %w", err)
	}
	return out, nil
}
