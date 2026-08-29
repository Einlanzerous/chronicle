package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// The sweeper, against a fake store. What is under test here is the ORDER of
// the two writes and what happens when the second fails — the store's own tests
// cover the predicate, and they are the ones that decide what may be deleted.

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

type fakeStore struct {
	mu       sync.Mutex
	rows     []store.PrunableMemo
	marked   map[uuid.UUID]bool
	refuse   map[uuid.UUID]bool // claims to lose, as a pin would
	heldBack int
	err      error
}

func (f *fakeStore) PrunableAudio(context.Context, time.Duration, int) ([]store.PrunableMemo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]store.PrunableMemo, 0, len(f.rows))
	for _, r := range f.rows {
		if !f.marked[r.MemoID] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeStore) MarkAudioPruned(_ context.Context, id uuid.UUID, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refuse[id] || f.marked[id] {
		return false, nil
	}
	f.marked[id] = true
	return true, nil
}

func (f *fakeStore) HeldBackFromPruning(context.Context, time.Duration) (int, error) {
	return f.heldBack, nil
}

// harness builds a pruner over a real audio root with real files on disk.
func harness(t *testing.T, n int) (*Pruner, *fakeStore, []string) {
	t.Helper()
	root := t.TempDir()
	as, err := audio.New(root)
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeStore{marked: map[uuid.UUID]bool{}, refuse: map[uuid.UUID]bool{}}

	var paths []string
	for i := 0; i < n; i++ {
		author := uuid.New()
		hash := hashOf(t, i)
		ref := audio.Ref{AuthorID: author, ContentHash: hash}
		p, err := as.Path(ref)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("pretend this is opus"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
		fs.rows = append(fs.rows, store.PrunableMemo{
			MemoID: uuid.New(), AuthorID: author, ContentHash: hash,
			ByteSize: 20, CapturedAt: time.Now().Add(-48 * time.Hour), Retention: "days_30",
		})
	}
	return &Pruner{Store: fs, Audio: as, Logger: quiet(), Window: time.Nanosecond}, fs, paths
}

func hashOf(t *testing.T, i int) string {
	t.Helper()
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for j := range out {
		out[j] = hex[(i+j)%16]
	}
	return string(out)
}

// A DRY RUN TOUCHES NOTHING, and lists exactly what a real run would delete —
// which is a property rather than a claim, because both read one predicate.
func TestADryRunDeletesNothingAndListsWhatARunWould(t *testing.T) {
	p, fs, paths := harness(t, 3)
	ctx := context.Background()

	dry, err := p.Sweep(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Considered) != 3 || dry.Pruned != 0 {
		t.Fatalf("dry run considered %d and pruned %d; want 3 and 0", len(dry.Considered), dry.Pruned)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("a dry run deleted %s", path)
		}
	}
	if len(fs.marked) != 0 {
		t.Fatal("a dry run marked rows")
	}

	real, err := p.Sweep(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if real.Pruned != len(dry.Considered) {
		t.Fatalf("the real run pruned %d where the dry run listed %d; they do not agree",
			real.Pruned, len(dry.Considered))
	}
	for _, path := range paths {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s survived a real run", path)
		}
	}
}

// THE MARK COMES FIRST, AND A FAILED UNLINK LEAVES AN ORPHAN. The other order
// leaves a memo claiming audio that is gone, which is indistinguishable from
// data loss; an orphan is a file the storage report can see and this job may
// retry.
func TestAFailedUnlinkLeavesAnOrphanAndKeepsTheMark(t *testing.T) {
	p, fs, paths := harness(t, 1)
	ctx := context.Background()

	// The file is not where the layout says it is — a directory in its place is
	// the simplest way to make os.Remove fail for a reason that is not
	// "already gone".
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(paths[0], "in-the-way"), 0o755); err != nil {
		t.Fatal(err)
	}

	rep, err := p.Sweep(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Lost != 1 || rep.Pruned != 0 {
		t.Fatalf("lost %d pruned %d; an unlink that failed is neither a prune nor an error",
			rep.Lost, rep.Pruned)
	}
	if !fs.marked[fs.rows[0].MemoID] {
		t.Fatal("the mark was rolled back after a failed unlink. A memo whose audio MAY or " +
			"may not exist is worse than an orphan: nothing can see the first, and the " +
			"storage report already sees the second")
	}
}

// A file that is ALREADY GONE is not a failure. The mark is what makes the
// memo pruned; an absent file is a sweep that was interrupted after the mark
// and is now finishing.
func TestAnAlreadyMissingFileCountsAsPruned(t *testing.T) {
	p, _, paths := harness(t, 1)
	if err := os.Remove(paths[0]); err != nil {
		t.Fatal(err)
	}

	rep, err := p.Sweep(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Pruned != 1 || rep.Lost != 0 {
		t.Fatalf("pruned %d lost %d; a retry of an interrupted sweep completes it", rep.Pruned, rep.Lost)
	}
}

// A CLAIM LOST TO A PIN IS A NO-OP, and the file stays. This is the shape the
// compare-and-swap exists for: somebody pinned the memo between the read and
// the mark.
func TestALostClaimLeavesTheFileAlone(t *testing.T) {
	p, fs, paths := harness(t, 2)
	fs.refuse[fs.rows[0].MemoID] = true

	rep, err := p.Sweep(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || rep.Pruned != 1 {
		t.Fatalf("skipped %d pruned %d; want one of each", rep.Skipped, rep.Pruned)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatal("the file of a memo whose claim was refused was deleted anyway")
	}
	if _, err := os.Stat(paths[1]); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the file of a memo whose claim was taken survived")
	}
}

// The held-back count reaches the report, because it is the visible half of an
// accepted gap rather than a silence.
func TestTheReportCarriesWhatTheGateIsHolding(t *testing.T) {
	p, fs, _ := harness(t, 1)
	fs.heldBack = 7

	rep, err := p.Sweep(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if rep.HeldBack != 7 {
		t.Fatalf("held back %d, want 7", rep.HeldBack)
	}
	if !contains(rep.String(), "7 held back") {
		t.Fatalf("the rendered report does not show it:\n%s", rep)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
