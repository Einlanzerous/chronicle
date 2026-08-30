package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Einlanzerous/chronicle/internal/scribe"
)

const testProposer = "ollama/gemma4:31b@v1"

// seedMemoWithTranscript gives a memo one durable transcript and returns both
// ids, which is the state Scribe actually finds a memo in.
func seedMemoWithTranscript(t *testing.T, s *Store, ctx context.Context, hash, model string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: owner.ID, ContentHash: hash, ByteSize: 1024,
		Source: "copyparty", SourceRef: "/inbox/" + hash + ".opus",
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	tr, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: res.Memo.ID, Text: "a memo about something", Model: model, Backend: "vulkan",
	})
	if err != nil {
		t.Fatalf("RecordTranscript: %v", err)
	}
	return res.Memo.ID, tr.ID
}

func validOutcome(conf float64) scribe.Outcome {
	key := "CHRN"
	return scribe.Outcome{
		Proposal: &scribe.Proposal{
			Destination: scribe.DestTicket, Confidence: conf, Reason: "names an owner",
			Title: "Do the thing", ProjectKey: &key, TicketType: "task", Description: "## Summary",
		},
		Raw:    []byte(`{"destination":"TICKET","confidence":` + "0.9" + `}`),
		Status: scribe.StatusValid,
	}
}

// ============================================================================
// R4 — the grant migration 0007 moves, and the line it does not move.
// ============================================================================

// SCRIBE CAN READ THE CORPUS IT DERIVES FROM. This is the whole of ruling R4,
// and before 0007 it was false: 0001 revoked schema tier2 from chronicle_tier1
// outright, so a role that CLAUDE.md expects to produce "Scribe proposals,
// extracted entities, search indexes" could not read the transcripts and memos
// all three derive from. You cannot derive from a corpus you cannot read.
func TestTier1RoleCanReadTheCorpusItDerivesFrom(t *testing.T) {
	s, ctx := newTestStore(t)
	seedMemoWithTranscript(t, s, ctx, strings.Repeat("a", 64), "whisper.cpp/small.en")

	pool := tier1Pool(t, ctx)
	defer pool.Close()

	for _, q := range []string{
		`SELECT count(*) FROM tier2.memos`,
		`SELECT count(*) FROM tier2.transcripts`,
		`SELECT text FROM tier2.transcripts LIMIT 1`,
	} {
		var got any
		if err := pool.QueryRow(ctx, q).Scan(&got); err != nil {
			t.Errorf("chronicle_tier1 could not run %q: %v — Scribe cannot route without this", q, err)
		}
	}
}

// AND STILL CANNOT WRITE A WORD OF IT. This is the invariant, stated the way
// CLAUDE.md states it: "no tier-1 write path can reach a tier-2 table". R4
// widened the read and left this exactly where it was, which is the entire
// argument for why the widening was safe.
//
// CHRN-52 inherits this test's shape. Its subject is no longer "the role sees
// nothing" but "the role reads two tables and writes none", which is a sharper
// claim and a truer one.
func TestTier1RoleStillCannotWriteTheCorpus(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, _ := seedMemoWithTranscript(t, s, ctx, strings.Repeat("b", 64), "whisper.cpp/small.en")

	pool := tier1Pool(t, ctx)
	defer pool.Close()

	for _, q := range []string{
		`UPDATE tier2.memos SET state = 'triaged' WHERE id = $1`,
		`DELETE FROM tier2.memos WHERE id = $1`,
		`UPDATE tier2.transcripts SET text = 'rewritten' WHERE memo_id = $1`,
		`DELETE FROM tier2.transcripts WHERE memo_id = $1`,
	} {
		if _, err := pool.Exec(ctx, q, memoID); err == nil {
			t.Errorf("chronicle_tier1 executed %q — a tier-1 write path reached a tier-2 table", q)
		}
	}

	// The INSERT is separate because it takes no memo id, and it is the one
	// that would let a derived process fabricate authored content outright.
	if _, err := pool.Exec(ctx,
		`INSERT INTO tier2.transcripts (memo_id, text, segments, model, backend)
		 VALUES ($1, 'invented', '[]'::jsonb, 'whisper.cpp/large-v3', 'vulkan')`, memoID); err == nil {
		t.Error("chronicle_tier1 inserted a transcript; a derived process can author tier-2 content")
	}
}

