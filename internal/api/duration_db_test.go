package api

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
	"github.com/Einlanzerous/chronicle/internal/triage"
)

// CHRN-85. store.DurationSource's strings ARE the wire's enum, and durationOf
// casts between them. A cast cannot drop a duration the way a switch with a
// default would, and this is what stops it carrying a string the contract does
// not know: a source added on one side alone fails here.
func TestDurationSourceIsTheWiresEnum(t *testing.T) {
	for _, c := range []struct {
		store store.DurationSource
		wire  wire.MemoProvenanceDurationSource
	}{
		{store.DurationFromHeader, wire.MemoProvenanceDurationSourceMemoHeader},
		{store.DurationFromTranscript, wire.MemoProvenanceDurationSourceTranscript},
	} {
		got := wire.MemoProvenanceDurationSource(c.store)
		if got != c.wire || !got.Valid() {
			t.Errorf("store source %q casts to %q (valid=%v), want the wire's %q",
				c.store, got, got.Valid(), c.wire)
		}
	}
}

// ── a triage.Service that can only answer Batch, Hold and Deferred ──────────
//
// Those three read tier 2 and never touch a proposal, a ticket or the
// catalogue, so the stubs below are what a Service needs to exist and nothing
// more. A call to any of them that was not expected is a test that fails
// loudly rather than one that quietly passes.

type durNoTier1 struct{}

func (durNoTier1) ProposalsForMemos(context.Context, []uuid.UUID, string) (map[uuid.UUID]store.Proposal, error) {
	return map[uuid.UUID]store.Proposal{}, nil
}
func (durNoTier1) GetProposal(context.Context, uuid.UUID, string) (store.Proposal, error) {
	return store.Proposal{}, errors.New("durNoTier1: GetProposal is not part of this test")
}
func (durNoTier1) BumpProposalGeneration(context.Context, uuid.UUID, string, *scribe.Proposal,
	[]scribe.ClearedField, scribe.Status) (store.Proposal, error) {
	return store.Proposal{}, errors.New("durNoTier1: BumpProposalGeneration is not part of this test")
}

type durNoTickets struct{}

func (durNoTickets) CreateTicket(context.Context, switchyard.NewTicket) (switchyard.Ticket, error) {
	return switchyard.Ticket{}, errors.New("durNoTickets: CreateTicket is not part of this test")
}
func (durNoTickets) TicketsByMemo(context.Context, uuid.UUID) ([]switchyard.Ticket, error) {
	return nil, errors.New("durNoTickets: TicketsByMemo is not part of this test")
}
func (durNoTickets) TicketURL(string) string { return "" }

type durNoCatalogue struct{}

func ptrInt64(v int64) *int64 { return &v }

func (durNoCatalogue) Fetch(context.Context) (*catalogue.Snapshot, error) {
	return nil, errors.New("durNoCatalogue: Fetch is not part of this test")
}

// toTranscribed walks a memo to the state triage finds it in.
func (rig *memoDBRig) toTranscribed(t *testing.T, id uuid.UUID) {
	t.Helper()
	for _, step := range [][2]string{
		{store.StateCaptured, store.StateQueued},
		{store.StateQueued, store.StateTranscribing},
		{store.StateTranscribing, store.StateTranscribed},
	} {
		if _, err := rig.st.AdvanceMemoState(rig.ctx, id, step[0], step[1], ""); err != nil {
			t.Fatalf("advance %s -> %s: %v", step[0], step[1], err)
		}
	}
}

