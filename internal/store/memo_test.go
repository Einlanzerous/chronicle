package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CHRN-18's twelve Done-when assertions. The decision they check is in
// docs/decisions/chrn-18-memo-model-and-idempotency.md.

// hashOf is the identity function the whole design turns on: SHA-256 over the
// bytes exactly as they arrived, lowercase hex.
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// sqlState pulls the SQLSTATE off a pgx error, or "" if it carries none. The
// tests assert on the code rather than the message so that rewording a RAISE
// does not silently turn an assertion into a tautology.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// newAuthor creates an account to hang memos off.
func newAuthor(t *testing.T, s *Store, ctx context.Context, email string) uuid.UUID {
	t.Helper()
	u, err := s.CreateUser(ctx, email, "Author", KindPerson)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return u.ID
}

// rowVersion returns the memo row's xmin — the transaction that last wrote this
// row version. It is the exact test for "was this row rewritten", where
// updated_at is only a proxy: now() is transaction-start time, so a same-
// transaction rewrite leaves updated_at untouched and an assertion on it would
// pass whether or not the write happened.
func rowVersion(t *testing.T, s *Store, ctx context.Context, id uuid.UUID) string {
	t.Helper()
	var v string
	if err := s.Pool().QueryRow(ctx,
		`SELECT xmin::text FROM tier2.memos WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("read row version: %v", err)
	}
	return v
}

func countMemos(t *testing.T, s *Store, ctx context.Context) int {
	t.Helper()
	var n int
	if err := s.Pool().QueryRow(ctx, `SELECT count(*) FROM tier2.memos`).Scan(&n); err != nil {
		t.Fatalf("count memos: %v", err)
	}
	return n
}

// Done when #1: one recording delivered four ways is one memo with four
// arrivals. This is the epic's exit criterion — "a memo dropped into Copyparty
// and a memo uploaded from a phone both land as identical rows".
func TestFourDeliveriesOneMemo(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "four@x")
	hash := hashOf("the same recording")

	deliveries := []Arrival{
		{Source: SourceCopyparty, SourceRef: "/data/memos/a.opus"},
		{Source: SourceUpload, IdempotencyKey: "k-aaaaaaaaaaaaaaaa"},
		{Source: SourceCopyparty, SourceRef: "/data/memos/backup/a.opus"},
		{Source: SourceUpload, IdempotencyKey: "k-bbbbbbbbbbbbbbbb"},
	}

	var first uuid.UUID
	for i, d := range deliveries {
		d.AuthorID, d.ContentHash, d.ByteSize = author, hash, 4096
		res, err := s.IngestMemo(ctx, d)
		if err != nil {
			t.Fatalf("delivery %d: %v", i, err)
		}
		if i == 0 {
			first = res.Memo.ID
			if res.Collapsed {
				t.Error("the first delivery reported itself a collapse")
			}
		} else if res.Memo.ID != first {
			t.Fatalf("delivery %d landed on memo %s, want %s", i, res.Memo.ID, first)
		} else if !res.Collapsed {
			t.Errorf("delivery %d did not report as a collapse", i)
		}
		if want := i + 1; res.Deliveries != want {
			t.Errorf("after delivery %d, Deliveries = %d, want %d", i, res.Deliveries, want)
		}
	}

	if n := countMemos(t, s, ctx); n != 1 {
		t.Errorf("memos = %d, want 1", n)
	}
}

// Done when #2: the same, concurrently. Run with -race.
func TestFourDeliveriesOneMemoConcurrent(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "conc@x")
	hash := hashOf("recorded once, delivered by everybody at once")

	const n = 8
	ids := make([]uuid.UUID, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := Arrival{
				AuthorID: author, ContentHash: hash, ByteSize: 4096,
				Source: SourceCopyparty, SourceRef: fmt.Sprintf("/data/memos/%d.opus", i),
			}
			res, err := s.IngestMemo(ctx, d)
			ids[i], errs[i] = res.Memo.ID, err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent delivery %d: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Errorf("delivery %d got memo %s, want %s", i, ids[i], ids[0])
		}
	}
	if got := countMemos(t, s, ctx); got != 1 {
		t.Errorf("memos = %d, want 1", got)
	}
	var arrivals int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tier2.memo_arrivals WHERE memo_id = $1`, ids[0]).Scan(&arrivals); err != nil {
		t.Fatalf("count arrivals: %v", err)
	}
	if arrivals != n {
		t.Errorf("arrivals = %d, want %d", arrivals, n)
	}
}

