package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// unroutedMemo ingests a memo and walks it to `to`, giving it a transcript on
// the way when withTranscript is set. Built on the owner's Store, because
// fixtures are tier-2 writes; the reads under test go through chronicle_tier1.
func unroutedMemo(t *testing.T, s *Store, ctx context.Context, label, to string, transcript string, partial bool) uuid.UUID {
	t.Helper()
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	sum := sha256.Sum256([]byte(label))
	hash := hex.EncodeToString(sum[:])
	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: owner.ID, ContentHash: hash, ByteSize: 1024,
		Source: SourceUpload, SourceRef: "test/" + hash[:8],
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	id := res.Memo.ID
	if transcript != "" {
		if _, err := s.RecordTranscript(ctx, TranscriptInput{
			MemoID: id, Text: transcript, Model: "whisper.cpp/small.en", Backend: "vulkan", Partial: partial,
		}); err != nil {
			t.Fatalf("RecordTranscript: %v", err)
		}
	}
	path := map[string][]string{
		StateCaptured:     {StateCaptured},
		StateQueued:       {StateCaptured, StateQueued},
		StateTranscribing: {StateCaptured, StateQueued, StateTranscribing},
		StateTranscribed:  {StateCaptured, StateQueued, StateTranscribing, StateTranscribed},
		StateTriaged:      {StateCaptured, StateQueued, StateTranscribing, StateTranscribed, StateTriaged},
		StateHeld:         {StateCaptured, StateQueued, StateTranscribing, StateTranscribed, StateHeld},
		StateDiscarded:    {StateCaptured, StateQueued, StateTranscribing, StateTranscribed, StateDiscarded},
	}[to]
	for i := 1; i < len(path); i++ {
		if _, err := s.AdvanceMemoState(ctx, id, path[i-1], path[i], ""); err != nil {
			t.Fatalf("advance %s -> %s: %v", path[i-1], path[i], err)
		}
	}
	return id
}

