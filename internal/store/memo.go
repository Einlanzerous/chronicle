package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// CHRN-18 — the memo, and the rule that makes it one row whichever path it
// arrives by. The decision, and the reasoning this file only summarises, is in
// docs/decisions/chrn-18-memo-model-and-idempotency.md.
//
// Identity is the SHA-256 of the bytes as they arrived, scoped to the author.
// The client's idempotency key names an ATTEMPT rather than a memo: it is the
// only handle that exists before the last byte lands, and the hash is the only
// one that catches the same recording arriving by both paths. Both are needed
// and they answer different questions.

// Memo states. 'discarded' is terminal and appears only as a transition target;
// the edges themselves live in tier2.memos_guard, because Go is not the only
// thing that will ever hold a connection to this database.
const (
	StateCaptured     = "captured"
	StateQueued       = "queued"
	StateTranscribing = "transcribing"
	StateTranscribed  = "transcribed"
	StateTriaged      = "triaged"
	StateHeld         = "held"
	StateDiscarded    = "discarded"
)

// Retention levels, in ascending order of how long the audio survives. An
// arrival may only ever raise this — see the ratchet in IngestMemo.
const (
	RetentionDiscardNow = "discard_now"
	RetentionDays30     = "days_30"
	RetentionForever    = "forever"
)

// Arrival sources. Copyparty is the watched folder a sync client delivers to;
// upload is the Chronicle app posting directly.
const (
	SourceCopyparty = "copyparty"
	SourceUpload    = "upload"
)

// Postgres SQLSTATEs this file maps. The CH0xx codes are raised by
// tier2.memos_guard; they are distinct so a caller can tell which rule it broke
// rather than reading three different failures as one check_violation.
const (
	pgForeignKeyViolation = "23503"
	pgIllegalTransition   = "CH001"
	pgMemoImmutable       = "CH002"
	pgMemoBadInitialState = "CH003"
)

// ErrKeyReused is returned when an idempotency key is presented again with
// different bytes behind it. It is a client bug — a key recycled across
// recordings — and the honest answer is a refusal, because the alternative is
// silently returning the wrong memo. A 409 tells the client to mint a fresh key.
var ErrKeyReused = errors.New("store: idempotency key already used for different content")

// ErrAuthorHasMemos is returned when deleting an account the corpus references.
// Removing a person who has recorded memos is a conversation about their
// corpus, not a button.
var ErrAuthorHasMemos = errors.New("store: account has memos and cannot be removed")

// ErrIllegalTransition and ErrMemoImmutable surface the guard's refusals as
// typed errors so a handler can answer 409 rather than 500.
var (
	ErrIllegalTransition = errors.New("store: illegal memo state transition")
	ErrMemoImmutable     = errors.New("store: memo identity and captured_at are immutable")
)

// Memo is one recording. The audio behind it may be gone — audio_pruned_at set
// by CHRN-22 — while the memo and its transcript remain: that asymmetry is the
// whole retention design.
type Memo struct {
	ID          uuid.UUID
	AuthorID    uuid.UUID
	ContentHash string
	ByteSize    int64
	CapturedAt  time.Time
	State       string
	StateReason *string

	Retention     string
	AudioPrunedAt *time.Time

	// Filled by CHRN-21 after normalisation; nil until it has run.
	DurationMS   *int32
	Codec        *string
	SampleRateHz *int32

	OriginalFilename *string

	CreatedAt time.Time
	UpdatedAt time.Time
}

// AudioPruned reports whether the audio has been deleted. Expressed as a
// timestamp rather than a nulled path because there is no path column: CHRN-23
// derives the path from content_hash, which is immutable.
func (m Memo) AudioPruned() bool { return m.AudioPrunedAt != nil }

// Arrival is one delivery of one recording. Both ingest paths build this; the
// fields they leave empty are what distinguishes them.
type Arrival struct {
	AuthorID    uuid.UUID
	ContentHash string
	ByteSize    int64
	Source      string

	// IdempotencyKey is set by the upload path and empty for the watcher, which
	// has no way to mint one. Empty means "identify me by hash alone".
	IdempotencyKey string
	// SourceRef is the watched path, or an upload session id. Required on the
	// copyparty side: it is that path's only handle.
	SourceRef string

	// Retention carries a capture-time choice, and is empty when the path has
	// no opinion. Empty is NOT days_30 — see the ratchet. The watcher always
	// leaves this empty, because a default arriving from a rescan must never
	// outrank something a person chose.
	Retention string

	OriginalFilename string
}

// IngestResult is what one delivery produced.
type IngestResult struct {
	Memo Memo
	// Deliveries is how many arrivals this memo now has. More than one means
	// this delivery collapsed into an existing memo rather than creating one.
	Deliveries int
}

