// Package retention deletes audio, and nothing else in this repository does.
//
// It is its own package for two reasons. `internal/audio` computes where a
// recording lives; the code that unlinks it should be nameable on its own line.
// And `REVIEW.md` §7 requires this ticket to add its package to
// `sensitive_paths`, so that every later PR touching the pruner is reviewed at
// the expensive tier rather than silently at the cheap one.
//
// The decision is docs/decisions/chrn-22-retention-pruner.md. Two rules from it
// are worth repeating where the code is:
//
//   - NOTHING IS DELETED WITHOUT A DURABLE TRANSCRIPT. If transcription failed
//     or never ran, the audio is the only copy of that thought, and the
//     calendar is not a good enough reason to destroy it.
//   - THE TRANSCRIPT IS NEVER DELETED, at any age, under any setting. The
//     asymmetry is the design.
package retention

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// DefaultInterval is how often the sweep runs, and therefore what "immediately
// for DISCARD NOW" means. An hour: short enough that immediately is honest, and
// long enough that the query is free against a corpus projected to settle
// around 340 MB.
const DefaultInterval = time.Hour

// DefaultBatch bounds one sweep. A pruner that unlinked ten thousand files in
// one pass would hold nothing open that matters, but it would also produce one
// log line per file and no opportunity to notice it going wrong.
const DefaultBatch = 200

// Store is the slice of Chronicle's store this needs — written down here rather
// than inferred from call sites, and small on purpose: this package can read
// what may be deleted, claim one, and count what is being held back. It cannot
// advance a memo, write a transcript, or reach tier 1.
type Store interface {
	PrunableAudio(ctx context.Context, window time.Duration, limit int) ([]store.PrunableMemo, error)
	MarkAudioPruned(ctx context.Context, memoID uuid.UUID, window time.Duration) (bool, error)
	HeldBackFromPruning(ctx context.Context, window time.Duration) (int, error)
}

// Pruner deletes audio whose memo has a durable transcript and whose window has
// passed.
type Pruner struct {
	Store  Store
	Audio  *audio.Store
	Logger *slog.Logger

	// Window is how long audio is kept. It is audio.ProjectionWindow, declared
	// once, because the label a person reads and the deadline this job uses
	// must be the same number.
	Window time.Duration

	Interval time.Duration
	Batch    int
}

// Report is what one sweep did, and what it declined to do.
type Report struct {
	// DryRun means nothing was deleted and nothing was marked.
	DryRun bool

	// Considered is what the predicate matched.
	Considered []store.PrunableMemo

	// Pruned is what this sweep actually deleted. On a dry run it is empty and
	// Considered is the answer.
	Pruned int

	// Bytes freed, counted from the memo rows rather than from the filesystem:
	// byte_size is immutable, and the file is gone by the time anyone asks.
	Bytes int64

	// Lost is a claim that was won and whose file could not be unlinked. The
	// mark stands — the memo says pruned, the file may still be there — which
	// is an ORPHAN, the one shape the storage report can already see and this
	// job may safely retry.
	Lost int

	// Skipped is a claim lost to a concurrent change: somebody pinned the memo
	// between the read and the mark, which is exactly what the compare-and-swap
	// exists to honour.
	Skipped int

	// HeldBack is memos past their window with no durable transcript. Not an
	// error and not a backlog — the gate doing its job — but a number worth
	// seeing, because a memo discarded before it was transcribed lands here
	// permanently.
	HeldBack int
}

// Run sweeps until ctx is cancelled.
func (p *Pruner) Run(ctx context.Context) error {
	interval := p.Interval
	if interval <= 0 {
		interval = DefaultInterval
	}
	p.Logger.Info("retention pruner started",
		"interval", interval.String(), "window", p.window().String(),
		"gate", "a durable transcript, from a known runner at or above the model floor")

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			p.Logger.Info("retention pruner stopped")
			return nil
		case <-t.C:
			if _, err := p.Sweep(ctx, false); err != nil && ctx.Err() == nil {
				p.Logger.Error("retention sweep failed", "error", err)
			}
		}
	}
}

// Sweep is one pass. With dryRun set it reads and reports and touches nothing —
// and it reads THE SAME PREDICATE the real run marks with, which is what makes
// "a dry run lists exactly what a real run would delete" a property rather than
// a claim.
func (p *Pruner) Sweep(ctx context.Context, dryRun bool) (Report, error) {
	rep := Report{DryRun: dryRun}
	window := p.window()

	batch := p.Batch
	if batch <= 0 {
		batch = DefaultBatch
	}

	var err error
	rep.Considered, err = p.Store.PrunableAudio(ctx, window, batch)
	if err != nil {
		return rep, err
	}
	if rep.HeldBack, err = p.Store.HeldBackFromPruning(ctx, window); err != nil {
		return rep, err
	}

	if dryRun {
		return rep, nil
	}

	for _, m := range rep.Considered {
		claimed, err := p.Store.MarkAudioPruned(ctx, m.MemoID, window)
		if err != nil {
			return rep, err
		}
		if !claimed {
			// Pinned, or already pruned, between the read and now. The
			// compare-and-swap is what makes that a no-op rather than a
			// deletion somebody had just asked not to happen.
			rep.Skipped++
			p.Logger.Info("memo changed under the sweep; not pruned", "memo", m.MemoID)
			continue
		}

		path, err := p.Audio.Path(m.Ref())
		if err != nil {
			rep.Lost++
			p.Logger.Error("a claimed memo has no derivable path; its file is now an orphan",
				"memo", m.MemoID, "error", err)
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			// THE MARK STANDS. Re-clearing it would mean a memo whose audio may
			// or may not exist, which is worse than an orphan: the storage
			// report can see an orphan and nothing can see the other thing.
			rep.Lost++
			p.Logger.Error("could not unlink a claimed recording; it is now an orphan",
				"memo", m.MemoID, "error", err)
			continue
		}

		rep.Pruned++
		rep.Bytes += m.ByteSize
		p.Logger.Info("audio pruned", "memo", m.MemoID, "bytes", m.ByteSize,
			"retention", m.Retention, "captured", m.CapturedAt.Format(time.RFC3339))
	}

	if rep.Pruned > 0 || rep.Lost > 0 {
		p.Logger.Info("retention sweep complete",
			"pruned", rep.Pruned, "bytes", rep.Bytes, "orphaned", rep.Lost,
			"skipped", rep.Skipped, "held_back", rep.HeldBack)
	}
	return rep, nil
}

func (p *Pruner) window() time.Duration {
	if p.Window <= 0 {
		return audio.ProjectionWindow
	}
	return p.Window
}

// String renders a dry run for an operator.
func (r Report) String() string {
	s := fmt.Sprintf("%d memo(s) would be pruned", len(r.Considered))
	if !r.DryRun {
		s = fmt.Sprintf("%d memo(s) pruned, %d orphaned, %d skipped", r.Pruned, r.Lost, r.Skipped)
	}
	var bytes int64
	for _, m := range r.Considered {
		bytes += m.ByteSize
	}
	return fmt.Sprintf("%s, %d byte(s)\n%d held back · no durable transcript", s, bytes, r.HeldBack)
}