// The grant names two tables, and 0007 deliberately uses no ALTER DEFAULT
// PRIVILEGES — so a tier-2 table added tomorrow is unreadable until somebody
// grants it on purpose. tier2.users and tier2.user_tokens are the two whose
// exposure would matter most, and TestTier1RoleCannotReachCredentials already
// holds that line; this checks the general rule it is an instance of.
func TestTier1ReadIsNotABlanketSchemaGrant(t *testing.T) {
	_, ctx := newTestStore(t)
	pool := tier1Pool(t, ctx)
	defer pool.Close()

	var canSelectUsers bool
	if err := pool.QueryRow(ctx,
		`SELECT has_table_privilege(current_user, 'tier2.users', 'SELECT')`).Scan(&canSelectUsers); err != nil {
		t.Fatalf("privilege check: %v", err)
	}
	if canSelectUsers {
		t.Error("chronicle_tier1 holds SELECT on tier2.users; 0007 granted more than two tables")
	}
	// And the two it should have.
	for _, table := range []string{"tier2.memos", "tier2.transcripts"} {
		var ok bool
		if err := pool.QueryRow(ctx,
			`SELECT has_table_privilege(current_user, $1, 'SELECT')`, table).Scan(&ok); err != nil {
			t.Fatalf("privilege check: %v", err)
		}
		if !ok {
			t.Errorf("chronicle_tier1 lacks SELECT on %s", table)
		}
		for _, priv := range []string{"INSERT", "UPDATE", "DELETE"} {
			var held bool
			if err := pool.QueryRow(ctx,
				`SELECT has_table_privilege(current_user, $1, $2)`, table, priv).Scan(&held); err != nil {
				t.Fatalf("privilege check: %v", err)
			}
			if held {
				t.Errorf("chronicle_tier1 holds %s on %s — this is the line", priv, table)
			}
		}
	}
}

func tier1Pool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("CHRONICLE_TEST_TIER1_DATABASE_URL")
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_TIER1_DATABASE_URL not set; skipping tier-role test")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as chronicle_tier1: %v", err)
	}
	// The positive control the existing isolation test insists on: without it
	// every assertion would also pass on a connection that never worked.
	var role string
	if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatalf("the tier-1 connection does not work, so nothing proves anything: %v", err)
	}
	if role != "chronicle_tier1" {
		t.Fatalf("connected as %q, want chronicle_tier1", role)
	}
	return pool
}

// ============================================================================
// §7 — the two asymmetries.
// ============================================================================

func TestSaveProposalSupersedesAndAdvancesTheGeneration(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("c", 64), "whisper.cpp/small.en")

	first, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.5))
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	if first.Generation != 1 {
		t.Fatalf("generation %d on a first write, want 1", first.Generation)
	}

	second, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.9))
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	if second.Generation != 2 {
		t.Fatalf("generation %d after a supersede, want 2", second.Generation)
	}
	if second.Payload.Confidence != 0.9 {
		t.Fatalf("the new payload did not land: %+v", second.Payload)
	}
	if second.SupersededPayload == nil || second.SupersededPayload.Confidence != 0.5 {
		t.Fatal("the previous payload was not kept; one generation of history is what answers " +
			"'did that prompt change help'")
	}
}

