package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// CHRN-41's `Done when`: a phrase spoken into a memo in March is findable in
// September, results say whether they are authored or transcribed, and search
// stays fast at 10× the current corpus.

func transcribe(t *testing.T, s *Store, ctx context.Context, memo Memo, model, text string, partial bool) {
	t.Helper()
	if _, err := s.RecordTranscript(ctx, TranscriptInput{
		MemoID: memo.ID, Text: text, Model: model, Backend: "whisper.cpp", Partial: partial,
	}); err != nil {
		t.Fatalf("RecordTranscript(%s): %v", model, err)
	}
}

func kinds(hits []SearchHit) map[string]int {
	m := map[string]int{}
	for _, h := range hits {
		m[h.Kind]++
	}
	return m
}

// THE HEADLINE CLAIM. The audio is gone — CHRN-22 pruned it at thirty days —
// and the transcript is the only remaining account of what was said. If this
// does not work, six months of untriaged memos are unreachable.
func TestAPhraseSpokenInMarchIsFindableInSeptember(t *testing.T) {
	s, ctx := newTestStore(t)
	memo := newTranscribableMemo(t, s, ctx, "march@example.com")
	transcribe(t, s, ctx, memo, "whisper.cpp/small.en",
		"I keep coming back to the idea of a pocket recorder that files itself", false)

	// The audio is pruned. captured_at is deliberately NOT backdated here:
	// tier2.memos_guard refuses to move it (CH002), because a prune deadline a
	// caller can move is one that can be moved onto today. The point stands
	// without it — what makes the words findable is the transcript, and the
	// recording it came from is now gone.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE tier2.memos SET audio_pruned_at = now() WHERE id = $1`, memo.ID); err != nil {
		t.Fatalf("prune: %v", err)
	}

	hits, err := s.Search(ctx, "pocket recorder", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1: %+v", len(hits), hits)
	}
	if hits[0].Kind != HitTranscript {
		t.Errorf("kind = %q, want %q", hits[0].Kind, HitTranscript)
	}
	if hits[0].MemoID == nil || *hits[0].MemoID != memo.ID {
		t.Errorf("memo = %v, want %s", hits[0].MemoID, memo.ID)
	}
	if !strings.Contains(hits[0].Snippet, "pocket") {
		t.Errorf("snippet does not show the match: %q", hits[0].Snippet)
	}
}

// `Done when` #2 — one is what a person decided to write down, the other is
// what they happened to say into a phone, and a result set that cannot tell
// them apart is not usable.
func TestResultsSayWhetherTheyAreAuthoredOrTranscribed(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "both@example.com")
	mkNote(t, s, ctx, page, author, "Naming", "the estate uses lowercase slugs everywhere")
	memo := newTranscribableMemo(t, s, ctx, "both-memo@example.com")
	transcribe(t, s, ctx, memo, "whisper.cpp/small.en", "something about lowercase slugs again", false)

	hits, err := s.Search(ctx, "slugs", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := kinds(hits); got[HitNote] != 1 || got[HitTranscript] != 1 {
		t.Fatalf("kinds = %v, want one of each", got)
	}
	for _, h := range hits {
		switch h.Kind {
		case HitNote:
			if h.NoteID == nil || h.Number == nil || h.Ref() == "" {
				t.Errorf("note hit has no identity: %+v", h)
			}
			if h.MemoID != nil {
				t.Errorf("note hit carries a memo: %+v", h)
			}
		case HitTranscript:
			if h.MemoID == nil || h.Model == "" {
				t.Errorf("transcript hit has no memo or model: %+v", h)
			}
			if h.NoteID != nil || h.Ref() != "" {
				t.Errorf("transcript hit carries a note: %+v", h)
			}
		}
	}
}

// THE ONE THE INDEX SHAPE MAKES POSSIBLE TO GET WRONG. The GIN index covers
// every revision, because an index predicate cannot reach through
// notes.current_revision_id. Superseded text is therefore in the index, and it
// is the join in searchSQL — not the index — that keeps it out of results. Get
// that wrong and search returns sentences the note no longer contains.
func TestSupersededTextIsNotFound(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "superseded@example.com")
	n := mkNote(t, s, ctx, page, author, "Draft", "the original mentions aardvarks")

	if _, err := s.AppendRevision(ctx, n.ID, NewRevision{
		AuthorID: author, ConfirmedBy: author, Title: "Draft", Body: "the revision mentions buffalo instead",
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}

	if hits, err := s.Search(ctx, "aardvarks", 10); err != nil {
		t.Fatalf("Search: %v", err)
	} else if len(hits) != 0 {
		t.Errorf("superseded text is still findable: %+v", hits)
	}
	if hits, err := s.Search(ctx, "buffalo", 10); err != nil {
		t.Fatalf("Search: %v", err)
	} else if len(hits) != 1 {
		t.Errorf("current text hits = %d, want 1", len(hits))
	}

	// The old revision is still READABLE — history is intact, it is only
	// search that shows the live text.
	revs, err := s.NoteRevisions(ctx, n.ID)
	if err != nil || len(revs) != 2 || !strings.Contains(revs[0].Body, "aardvarks") {
		t.Errorf("history lost the superseded text: %+v (%v)", revs, err)
	}
}

