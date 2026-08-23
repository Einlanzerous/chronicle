package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Serve runs srv until ctx is cancelled, then shuts it down gracefully.
//
// Graceful shutdown is not polish here. E2's ingest and E3's transcription both
// hold work in flight, and a service that drops in-flight work on redeploy
// loses memos — the one thing this system is not allowed to do. On SIGTERM the
// listener closes immediately (no new connections) while in-flight handlers are
// given `grace` to finish. Only if they overrun does the process force the
// connections closed, and that case is logged as an error rather than passing
// silently.
//
// Returns nil on a clean shutdown, including when in-flight work finished
// early; returns an error if the listener failed or the grace period expired.
func Serve(ctx context.Context, srv *http.Server, grace time.Duration, logger *slog.Logger) error {
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}
	return ServeListener(ctx, srv, ln, grace, logger)
}

// ServeListener is Serve on an already-open listener. Tests use it so they can
// bind :0 and still learn the port.
func ServeListener(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	logger.Info("listening", "addr", ln.Addr().String())

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutdown signalled; draining in-flight requests", "grace", grace.String())
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grace)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Shutdown returns the context error when the grace period expires
		// with connections still open. That means work WAS dropped, so it is
		// an error and not a routine event.
		logger.Error("graceful shutdown exceeded grace period; connections forced closed",
			"grace", grace.String(), "error", err)
		_ = srv.Close()
		return err
	}

	logger.Info("shutdown complete; no in-flight requests dropped")
	return <-errCh
}