// Done when #3: concurrent retries of ONE key all get the same memo and none
// errors. This is the case the advisory lock exists for — without it the losers
// hit memo_arrivals_key and get an error instead of their memo.
func TestConcurrentRetriesOfOneKeyCollapse(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "retry@x")
	hash := hashOf("a phone flushing its queue over flaky mobile data")

	const n = 8
	ids := make([]uuid.UUID, n)
	errs := make([]error, n)
	collapsed := make([]bool, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := s.IngestMemo(ctx, Arrival{
				AuthorID: author, ContentHash: hash, ByteSize: 4096,
				Source: SourceUpload, IdempotencyKey: "k-one-key-many-tries",
			})
			ids[i], errs[i], collapsed[i] = res.Memo.ID, err, res.Collapsed
		}(i)
	}
	wg.Wait()

	collapses := 0
	for i, err := range errs {
		if err != nil {
			t.Fatalf("retry %d errored instead of returning its memo: %v", i, err)
		}
		if ids[i] != ids[0] {
			t.Errorf("retry %d got memo %s, want %s", i, ids[i], ids[0])
		}
		if collapsed[i] {
			collapses++
		}
	}
	// Exactly one of the racers created the memo; the other seven collapsed
	// onto it and must each say so.
	if collapses != n-1 {
		t.Errorf("collapses reported = %d, want %d", collapses, n-1)
	}
	if got := countMemos(t, s, ctx); got != 1 {
		t.Errorf("memos = %d, want 1", got)
	}
	var arrivals int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FROM tier2.memo_arrivals WHERE memo_id = $1`, ids[0]).Scan(&arrivals); err != nil {
		t.Fatalf("count arrivals: %v", err)
	}
	if arrivals != 1 {
		t.Errorf("arrivals = %d, want 1: one key is one attempt however often it is retried", arrivals)
	}
}

// Done when #4: retries are free. A repeat of a delivery already recorded does
// not rewrite the memo row at all.
func TestRetriesDoNotRewriteTheRow(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "free@x")
	arrival := Arrival{
		AuthorID: author, ContentHash: hashOf("say it once"), ByteSize: 128,
		Source: SourceUpload, IdempotencyKey: "k-cccccccccccccccc",
		Retention: RetentionDays30,
	}

	first, err := s.IngestMemo(ctx, arrival)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	before := rowVersion(t, s, ctx, first.Memo.ID)

	for i := 0; i < 3; i++ {
		again, err := s.IngestMemo(ctx, arrival)
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if again.Memo.ID != first.Memo.ID {
			t.Fatalf("retry %d got a different memo", i)
		}
		// The write not happening is half of it. Decision §10 needs the retry
		// to SAY it collapsed, or the "zero collapses where four were expected"
		// alarm never fires.
		if !again.Collapsed {
			t.Errorf("retry %d did not report as a collapse", i)
		}
	}

	if after := rowVersion(t, s, ctx, first.Memo.ID); after != before {
		t.Errorf("row version %s -> %s: a retry rewrote the row", before, after)
	}
}

// Done when #5: a re-delivery writes nothing. The watcher observes and never
// consumes, so it keeps seeing the same file on every scan — if each sighting
// recorded an arrival, the table would grow without bound for a corpus that is
// not changing.
func TestRepeatedSightingWritesNothing(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "watch@x")
	sighting := Arrival{
		AuthorID: author, ContentHash: hashOf("a file that just sits there"), ByteSize: 64,
		Source: SourceCopyparty, SourceRef: "/data/memos/still-here.opus",
	}

	first, err := s.IngestMemo(ctx, sighting)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	before := rowVersion(t, s, ctx, first.Memo.ID)

	for i := 0; i < 5; i++ {
		res, err := s.IngestMemo(ctx, sighting)
		if err != nil {
			t.Fatalf("rescan %d: %v", i, err)
		}
		if res.Deliveries != 1 {
			t.Errorf("rescan %d: Deliveries = %d, want 1; a repeated sighting is not a delivery",
				i, res.Deliveries)
		}
		if !res.Collapsed {
			t.Errorf("rescan %d did not report as a collapse, so §10's log line would stay silent", i)
		}
	}
	if after := rowVersion(t, s, ctx, first.Memo.ID); after != before {
		t.Errorf("row version %s -> %s: a rescan rewrote the row", before, after)
	}
}

// Done when #6: the edges are the database's, not Go's. Every assertion here
// goes through raw SQL on the pool, because the point is that the rule holds for
// a psql session and a future worker in another language too.
func TestGuardRejectsFromRawSQL(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "guard@x")

	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("guarded"), ByteSize: 32,
		Source: SourceUpload, IdempotencyKey: "k-dddddddddddddddd",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	id := res.Memo.ID

	cases := []struct {
		name, sqlstate, query string
		args                  []any
	}{
		{
			name: "illegal transition", sqlstate: pgIllegalTransition,
			query: `UPDATE tier2.memos SET state = 'transcribed' WHERE id = $1`,
			args:  []any{id},
		},
		{
			name: "discarded is terminal", sqlstate: pgIllegalTransition,
			query: `UPDATE tier2.memos SET state = 'queued'
			         WHERE id = (SELECT id FROM tier2.memos WHERE state = 'discarded' LIMIT 1)`,
		},
		{
			name: "captured_at is immutable", sqlstate: pgMemoImmutable,
			query: `UPDATE tier2.memos SET captured_at = now() - interval '99 days' WHERE id = $1`,
			args:  []any{id},
		},
		{
			name: "content_hash is immutable", sqlstate: pgMemoImmutable,
			query: `UPDATE tier2.memos SET content_hash = $2 WHERE id = $1`,
			args:  []any{id, hashOf("different bytes")},
		},
		{
			name: "created in a state other than captured", sqlstate: pgMemoBadInitialState,
			query: `INSERT INTO tier2.memos (author_id, content_hash, byte_size, state)
			        VALUES ($1, $2, 1, 'queued')`,
			args: []any{author, hashOf("born queued")},
		},
	}

	// The discarded case needs a discarded memo to exist first.
	discarded, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("to be discarded"), ByteSize: 1,
		Source: SourceUpload, IdempotencyKey: "k-eeeeeeeeeeeeeeee",
	})
	if err != nil {
		t.Fatalf("ingest discardable: %v", err)
	}
	if _, err := s.AdvanceMemoState(ctx, discarded.Memo.ID, StateCaptured, StateDiscarded, "test"); err != nil {
		t.Fatalf("discard: %v", err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Pool().Exec(ctx, tc.query, tc.args...)
			if err == nil {
				t.Fatalf("raw SQL was accepted; the guard did not fire")
			}
			if got := sqlState(err); got != tc.sqlstate {
				t.Errorf("SQLSTATE = %q, want %q (error: %v)", got, tc.sqlstate, err)
			}
		})
	}

	// And the typed error, for the path that goes through Go.
	if _, err := s.AdvanceMemoState(ctx, id, StateCaptured, StateTranscribed, ""); !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("AdvanceMemoState illegal edge = %v, want ErrIllegalTransition", err)
	}
}

// Done when #7, the half CHRN-18 can execute: a hold survives its audio. The
// trigger must accept the release path with audio_pruned_at already set, or a
// memo held at the routing stage becomes unreleasable 30 days later — a hold
// that quietly turns into a deletion. The other half, that a worker claiming a
// memo with a durable transcript does not re-run ASR, belongs to E3, which owns
// both the worker and the transcript table.
func TestHoldSurvivesItsAudio(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "hold@x")

	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("held for routing"), ByteSize: 99,
		Source: SourceUpload, IdempotencyKey: "k-ffffffffffffffff",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	id := res.Memo.ID

	walk := func(reason string, states ...string) {
		t.Helper()
		from := states[0]
		for _, to := range states[1:] {
			if _, err := s.AdvanceMemoState(ctx, id, from, to, reason); err != nil {
				t.Fatalf("advance %s -> %s: %v", from, to, err)
			}
			from = to
		}
	}
	walk("", StateCaptured, StateQueued, StateTranscribing, StateTranscribed)
	walk("routing ambiguous", StateTranscribed, StateHeld)

	// CHRN-22 prunes the audio while it sits held. The transcript stays.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.memos SET audio_pruned_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("prune audio: %v", err)
	}

	// The release path, with no audio left to read.
	from := StateHeld
	for _, to := range []string{StateQueued, StateTranscribing, StateTranscribed, StateTriaged} {
		m, err := s.AdvanceMemoState(ctx, id, from, to, "")
		if err != nil {
			t.Fatalf("release %s -> %s after the audio was pruned: %v", from, to, err)
		}
		if !m.AudioPruned() {
			t.Fatalf("audio_pruned_at was cleared by the move to %s", to)
		}
		from = to
	}

	// Releasing a hold clears the reason it was held for. Left merged, a memo
	// that is no longer held still explains why it was.
	final, err := s.GetMemo(ctx, id)
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}
	if final.StateReason != nil {
		t.Errorf("state_reason = %q after release, want cleared", *final.StateReason)
	}
}

// Done when #8: a key presented again with different bytes is refused, and
// creates nothing.
func TestKeyReuseWithDifferentBytesIsRefused(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "reuse@x")

	if _, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("the first recording"), ByteSize: 10,
		Source: SourceUpload, IdempotencyKey: "k-recycled-by-a-bug",
	}); err != nil {
		t.Fatalf("first: %v", err)
	}

	_, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("a completely different recording"), ByteSize: 20,
		Source: SourceUpload, IdempotencyKey: "k-recycled-by-a-bug",
	})
	if !errors.Is(err, ErrKeyReused) {
		t.Fatalf("err = %v, want ErrKeyReused", err)
	}
	if n := countMemos(t, s, ctx); n != 1 {
		t.Errorf("memos = %d, want 1: the refused arrival left a row behind", n)
	}
}

// Done when #9: the ratchet holds, and only for real opinions. Three
// assertions, because the rule failed in three different ways in draft.
func TestRetentionRatchet(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "ratchet@x")

	t.Run("a lower arrival does not lower a pin", func(t *testing.T) {
		hash := hashOf("pinned forever")
		base := Arrival{AuthorID: author, ContentHash: hash, ByteSize: 1,
			Source: SourceUpload, IdempotencyKey: "k-1111111111111111"}

		pinned := base
		pinned.Retention = RetentionForever
		if _, err := s.IngestMemo(ctx, pinned); err != nil {
			t.Fatalf("pin: %v", err)
		}

		lower := base
		lower.Retention, lower.IdempotencyKey = RetentionDays30, "k-2222222222222222"
		res, err := s.IngestMemo(ctx, lower)
		if err != nil {
			t.Fatalf("re-upload: %v", err)
		}
		if res.Memo.Retention != RetentionForever {
			t.Errorf("retention = %s, want forever", res.Memo.Retention)
		}
	})

	t.Run("a copyparty default does not lift an authored discard_now", func(t *testing.T) {
		hash := hashOf("discard this now")
		authored := Arrival{AuthorID: author, ContentHash: hash, ByteSize: 1,
			Source: SourceUpload, IdempotencyKey: "k-3333333333333333",
			Retention: RetentionDiscardNow}
		if _, err := s.IngestMemo(ctx, authored); err != nil {
			t.Fatalf("authored discard: %v", err)
		}

		// The watcher finds the same bytes on disk and has no opinion at all.
		// If "no opinion" were spelled days_30, this would silently keep for a
		// month something a person asked to be thrown away.
		res, err := s.IngestMemo(ctx, Arrival{
			AuthorID: author, ContentHash: hash, ByteSize: 1,
			Source: SourceCopyparty, SourceRef: "/data/memos/discard-me.opus",
		})
		if err != nil {
			t.Fatalf("rescan: %v", err)
		}
		if res.Memo.Retention != RetentionDiscardNow {
			t.Errorf("retention = %s, want discard_now: a default outranked an authored choice",
				res.Memo.Retention)
		}
	})

	t.Run("a discarded memo is not resurrected", func(t *testing.T) {
		hash := hashOf("already gone")
		first := Arrival{AuthorID: author, ContentHash: hash, ByteSize: 1,
			Source: SourceUpload, IdempotencyKey: "k-4444444444444444"}
		res, err := s.IngestMemo(ctx, first)
		if err != nil {
			t.Fatalf("ingest: %v", err)
		}
		if _, err := s.AdvanceMemoState(ctx, res.Memo.ID, StateCaptured, StateDiscarded, "user asked"); err != nil {
			t.Fatalf("discard: %v", err)
		}

		late := first
		late.Retention, late.IdempotencyKey = RetentionForever, "k-5555555555555555"
		after, err := s.IngestMemo(ctx, late)
		if err != nil {
			t.Fatalf("late arrival: %v", err)
		}
		if after.Memo.Retention == RetentionForever {
			t.Error("a forever arrival ratcheted a discarded memo")
		}
		if after.Memo.State != StateDiscarded {
			t.Errorf("state = %s, want discarded", after.Memo.State)
		}
	})
}

// Done when #10: identity is author-scoped. Forwarding somebody's recording
// records it under the forwarder rather than silently re-attributing the
// original.
func TestIdenticalBytesFromTwoAuthorsAreTwoMemos(t *testing.T) {
	s, ctx := newTestStore(t)
	a := newAuthor(t, s, ctx, "one@x")
	b := newAuthor(t, s, ctx, "two@x")
	hash := hashOf("the same audio, forwarded")

	ra, err := s.IngestMemo(ctx, Arrival{AuthorID: a, ContentHash: hash, ByteSize: 7,
		Source: SourceUpload, IdempotencyKey: "k-6666666666666666"})
	if err != nil {
		t.Fatalf("author a: %v", err)
	}
	rb, err := s.IngestMemo(ctx, Arrival{AuthorID: b, ContentHash: hash, ByteSize: 7,
		Source: SourceUpload, IdempotencyKey: "k-7777777777777777"})
	if err != nil {
		t.Fatalf("author b: %v", err)
	}
	if ra.Memo.ID == rb.Memo.ID {
		t.Fatal("two authors collapsed onto one memo")
	}
	if n := countMemos(t, s, ctx); n != 2 {
		t.Errorf("memos = %d, want 2", n)
	}
}

// Done when #11: an author the corpus references cannot be deleted. The FK is
// RESTRICT, and the 23503 becomes a typed error rather than a 500.
func TestAuthorWithMemosCannotBeDeleted(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "keeper@x")

	if _, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("worth keeping"), ByteSize: 3,
		Source: SourceUpload, IdempotencyKey: "k-8888888888888888",
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if err := s.DeleteUser(ctx, author); !errors.Is(err, ErrAuthorHasMemos) {
		t.Fatalf("DeleteUser = %v, want ErrAuthorHasMemos", err)
	}

	// And an author with none still deletes.
	empty := newAuthor(t, s, ctx, "empty@x")
	if err := s.DeleteUser(ctx, empty); err != nil {
		t.Errorf("DeleteUser on an author with no memos = %v, want nil", err)
	}
}

// CHRN-18's Done-when #12 said "the tier-1 role cannot read the corpus", and
// CHRN-32's ruling R4 SUPERSEDES THAT by decision, so this test now asserts the
// line that replaced it rather than the one it was written for.
//
// Why the old line could not survive: CLAUDE.md defines tier 1 to include
// "whatever Chronicle derives from its own corpus -- Scribe proposals,
// extracted entities, search indexes", and all three derive FROM tier 2. A
// tier-1 role that cannot read tier 2 makes the second half of tier 1's own
// definition unimplementable. Migration 0007 grants SELECT on tier2.memos and
// tier2.transcripts, and nothing else.
//
// The invariant is untouched, because the invariant is about writes: "no tier-1
// write path can reach a tier-2 table". So this test now proves three things
// where it used to prove one:
//
//  1. memos is READABLE           -- R4's grant is actually applied
//  2. memos is NOT WRITABLE       -- the line, unchanged
//  3. memo_arrivals is UNREACHABLE -- the grant is two tables and not a schema
//
// (3) is the one that would rot silently. 0007 deliberately uses no ALTER
// DEFAULT PRIVILEGES, so a tier-2 table added later stays unreadable until
// somebody grants it by name; memo_arrivals is the standing proof of that.
//
// Skips without a tier-1 DSN rather than passing vacuously.
func TestTier1RoleReadsTheCorpusAndCannotWriteIt(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "tier@x")
	if _, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("private"), ByteSize: 5,
		Source: SourceUpload, IdempotencyKey: "k-9999999999999999",
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	dsn := os.Getenv("CHRONICLE_TEST_TIER1_DATABASE_URL")
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_TIER1_DATABASE_URL not set; skipping tier-isolation test")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as chronicle_tier1: %v", err)
	}
	defer pool.Close()

	// Positive control, for the same reason CHRN-71's has one: without it every
	// assertion below would also pass on a connection that never worked.
	var role string
	if err := pool.QueryRow(ctx, `SELECT current_user`).Scan(&role); err != nil {
		t.Fatalf("the tier-1 connection does not work, so nothing below proves anything: %v", err)
	}
	if role != "chronicle_tier1" {
		t.Fatalf("connected as %q, want chronicle_tier1", role)
	}
	// ...and that the role can reach its OWN schema. Without this the whole
	// test would also pass against a role holding no privileges anywhere,
	// which proves nothing about the boundary.
	var canReachTier1 bool
	if err := pool.QueryRow(ctx,
		`SELECT has_schema_privilege(current_user, 'tier1', 'USAGE')`).Scan(&canReachTier1); err != nil {
		t.Fatalf("check tier1 privilege: %v", err)
	}
	if !canReachTier1 {
		t.Fatal("chronicle_tier1 cannot reach its own schema; the grants in 0001 are not applied")
	}

	// (1) R4's grant. Scribe joins these two to find what to route, so a
	// failure here is Scribe unable to run at all.
	for _, q := range []string{
		`SELECT count(*) FROM tier2.memos`,
		`SELECT content_hash FROM tier2.memos LIMIT 1`,
	} {
		var got any
		if err := pool.QueryRow(ctx, q).Scan(&got); err != nil {
			t.Errorf("chronicle_tier1 could not run %q: %v — R4's grant is not applied", q, err)
		}
	}

	// (3) And the grant stops at two tables. Every probe scans into `any`, so
	// the refusal cannot come from a type mismatch instead of from privileges.
	for _, q := range []string{
		`SELECT count(*) FROM tier2.memo_arrivals`,
		`SELECT idempotency_key FROM tier2.memo_arrivals LIMIT 1`,
	} {
		var got any
		err := pool.QueryRow(ctx, q).Scan(&got)
		if err == nil {
			t.Errorf("chronicle_tier1 executed %q; 0007 granted a schema where it should have "+
				"granted two tables", q)
			continue
		}
		// "relation does not exist" would also be non-nil, and would mean this
		// test passes because the migration never ran.
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("chronicle_tier1 running %q failed with %v; want a permission denial", q, err)
		}
	}

	// (2) THE LINE. Doctrine is about write paths reaching tier-2 tables, and
	// this is the assertion R4 was argued to leave untouched.
	if _, err := pool.Exec(ctx,
		`INSERT INTO tier2.memos (author_id, content_hash, byte_size) VALUES ($1, $2, 1)`,
		author, hashOf("forged")); err == nil {
		t.Error("chronicle_tier1 wrote a memo; a tier-1 path reached a tier-2 table")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("tier-1 write failed with %v; want a permission denial", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE tier2.memos SET state = 'triaged'`); err == nil {
		t.Error("chronicle_tier1 marked a memo triaged; the acceptance is a tier-2 write and " +
			"a router must not be able to make it")
	}
	_ = s
}

