package asr

import (
	"context"
	"log/slog"
	"time"
)

// Reaper is what makes "no job is ever dropped" true across a crash.
//
// It runs two sweeps that are DIFFERENT THINGS and must not be confused with
// each other, sharing one goroutine because both are housekeeping on a ticker
// and two goroutines for a weekly sweep would be ceremony:
//
//   - LEASE EXPIRY, on a short interval. A worker that died stops renewing;
//     this returns its job to the queue with attempts incremented, or — if the
//     job had been cancelled — sends it to `cancelled` and never back to the
//     queue. Nothing runs inside the dying process, which is the point: a
//     shutdown hook is not a mechanism, because kill -9 does not run it.
//
//   - RESULT PAYLOAD PURGE, on a long one. This drops the transcript payload of
//     jobs past their TTL. IT DOES NOT DELETE JOB ROWS and it has nothing
//     whatever to do with CHRN-22's retention pruner: a result here is
//     regenerable from audio the client still holds, whereas a memo's audio is
//     not regenerable from anything. The row surviving the purge is what lets a
//     late fetch answer 410 Gone rather than 404.
type Reaper struct {
	Store  *Store
	Logger *slog.Logger

	// Interval is how often leases are checked. It should be well under the
	// lease TTL — a lease that expires and then waits a full interval to be
	// noticed is a job that sits idle for the sum of the two.
	Interval time.Duration

	// PurgeInterval is how often expired result payloads are swept.
	PurgeInterval time.Duration

	// MaxAttempts is CHRN-28's ceiling for a lease that simply expired. The
	// reaper never sees the expensive reasons — a deadline breach is released
	// by the worker, which is alive — so it carries one number rather than two.
	MaxAttempts int
}

// Run sweeps until ctx is cancelled.
func (r *Reaper) Run(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	purgeEvery := r.PurgeInterval
	if purgeEvery <= 0 {
		purgeEvery = time.Hour
	}

	leases := time.NewTicker(interval)
	defer leases.Stop()
	purges := time.NewTicker(purgeEvery)
	defer purges.Stop()

	r.Logger.Info("job reaper started",
		"lease_check", interval.String(), "result_purge", purgeEvery.String())

	for {
		select {
		case <-ctx.Done():
			r.Logger.Info("job reaper stopped")
			return nil
		case <-leases.C:
			r.reapOnce(ctx)
		case <-purges.C:
			r.purgeOnce(ctx)
		}
	}
}

func (r *Reaper) reapOnce(ctx context.Context) {
	max := r.MaxAttempts
	if max < 1 {
		max = DefaultMaxAttempts
	}
	reaped, err := r.Store.Reap(ctx, max)
	if err != nil {
		if ctx.Err() == nil {
			r.Logger.Error("lease reap failed", "error", err)
		}
		return
	}
	for _, j := range reaped {
		if j.Status == StatusFailed {
			// AT ERROR, because this is the end of the line for a memo. The
			// retry ceiling has stopped it and nothing will pick it up again;
			// a human is the only thing that moves it now.
			r.Logger.Error("RETRY CEILING REACHED; job dead-lettered and will not be retried",
				"job", j.ID, "attempts", j.Attempts, "code", ExhaustedCode)
			continue
		}
		// At WARN, and one line per job. A reaped lease means a worker died or
		// stalled, which is not routine, and a count alone would not say which
		// job to go and look at.
		r.Logger.Warn("lease expired; job released",
			"job", j.ID, "to", j.Status, "attempts", j.Attempts)
	}
}

func (r *Reaper) purgeOnce(ctx context.Context) {
	n, err := r.Store.PurgeResults(ctx)
	if err != nil {
		if ctx.Err() == nil {
			r.Logger.Error("result purge failed", "error", err)
		}
		return
	}
	if n > 0 {
		r.Logger.Info("purged expired result payloads",
			"count", n, "note", "the job rows remain; a late fetch answers 410")
	}
}
