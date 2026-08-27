package upload

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// The sweep, and what it is emphatically not.
//
// This deletes ABANDONED PARTIAL UPLOADS. CHRN-22's pruner deletes the audio of
// finished memos. They are both "a background job that removes audio files" and
// they are not the same kind of thing at all, so the difference is worth
// stating where somebody reading one might reach for the other:
//
//   - A partial upload is regenerable. The phone still holds the recording —
//     that is the argument 0005 makes for the session table being tier 1 — so
//     deleting it costs a re-send.
//   - A finished memo's audio is NOT regenerable, which is why CHRN-22 is a
//     Mode C ticket, why its deletion is gated on a durable transcript, and why
//     CLAUDE.md calls getting it wrong the single worst thing this system can
//     do.
//
// Nothing here may ever be pointed at a path under an author's directory, and
// nothing here consults a memo row. It works on tier1.memo_uploads and on
// StagingDir, and those are the only two things it can name.

// SweepResult is what one pass removed.
type SweepResult struct {
	// Expired is sessions that made no progress inside the TTL.
	Expired int
	// Unclaimed is staging files with no session row at all: the residue of a
	// finalise or an abandon that removed the row and then failed, or of a
	// database restored behind a filesystem that was not.
	Unclaimed int
	Bytes     int64
}

// Run sweeps until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	s.logger.Info("upload sweeper started",
		"every", s.sweepInterval.String(), "ttl", s.ttl.String(),
		"staging", s.audio.StagingRoot())

	t := time.NewTicker(s.sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.logger.Info("upload sweeper stopped")
			return nil
		case <-t.C:
			res, err := s.Sweep(ctx)
			if err != nil {
				// Logged and retried on the next tick rather than returned. A
				// sweep that cannot run leaves disk behind, which is a cost;
				// stopping the loop would leave it behind forever.
				s.logger.Warn("upload sweep failed; will retry", "error", err)
				continue
			}
			if res.Expired > 0 || res.Unclaimed > 0 {
				s.logger.Info("swept abandoned uploads",
					"expired", res.Expired, "unclaimed", res.Unclaimed, "bytes", res.Bytes)
			}
		}
	}
}

// Sweep removes abandoned sessions and the staging files nothing can claim.
func (s *Service) Sweep(ctx context.Context) (SweepResult, error) {
	var res SweepResult

	stale, err := s.sessions.StaleUploads(ctx, s.now().Add(-s.ttl))
	if err != nil {
		return res, err
	}
	for _, u := range stale {
		// Under the session's own lock, so a chunk that arrives in the same
		// instant either finishes first (and touches the row, though this pass
		// has already decided) or waits. The worst case is one wasted chunk,
		// not a file deleted from under a write.
		unlock := s.lock(u.ID)
		size := s.stagingSize(u.ID)
		if err := s.removeStaging(u.ID); err != nil {
			unlock()
			return res, err
		}
		if err := s.sessions.DeleteUpload(ctx, u.ID); err != nil {
			unlock()
			return res, err
		}
		unlock()
		res.Expired++
		res.Bytes += size
		s.logger.Info("expired an abandoned upload",
			"upload_id", u.ID, "idle_for", s.now().Sub(u.LastActivityAt).Round(time.Second).String(),
			"had_bytes", size, "declared_bytes", u.ByteSize)
	}

	unclaimed, bytes, err := s.sweepUnclaimed(ctx)
	if err != nil {
		return res, err
	}
	res.Unclaimed = unclaimed
	res.Bytes += bytes
	return res, nil
}

// sweepUnclaimed removes staging files with no session row.
//
// **The order of the two reads is the correctness argument, and it is the
// reverse of the obvious one.** The directory is listed FIRST and the live ids
// read SECOND, so a session opened between them appears in the id set but not
// in the listing, and is therefore never a candidate. Done the other way round
// — ids first, listing second — a session created in the gap would be missing
// from the ids and present on disk, and the sweep would delete a file a request
// was in the middle of writing.
//
// unclaimedGrace is a second, independent guard on the same hazard: a file has
// to be untouched for an hour before it is even considered. Either one alone
// would do; both are here because the cost of the belt is nothing and the cost
// of the failure is a client's upload disappearing mid-transfer.
func (s *Service) sweepUnclaimed(ctx context.Context) (int, int64, error) {
	entries, err := os.ReadDir(s.audio.StagingRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("upload: read staging directory: %w", err)
	}

	live, err := s.sessions.LiveUploadIDs(ctx)
	if err != nil {
		return 0, 0, err
	}

	cutoff := s.now().Add(-unclaimedGrace)
	var n int
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		id, err := uuid.Parse(e.Name())
		if err != nil {
			// Not a name this service writes. Left alone and reported: the
			// staging directory is inside the audio root, and "delete what you
			// do not understand" is the wrong default anywhere under it.
			s.logger.Warn("unrecognised file in the upload staging directory; leaving it",
				"name", e.Name(), "staging", s.audio.StagingRoot())
			continue
		}
		if _, ok := live[id]; ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return n, total, fmt.Errorf("upload: stat staging file: %w", err)
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		unlock := s.lock(id)
		err = s.removeStaging(id)
		unlock()
		if err != nil {
			return n, total, err
		}
		n++
		total += info.Size()
		s.logger.Info("removed an upload staging file no session claims",
			"upload_id", id, "bytes", info.Size())
	}
	return n, total, nil
}

// stagingSize is the size of a session's bytes, or zero if there are none. Used
// for reporting only, so a stat failure is not worth failing a sweep over.
func (s *Service) stagingSize(id uuid.UUID) int64 {
	path, err := s.audio.StagingPath(id)
	if err != nil {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