// CHRN-85, the drift test the plan's review asked for: ONE duration for a memo
// on every surface that shows one -- the triage batch, the hold response, the
// deferred list and its provenance -- because they all ask store.ResolveDuration
// and none of them chooses a column.
//
// The first three memos are the shapes prod actually has (measured 2026-09-26):
//
//	legacy    the m4a eval corpus: NULL header, transcript 34773 -> 34773
//	ogg       a phone's Ogg Opus: header 104000, transcript 104019, the constant
//	          19 ms the two columns differ by -> the header, 104000
//	unmeasured  no header and a transcript that recorded no duration -> null,
//	          and never a 0 that renders as 0:00
//
// The last two are reachable and not on prod yet, and they are what a review of
// this PR found: HoldForTriage gates on `transcribed` alone, so a memo whose only
// transcript is PARTIAL, or complete but from a model outside the durable set,
// can be held. The triage batch omits it (its durable floor, by design), and the
// deferred list still lists it -- so the duration cannot ride on the durable row
// the excerpt comes from, or the hold response and the list disagree about the
// same memo.
//
// Over real Postgres, because the risk this guards is a Scan list that drops
// the new column -- CHRN-118's trap -- and only a real query has one.
func TestEverySurfaceStatesTheSameDuration(t *testing.T) {
	rig := realMemos(t)
	svc, err := triage.New(triage.Options{
		Store: rig.st, Tier1: durNoTier1{}, Tickets: durNoTickets{}, Catalogue: durNoCatalogue{},
		Proposer: "test/proposer@v1",
	})
	if err != nil {
		t.Fatalf("triage.New: %v", err)
	}

	type shape struct {
		name       string
		body       string
		header     *int32
		transcript *int64
		wantMS     *int32
		wantSource *wire.MemoProvenanceDurationSource

		// model and partial default to a complete small.en, the durable case.
		model   string
		partial bool
		// notInBatch: the triage batch OMITS a memo with no durable transcript,
		// so this shape is only judged on the surfaces that can show it.
		notInBatch bool
	}
	fromTranscript := wire.MemoProvenanceDurationSourceTranscript
	fromHeader := wire.MemoProvenanceDurationSourceMemoHeader
	shapes := []*shape{
		{name: "legacy", body: "an m4a from the eval corpus", transcript: ptrTo(int64(34773)),
			wantMS: ptrTo(int32(34773)), wantSource: &fromTranscript},
		{name: "ogg", body: "an opus from the phone", header: ptrTo(int32(104000)), transcript: ptrTo(int64(104019)),
			wantMS: ptrTo(int32(104000)), wantSource: &fromHeader},
		{name: "unmeasured", body: "a transcript that measured nothing"},
		{name: "partial-only", body: "half of a th", transcript: ptrInt64(30000), partial: true, notInBatch: true,
			wantMS: ptrTo(int32(30000)), wantSource: &fromTranscript},
		{name: "not-a-durable-model", body: "a base.en transcript", transcript: ptrInt64(45000),
			model: "whisper.cpp/base.en", notInBatch: true,
			wantMS: ptrTo(int32(45000)), wantSource: &fromTranscript},
	}

	ids := make([]uuid.UUID, len(shapes))
	for i, s := range shapes {
		m := rig.memo(t, rig.owner, s.body, s.name+".m4a")
		ids[i] = m.ID
		if s.header != nil {
			if _, err := rig.st.SetMemoAudioInfo(rig.ctx, m.ID, store.AudioInfo{
				DurationMS: *s.header, Codec: "opus", SampleRateHz: 48000,
			}); err != nil {
				t.Fatalf("SetMemoAudioInfo(%s): %v", s.name, err)
			}
		}
		model := s.model
		if model == "" {
			model = "whisper.cpp/small.en"
		}
		if _, err := rig.st.RecordTranscript(rig.ctx, store.TranscriptInput{
			MemoID: m.ID, Text: s.body, Model: model, Backend: "vulkan", Partial: s.partial,
			AudioDurationMS: s.transcript,
		}); err != nil {
			t.Fatalf("RecordTranscript(%s): %v", s.name, err)
		}
		rig.toTranscribed(t, m.ID)
	}

	same := func(surface string, s *shape, got *int32) {
		t.Helper()
		switch {
		case s.wantMS == nil && got != nil:
			t.Errorf("%s: %s duration_ms = %d, want null (nothing measured it; never a zero)", surface, s.name, *got)
		case s.wantMS != nil && got == nil:
			t.Errorf("%s: %s duration_ms = null, want %d", surface, s.name, *s.wantMS)
		case s.wantMS != nil && *got != *s.wantMS:
			t.Errorf("%s: %s duration_ms = %d, want %d", surface, s.name, *got, *s.wantMS)
		}
	}
	byID := func(items []triage.BatchItem, id uuid.UUID) triage.BatchItem {
		t.Helper()
		for _, it := range items {
			if it.MemoID == id {
				return it
			}
		}
		t.Fatalf("memo %s is not in the batch", id)
		return triage.BatchItem{}
	}

	// 1. The batch, before anything is held.
	batch, err := svc.Batch(rig.ctx, rig.owner, triage.MaxLimit)
	if err != nil {
		t.Fatalf("Batch: %v", err)
	}
	for i, s := range shapes {
		if s.notInBatch {
			for _, it := range batch {
				if it.MemoID == ids[i] {
					t.Errorf("batch: %s is offered for triage, but it has no durable transcript", s.name)
				}
			}
			continue
		}
		same("batch", s, byID(batch, ids[i]).DurationMS)
	}

	// 2. The hold response: the site nobody had named. GetMemo carries no
	// transcript, so without its own read this returned the header alone and a
	// legacy memo was 34773 in the deferred list and null in the hold that put
	// it there.
	for i, s := range shapes {
		held, err := svc.Hold(rig.ctx, rig.owner, ids[i], "for later")
		if err != nil {
			t.Fatalf("Hold(%s): %v", s.name, err)
		}
		same("hold response", s, held.DurationMS)
	}

	// 3. The deferred list over the same rows.
	deferred, err := svc.Deferred(rig.ctx, rig.owner, triage.MaxLimit)
	if err != nil {
		t.Fatalf("Deferred: %v", err)
	}
	if len(deferred) != len(shapes) {
		t.Fatalf("deferred = %d items, want %d", len(deferred), len(shapes))
	}
	for i, s := range shapes {
		var found bool
		for _, d := range deferred {
			if d.MemoID == ids[i] {
				found = true
				same("deferred list", s, d.DurationMS)

				// THE EXCERPT STAYS ON THE DURABLE ROW while the duration moved
				// off it. A memo with no durable transcript is still listed here
				// (LEFT JOIN), with an empty excerpt and a real length: routing
				// evidence is gated on durability and the length is not. Without
				// this, only a reading of the SQL says the two were separated.
				if s.notInBatch && d.Excerpt != "" {
					t.Errorf("deferred list: %s carries excerpt %q from a transcript that is not durable", s.name, d.Excerpt)
				}
				if !s.notInBatch && d.Excerpt == "" {
					t.Errorf("deferred list: %s has a durable transcript and no excerpt", s.name)
				}
			}
		}
		if !found {
			t.Errorf("deferred list: %s is missing", s.name)
		}
	}

	// 4. Provenance, which resolved it first and now shares the rule.
	n, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
		Title: "One duration", Body: "From three memos.",
		MemoID: &ids[0], Verb: ptrTo(store.VerbCreate),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	for i := 1; i < len(ids); i++ {
		if _, err := rig.st.AppendRevision(rig.ctx, n.ID, store.NewRevision{
			AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
			Title: "One duration", Body: "And another.",
			MemoID: &ids[i], Verb: ptrTo(store.VerbAppend),
		}); err != nil {
			t.Fatalf("AppendRevision: %v", err)
		}
	}
	rec := rig.get(t, "/notes/"+n.Ref()+"/provenance", rig.ownerTok, "getNoteProvenance", http.StatusOK)
	items := decodeInto[wire.ProvenanceList](t, rec).Items
	if len(items) != len(shapes) {
		t.Fatalf("provenance = %d items, want %d", len(items), len(shapes))
	}
	for i, s := range shapes {
		var got *int32
		if items[i].DurationMs != nil {
			v := int32(*items[i].DurationMs)
			got = &v
		}
		same("provenance", s, got)
		switch {
		case s.wantSource == nil && items[i].DurationSource != nil:
			t.Errorf("provenance: %s names source %q for a null duration", s.name, *items[i].DurationSource)
		case s.wantSource != nil && (items[i].DurationSource == nil || *items[i].DurationSource != *s.wantSource):
			t.Errorf("provenance: %s source = %v, want %q", s.name, items[i].DurationSource, *s.wantSource)
		}
	}

	// NOTHING WAS COPIED. Every surface above answered from a read, and the
	// legacy memo's own column is still empty: the transcript's number was never
	// written onto the memo (criterion 4, the negative of the backfill shape).
	legacy, err := rig.st.GetMemo(rig.ctx, ids[0])
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}
	if legacy.DurationMS != nil {
		t.Errorf("memos.duration_ms = %d for the m4a memo, want NULL: the transcript's duration is READ, never copied onto the memo",
			*legacy.DurationMS)
	}
}
