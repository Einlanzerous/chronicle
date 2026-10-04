package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/metrics"
)

type fakeMetrics struct{ snap metrics.Snapshot }

func (f fakeMetrics) Collect(context.Context) metrics.Snapshot { return f.snap }

func metricsRouter(t *testing.T, m Metrics) http.Handler {
	t.Helper()
	f := newFakeAccounts()
	owner := person("owner@example.com", true)
	member := person("member@example.com", false)
	f.byEmail[owner.Email] = owner
	f.byEmail[member.Email] = member
	f.sessions["chr_owner"] = owner
	f.sessions["chr_member"] = member
	return NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(),
		Version: "test", SecureCookies: true, Metrics: m,
	})
}

func getMetricsAs(h http.Handler, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// /metrics is hand-registered, so the credential policy.go gives every
// generated route is NOT applied by the table -- these are what stand in for it.
func TestMetricsIsOwnerOnly(t *testing.T) {
	h := metricsRouter(t, fakeMetrics{snap: metrics.Snapshot{Failed: map[string]error{}}})

	if rec := getMetricsAs(h, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401", rec.Code)
	}
	if rec := getMetricsAs(h, "chr_member"); rec.Code != http.StatusForbidden {
		t.Errorf("a non-owner member = %d, want 403", rec.Code)
	}
	rec := getMetricsAs(h, "chr_owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("owner = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type = %q, want the Prometheus text type", ct)
	}
	if !strings.Contains(rec.Body.String(), "chronicle_metrics_section_up") {
		t.Errorf("body carries no metrics:\n%s", rec.Body.String())
	}
}

func TestMetricsWithoutACollectorSays503NotEmpty(t *testing.T) {
	h := metricsRouter(t, nil)
	if rec := getMetricsAs(h, "chr_owner"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no collector = %d, want 503", rec.Code)
	}
}

func TestMetricsRefusesWhenThereIsNoCredentialSurface(t *testing.T) {
	h := NewRouter(Deps{DB: fakePinger{}, Logger: discardLogger(), Metrics: fakeMetrics{}})
	if rec := getMetricsAs(h, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("no accounts = %d, want 503 and never the exposition", rec.Code)
	}
}
