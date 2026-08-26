package audio

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func write(t *testing.T, s *Store, ref Ref, n int) {
	t.Helper()
	p, err := s.Path(ref)
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An empty corpus is the true state of this service until CHRN-19 or CHRN-20
// writes the first file, and a root that does not exist yet must read as that
// rather than as an error.
func TestScanOfAMissingRootIsAnEmptyCorpus(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(d.Files) != 0 || d.Bytes != 0 || len(d.Strays) != 0 {
		t.Errorf("empty root scanned as %+v", d)
	}
}

func TestScanCountsRecordingsAndSetsStraysAside(t *testing.T) {
	s := newTestStore(t)
	author := uuid.New()
	write(t, s, Ref{AuthorID: author, ContentHash: hashA}, 100)
	write(t, s, Ref{AuthorID: author, ContentHash: hashB}, 250)

	// Two things this layout did not write, in two shapes: a loose file at the
	// root, and one sitting in the right place under a wrong name.
	if err := os.WriteFile(filepath.Join(s.Root(), "README"), make([]byte, 7), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Root(), author.String(), "ff", "scratch.tmp"), make([]byte, 3), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := s.Scan()
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(d.Files) != 2 || d.Bytes != 350 {
		t.Errorf("files = %d bytes = %d, want 2 and 350", len(d.Files), d.Bytes)
	}
	if len(d.Strays) != 2 || d.StrayBytes != 10 {
		t.Errorf("strays = %v (%d bytes), want 2 and 10", d.Strays, d.StrayBytes)
	}
	// Strays must never be counted as corpus, or a hand-dropped file inflates
	// the budget and then shows up as an orphan somebody could act on.
	for _, stray := range d.Strays {
		if _, ok := refFromRel(stray); ok {
			t.Errorf("stray %q was recognised as a recording", stray)
		}
	}
}

func TestReconcileNamesBothDirectionsAndTheThirdState(t *testing.T) {
	author := uuid.New()
	other := uuid.New()

	present := Ref{AuthorID: author, ContentHash: hashA}
	orphan := Ref{AuthorID: other, ContentHash: hashB}
	missing := Ref{AuthorID: author, ContentHash: hashB}
	truncated := Ref{AuthorID: other, ContentHash: hashA}

	d := OnDisk{Files: map[Ref]int64{
		present:   100,
		orphan:    64,
		truncated: 5,
	}, Bytes: 169}
	want := Expected{
		present:   100,
		missing:   400,
		truncated: 4096,
	}

	r := Reconcile(d, want)

	if len(r.Orphans) != 1 || r.Orphans[0] != orphan || r.OrphanBytes != 64 {
		t.Errorf("orphans = %+v (%d bytes), want just %+v at 64", r.Orphans, r.OrphanBytes, orphan)
	}
	if len(r.Missing) != 1 || r.Missing[0] != missing || r.MissingBytes != 400 {
		t.Errorf("missing = %+v (%d bytes), want just %+v at 400", r.Missing, r.MissingBytes, missing)
	}
	if len(r.Mismatched) != 1 || r.Mismatched[0].Ref != truncated {
		t.Fatalf("mismatched = %+v, want just %+v", r.Mismatched, truncated)
	}
	if r.Mismatched[0].OnDisk != 5 || r.Mismatched[0].Recorded != 4096 {
		t.Errorf("mismatch = %+v, want 5 on disk against 4096 recorded", r.Mismatched[0])
	}
	// A truncated file is not an orphan and not missing. Reporting it as
	// either would be the wrong prompt: one invites deletion, the other
	// invites panic.
	for _, ref := range append(append([]Ref{}, r.Orphans...), r.Missing...) {
		if ref == truncated {
			t.Errorf("the truncated file was also reported as orphaned or missing")
		}
	}
}

// The end-to-end shape CHRN-22 depends on: a memo whose audio was pruned no
// longer expects a file, so a leftover on disk reads as an orphan rather than
// as agreement.
func TestPrunedMemoLeavesItsFileAnOrphan(t *testing.T) {
	s := newTestStore(t)
	author := uuid.New()
	ref := Ref{AuthorID: author, ContentHash: hashA}
	write(t, s, ref, 100)

	d, err := s.Scan()
	if err != nil {
		t.Fatal(err)
	}
	// AudioInventory omits pruned memos, so Expected is empty for this one.
	r := Reconcile(d, Expected{})
	if len(r.Orphans) != 1 || r.Orphans[0] != ref {
		t.Fatalf("orphans = %+v, want just %+v", r.Orphans, ref)
	}
	if len(r.Missing) != 0 {
		t.Errorf("missing = %+v, want none", r.Missing)
	}
}

func TestVolumeReportsTheFilesystem(t *testing.T) {
	s := newTestStore(t)
	v := s.Volume()
	if !v.Known {
		t.Skip("volume figures unavailable on this platform")
	}
	if v.TotalBytes <= 0 || v.FreeBytes <= 0 || v.FreeBytes > v.TotalBytes {
		t.Errorf("Volume = %+v, want a plausible filesystem", v)
	}
}