func ids(ms []Memo) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// CHRN-126 criterion 1: exactly the memos the Scribe still owes, newest first,
// read as chronicle_tier1 so a statement outside the grants fails here.
func TestUnroutedMemosIsExactlyWhatTheScribeStillOwes(t *testing.T) {
	s, ctx := newTestStore(t)
	pool := tier1Pool(t, ctx)
	defer pool.Close()
	t1 := NewTier1(pool)

	const other = "ollama/gemma4:31b@v0"
	owed := func(label string) uuid.UUID {
		return unroutedMemo(t, s, ctx, label, StateTranscribed, "a memo about "+label, false)
	}

	// Owed, oldest to newest by insertion — captured_at is now() per insert.
	plain := owed("plain")
	underOther := owed("under another proposer")
	held := owed("held for triage")
	excluded := owed("in backoff")
	newest := owed("the newest")

	// Not owed.
	for _, st := range []string{StateCaptured, StateQueued, StateTranscribing, StateTriaged, StateHeld, StateDiscarded} {
		unroutedMemo(t, s, ctx, "state "+st, st, "a memo in "+st, false)
	}
	unroutedMemo(t, s, ctx, "partial only", StateTranscribed, "half a sent", true)
	unroutedMemo(t, s, ctx, "no transcript at all", StateCaptured, "", false)
	for _, out := range []scribe.Outcome{
		{Proposal: &scribe.Proposal{Destination: scribe.DestNote, Confidence: 0.9, Reason: "r", Title: "t", Body: "b"},
			Raw: []byte(`{}`), Status: scribe.StatusValid},
		{Proposal: &scribe.Proposal{Destination: scribe.DestNote, Confidence: 0.9, Reason: "r", Title: "t", Body: "b"},
			Raw: []byte(`{}`), Status: scribe.StatusNeedsInput},
		{Raw: []byte("nope"), Status: scribe.StatusInvalid, Err: errors.New("scribe: invalid proposal")},
	} {
		id := owed("routed " + string(out.Status))
		tr, err := t1.TranscriptForScribe(ctx, id)
		if err != nil {
			t.Fatalf("TranscriptForScribe: %v", err)
		}
		if _, err := t1.SaveProposal(ctx, id, tr.ID, testProposer, out); err != nil {
			t.Fatalf("SaveProposal(%s): %v", out.Status, err)
		}
	}

	// A row under ANOTHER proposer does not make a memo routed (⚖1).
	tr, err := t1.TranscriptForScribe(ctx, underOther)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := t1.SaveProposal(ctx, underOther, tr.ID, other, validOutcome(0.9)); err != nil {
		t.Fatal(err)
	}
	// A triage hold defers a decision; the memo is still owed a proposal.
	owner, _ := s.GetOwner(ctx)
	if _, err := s.HoldForTriage(ctx, held, owner.ID, "later"); err != nil {
		t.Fatalf("HoldForTriage: %v", err)
	}

	got, err := t1.UnroutedMemos(ctx, testProposer, nil, 50)
	if err != nil {
		t.Fatalf("UnroutedMemos: %v", err)
	}
	want := []uuid.UUID{newest, excluded, held, underOther, plain}
	if !slices.Equal(ids(got), want) {
		t.Fatalf("UnroutedMemos = %v\nwant (newest first) %v", ids(got), want)
	}

	// exclude is applied before the limit, so a memo in backoff does not
	// spend a slot: with a limit of 2 and the newest excluded, the two that
	// come back are the next two.
	got, err = t1.UnroutedMemos(ctx, testProposer, []uuid.UUID{newest}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []uuid.UUID{excluded, held}; !slices.Equal(ids(got), want) {
		t.Fatalf("with newest excluded, limit 2 = %v, want %v", ids(got), want)
	}

	// Under the other proposer, underOther is the routed one and the three
	// routed under testProposer are owed: 4 + 3.
	got, err = t1.UnroutedMemos(ctx, other, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ids(got), underOther) || len(got) != 7 {
		t.Fatalf("under %s: %d memos %v, want 7 without %s", other, len(got), ids(got), underOther)
	}
}

// CHRN-126 criterion 11, the store half: the busy signal's two clauses and the
// states and jobs it deliberately ignores.
func TestTranscriptionBusyCountsCapturedAndRecentInFlightOnly(t *testing.T) {
	s, ctx := newTestStore(t)
	pool := tier1Pool(t, ctx)
	defer pool.Close()
	t1 := NewTier1(pool)
	const window = 60 * time.Minute

	busy := func() TranscriptionLoad {
		t.Helper()
		l, err := t1.TranscriptionBusy(ctx, window)
		if err != nil {
			t.Fatalf("TranscriptionBusy: %v", err)
		}
		return l
	}

	// Idle states only: queued, held and transcribed are not GPU work.
	unroutedMemo(t, s, ctx, "queued", StateQueued, "", false)
	unroutedMemo(t, s, ctx, "held", StateHeld, "x", false)
	unroutedMemo(t, s, ctx, "transcribed", StateTranscribed, "x", false)

	// A settled job, a failed one, an unsubmitted one, and one submitted 61
	// minutes ago and never collected.
	// memo_jobs_in_flight allows one unsettled attempt per memo, so each job
	// below gets a memo of its own.
	job := func(label string) MemoJob {
		t.Helper()
		m := unroutedMemo(t, s, ctx, label, StateQueued, "", false)
		j, err := s.BeginTranscription(ctx, m, "small.en", strings.Repeat("0", 64))
		if err != nil {
			t.Fatalf("BeginTranscription: %v", err)
		}
		return j
	}
	submit := func(j MemoJob) {
		t.Helper()
		if _, err := s.RecordJobSubmitted(ctx, j.ID, uuid.New()); err != nil {
			t.Fatalf("RecordJobSubmitted: %v", err)
		}
	}
	settled := job("settled")
	submit(settled)
	if err := s.RecordJobCollected(ctx, settled.ID); err != nil {
		t.Fatal(err)
	}
	failed := job("failed")
	submit(failed)
	if err := s.RecordJobFailure(ctx, failed.ID, "inference_failed", "boom"); err != nil {
		t.Fatal(err)
	}
	job("unsubmitted")
	stale := job("stale")
	submit(stale)
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier1.memo_jobs SET submitted_at = now() - interval '61 minutes' WHERE id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}

	if l := busy(); l.Busy() {
		t.Fatalf("busy = %+v with no captured memo and no recent in-flight job", l)
	}

	// A recent, unsettled job holds the GPU.
	live := job("live")
	submit(live)
	if l := busy(); !l.Busy() || l.InFlight != 1 || l.Captured != 0 {
		t.Fatalf("busy = %+v, want one in flight", l)
	}
	if err := s.RecordJobCollected(ctx, live.ID); err != nil {
		t.Fatal(err)
	}

	// A captured memo is about to be submitted.
	unroutedMemo(t, s, ctx, "captured", StateCaptured, "", false)
	if l := busy(); !l.Busy() || l.Captured != 1 || l.InFlight != 0 {
		t.Fatalf("busy = %+v, want one captured", l)
	}
}

func TestMemoStateReadsOnTheTier1Pool(t *testing.T) {
	s, ctx := newTestStore(t)
	pool := tier1Pool(t, ctx)
	defer pool.Close()
	t1 := NewTier1(pool)

	id := unroutedMemo(t, s, ctx, "state", StateTranscribed, "x", false)
	if got, err := t1.MemoState(ctx, id); err != nil || got != StateTranscribed {
		t.Fatalf("MemoState = %q, %v", got, err)
	}
	if _, err := t1.MemoState(ctx, uuid.New()); err != ErrNotFound {
		t.Fatalf("MemoState(unknown) err = %v, want ErrNotFound", err)
	}
}
