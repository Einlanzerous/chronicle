// Package store wires Chronicle to its Postgres backend: connection pooling
// and a small embedded migrator. Mirrors the construct-server house pattern —
// no ORM, no external migration tool.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx pool for dsn and verifies it is reachable with a Ping.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("store: parse dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return pool, nil
}

// ConnectWithRetry opens the pool, retrying transient failures with exponential
// backoff up to maxWait. A Postgres restart (recreating the shared container)
// is ridden out rather than crash-looping the process. A genuinely bad DSN
// still fails, just after the budget.
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
			return nil, fmt.Errorf("store: unreachable after %s: %w", maxWait, lastErr)
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
