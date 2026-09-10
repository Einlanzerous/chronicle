package triage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// land commits a NOTE or a DISCUSSION.
//
// It is step 6 and step 7 at once, and that is ruling 3: no outward call means
// nothing can succeed elsewhere and fail here, so the claim, the write and the
// confirm are one transaction. The store owns the ordering; this function owns
// the two things that have to happen BEFORE the transaction opens — deciding
// that the actor may write authored text at all, and composing the text.
func (s *Service) land(ctx context.Context, res Result, actor store.User, memo store.Memo,
	decided *scribe.Proposal, d store.Decision) Result {

	// AN AGENT IS REFUSED HERE, WITH A REASON, RATHER THAN AT THE DATABASE.
	//
	// canDecide admits an admin or the memo's author and says nothing about
	// kind, so an agent account holding a session or a token that reaches this
	// endpoint — CHRN-67 is the obvious route — would pass it. It would then
	// meet CH041 on the note revision, or CH023 on the link row, as a trigger
	// error, which `fail` reports as a TRANSIENT failure: the client would be
	// invited to retry something that can never succeed.
	//
	// CHRN-39's rule is that a person confirms. Naming it costs one branch, and
	// it is the kind of line that is missing until somebody hits it.
	if actor.Kind != store.KindPerson {
		return refuse(res, "a landing into the corpus is confirmed by a person — "+
			"an agent may propose this decision but may not be the one who accepts it")
	}

	switch decided.Destination {
	case scribe.DestDiscussion:
		return s.landDiscussion(ctx, res, actor, memo, decided, d)
	default:
		return s.landNote(ctx, res, actor, memo, decided, d)
	}
}

func (s *Service) landNote(ctx context.Context, res Result, actor store.User, memo store.Memo,
	decided *scribe.Proposal, d store.Decision) Result {

	in := store.NoteLanding{
		Decision:    d,
		Verb:        string(decided.Verb),
		Title:       decided.Title,
		Body:        decided.Body,
		AuthorID:    memo.AuthorID,
		ConfirmedBy: actor.ID,
	}
	if decided.PagePath != nil {
		in.PagePath = *decided.PagePath
	}

	if decided.Verb.NeedsTarget() {
		if decided.TargetNote == nil {
			return refuse(res, fmt.Sprintf("a %s needs the note it acts on", decided.Verb))
		}
		n, err := store.ParseNoteRef(*decided.TargetNote)
		if err != nil {
			return refuse(res, err.Error())
		}
		in.TargetNumber = &n
	}

	// APPEND AND SUPERSEDE IGNORE page_path, AND SPECIFICALLY IT IS NOT A MOVE.
	//
	// The note they act on already has a page. A model that named a different
	// one has said where it thinks the note belongs, which is a fifth verb
	// nobody granted — and acquiring it here, silently, as a side effect of
	// adding a paragraph, is exactly the unattended write to authored text this
	// ticket exists to bound. Cleared rather than passed, so the store cannot
	// read it by accident.
	if decided.Verb == scribe.VerbAppend || decided.Verb == scribe.VerbSupersede {
		in.PagePath = ""
	}

	if decided.Verb == scribe.VerbRelate {
		in.Body = relateBody(decided.Title, decided.Body, *in.TargetNumber)
	}

	link, _, _, err := s.store.LandNote(ctx, in)
	return s.landed(ctx, res, link, err)
}

func (s *Service) landDiscussion(ctx context.Context, res Result, actor store.User, memo store.Memo,
	decided *scribe.Proposal, d store.Decision) Result {

	link, _, _, err := s.store.LandDiscussion(ctx, store.DiscussionLanding{
		Decision: d,
		Title:    decided.Title,
		// TURN 1 IS THE OPENING POST, authored by the memo's author. Scribe
		// drafting the words does not make Scribe the author, and CH091 would
		// refuse it at seq 1 if anybody tried.
		Body:        decided.OpeningPost,
		AuthorID:    memo.AuthorID,
		ConfirmedBy: actor.ID,
	})
	return s.landed(ctx, res, link, err)
}