// The claim primitive. Decision §7 assigns claiming to a compare-and-swap so
// two workers cannot both take one memo — and the trigger cannot supply that on
// its own, because its edge check is skipped when the state does not change.
func TestClaimIsCompareAndSwap(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "claim@x")

	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("one memo, many workers"), ByteSize: 12,
		Source: SourceUpload, IdempotencyKey: "k-claimclaimclaim",
	})
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	id := res.Memo.ID
	if _, err := s.AdvanceMemoState(ctx, id, StateCaptured, StateQueued, ""); err != nil {
		t.Fatalf("queue: %v", err)
	}

	const workers = 6
	won := make([]bool, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.AdvanceMemoState(ctx, id, StateQueued, StateTranscribing, "")
			won[i] = err == nil
		}(i)
	}
	wg.Wait()

	claims := 0
	for _, w := range won {
		if w {
			claims++
		}
	}
	if claims != 1 {
		t.Errorf("%d workers claimed the same memo, want exactly 1", claims)
	}

	// And a stale `from` is refused with the reason, not silently ignored.
	_, err = s.AdvanceMemoState(ctx, id, StateQueued, StateTranscribing, "")
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("stale claim = %v, want ErrIllegalTransition", err)
	}
	if err != nil && !strings.Contains(err.Error(), StateTranscribing) {
		t.Errorf("error %q does not say what state the memo is actually in", err)
	}
}

