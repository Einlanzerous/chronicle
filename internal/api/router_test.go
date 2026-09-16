package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/Einlanzerous/chronicle/web"
)

// TestWebAppRouting is CHRN-53's Done-when made mechanical for the router
// side: the app is served by the Go binary, mounted the way the decision
// comment on CHRN-53 describes -- outside policyRouter, after the generated
// registrations -- and the API still owns the root.
func TestWebAppRouting(t *testing.T) {
	spa := web.HandlerFS(fstest.MapFS{
		"index.html": {Data: []byte("<!doctype html><title>t</title>")},
	})

	h := NewRouter(Deps{
		DB:       fakePinger{},
		Accounts: newFakeAccounts(),
		Logger:   discardLogger(),
		Version:  "test",
		Web:      spa,
	})

	t.Run("GET / redirects to /app/", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		if rec.Code != 302 {
			t.Fatalf("status = %d, want 302", rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/app/" {
			t.Errorf("Location = %q, want /app/", loc)
		}
	})

	t.Run("GET /app/anything serves the SPA", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/anything", nil))
		if rec.Code != 200 {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	})

	t.Run("GET /notes/CHR-0001 still hits the API", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/notes/CHR-0001", nil))
		// Unauthenticated: the credential policy rejects before the handler
		// runs. The point of this case is what it is NOT -- 401 from the JSON
		// API, never the SPA's index.html -- proving the catch-all does not
		// shadow a documented route.
		if rec.Code != 401 {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json, not the SPA's HTML", ct)
		}
	})

	t.Run("GET /healthz is unchanged", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
		if rec.Code != 200 {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
	})
}

// TestWebAppDefaultsToProductionHandler proves NewRouter falls back to the
// real web.Handler() when Deps.Web is nil, i.e. that the wiring above is not
// only reachable through the test fixture. It does not assert WHICH of
// web.Handler()'s two answers comes back, because that depends on whether
// `bun run build` has run in this checkout: the checked-in placeholder
// (dist/.gitkeep) gives "web UI not built" (500), and a real local build
// gives the app (200) -- both are the DOCUMENTED behaviour of the same code
// path, and web/embed_test.go is where each is pinned down individually
// against a controlled fixture. This only proves the request reaches
// web.Handler() and comes back as one coherent answer or the other, never a
// panic or an unrelated 404.
func TestWebAppDefaultsToProductionHandler(t *testing.T) {
	h := NewRouter(Deps{
		DB:       fakePinger{},
		Accounts: newFakeAccounts(),
		Logger:   discardLogger(),
		Version:  "test",
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app/", nil))
	switch rec.Code {
	case http.StatusOK:
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Errorf("Content-Type = %q, want text/html", ct)
		}
	case http.StatusInternalServerError:
		if rec.Body.String() != "web UI not built\n" {
			t.Errorf("body = %q, want the clear not-built message", rec.Body.String())
		}
	default:
		t.Fatalf("status = %d, want 200 (real build present) or 500 (placeholder only)", rec.Code)
	}
}
