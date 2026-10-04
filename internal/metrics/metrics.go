// Package metrics answers "is Chronicle healthy" with four numbers, none of
// which Chronicle stores (CHRN-69).
//
//   - QUEUE: how many memos are waiting for a transcript, and how long the
//     oldest has waited. A memo that has waited six hours is a broken pipeline
//     nobody has noticed yet.
//   - ASR LATENCY: how fast transcription is going, set against the CHRN-12
//     measurement (docs/benchmarks/whisper-model-choice.md).
//   - ROUTING AGREEMENT: how often a person's routing decision matched the
//     Scribe's proposal, over time. Not accuracy; see store.RoutingAgreement.
//   - PRUNE VOLUME: what the retention pruner deleted, and an independent
//     check that nothing it deleted should have been kept.
//
// They are recomputed from the tables that already hold each fact, every time
// they are asked for, so they cannot drift from the corpus. They are TIER-1
// SHAPED -- derived, disposable, never hand-edited -- and live in no table.
//
// Two surfaces read them, so that "visible without SSH" does not depend on any
// one consumer being configured: GET /metrics (Prometheus text, owner only) and
// a structured log line every period, which Dozzle shows and Datadog can
// monitor without a scrape. Both come from one Collect.
package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// Windows. Chosen to be read by a person: a week is the shortest span in which
// a handful of memos a day gives the ratios a denominator, and four weeks is
// the span it is compared against.
const (
	ASRWindow     = 7 * 24 * time.Hour
	RoutingShort  = 7 * 24 * time.Hour
	RoutingLong   = 28 * 24 * time.Hour
	DefaultPeriod = 5 * time.Minute
)

// Alert thresholds. Each one is a judgement, not a measurement, and is named
// here rather than buried so that tuning it is a one-line diff.
const (
	// QueueStall is the ticket's own figure: six hours waiting is a broken
	// pipeline, not a busy one.
	QueueStall = 6 * time.Hour

	// ASRDriftFloor is the fraction of the CHRN-12 baseline below which
	// latency is called drift. Deliberately low (a quarter): the measured
	// interval includes the pump's poll interval and the service's own queue,
	// which the benchmark does not, so a healthy GPU reads well under 1.0 here
	// and a threshold near 1.0 would alarm permanently. Uncalibrated against
	// production; it is expected to move once there is a week of figures.
	ASRDriftFloor = 0.25

	// ASRMinJobs is how many jobs a model needs in the window before drift is
	// reported at all.
	ASRMinJobs = 5

	// RoutingDrop is how far the short window's agreement may fall below the
	// long window's before it is called out, and RoutingMinAided how many aided
	// decisions the short window needs for that to mean anything.
	RoutingDrop     = 0.15
	RoutingMinAided = 10
)

// Baselines is the CHRN-12 resident-model x-realtime on the R9700 (Vulkan,
// 60 s Opus note, model held in memory). Keyed by the bare model name a job
// carries. Models the benchmark did not measure have no entry, and report no
// ratio rather than an invented one.
var Baselines = map[string]float64{
	"base.en":   76.4,
	"small.en":  59.6,
	"medium.en": 36.8,
	"large-v3":  18.3,
}

// Source is the slice of the store this reads. Written down here so the package
// can be tested without a database, and so it is visible that every method is a
// read.
type Source interface {
	QueueMetrics(ctx context.Context) (store.QueueMetrics, error)
	TriageBacklog(ctx context.Context) (store.TriageBacklog, error)
	ASRLatencyByModel(ctx context.Context, window time.Duration) ([]store.ASRLatency, error)
	RoutingAgreement(ctx context.Context, window time.Duration) (store.RoutingAgreement, error)
	PruneMetrics(ctx context.Context, window time.Duration) (store.PruneMetrics, error)
}

// Snapshot is all four numbers at one instant.
type Snapshot struct {
	At time.Time

	Queue   store.QueueMetrics
	Triage  store.TriageBacklog
	ASR     []store.ASRLatency
	Routing map[string]store.RoutingAgreement // keyed "7d", "28d"
	Prune   store.PruneMetrics

	// Failed names each section that could not be read, with why. A section
	// that failed is absent from the output rather than zero: a zero queue and
	// an unreadable queue want completely different responses.
	Failed map[string]error
}

