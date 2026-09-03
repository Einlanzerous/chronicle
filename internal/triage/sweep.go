package triage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// The sweep: what happens to a decision whose T2 never finished.
//
// T1 commits a pending row, then T2 calls Switchyard and confirms. Every gap in
// that sequence is a row that exists with no answer on it, and NOTHING LOCAL
// CAN SAY WHETHER A TICKET IS BEHIND IT. The process may have died before the
// call, after the call, or with the call in flight, and all three leave exactly
// the same row.
//
// So the sweep asks Switchyard, by memo, and acts on what it finds:
//
//	1 · exactly one  → confirm it. The trail is restored.
//	2 · none         → create, with the STORED decision and the STORED key.
//	3 · unreachable  → leave it pending. Try again next pass.
//	4 · more than one→ CONFIRM NOTHING. Two tickets claim one memo and picking
//	                   one orphans the other. It needs a person.
//	5 · refused (4xx)→ mark the row refused, so the operator is told rather
//	                   than watching the memo silently reappear.
//
// TWO THINGS ARE LOAD-BEARING AND EASY TO GET WRONG.
//
// ROWS ARE CLAIMED `FOR UPDATE SKIP LOCKED`, NEVER BY AGE. A pending row is
// committed the instant T1 returns, so an age-based sweep running while that
// item's own T2 is mid-call would see a row it considers abandoned, search,
// find nothing yet, and create — a duplicate manufactured by the very thing
// meant to prevent one. A row locked by its own T2 is in flight BY DEFINITION.
//
// THE MEMO-STATE RULE APPLIES HERE TOO, and at both doors. It is not enough to
// enforce it in T2: a pending row, a crash, an operator who holds the memo
// overnight, and a sweep that creates anyway would fail the advance on every
// pass forever.

// DefaultSweepInterval is how often the background pass runs. Long, because the
// sweep is a recovery mechanism and not a work queue: the ordinary path resolves
// its own rows, and every batch sweeps before it starts.
const DefaultSweepInterval = 5 * time.Minute

// maxSweepPass bounds one pass. A sweep that walked ten thousand rows would
// hold nothing that matters, but it would make one outward call per row and
// give nobody the chance to notice it going wrong.
const maxSweepPass = 50

// SweepReport is what one pass did. Counted rather than listed: an operator
// reads GET /admin/triage for the rows themselves.
type SweepReport struct {
	Examined int

	// Confirmed found the ticket already there; Created had to make it.
	Confirmed int
	Created   int

	// Refused is case 5 plus the memo-state refusals. Ambiguous is case 4 —
	// THE ONE THAT NEEDS A PERSON. Unresolved is case 3.
	Refused    int
	Ambiguous  int
	Unresolved int
}

func (r SweepReport) empty() bool { return r.Examined == 0 }

// Run sweeps on an interval until ctx is done. Wired like the retention pruner:
// it shares the server's context, so SIGTERM stops it.
func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = DefaultSweepInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			rep, err := s.Sweep(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				s.logger.ErrorContext(ctx, "triage sweep failed", "error", err)
				continue
			}
			if !rep.empty() {
				s.logSweep(ctx, rep)
			}
		}
	}
}

func (s *Service) logSweep(ctx context.Context, rep SweepReport) {
	// AMBIGUOUS IS A WARNING, not a count on an info line. It is the only
	// outcome here that no amount of retrying resolves, and the operator has to
	// be told there is something waiting on them.
	level := slog.LevelInfo
	if rep.Ambiguous > 0 {
		level = slog.LevelWarn
	}
	s.logger.Log(ctx, level, "triage sweep",
		"examined", rep.Examined, "confirmed", rep.Confirmed, "created", rep.Created,
		"refused", rep.Refused, "ambiguous", rep.Ambiguous, "unresolved", rep.Unresolved)
}

// Sweep makes one pass over the unresolved link rows.
//
// Rows it leaves pending are added to a skip list, so one unreachable
// Switchyard makes the pass end rather than spin on the oldest row.
func (s *Service) Sweep(ctx context.Context) (SweepReport, error) {
	var rep SweepReport
	var skip []uuid.UUID

	for i := 0; i < maxSweepPass; i++ {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		var outcome store.LinkAction
		var created, ambiguous bool

		link, err := s.store.SweepMemoLink(ctx, skip,
			func(ctx context.Context, att store.LinkAttempt) (store.LinkResolution, error) {
				r, madeOne, amb := s.sweepOne(ctx, att)
				outcome, created, ambiguous = r.Action, madeOne, amb
				return r, nil
			})
		switch {
		case errors.Is(err, store.ErrNotFound):
			// Nothing left to claim: every remaining row is either resolved or
			// held by somebody else's transaction, which is to say in flight.
			return rep, nil
		case errors.Is(err, store.ErrLinkLocked):
			return rep, nil
		case err != nil:
			return rep, err
		}

		rep.Examined++
		switch {
		case outcome == store.LinkConfirm && created:
			rep.Created++
		case outcome == store.LinkConfirm:
			rep.Confirmed++
		case outcome == store.LinkRefuse:
			rep.Refused++
		case ambiguous:
			rep.Ambiguous++
			skip = append(skip, link.ID)
		default:
			rep.Unresolved++
			skip = append(skip, link.ID)
		}
	}
	return rep, nil
}

