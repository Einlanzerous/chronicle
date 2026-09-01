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

	"github.com/Einlanzerous/chronicle/internal/scribe/eval"
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

	for i := 0; i < transcribe.DefaultMaxAttempts; i++ {
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
	if before != transcribe.DefaultMaxAttempts {
		t.Fatalf("countable attempts = %d, want %d", before, transcribe.DefaultMaxAttempts)
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
	if kept != transcribe.DefaultMaxAttempts {
		t.Fatalf("%d attempt rows survived; want all %d kept as evidence", kept, transcribe.DefaultMaxAttempts)
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
	if n != transcribe.DefaultMaxAttempts {
		t.Fatalf("countable attempts = %d; a refused release must not spend them", n)
	}
}

// --dry-run returns before the JSON block, so the pair used to write no file
// and say nothing — REVIEW.md §8's cautionary tale, in one flag. Refused rather
// than made to work: the resolution holds the transcripts, and those do not
// leave the corpus.
//
// Checked here rather than in the eval package because it is a property of the
// command's flag handling, and it must fire before anything reads config or
// opens a pool — which is what makes this test runnable with no environment.
func TestEvalRefusesJSONWithoutAScoredRun(t *testing.T) {
	err := runEval([]string{"--dry-run", "--json", "/should/not/be/written.json"})
	if err == nil {
		t.Fatal("--dry-run --json was accepted and does nothing")
	}
	if !strings.Contains(err.Error(), "produces no scores") {
		t.Fatalf("err = %v, want it to say why", err)
	}
}

// The unset-URL complaint must be REACHABLE, which is a statement about
// ordering as much as about wording: `config.Load()` errors on a missing
// CHRONICLE_DATABASE_URL, so a router built from the full config would make a
// synthetic run fail by naming the database and this message would never be
// printed. Running with no scribe variables and no DSN is the whole test.
func TestEvalNamesTheMissingOllamaURLRatherThanTheDatabase(t *testing.T) {
	for _, k := range []string{
		"CHRONICLE_SCRIBE_OLLAMA_URL", "CHRONICLE_SCRIBE_MODEL",
		"CHRONICLE_DATABASE_URL", "DATABASE_URL",
	} {
		t.Setenv(k, "")
	}
	err := runEval([]string{"--stratum", "synthetic", "--labels", "../../docs/eval/routing-v1.yaml"})
	if err == nil {
		t.Fatal("a synthetic run with no model configured was accepted")
	}
	if !strings.Contains(err.Error(), "CHRONICLE_SCRIBE_OLLAMA_URL") {
		t.Fatalf("err = %v, want it to name the scribe URL", err)
	}
	if strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("err = %v — a synthetic run reached for the database", err)
	}
}

// The ruling on CHRN-30's plan: the fixture catalogue is synthetic-only. Now
// that CHRN-31 exists, the real stratum needs it CONFIGURED — and with no live
// list the run is refused rather than quietly falling back to the fixture,
// which would grade a router nobody will run.
func TestEvalRefusesTheRealStratumWithoutTheLiveCatalogue(t *testing.T) {
	t.Setenv("CHRONICLE_SCRIBE_OLLAMA_URL", "http://127.0.0.1:11434")
	t.Setenv("CHRONICLE_SCRIBE_MODEL", "gemma4:31b")
	t.Setenv("CHRONICLE_SWITCHYARD_URL", "")
	t.Setenv("CHRONICLE_SWITCHYARD_TOKEN", "")

	_, err := newRouter(context.Background(), "../../docs/eval", eval.StratumReal)
	if err == nil || !strings.Contains(err.Error(), "CHRN-31") {
		t.Fatalf("err = %v, want it to name CHRN-31", err)
	}
}
