// Package asrtest is the ONE exported doorway into the asr/ subtree.
//
// Everything else under asr/ is internal, and the subtree imports nothing
// outside itself (CHRN-82 §2), so that it can become its own repository by a
// filter-repo. The direction here is the permitted one: Chronicle — client one,
// which happens to live beside the service — runs the real service in-process
// against its generated client, so that the only thing standing between the
// two is asr/openapi.yaml. That test exists BECAUSE both halves share a
// repository; it is a benefit of the arrangement, and this package is what
// keeps it running across the boundary without making a hole in it.
//
// It is a test harness and nothing else. A non-test importer of this package
// is a boundary violation with extra steps, and verify.sh's inward check would
// let it through; the name is the guard.
package asrtest

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Einlanzerous/chronicle/asr/internal/asr"
	"github.com/Einlanzerous/chronicle/asr/internal/wire"
)

// The types a caller-supplied Transcriber has to speak, re-exported as aliases
// so a stub outside the subtree can implement the internal interface. Aliases,
// not copies: they ARE the service's types.
type (
	Transcriber       = asr.Transcriber
	TranscribeRequest = asr.TranscribeRequest
	Transcript        = asr.Transcript
	Segment           = wire.Segment
)

// Options is what Start needs. DSN, Transcriber and Tokens are required; the
// service refuses an empty token set at boot and the harness mirrors that
// rather than quietly opening the surface.
type Options struct {
	DSN         string
	Transcriber Transcriber
	// Tokens maps bearer token -> client_id, exactly as ASR_CLIENT_TOKENS does.
	Tokens map[string]string
	// DefaultModel defaults to small.en, the deployment default.
	DefaultModel string
	// MaxAudioBytes defaults to 1 MiB, which is plenty for a test body.
	MaxAudioBytes int64
	// Logger defaults to discarding everything below ERROR.
	Logger *slog.Logger
}

// Server is a running service: its URL, and its database for assertions about
// the rows it holds — which matter, because nothing in that service may become
// the only copy of anything.
type Server struct {
	URL  string
	Pool *pgxpool.Pool
}

// Start connects to the service's database, migrates it, empties the job
// table, serves the API on an httptest.Server and runs one worker around the
// supplied Transcriber. Everything is torn down on t.Cleanup, worker first.
func Start(t testing.TB, ctx context.Context, o Options) *Server {
	t.Helper()
	if o.DSN == "" || o.Transcriber == nil || len(o.Tokens) == 0 {
		t.Fatal("asrtest.Start: DSN, Transcriber and Tokens are required")
	}
	if o.DefaultModel == "" {
		o.DefaultModel = "small.en"
	}
	if o.MaxAudioBytes == 0 {
		o.MaxAudioBytes = 1 << 20
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	pool, err := asr.Connect(ctx, o.DSN)
	if err != nil {
		t.Fatalf("asrtest: connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := asr.Migrate(ctx, pool); err != nil {
		t.Fatalf("asrtest: migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE jobs`); err != nil {
		t.Fatalf("asrtest: truncate: %v", err)
	}
	store := asr.New(pool, "vulkan", time.Hour)

	srv := httptest.NewServer(asr.NewRouter(asr.Deps{
		Store:         store,
		Transcriber:   o.Transcriber,
		Logger:        o.Logger,
		Tokens:        o.Tokens,
		DefaultModel:  o.DefaultModel,
		MaxAudioBytes: o.MaxAudioBytes,
	}))
	t.Cleanup(srv.Close)

	worker := &asr.Worker{
		Store: store, Transcriber: o.Transcriber, Logger: o.Logger,
		ID: "asrtest", LeaseTTL: 30 * time.Second, Idle: 50 * time.Millisecond,
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = worker.Run(workerCtx)
	}()
	// Registered last so it runs FIRST: a worker still claiming jobs while the
	// pool closes under it produces errors that read as test failures and are
	// not.
	t.Cleanup(func() { stopWorker(); <-done })

	return &Server{URL: srv.URL, Pool: pool}
}
