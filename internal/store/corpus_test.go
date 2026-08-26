package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CHRN-23's accounting. The layout half is unit-tested in internal/audio; what
// needs Postgres is that these two queries agree with the schema and with each
// other. newTestStore resets to an empty database, so the numbers are absolute.

// prune records a prune the way CHRN-22 will, so the accounting is tested
// against the state that ticket produces rather than one invented here.
func prune(t *testing.T, s *Store, ctx context.Context, id uuid.UUID) {
	t.Helper()
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.memos SET audio_pruned_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("mark pruned: %v", err)
	}
}

// backdated inserts a memo captured in the past. It goes in directly because
// captured_at is immutable to an UPDATE (0003's trigger, CH002) — which is the
// property the window depends on, so the test must not work around it by
// disabling the thing it relies on.
func backdated(t *testing.T, s *Store, ctx context.Context, author uuid.UUID, hash string, size int64, age time.Duration) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := s.Pool().QueryRow(ctx, `
		INSERT INTO tier2.memos (author_id, content_hash, byte_size, captured_at, state)
		VALUES ($1, $2, $3, now() - $4::interval, 'captured')
		RETURNING id`, author, hash, size, age).Scan(&id)
	if err != nil {
		t.Fatalf("insert backdated memo: %v", err)
	}
	return id
}

func TestAudioInventoryIsTheDatabasesExpectationOfTheDisk(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "inventory@x")

	kept, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("kept"), ByteSize: 100,
		Source: SourceUpload, IdempotencyKey: "k-inventory-kept0",
	})
	if err != nil {
		t.Fatalf("ingest kept: %v", err)
	}
	gone, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("pruned"), ByteSize: 250,
		Source: SourceUpload, IdempotencyKey: "k-inventory-prune",
	})
	if err != nil {
		t.Fatalf("ingest pruned: %v", err)
	}

	inv, err := s.AudioInventory(ctx)
	if err != nil {
		t.Fatalf("AudioInventory: %v", err)
	}
	if len(inv) != 2 {
		t.Fatalf("inventory holds %d memos, want 2", len(inv))
	}

	// A pruned memo no longer expects a file, so a leftover on disk reads as
	// an orphan rather than as agreement. That is the whole contract between
	// this query and internal/audio.Reconcile.
	prune(t, s, ctx, gone.Memo.ID)

	inv, err = s.AudioInventory(ctx)
	if err != nil {
		t.Fatalf("AudioInventory: %v", err)
	}
	if len(inv) != 1 {
		t.Fatalf("inventory holds %d after a prune, want 1", len(inv))
	}
	if inv[0].ContentHash != kept.Memo.ContentHash {
		t.Errorf("inventory kept %s, want the unpruned memo", inv[0].ContentHash)
	}
	if inv[0].AuthorID != author {
		t.Errorf("author = %s, want %s — the path is derived from this", inv[0].AuthorID, author)
	}
	if inv[0].ByteSize != 100 {
		t.Errorf("byte size = %d, want the size ingest recorded (100)", inv[0].ByteSize)
	}
}

func TestCorpusStatsCountsWhatIsPresentAndWhatWasEverHeld(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "stats@x")

	a, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("stats-a"), ByteSize: 1000,
		Source: SourceUpload, IdempotencyKey: "k-stats-aaaaaaaa",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("stats-b"), ByteSize: 2000,
		Source: SourceUpload, IdempotencyKey: "k-stats-bbbbbbbb",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.CorpusStats(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CorpusStats: %v", err)
	}
	if got.Memos != 2 || got.AudioPresent != 2 || got.AudioPruned != 0 {
		t.Errorf("memos=%d present=%d pruned=%d, want 2/2/0", got.Memos, got.AudioPresent, got.AudioPruned)
	}
	if got.RecordedBytes != 3000 || got.EverBytes != 3000 || got.WindowBytes != 3000 {
		t.Errorf("recorded=%d ever=%d window=%d, want 3000 each",
			got.RecordedBytes, got.EverBytes, got.WindowBytes)
	}
	if got.OldestCapture == nil || got.NewestCapture == nil {
		t.Error("capture bounds are nil with two memos in the corpus")
	}

	// A prune moves bytes out of RecordedBytes and leaves EverBytes alone.
	// "What the disk should hold" and "what this corpus has carried" are
	// different questions, and the report answers both.
	prune(t, s, ctx, a.Memo.ID)

	after, err := s.CorpusStats(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CorpusStats: %v", err)
	}
	if after.RecordedBytes != 2000 {
		t.Errorf("recorded bytes = %d after pruning 1000, want 2000", after.RecordedBytes)
	}
	if after.EverBytes != 3000 {
		t.Errorf("ever bytes = %d after a prune, want it unchanged at 3000", after.EverBytes)
	}
	if after.AudioPresent != 1 || after.AudioPruned != 1 {
		t.Errorf("present=%d pruned=%d, want 1/1", after.AudioPresent, after.AudioPruned)
	}
	if after.Memos != 2 {
		t.Errorf("memos = %d, want 2 — pruning audio does not delete the memo", after.Memos)
	}
}

// The window runs from captured_at, the only clock CHRN-22 may prune on
// (decision §4). A memo captured before the window is outside it however
// recently it was re-delivered, and it is still part of the corpus.
func TestCorpusStatsWindowRunsFromCapturedAt(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "window@x")

	backdated(t, s, ctx, author, hashOf("window-old"), 4096, 60*24*time.Hour)
	if _, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("window-new"), ByteSize: 512,
		Source: SourceUpload, IdempotencyKey: "k-window-fresh00",
	}); err != nil {
		t.Fatal(err)
	}

	narrow, err := s.CorpusStats(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CorpusStats: %v", err)
	}
	if narrow.WindowMemos != 1 || narrow.WindowBytes != 512 {
		t.Errorf("30-day window = %d memos / %d bytes, want 1 / 512",
			narrow.WindowMemos, narrow.WindowBytes)
	}
	if narrow.RecordedBytes != 4608 {
		t.Errorf("recorded bytes = %d, want 4608 — age does not remove a memo from the corpus",
			narrow.RecordedBytes)
	}

	wide, err := s.CorpusStats(ctx, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("CorpusStats: %v", err)
	}
	if wide.WindowMemos != 2 || wide.WindowBytes != 4608 {
		t.Errorf("90-day window = %d memos / %d bytes, want 2 / 4608",
			wide.WindowMemos, wide.WindowBytes)
	}
}

func TestCorpusStatsOnAnEmptyCorpusIsZeroRatherThanAnError(t *testing.T) {
	s, ctx := newTestStore(t)

	got, err := s.CorpusStats(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CorpusStats: %v", err)
	}
	if got.Memos != 0 || got.RecordedBytes != 0 || got.EverBytes != 0 || got.WindowBytes != 0 {
		t.Errorf("empty corpus reported %+v", got)
	}
	// sum() over no rows is NULL, and a report that errors on a fresh install
	// is a report nobody trusts on the day it would be most reassuring.
	if got.OldestCapture != nil || got.NewestCapture != nil {
		t.Errorf("capture bounds = %v / %v on an empty corpus, want nil",
			got.OldestCapture, got.NewestCapture)
	}

	inv, err := s.AudioInventory(ctx)
	if err != nil {
		t.Fatalf("AudioInventory: %v", err)
	}
	if len(inv) != 0 {
		t.Errorf("inventory = %d on an empty corpus", len(inv))
	}
}