// Duplicate reports whether this delivery was a repeat of one already recorded.
func (r IngestResult) Duplicate() bool { return r.Deliveries > 1 }

const memoColumns = `id, author_id, content_hash, byte_size, captured_at,
	state, state_reason, retention, audio_pruned_at,
	duration_ms, codec, sample_rate_hz, original_filename,
	created_at, updated_at`

func scanMemo(row pgx.Row) (Memo, error) {
	var m Memo
	err := row.Scan(&m.ID, &m.AuthorID, &m.ContentHash, &m.ByteSize, &m.CapturedAt,
		&m.State, &m.StateReason, &m.Retention, &m.AudioPrunedAt,
		&m.DurationMS, &m.Codec, &m.SampleRateHz, &m.OriginalFilename,
		&m.CreatedAt, &m.UpdatedAt)
	return m, err
}

// nullable turns an empty string into a SQL NULL. The distinction matters for
// Retention, where "" means "no opinion" and must not become a default that
// competes with an authored choice.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// IngestMemo records one delivery and returns the memo it belongs to, creating
// that memo only if these exact bytes have not been seen from this author
// before. It is safe to call repeatedly with the same arrival: that is the
// point of it.
//
// The whole thing is one transaction, and it relies on READ COMMITTED — the
// Postgres default. Each statement takes a fresh snapshot, which is what lets
// the lookup below see an arrival a competing transaction committed while this
// one was waiting on the advisory lock. Under REPEATABLE READ the lookup would
// read a snapshot older than that commit and the second caller would fail.
func (s *Store) IngestMemo(ctx context.Context, in Arrival) (IngestResult, error) {
	if in.AuthorID == uuid.Nil {
		return IngestResult{}, fmt.Errorf("%w: arrival has no author", ErrInvalidInput)
	}
	if in.Source != SourceCopyparty && in.Source != SourceUpload {
		return IngestResult{}, fmt.Errorf("%w: unknown arrival source %q", ErrInvalidInput, in.Source)
	}
	if in.Retention != "" &&
		in.Retention != RetentionDiscardNow &&
		in.Retention != RetentionDays30 &&
		in.Retention != RetentionForever {
		return IngestResult{}, fmt.Errorf("%w: unknown retention %q", ErrInvalidInput, in.Retention)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return IngestResult{}, fmt.Errorf("store: ingest memo: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if in.IdempotencyKey != "" {
		// Serialise same-key arrivals BEFORE the lookup. Without this, two
		// concurrent retries of one key both miss the lookup, both reach the
		// upsert (correctly getting the same memo), and then the loser's
		// arrival insert hits memo_arrivals_key and returns an error instead of
		// a memo — which is precisely the failure "retries are free" names.
		// A phone flushing a queue over flaky mobile data does this routinely.
		if _, err := tx.Exec(ctx,
			`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`,
			in.AuthorID.String()+":"+in.IdempotencyKey); err != nil {
			return IngestResult{}, fmt.Errorf("store: ingest memo: lock key: %w", err)
		}

		var memoID uuid.UUID
		var seenHash string
		err := tx.QueryRow(ctx, `
			SELECT a.memo_id, m.content_hash
			  FROM tier2.memo_arrivals a
			  JOIN tier2.memos m ON m.id = a.memo_id
			 WHERE a.author_id = $1 AND a.idempotency_key = $2`,
			in.AuthorID, in.IdempotencyKey).Scan(&memoID, &seenHash)
		switch {
		case err == nil && seenHash != in.ContentHash:
			return IngestResult{}, ErrKeyReused
		case err == nil:
			// A retry of a delivery already recorded. Write nothing at all —
			// no arrival row, no state change, not even an updated_at bump.
			res, err := loadResult(ctx, tx, memoID)
			if err != nil {
				return IngestResult{}, err
			}
			if err := tx.Commit(ctx); err != nil {
				return IngestResult{}, fmt.Errorf("store: ingest memo: %w", err)
			}
			return res, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return IngestResult{}, fmt.Errorf("store: ingest memo: lookup key: %w", err)
		}
	}

	// The ratchet. An arrival may raise retention and never lower it, so a
	// re-upload carrying the default cannot undo a FOREVER pin set afterwards
	// in the UI, and a rescan carrying no opinion cannot lift an authored
	// DISCARD NOW. The WHERE references $4 rather than EXCLUDED.retention
	// deliberately: EXCLUDED carries the COALESCE'd value, so a no-opinion
	// arrival would present itself as days_30 and ratchet a discard_now up.
	//
	// When the conflict resolves to no update, RETURNING is empty — which is
	// the common re-delivery case and exactly what is wanted: no write, no
	// trigger, no updated_at bump.
	retention := nullable(in.Retention)
	var memoID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO tier2.memos (author_id, content_hash, byte_size, retention, original_filename)
		VALUES ($1, $2, $3, COALESCE($4, 'days_30'), $5)
		ON CONFLICT (author_id, content_hash) DO UPDATE
		   SET retention = $4
		 WHERE $4 IS NOT NULL
		   AND tier2.memos.state <> 'discarded'
		   AND tier2.retention_rank($4) > tier2.retention_rank(tier2.memos.retention)
		RETURNING id`,
		in.AuthorID, in.ContentHash, in.ByteSize, retention, nullable(in.OriginalFilename)).Scan(&memoID)
	if errors.Is(err, pgx.ErrNoRows) {
		// No update fired. Safe to read the row plainly: ON CONFLICT has
		// already waited out any competing transaction, so it is committed
		// and visible to this statement's snapshot.
		err = tx.QueryRow(ctx,
			`SELECT id FROM tier2.memos WHERE author_id = $1 AND content_hash = $2`,
			in.AuthorID, in.ContentHash).Scan(&memoID)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation {
			return IngestResult{}, fmt.Errorf("%w: no such author", ErrInvalidInput)
		}
		return IngestResult{}, fmt.Errorf("store: ingest memo: upsert: %w", err)
	}

	// Record the delivery. The ON CONFLICT targets the sighting index only, so
	// a repeated sighting of one file is absorbed while a repeated KEY still
	// raises — with the advisory lock held, one reaching here is a bug rather
	// than a race, and swallowing it would hide that.
	if _, err := tx.Exec(ctx, `
		INSERT INTO tier2.memo_arrivals (memo_id, author_id, source, idempotency_key, source_ref)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (memo_id, source, source_ref) WHERE idempotency_key IS NULL DO NOTHING`,
		memoID, in.AuthorID, in.Source, nullable(in.IdempotencyKey), nullable(in.SourceRef)); err != nil {
		return IngestResult{}, fmt.Errorf("store: ingest memo: record arrival: %w", err)
	}

	res, err := loadResult(ctx, tx, memoID)
	if err != nil {
		return IngestResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return IngestResult{}, fmt.Errorf("store: ingest memo: %w", err)
	}
	return res, nil
}

// loadResult reads the memo and counts its deliveries. The count is the
// definition of "was this a duplicate" rather than a proxy for it, which is why
// it is preferred to inspecting whether the upsert inserted.
func loadResult(ctx context.Context, tx pgx.Tx, memoID uuid.UUID) (IngestResult, error) {
	m, err := scanMemo(tx.QueryRow(ctx,
		`SELECT `+memoColumns+` FROM tier2.memos WHERE id = $1`, memoID))
	if err != nil {
		return IngestResult{}, fmt.Errorf("store: ingest memo: read back: %w", err)
	}
	var n int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM tier2.memo_arrivals WHERE memo_id = $1`, memoID).Scan(&n); err != nil {
		return IngestResult{}, fmt.Errorf("store: ingest memo: count arrivals: %w", err)
	}
	return IngestResult{Memo: m, Deliveries: n}, nil
}

