package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A LANDING IS ONE TRANSACTION, AND THAT IS THE WHOLE POINT OF THIS FILE.
//
// The ticket path is two: claim a pending tier2.memo_links row, commit, then
// lock it, call Switchyard, confirm. 0008's header is explicit that the row
// goes in FIRST and that it is "the only thing in the whole path that stops one
// memo becoming two tickets" — because a create that succeeds remotely and
// fails to record locally is the failure that matters.
//
// A NOTE or DISCUSSION landing makes no outward call. Nothing can succeed
// elsewhere and fail here, so the split T1/T2 exists to survive cannot happen —
// and keeping it would not be merely redundant, it would be wrong. Committed on
// its own, the claim leaves a PENDING NOTE ROW between the two commits, and
// sweepOne answers those before every batch: a process that died in the gap
// would find its identical replay refused under its own key, for a landing that
// had no side effect anywhere. CHRN-95 ruling 3.
//
// So: claim, write, confirm and advance, all on one transaction. A pending NOTE
// or DISCUSSION row cannot exist, which is a stronger claim than "the local
// arms need no sweep" and, unlike it, is something a test can assert.
//
// LOCK ORDER IS LINK, THEN MEMO, THEN NOTE. appendRevisionTx documents
// note-then-discussion for CHRN-46; this path adds three in front of that and
// never takes a discussion after a note, so there is no cycle.

// ErrAlreadyLanded reports that the memo was decided by somebody else's batch
// while this one was in flight, and that decision is confirmed. The link is
// returned with it: it carries what the memo became, so a caller answers
// `applied` with that rather than landing a second note.
var ErrAlreadyLanded = errors.New("store: this memo has already been landed")

// ErrMemoNotTranscribed reports that the memo moved out from under the decision
// between the batch's GET and its POST — held, most likely.
//
// CHECKED IN GO, UNDER THE LOCK, BECAUSE THE DATABASE CANNOT SAY IT KINDLY.
// tier2.memos_guard has no held → triaged edge, so a landing that skipped this
// would write the note, then fail the advance, then roll the note back — and
// report a trigger error as a transient failure. The memo is read FOR UPDATE
// first so the answer cannot change under it.
var ErrMemoNotTranscribed = errors.New("store: this memo is no longer awaiting triage")

// ErrNoPage reports a NOTE landing with no page to land on. Stage 2 clears a
// pathless NOTE into needs_input long before this (CHRN-95 ruling 7), so
// reaching it means a caller skipped reconciliation.
var ErrNoPage = errors.New("store: a note must land on a page")

// NoteLanding is a decided NOTE proposal in the shape the store needs.
//
// VERBS CROSS AS STRINGS. internal/scribe owns the enum and this package does
// not import it — NewNote.Verb is already a *string for the same reason.
type NoteLanding struct {
	Decision Decision

	// Verb is create, append, supersede or relate.
	Verb string

	// TargetNumber is the note the verb acts on, nil only for create. It is a
	// number rather than a CHR-#### because parsing a reference is the caller's
	// job and IsNoteRef is where it is done.
	TargetNumber *int64

	// PagePath is where a create lands. Empty is legal ONLY for relate, which
	// falls back to the target's page — "near" is what relate means, and the
	// target's page is where near is. For append and supersede it is ignored
	// entirely and MUST be: a landing that moved authored text because a model
	// named a different page would be acquiring a fifth verb by omission.
	PagePath string

	Title string
	Body  string

	// AuthorID is the MEMO'S author, and ConfirmedBy is the person who agreed
	// to the landing. CHRN-39's split: Scribe drafting the text does not make
	// Scribe the author, and a batch confirming it does not make the confirmer
	// one either.
	AuthorID    uuid.UUID
	ConfirmedBy uuid.UUID
}

// DiscussionLanding is a decided DISCUSSION proposal.
//
// It carries no target thread: ruling 8 keeps every landed discussion a NEW
// thread while candidate retrieval over the corpus is unowned.
type DiscussionLanding struct {
	Decision Decision

	Title string
	Body  string

	AuthorID    uuid.UUID
	ConfirmedBy uuid.UUID
}

// LandNote writes a note from a decided proposal and confirms the memo's link
// to it, in one transaction.
func (s *Store) LandNote(ctx context.Context, in NoteLanding) (MemoLink, Note, NoteRevision, error) {
	// ONLY A CREATE MUST NAME ONE. relate falls back to the target's page,
	// and append and supersede act on a note that already has one.
	if in.Verb == VerbCreate && in.PagePath == "" {
		return MemoLink{}, Note{}, NoteRevision{}, ErrNoPage
	}

	var link MemoLink
	var note Note
	var rev NoteRevision

	err := s.inLandingTx(ctx, in.Decision, &link, func(ctx context.Context, tx pgx.Tx, state string) error {
		var err error
		switch in.Verb {
		case VerbCreate, VerbRelate:
			note, rev, err = landNewNote(ctx, tx, in)
		case VerbAppend, VerbSupersede:
			note, rev, err = landOntoNote(ctx, tx, in)
		default:
			return fmt.Errorf("%w: unknown verb %q", ErrInvalidInput, in.Verb)
		}
		if err != nil {
			return err
		}

		link, err = confirmLandingTx(ctx, tx, link.ID, &note.ID, nil, in.ConfirmedBy)
		if err != nil {
			return err
		}
		_, err = advanceMemoState(ctx, tx, in.Decision.MemoID, state, StateTriaged, "landed as a note at triage")
		return err
	})
	if err != nil {
		return link, Note{}, NoteRevision{}, err
	}
	return link, note, rev, nil
}