// THE RULE THAT COSTS NOTHING TO STATE AND EVERYTHING TO GET WRONG. A memo with
// a good proposal from Monday, re-run on Tuesday under a bumped prompt version
// that fails validation three times, must not lose Monday's proposal. Otherwise
// a prompt regression silently turns working memos into work.
func TestAFailedRerunNeverDisplacesAValidPayload(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("d", 64), "whisper.cpp/small.en")

	good, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.86))
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	failed := scribe.Outcome{
		Raw:    []byte(`{"destination":"NONSENSE"}`),
		Status: scribe.StatusInvalid,
		Err:    errors.New("destination: must be one of [NOTE TICKET DISCUSSION DISCARD]"),
	}
	after, err := s.SaveProposal(ctx, memoID, trID, testProposer, failed)
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	if after.Payload == nil || after.Payload.Confidence != 0.86 {
		t.Fatal("the failed re-run destroyed a proposal that validated")
	}
	if after.Status != scribe.StatusValid {
		t.Fatalf("status %q — a failed attempt must not downgrade a usable proposal", after.Status)
	}
	if after.Generation != good.Generation {
		t.Fatalf("generation moved from %d to %d on a run that changed no payload",
			good.Generation, after.Generation)
	}
	if !strings.Contains(after.Error, "NONSENSE") && !strings.Contains(after.Error, "destination") {
		t.Fatalf("the failure was not recorded: %q", after.Error)
	}
	if after.LastAttemptRaw == "" {
		t.Fatal("the failed attempt's output was dropped; it is the evidence of why the prompt broke")
	}
	// THE PAIRING. raw_output is the text payload was parsed from, always — so
	// CHRN-36 never diffs attempt N's junk against attempt N-1's proposal.
	if strings.Contains(after.RawOutput, "NONSENSE") {
		t.Fatal("raw_output holds the failed attempt's output; it must pair with the payload beside it")
	}
}

// `invalid` means exactly one thing: no run has ever produced a valid proposal
// for this memo under this proposer.
func TestInvalidMeansNoRunHasEverSucceeded(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("e", 64), "whisper.cpp/small.en")

	out := scribe.Outcome{Raw: []byte(`{}`), Status: scribe.StatusInvalid, Err: errors.New("empty")}
	p, err := s.SaveProposal(ctx, memoID, trID, testProposer, out)
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	if p.Status != scribe.StatusInvalid || p.Payload != nil {
		t.Fatalf("status=%q payload=%v, want invalid with no payload", p.Status, p.Payload)
	}
	if p.RawOutput != "" {
		t.Fatal("raw_output is set with no payload to pair with")
	}
}

// A run that succeeds after failures clears the failure record, so a stale
// error is never read as current.
func TestASuccessfulRunClearsAnEarlierFailure(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("f", 64), "whisper.cpp/small.en")

	_, err := s.SaveProposal(ctx, memoID, trID, testProposer,
		scribe.Outcome{Raw: []byte(`{}`), Status: scribe.StatusInvalid, Err: errors.New("empty")})
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	p, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.7))
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	if p.Error != "" || p.LastAttemptRaw != "" {
		t.Fatalf("a stale failure survived a successful run: error=%q last=%q", p.Error, p.LastAttemptRaw)
	}
}

// §2 [rev 2]: generation belongs to the PAYLOAD, not to the run. Stage 2 at
// acceptance clearing an archived project is a payload change with no Scribe
// run behind it, and a counter that only moved on a supersede would sit still
// while the bytes moved — letting the next accept echo a generation that
// matched a payload that had changed.
func TestGenerationMovesWhenAcceptanceMutatesThePayload(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("1", 64), "whisper.cpp/small.en")

	before, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.9))
	if err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	// The project was archived between proposal and acceptance.
	mutated := *before.Payload
	empty := ""
	mutated.ProjectKey = &empty
	cleared := []scribe.ClearedField{{Field: "project_key", Value: "CHRN", Reason: "no such live Switchyard project"}}

	after, err := s.BumpProposalGeneration(ctx, memoID, testProposer, &mutated, cleared, scribe.StatusNeedsInput)
	if err != nil {
		t.Fatalf("BumpProposalGeneration: %v", err)
	}
	if after.Generation != before.Generation+1 {
		t.Fatalf("generation %d, want %d — a payload moved with no run behind it",
			after.Generation, before.Generation+1)
	}
	if after.Status != scribe.StatusNeedsInput || len(after.Cleared) != 1 {
		t.Fatalf("status=%q cleared=%+v", after.Status, after.Cleared)
	}
}

// ============================================================================
// The database's own guards.
// ============================================================================