// A memo transcribed by two models is one thing somebody said, not two.
func TestOneMemoYieldsOneHitAcrossModels(t *testing.T) {
	s, ctx := newTestStore(t)
	memo := newTranscribableMemo(t, s, ctx, "twomodels@example.com")
	transcribe(t, s, ctx, memo, "whisper.cpp/small.en", "the quick brown fox jumps", false)
	transcribe(t, s, ctx, memo, "whisper.cpp/medium.en", "the quick brown fox jumps over", false)

	hits, err := s.Search(ctx, "brown fox", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1 — one memo is one result: %+v", len(hits), hits)
	}
	if hits[0].Model == "" {
		t.Error("the surviving hit does not say which decode matched")
	}
}

// A partial is what somebody said up to the point the decode gave out, and it
// is still the only record of that much.
func TestPartialTranscriptsAreSearchable(t *testing.T) {
	s, ctx := newTestStore(t)
	memo := newTranscribableMemo(t, s, ctx, "partial@example.com")
	transcribe(t, s, ctx, memo, "whisper.cpp/small.en", "half a sentence about zeppelins", true)

	hits, err := s.Search(ctx, "zeppelins", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("a partial transcript is not searchable: %+v", hits)
	}
}

func TestTitleOutranksBody(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "rank@example.com")
	mkNote(t, s, ctx, page, author, "Something else", "this one only mentions zeppelins in passing")
	mkNote(t, s, ctx, page, author, "Zeppelins", "a note actually about them")

	hits, err := s.Search(ctx, "zeppelins", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	if hits[0].Title != "Zeppelins" {
		t.Errorf("ranked %q first, want the note whose TITLE matches", hits[0].Title)
	}
}

// An empty question is not an empty corpus, and the two must not look alike.
func TestAnEmptyQueryIsReportedAsSuch(t *testing.T) {
	s, ctx := newTestStore(t)
	for _, q := range []string{"", "   ", "!!!", "--- ...", `""`} {
		if _, err := s.Search(ctx, q, 10); !errors.Is(err, ErrEmptyQuery) {
			t.Errorf("Search(%q) err = %v, want ErrEmptyQuery", q, err)
		}
	}
}

// websearch_to_tsquery is the syntax people already type, and it never raises
// on malformed input — which matters when the input is whatever somebody put
// in a box.
func TestWebsearchSyntaxWorks(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "syntax@example.com")
	mkNote(t, s, ctx, page, author, "Alpha", "the pocket recorder files itself")
	mkNote(t, s, ctx, page, author, "Beta", "a recorder that does not file anything")

	// A quoted phrase is a phrase.
	if hits, err := s.Search(ctx, `"pocket recorder"`, 10); err != nil {
		t.Fatalf("phrase: %v", err)
	} else if len(hits) != 1 || hits[0].Title != "Alpha" {
		t.Errorf("phrase search = %+v, want only Alpha", hits)
	}
	// Bare words are ANDed. Note the terms are chosen so that stemming cannot
	// make this pass by accident: "files" and "file" share a stem, so
	// `recorder files` would match Beta too and prove nothing about AND.
	if hits, err := s.Search(ctx, "recorder pocket", 10); err != nil {
		t.Fatalf("and: %v", err)
	} else if len(hits) != 1 || hits[0].Title != "Alpha" {
		t.Errorf("AND search = %+v, want only Alpha", hits)
	}
	// Negation excludes.
	if hits, err := s.Search(ctx, "recorder -pocket", 10); err != nil {
		t.Fatalf("negation: %v", err)
	} else if len(hits) != 1 || hits[0].Title != "Beta" {
		t.Errorf("negated search = %+v, want only Beta", hits)
	}
	// Garbage does not raise.
	if _, err := s.Search(ctx, `foo AND ) OR "unclosed`, 10); err != nil {
		t.Errorf("malformed query raised: %v", err)
	}
}