// sweepOne resolves one claimed row. It reports whether it created a ticket and
// whether it found an ambiguity, because the caller counts those separately and
// neither is readable from the action alone.
func (s *Service) sweepOne(ctx context.Context, att store.LinkAttempt) (res store.LinkResolution, created, ambiguous bool) {
	link := att.Link

	// A destination that makes no outward call has nothing to search for. A
	// pending DISCARD is a T2 that died between the claim and the confirm, and
	// there is no external side effect to reconcile — just finish it.
	if link.Destination != store.LinkTicket {
		if link.Destination == store.LinkDiscard {
			return store.LinkResolution{
				Action:    store.LinkConfirm,
				AdvanceTo: advanceIfTranscribed(att, store.StateDiscarded),
				Reason:    "discarded at triage",
				Swept:     true,
			}, false, false
		}
		// NOTE and DISCUSSION are refused before T1 until CHRN-37 lands, so a
		// row here cannot have come from this code. Refused rather than left
		// pending forever, and the reason says what would have to ship.
		return refuseSweep(link, nil, fmt.Sprintf(
			"%s cannot land yet — it needs the page tree from CHRN-37", link.Destination)), false, false
	}

	found, err := s.tickets.TicketsByMemo(ctx, link.MemoID)
	if err != nil {
		var se *switchyard.Error
		if errors.As(err, &se) && !se.Retryable() {
			// Case 5, on the search rather than the create. The search is by
			// metadata that CHRN-35 stamps; a permanent refusal of it is a
			// decision that will never be resolvable.
			return refuseSweep(link, &se.Status, se.Error()), false, false
		}
		// CASE 3 · Unreachable. Leave it pending — and CARRY THE CANDIDATES
		// FORWARD. A pass that could not look must not erase what an earlier
		// pass found, or an ambiguity would silently downgrade to "unresolved"
		// on the first network blip and the sweep would then create a third
		// ticket.
		return store.LinkResolution{
			Action: store.LinkLeave, Swept: true, CandidateKeys: link.CandidateKeys,
		}, false, false
	}

	keys := make([]string, 0, len(found))
	for _, t := range found {
		keys = append(keys, t.Key)
	}

	switch len(found) {
	case 1:
		// CASE 1 · The trail is restored. If the memo has since been held, the
		// LINK IS STILL CONFIRMED AND THE MEMO IS LEFT ALONE: the ticket is
		// real and the trail back to the recording matters more than the state,
		// and whether a held memo may become triaged is CHRN-34's question and
		// not this ticket's to settle.
		return store.LinkResolution{
			Action:        store.LinkConfirm,
			TicketKey:     found[0].Key,
			AdvanceTo:     advanceIfTranscribed(att, store.StateTriaged),
			Swept:         true,
			CandidateKeys: keys,
		}, false, false

	case 0:
		// CASE 2 · Nothing was ever created.
		if att.MemoState != store.StateTranscribed {
			// DO NOT CREATE. The operator held this memo after the decision was
			// written; creating now would file a ticket they have already
			// stepped back from, and then fail the advance forever.
			return refuseSweep(link, nil,
				"this memo was put on hold before the decision landed, so nothing was created — "+
					"releasing a hold is CHRN-34's"), false, false
		}
		if s.beforeCreate != nil {
			if err := s.beforeCreate(link.MemoID); err != nil {
				return store.LinkResolution{Action: store.LinkLeave, Swept: true,
					CandidateKeys: link.CandidateKeys}, false, false
			}
		}
		t, err := s.tickets.CreateTicket(ctx, switchyard.NewTicket{
			ProjectKey:     link.SentProjectKey,
			Type:           link.SentType,
			Title:          link.SentTitle,
			Description:    link.SentDescription,
			IdempotencyKey: link.SentIdempotencyKey,
			MemoID:         link.MemoID,
		})
		if err != nil {
			var se *switchyard.Error
			if errors.As(err, &se) && !se.Retryable() {
				// CASE 5 · And this is also the answer to "the stored decision
				// can go stale". Case 2 re-sends sent_project_key WITHOUT
				// reconciling — there is no client here to answer needs_input
				// to — so a project archived since the decision was written
				// comes back 4xx, and this ends it with a reason the operator
				// can read instead of a retry every five minutes forever.
				return refuseSweep(link, &se.Status, se.Error()), false, false
			}
			return store.LinkResolution{Action: store.LinkLeave, Swept: true,
				CandidateKeys: link.CandidateKeys}, false, false
		}
		return store.LinkResolution{
			Action:        store.LinkConfirm,
			TicketKey:     t.Key,
			AdvanceTo:     store.StateTriaged,
			Swept:         true,
			CandidateKeys: []string{t.Key},
		}, true, false

	default:
		// CASE 4 · CONFIRM NOTHING. Two tickets carry this memo's id — a race
		// from before the pending row existed, or somebody who copied the
		// metadata by hand. Confirming either turns the other into an orphan
		// nobody will ever find, and there is no evidence here that says which.
		// The row stays pending, both keys are recorded, and a person decides.
		return store.LinkResolution{
			Action: store.LinkLeave, Swept: true, CandidateKeys: keys,
		}, false, true
	}
}

func refuseSweep(link store.MemoLink, status *int, reason string) store.LinkResolution {
	return store.LinkResolution{
		Action:        store.LinkRefuse,
		RefusedStatus: status,
		RefusedReason: reason,
		Swept:         true,
		CandidateKeys: link.CandidateKeys,
	}
}

// advanceIfTranscribed returns the target state only when the memo is still
// where the decision left it. Empty leaves the memo alone, which is not a
// no-op: it is the whole of the "confirm the link, leave the hold standing"
// rule, expressed where both callers reach it.
func advanceIfTranscribed(att store.LinkAttempt, to string) string {
	if att.MemoState == store.StateTranscribed {
		return to
	}
	return ""
}
