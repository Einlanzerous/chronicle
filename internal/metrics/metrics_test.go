package metrics

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

type fakeSource struct {
	queue   store.QueueMetrics
	triage  store.TriageBacklog
	asr     []store.ASRLatency
	short   store.RoutingAgreement
	long    store.RoutingAgreement
	prune   store.PruneMetrics
	pruneEr error
}

func (f fakeSource) QueueMetrics(context.Context) (store.QueueMetrics, error) { return f.queue, nil }
func (f fakeSource) TriageBacklog(context.Context) (store.TriageBacklog, error) {
	return f.triage, nil
}
func (f fakeSource) ASRLatencyByModel(context.Context, time.Duration) ([]store.ASRLatency, error) {
	return f.asr, nil
}
func (f fakeSource) RoutingAgreement(_ context.Context, w time.Duration) (store.RoutingAgreement, error) {
	if w == RoutingShort {
		return f.short, nil
	}
	return f.long, nil
}
func (f fakeSource) PruneMetrics(context.Context, time.Duration) (store.PruneMetrics, error) {
	return f.prune, f.pruneEr
}

var epoch = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func collect(f fakeSource) Snapshot {
	c := &Collector{Source: f, Window: 30 * 24 * time.Hour, Now: func() time.Time { return epoch }}
	return c.Collect(context.Background())
}

func names(a []Alert) string {
	var n []string
	for _, x := range a {
		n = append(n, x.Name)
	}
	return strings.Join(n, ",")
}

func TestAHealthyCorpusRaisesNothing(t *testing.T) {
	recent := epoch.Add(-10 * time.Minute)
	s := collect(fakeSource{
		queue: store.QueueMetrics{ByState: map[string]int64{"queued": 1}, Depth: 1, OldestCapturedAt: &recent},
		asr:   []store.ASRLatency{{Model: "small.en", Jobs: 20, AudioSeconds: 1200, WallSeconds: 60}},
		short: store.RoutingAgreement{Agreed: 9, Corrected: 1},
		long:  store.RoutingAgreement{Agreed: 36, Corrected: 4},
	})
	if a := s.Alerts(); len(a) != 0 {
		t.Fatalf("alerts on a healthy corpus: %s", names(a))
	}
}

func TestAStalledQueueWarnsAtSixHours(t *testing.T) {
	at := func(age time.Duration) []Alert {
		old := epoch.Add(-age)
		return collect(fakeSource{queue: store.QueueMetrics{Depth: 1, OldestCapturedAt: &old}}).Alerts()
	}
	if a := at(5 * time.Hour); len(a) != 0 {
		t.Errorf("five hours warned: %s", names(a))
	}
	a := at(7 * time.Hour)
	if names(a) != "queue_stalled" || a[0].Level != slog.LevelWarn {
		t.Errorf("seven hours: %s", names(a))
	}
}

func TestASRDriftNeedsEnoughJobsAndAKnownBaseline(t *testing.T) {
	slow := store.ASRLatency{Model: "small.en", Jobs: 10, AudioSeconds: 100, WallSeconds: 100} // 1x vs 59.6x
	if n := names(collect(fakeSource{asr: []store.ASRLatency{slow}}).Alerts()); n != "asr_drift" {
		t.Errorf("1x against a 59.6x baseline: %q", n)
	}
	few := slow
	few.Jobs = 2
	if n := names(collect(fakeSource{asr: []store.ASRLatency{few}}).Alerts()); n != "" {
		t.Errorf("two jobs alerted: %q", n)
	}
	unknown := slow
	unknown.Model = "tiny.en"
	if n := names(collect(fakeSource{asr: []store.ASRLatency{unknown}}).Alerts()); n != "" {
		t.Errorf("a model with no baseline alerted: %q", n)
	}
}

func TestRoutingDriftComparesTheWeekWithTheMonth(t *testing.T) {
	s := collect(fakeSource{
		short: store.RoutingAgreement{Agreed: 5, Corrected: 10}, // 33%
		long:  store.RoutingAgreement{Agreed: 40, Corrected: 20},
	})
	if n := names(s.Alerts()); n != "routing_drift" {
		t.Errorf("a collapse in agreement: %q", n)
	}
	thin := collect(fakeSource{
		short: store.RoutingAgreement{Agreed: 0, Corrected: 3},
		long:  store.RoutingAgreement{Agreed: 40, Corrected: 3},
	})
	if n := names(thin.Alerts()); n != "" {
		t.Errorf("three decisions are noise, not drift: %q", n)
	}
}

// THE ONE THAT MUST NEVER BE QUIET.
func TestAPruneViolationIsAnErrorAndNamesTheMemo(t *testing.T) {
	id := uuid.New()
	s := collect(fakeSource{prune: store.PruneMetrics{WithoutTranscript: 1, ViolatingMemos: []uuid.UUID{id}}})
	a := s.Alerts()
	if names(a) != "prune_violation" || a[0].Level != slog.LevelError {
		t.Fatalf("alerts: %s", names(a))
	}
	if !strings.Contains(a[0].Text, id.String()) {
		t.Errorf("the alert does not name the memo: %s", a[0].Text)
	}
	if !strings.Contains(s.Prometheus(), `chronicle_prune_violations{rule="no_durable_transcript"} 1`) {
		t.Errorf("the exposition does not carry the violation:\n%s", s.Prometheus())
	}
}

// A section that cannot be read is absent, never zero.
func TestAnUnreadableSectionIsAbsentNotZero(t *testing.T) {
	s := collect(fakeSource{pruneEr: errors.New("connection refused")})
	out := s.Prometheus()
	if strings.Contains(out, "chronicle_prune_violations") {
		t.Errorf("an unreadable prune ledger still reported violations (as zero):\n%s", out)
	}
	if !strings.Contains(out, `chronicle_metrics_section_up{section="prune"} 0`) ||
		!strings.Contains(out, `chronicle_metrics_section_up{section="queue"} 1`) {
		t.Errorf("section_up does not say which section failed:\n%s", out)
	}
	if n := names(s.Alerts()); n != "metrics_unreadable" {
		t.Errorf("alerts: %q", n)
	}
}

func TestPrometheusCarriesAllFourNumbers(t *testing.T) {
	old := epoch.Add(-3 * time.Hour)
	last := epoch.Add(-time.Hour)
	out := collect(fakeSource{
		queue: store.QueueMetrics{ByState: map[string]int64{"captured": 2}, Depth: 2, OldestCapturedAt: &old},
		asr:   []store.ASRLatency{{Model: "small.en", Jobs: 3, AudioSeconds: 600, WallSeconds: 20, P50Seconds: 5, P95Seconds: 9}},
		short: store.RoutingAgreement{Agreed: 3, Corrected: 1, Unaided: 2},
		prune: store.PruneMetrics{Last24h: 4, Last24hBytes: 4096, LastPrunedAt: &last},
	}).Prometheus()
	for _, want := range []string{
		`chronicle_queue_depth{state="captured"} 2`,
		`chronicle_queue_oldest_age_seconds 10800`,
		`chronicle_asr_realtime{model="small.en"} 30`,
		`chronicle_asr_baseline_realtime{model="small.en"} 59.6`,
		`chronicle_routing_decisions{window="7d",outcome="unaided"} 2`,
		`chronicle_routing_agreement_ratio{window="7d"} 0.75`,
		`chronicle_prune_bytes_24h 4096`,
		`chronicle_prune_violations{rule="no_durable_transcript"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
