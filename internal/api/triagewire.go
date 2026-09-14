package api

import (
	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/triage"
)

// The triage payloads, mapped between the domain types and the generated ones.
//
// ============================================================================
// WHY THIS FILE EXISTS RATHER THAN `x-go-type` POINTING AT internal/triage.
// ============================================================================
//
// oapi-codegen can be told to alias a schema to an existing Go type, and for
// this group that would have been a tenth of the code: `triage.BatchItem`
// already carries json tags and was already serialised straight to the wire.
//
// It would also have inverted the contract. The document would then describe
// whatever the Go struct happened to say, `wire.BatchItem` would BE
// `triage.BatchItem`, and the staleness guard would stay green through any
// change to the domain type — because the generated file would still match a
// document that promised nothing. A guard that cannot fail is not one.
//
// So the shapes are transcribed into openapi.yaml and mapped here. The cost is
// this file; what it buys is that a field added to `triage.BatchItem` and not
// to the document does not silently reach three generated clients, and that
// `apitest.Conform` has something to check against that is not a restatement
// of the code it is checking.
//
// ============================================================================
// THESE ARE CHRN-32's AND CHRN-33's CONTRACTS, NOT NEW DECISIONS.
// ============================================================================
//
// Every field below exists because one of those tickets decided it should. The
// mapping is allowed to rename (Go's `DurationMS` is the wire's `duration_ms`)
// and nothing else — not to drop a field, not to flatten a nil into a zero, and
// not to decide that something looks redundant. The pair of "what the Scribe
// proposed" and "what it was derived from" is the whole reason a person can
// confirm a decision rather than rubber-stamp one.

func toBatchItems(in []triage.BatchItem) []wire.BatchItem {
	out := make([]wire.BatchItem, 0, len(in))
	for _, it := range in {
		item := wire.BatchItem{
			MemoId:        it.MemoID,
			CapturedAt:    it.CapturedAt,
			DurationMs:    it.DurationMS,
			Excerpt:       it.Excerpt,
			Proposer:      it.Proposer,
			Generation:    it.Generation,
			Status:        it.Status,
			PreAcceptable: it.PreAcceptable,
			ClearedFields: toClearedFields(it.Cleared),
		}
		if it.Error != "" {
			item.Error = &it.Error
		}
		if p := toProposal(it.Proposal); p != nil {
			item.Proposal = p
		}
		if l := toLinkState(it.Link); l != nil {
			item.Link = l
		}
		out = append(out, item)
	}
	return out
}

// toProposal renders the Scribe's proposal. Nil in, nil out: a memo with no
// usable proposal carries `error` instead, and inventing an empty Proposal
// there would make "the model said nothing useful" indistinguishable from "the
// model proposed a note with no title".
func toProposal(p *scribe.Proposal) *wire.Proposal {
	if p == nil {
		return nil
	}
	out := wire.Proposal{
		// THE TIER-1 MARKING (CHRN-100). A proposal is a derived row on the
		// tier-1 pool, and was the one tier-1 payload on the wire before the
		// estate wiki joined it — unmarked. Stamped here, on the way out,
		// because it is a fact about the store and not a field of the
		// Scribe's.
		Generated: generatedByChronicle(),

		Destination: wire.ProposalDestination(p.Destination),
		Confidence:  p.Confidence,
		Reason:      p.Reason,
		NearestPage: p.NearestPage,
		ProjectKey:  p.ProjectKey,
		PagePath:    p.PagePath,
		Verb:        verbOf(p.Verb),
		Title:       optional(p.Title),
		TicketType:  optional(p.TicketType),
		Description: optional(p.Description),

		// The three a first pass dropped, and the reason they matter more than
		// their size: TargetNote is what an `append` acts on and is required
		// unless the verb is `create`; Body and OpeningPost are the drafted
		// text acceptance writes. A person confirming without them is
		// confirming a change to authored text they cannot see, which is the
		// one thing the confirmation exists to prevent.
		TargetNote:  p.TargetNote,
		Body:        optional(p.Body),
		OpeningPost: optional(p.OpeningPost),
	}
	return &out
}

func toClearedFields(in []scribe.ClearedField) []wire.ClearedField {
	if len(in) == 0 {
		return nil
	}
	out := make([]wire.ClearedField, 0, len(in))
	for _, c := range in {
		out = append(out, wire.ClearedField{Field: c.Field, Value: c.Value, Reason: c.Reason})
	}
	return out
}