// GetMemo returns one memo by id.
func (s *Store) GetMemo(ctx context.Context, id uuid.UUID) (Memo, error) {
	m, err := scanMemo(s.pool.QueryRow(ctx,
		`SELECT `+memoColumns+` FROM tier2.memos WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Memo{}, ErrNotFound
	}
	if err != nil {
		return Memo{}, fmt.Errorf("store: get memo: %w", err)
	}
	return m, nil
}

// AdvanceMemoState moves a memo along the state machine. This is the only write
// path for memos.state in Go — the edges themselves are enforced by the trigger,
// so this exists to give the refusal a typed error rather than to be the check.
// reason is stored for the states that want one ('held', a failed decode) and
// may be empty.
func (s *Store) AdvanceMemoState(ctx context.Context, id uuid.UUID, to, reason string) (Memo, error) {
	m, err := scanMemo(s.pool.QueryRow(ctx, `
		UPDATE tier2.memos
		   SET state = $2, state_reason = COALESCE($3, state_reason)
		 WHERE id = $1
		RETURNING `+memoColumns, id, to, nullable(reason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Memo{}, ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgIllegalTransition:
			return Memo{}, fmt.Errorf("%w: %s", ErrIllegalTransition, pgErr.Message)
		case pgMemoImmutable:
			return Memo{}, fmt.Errorf("%w: %s", ErrMemoImmutable, pgErr.Message)
		}
	}
	if err != nil {
		return Memo{}, fmt.Errorf("store: advance memo state: %w", err)
	}
	return m, nil
}
