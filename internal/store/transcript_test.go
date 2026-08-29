package store

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// CHRN-27, and the predicate CHRN-25 §5 settled. The one query CHRN-22 gates
// audio deletion on lives here, so the four cases §5 enumerates are asserted
// here rather than left for the Mode C diff to argue about.

func newTranscribableMemo(t *testing.T, s *Store, ctx context.Context, email string) Memo {
	t.Helper()
	author := newAuthor(t, s, ctx, email)
	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID:    author,
		ContentHash: hashOf(email),
		ByteSize:    1024,
		Source:      SourceUpload,
		SourceRef:   "test",
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	return res.Memo
}

// THE FOUR CASES §5 ENUMERATES, and REVIEW.md §3 asks a reviewer to trace.
//
// Getting this wrong in the permissive direction deletes audio for a memo that
// was never transcribed, which CLAUDE.md calls the single worst thing this
// system can do.
func TestDurableTranscriptPredicate(t *testing.T) {
	s, ctx := newTestStore(t)

	t.Run("no transcript at all is not durable", func(t *testing.T) {
		m := newTranscribableMemo(t, s, ctx, "dur-none@example.test")
		durable, err := s.HasDurableTranscript(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if durable {
			t.Fatal("a memo with no transcript read as durable; its audio is the only copy of that thought")
		}
	})

	t.Run("a partial transcript is not durable", func(t *testing.T) {
		m := newTranscribableMemo(t, s, ctx, "dur-partial@example.test")
		if _, err := s.RecordTranscript(ctx, TranscriptInput{
			MemoID: m.ID, Text: "half of a th", Partial: true,
			Model: "whisper.cpp/small.en", Backend: "vulkan",
		}); err != nil {
			t.Fatal(err)
		}
		durable, err := s.HasDurableTranscript(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if durable {
			t.Fatal("a partial transcript satisfied the gate — CHRN-28 is explicit that it must never")
		}
	})

	t.Run("a complete transcript is durable", func(t *testing.T) {
		m := newTranscribableMemo(t, s, ctx, "dur-ok@example.test")
		if _, err := s.RecordTranscript(ctx, TranscriptInput{
			MemoID: m.ID, Text: "the whole thought", Partial: false,
			Model: "whisper.cpp/small.en", Backend: "vulkan",
		}); err != nil {
			t.Fatal(err)
		}
		durable, err := s.HasDurableTranscript(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !durable {
			t.Fatal("a complete transcript did not satisfy the gate")
		}
	})

	// THE ONE WORTH ARGUING, and the one an implementer will get wrong.
	//
	// A memo that is forty seconds of silence has a true and complete answer,
	// and the answer is "no speech". Treating that as not-durable keeps the
	// audio of exactly the recordings least worth keeping, forever, while the
	// UI's PRUNES label quietly becomes a lie for them.
	t.Run("empty text with a completed run IS durable", func(t *testing.T) {
		m := newTranscribableMemo(t, s, ctx, "dur-silence@example.test")
		if _, err := s.RecordTranscript(ctx, TranscriptInput{
			MemoID: m.ID, Text: "", Segments: []Segment{}, Partial: false,
			Model: "whisper.cpp/small.en", Backend: "vulkan",
		}); err != nil {
			t.Fatal(err)
		}
		durable, err := s.HasDurableTranscript(ctx, m.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !durable {
			t.Fatal("a silent memo with a completed run read as not-durable. That is the " +
				"safe direction and it is still wrong: it strands the audio of every such " +
				"memo forever, silently, where nobody checks")
		}
	})
}

// The gate is NOT `covered_ms >= audio_duration_ms`, and this is the fixture
// that would break such an implementation: a perfectly complete run over a
// recording that ends in silence.
func TestTheGateIsNotComputedFromCoverage(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "dur-coverage@example.test")

	duration := int64(60000)
	covered := int64(41500) // eighteen and a half seconds of trailing silence
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: m.ID, Text: "a complete thought", Partial: false,
		Model: "whisper.cpp/small.en", Backend: "vulkan",
		AudioDurationMS: &duration, CoveredMS: &covered,
	}); err != nil {
		t.Fatal(err)
	}

	durable, err := s.HasDurableTranscript(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !durable {
		t.Fatal("coverage short of duration defeated the gate. whisper emits segments only " +
			"where there is speech, so this is what MOST of the corpus looks like — a pruner " +
			"gated on coverage would mark it all not-durable and never fire")
	}
}

// A complete run may replace a partial one. Nothing may go the other way.
func TestPartialMayBeUpgradedButNeverDowngraded(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "upgrade@example.test")

	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: m.ID, Text: "half a th", Partial: true, Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatal(err)
	}

	upgraded, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: m.ID, Text: "half a thought and the rest", Partial: false,
		Model: "whisper.cpp/small.en", Backend: "vulkan",
	})
	if err != nil {
		t.Fatalf("upgrading a partial: %v", err)
	}
	if upgraded.Partial || upgraded.Text != "half a thought and the rest" {
		t.Fatalf("the complete run did not replace the partial one: %+v", upgraded)
	}

	// And back the other way is refused. Not avoided by convention — refused,
	// so CHRN-28's retry policy cannot downgrade a good transcript by
	// re-collecting a bad one.
	kept, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: m.ID, Text: "half a th", Partial: true, Model: "whisper.cpp/small.en", Backend: "vulkan",
	})
	if err != nil {
		t.Fatalf("re-collecting a partial should be a no-op, not an error: %v", err)
	}
	if kept.Partial || kept.Text != "half a thought and the rest" {
		t.Fatalf("a partial overwrote a complete transcript: %+v", kept)
	}

	// The database refuses it directly too, so this does not rest on the
	// upsert's WHERE clause being right forever.
	_, err = s.Pool().Exec(ctx,
		`UPDATE tier2.transcripts SET partial = true WHERE memo_id = $1`, m.ID)
	if err == nil {
		t.Fatal("the database allowed a complete transcript to be marked partial")
	}
}