// landed turns the store's answer into a result.
//
// ErrAlreadyLanded is `applied` AND NOT A FAILURE. Another batch confirmed this
// memo while this one was in flight; the row it comes back with carries what
// the memo became, so the honest answer is the one step 2 would have given a
// moment later. That is also what makes a replayed batch idempotent — the
// second call reports the first call's note rather than writing a second.
func (s *Service) landed(ctx context.Context, res Result, link store.MemoLink, err error) Result {
	switch {
	case err == nil, errors.Is(err, store.ErrAlreadyLanded):
		return s.applyLink(ctx, res, link)

	case errors.Is(err, store.ErrNotFound):
		return refuse(res, "no such memo, or it belongs to another author")

	case errors.Is(err, store.ErrMemoNotTranscribed):
		// The memo was held between the GET and the POST. Not a failure and
		// not a stale payload: the decision is fine and the memo is not
		// available to receive it.
		return refuse(res, "this memo is no longer awaiting triage — it was held or decided "+
			"since you fetched it; release it and decide again")

	case errors.Is(err, store.ErrLinkLocked):
		return failed(res, "another decision for this memo is in flight; retry shortly")

	case errors.Is(err, store.ErrLinkKeyReused):
		return refuse(res, "this decision was already refused under its own key; change the decision "+
			"and it will be attempted afresh")

	case errors.Is(err, store.ErrNoPage):
		// Stage 2 clears a pathless NOTE into needs_input, so this is
		// unreachable from the accept path and is a bug rather than an
		// operator's problem if it ever fires.
		return refuse(res, "this note names no page to land on")

	case errors.Is(err, store.ErrNoteDeleted):
		return refuse(res, "that note has been deleted; it cannot be appended to or superseded")

	default:
		return s.fail(ctx, res, "land", err)
	}
}

// relateBody appends the reference that makes a `relate` an actual link.
//
// CHRN-39 handed this here: a Scribe-proposed body will not contain CHR-0311 on
// its own, and tier1.note_links is DERIVED from the text by reindexLinks. No
// reference in the body means no edge in the graph, and `relate` would produce
// a note that relates to nothing.
//
// ONLY WHEN THE BODY DOES NOT ALREADY SAY IT (CHRN-95 ruling 6). A model asked
// to relate two ideas often quotes the target in its prose, and appending
// anyway prints the number twice — once as a sentence and once as a footer. The
// graph is identical either way, since reindexLinks de-duplicates by number;
// this is about what the note reads like to the person who opens it.
//
// CHECKED WITH markdown.References OVER TITLE AND BODY, which is exactly what
// reindexLinks parses — the same function over the same text, so the check and
// the consequence cannot disagree. It skips code spans and fenced blocks, so a
// body that mentions the target only inside backticks correctly gains the
// footer: that mention would not have produced an edge either.
func relateBody(title, body string, target int64) string {
	for _, r := range markdown.References([]byte(title + "\n\n" + body)) {
		if r.System == markdown.SystemChronicle && r.Target == markdown.TargetNote &&
			r.Number == target {
			return body
		}
	}
	// A trailing line after a blank one, so it reads as a footer and survives
	// an edit that rewrites everything above it.
	return strings.TrimRight(body, "\n") + "\n\nRelated: " + store.FormatNoteRef(target)
}

// landedRef renders the handle of whatever a memo became locally.
//
// THE HANDLE IS READ, NOT STORED. tier2.memo_links carries the row id of what
// the memo became; CHR-0311 and DSC-0007 are rendered from the number on that
// row. Denormalising the handle onto the link would put a second copy of a
// value tier2.notes already owns into the very table that exists to point at it
// — a small copy, and exactly the kind that goes stale unnoticed.
//
// A FAILED READ DOES NOT FAIL THE ANSWER. The landing committed and the memo is
// decided; saying so without the handle beats reporting a failure for a write
// that succeeded. The miss is logged, because a link pointing at a note that
// cannot be read is a real problem somewhere else.
func (s *Service) landedRef(ctx context.Context, link store.MemoLink) (noteRef, discussionRef string) {
	switch {
	case link.NoteID != nil:
		n, err := s.store.NoteByID(ctx, *link.NoteID)
		if err != nil {
			s.logger.ErrorContext(ctx, "triage could not render a landed note's handle",
				"memo_id", link.MemoID, "note_id", *link.NoteID, "error", err)
			return "", ""
		}
		return store.FormatNoteRef(n.Number), ""
	case link.DiscussionID != nil:
		d, err := s.store.DiscussionByID(ctx, *link.DiscussionID)
		if err != nil {
			s.logger.ErrorContext(ctx, "triage could not render a landed thread's handle",
				"memo_id", link.MemoID, "discussion_id", *link.DiscussionID, "error", err)
			return "", ""
		}
		return "", store.FormatDiscussionRef(d.Number)
	}
	return "", ""
}
