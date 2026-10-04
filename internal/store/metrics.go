package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CHRN-69, the four numbers that say whether Chronicle is healthy.
//
// EVERYTHING HERE IS A READ, AND A DERIVED ONE. Nothing in this file writes a
// row, and nothing is stored: a metric that was persisted would be a second
// account of the corpus that could disagree with the first. Each number is
// recomputed from the tables that already hold the fact, on the MAIN pool --
// the join across tier 1 and tier 2 that routing accuracy needs is exactly what
// the tier-1 role may not do, so this deliberately does not go through
// Tier1Store and widens nothing.
//
// The statements are schema-qualified throughout, as every query here is.

// QueueMetrics is the transcription backlog: memos that have been captured and
// have no transcript yet.
type QueueMetrics struct {
	// ByState counts memos in each of the three pre-transcript states. Always
	// carries all three keys, so a zero is a stated zero rather than an absence.
	ByState map[string]int64

	// Depth is their sum.
	Depth int64

	// OldestCapturedAt is when the longest-waiting memo arrived; nil when
	// nothing is waiting. captured_at and not updated_at, because updated_at
	// moves when somebody pins the memo, and pinning a stuck memo must not make
	// the stall look younger.
	OldestCapturedAt *time.Time

	// Held is memos whose transcription gave up. Not queue depth -- they are
	// not waiting, they are stopped -- but a pipeline that is broken shows here
	// first, and a number that only counted the waiting would call it healthy.
	Held int64
}

// QueueMetrics reads the backlog at one instant.
func (s *Store) QueueMetrics(ctx context.Context) (QueueMetrics, error) {
	q := QueueMetrics{ByState: map[string]int64{
		StateCaptured: 0, StateQueued: 0, StateTranscribing: 0,
	}}
	rows, err := s.pool.Query(ctx, `
		SELECT state, count(*), min(captured_at)
		  FROM tier2.memos
		 WHERE state IN ('captured', 'queued', 'transcribing', 'held')
		 GROUP BY state`)
	if err != nil {
		return q, fmt.Errorf("store: queue metrics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			state  string
			n      int64
			oldest time.Time
		)
		if err := rows.Scan(&state, &n, &oldest); err != nil {
			return q, err
		}
		if state == StateHeld {
			q.Held = n
			continue
		}
		q.ByState[state] = n
		q.Depth += n
		if q.OldestCapturedAt == nil || oldest.Before(*q.OldestCapturedAt) {
			o := oldest
			q.OldestCapturedAt = &o
		}
	}
	return q, rows.Err()
}

// ASRLatency is what transcription took, for one model, over a window.
type ASRLatency struct {
	Model string

	// Jobs is how many collected attempts the figures cover.
	Jobs int64

	// AudioSeconds and WallSeconds are summed over those jobs, so the aggregate
	// speed is audio over wall -- weighted by length, which a mean of per-job
	// ratios would not be: one two-second memo would count as much as one of
	// ten minutes.
	AudioSeconds float64
	WallSeconds  float64

	// P50 and P95 are per-job wall-clock seconds.
	P50Seconds float64
	P95Seconds float64
}

// Realtime is the aggregate x-realtime: seconds of audio transcribed per second
// of wall clock. Zero when there is nothing to divide by.
func (l ASRLatency) Realtime() float64 {
	if l.WallSeconds <= 0 {
		return 0
	}
	return l.AudioSeconds / l.WallSeconds
}

