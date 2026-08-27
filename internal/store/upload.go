package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Uploads in flight (CHRN-20), in tier 1.
//
// One row per session, holding what the client DECLARED it is about to send.
// The bytes themselves live in a staging file whose size is the offset — see
// internal/upload for why the offset is deliberately not a column here.
//
// Tier 1 because a partial upload is regenerable from a source of truth outside
// Chronicle: the phone still holds the recording until the memo is
// acknowledged. Losing this table costs bandwidth, not data. 0005 makes the
// argument in full.

// ErrUploadKeyReused is returned when an idempotency key is presented again
// with a different declaration behind it — a different hash, or a different
// length.
//
// It is the same client bug ErrKeyReused names at ingest, caught one step
// earlier: a key recycled across recordings. Answering it here rather than
// silently opening a second session is what stops a resume attaching itself to
// the wrong file, which would then fail its hash check after a full transfer
// instead of at the first request.
var ErrUploadKeyReused = errors.New("store: upload key already used for a different declaration")

// Upload is one upload session.
type Upload struct {
	ID             uuid.UUID
	AuthorID       uuid.UUID
	IdempotencyKey string

	// ContentHash and ByteSize are what the client says it is sending. Nothing
	// in this package verifies them; internal/upload hashes what actually
	// arrived and refuses to commit a memo when the two disagree.
	ContentHash string
	ByteSize    int64

	// Retention is the capture-time choice, empty when the client had no
	// opinion. Empty is NOT days_30 — CHRN-18's ratchet distinguishes them.
	Retention        string
	OriginalFilename string

	CreatedAt      time.Time
	LastActivityAt time.Time
}

const uploadColumns = `id, author_id, idempotency_key, content_hash, byte_size,
	retention, original_filename, created_at, last_activity_at`

func scanUpload(row pgx.Row) (Upload, error) {
	var u Upload
	var retention, filename *string
	err := row.Scan(&u.ID, &u.AuthorID, &u.IdempotencyKey, &u.ContentHash, &u.ByteSize,
		&retention, &filename, &u.CreatedAt, &u.LastActivityAt)
	if retention != nil {
		u.Retention = *retention
	}
	if filename != nil {
		u.OriginalFilename = *filename
	}
	return u, err
}

// OpenUpload starts an upload session, or returns the one this author already
// has open under the same idempotency key.
//
// created reports which happened, and the caller needs to know: a fresh session
// starts at offset zero, an existing one starts wherever its staging file got
// to. **That is the whole resume mechanism from the client's side** — a phone
// that lost its upload id (a process restart, a reinstall, a crash) presents the
// key it still has and gets its half-written upload back rather than a second
// one beside it.
//
// A key presented again with a different hash or length is refused rather than
// resolved. The alternative is attaching a resume to the wrong file and
// discovering it at the hash check, after the client has spent a whole transfer
// on it.
func (s *Store) OpenUpload(ctx context.Context, in Upload) (u Upload, created bool, err error) {
	if in.AuthorID == uuid.Nil {
		return Upload{}, false, fmt.Errorf("%w: upload has no author", ErrInvalidInput)
	}
	if l := len(in.IdempotencyKey); l < 16 || l > 200 {
		return Upload{}, false, fmt.Errorf("%w: idempotency key must be 16 to 200 characters, got %d", ErrInvalidInput, l)
	}
	if !contentHashPattern.MatchString(in.ContentHash) {
		return Upload{}, false, fmt.Errorf("%w: content hash must be 64 lowercase hex characters", ErrInvalidInput)
	}
	if in.ByteSize <= 0 {
		return Upload{}, false, fmt.Errorf("%w: byte size must be positive, got %d", ErrInvalidInput, in.ByteSize)
	}
	if in.Retention != "" &&
		in.Retention != RetentionDiscardNow &&
		in.Retention != RetentionDays30 &&
		in.Retention != RetentionForever {
		return Upload{}, false, fmt.Errorf("%w: unknown retention %q", ErrInvalidInput, in.Retention)
	}

	u, err = scanUpload(s.pool.QueryRow(ctx, `
		INSERT INTO tier1.memo_uploads
		       (author_id, idempotency_key, content_hash, byte_size, retention, original_filename)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (author_id, idempotency_key) DO NOTHING
		RETURNING `+uploadColumns,
		in.AuthorID, in.IdempotencyKey, in.ContentHash, in.ByteSize,
		nullable(in.Retention), nullable(in.OriginalFilename)))
	if err == nil {
		return u, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Upload{}, false, fmt.Errorf("store: open upload: %w", err)
	}

	// DO NOTHING fired: this author already has a session under this key.
	u, err = scanUpload(s.pool.QueryRow(ctx,
		`SELECT `+uploadColumns+`
		   FROM tier1.memo_uploads WHERE author_id = $1 AND idempotency_key = $2`,
		in.AuthorID, in.IdempotencyKey))
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted between the insert and this read — a finalise or a sweep
		// landing in the gap. Rare, and the honest answer is "try again"
		// rather than inventing a session: a retry takes the insert branch.
		return Upload{}, false, fmt.Errorf("store: open upload: session vanished mid-open, retry")
	}
	if err != nil {
		return Upload{}, false, fmt.Errorf("store: open upload: %w", err)
	}
	if u.ContentHash != in.ContentHash || u.ByteSize != in.ByteSize {
		return Upload{}, false, ErrUploadKeyReused
	}
	// The stored declaration wins, including its retention. A resume is a
	// continuation of one attempt, not a chance to redeclare it — and retention
	// is not lost by that, because CHRN-18's ratchet at ingest is where the
	// level is actually decided and it can only ever be raised afterwards.
	return u, false, nil
}