// Collector gathers snapshots.
type Collector struct {
	Source Source
	Window time.Duration // the retention window the pruner uses
	Now    func() time.Time
}

// Collect reads every section independently, so one broken query does not
// blank the other three.
func (c *Collector) Collect(ctx context.Context) Snapshot {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	s := Snapshot{At: now(), Failed: map[string]error{}, Routing: map[string]store.RoutingAgreement{}}

	var err error
	if s.Queue, err = c.Source.QueueMetrics(ctx); err != nil {
		s.Failed["queue"] = err
	}
	if s.Triage, err = c.Source.TriageBacklog(ctx); err != nil {
		s.Failed["triage"] = err
	}
	if s.ASR, err = c.Source.ASRLatencyByModel(ctx, ASRWindow); err != nil {
		s.Failed["asr"] = err
	}
	short, errS := c.Source.RoutingAgreement(ctx, RoutingShort)
	long, errL := c.Source.RoutingAgreement(ctx, RoutingLong)
	if errS != nil || errL != nil {
		if errS != nil {
			s.Failed["routing"] = errS
		} else {
			s.Failed["routing"] = errL
		}
	} else {
		s.Routing["7d"], s.Routing["28d"] = short, long
	}
	if s.Prune, err = c.Source.PruneMetrics(ctx, c.Window); err != nil {
		s.Failed["prune"] = err
	}
	return s
}

// Alert is something the snapshot says is wrong.
type Alert struct {
	Level slog.Level
	Name  string
	Text  string
}

func (s Snapshot) ok(section string) bool {
	_, bad := s.Failed[section]
	return !bad
}

