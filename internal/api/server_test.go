package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestServeDrainsInFlightRequest is the Done-when for CHRN-15: SIGTERM must not
// drop work already in hand. A request is in a handler when shutdown is
// signalled; it must still get its response.
func TestServeDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(300 * time.Millisecond) // work in flight
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("finished"))
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	url := "http://" + ln.Addr().String() + "/slow"

	ctx, cancel := context.WithCancel(context.Background())
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- ServeListener(ctx, &http.Server{Handler: mux}, ln, 5*time.Second, discardLogger())
	}()

	respCh := make(chan *http.Response, 1)
	getErr := make(chan error, 1)
	go func() {
		resp, err := http.Get(url)
		if err != nil {
			getErr <- err
			return
		}
		respCh <- resp
	}()

	// Only signal shutdown once the handler is genuinely executing.
	select {
	case <-started:
	case err := <-getErr:
		t.Fatalf("request failed before handler ran: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()

	select {
	case err := <-getErr:
		t.Fatalf("in-flight request was dropped: %v", err)
	case resp := <-respCh:
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != "finished" {
			t.Errorf("body = %q, want %q", body, "finished")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight request never completed")
	}

	select {
	case err := <-srvErr:
		if err != nil {
			t.Errorf("Serve returned %v, want nil on a clean drain", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
}

// TestServeReportsWhenGraceExpires: if in-flight work overruns the grace
// period the connection IS dropped, and that must surface as an error rather
// than pass for a clean shutdown.
func TestServeReportsWhenGraceExpires(t *testing.T) {
	started := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/forever", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(3 * time.Second)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	url := "http://" + ln.Addr().String() + "/forever"

	ctx, cancel := context.WithCancel(context.Background())
	srvErr := make(chan error, 1)
	go func() {
		srvErr <- ServeListener(ctx, &http.Server{Handler: mux}, ln, 100*time.Millisecond, discardLogger())
	}()
	go func() {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started")
	}
	cancel()

	select {
	case err := <-srvErr:
		if err == nil {
			t.Error("Serve returned nil, want an error when the grace period expired with work in flight")
		} else if !errors.Is(err, context.DeadlineExceeded) {
			t.Logf("grace-expiry error (acceptable): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after grace expiry")
	}
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestHealthzIsDependencyFree(t *testing.T) {
	h := NewRouter(Deps{DB: fakePinger{err: errors.New("db is down")}, Logger: discardLogger(), Version: "test"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	// Liveness must not depend on the database: restarting the process is the
	// wrong remedy for Postgres being down.
	if rec.Code != http.StatusOK {
		t.Errorf("healthz with a dead database = %d, want 200", rec.Code)
	}
}

func TestReadyzFollowsTheDatabase(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"database up", nil, http.StatusOK},
		{"database down", errors.New("connection refused"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewRouter(Deps{DB: fakePinger{err: tc.err}, Logger: discardLogger(), Version: "test"})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tc.want {
				t.Errorf("readyz = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