// GetUpload reads one session by id. ErrNotFound when it is unknown.
func (s *Store) GetUpload(ctx context.Context, id uuid.UUID) (Upload, error) {
	u, err := scanUpload(s.pool.QueryRow(ctx,
		`SELECT `+uploadColumns+` FROM tier1.memo_uploads WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Upload{}, ErrNotFound
	}
	if err != nil {
		return Upload{}, fmt.Errorf("store: get upload: %w", err)
	}
	return u, nil
}

// TouchUpload records that a session made progress.
//
// Expiry is measured from last activity and not from creation, so a genuinely
// slow upload — a long recording over poor mobile data, which is the case this
// endpoint exists for — is never swept out from under itself. What is being
// bounded is abandonment, not duration.
func (s *Store) TouchUpload(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier1.memo_uploads SET last_activity_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("store: touch upload: %w", err)
	}
	return nil
}

// DeleteUpload removes a session by id. Callers: the client abandoning one, and
// the sweep. A finished upload releases itself through ClearUploadKey instead.
func (s *Store) DeleteUpload(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM tier1.memo_uploads WHERE id = $1`, id); err != nil {
		return fmt.Errorf("store: delete upload: %w", err)
	}
	return nil
}

// ClearUploadKey removes whatever session this author holds under this key, if
// any. It is how a finished upload releases its key.
//
// By key rather than by id, because the two callers do not both have an id. A
// finalise does; the already-held shortcut does not — it answers before a
// session is ever looked up, and the row it needs to clear is one left behind by
// an earlier attempt that renamed its bytes into place and then failed before
// recording the memo. Deleting by key covers both with one statement, and for
// the finalise it names exactly the same row.
func (s *Store) ClearUploadKey(ctx context.Context, authorID uuid.UUID, key string) error {
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM tier1.memo_uploads WHERE author_id = $1 AND idempotency_key = $2`,
		authorID, key); err != nil {
		return fmt.Errorf("store: clear upload key: %w", err)
	}
	return nil
}

// CountOpenUploads is how many sessions this author has in flight. It bounds
// the staging directory: each open session can hold up to its declared size on
// disk, and nothing else limits how many an authenticated client may start.
func (s *Store) CountOpenUploads(ctx context.Context, authorID uuid.UUID) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM tier1.memo_uploads WHERE author_id = $1`, authorID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count open uploads: %w", err)
	}
	return n, nil
}

// StaleUploads lists sessions with no activity since `before`.
func (s *Store) StaleUploads(ctx context.Context, before time.Time) ([]Upload, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+uploadColumns+`
		   FROM tier1.memo_uploads WHERE last_activity_at < $1 ORDER BY last_activity_at`, before)
	if err != nil {
		return nil, fmt.Errorf("store: stale uploads: %w", err)
	}
	defer rows.Close()

	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, fmt.Errorf("store: stale uploads: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: stale uploads: %w", err)
	}
	return out, nil
}

// LiveUploadIDs is every session id currently on record.
//
// The sweep needs it to answer the other direction: a staging file whose
// session row is gone is unreachable — no request can name it, because the id
// is how a request names anything — so it is disk nothing will ever claim.
// Deleting it needs the whole set, not a per-file lookup, because the absence
// is the finding.
func (s *Store) LiveUploadIDs(ctx context.Context) (map[uuid.UUID]struct{}, error) {
	rows, err := s.pool.Query(ctx, `SELECT id FROM tier1.memo_uploads`)
	if err != nil {
		return nil, fmt.Errorf("store: live upload ids: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: live upload ids: %w", err)
		}
		out[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: live upload ids: %w", err)
	}
	return out, nil
}
