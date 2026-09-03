package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

// Tier1Store is how derived work reaches the database, and it is a SEPARATE
// TYPE rather than a second pool hidden inside Store on purpose.
//
// Ruling R4 grants chronicle_tier1 SELECT on tier2.memos and tier2.transcripts
// and no write anywhere in tier 2. That grant is the enforcement mechanism
// CLAUDE.md names — but a grant enforces nothing unless something connects on
// it, which is the whole argument §1.1 makes against leaving Scribe on the main
// role until CHRN-52.
//
// Being a distinct type buys a second, cheaper guarantee that holds even on a
// deployment where the DSN has not been split yet: THERE IS NO METHOD HERE THAT
// WRITES TIER 2. Not "there is one nobody calls" — there is none to call. The
// grant is the outer wall and the type is the inner one, and CHRN-52's test
// checks the wall rather than taking this comment's word for it.
//
// Every proposal method lives here and none on Store, so "proposals are reached
// through the tier-1 store" is a rule with no exceptions to remember.
type Tier1Store struct {
	pool *pgxpool.Pool
}

// NewTier1 wraps a pool opened as the tier-1 role. It does not verify the role;
// Role reports it, and cmd/chronicle logs what it finds, because an operator
// who points this at the wrong DSN should be told rather than reassured.
func NewTier1(pool *pgxpool.Pool) *Tier1Store { return &Tier1Store{pool: pool} }

// Role reports the database role this pool actually connects as.
//
// It exists because the boot warning it feeds used to be a lie waiting to
// happen: it reported whether CHRONICLE_TIER1_DATABASE_URL was SET, so setting
// it to any DSN at all silenced the warning whether or not the DSN belonged to
// chronicle_tier1. Asking the database is the only answer that cannot drift
// from the truth.
func (s *Tier1Store) Role(ctx context.Context) (string, error) {
	var role string
	if err := s.pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		return "", fmt.Errorf("store: tier-1 role: %w", err)
	}
	return role, nil
}

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
func (s *Tier1Store) GetProposal(ctx context.Context, memoID uuid.UUID, proposer string) (Proposal, error) {
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
func (s *Tier1Store) SaveProposal(ctx context.Context, memoID, transcriptID uuid.UUID, proposer string, out scribe.Outcome) (Proposal, error) {
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

func (s *Tier1Store) saveFailedProposal(ctx context.Context, memoID, transcriptID uuid.UUID, proposer string, out scribe.Outcome) (Proposal, error) {
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
// cleared is APPENDED to whatever the write-time run already recorded, not
// substituted for it. Review found the trace that makes the difference: a
// proposal whose advisory nearest_page was cleared at write time, then accepted
// two days later against an archived project, re-runs Reconcile on the STORED
// payload — where NearestPage is already nil, so nothing re-reports it — and a
// replace would drop the hallucination on the floor. 0007 states the column's
// purpose as the hallucination rate CHRN-36 reports, and nearest_page is the
// one field that rate is about.
func (s *Tier1Store) BumpProposalGeneration(ctx context.Context, memoID uuid.UUID, proposer string, p *scribe.Proposal, cleared []scribe.ClearedField, status scribe.Status) (Proposal, error) {
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
		        cleared_fields = COALESCE(cleared_fields, '[]'::jsonb) || COALESCE($5::jsonb, '[]'::jsonb)
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
func (s *Tier1Store) TranscriptForScribe(ctx context.Context, memoID uuid.UUID) (Transcript, error) {
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

// MemoByHash resolves CHRN-36's label identity — tier2.memos.content_hash — to
// the memo it names.
//
// ON Tier1Store rather than Store, because the eval harness is DERIVED work and
// reads the corpus the way Scribe does: ruling R4 grants chronicle_tier1 SELECT
// on tier2.memos, and a harness that reached the corpus through the main role
// would be scoring the router while standing outside the boundary the router
// stands inside.
//
// The uniqueness constraint is on (author_id, content_hash), so two accounts
// ingesting the same bytes are two memos with one hash. That is refused rather
// than resolved: an eval run that silently picked one of them would score a
// memo the labeller never read, and there is no field on the label that could
// say which. It cannot happen on a single-author estate, which is exactly why
// it would be found the hard way.
func (s *Tier1Store) MemoByHash(ctx context.Context, contentHash string) (Memo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+memoColumns+` FROM tier2.memos WHERE content_hash = $1 LIMIT 2`, contentHash)
	if err != nil {
		return Memo{}, fmt.Errorf("store: memo by hash: %w", err)
	}
	defer rows.Close()

	var found []Memo
	for rows.Next() {
		m, err := scanMemo(rows)
		if err != nil {
			return Memo{}, fmt.Errorf("store: memo by hash: %w", err)
		}
		found = append(found, m)
	}
	if err := rows.Err(); err != nil {
		return Memo{}, fmt.Errorf("store: memo by hash: %w", err)
	}
	switch len(found) {
	case 0:
		return Memo{}, ErrNotFound
	case 1:
		return found[0], nil
	default:
		return Memo{}, fmt.Errorf("store: content_hash %s names %d memos across authors, so it does not identify one", contentHash, len(found))
	}
}

// ProposalsForMemos returns one proposer's proposals for a set of memos, keyed
// by memo. Memos Scribe has never run over are simply absent, and ABSENT IS A
// REAL ANSWER the triage screen must render — see CHRN-33's generation echo,
// where "I saw no proposal" is `null` and is checked like any other value.
//
// ONE PROPOSER, not all of them. The identity of a proposal is
// `(memo_id, proposer)`, so a batch fetched across proposers would hand the
// screen two cards for one memo — the same failure keying on transcript_id
// would have caused, arriving through the other door.
func (s *Tier1Store) ProposalsForMemos(ctx context.Context, memoIDs []uuid.UUID, proposer string) (map[uuid.UUID]Proposal, error) {
	out := map[uuid.UUID]Proposal{}
	if len(memoIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+proposalColumns+` FROM tier1.memo_proposals
		  WHERE memo_id = ANY($1) AND proposer = $2`, memoIDs, proposer)
	if err != nil {
		return nil, fmt.Errorf("store: proposals for memos: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProposal(rows)
		if err != nil {
			return nil, fmt.Errorf("store: proposals for memos: %w", err)
		}
		out[p.MemoID] = p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: proposals for memos: %w", err)
	}
	return out, nil
}