// Alerts evaluates the snapshot against the thresholds above.
func (s Snapshot) Alerts() []Alert {
	var out []Alert

	if s.ok("prune") && s.Prune.Violated() {
		out = append(out, Alert{slog.LevelError, "prune_violation", fmt.Sprintf(
			"audio was pruned that the rules say should be kept: %d without a durable transcript, "+
				"%d pinned, %d before the window (memos %v)",
			s.Prune.WithoutTranscript, s.Prune.Pinned, s.Prune.Early, s.Prune.ViolatingMemos)})
	}
	if s.ok("queue") && s.Queue.OldestCapturedAt != nil {
		if age := s.At.Sub(*s.Queue.OldestCapturedAt); age > QueueStall {
			out = append(out, Alert{slog.LevelWarn, "queue_stalled", fmt.Sprintf(
				"the oldest of %d memo(s) awaiting a transcript has waited %s (threshold %s)",
				s.Queue.Depth, age.Round(time.Minute), QueueStall)})
		}
	}
	if s.ok("asr") {
		for _, l := range s.ASR {
			base, known := Baselines[l.Model]
			if !known || l.Jobs < ASRMinJobs {
				continue
			}
			if r := l.Realtime(); r < base*ASRDriftFloor {
				out = append(out, Alert{slog.LevelWarn, "asr_drift", fmt.Sprintf(
					"%s is transcribing at %.1fx realtime over %d jobs against a CHRN-12 baseline of %.1fx "+
						"(floor %.0f%%); the GPU may be contended or the model changed",
					l.Model, r, l.Jobs, base, ASRDriftFloor*100)})
			}
		}
	}
	if sh, found := s.Routing["7d"]; found && sh.Aided() >= RoutingMinAided {
		if lg := s.Routing["28d"]; lg.Aided() > sh.Aided() && lg.Ratio()-sh.Ratio() > RoutingDrop {
			out = append(out, Alert{slog.LevelWarn, "routing_drift", fmt.Sprintf(
				"the Scribe's proposals were taken as proposed %.0f%% of the time this week against %.0f%% over four weeks; "+
					"re-run `chronicle eval`", sh.Ratio()*100, lg.Ratio()*100)})
		}
	}
	for name, err := range s.Failed {
		out = append(out, Alert{slog.LevelWarn, "metrics_unreadable", fmt.Sprintf("%s could not be read: %v", name, err)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Log writes the snapshot as ONE structured info line, then one line per alert
// at the alert's own level. The info line is the "next morning" view: a person
// reading Dozzle, or a Datadog monitor on the alert lines, needs no scrape.
func (s Snapshot) Log(l *slog.Logger) {
	var attrs []any
	if s.ok("queue") {
		attrs = append(attrs, "queue_depth", s.Queue.Depth, "queue_held", s.Queue.Held,
			"queue_oldest_age_s", ageSeconds(s.At, s.Queue.OldestCapturedAt))
	}
	if s.ok("triage") {
		attrs = append(attrs, "triage_backlog", s.Triage.Total,
			"triage_oldest_age_s", ageSeconds(s.At, s.Triage.OldestCapturedAt))
	}
	if s.ok("asr") {
		for _, a := range s.ASR {
			attrs = append(attrs, "asr_"+a.Model+"_realtime", round1(a.Realtime()), "asr_"+a.Model+"_jobs", a.Jobs)
		}
	}
	if r, found := s.Routing["7d"]; found {
		attrs = append(attrs, "routing_7d_decided", r.Decided, "routing_7d_agreed", r.Agreed,
			"routing_7d_corrected", r.Corrected, "routing_7d_unaided", r.Unaided)
	}
	if s.ok("prune") {
		attrs = append(attrs, "prune_24h", s.Prune.Last24h, "prune_24h_bytes", s.Prune.Last24hBytes,
			"prune_total", s.Prune.Total, "prune_held_back", s.Prune.HeldBack,
			"prune_violations", s.Prune.WithoutTranscript+s.Prune.Pinned+s.Prune.Early,
			"prune_without_transcript", s.Prune.WithoutTranscript)
	}
	l.Info("metrics snapshot", attrs...)
	for _, a := range s.Alerts() {
		l.Log(context.Background(), a.Level, a.Text, "alert", a.Name)
	}
}

func ageSeconds(now time.Time, t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return int64(now.Sub(*t).Seconds())
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// Run logs a snapshot every period until ctx is cancelled. It logs once at
// start, so a redeploy is followed immediately by the state it came up in.
func (c *Collector) Run(ctx context.Context, l *slog.Logger, period time.Duration) error {
	if period <= 0 {
		period = DefaultPeriod
	}
	l.Info("metrics logger started", "period", period.String())
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		c.Collect(cctx).Log(l)
		cancel()
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Prometheus renders the snapshot in the text exposition format. Hand-written:
// the format is a page long, and a client library is a dependency for a
// handful of gauges. Every metric is a gauge, because each is recomputed from
// the corpus at scrape time rather than accumulated in process -- a counter
// that lived in memory would reset to zero on every deploy and make a prune
// volume look like it had stopped.
func (s Snapshot) Prometheus() string {
	var b strings.Builder
	g := func(name, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	}
	v := func(name, labels string, val any) {
		if labels != "" {
			labels = "{" + labels + "}"
		}
		fmt.Fprintf(&b, "%s%s %v\n", name, labels, val)
	}

	g("chronicle_metrics_section_up", "1 when the section could be read from the database, 0 when its figures are absent")
	for _, sec := range []string{"queue", "triage", "asr", "routing", "prune"} {
		up := 0
		if s.ok(sec) {
			up = 1
		}
		v("chronicle_metrics_section_up", `section="`+sec+`"`, up)
	}

	if s.ok("queue") {
		g("chronicle_queue_depth", "memos awaiting a transcript, by state")
		for _, st := range []string{store.StateCaptured, store.StateQueued, store.StateTranscribing} {
			v("chronicle_queue_depth", `state="`+st+`"`, s.Queue.ByState[st])
		}
		g("chronicle_queue_oldest_age_seconds", "age of the longest-waiting memo; 0 when nothing waits")
		v("chronicle_queue_oldest_age_seconds", "", ageSeconds(s.At, s.Queue.OldestCapturedAt))
		g("chronicle_queue_held", "memos whose transcription gave up and need a person")
		v("chronicle_queue_held", "", s.Queue.Held)
	}
	if s.ok("triage") {
		g("chronicle_triage_backlog", "transcribed memos awaiting a routing decision")
		v("chronicle_triage_backlog", "", s.Triage.Total)
		g("chronicle_triage_oldest_age_seconds", "age of the longest-waiting undecided memo")
		v("chronicle_triage_oldest_age_seconds", "", ageSeconds(s.At, s.Triage.OldestCapturedAt))
	}
	if s.ok("asr") {
		g("chronicle_asr_jobs", "collected transcription jobs in the trailing 7 days, by model")
		g("chronicle_asr_wall_seconds", "submit-to-collect seconds per job (includes service queue and poll interval, so it overstates inference)")
		g("chronicle_asr_realtime", "seconds of audio per wall-clock second, length-weighted")
		g("chronicle_asr_baseline_realtime", "CHRN-12 measured resident-model x-realtime, where the model was measured")
		g("chronicle_asr_realtime_ratio", "chronicle_asr_realtime over the CHRN-12 baseline")
		for _, l := range s.ASR {
			m := `model="` + l.Model + `"`
			v("chronicle_asr_jobs", m, l.Jobs)
			v("chronicle_asr_wall_seconds", m+`,quantile="0.5"`, l.P50Seconds)
			v("chronicle_asr_wall_seconds", m+`,quantile="0.95"`, l.P95Seconds)
			v("chronicle_asr_realtime", m, l.Realtime())
			if base, known := Baselines[l.Model]; known {
				v("chronicle_asr_baseline_realtime", m, base)
				v("chronicle_asr_realtime_ratio", m, l.Realtime()/base)
			}
		}
	}
	if s.ok("routing") {
		g("chronicle_routing_decisions", "routing decisions made in the window, by agreement with the Scribe's proposal")
		g("chronicle_routing_agreement_ratio", "agreed over (agreed + corrected); NOT accuracy. Absent when the Scribe had a say in no decision")
		for _, w := range []string{"7d", "28d"} {
			r := s.Routing[w]
			v("chronicle_routing_decisions", `window="`+w+`",outcome="agreed"`, r.Agreed)
			v("chronicle_routing_decisions", `window="`+w+`",outcome="corrected"`, r.Corrected)
			v("chronicle_routing_decisions", `window="`+w+`",outcome="unaided"`, r.Unaided)
			if r.Aided() > 0 {
				v("chronicle_routing_agreement_ratio", `window="`+w+`"`, r.Ratio())
			}
		}
	}
	if s.ok("prune") {
		p := s.Prune
		g("chronicle_prune_memos_24h", "memos whose audio was pruned in the last 24 hours")
		v("chronicle_prune_memos_24h", "", p.Last24h)
		g("chronicle_prune_bytes_24h", "bytes of audio pruned in the last 24 hours")
		v("chronicle_prune_bytes_24h", "", p.Last24hBytes)
		g("chronicle_prune_memos_total", "memos whose audio has ever been pruned")
		v("chronicle_prune_memos_total", "", p.Total)
		g("chronicle_prune_bytes_total", "bytes of audio ever pruned")
		v("chronicle_prune_bytes_total", "", p.TotalBytes)
		g("chronicle_prune_last_timestamp_seconds", "unix time of the most recent prune; absent if none")
		if p.LastPrunedAt != nil {
			v("chronicle_prune_last_timestamp_seconds", "", p.LastPrunedAt.Unix())
		}
		g("chronicle_prune_held_back", "memos past their window that the gate refuses to prune for want of a durable transcript")
		v("chronicle_prune_held_back", "", p.HeldBack)
		g("chronicle_prune_violations", "pruned memos that broke a rule the pruner must obey, by rule; every one should be 0 forever")
		v("chronicle_prune_violations", `rule="no_durable_transcript"`, p.WithoutTranscript)
		v("chronicle_prune_violations", `rule="pinned"`, p.Pinned)
		v("chronicle_prune_violations", `rule="before_window"`, p.Early)
	}
	return b.String()
}