// toLinkState renders how a decision's journey to Switchyard ended, or has not.
//
// The four refused/swept fields travel as pointers because their ABSENCE is a
// fact: a link that was never swept is not the same as one swept and found
// clean, and `refused_status` is the one a client needs in order to know that a
// resend is pointless — Switchyard caches 4xx, so the remedy is a new key.
func toLinkState(l *triage.LinkState) *wire.LinkState {
	if l == nil {
		return nil
	}
	out := wire.LinkState{
		Destination:   l.Destination,
		State:         l.State,
		DecidedAt:     l.DecidedAt,
		TicketKey:     optional(l.TicketKey),
		TicketUrl:     optional(l.TicketURL),
		NoteRef:       optional(l.NoteRef),
		DiscussionRef: optional(l.DiscussionRef),
		CandidateKeys: l.CandidateKeys,
		SweptAt:       l.SweptAt,
		RefusedStatus: l.RefusedStatus,
		RefusedReason: optional(l.RefusedReason),
		RefusedAt:     l.RefusedAtTime,
	}
	return &out
}

func linkStates(in []triage.LinkState) []wire.LinkState {
	out := make([]wire.LinkState, 0, len(in))
	for i := range in {
		if l := toLinkState(&in[i]); l != nil {
			out = append(out, *l)
		}
	}
	return out
}

// fromDecisions maps a client's confirmations onto the service's input.
//
// `generation` travels back unchanged, which is the point of carrying it out:
// the service refuses a decision made against a proposal that has since been
// regenerated, rather than confirming text nobody saw.
func fromDecisions(in []wire.TriageDecision) []triage.Item {
	out := make([]triage.Item, 0, len(in))
	for _, d := range in {
		item := triage.Item{
			MemoID:      d.MemoId,
			Proposer:    deref(d.Proposer),
			Generation:  d.Generation,
			ConfirmEdit: deref(d.ConfirmEdit),
		}
		if o := d.Override; o != nil {
			item.Override = &triage.Override{
				Destination: deref(o.Destination),
				Title:       deref(o.Title),
				ProjectKey:  deref(o.ProjectKey),
				TicketType:  deref(o.TicketType),
				Description: deref(o.Description),
				Verb:        string(deref(o.Verb)),
				TargetNote:  deref(o.TargetNote),
				PagePath:    deref(o.PagePath),
				Body:        deref(o.Body),
				OpeningPost: deref(o.OpeningPost),
			}
		}
		out = append(out, item)
	}
	return out
}

func toResults(in []triage.Result) []wire.TriageResult {
	out := make([]wire.TriageResult, 0, len(in))
	for _, r := range in {
		out = append(out, wire.TriageResult{
			MemoId:        r.MemoID,
			Status:        r.Status,
			Destination:   optional(r.Destination),
			TicketKey:     optional(r.TicketKey),
			TicketUrl:     optional(r.TicketURL),
			NoteRef:       optional(r.NoteRef),
			DiscussionRef: optional(r.DiscussionRef),
			Generation:    r.Generation,
			Cleared:       toClearedFields(r.Cleared),
			Reason:        optional(r.Reason),
		})
	}
	return out
}

func toDeferredItem(d triage.DeferredItem) wire.DeferredItem {
	return wire.DeferredItem{
		MemoId:     d.MemoID,
		CapturedAt: d.CapturedAt,
		DurationMs: d.DurationMS,
		Excerpt:    optional(d.Excerpt),
		Reason:     optional(d.Reason),
		HeldBy:     d.HeldBy,
		HeldAt:     d.HeldAt,
		AgeSeconds: d.AgeSeconds,
	}
}

func toDeferredItems(in []triage.DeferredItem) []wire.DeferredItem {
	out := make([]wire.DeferredItem, 0, len(in))
	for _, d := range in {
		out = append(out, toDeferredItem(d))
	}
	return out
}

func toTriageReport(rep triage.AdminReport) wire.TriageReport {
	return wire.TriageReport{
		Backlog: wire.BacklogReport{
			Total:            rep.Backlog.Total,
			Today:            rep.Backlog.Today,
			ThisWeek:         rep.Backlog.ThisWeek,
			Older:            rep.Backlog.Older,
			OldestCapturedAt: rep.Backlog.OldestCapturedAt,
		},
		Deferred:   rep.Deferred,
		InFlight:   linkStates(rep.InFlight),
		Unresolved: linkStates(rep.Unresolved),
		Ambiguous:  linkStates(rep.Ambiguous),
		Refused:    linkStates(rep.Refused),
	}
}

// verbOf renders the Scribe's verb, absent when it proposed none — which is
// what a directly-authored memo has, and is not the same as proposing `create`.
func verbOf(v scribe.Verb) *wire.ProposalVerb {
	if v == "" {
		return nil
	}
	out := wire.ProposalVerb(v)
	return &out
}

// optional turns "" into absent.
//
// The domain types use the empty string for "not set" because that is what a
// Go struct does; the wire says so with omitempty on a pointer, so that a
// client can tell an absent `ticket_key` from one that is genuinely empty. The
// two are the same fact here, and this is the one place the translation lives.
func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// deref reads an optional wire field into the domain's zero-means-unset shape.
func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
