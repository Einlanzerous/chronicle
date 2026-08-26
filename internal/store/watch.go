package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The watcher's seen-ledger (CHRN-19), in tier 1.
//
// It answers one question — "have I already read the file at this path, as it
// is right now" — and it exists so that a rescan is not a re-delivery. CHRN-18
// §3: "an arrival row means a delivery, not a scan."

// SeenFile is one observed file identity and what it hashed to.
type SeenFile struct {
	Path        string
	SizeBytes   int64
	ModTime     time.Time
	ContentHash string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

// SeenIndex is the whole ledger, keyed by path. The watcher loads it once per
// scan rather than issuing a query per file: the inbox is a few thousand files
// at most and one round trip beats a few thousand.
type SeenIndex map[string]SeenFile

// Matches reports whether the file at path with this size and mtime is one the
// ledger has already read.
//
// All three must agree. mtime alone is too weak — a rewrite can preserve it —
// and re-hashing on every poll is the cost this ledger exists to avoid.
// Truncated to microseconds on both sides because that is Postgres's
// TIMESTAMPTZ resolution, and a nanosecond from the filesystem that the
// database rounded would make every file look new forever.
func (ix SeenIndex) Matches(path string, size int64, mtime time.Time) bool {
	got, ok := ix[path]
	if !ok {
		return false
	}
	return got.SizeBytes == size && got.ModTime.Truncate(time.Microsecond).Equal(mtime.Truncate(time.Microsecond))
}

// LoadSeen reads the whole ledger.
func (s *Store) LoadSeen(ctx context.Context) (SeenIndex, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT path, size_bytes, mtime, content_hash, first_seen_at, last_seen_at
		  FROM tier1.watch_seen`)
	if err != nil {
		return nil, fmt.Errorf("store: load seen: %w", err)
	}
	defer rows.Close()

	ix := SeenIndex{}
	for rows.Next() {
		var f SeenFile
		if err := rows.Scan(&f.Path, &f.SizeBytes, &f.ModTime, &f.ContentHash,
			&f.FirstSeenAt, &f.LastSeenAt); err != nil {
			return nil, fmt.Errorf("store: load seen: %w", err)
		}
		ix[f.Path] = f
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: load seen: %w", err)
	}
	return ix, nil
}

// MarkSeen records that the file at this path, with this size and mtime, has
// been read and hashed to this value.
//
// first_seen_at is preserved on conflict: it is when this path first delivered
// something, and a file that is rewritten in place has not stopped being the
// same path. last_seen_at moves.
func (s *Store) MarkSeen(ctx context.Context, f SeenFile) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tier1.watch_seen (path, size_bytes, mtime, content_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (path) DO UPDATE
		   SET size_bytes   = EXCLUDED.size_bytes,
		       mtime        = EXCLUDED.mtime,
		       content_hash = EXCLUDED.content_hash,
		       last_seen_at = now()`,
		f.Path, f.SizeBytes, f.ModTime, f.ContentHash)
	if err != nil {
		return fmt.Errorf("store: mark seen: %w", err)
	}
	return nil
}

// ForgetSeen drops one path from the ledger, so the next scan reads it again.
// The recovery lever: it costs a re-hash and nothing else, which is what makes
// this table disposable in the sense the tier split means.
func (s *Store) ForgetSeen(ctx context.Context, path string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM tier1.watch_seen WHERE path = $1`, path); err != nil {
		return fmt.Errorf("store: forget seen: %w", err)
	}
	return nil
}

// GetSeen reads one entry. ErrNotFound when the path is unknown.
func (s *Store) GetSeen(ctx context.Context, path string) (SeenFile, error) {
	var f SeenFile
	err := s.pool.QueryRow(ctx, `
		SELECT path, size_bytes, mtime, content_hash, first_seen_at, last_seen_at
		  FROM tier1.watch_seen WHERE path = $1`, path).
		Scan(&f.Path, &f.SizeBytes, &f.ModTime, &f.ContentHash, &f.FirstSeenAt, &f.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return SeenFile{}, ErrNotFound
	}
	if err != nil {
		return SeenFile{}, fmt.Errorf("store: get seen: %w", err)
	}
	return f, nil
}
