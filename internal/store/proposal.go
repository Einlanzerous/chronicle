package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// Proposal is one row of tier1.memo_proposals: what Scribe said a memo should
// become, plus everything needed to attribute it and to notice it has moved.
//
// TIER 1. Delete every row and re-run Scribe over the transcripts and they come
// back, which is the test CLAUDE.md gives — and CLAUDE.md names Scribe
// proposals among the things tier 1 holds. The acceptance that follows one is a
// TIER-2 write and lives elsewhere (CHRN-33): a router that could route and
// commit would be a tier-1 process authoring tier-2 state.
type Proposal struct {
	ID           uuid.UUID
	MemoID       uuid.UUID
	TranscriptID uuid.UUID
	Proposer     string

	// Generation moves on every payload MUTATION, not on every Scribe run — a
	// supersede is not the only door, since stage 2 at acceptance can clear a
	// field with no run behind it. An accept echoes this and a mismatch is
	// re-shown, which is what stops an operator committing a proposal they
	// never saw.
	Generation int
	Status     scribe.Status

	// Payload is nil only when Status is invalid, and the database enforces
	// the biconditional rather than trusting this package to.
	Payload           *scribe.Proposal
	SupersededPayload *scribe.Proposal
	Cleared           []scribe.ClearedField

	Error string

	// RawOutput is ALWAYS the text Payload was parsed from. LastAttemptRaw is
	// where a failed re-run's output goes, so the pair above stays a pair.
	RawOutput      string
	LastAttemptRaw string

	CreatedAt time.Time
	UpdatedAt time.Time
}

const proposalColumns = `id, memo_id, transcript_id, proposer, generation, status,
	payload, superseded_payload, cleared_fields, error, raw_output, last_attempt_raw,
	created_at, updated_at`

func scanProposal(row pgx.Row) (Proposal, error) {
	var p Proposal
	var payload, superseded, cleared []byte
	var errText, rawOut, lastRaw *string
	err := row.Scan(&p.ID, &p.MemoID, &p.TranscriptID, &p.Proposer, &p.Generation,
		&p.Status, &payload, &superseded, &cleared, &errText, &rawOut, &lastRaw,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Proposal{}, ErrNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &p.Payload); err != nil {
			return Proposal{}, fmt.Errorf("store: decode proposal payload: %w", err)
		}
	}
	if len(superseded) > 0 {
		if err := json.Unmarshal(superseded, &p.SupersededPayload); err != nil {
			return Proposal{}, fmt.Errorf("store: decode superseded payload: %w", err)
		}
	}
	if len(cleared) > 0 {
		if err := json.Unmarshal(cleared, &p.Cleared); err != nil {
			return Proposal{}, fmt.Errorf("store: decode cleared fields: %w", err)
		}
	}
	if errText != nil {
		p.Error = *errText
	}
	if rawOut != nil {
		p.RawOutput = *rawOut
	}
	if lastRaw != nil {
		p.LastAttemptRaw = *lastRaw
	}
	return p, nil
}

// GetProposal returns a memo's proposal under one proposer. ErrNotFound when
// Scribe has never run over it.
func (s *Store) GetProposal(ctx context.Context, memoID uuid.UUID, proposer string) (Proposal, error) {
	p, err := scanProposal(s.pool.QueryRow(ctx,
		`SELECT `+proposalColumns+` FROM tier1.memo_proposals
		  WHERE memo_id = $1 AND proposer = $2`, memoID, proposer))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Proposal{}, fmt.Errorf("store: get proposal: %w", err)
	}
	return p, err
}

// SaveProposal writes the result of one Scribe run, and is where §7's two
// asymmetries are enforced.
//
// A SUCCESSFUL RUN supersedes: the previous payload moves to
// superseded_payload, the new one lands with its raw output, and generation
// advances. One generation of history and not an append-only log — tier 1 is
// disposable, and a full record of every proposal the model ever made is a
// table that grows without a reader. One generation answers "did that prompt
// change help", which is the only question asked of it.
//
// A FAILED RUN NEVER DISPLACES A PAYLOAD THAT WAS VALID. It records the error
// and the raw output in last_attempt_raw, leaves payload, raw_output, status
// and generation exactly as they were, and the operator keeps seeing the
// proposal that validated. Otherwise a prompt regression on Tuesday costs a
// perfectly good proposal from Monday, and the memo silently becomes work.
//
// The two together give `invalid` exactly one meaning: no run has ever produced
// a valid proposal for this memo under this proposer. Migration 0007's
// proposals_invalid_iff_no_payload constraint holds the same line in the
// database, so a future writer that forgets this comment still cannot break it.
func (s *Store) SaveProposal(ctx context.Context, memoID, transcriptID uuid.UUID, proposer string, out scribe.Outcome) (Proposal, error) {
	if out.Proposal == nil {
		return s.saveFailedProposal(ctx, memoID, transcriptID, proposer, out)
	}

	payload, err := json.Marshal(out.Proposal)
	if err != nil {
		return Proposal{}, fmt.Errorf("store: encode proposal payload: %w", err)
	}
	// Marshalled rather than left nil so the column is a JSON array on every
	// row that has one, and CHRN-36 can count clearings without a NULL branch.
	var cleared []byte
	if len(out.Cleared) > 0 {
		if cleared, err = json.Marshal(out.Cleared); err != nil {
			return Proposal{}, fmt.Errorf("store: encode cleared fields: %w", err)
		}
	}

	p, err := scanProposal(s.pool.QueryRow(ctx,
		`INSERT INTO tier1.memo_proposals
		     (memo_id, transcript_id, proposer, generation, status, payload,
		      cleared_fields, raw_output)
		 VALUES ($1, $2, $3, 1, $4, $5, $6, $7)
		 ON CONFLICT (memo_id, proposer) DO UPDATE SET
		     transcript_id      = EXCLUDED.transcript_id,
		     generation         = tier1.memo_proposals.generation + 1,
		     status             = EXCLUDED.status,
		     superseded_payload = tier1.memo_proposals.payload,
		     payload            = EXCLUDED.payload,
		     cleared_fields     = EXCLUDED.cleared_fields,
		     raw_output         = EXCLUDED.raw_output,
		     -- A run that succeeded clears the last failure rather than
		     -- leaving it to be read as current.
		     error              = NULL,
		     last_attempt_raw   = NULL
		 RETURNING `+proposalColumns, memoID, transcriptID, proposer, out.Status,
		payload, cleared, string(out.Raw)))
	if err != nil {
		return Proposal{}, fmt.Errorf("store: save proposal: %w", err)
	}
	return p, nil
}

