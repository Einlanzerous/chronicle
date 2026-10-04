package api

import (
	"context"
	"net/http"
	"time"

	"github.com/Einlanzerous/chronicle/internal/metrics"
)

// Metrics is what GET /metrics reads (CHRN-69). A *metrics.Collector in
// production; an interface so the route can be tested without a database.
type Metrics interface {
	Collect(ctx context.Context) metrics.Snapshot
}

// getMetrics serves the four health numbers as Prometheus text.
//
// HAND-REGISTERED, NOT IN openapi.yaml, and that is deliberate rather than an
// oversight in CHRN-97's "every route is in the document" rule. The document
// describes the API three clients are generated against; a text exposition
// format read by a scraper is not part of that API, and putting it there would
// regenerate the Go, web and Dart clients to carry an operation none of them
// calls. It is nonetheless NOT an unguarded route: router.go wraps it with the
// owner credential through the same guarded() the generated routes use, and a
// test asserts an anonymous request and a non-owner member are both refused.
//
// Owner only, because it names the corpus's size and the memos a violation
// concerns. A scraper therefore needs a credential; see the PR for what that
// leaves to the estate's own configuration.
func (a *api) getMetrics(w http.ResponseWriter, r *http.Request) {
	if a.metrics == nil {
		writeError(w, http.StatusServiceUnavailable, codeMetricsUnconfigured,
			"metrics need a database")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	snap := a.metrics.Collect(ctx)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(snap.Prometheus()))
}
