package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CHRN-22's gate, at the level it is written. Mode C, so these are not
// illustrations: each refusal is a CONTROL AND A VARIANT swept together, where
// the variant differs from the control in exactly one respect. If the clause
// that refuses it were deleted, the variant would appear beside the control and
// the test fails — which is what "mutation-tested" has to mean without editing
// the source to prove it.
//
// The window is a nanosecond throughout, so every memo is past it: captured_at
// is immutable (CH002) and cannot be backdated, which is the point of the
// column and an obstacle to testing it any other way.

const testWindow = time.Nanosecond

// durable gives a memo a transcript that satisfies the floor.
func durable(t *testing.T, s *Store, ctx context.Context, memoID uuid.UUID, model string) {
	t.Helper()
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: memoID, Text: "a thought, spoken", Partial: false,
		Model: model, Backend: "vulkan",
	}); err != nil {
		t.Fatalf("RecordTranscript: %v", err)
	}
}

// prunableIDs is the dry run as a set, so a test can ask "is this one in it".
func prunableIDs(t *testing.T, s *Store, ctx context.Context) map[uuid.UUID]bool {
	t.Helper()
	rows, err := s.PrunableAudio(ctx, testWindow, 100)
	if err != nil {
		t.Fatalf("PrunableAudio: %v", err)
	}
	out := map[uuid.UUID]bool{}
	for _, r := range rows {
		out[r.MemoID] = true
	}
	return out
}

// control is a memo that MUST be prunable, so that a variant's absence means
// the clause under test refused it rather than the fixture being broken.
func control(t *testing.T, s *Store, ctx context.Context, email string) Memo {
	t.Helper()
	m := newTranscribableMemo(t, s, ctx, email)
	durable(t, s, ctx, m.ID, "whisper.cpp/small.en")
	return m
}

// THE SIX REFUSALS. Every one of them is a recording this system would
// otherwise destroy while its only transcript was absent, incomplete, or made
// by something whose output has never been measured.
func TestThePrunableGateRefusesTheSixCases(t *testing.T) {
	s, ctx := newTestStore(t)
	ok := control(t, s, ctx, "prune-control@example.test")

	// 1 · never transcribed. The audio is the only copy of that thought.
	pending := newTranscribableMemo(t, s, ctx, "prune-pending@example.test")

	// 2 · transcription ran and did not complete.
	partial := newTranscribableMemo(t, s, ctx, "prune-partial@example.test")
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: partial.ID, Text: "half a th", Partial: true,
		Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatal(err)
	}

	// 3 · below the QUALITY floor. E3's CPU fallback is base.en: server-run and
	// phone-grade, which is exactly why a provenance-only rule is wrong.
	tooSmall := newTranscribableMemo(t, s, ctx, "prune-base@example.test")
	durable(t, s, ctx, tooSmall.ID, "whisper.cpp/base.en")

	// 4 · an UNKNOWN RUNNER at a sufficient model. CHRN-81's phone: a quantised
	// build nothing here has measured is not trusted to have produced what its
	// model name claims.
	unknownRunner := newTranscribableMemo(t, s, ctx, "prune-device@example.test")
	durable(t, s, ctx, unknownRunner.ID, "whisperkit/small.en")

	// 5 · a model name with NO RUNNER AT ALL. The shape every hand-written
	// fixture used before this ticket, and the shape the corpus never contains.
	unqualified := newTranscribableMemo(t, s, ctx, "prune-bare@example.test")
	durable(t, s, ctx, unqualified.ID, "small.en")

	// 6 · pinned. `forever` means forever.
	pinned := newTranscribableMemo(t, s, ctx, "prune-pinned@example.test")
	durable(t, s, ctx, pinned.ID, "whisper.cpp/small.en")
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET retention = 'forever' WHERE id = $1`, pinned.ID); err != nil {
		t.Fatal(err)
	}

	got := prunableIDs(t, s, ctx)
	if !got[ok.ID] {
		t.Fatal("the control was not prunable, so every assertion below proves nothing")
	}
	for _, c := range []struct {
		name string
		id   uuid.UUID
	}{
		{"never transcribed", pending.ID},
		{"partial transcript", partial.ID},
		{"below the model floor (base.en)", tooSmall.ID},
		{"unknown runner (whisperkit)", unknownRunner.ID},
		{"model name with no runner", unqualified.ID},
		{"pinned forever", pinned.ID},
	} {
		if got[c.id] {
			t.Errorf("%s was listed for deletion beside the control; "+
				"the clause that refuses it is not doing anything", c.name)
		}
	}
}

// THE TWO ACCEPTANCES, and the first is the trap CHRN-25 §5 argued about:
// EMPTY TEXT WITH A COMPLETED RUN IS DURABLE. Forty seconds of silence has a
// true and complete answer, and refusing it would keep the audio of exactly the
// memos most worth pruning — with the PRUNES label quietly lying about them.
func TestThePrunableGateAcceptsEmptyTextAndDiscardNow(t *testing.T) {
	s, ctx := newTestStore(t)

	silent := newTranscribableMemo(t, s, ctx, "prune-silent@example.test")
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: silent.ID, Text: "", Partial: false,
		Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatal(err)
	}

	discard := newTranscribableMemo(t, s, ctx, "prune-discard@example.test")
	durable(t, s, ctx, discard.ID, "whisper.cpp/medium.en")
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET retention = 'discard_now' WHERE id = $1`, discard.ID); err != nil {
		t.Fatal(err)
	}

	got := prunableIDs(t, s, ctx)
	if !got[silent.ID] {
		t.Fatal("a silent memo with a completed run was refused; the pruner would never fire " +
			"for it and its PRUNES label would be a lie")
	}
	if !got[discard.ID] {
		t.Fatal("a DISCARD NOW memo with a durable transcript was refused")
	}
}