// A malformed arrival is a caller's mistake, so it must be ErrInvalidInput —
// which the handler answers 4xx to — rather than a column CHECK surfacing as a
// generic error and becoming a 500.
func TestMalformedArrivalIsACallerError(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "malformed@x")
	good := Arrival{
		AuthorID: author, ContentHash: hashOf("fine"), ByteSize: 10,
		Source: SourceUpload, IdempotencyKey: "k-aaaaaaaaaaaaaaaa",
	}

	cases := map[string]func(*Arrival){
		"empty hash":     func(a *Arrival) { a.ContentHash = "" },
		"short hash":     func(a *Arrival) { a.ContentHash = "abc123" },
		"uppercase hex":  func(a *Arrival) { a.ContentHash = strings.ToUpper(hashOf("fine")) },
		"non-hex":        func(a *Arrival) { a.ContentHash = strings.Repeat("z", 64) },
		"zero bytes":     func(a *Arrival) { a.ByteSize = 0 },
		"negative bytes": func(a *Arrival) { a.ByteSize = -1 },
		"no author":      func(a *Arrival) { a.AuthorID = uuid.Nil },
		"unknown source": func(a *Arrival) { a.Source = "carrier-pigeon" },
		"bad retention":  func(a *Arrival) { a.Retention = "eventually" },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			bad := good
			break_(&bad)
			if _, err := s.IngestMemo(ctx, bad); !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
	if n := countMemos(t, s, ctx); n != 0 {
		t.Errorf("memos = %d, want 0: a rejected arrival created a row", n)
	}
}