// `Done when` #3 — "search stays fast at 10× the current corpus".
//
// ASSERTED AS A PLAN, MEASURED AS A NUMBER. A wall-clock assertion alone would
// be a flake on a busy box and would still pass if the planner quietly stopped
// using the index — a sequential scan over a small table is fast too, right up
// until it is not. So this asserts the structural claim (both GIN indexes are
// used) and logs the timing for the record.
func TestSearchUsesTheIndexAtScale(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "scale@example.com")

	const seed = 5000 // the estate's corpus is ~17 real memos; this is ~300×
	seedNotes(t, s, ctx, page, author, seed)
	seedTranscripts(t, s, ctx, seed)

	if _, err := s.Pool().Exec(ctx,
		`ANALYZE tier2.notes, tier2.note_revisions, tier2.transcripts`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	plan := explain(t, s, ctx, "chiffchaff")
	for _, want := range []string{"note_revisions_fts", "transcripts_fts"} {
		if !strings.Contains(plan, want) {
			t.Errorf("the planner is not using %s at %d rows:\n%s", want, seed, plan)
		}
	}
	if strings.Contains(plan, "Seq Scan on note_revisions") ||
		strings.Contains(plan, "Seq Scan on transcripts") {
		t.Errorf("a sequential scan survived at %d rows:\n%s", seed, plan)
	}

	start := time.Now()
	hits, err := s.Search(ctx, "chiffchaff", 50)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	t.Logf("search over %d notes + %d transcripts: %d hits in %s", seed, seed, len(hits), elapsed)
	if len(hits) != 2 {
		t.Errorf("hits = %d, want the 2 needles", len(hits))
	}
	// Generous by two orders of magnitude: this catches a plan collapse, not a
	// busy machine.
	if elapsed > 2*time.Second {
		t.Errorf("search took %s over %d rows", elapsed, seed)
	}
}

// seedNotes bulk-inserts notes and their first revisions. Raw SQL rather than
// CreateNote: 5000 transactions would make this a test of round-trip latency.
// The deferred foreign keys make the revision-then-note order work inside one
// statement, exactly as CreateNote relies on.
func seedNotes(t *testing.T, s *Store, ctx context.Context, page, author uuid.UUID, n int) {
	t.Helper()
	if _, err := s.Pool().Exec(ctx, `
		WITH ids AS (
		    SELECT gen_random_uuid() AS nid, gen_random_uuid() AS rid, i
		      FROM generate_series(1, $1) i
		), r AS (
		    INSERT INTO tier2.note_revisions
		                (id, note_id, seq, title, body, author_id, confirmed_by)
		    SELECT rid, nid, 1,
		           'seed note ' || i,
		           'filler prose about estates and conventions and services number ' || i ||
		           CASE WHEN i = 1 THEN ' chiffchaff' ELSE '' END,
		           $3, $3
		      FROM ids
		)
		INSERT INTO tier2.notes (id, page_id, current_revision_id, author_id)
		SELECT nid, $2, rid, $3 FROM ids`, n, page, author); err != nil {
		t.Fatalf("seed notes: %v", err)
	}
}

// seedTranscripts bulk-inserts one memo and one transcript per row. Memos need
// a distinct content_hash each, which the index makes from the series.
func seedTranscripts(t *testing.T, s *Store, ctx context.Context, n int) {
	t.Helper()
	author := newAuthor(t, s, ctx, "scale-memos@example.com")
	if _, err := s.Pool().Exec(ctx, `
		WITH m AS (
		    INSERT INTO tier2.memos (author_id, content_hash, byte_size)
		    SELECT $2, lpad(to_hex(i), 64, '0'), 1024 FROM generate_series(1, $1) i
		    RETURNING id
		), numbered AS (
		    SELECT id, row_number() OVER () AS i FROM m
		)
		INSERT INTO tier2.transcripts (memo_id, text, partial, model, backend)
		SELECT id,
		       'spoken filler about the estate and the wiki and recordings number ' || i ||
		       CASE WHEN i = 1 THEN ' chiffchaff' ELSE '' END,
		       false, 'whisper.cpp/small.en', 'whisper.cpp'
		  FROM numbered`, n, author); err != nil {
		t.Fatalf("seed transcripts: %v", err)
	}
}

// explain returns the query plan for the real search statement, so the
// assertion is about the statement that ships rather than about a paraphrase.
func explain(t *testing.T, s *Store, ctx context.Context, query string) string {
	t.Helper()
	rows, err := s.Pool().Query(ctx, "EXPLAIN "+searchSQL, query, 50)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("EXPLAIN scan: %v", err)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	return b.String()
}
