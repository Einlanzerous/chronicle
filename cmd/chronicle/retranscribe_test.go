package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/transcribe"
)

// `chronicle retranscribe` is the "retry" half of CHRN-27's Done-when, and it
// is the one part of that sentence a test of the pump cannot reach. It had no
// test until a review pointed out that it was also where the retry silently
// stopped working.

func retranscribeStore(t *testing.T) (*store.Store, context.Context) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// runRetranscribe loads config from the environment, as every subcommand
	// does. Pointed at the same database the assertions read.
	t.Setenv("CHRONICLE_DATABASE_URL", dsn)
	return store.New(pool), ctx
}

// heldMemoAtTheCeiling builds the exact situation the ceiling produces: a memo
// in `held` with MaxAttempts settled attempts behind it.
func heldMemoAtTheCeiling(t *testing.T, s *store.Store, ctx context.Context, email string) uuid.UUID {
	t.Helper()
	user, err := s.CreateUser(ctx, email, "Author", store.KindPerson)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(email))
	res, err := s.IngestMemo(ctx, store.Arrival{
		AuthorID: user.ID, ContentHash: hex.EncodeToString(sum[:]), ByteSize: 512,
		Source: store.SourceUpload, SourceRef: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < transcribe.MaxAttempts; i++ {
		job, err := s.BeginTranscription(ctx, res.Memo.ID, "small.en", res.Memo.ContentHash)
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if err := s.RecordJobFailure(ctx, job.ID, "transcription_failed", "no"); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.AdvanceMemoState(ctx, res.Memo.ID, store.StateCaptured, store.StateQueued, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdvanceMemoState(ctx, res.Memo.ID, store.StateQueued, store.StateHeld,
		"transcription failed 5 times; held for review (retry policy is CHRN-28)"); err != nil {
		t.Fatal(err)
	}
	return res.Memo.ID
}

// RELEASING A MEMO HAS TO ACTUALLY RELEASE IT.
//
// Without superseding the spent attempts, this command walks `held → queued`,
// prints "released", and the next sweep counts the same attempt rows, hits the
// ceiling, and holds the memo straight back — leaving it untranscribable by any
// path the service or the CLI offers, while the command reports success.
func TestRetranscribeClearsTheAttemptsThatHeldTheMemo(t *testing.T) {
	s, ctx := retranscribeStore(t)
	memoID := heldMemoAtTheCeiling(t, s, ctx, "ceiling@example.test")

	before, err := s.CountMemoJobs(ctx, memoID)
	if err != nil {
		t.Fatal(err)
	}
	if before != transcribe.MaxAttempts {
		t.Fatalf("countable attempts = %d, want %d", before, transcribe.MaxAttempts)
	}

	if err := runRetranscribe([]string{"--memo", memoID.String()}); err != nil {
		t.Fatalf("retranscribe: %v", err)
	}

	m, err := s.GetMemo(ctx, memoID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != store.StateQueued {
		t.Fatalf("state %q, want queued", m.State)
	}
	if m.StateReason != nil {
		t.Fatalf("state_reason = %v; a released memo must not keep explaining a hold that is over", *m.StateReason)
	}

	after, err := s.CountMemoJobs(ctx, memoID)
	if err != nil {
		t.Fatal(err)
	}
	if after != 0 {
		t.Fatalf("countable attempts = %d after a release; the pump's ceiling will hold this "+
			"memo again on its next sweep and the command will have achieved nothing", after)
	}

	// And the record of what was tried is KEPT, not deleted. The rows are the
	// evidence; losing them makes the second attempt at diagnosis start from
	// nothing.
	var kept int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tier1.memo_jobs WHERE memo_id = $1`, memoID).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != transcribe.MaxAttempts {
		t.Fatalf("%d attempt rows survived; want all %d kept as evidence", kept, transcribe.MaxAttempts)
	}
}

// With no --memo it releases every held memo, and says how many.
func TestRetranscribeWithNoMemoReleasesEveryHeldMemo(t *testing.T) {
	s, ctx := retranscribeStore(t)
	a := heldMemoAtTheCeiling(t, s, ctx, "all-a@example.test")
	b := heldMemoAtTheCeiling(t, s, ctx, "all-b@example.test")

	if err := runRetranscribe(nil); err != nil {
		t.Fatalf("retranscribe: %v", err)
	}
	for _, id := range []uuid.UUID{a, b} {
		m, err := s.GetMemo(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if m.State != store.StateQueued {
			t.Fatalf("memo %s is in state %q, want queued", id, m.State)
		}
	}
}

// A memo that is not held is NAMED rather than shrugged at. Releasing one that
// is already transcribing would be a second attempt on the same recording.
func TestRetranscribeRefusesAMemoThatIsNotHeld(t *testing.T) {
	s, ctx := retranscribeStore(t)
	user, err := s.CreateUser(ctx, "nothold@example.test", "Author", store.KindPerson)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("nothold"))
	res, err := s.IngestMemo(ctx, store.Arrival{
		AuthorID: user.ID, ContentHash: hex.EncodeToString(sum[:]), ByteSize: 512,
		Source: store.SourceUpload, SourceRef: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	err = runRetranscribe([]string{"--memo", res.Memo.ID.String()})
	if err == nil {
		t.Fatal("a captured memo was released as though it were held")
	}
	if !strings.Contains(err.Error(), "not held") {
		t.Fatalf("error = %v; it should say what state the memo is actually in", err)
	}
}

// An unknown memo is an error naming the id, not a silent success.
func TestRetranscribeRefusesAnUnknownMemo(t *testing.T) {
	_, _ = retranscribeStore(t)
	id := uuid.New()
	err := runRetranscribe([]string{"--memo", id.String()})
	if err == nil || !strings.Contains(err.Error(), id.String()) {
		t.Fatalf("error = %v; it should name the memo that does not exist", err)
	}
}

// A MEMO WHOSE AUDIO IS GONE IS NOT RELEASED.
//
// Nothing sets audio_pruned_at until CHRN-22, so this cannot fire today. The day
// it can, releasing one would put it in `queued` — where
// MemosAwaitingTranscription excludes pruned memos, so no sweep returns it, no
// report lists it as stuck, and it counts in `pending` forever. A silent
// permanent limbo is worse than a refusal that says why.
func TestRetranscribeRefusesAMemoWhoseAudioIsGone(t *testing.T) {
	s, ctx := retranscribeStore(t)
	memoID := heldMemoAtTheCeiling(t, s, ctx, "pruned@example.test")

	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.memos SET audio_pruned_at = now() WHERE id = $1`, memoID); err != nil {
		t.Fatal(err)
	}

	if err := runRetranscribe([]string{"--memo", memoID.String()}); err != nil {
		t.Fatalf("retranscribe: %v", err)
	}

	m, err := s.GetMemo(ctx, memoID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != store.StateHeld {
		t.Fatalf("state %q; a memo with no audio was released into a queue that will never "+
			"return it, and nothing would report it as stuck", m.State)
	}
	// And the attempts were not cleared either — nothing about it changed.
	n, err := s.CountMemoJobs(ctx, memoID)
	if err != nil {
		t.Fatal(err)
	}
	if n != transcribe.MaxAttempts {
		t.Fatalf("countable attempts = %d; a refused release must not spend them", n)
	}
}