// CHRN-21 — recording what the audio is. No migration: these three columns have
// been nullable and unset since 0003, which is why the ticket blocked nothing.

func TestSetMemoAudioInfoFillsTheColumns(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "describer@example.com")

	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("recording"), ByteSize: 4096,
		Source: SourceUpload, IdempotencyKey: "idem-key-describe-01",
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	if res.Memo.DurationMS != nil || res.Memo.Codec != nil || res.Memo.SampleRateHz != nil {
		t.Fatal("a fresh memo already carries audio metadata")
	}

	got, err := s.SetMemoAudioInfo(ctx, res.Memo.ID, AudioInfo{
		DurationMS: 37500, Codec: "opus", SampleRateHz: 48000,
	})
	if err != nil {
		t.Fatalf("SetMemoAudioInfo: %v", err)
	}
	if got.DurationMS == nil || *got.DurationMS != 37500 {
		t.Fatalf("duration %v, want 37500", got.DurationMS)
	}
	if got.Codec == nil || *got.Codec != "opus" {
		t.Fatalf("codec %v, want opus", got.Codec)
	}
	if got.SampleRateHz == nil || *got.SampleRateHz != 48000 {
		t.Fatalf("sample rate %v, want 48000", got.SampleRateHz)
	}
	// It must not have disturbed anything the guard protects.
	if got.ContentHash != res.Memo.ContentHash || got.ByteSize != res.Memo.ByteSize ||
		!got.CapturedAt.Equal(res.Memo.CapturedAt) || got.State != res.Memo.State {
		t.Fatal("describing a memo moved its identity, capture time or state")
	}
}

