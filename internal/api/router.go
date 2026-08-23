// Package api is Chronicle's HTTP surface: the router every later endpoint
// hangs off, plus the two health probes the deploy path needs.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the slice of the store the readiness probe needs. An interface so
// readiness can be tested without a database.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps is what the router needs to serve.
type Deps struct {
	DB      Pinger
	Logger  *slog.Logger
	Version string
}

// NewRouter builds the HTTP handler.
//
// The two probes answer different questions and must not be collapsed:
//
//	/healthz — is this process alive? No dependencies. If it answers, the
//	           binary is running, and a restart is the wrong remedy for a
//	           database that is merely down.
//	/readyz  — can this process serve traffic? Pings the database. A load
//	           balancer takes an unready instance out of rotation; it does
//	           not kill it.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"version": d.Version,
		})
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if d.DB != nil {
			if err := d.DB.Ping(ctx); err != nil {
				d.Logger.Warn("readiness probe failed", "check", "database", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"status": "unready",
					"check":  "database",
				})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	return requestLogger(d.Logger, mux)
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLogger emits one structured line per request. Health probes log at
// debug so a 10-second liveness check does not bury everything else at info.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		switch {
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
			level = slog.LevelDebug
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		// Datadog's standard log attributes, so these map without a custom
		// pipeline: http.method / http.status_code / http.url, and `duration`
		// in NANOSECONDS, which is what Datadog documents that field to be.
		// Dozzle shows the raw JSON, which stays readable either way.
		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("http.method", r.Method),
			slog.String("http.url", r.URL.Path),
			slog.Int("http.status_code", rec.status),
			slog.Int64("duration", time.Since(start).Nanoseconds()),
		)
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
