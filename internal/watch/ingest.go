package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

type ingestOutcome struct {
	collapsed           bool
	changedWhileReading bool
	hash                string
}

// ingestFile copies one inbox file into the audio store and records the memo.
//
// # The hazard, stated exactly
//
// A file appearing in a directory is not a file that has finished uploading —
// and for THIS design the consequence is worse than a truncated recording. The
// memo's identity is the SHA-256 of its bytes, so a hash taken over a
// half-written file is a *different hash*, which means a partial read does not
// produce a broken memo: it produces a SECOND memo, and then the complete file
// produces a third. The dedup rule cannot save it, because as far as the rule
// is concerned those are three different recordings.
//
// # Three guards, and only the third is a guarantee
//
//  1. Names that say "in progress" are skipped (isPartial). Cheap, and only as
//     good as the writer's naming.
//  2. A settle window: the file must have been untouched for Settle before it
//     is read at all. Cheap, and defeated by any writer that stalls.
//  3. **The file is re-stated after the copy, through the same open handle, and
//     the result must match what it was before.** If size or mtime moved, the
//     bytes we just hashed were a snapshot of something still being written, so
//     the temp file is discarded and nothing is recorded — the ledger is not
//     updated, so the next scan simply tries again.
//
// Only the third can actually detect the failure rather than hope to avoid it.
// The first two exist to make it rare; the third makes it harmless.
//
// # The order of the three writes is deliberate
//
// File, then memo row, then ledger. Every crash window then fails in the
// recoverable direction:
//
//   - after the file, before the row → a file with no memo. That is an ORPHAN,
//     which CHRN-23's report detects and which the next scan resolves anyway
//     because the ledger never recorded it.
//   - after the row, before the ledger → a re-read next scan, which collapses on
//     the content hash and writes no second arrival. Costs one re-hash.
//
// The reverse order would leave a memo row whose audio is not there — CHRN-23's
// `missing`, the one state that means something irreplaceable is gone.
//
// **So the ledger is a performance mechanism, not a correctness one.** Delete it
// and nothing is duplicated; the next scan re-hashes and every arrival collapses.
// That is what makes it honestly tier 1.
func (w *Watcher) ingestFile(ctx context.Context, path string, authorID uuid.UUID, walked os.FileInfo) (ingestOutcome, error) {
	var out ingestOutcome

	src, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Vanished between the walk and the open. Ordinary with a sync
			// client underneath; nothing to record.
			out.changedWhileReading = true
			return out, nil
		}
		return out, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = src.Close() }()

	// Stat through the OPEN HANDLE from here on. A path can be replaced
	// underneath us; a file descriptor cannot, so before and after describe the
	// same inode and the comparison means what it says.
	before, err := src.Stat()
	if err != nil {
		return out, fmt.Errorf("stat %s: %w", path, err)
	}
	if changed(walked, before) {
		out.changedWhileReading = true
		return out, nil
	}

	tmp, err := os.CreateTemp(w.audio.Root(), ".ingest-*")
	if err != nil {
		return out, fmt.Errorf("create temp in %s: %w", w.audio.Root(), err)
	}
	tmpName := tmp.Name()
	// Removed unless the rename below claims it. Harmless after a successful
	// rename: the name no longer exists.
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	sum := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, sum), src)
	if err != nil {
		return out, fmt.Errorf("copy %s: %w", path, err)
	}

	after, err := src.Stat()
	if err != nil {
		return out, fmt.Errorf("re-stat %s: %w", path, err)
	}
	if changed(before, after) || written != before.Size() {
		out.changedWhileReading = true
		return out, nil
	}
	if written <= 0 {
		out.changedWhileReading = true
		return out, nil
	}

	out.hash = hex.EncodeToString(sum.Sum(nil))
	ref := audio.Ref{AuthorID: authorID, ContentHash: out.hash}
	final, err := w.audio.Path(ref)
	if err != nil {
		return out, err
	}

	// fsync before the rename. Without it the rename can be durable while the
	// bytes are not, and a power cut leaves a correctly named file with a
	// hole in it — which is content-addressed storage lying about its content.
	if err := tmp.Sync(); err != nil {
		return out, fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return out, fmt.Errorf("close temp: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return out, fmt.Errorf("create %s: %w", filepath.Dir(final), err)
	}
	// Atomic within the filesystem, and the temp file was created inside the
	// audio root precisely so that holds. There is never a partially written
	// file at the destination path, which is the property CHRN-22 and the
	// player both depend on.
	if err := os.Rename(tmpName, final); err != nil {
		return out, fmt.Errorf("rename into %s: %w", final, err)
	}

	res, err := w.ingest.IngestMemo(ctx, store.Arrival{
		AuthorID:    authorID,
		ContentHash: out.hash,
		ByteSize:    written,
		Source:      store.SourceCopyparty,
		// The watched path, relative to the inbox root: stable across a move of
		// the root itself, and required non-null for this source by
		// memo_arrivals_watched_path. It is this arrival's only handle — the
		// watcher has no idempotency key to mint — and it is inside
		// memo_arrivals_sighting, which is what makes a repeat sighting write
		// no row.
		SourceRef:        w.sourceRef(path),
		OriginalFilename: filepath.Base(path),
		// Retention is deliberately empty. CHRN-18's ratchet: a default
		// arriving from a rescan must never outrank a choice a person made.
	})
	if err != nil {
		return out, fmt.Errorf("ingest %s: %w", path, err)
	}
	out.collapsed = res.Collapsed

	// §10's line. The signal is Collapsed and NEVER Deliveries > 1: a repeated
	// sighting deliberately writes no arrival row, so the count stays at 1
	// through eight re-scans and the inference would report false for exactly
	// the case this line exists to make visible.
	//
	// No filename, no transcript, nothing authored — REVIEW.md §8.
	if res.Collapsed {
		w.logger.Info("duplicate arrival",
			"memo_id", res.Memo.ID,
			"source", store.SourceCopyparty,
			"arrival_count", res.Deliveries)
	} else {
		w.logger.Info("memo captured",
			"memo_id", res.Memo.ID,
			"source", store.SourceCopyparty,
			"bytes", written)
	}

	if err := w.ledger.MarkSeen(ctx, store.SeenFile{
		Path:        path,
		SizeBytes:   before.Size(),
		ModTime:     before.ModTime(),
		ContentHash: out.hash,
	}); err != nil {
		// Not fatal, and deliberately so: the memo is already recorded, and a
		// missed ledger write costs one re-hash next scan, which then collapses.
		w.logger.Warn("could not record the file in the seen-ledger; it will be re-read",
			"path", path, "error", err)
	}
	return out, nil
}

// sourceRef is the arrival's handle: the path relative to the inbox root, so
// relocating the root does not turn every known file into a new sighting.
func (w *Watcher) sourceRef(path string) string {
	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		return path
	}
	return rel
}

// changed reports whether two stats of the same file disagree about its
// content. Size or mtime moving means it was written between them.
func changed(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return true
	}
	return a.Size() != b.Size() || !a.ModTime().Equal(b.ModTime())
}
