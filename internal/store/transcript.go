package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The transcript, and the one query CHRN-22 is allowed to gate audio deletion
// on. Tier 2: the transcript OUTLIVES the audio, and once CHRN-22 has pruned
// that audio at thirty days nothing regenerates it.

// SQLSTATEs raised by tier2.transcripts_guard.
const (
	pgTranscriptReattributed = "CH004"
	pgTranscriptDowngraded   = "CH005"
)

// ErrTranscriptDowngrade reports an attempt to replace a complete transcript
// with a partial one, or to rewrite a complete one in place. Refused by the
// database rather than by convention, so that a retry policy (CHRN-28) cannot
// downgrade a good transcript by re-collecting a bad one.
var ErrTranscriptDowngrade = errors.New("store: a complete transcript may not be replaced by a partial one")

// Segment is one span of speech.
//
// Deliberately NOT internal/asrclient's generated type. The store should not
// know the wire format: the pump converts once, at the boundary, so that a
// change to deploy/asr/openapi.yaml cannot reach into Chronicle's schema.
type Segment struct {
	StartMS int64  `json:"start_ms"`
	EndMS   int64  `json:"end_ms"`
	Text    string `json:"text"`
}

// Transcript is what a memo said.
type Transcript struct {
	ID     uuid.UUID
	MemoID uuid.UUID

	// Text is never nil and MAY BE EMPTY. A memo that is forty seconds of
	// silence has a true and complete answer, and the answer is "no speech".
	Text     string
	Segments []Segment

	// Partial is a fact the ASR SERVICE recorded about whether its own run
	// completed, carried across the boundary unchanged. It is never computed
	// here, and never from CoveredMS against AudioDurationMS.
	Partial bool

	Model   string
	Backend string

	// Evidence, not predicates. CoveredMS is short of the duration on any
	// recording that ends in silence, which is most of them.
	AudioDurationMS *int64
	CoveredMS       *int64

	TranscribedAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Durable reports whether this transcript satisfies CHRN-25 §5.
//
// The predicate is `succeeded AND NOT partial`, and the `succeeded` half is
// already spent: a row exists here only because a run reached `succeeded`, and
// internal/transcribe writes one for nothing else. So what is left to test is
// the other half.
//
// EMPTY TEXT IS DURABLE. That is the ruling, and it is the one this method
// exists to stop anyone re-deciding: `len(t.Text) > 0` looks like an obvious
// improvement and would strand the audio of exactly the memos that should
// prune, silently, in the direction nobody checks.
func (t Transcript) Durable() bool { return !t.Partial }

const transcriptColumns = `id, memo_id, text, segments, partial, model, backend,
	audio_duration_ms, covered_ms, transcribed_at, created_at, updated_at`

func scanTranscript(row pgx.Row) (Transcript, error) {
	var t Transcript
	var segments []byte
	err := row.Scan(&t.ID, &t.MemoID, &t.Text, &segments, &t.Partial, &t.Model,
		&t.Backend, &t.AudioDurationMS, &t.CoveredMS, &t.TranscribedAt,
		&t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transcript{}, ErrNotFound
	}
	if err != nil {
		return Transcript{}, err
	}
	if err := json.Unmarshal(segments, &t.Segments); err != nil {
		return Transcript{}, fmt.Errorf("store: decode segments: %w", err)
	}
	if t.Segments == nil {
		t.Segments = []Segment{}
	}
	return t, nil
}

// TranscriptInput is one collected result.
type TranscriptInput struct {
	MemoID          uuid.UUID
	Text            string
	Segments        []Segment
	Partial         bool
	Model           string
	Backend         string
	AudioDurationMS *int64
	CoveredMS       *int64
}

// RecordTranscript stores a collected result.
//
// UPSERT on (memo_id, model), and the conflict rule is one-way: a COMPLETE run
// may replace a partial one, and a partial may never replace a complete one.
// The reverse direction is refused by the database (CH005) rather than merely
// avoided here, so a future retry policy cannot downgrade a good transcript by
// re-collecting a bad one.
//
// Collecting the same result twice is a no-op. That matters because what
// ordinarily stops a double-collection is a TIER 1 row, and tier 1 is allowed
// to be lost — so the tier-2 index has to make losing it harmless rather than
// duplicating authored content.
//
// A row is written for EVERY succeeded result, empty text included. CHRN-25 §5
// binds that here in as many words, and names the line that would break it:
// `if text == "" { return }` is entirely innocent-looking, inverts the ruling
// in the SAFE direction so nothing ever complains, and strands the audio of
// exactly the memos the ruling argued should prune.
func (s *Store) RecordTranscript(ctx context.Context, in TranscriptInput) (Transcript, error) {
	segments := in.Segments
	if segments == nil {
		segments = []Segment{}
	}
	encoded, err := json.Marshal(segments)
	if err != nil {
		return Transcript{}, fmt.Errorf("store: encode segments: %w", err)
	}

	t, err := scanTranscript(s.pool.QueryRow(ctx, `
		INSERT INTO tier2.transcripts
		    (memo_id, text, segments, partial, model, backend,
		     audio_duration_ms, covered_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (memo_id, model) DO UPDATE
		   SET text              = excluded.text,
		       segments          = excluded.segments,
		       partial           = excluded.partial,
		       backend           = excluded.backend,
		       audio_duration_ms = excluded.audio_duration_ms,
		       covered_ms        = excluded.covered_ms,
		       transcribed_at    = now()
		 WHERE tier2.transcripts.partial AND NOT excluded.partial
		RETURNING `+transcriptColumns,
		in.MemoID, in.Text, encoded, in.Partial, in.Model, in.Backend,
		in.AudioDurationMS, in.CoveredMS))

	if errors.Is(err, ErrNotFound) {
		// The conflict fired and the WHERE refused the update: there is
		// already a transcript for this memo and model that this result does
		// not improve on. Re-collecting an identical result lands here too,
		// which is why it is not an error — return what is stored.
		return s.transcriptFor(ctx, in.MemoID, in.Model)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgTranscriptDowngraded:
			return Transcript{}, fmt.Errorf("%w: %s", ErrTranscriptDowngrade, pgErr.Message)
		case pgTranscriptReattributed:
			return Transcript{}, fmt.Errorf("store: %s", pgErr.Message)
		case pgForeignKeyViolation:
			return Transcript{}, ErrNotFound
		}
	}
	if err != nil {
		return Transcript{}, fmt.Errorf("store: record transcript: %w", err)
	}
	return t, nil
}

