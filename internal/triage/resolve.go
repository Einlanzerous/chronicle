package triage

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// T2 — lock the link row, then the memo row, decide, act, commit.
//
// THE BRANCH TABLE IS THE TICKET. Everything hard about CHRN-33 is which of
// these five arms a given attempt takes, and the two arms that look redundant
// are the ones that matter:
//
//	already confirmed?  → applied with the stored key. NO OUTWARD CALL.
//	memo not transcribed? → refused. No side effect, and nothing sent.
//	refused and not re-armed? → refused with the status that refused it.
//	row pre-existed?    → ambiguous → refused; otherwise failed, naming the
//	                      sweep. **NEVER CREATE.**
//	row is ours         → create with the STORED decision and STORED key,
//	                      confirm, advance the memo, commit.

// resolve runs T2 and turns its verdict into the client's result.
func (s *Service) resolve(ctx context.Context, res Result, memoID uuid.UUID, claim store.LinkClaim) Result {
	var outcome Result
	_, err := s.store.ResolveMemoLink(ctx, memoID, claim,
		func(ctx context.Context, att store.LinkAttempt) (store.LinkResolution, error) {
			r, o, err := s.decide(ctx, res, att)
			outcome = o
			return r, err
		})

	switch {
	case err == nil:
		return outcome
	case errors.Is(err, store.ErrLinkLocked):
		// Another batch, or the sweep, holds this memo and is mid-call. The
		// holder is bounded by CHRN-35's own timeout, so this is transient by
		// construction — and answering `failed` rather than waiting is what
		// stops one stuck memo queueing every retry of it behind a connection.
		return failed(res, "another decision for this memo is in flight; retry shortly")
	case errors.Is(err, store.ErrNotFound):
		return refuse(res, "no such memo, or it belongs to another author")
	}
	// Including the injected crash hooks: T2 rolled back, so the PENDING ROW
	// FROM T1 SURVIVES and the sweep owns what happens next. That is the whole
	// recovery design, arriving through its ordinary error path.
	return s.fail(ctx, res, "resolve memo link", err)
}

