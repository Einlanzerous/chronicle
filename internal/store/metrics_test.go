package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CHRN-69's four numbers, against a real database. The case that matters most
// is the prune one: the check for an incorrect deletion is only worth having if
// it fires on an incorrect deletion, so these tests CREATE one -- by writing
// audio_pruned_at directly, the way a bug in the pruner would -- and require it
// to be seen.

func TestQueueMetricsCountsDepthAndAgeAndKeepsHeldApart(t *testing.T) {
	s, ctx := newTestStore(t)

	q, err := s.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if q.Depth != 0 || q.OldestCapturedAt != nil {
		t.Fatalf("an empty corpus reported depth %d oldest %v", q.Depth, q.OldestCapturedAt)
	}
	for _, st := range []string{StateCaptured, StateQueued, StateTranscribing} {
		if _, ok := q.ByState[st]; !ok {
			t.Errorf("state %q missing from an empty report; a zero must be stated, not absent", st)
		}
	}

	old := newTranscribableMemo(t, s, ctx, "q-old@example.test")
	newTranscribableMemo(t, s, ctx, "q-new@example.test")
	held := newTranscribableMemo(t, s, ctx, "q-held@example.test")
	if _, err := s.AdvanceMemoState(ctx, held.ID, StateCaptured, StateHeld, "decode failed"); err != nil {
		t.Fatal(err)
	}
	// captured_at is immutable by trigger (CH002); the test sidesteps it the
	// one way a test may, by disabling the trigger for the statement.
	backdate(t, s, ctx, old.ID, 7*time.Hour)

	q, err = s.QueueMetrics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if q.Depth != 2 {
		t.Errorf("depth = %d, want 2: a held memo is stopped, not waiting", q.Depth)
	}
	if q.Held != 1 {
		t.Errorf("held = %d, want 1", q.Held)
	}
	if q.OldestCapturedAt == nil || time.Since(*q.OldestCapturedAt) < 6*time.Hour+50*time.Minute {
		t.Errorf("oldest = %v, want the 7h-old memo", q.OldestCapturedAt)
	}
}