// LandDiscussion opens a thread from a decided proposal and confirms the memo's
// link to it, in one transaction.
//
// TURN 1 IS AUTHORED BY THE MEMO'S AUTHOR, not by whoever accepted the batch.
// CH091 refuses an agent at seq 1 and would refuse Scribe; the confirmer is
// recorded on the link row, where CH023 refuses an agent in turn.
func (s *Store) LandDiscussion(ctx context.Context, in DiscussionLanding) (MemoLink, Discussion, DiscussionTurn, error) {
	var link MemoLink
	var disc Discussion
	var turn DiscussionTurn

	err := s.inLandingTx(ctx, in.Decision, &link, func(ctx context.Context, tx pgx.Tx, state string) error {
		var err error
		disc, turn, err = openDiscussionTx(ctx, tx, NewDiscussion{
			Title:    in.Title,
			AuthorID: in.AuthorID,
			Body:     in.Body,
			MemoID:   &in.Decision.MemoID,
		})
		if err != nil {
			return err
		}

		link, err = confirmLandingTx(ctx, tx, link.ID, nil, &disc.ID, in.ConfirmedBy)
		if err != nil {
			return err
		}
		_, err = advanceMemoState(ctx, tx, in.Decision.MemoID, state, StateTriaged, "landed as a discussion at triage")
		return err
	})
	if err != nil {
		return link, Discussion{}, DiscussionTurn{}, err
	}
	return link, disc, turn, nil
}

// inLandingTx runs the half of a landing both destinations share: the claim,
// the memo lock and its state check, and the commit.
//
// `link` is written before fn runs so that a caller can report what it found
// even when fn fails — ErrAlreadyLanded's whole value is the row it comes with.
func (s *Store) inLandingTx(ctx context.Context, d Decision, link *MemoLink,
	fn func(context.Context, pgx.Tx, string) error) error {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: land: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The claim below conflicts on UNIQUE (memo_id) and would otherwise wait on
	// the index with no deadline — ClaimMemoLink's comment has the long form.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+linkLockTimeout+`'`); err != nil {
		return fmt.Errorf("store: land: %w", err)
	}

	claimed, claim, err := claimMemoLinkTx(ctx, tx, d)
	if err != nil {
		return err
	}
	*link = claimed
	if !claim.Ours() {
		if claimed.Confirmed() {
			return ErrAlreadyLanded
		}
		// Somebody else's pending row. The same answer T1 gives a waiter,
		// because from a client's side these are one situation.
		return ErrLinkLocked
	}

	var state string
	err = tx.QueryRow(ctx, `SELECT state FROM tier2.memos WHERE id = $1 FOR UPDATE`,
		d.MemoID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: land: lock memo: %w", err)
	}
	if state != StateTranscribed {
		return ErrMemoNotTranscribed
	}

	if err := fn(ctx, tx, state); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return translateLinkError(noteError(err))
	}
	return nil
}

// landNewNote handles create and relate, which both write a note that did not
// exist and touch nothing that did.
func landNewNote(ctx context.Context, tx pgx.Tx, in NoteLanding) (Note, NoteRevision, error) {
	pageID, err := landingPage(ctx, tx, in)
	if err != nil {
		return Note{}, NoteRevision{}, err
	}
	verb := in.Verb
	return createNoteTx(ctx, tx, NewNote{
		PageID:      pageID,
		AuthorID:    in.AuthorID,
		Title:       in.Title,
		Body:        in.Body,
		MemoID:      &in.Decision.MemoID,
		ConfirmedBy: in.ConfirmedBy,
		Verb:        &verb,
	})
}

