package store

import (
	"errors"
	"testing"
	"time"
)

// The tier-1 seen-ledger (CHRN-19). The watcher's own logic is unit-tested in
// internal/watch against a fake; what needs Postgres is that these queries
// agree with 0004 and that the identity comparison survives a round trip.

func TestSeenLedgerRoundTrips(t *testing.T) {
	s, ctx := newTestStore(t)

	// Deliberately carries sub-microsecond precision: a filesystem hands out
	// nanoseconds and TIMESTAMPTZ stores microseconds, and if Matches did not
	// account for that every file would look new on every scan, forever.
	mtime := time.Now().Add(-time.Hour).Truncate(time.Nanosecond).Add(437 * time.Nanosecond)
	f := SeenFile{
		Path:        "/data/chronicle/inbox/acct/note.opus",
		SizeBytes:   4096,
		ModTime:     mtime,
		ContentHash: hashOf("a recording"),
	}
	if err := s.MarkSeen(ctx, f); err != nil {
		t.Fatalf("MarkSeen: %v", err)
	}

	got, err := s.GetSeen(ctx, f.Path)
	if err != nil {
		t.Fatalf("GetSeen: %v", err)
	}
	if got.SizeBytes != f.SizeBytes || got.ContentHash != f.ContentHash {
		t.Errorf("got %+v, want %+v", got, f)
	}

	ix, err := s.LoadSeen(ctx)
	if err != nil {
		t.Fatalf("LoadSeen: %v", err)
	}
	if !ix.Matches(f.Path, f.SizeBytes, f.ModTime) {
		t.Errorf("Matches said no to the file it had just recorded (stored %v, asked %v)",
			ix[f.Path].ModTime, f.ModTime)
	}
	// Any of the three moving means read it again.
	if ix.Matches(f.Path, f.SizeBytes+1, f.ModTime) {
		t.Error("a changed size still matched")
	}
	if ix.Matches(f.Path, f.SizeBytes, f.ModTime.Add(time.Second)) {
		t.Error("a changed mtime still matched")
	}
	if ix.Matches("/data/chronicle/inbox/acct/other.opus", f.SizeBytes, f.ModTime) {
		t.Error("an unknown path matched")
	}
}

// A file rewritten in place is still the same path. first_seen_at is when that
// path first delivered something and must not move; last_seen_at must.
func TestMarkSeenKeepsFirstSeenAndMovesLastSeen(t *testing.T) {
	s, ctx := newTestStore(t)
	path := "/data/chronicle/inbox/acct/rewritten.opus"

	first := SeenFile{Path: path, SizeBytes: 10, ModTime: time.Now().Add(-2 * time.Hour), ContentHash: hashOf("v1")}
	if err := s.MarkSeen(ctx, first); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetSeen(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	second := SeenFile{Path: path, SizeBytes: 20, ModTime: time.Now().Add(-time.Hour), ContentHash: hashOf("v2")}
	if err := s.MarkSeen(ctx, second); err != nil {
		t.Fatal(err)
	}
	after, err := s.GetSeen(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	if !after.FirstSeenAt.Equal(before.FirstSeenAt) {
		t.Errorf("first_seen_at moved: %v then %v", before.FirstSeenAt, after.FirstSeenAt)
	}
	if !after.LastSeenAt.After(before.LastSeenAt) && !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Errorf("last_seen_at went backwards: %v then %v", before.LastSeenAt, after.LastSeenAt)
	}
	if after.ContentHash != hashOf("v2") || after.SizeBytes != 20 {
		t.Errorf("the rewrite was not recorded: %+v", after)
	}
}

// The recovery lever. Forgetting a path costs a re-hash and nothing else, which
// is what "tier 1 is disposable" has to mean concretely.
func TestForgetSeenMakesTheNextScanReadItAgain(t *testing.T) {
	s, ctx := newTestStore(t)
	path := "/data/chronicle/inbox/acct/note.opus"
	if err := s.MarkSeen(ctx, SeenFile{
		Path: path, SizeBytes: 1, ModTime: time.Now(), ContentHash: hashOf("x"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetSeen(ctx, path); err != nil {
		t.Fatalf("ForgetSeen: %v", err)
	}
	if _, err := s.GetSeen(ctx, path); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSeen after ForgetSeen = %v, want ErrNotFound", err)
	}
	// And forgetting something unknown is not an error — the lever should be
	// safe to pull twice.
	if err := s.ForgetSeen(ctx, path); err != nil {
		t.Errorf("ForgetSeen on an unknown path: %v", err)
	}
}

func TestLoadSeenOnAnEmptyLedger(t *testing.T) {
	s, ctx := newTestStore(t)
	ix, err := s.LoadSeen(ctx)
	if err != nil {
		t.Fatalf("LoadSeen: %v", err)
	}
	if len(ix) != 0 {
		t.Errorf("empty ledger loaded %d entries", len(ix))
	}
	if ix.Matches("/anything", 1, time.Now()) {
		t.Error("an empty ledger matched a path")
	}
}