// Collecting the same result twice is a no-op, which is what makes losing the
// TIER 1 bookkeeping harmless rather than duplicating authored content.
func TestRecollectingIsANoOp(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "recollect@example.test")

	in := TranscriptInput{
		MemoID: m.ID, Text: "said once", Partial: false,
		Segments: []Segment{{StartMS: 0, EndMS: 900, Text: "said once"}},
		Model:    "whisper.cpp/small.en", Backend: "vulkan",
	}
	for i := 0; i < 3; i++ {
		if _, err := s.RecordTranscript(ctx, in); err != nil {
			t.Fatalf("collection %d: %v", i, err)
		}
	}

	var n int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tier2.transcripts WHERE memo_id = $1`, m.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d transcript rows for one memo and one model; want 1", n)
	}
}

// A different model is a different answer to the same question, and both are
// worth keeping — which is also what makes the model column mean anything.
func TestASecondModelIsASecondTranscript(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "twomodels@example.test")

	for _, model := range []string{"small.en", "medium.en"} {
		if _, err := s.RecordTranscript(ctx, TranscriptInput{
			MemoID: m.ID, Text: "said by " + model, Partial: false,
			Model: model, Backend: "vulkan",
		}); err != nil {
			t.Fatal(err)
		}
	}

	var n int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tier2.transcripts WHERE memo_id = $1`, m.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("%d transcripts; want one per model", n)
	}
}

// A transcript for a memo that does not exist is refused. There is no state in
// which a transcript floats free of the recording it came from.
func TestTranscriptNeedsAMemo(t *testing.T) {
	s, ctx := newTestStore(t)
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: uuid.New(), Text: "orphan", Partial: false,
		Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

// Segments round-trip, and an empty list stays a list rather than becoming
// nil. Nothing downstream should ever meet a transcript it might read as
// absent.
func TestSegmentsRoundTrip(t *testing.T) {
	s, ctx := newTestStore(t)
	m := newTranscribableMemo(t, s, ctx, "segments@example.test")

	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: m.ID, Text: "", Segments: nil, Partial: false,
		Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTranscript(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Segments == nil {
		t.Fatal("an empty segment list came back nil")
	}
	if !got.Durable() {
		t.Fatal("Durable() disagreed with HasDurableTranscript on an empty complete run")
	}
}