// landOntoNote handles append and supersede, which write into text somebody
// already wrote.
//
// THE CURRENT BODY IS READ INSIDE THIS TRANSACTION, AFTER THE NOTE IS LOCKED.
// appendRevisionTx takes Title and Body verbatim; "old + blank line + new" is
// the caller's arithmetic, and CurrentRevision runs on the pool. Composed from
// a pool read taken before the lock, an append would silently drop a revision
// committed in between — which is precisely the 3 a.m. failure CHRN-39's
// revision log exists to make recoverable, arriving one layer earlier.
func landOntoNote(ctx context.Context, tx pgx.Tx, in NoteLanding) (Note, NoteRevision, error) {
	if in.TargetNumber == nil {
		return Note{}, NoteRevision{}, fmt.Errorf("%w: %s needs a target note", ErrInvalidInput, in.Verb)
	}
	note, err := scanNote(tx.QueryRow(ctx,
		`SELECT `+noteColumns+` FROM tier2.notes WHERE number = $1 FOR UPDATE`, *in.TargetNumber))
	if err != nil {
		return Note{}, NoteRevision{}, err
	}
	if note.DeletedAt != nil {
		return Note{}, NoteRevision{}, ErrNoteDeleted
	}

	current, err := scanRevision(tx.QueryRow(ctx, `
		SELECT `+prefixed(revisionColumns, "r")+`
		  FROM tier2.notes n JOIN tier2.note_revisions r ON r.id = n.current_revision_id
		 WHERE n.id = $1`, note.ID))
	if err != nil {
		return Note{}, NoteRevision{}, err
	}

	// THE TITLE IS THE VERBS TABLE'S, not a convenience. An append adds to a
	// note that keeps its identity, so the title it already has is the title it
	// keeps. A supersede is a rewrite, and the proposal's title is required, so
	// it is the one that lands.
	title, body := in.Title, in.Body
	if in.Verb == VerbAppend {
		title = current.Title
		body = current.Body + "\n\n" + in.Body
	}

	verb := in.Verb
	rev, err := appendRevisionTx(ctx, tx, note.ID, NewRevision{
		AuthorID:    in.AuthorID,
		Title:       title,
		Body:        body,
		MemoID:      &in.Decision.MemoID,
		ConfirmedBy: in.ConfirmedBy,
		Verb:        &verb,
	})
	if err != nil {
		return Note{}, NoteRevision{}, err
	}
	return note, rev, nil
}

// landingPage answers where a new note goes.
//
// relate with no page_path lands on the TARGET'S page, because "near" is what
// relate means and the target's page is where near is. An explicit page_path
// overrides that: a model that named a page answered the question directly, and
// an explicit answer beats a derived one.
func landingPage(ctx context.Context, tx pgx.Tx, in NoteLanding) (uuid.UUID, error) {
	if in.PagePath != "" {
		return resolveOrCreatePathTx(ctx, tx, in.PagePath)
	}
	if in.Verb != VerbRelate || in.TargetNumber == nil {
		return uuid.Nil, ErrNoPage
	}
	var pageID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT page_id FROM tier2.notes WHERE number = $1 AND deleted_at IS NULL`,
		*in.TargetNumber).Scan(&pageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("store: landing page: %w", err)
	}
	return pageID, nil
}

// resolveOrCreatePathTx resolves a path, creating the segments that do not
// exist yet.
//
// CREATING IS THE CONTRACT, NOT A FALLBACK. Proposal.PagePath's own comment
// says it MAY name a page that does not exist, and stage 2 deliberately admits
// a path whose nearest live ancestor is there — precisely so a note can propose
// a new leaf. Refusing here would make that permission meaningless.
//
// Inside the caller's transaction, so a landing that fails afterwards leaves no
// half-built tree behind. CreatePage runs on the pool and cannot be used.
func resolveOrCreatePathTx(ctx context.Context, tx pgx.Tx, path string) (uuid.UUID, error) {
	segs, err := SplitPath(path)
	if err != nil {
		return uuid.Nil, err
	}

	var parent *uuid.UUID
	for _, slug := range segs {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT id FROM tier2.pages
			 WHERE slug = $1
			   AND parent_id IS NOT DISTINCT FROM $2`, slug, parent).Scan(&id)
		switch {
		case err == nil:
		case errors.Is(err, pgx.ErrNoRows):
			if err := tx.QueryRow(ctx, `
				INSERT INTO tier2.pages (parent_id, slug)
				VALUES ($1, $2)
				RETURNING id`, parent, slug).Scan(&id); err != nil {
				return uuid.Nil, pageError(err)
			}
		default:
			return uuid.Nil, fmt.Errorf("store: resolve path: %w", err)
		}
		next := id
		parent = &next
	}
	return *parent, nil
}

// confirmLandingTx writes what the memo became onto its link row.
//
// One UPDATE for the back-pointer, confirmed_at and confirmed_by together,
// because the four CHECKs 0017 adds are row-level: a confirmed NOTE row with a
// null note_id, or with no actor, is refused rather than written and repaired.
func confirmLandingTx(ctx context.Context, tx pgx.Tx, linkID uuid.UUID,
	noteID, discussionID *uuid.UUID, by uuid.UUID) (MemoLink, error) {

	if by == uuid.Nil {
		return MemoLink{}, fmt.Errorf("%w: no actor was supplied to confirm the landing", ErrInvalidInput)
	}
	return scanMemoLink(tx.QueryRow(ctx,
		`UPDATE tier2.memo_links
		    SET note_id       = $2,
		        discussion_id = $3,
		        confirmed_at  = now(),
		        confirmed_by  = $4
		  WHERE id = $1
		 RETURNING `+memoLinkColumns,
		linkID, noteID, discussionID, by))
}