// ASRLatencyByModel measures submit-to-collect over the trailing window.
//
// WHAT THIS IS NOT: inference time. The ASR service reports no per-job timing,
// so the only two clocks Chronicle holds are when it submitted a job and when
// it collected the result. That interval includes the service's own queue and
// up to one pump interval (transcribe.DefaultInterval) before the result is
// noticed, so it OVERSTATES inference and understates x-realtime against the
// CHRN-12 figures, which are resident-model inference alone. It is the right
// number for "is the pipeline getting slower" and the wrong one for "is the GPU
// as fast as measured"; the baseline comparison is a drift signal, not a
// reproduction of the benchmark.
//
// Audio length comes from the transcript (audio_duration_ms, the service's own
// account), via the latest complete one, so a memo whose recorded header
// duration disagrees with the decode is measured against what was decoded.
func (s *Store) ASRLatencyByModel(ctx context.Context, window time.Duration) ([]ASRLatency, error) {
	rows, err := s.pool.Query(ctx, `
		WITH done AS (
		  SELECT j.model,
		         t.audio_duration_ms / 1000.0                                AS audio_s,
		         extract(epoch FROM (j.collected_at - j.submitted_at))       AS wall_s
		    FROM tier1.memo_jobs j
		    JOIN LATERAL (SELECT audio_duration_ms
		                    FROM tier2.transcripts t
		                   WHERE t.memo_id = j.memo_id
		                     AND NOT t.partial
		                     AND t.audio_duration_ms > 0
		                   ORDER BY t.transcribed_at DESC
		                   LIMIT 1) t ON true
		   WHERE j.collected_at IS NOT NULL
		     AND j.submitted_at IS NOT NULL
		     AND j.collected_at >= j.submitted_at
		     AND j.collected_at > now() - make_interval(secs => $1))
		SELECT model, count(*), sum(audio_s)::float8, sum(wall_s)::float8,
		       percentile_cont(0.5)  WITHIN GROUP (ORDER BY wall_s)::float8,
		       percentile_cont(0.95) WITHIN GROUP (ORDER BY wall_s)::float8
		  FROM done
		 GROUP BY model
		 ORDER BY model`, window.Seconds())
	if err != nil {
		return nil, fmt.Errorf("store: asr latency: %w", err)
	}
	defer rows.Close()
	var out []ASRLatency
	for rows.Next() {
		var l ASRLatency
		if err := rows.Scan(&l.Model, &l.Jobs, &l.AudioSeconds, &l.WallSeconds,
			&l.P50Seconds, &l.P95Seconds); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// RoutingAgreement is how often a person's routing decision matched the
// Scribe's proposal, over a window.
//
// IT IS AGREEMENT, NOT ACCURACY, and the difference is the whole caveat. There
// is no ground truth in production: the only label is what the operator chose,
// and the operator chose it looking at the Scribe's proposal. Agreement
// therefore reads high whenever people accept a pre-filled card without
// thinking, and it cannot see a wrong proposal that was accepted. What it CAN
// see, honestly, is movement: a Scribe that starts being corrected more often
// is drifting, whichever way the truth lies. The point-in-time accuracy figure
// is `chronicle eval` (CHRN-36); this is the live signal that says when to
// re-run it.
type RoutingAgreement struct {
	// Decided is every routing decision made in the window.
	Decided int64

	// Agreed chose the destination the Scribe proposed.
	Agreed int64

	// Corrected chose a different one.
	Corrected int64

	// Unaided had no proposal at all -- the Scribe was off, or failed, or the
	// memo predates it. Counted apart: folding them into either side would
	// make the ratio move when the Scribe's coverage does.
	Unaided int64
}

// Ratio is Agreed over (Agreed + Corrected), the decisions the Scribe had a
// say in. NaN-free: zero when it had a say in none, which callers must read
// together with Aided.
func (r RoutingAgreement) Ratio() float64 {
	if n := r.Agreed + r.Corrected; n > 0 {
		return float64(r.Agreed) / float64(n)
	}
	return 0
}

// Aided is the decisions the ratio is computed over.
func (r RoutingAgreement) Aided() int64 { return r.Agreed + r.Corrected }

// RoutingAgreement compares tier2.memo_links (what a person chose) with the
// latest Scribe payload for the same memo (tier1.memo_proposals), over
// decisions made in the trailing window.
//
// A proposal counts when it carries a payload at all -- `valid` and
// `needs_input` both do, and a needs_input TICKET still predicted TICKET. An
// `invalid` one has none and is Unaided. A refused link is still a decision.
func (s *Store) RoutingAgreement(ctx context.Context, window time.Duration) (RoutingAgreement, error) {
	var r RoutingAgreement
	err := s.pool.QueryRow(ctx, `
		WITH latest AS (
		  SELECT DISTINCT ON (memo_id) memo_id, payload->>'destination' AS dest
		    FROM tier1.memo_proposals
		   WHERE payload IS NOT NULL
		   ORDER BY memo_id, updated_at DESC)
		SELECT count(*),
		       count(*) FILTER (WHERE p.dest = l.destination),
		       count(*) FILTER (WHERE p.dest IS NOT NULL AND p.dest <> l.destination),
		       count(*) FILTER (WHERE p.dest IS NULL)
		  FROM tier2.memo_links l
		  LEFT JOIN latest p ON p.memo_id = l.memo_id
		 WHERE l.created_at > now() - make_interval(secs => $1)`,
		window.Seconds()).Scan(&r.Decided, &r.Agreed, &r.Corrected, &r.Unaided)
	if err != nil {
		return r, fmt.Errorf("store: routing agreement: %w", err)
	}
	return r, nil
}

// PruneMetrics is what the retention pruner has done, read back from the
// memos it marked -- not from the pruner's own counters, which die with the
// process and would report zero after every redeploy.
type PruneMetrics struct {
	// Last24h and Total are memos whose audio has been pruned, with the bytes
	// they held (byte_size is immutable, and the file is gone).
	Last24h      int64
	Last24hBytes int64
	Total        int64
	TotalBytes   int64

	// LastPrunedAt is nil when nothing has ever been pruned.
	LastPrunedAt *time.Time

	// HeldBack is memos past their window that the gate is refusing to prune
	// because they have no durable transcript. The gate working, and not an
	// error -- but the number that explains why Total is lower than the
	// calendar says it should be.
	HeldBack int64

	// Violations are pruned memos that should not have been, by the rules the
	// pruner is supposed to obey. EVERY ONE OF THESE SHOULD BE ZERO FOREVER:
	// they are the "an incorrect deletion would be obvious the next morning"
	// half of CHRN-69's Done-when. They re-derive each rule independently of
	// the pruner's own clause (prunableClause) rather than reusing it -- a
	// check that shared the pruner's predicate would agree with it when it was
	// wrong.
	//
	// WithoutTranscript: no durable transcript existed. THE WORST THING THIS
	// SYSTEM CAN DO (CLAUDE.md invariant 1): the audio was the only copy.
	// Pinned: retention is `forever`. Early: retention `days_30` and the window
	// had not passed.
	WithoutTranscript int64
	Pinned            int64
	Early             int64

	// ViolatingMemos names up to ten of them, newest prune first, so the line
	// that reports a violation says which memos to look at.
	ViolatingMemos []uuid.UUID
}

// Violated reports whether any rule was broken.
func (p PruneMetrics) Violated() bool {
	return p.WithoutTranscript+p.Pinned+p.Early > 0
}

// PruneMetrics reads the prune ledger. window is the retention window the
// pruner runs with (audio.ProjectionWindow), so "early" means the same thing
// here as there.
func (s *Store) PruneMetrics(ctx context.Context, window time.Duration) (PruneMetrics, error) {
	var p PruneMetrics
	// $1 window secs, $2 runner allow-list, $3 model allow-list: the same
	// parameter order DurableClause documents.
	const noDurable = `NOT EXISTS (SELECT 1 FROM tier2.transcripts
	                               WHERE memo_id = m.id AND ` + DurableClause + `)`
	const early = `m.retention = 'days_30'
	               AND m.audio_pruned_at < m.captured_at + make_interval(secs => $1)`

	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE m.audio_pruned_at > now() - interval '24 hours'),
		       coalesce(sum(m.byte_size) FILTER (WHERE m.audio_pruned_at > now() - interval '24 hours'), 0),
		       count(*),
		       coalesce(sum(m.byte_size), 0),
		       max(m.audio_pruned_at),
		       count(*) FILTER (WHERE `+noDurable+`),
		       count(*) FILTER (WHERE m.retention = 'forever'),
		       count(*) FILTER (WHERE `+early+`)
		  FROM tier2.memos m
		 WHERE m.audio_pruned_at IS NOT NULL`,
		window.Seconds(), SufficientRunners, SufficientModels).
		Scan(&p.Last24h, &p.Last24hBytes, &p.Total, &p.TotalBytes, &p.LastPrunedAt,
			&p.WithoutTranscript, &p.Pinned, &p.Early)
	if err != nil {
		return p, fmt.Errorf("store: prune metrics: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
		SELECT m.id
		  FROM tier2.memos m
		 WHERE m.audio_pruned_at IS NOT NULL
		   AND (`+noDurable+` OR m.retention = 'forever' OR (`+early+`))
		 ORDER BY m.audio_pruned_at DESC
		 LIMIT 10`,
		window.Seconds(), SufficientRunners, SufficientModels)
	if err != nil {
		return p, fmt.Errorf("store: prune violations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return p, err
		}
		p.ViolatingMemos = append(p.ViolatingMemos, id)
	}
	if err := rows.Err(); err != nil {
		return p, err
	}

	held, err := s.HeldBackFromPruning(ctx, window)
	if err != nil {
		return p, err
	}
	p.HeldBack = int64(held)
	return p, nil
}