func (s *Store) transcriptFor(ctx context.Context, memoID uuid.UUID, model string) (Transcript, error) {
	t, err := scanTranscript(s.pool.QueryRow(ctx,
		`SELECT `+transcriptColumns+` FROM tier2.transcripts WHERE memo_id = $1 AND model = $2`,
		memoID, model))
	if err != nil {
		return Transcript{}, err
	}
	return t, nil
}

// HasDurableTranscript is THE GATE, and CHRN-22 may use nothing else.
//
// Three things about it are load-bearing, all of them from CHRN-25 §5:
//
//  1. It reads CHRONICLE, never the ASR service. That service answers 410 for a
//     result older than seven days, and the pruner fires at thirty — so at the
//     moment it runs, the job it would have to consult is routinely gone.
//  2. It is `NOT partial`, and nothing else. Never `covered_ms >=
//     audio_duration_ms`: whisper emits segments only where there is speech, so
//     an ordinary memo with trailing silence has covered_ms short of its
//     duration on a perfectly complete run.
//  3. EMPTY TEXT COUNTS. There is deliberately no `AND text <> ”` here, and
//     adding one would keep the audio of every silent memo forever while the
//     UI's PRUNES label quietly became a lie for them.
//
// Getting this wrong in the permissive direction deletes audio for a memo that
// was never transcribed, which CLAUDE.md names as the single worst thing this
// system can do.
func (s *Store) HasDurableTranscript(ctx context.Context, memoID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tier2.transcripts WHERE memo_id = $1 AND NOT partial)`,
		memoID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("store: durable transcript check: %w", err)
	}
	return ok, nil
}

// PartialMemo is one memo whose only transcript is incomplete.
type PartialMemo struct {
	MemoID        uuid.UUID
	Model         string
	TranscribedAt time.Time
}

// PartialTranscripts lists memos whose best transcript is partial, newest
// first, with a total.
//
// They are otherwise INVISIBLE, and that is the reason this exists. A memo with
// a partial transcript is `transcribed`, so MemosAwaitingTranscription does not
// return it; it is not `held`, so `chronicle retranscribe` will not release it;
// and its audio correctly does not prune, because HasDurableTranscript says no.
// Every one of those behaviours is right, and together they make a partial memo
// read as a healthy one.
//
// CHRN-28 settled the retry policy, and its answer for partials is the one
// already in force: keep them, mark them partial, and never let one satisfy the
// durable gate. So this count is not a backlog to work through — it is the list
// of memos whose best answer so far is incomplete, and the only way to see them.
func (s *Store) PartialTranscripts(ctx context.Context, limit int) (int64, []PartialMemo, error) {
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT count(DISTINCT memo_id) FROM tier2.transcripts t
		 WHERE t.partial
		   AND NOT EXISTS (SELECT 1 FROM tier2.transcripts d
		                    WHERE d.memo_id = t.memo_id AND NOT d.partial)`).Scan(&total); err != nil {
		return 0, nil, fmt.Errorf("store: count partial transcripts: %w", err)
	}
	if total == 0 {
		return 0, nil, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT ON (t.memo_id) t.memo_id, t.model, t.transcribed_at
		  FROM tier2.transcripts t
		 WHERE t.partial
		   AND NOT EXISTS (SELECT 1 FROM tier2.transcripts d
		                    WHERE d.memo_id = t.memo_id AND NOT d.partial)
		 ORDER BY t.memo_id, t.transcribed_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return 0, nil, fmt.Errorf("store: list partial transcripts: %w", err)
	}
	defer rows.Close()

	var out []PartialMemo
	for rows.Next() {
		var p PartialMemo
		if err := rows.Scan(&p.MemoID, &p.Model, &p.TranscribedAt); err != nil {
			return 0, nil, err
		}
		out = append(out, p)
	}
	return total, out, rows.Err()
}

// GetTranscript returns a memo's best transcript: a complete one if there is
// one, otherwise the most recent partial. ErrNotFound when there is none.
func (s *Store) GetTranscript(ctx context.Context, memoID uuid.UUID) (Transcript, error) {
	t, err := scanTranscript(s.pool.QueryRow(ctx, `
		SELECT `+transcriptColumns+` FROM tier2.transcripts
		 WHERE memo_id = $1
		 ORDER BY partial ASC, transcribed_at DESC
		 LIMIT 1`, memoID))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Transcript{}, fmt.Errorf("store: get transcript: %w", err)
	}
	return t, err
}