// DISCARD NOW STILL WAITS FOR THE TRANSCRIPT. CHRN-18 §5 keeps the two apart:
// the retention governs the audio file, and the gate is not optional for it.
func TestDiscardNowStillWaitsForADurableTranscript(t *testing.T) {
	s, ctx := newTestStore(t)
	ok := control(t, s, ctx, "discard-control@example.test")

	m := newTranscribableMemo(t, s, ctx, "discard-untranscribed@example.test")
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET retention = 'discard_now' WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}

	got := prunableIDs(t, s, ctx)
	if !got[ok.ID] {
		t.Fatal("the control was not prunable")
	}
	if got[m.ID] {
		t.Fatal("DISCARD NOW deleted audio that was never transcribed; the gate is not optional")
	}
}

// THE PRUNER DOES NOT READ `state`. CHRN-18 §6 hands the rule over: a
// destructive job must not rest on a second, softer fact, and a bug in the
// state machine must not be able to become data loss.
func TestTheGateIgnoresMemoState(t *testing.T) {
	s, ctx := newTestStore(t)

	discarded := control(t, s, ctx, "state-discarded@example.test")
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET state = 'discarded' WHERE id = $1`, discarded.ID); err != nil {
		t.Fatal(err)
	}

	if !prunableIDs(t, s, ctx)[discarded.ID] {
		t.Fatal("a discarded memo was treated differently; the gate is the transcript, full stop")
	}
}

// THE MARK IS A COMPARE-AND-SWAP OVER THE WHOLE PREDICATE. The window between
// listing a memo and unlinking its file is the window in which a person pins it,
// and under a plain `WHERE id = $1` their pin would be honoured by the database
// and ignored by the pruner.
func TestPinningBetweenTheReadAndTheMarkWins(t *testing.T) {
	s, ctx := newTestStore(t)
	m := control(t, s, ctx, "pin-race@example.test")

	if !prunableIDs(t, s, ctx)[m.ID] {
		t.Fatal("setup: the memo was not prunable")
	}

	// The person pins it after the sweep read the list and before it marks.
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET retention = 'forever' WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}

	claimed, err := s.MarkAudioPruned(ctx, m.ID, testWindow)
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("the mark ignored a pin that landed after the read; the audio would have been " +
			"unlinked for a memo somebody had just asked to keep forever")
	}
}

// A claim is taken once. Two sweeps racing must not both unlink.
func TestTheMarkIsTakenOnlyOnce(t *testing.T) {
	s, ctx := newTestStore(t)
	m := control(t, s, ctx, "mark-once@example.test")

	first, err := s.MarkAudioPruned(ctx, m.ID, testWindow)
	if err != nil || !first {
		t.Fatalf("first claim = %v, %v; want it taken", first, err)
	}
	second, err := s.MarkAudioPruned(ctx, m.ID, testWindow)
	if err != nil {
		t.Fatal(err)
	}
	if second {
		t.Fatal("the same memo was claimed twice; two sweeps would both unlink it")
	}
	if prunableIDs(t, s, ctx)[m.ID] {
		t.Fatal("a marked memo is still listed for deletion")
	}
}

// THE STATUS AND THE JOB ARE THE SAME PREDICATE, which is how "the date the UI
// shows is the date the job uses" holds by construction. `pruned` is checked
// FIRST — it is the state this job produces, so a status function without it
// has an undefined case on exactly the rows the pruner creates.
func TestRetentionStatusMatchesWhatTheJobWillDo(t *testing.T) {
	s, ctx := newTestStore(t)

	scheduled := control(t, s, ctx, "status-sched@example.test")
	awaiting := newTranscribableMemo(t, s, ctx, "status-await@example.test")
	pinned := control(t, s, ctx, "status-pinned@example.test")
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET retention = 'forever' WHERE id = $1`, pinned.ID); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		id      uuid.UUID
		want    string
		hasDate bool
	}{
		{scheduled.ID, RetentionScheduled, true},
		{awaiting.ID, RetentionAwaitingTranscript, false},
		{pinned.ID, RetentionStatusPinned, false},
	} {
		got, at, err := s.RetentionStatus(ctx, c.id, testWindow)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Fatalf("status %q, want %q", got, c.want)
		}
		if (at != nil) != c.hasDate {
			t.Fatalf("%s carries date %v; a status with no date the job will use must not "+
				"invent one, and one with a date must show it", got, at)
		}
	}

	// And after the mark, `pruned` — checked before everything else.
	if _, err := s.MarkAudioPruned(ctx, scheduled.ID, testWindow); err != nil {
		t.Fatal(err)
	}
	got, at, err := s.RetentionStatus(ctx, scheduled.ID, testWindow)
	if err != nil {
		t.Fatal(err)
	}
	if got != RetentionPruned || at == nil {
		t.Fatalf("status %q at %v after the mark; want pruned with the date it happened", got, at)
	}
}

