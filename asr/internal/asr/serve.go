package asr

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
// A near-copy of Chronicle's internal/api.Serve, and NOT an import of it. That
// package reaches Chronicle's store, its audio layout and its upload service,
// so importing it here would make this service depend at compile time on the
// schema the whole tier split exists to keep it away from — for the sake of
// thirty lines that have no dependency beyond net/http.
//
// Graceful shutdown is not polish. This service holds transcriptions in flight,
// and while the lease makes a dropped one recoverable rather than lost, a
// redeploy that abandons every running job re-runs all of them on the GPU.
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
		logger.Error("graceful shutdown exceeded grace period; connections forced closed",
			"grace", grace.String(), "error", err)
		_ = srv.Close()
		return err
	}

	logger.Info("shutdown complete; no in-flight requests dropped")
	return <-errCh
}
