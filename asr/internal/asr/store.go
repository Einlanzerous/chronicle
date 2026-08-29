// Package asr is the estate's transcription service: the job contract from
// CHRN-25, its store, its HTTP surface, and the placeholder worker CHRN-26
// replaces.
//
// It is a SEPARATE SERVICE from Chronicle, sharing this repository until
// CHRN-29 decides whether to split it out. It has its own database, its own
// role, its own migrations and its own migrator, and it must never reach into
// Chronicle's store — Catenary is the second client, and the whole reason this
// is an estate service rather than a Chronicle package is that Catenary must
// not depend on Chronicle's schema.
package asr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned by every lookup that resolves nothing.
//
// It is also what a client is given for ANOTHER CLIENT'S JOB, and the handler
// turns it into a 404 rather than a 403 — CHRN-71's precedent, for its reason:
// a 403 confirms that the id exists.
var ErrNotFound = errors.New("asr: not found")

// ErrKeyMismatch reports that an Idempotency-Key was reused for a different
// spec or different audio. The handler answers 409; the client's move is to
// mint a fresh key, which produces a second job — the correct outcome, because
// it asked for a different thing.
var ErrKeyMismatch = errors.New("asr: idempotency key was used for a different request")

// ErrNotTerminal reports that a result was asked for before the job finished.
var ErrNotTerminal = errors.New("asr: job has not reached a terminal state")

// ErrResultPurged reports that the job is terminal but its result payload has
// aged out. The JOB ROW survives that purge, which is the whole reason this is
// a distinct error: the handler answers 410 Gone rather than 404, so a client
// that comes back late learns its result expired rather than that its job never
// existed. "Result expired" is not "transcription failed".
var ErrResultPurged = errors.New("asr: result payload has been purged")

// Store is the query surface over the ASR database's pool.
type Store struct {
	pool *pgxpool.Pool

	// backend names what produced a transcript — "vulkan" on the R9700. It is
	// recorded on every result because a corpus transcribed by two different
	// backends over time is one whose quality varies invisibly.
	backend string

	// resultTTL is how long a result PAYLOAD is kept. The job row outlives it,
	// which is what lets a late fetch answer 410 rather than 404.
	resultTTL time.Duration
}

// New wraps an existing pool.
func New(pool *pgxpool.Pool, backend string, resultTTL time.Duration) *Store {
	return &Store{pool: pool, backend: backend, resultTTL: resultTTL}
}

// Pool exposes the underlying pool for the migrator and for tests.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Ping satisfies the readiness probe.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Connect opens a pgx pool for dsn and verifies it is reachable.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("asr: parse dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("asr: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("asr: ping: %w", err)
	}
	return pool, nil
}

// ConnectWithRetry opens the pool, retrying transient failures with exponential
// backoff up to maxWait, so that a Postgres restart is ridden out rather than
// crash-looping the process.
func ConnectWithRetry(ctx context.Context, dsn string, maxWait time.Duration) (*pgxpool.Pool, error) {
	deadline := time.Now().Add(maxWait)
	backoff := 250 * time.Millisecond
	var lastErr error
	for {
		pool, err := Connect(ctx, dsn)
		if err == nil {
			return pool, nil
		}
		lastErr = err
		if time.Now().Add(backoff).After(deadline) {
			return nil, fmt.Errorf("asr: unreachable after %s: %w", maxWait, lastErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
}