// backdate moves a memo's captured_at into the past.
func backdate(t *testing.T, s *Store, ctx context.Context, id uuid.UUID, by time.Duration) {
	t.Helper()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `ALTER TABLE tier2.memos DISABLE TRIGGER memos_guard`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE tier2.memos SET captured_at = captured_at - make_interval(secs => $2) WHERE id = $1`,
		id, by.Seconds()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ALTER TABLE tier2.memos ENABLE TRIGGER memos_guard`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestASRLatencyIsLengthWeightedAndSplitByModel(t *testing.T) {
	s, ctx := newTestStore(t)

	// One 60 s memo collected 2 s after submit, one 120 s memo after 4 s: 180 s
	// of audio in 6 s of wall clock is 30x, whatever the per-job ratios say.
	for i, c := range []struct {
		email  string
		audio  int64
		wall   float64
		model  string
		jobMod string
	}{
		{"asr-a@example.test", 60_000, 2, "whisper.cpp/small.en", "small.en"},
		{"asr-b@example.test", 120_000, 4, "whisper.cpp/small.en", "small.en"},
		{"asr-c@example.test", 30_000, 3, "whisper.cpp/base.en", "base.en"},
	} {
		m := newTranscribableMemo(t, s, ctx, c.email)
		dur := c.audio
		if _, err := s.RecordTranscript(ctx, TranscriptInput{
			MemoID: m.ID, Text: "x", Model: c.model, Backend: "vulkan", AudioDurationMS: &dur,
		}); err != nil {
			t.Fatal(err)
		}
		j, err := s.BeginTranscription(ctx, m.ID, c.jobMod, strings.Repeat("a", 63)+string(rune('a'+i)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.pool.Exec(ctx, `
			UPDATE tier1.memo_jobs
			   SET submitted_at = now() - make_interval(secs => $2),
			       collected_at = now()
			 WHERE id = $1`, j.ID, c.wall); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ASRLatencyByModel(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]ASRLatency{}
	for _, l := range got {
		by[l.Model] = l
	}
	small := by["small.en"]
	if small.Jobs != 2 {
		t.Fatalf("small.en jobs = %d, want 2", small.Jobs)
	}
	if r := small.Realtime(); r < 29.5 || r > 30.5 {
		t.Errorf("small.en realtime = %.2f, want 30 (180 s of audio in 6 s)", r)
	}
	if small.P95Seconds < small.P50Seconds {
		t.Errorf("p95 %.2f below p50 %.2f", small.P95Seconds, small.P50Seconds)
	}
	if by["base.en"].Jobs != 1 {
		t.Errorf("base.en jobs = %d, want 1: models are reported apart", by["base.en"].Jobs)
	}

	// Outside the window is outside.
	got, err = s.ASRLatencyByModel(ctx, time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a nanosecond window returned %d models, want 0", len(got))
	}
}

func TestRoutingAgreementSeparatesAgreedCorrectedAndUnaided(t *testing.T) {
	s, ctx := newTestStore(t)

	// agreed: the Scribe said TICKET, the person sent a TICKET.
	agreed := seedTriageable(t, s, ctx, strings.Repeat("a", 64))
	trID := transcriptID(t, s, ctx, agreed)
	if _, err := derived(s).SaveProposal(ctx, agreed, trID, testProposer, validOutcome(0.9)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimMemoLink(ctx, aDecision(agreed, "agree-key-0000001")); err != nil {
		t.Fatal(err)
	}

	// corrected: the Scribe said TICKET, the person chose NOTE.
	corrected := seedTriageable(t, s, ctx, strings.Repeat("b", 64))
	if _, err := derived(s).SaveProposal(ctx, corrected, transcriptID(t, s, ctx, corrected), testProposer, validOutcome(0.9)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimMemoLink(ctx, aDecision(corrected, "correct-key-000001")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE tier2.memo_links
	    SET destination = 'NOTE', ticket_key = NULL WHERE memo_id = $1`, corrected); err != nil {
		t.Fatal(err)
	}

	// unaided: a decision with no proposal at all.
	unaided := seedTriageable(t, s, ctx, strings.Repeat("c", 64))
	if _, _, err := s.ClaimMemoLink(ctx, aDecision(unaided, "unaided-key-000001")); err != nil {
		t.Fatal(err)
	}

	r, err := s.RoutingAgreement(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if r.Decided != 3 || r.Agreed != 1 || r.Corrected != 1 || r.Unaided != 1 {
		t.Fatalf("got %+v, want 3 decided: 1 agreed, 1 corrected, 1 unaided", r)
	}
	if r.Ratio() != 0.5 {
		t.Errorf("ratio = %v, want 0.5: the unaided decision must not move it", r.Ratio())
	}

	if r, err = s.RoutingAgreement(ctx, time.Nanosecond); err != nil || r.Decided != 0 {
		t.Errorf("a nanosecond window: %+v err=%v, want nothing decided", r, err)
	}
}

func transcriptID(t *testing.T, s *Store, ctx context.Context, memoID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx,
		`SELECT id FROM tier2.transcripts WHERE memo_id = $1 LIMIT 1`, memoID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A CLEAN LEDGER REPORTS ZERO VIOLATIONS, AND EACH WAY OF DELETING WRONGLY
// REPORTS ITSELF. The control is a prune the rules allow; the three variants
// each differ from it in exactly one respect.
func TestPruneMetricsSeesAnIncorrectDeletion(t *testing.T) {
	s, ctx := newTestStore(t)
	const window = 30 * 24 * time.Hour

	mark := func(id uuid.UUID) {
		t.Helper()
		if _, err := s.pool.Exec(ctx,
			`UPDATE tier2.memos SET audio_pruned_at = now() WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}

	// The control: past the window, durable transcript, days_30.
	ok := newTranscribableMemo(t, s, ctx, "pm-ok@example.test")
	durable(t, s, ctx, ok.ID, "whisper.cpp/small.en")
	backdate(t, s, ctx, ok.ID, 31*24*time.Hour)
	mark(ok.ID)

	p, err := s.PruneMetrics(ctx, window)
	if err != nil {
		t.Fatal(err)
	}
	if p.Total != 1 || p.Last24h != 1 || p.Last24hBytes != 1024 || p.TotalBytes != 1024 {
		t.Errorf("volume = %+v, want 1 memo and 1024 bytes", p)
	}
	if p.Violated() || len(p.ViolatingMemos) != 0 {
		t.Fatalf("a correct prune was reported as a violation: %+v", p)
	}
	if p.LastPrunedAt == nil {
		t.Error("LastPrunedAt is nil after a prune")
	}

	// Variant 1: never transcribed. THE WORST CASE.
	none := newTranscribableMemo(t, s, ctx, "pm-none@example.test")
	backdate(t, s, ctx, none.ID, 31*24*time.Hour)
	mark(none.ID)

	// Variant 2: a transcript below the model floor is not a durable one.
	base := newTranscribableMemo(t, s, ctx, "pm-base@example.test")
	durable(t, s, ctx, base.ID, "whisper.cpp/base.en")
	backdate(t, s, ctx, base.ID, 31*24*time.Hour)
	mark(base.ID)

	p, err = s.PruneMetrics(ctx, window)
	if err != nil {
		t.Fatal(err)
	}
	if p.WithoutTranscript != 2 {
		t.Errorf("without transcript = %d, want 2 (never transcribed + below the floor)", p.WithoutTranscript)
	}
	if !p.Violated() {
		t.Fatal("two incorrect deletions were not reported as a violation")
	}
	named := map[uuid.UUID]bool{}
	for _, id := range p.ViolatingMemos {
		named[id] = true
	}
	for name, id := range map[string]uuid.UUID{
		"never transcribed": none.ID, "below the floor": base.ID,
	} {
		if !named[id] {
			t.Errorf("the %s memo is not among the named violations", name)
		}
	}
	if named[ok.ID] {
		t.Error("the correct prune is named as a violation")
	}
}

// A CORRECT PRUNE STAYS CORRECT WHATEVER HAPPENS TO THE ROW AFTERWARDS.
//
// The review of this ticket found that IngestMemo's ratchet can raise
// retention on an already-pruned memo (a re-delivery carrying `forever`), and
// that a check reading retention NOW would then report a correct prune as a
// violation permanently. The metric therefore rests on the transcript alone,
// and this pins that: a DISCARD NOW prune inside the window, then a retention
// raise of the kind the ratchet allows, must both read clean.
func TestPruneMetricsIgnoresRetentionAfterThePrune(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "pm-discard@example.test")
	durable(t, s, ctx, m.ID, "whisper.cpp/small.en")
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier2.memos SET retention = 'discard_now', audio_pruned_at = now() WHERE id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	for _, retention := range []string{"discard_now", "forever"} {
		if _, err := s.pool.Exec(ctx,
			`UPDATE tier2.memos SET retention = $2 WHERE id = $1`, m.ID, retention); err != nil {
			t.Fatal(err)
		}
		p, err := s.PruneMetrics(ctx, 30*24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if p.Total != 1 || p.Violated() {
			t.Fatalf("with retention %q after the prune: %+v", retention, p)
		}
	}
}
