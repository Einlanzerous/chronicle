package audio

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// OnDisk is what one walk of the tree found.
type OnDisk struct {
	// Files is every recording this layout recognises, and its size in bytes.
	Files map[Ref]int64
	// Bytes is the total of Files.
	Bytes int64

	// Strays are paths under the root that this layout did not write, relative
	// to the root. They are counted and reported but NEVER treated as corpus
	// and never offered to the pruner: a file we cannot name is a file we do
	// not understand, and "delete what you do not understand" is the wrong
	// default for a directory holding the only copy of somebody's recording.
	Strays     []string
	StrayBytes int64

	// Staging is what StagingDir holds: uploads in flight (CHRN-20). Counted
	// separately from both corpus and strays, because it is neither — these
	// are files this service wrote and understands, which are not yet
	// recordings and may never become any.
	//
	// The distinction is not bookkeeping neatness. Left in Strays, a phone
	// mid-upload would read as "files under the root that we cannot name",
	// which is a warning; and a stalled upload would sit in that warning
	// indefinitely, teaching whoever reads this report to ignore the field
	// that exists to be alarming. Counted here it is just disk with a known
	// owner and a known sweep.
	Staging      int
	StagingBytes int64
}

// Scan walks the store and reports what is actually on disk.
//
// A missing root is not an error — it is an empty corpus, which is the true
// state of this service until CHRN-19 or CHRN-20 writes the first file. An
// unreadable root is an error, because that is a different fact wearing the
// same shape.
func (s *Store) Scan() (OnDisk, error) {
	d := OnDisk{Files: map[Ref]int64{}}

	if _, err := os.Stat(s.root); err != nil {
		if os.IsNotExist(err) {
			return d, nil
		}
		return d, fmt.Errorf("audio: reading root %s: %w", s.root, err)
	}

	staging := s.StagingRoot()
	err := filepath.WalkDir(s.root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Uploads in flight are measured, not walked. Their names are
			// session ids rather than hashes, so every one of them would
			// otherwise be reported as a stray for as long as it took to
			// upload — see OnDisk.Staging.
			if p == staging {
				n, bytes, err := measure(p)
				if err != nil {
					return err
				}
				d.Staging, d.StagingBytes = n, bytes
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.root, p)
		if err != nil {
			return err
		}
		ref, ok := refFromRel(rel)
		if !ok {
			d.Strays = append(d.Strays, rel)
			d.StrayBytes += info.Size()
			return nil
		}
		d.Files[ref] = info.Size()
		d.Bytes += info.Size()
		return nil
	})
	if err != nil {
		return d, fmt.Errorf("audio: walking %s: %w", s.root, err)
	}
	sort.Strings(d.Strays)
	return d, nil
}

// measure totals the files directly under dir. Flat rather than recursive
// because the staging layout is flat — one file per session id — and a
// recursive walk here would quietly accept a shape nothing writes.
func measure(dir string) (int, int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("audio: reading %s: %w", dir, err)
	}
	var n int
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if os.IsNotExist(err) {
				// Finalised out from under the walk. Ordinary: an upload
				// completing during a storage report is not a finding.
				continue
			}
			return 0, 0, fmt.Errorf("audio: reading %s: %w", dir, err)
		}
		n++
		total += info.Size()
	}
	return n, total, nil
}

// Expected is what the database says should be on disk: one entry per memo
// whose audio has not been pruned, carrying the byte_size recorded at ingest.
type Expected map[Ref]int64

// Mismatch is a file whose size on disk is not the size the memo row recorded.
type Mismatch struct {
	Ref      Ref
	OnDisk   int64
	Recorded int64
}

// Reconciliation is the difference between what the database expects and what
// the disk holds.
type Reconciliation struct {
	// Orphans are recordings on disk that no unpruned memo expects — either
	// the memo is gone, or CHRN-22 recorded a prune whose unlink did not
	// happen. Safe to delete, and the only list that is.
	Orphans     []Ref
	OrphanBytes int64

	// Missing is the direction that matters. A memo says its audio is present
	// and it is not on disk: if that memo has no durable transcript, the only
	// copy of what somebody said is gone and nothing has reported it. CHRN-22
	// is forbidden to prune an untranscribed memo for exactly this reason, and
	// this is the number that says whether the promise held.
	Missing      []Ref
	MissingBytes int64

	// Mismatched is a third state that is neither: the file exists but is not
	// the size that was ingested. A truncated write, or an edit in place.
	//
	// CHRN-21 MUST SETTLE THIS BEFORE IT NORMALISES ANYTHING. byte_size is
	// "the bytes exactly as they arrived" and 0003's trigger refuses to move
	// it (CH002), while the layout gives a memo exactly one path. So if
	// Chronicle ever rewrites the audio in place, every successfully
	// normalised memo becomes a permanent `mismatched` — the state meaning
	// "something corrupted your audio" would be the steady state, which is
	// the false alarm this three-way split exists to avoid. Either the
	// normalised size gets its own column, or normalised memos are excluded
	// from the size comparison, or the decode does not happen here at all.
	// They are not equivalent; the choice belongs to CHRN-21.
	Mismatched []Mismatch
}

// Reconcile compares a scan against the database's expectation. It is a pure
// function so the interesting cases can be tested without either a filesystem
// or Postgres.
func Reconcile(d OnDisk, want Expected) Reconciliation {
	var r Reconciliation

	for ref, size := range d.Files {
		recorded, expected := want[ref]
		switch {
		case !expected:
			r.Orphans = append(r.Orphans, ref)
			r.OrphanBytes += size
		case size != recorded:
			r.Mismatched = append(r.Mismatched, Mismatch{Ref: ref, OnDisk: size, Recorded: recorded})
		}
	}
	for ref, size := range want {
		if _, ok := d.Files[ref]; !ok {
			r.Missing = append(r.Missing, ref)
			r.MissingBytes += size
		}
	}

	sortRefs(r.Orphans)
	sortRefs(r.Missing)
	sort.Slice(r.Mismatched, func(i, j int) bool { return less(r.Mismatched[i].Ref, r.Mismatched[j].Ref) })
	return r
}

func sortRefs(refs []Ref) { sort.Slice(refs, func(i, j int) bool { return less(refs[i], refs[j]) }) }

func less(a, b Ref) bool {
	if a.AuthorID != b.AuthorID {
		return a.AuthorID.String() < b.AuthorID.String()
	}
	return a.ContentHash < b.ContentHash
}

// ProjectionWindow is the rolling window the corpus sizing was projected over,
// and the same 30 days CHRN-22 prunes on. Stated once here so the pruner and
// the accounting cannot disagree about what "30 days" means — two copies of a
// retention interval is how a label promises one date and a job does another.
const ProjectionWindow = 30 * 24 * time.Hour

// ProjectedWindowBytes is what the corpus was predicted to settle at over
// ProjectionWindow: 340 MB, from the sizing in CHRN-22 (4.1 GB across 812
// memos). It is reported beside the measured figure precisely so a wrong
// prediction shows up as a number rather than as a full disk.
const ProjectedWindowBytes int64 = 340 << 20

// Volume reports the filesystem holding the store. Known is false off Linux,
// where the figures are absent rather than guessed — an unqualified zero would
// read as a full disk.
type Volume struct {
	TotalBytes int64
	FreeBytes  int64
	Known      bool
}

// Volume measures the filesystem the root sits on.
func (s *Store) Volume() Volume {
	free, total, ok := freeBytes(s.root)
	if !ok {
		return Volume{}
	}
	return Volume{TotalBytes: int64(total), FreeBytes: int64(free), Known: true}
}