// decide is the branch table. It returns what the store should write, what the
// client should be told, and an error only for a failure that must roll the
// transaction back.
func (s *Service) decide(ctx context.Context, res Result, att store.LinkAttempt) (store.LinkResolution, Result, error) {
	link := att.Link
	leave := store.LinkResolution{Action: store.LinkLeave}

	// ---- 1 · Confirmed is terminal, and it answers before anything else. ----
	//
	// BEFORE THE MEMO-STATE CHECK, deliberately. A confirmed memo has already
	// advanced out of `transcribed`, so a state check placed first would refuse
	// every replay of a decision that landed — reporting "this memo moved" for
	// a memo that moved BECAUSE OF THIS DECISION. The row is the authority on
	// what happened to it; the memo's state is a consequence.
	if link.Confirmed() {
		return leave, s.applyLink(res, link), nil
	}

	// ---- 2 · The memo must still be where the operator left it. ----
	//
	// UNDER LOCK AND BEFORE THE OUTWARD CALL, and the ordering is the whole
	// point. Checked afterwards, a memo moved to `held` between the GET and the
	// POST would get a real ticket and then fail the advance — tier2.memos_guard
	// has no `held > triaged` edge — and T2 would roll back with the ticket
	// still standing behind it. The sweep would then find that ticket, confirm
	// the link, and fail the same advance on every later pass.
	if att.MemoState != store.StateTranscribed {
		r := refuse(res, memoMovedReason(att.MemoState))
		switch att.Claim {
		case store.ClaimInserted:
			// Nothing has been sent, nothing has happened, and this memo has no
			// earlier decision. The row we inserted a moment ago is removed
			// rather than marked: a refusal is an outcome of a DECISION THAT
			// WAS ATTEMPTED, and this one never reached the wire.
			return store.LinkResolution{Action: store.LinkDrop}, r, nil
		case store.ClaimRearmed:
			// A CORRECTION TO AN EARLIER REFUSAL, on a memo that has since been
			// held. Dropping here would delete the record of the decision this
			// one was correcting, and the operator would find the memo waiting
			// in the morning with no account of either attempt — which is the
			// exact failure "a refusal marks, it does not delete" exists to
			// prevent. Marked instead, which also keeps the sweep off it.
			return store.LinkResolution{
				Action: store.LinkRefuse, RefusedReason: r.Reason,
			}, r, nil
		}
		return leave, r, nil
	}

	// ---- 3 · A refusal that this decision did not re-arm. ----
	//
	// T1 re-arms a refused row when the decision DIFFERS, with a fresh
	// idempotency key. Reaching here means the resend was identical, and an
	// identical resend refuses identically — Switchyard has the same answer
	// cached under the same key. Saying so is more useful than replaying it.
	if link.Refused() {
		return leave, refuse(res, refusedReason(link)), nil
	}

	// ---- 4 · The row pre-existed. NEVER CREATE FOR ONE. ----
	//
	// This arm is why `ours` is threaded from T1 rather than inferred here. A
	// row somebody else wrote is the sweep's business, and creating for one
	// would be the accept path silently picking a winner: for a row already
	// flagged ambiguous, the stored key would replay whichever ticket won
	// Switchyard's cache and T2 would confirm that one, orphaning the other.
	if !att.Claim.Ours() {
		if link.Ambiguous() {
			return leave, refuse(res, fmt.Sprintf(
				"more than one ticket already claims this memo (%v) — a person has to say which, "+
					"because confirming either would orphan the other",
				link.CandidateKeys)), nil
		}
		return leave, failed(res,
			"another decision for this memo has not finished landing; the sweep will resolve it, "+
				"and this batch will report the outcome"), nil
	}

	// ---- 5 · Ours. ----

	// A destination with no outward call is confirmed here and now, through the
	// same code path as every other one — which is what makes a replayed
	// DISCARD answer `applied` from the link row rather than needing a case of
	// its own.
	if link.Destination != store.LinkTicket {
		return store.LinkResolution{
			Action:    store.LinkConfirm,
			AdvanceTo: store.StateDiscarded,
			Reason:    "discarded at triage",
		}, s.applyLink(res, link), nil
	}

	if s.beforeCreate != nil {
		if err := s.beforeCreate(link.MemoID); err != nil {
			return leave, res, err
		}
	}

	// THE STORED DECISION AND THE STORED KEY, never what this request composed.
	// It is the same call the sweep makes for the same row, which is what makes
	// a retry a replay instead of a second ticket.
	t, err := s.tickets.CreateTicket(ctx, switchyard.NewTicket{
		ProjectKey:     link.SentProjectKey,
		Type:           link.SentType,
		Title:          link.SentTitle,
		Description:    link.SentDescription,
		IdempotencyKey: link.SentIdempotencyKey,
		MemoID:         link.MemoID,
	})

	if s.afterCreate != nil {
		if hookErr := s.afterCreate(link.MemoID); hookErr != nil {
			return leave, res, hookErr
		}
	}

	if err != nil {
		var se *switchyard.Error
		if errors.As(err, &se) && !se.Retryable() {
			// MARKED, NOT DELETED. The operator is owed an account of why their
			// decision evaporated, and the row is the only thing that can give
			// them one. A corrected decision re-arms this same row with a fresh
			// key, which is what stops Switchyard's cached 4xx refusing the
			// correction for the next twenty-four hours.
			status := se.Status
			return store.LinkResolution{
				Action:        store.LinkRefuse,
				RefusedStatus: &status,
				RefusedReason: se.Error(),
			}, refuse(res, se.Error()), nil
		}
		// Retryable, or the call never completed. THE ROW STAYS PENDING: a
		// ticket may or may not exist behind it, and only the sweep can find
		// out. Deleting it here would be this path asserting there is no
		// ticket, which is exactly what it does not know.
		return leave, failed(res, err.Error()), nil
	}

	out := s.applyLink(res, link)
	out.TicketKey, out.TicketURL = t.Key, t.URL
	return store.LinkResolution{
		Action:    store.LinkConfirm,
		TicketKey: t.Key,
		AdvanceTo: store.StateTriaged,
	}, out, nil
}

// applyLink answers `applied` from a link row, resolving the deep link from the
// key rather than storing one. INVARIANT 2 in one line: the key is the handle,
// the URL is derived from it at render time, and there is no column holding it.
func (s *Service) applyLink(res Result, link store.MemoLink) Result {
	out := applied(res, link)
	out.TicketURL = s.tickets.TicketURL(out.TicketKey)
	return out
}

func memoMovedReason(state string) string {
	if state == store.StateHeld {
		return "this memo is on hold and cannot be triaged — releasing a hold is CHRN-34's, " +
			"and nothing was sent"
	}
	return fmt.Sprintf("this memo moved to %q since you fetched it, so it is no longer awaiting a decision", state)
}

func refusedReason(link store.MemoLink) string {
	if link.RefusedStatus != nil {
		return fmt.Sprintf("Switchyard already refused this decision with %d, and it has that answer cached: %s. "+
			"Change the decision — a different project, type, title or description — and it will be sent afresh.",
			*link.RefusedStatus, link.RefusedReason)
	}
	return link.RefusedReason + " — change the decision and it will be attempted afresh."
}

func failed(res Result, reason string) Result {
	res.Status = StatusFailed
	res.Reason = reason
	return res
}