func TestAProposalCannotBeReattributed(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("2", 64), "whisper.cpp/small.en")
	if _, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.5)); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier1.memo_proposals SET proposer = 'ollama/gemma4:e4b@v1' WHERE memo_id = $1`, memoID)
	if err == nil {
		t.Fatal("a proposal was re-attributed to another proposer; it would then be credited to " +
			"a model that never said it")
	}
}

func TestGenerationMayNotDecrease(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("3", 64), "whisper.cpp/small.en")
	if _, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.5)); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}
	if _, err := s.SaveProposal(ctx, memoID, trID, testProposer, validOutcome(0.6)); err != nil {
		t.Fatalf("SaveProposal: %v", err)
	}

	_, err := s.Pool().Exec(ctx,
		`UPDATE tier1.memo_proposals SET generation = 1 WHERE memo_id = $1`, memoID)
	if err == nil {
		t.Fatal("generation went backwards; an accept could then be replayed against it")
	}
}

// An unqualified proposer is one CHRN-36 cannot attribute — the same defence
// DurableClause relies on for tier2.transcripts.model.
func TestAnUnqualifiedProposerIsRefused(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, trID := seedMemoWithTranscript(t, s, ctx, strings.Repeat("4", 64), "whisper.cpp/small.en")

	for _, bad := range []string{"gemma4:31b", "ollama/gemma4:31b", "gemma4:31b@v1"} {
		_, err := s.SaveProposal(ctx, memoID, trID, bad, validOutcome(0.5))
		if err == nil {
			t.Errorf("proposer %q was accepted; it names no runner or no prompt version", bad)
		}
	}
}

// ============================================================================
// §2 — which transcript Scribe reads.
// ============================================================================

// THE RANKING IS NEW AND DIVERGES FROM GetTranscript, deliberately.
// GetTranscript orders by `partial ASC, transcribed_at DESC` — most recent
// complete, which is right for display. Here a memo re-transcribed DOWN to
// small.en after a large-v3 run must still route from large-v3, or the routing
// error is one nobody would ever trace to its cause.
func TestTranscriptForScribePrefersQualityOverRecency(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID, _ := seedMemoWithTranscript(t, s, ctx, strings.Repeat("5", 64), "whisper.cpp/large-v3")

	// A later, worse transcript. RecordTranscript is upsert-on-(memo,model), so
	// this is a second row rather than a replacement.
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: memoID, Text: "worse and newer", Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatalf("RecordTranscript: %v", err)
	}

	// The display helper picks the newest, which is the small one.
	display, err := s.GetTranscript(ctx, memoID)
	if err != nil {
		t.Fatalf("GetTranscript: %v", err)
	}
	if display.Model != "whisper.cpp/small.en" {
		t.Fatalf("GetTranscript returned %q; this test's premise is that it prefers recency", display.Model)
	}

	// Scribe picks the best.
	got, err := s.TranscriptForScribe(ctx, memoID)
	if err != nil {
		t.Fatalf("TranscriptForScribe: %v", err)
	}
	if got.Model != "whisper.cpp/large-v3" {
		t.Fatalf("Scribe would route from %q; it must route from the best transcript the floor admits",
			got.Model)
	}
}

// The floor is shared with the pump and the pruner rather than restated. A
// transcript below it is not something to route from, and ErrNotFound is the
// correct answer rather than a gap.
func TestTranscriptForScribeRefusesWhatTheFloorRefuses(t *testing.T) {
	s, ctx := newTestStore(t)
	owner, err := s.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: owner.ID, ContentHash: strings.Repeat("6", 64), ByteSize: 512,
		Source: "copyparty", SourceRef: "/inbox/six.opus",
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	// Below the quality axis of CHRN-22's two-axis floor.
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: res.Memo.ID, Text: "phone grade", Model: "whisper.cpp/base.en", Backend: "cpu",
	}); err != nil {
		t.Fatalf("RecordTranscript: %v", err)
	}

	if _, err := s.TranscriptForScribe(ctx, res.Memo.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound — base.en is below the floor the pruner shares", err)
	}
}