// An absent input sample rate is stored as NULL, not as a confident zero. The
// column has no CHECK forbidding zero, so nothing else would catch it.
func TestSetMemoAudioInfoStoresAnUnknownRateAsNull(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "describer@example.com")

	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("norate"), ByteSize: 512,
		Source: SourceUpload, IdempotencyKey: "idem-key-describe-02",
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}

	got, err := s.SetMemoAudioInfo(ctx, res.Memo.ID, AudioInfo{DurationMS: 1000, Codec: "opus"})
	if err != nil {
		t.Fatalf("SetMemoAudioInfo: %v", err)
	}
	if got.SampleRateHz != nil {
		t.Fatalf("an unknown sample rate was stored as %d rather than NULL", *got.SampleRateHz)
	}
}

// 0003's CHECK is duration_ms > 0. Refused in Go so a bad probe is a caller
// error with a sentence, not a wrapped constraint violation.
func TestSetMemoAudioInfoRejectsANonPositiveDuration(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "describer@example.com")

	res, err := s.IngestMemo(ctx, Arrival{
		AuthorID: author, ContentHash: hashOf("zero"), ByteSize: 128,
		Source: SourceUpload, IdempotencyKey: "idem-key-describe-03",
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	for _, d := range []int32{0, -1} {
		if _, err := s.SetMemoAudioInfo(ctx, res.Memo.ID, AudioInfo{DurationMS: d, Codec: "opus"}); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("duration %d: want ErrInvalidInput, got %v", d, err)
		}
	}
	if _, err := s.SetMemoAudioInfo(ctx, uuid.New(), AudioInfo{DurationMS: 100, Codec: "opus"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("describing a memo that does not exist: want ErrNotFound, got %v", err)
	}
}