// A pruned memo's TRANSCRIPT SURVIVES. The asymmetry is the design, and this is
// the assertion that says so where the deletion happens.
func TestPruningNeverTouchesTheTranscript(t *testing.T) {
	s, ctx := newTestStore(t)
	m := control(t, s, ctx, "keep-transcript@example.test")

	if _, err := s.MarkAudioPruned(ctx, m.ID, testWindow); err != nil {
		t.Fatal(err)
	}
	durableStill, err := s.HasDurableTranscript(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !durableStill {
		t.Fatal("pruning the audio took the transcript with it")
	}
}

// CHRN-22 Ruling 2: audio is delivered once. A memo whose audio is merely
// MISSING is a different answer, and the healing path CHRN-20 built for it must
// keep working.
func TestAudioPrunedForDistinguishesPrunedFromMissing(t *testing.T) {
	s, ctx := newTestStore(t)
	m := control(t, s, ctx, "redeliver@example.test")

	pruned, err := s.AudioPrunedFor(ctx, m.AuthorID, m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if pruned {
		t.Fatal("a memo whose audio is on disk read as pruned; the upload path would refuse " +
			"to heal a file that is merely missing")
	}

	if _, err := s.MarkAudioPruned(ctx, m.ID, testWindow); err != nil {
		t.Fatal(err)
	}
	pruned, err = s.AudioPrunedFor(ctx, m.AuthorID, m.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if !pruned {
		t.Fatal("a pruned memo did not report itself; the audio would be re-delivered and " +
			"deleted again at the next sweep")
	}

	// And a memo nobody has ever had is not pruned either.
	absent, err := s.AudioPrunedFor(ctx, uuid.New(), hashOf("never-seen"))
	if err != nil {
		t.Fatal(err)
	}
	if absent {
		t.Fatal("an unknown memo read as pruned")
	}
}

// HELD BACK IS A NUMBER SOMEBODY CAN SEE. A memo discarded before it was
// transcribed can never satisfy the gate — the pump only reads `captured` and
// `queued` — so its audio stays indefinitely. That is the safe direction and
// the rule gets no exception, but it must not be a silence.
func TestHeldBackCountsWhatTheGateIsKeeping(t *testing.T) {
	s, ctx := newTestStore(t)
	control(t, s, ctx, "held-control@example.test")
	newTranscribableMemo(t, s, ctx, "held-1@example.test")
	newTranscribableMemo(t, s, ctx, "held-2@example.test")

	n, err := s.HeldBackFromPruning(ctx, testWindow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("held back %d, want 2 — the count is the visible half of an accepted gap", n)
	}
}