func (s *Store) saveFailedProposal(ctx context.Context, memoID, transcriptID uuid.UUID, proposer string, out scribe.Outcome) (Proposal, error) {
	msg := ""
	if out.Err != nil {
		msg = out.Err.Error()
	}

	// The INSERT lands `invalid` with no payload: no run has ever succeeded.
	// The UPDATE deliberately touches neither payload, raw_output, status nor
	// generation -- only the failure record. When there was no prior payload
	// the row was already `invalid`, so the status is right either way without
	// a branch.
	p, err := scanProposal(s.pool.QueryRow(ctx,
		`INSERT INTO tier1.memo_proposals
		     (memo_id, transcript_id, proposer, generation, status, error, last_attempt_raw)
		 VALUES ($1, $2, $3, 1, 'invalid', $4, $5)
		 ON CONFLICT (memo_id, proposer) DO UPDATE SET
		     error            = EXCLUDED.error,
		     last_attempt_raw = EXCLUDED.last_attempt_raw
		 RETURNING `+proposalColumns, memoID, transcriptID, proposer, msg, string(out.Raw)))
	if err != nil {
		return Proposal{}, fmt.Errorf("store: save failed proposal: %w", err)
	}
	return p, nil
}

// BumpProposalGeneration records a payload mutation that no Scribe run caused —
// stage 2 at acceptance clearing a field whose project was archived mid-week.
//
// It exists because generation belongs to THE PAYLOAD AND NOT TO THE RUN. A
// counter that only moved on a supersede would sit still while the bytes
// changed, and the client's next accept would echo a generation that still
// matched a payload that had moved. Same drift, different door.
func (s *Store) BumpProposalGeneration(ctx context.Context, memoID uuid.UUID, proposer string, p *scribe.Proposal, cleared []scribe.ClearedField, status scribe.Status) (Proposal, error) {
	payload, err := json.Marshal(p)
	if err != nil {
		return Proposal{}, fmt.Errorf("store: encode proposal payload: %w", err)
	}
	var clearedJSON []byte
	if len(cleared) > 0 {
		if clearedJSON, err = json.Marshal(cleared); err != nil {
			return Proposal{}, fmt.Errorf("store: encode cleared fields: %w", err)
		}
	}
	out, err := scanProposal(s.pool.QueryRow(ctx,
		`UPDATE tier1.memo_proposals
		    SET generation     = generation + 1,
		        status         = $3,
		        payload        = $4,
		        cleared_fields = $5
		  WHERE memo_id = $1 AND proposer = $2
		 RETURNING `+proposalColumns, memoID, proposer, status, payload, clearedJSON))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Proposal{}, fmt.Errorf("store: bump proposal generation: %w", err)
	}
	return out, err
}

// ScribeModelRank orders the models the floor admits, worst to best.
//
// A SEPARATE THING FROM THE FLOOR, and deliberately so. SufficientModels is an
// unranked SET — it says which transcripts may delete audio, not which is
// better — and GetTranscript orders by `partial ASC, transcribed_at DESC`,
// which is MOST RECENT COMPLETE rather than highest quality. That is the right
// answer for display and the wrong one here: a memo re-transcribed down to
// `small.en` after a `large-v3` run would hand Scribe the worse text, and the
// routing error would never be traced to its cause.
//
// HARD-CODED, NEVER CONFIGURATION, for the reason SufficientModels states about
// itself: internal/store/ is in `sensitive_paths`, so a constant here is
// reviewed at the expensive tier and an environment variable is reviewed by
// nobody.
var ScribeModelRank = []string{"small.en", "medium.en", "large-v3"}

// TranscriptForScribe returns the transcript Scribe should route from: the best
// one that clears CHRN-22's durable floor.
//
// The floor is SHARED with the pump and the pruner (DurableClause) rather than
// restated, for the reason HasDurableTranscript gives at length — one predicate,
// every caller. The ranking on top of it is this function's own.
//
// ErrNotFound when the memo has no transcript the floor admits, which is the
// correct answer rather than a gap: a memo whose only transcript is partial, or
// came from a model this deployment has never measured, is a memo there is
// nothing trustworthy to route from.
func (s *Store) TranscriptForScribe(ctx context.Context, memoID uuid.UUID) (Transcript, error) {
	t, err := scanTranscript(s.pool.QueryRow(ctx,
		`SELECT `+transcriptColumns+` FROM tier2.transcripts
		  WHERE memo_id = $1 AND `+DurableClause+`
		  ORDER BY array_position($4::text[], split_part(model, '/', 2)) DESC NULLS LAST,
		           transcribed_at DESC
		  LIMIT 1`,
		memoID, SufficientRunners, SufficientModels, ScribeModelRank))
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Transcript{}, fmt.Errorf("store: transcript for scribe: %w", err)
	}
	return t, err
}
