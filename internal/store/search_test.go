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

// ── CHRN-116 — the notes-only search ────────────────────────────────────────
//
// Nothing above this line was edited: Search, searchSQL, their tests and the
// explain helper are the owner path and stay as they were.

// No database. THE STATEMENT CANNOT REACH A TRANSCRIPT BECAUSE IT DOES NOT
// NAME ONE: the guarantee is the text of the constant, so the text is what is
// asserted. A later edit that points SearchNotes at the union fails here
// before it runs anywhere.
func TestSearchNotesStatementNamesNoTranscriptTable(t *testing.T) {
	lower := strings.ToLower(searchNotesSQL)
	for _, banned := range []string{"transcript", "memo"} {
		if strings.Contains(lower, banned) {
			t.Errorf("searchNotesSQL contains %q:\n%s", banned, searchNotesSQL)
		}
	}
	for _, want := range []string{"tier2.notes", "tier2.note_revisions"} {
		if !strings.Contains(searchNotesSQL, want) {
			t.Errorf("searchNotesSQL does not read %s:\n%s", want, searchNotesSQL)
		}
	}
	// Every relation after FROM or JOIN, by name, so a third table cannot
	// arrive in a spelling the substring checks above would miss. `q` is the
	// statement's own CTE holding the parsed query.
	fields := strings.Fields(searchNotesSQL)
	var relations []string
	for i, f := range fields {
		if (f == "FROM" || f == "JOIN") && i+1 < len(fields) && fields[i+1] != "q" {
			relations = append(relations, fields[i+1])
		}
	}
	if got := strings.Join(relations, " "); got != "tier2.notes tier2.note_revisions" {
		t.Errorf("searchNotesSQL reads %q, want exactly tier2.notes and tier2.note_revisions", got)
	}
	// The control: the owner statement does name the transcripts table, so the
	// absence above is not an artefact of how the check reads a statement.
	if !strings.Contains(searchSQL, "tier2.transcripts") {
		t.Error("searchSQL no longer names tier2.transcripts; this test's control is stale")
	}
}

// No database either, so it runs where the EXPLAIN test skips. The notes
// statement COPIES searchSQL's tsvector expression rather than sharing it, and
// a copy that drifts by one character costs the index silently. Whitespace is
// collapsed because the two statements indent differently; nothing else is.
func TestSearchNotesSharesTheIndexedExpression(t *testing.T) {
	const expr = `setweight(to_tsvector('english', r.title), 'A') || setweight(to_tsvector('english', r.body), 'B')`
	const headline = `ts_headline('english', r.body, q.tsq, 'MaxFragments=2,MinWords=6,MaxWords=20,FragmentDelimiter= … ')`
	flat := func(s string) string { return strings.Join(strings.Fields(s), " ") }

	for name, stmt := range map[string]string{"searchSQL": searchSQL, "searchNotesSQL": searchNotesSQL} {
		// Twice: once ranked, once matched. The match is the one the index
		// serves; the rank is the one that orders.
		if got := strings.Count(flat(stmt), expr); got != 2 {
			t.Errorf("%s carries the indexed notes expression %d times, want 2 (rank and match)", name, got)
		}
		if !strings.Contains(flat(stmt), headline) {
			t.Errorf("%s does not carry the shared ts_headline call", name)
		}
	}
	// And nothing else builds a tsvector in the notes statement: a second,
	// different expression would be the drift this test exists to catch.
	if got := strings.Count(searchNotesSQL, "to_tsvector("); got != 4 {
		t.Errorf("searchNotesSQL calls to_tsvector %d times, want 4 (title and body, twice)", got)
	}
}

// The store-level statement of the property CHRN-116 exists for: a transcript
// that matches is not returned, whoever said it.
func TestSearchNotesNeverReturnsATranscript(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "notes-only@example.com")
	n := mkNote(t, s, ctx, page, author, "Birds", "a chiffchaff sang in the hedge")
	memo := newTranscribableMemo(t, s, ctx, "notes-only-memo@example.com")
	transcribe(t, s, ctx, memo, "whisper.cpp/small.en", "I heard a chiffchaff and a zebrafinch", false)

	// The control: the owner's search finds the transcript, so it is findable
	// and its absence below means something.
	all, err := s.Search(ctx, "chiffchaff", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := kinds(all); got[HitNote] != 1 || got[HitTranscript] != 1 {
		t.Fatalf("control: Search kinds = %v, want one note and one transcript", got)
	}

	hits, err := s.SearchNotes(ctx, "chiffchaff", 10)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if len(hits) != 1 || hits[0].NoteID != n.ID {
		t.Fatalf("SearchNotes = %+v, want only note %s", hits, n.ID)
	}
	if hits[0].Ref() != n.Ref() || hits[0].Title != "Birds" || hits[0].PageID != page {
		t.Errorf("hit does not identify the note: %+v", hits[0])
	}
	if !strings.Contains(hits[0].Snippet, "<b>chiffchaff</b>") {
		t.Errorf("snippet does not mark the match: %q", hits[0].Snippet)
	}

	// A word only the transcript holds finds nothing at all.
	if hits, err := s.SearchNotes(ctx, "zebrafinch", 10); err != nil {
		t.Fatalf("SearchNotes: %v", err)
	} else if len(hits) != 0 {
		t.Errorf("a transcript-only word found %+v", hits)
	}
}

// What Search's notes arm does, SearchNotes does: live text of live notes.
func TestSearchNotesFindsOnlyLiveText(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "live@example.com")
	kept := mkNote(t, s, ctx, page, author, "Kept", "the original mentions aardvarks")
	gone := mkNote(t, s, ctx, page, author, "Gone", "this one mentions buffalo and is deleted")

	if _, err := s.AppendRevision(ctx, kept.ID, NewRevision{
		AuthorID: author, ConfirmedBy: author, Title: "Kept", Body: "the revision mentions buffalo instead",
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	if err := s.SoftDeleteNote(ctx, gone.ID, author); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}

	if hits, err := s.SearchNotes(ctx, "aardvarks", 10); err != nil {
		t.Fatalf("SearchNotes: %v", err)
	} else if len(hits) != 0 {
		t.Errorf("superseded text is still findable: %+v", hits)
	}
	hits, err := s.SearchNotes(ctx, "buffalo", 10)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if len(hits) != 1 || hits[0].NoteID != kept.ID {
		t.Errorf("buffalo = %+v, want only the live note %s (the soft-deleted one is %s)", hits, kept.ID, gone.ID)
	}
}

func TestSearchNotesRanksATitleAboveABodyAndHonoursTheLimit(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "notes-rank@example.com")
	mkNote(t, s, ctx, page, author, "Something else", "this one only mentions zeppelins in passing")
	mkNote(t, s, ctx, page, author, "Zeppelins", "a note actually about them")

	hits, err := s.SearchNotes(ctx, "zeppelins", 10)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if len(hits) != 2 || hits[0].Title != "Zeppelins" {
		t.Fatalf("hits = %+v, want two with the TITLE match first", hits)
	}
	if one, err := s.SearchNotes(ctx, "zeppelins", 1); err != nil || len(one) != 1 || one[0].Title != "Zeppelins" {
		t.Errorf("limit 1 = %+v (%v), want the best hit alone", one, err)
	}
}

func TestSearchNotesReportsAnEmptyQueryAsSuch(t *testing.T) {
	s, ctx := newTestStore(t)
	for _, q := range []string{"", "   ", "!!!", "--- ...", `""`} {
		if _, err := s.SearchNotes(ctx, q, 10); !errors.Is(err, ErrEmptyQuery) {
			t.Errorf("SearchNotes(%q) err = %v, want ErrEmptyQuery", q, err)
		}
	}
	// Garbage does not raise, as with Search.
	if _, err := s.SearchNotes(ctx, `foo AND ) OR "unclosed`, 10); err != nil {
		t.Errorf("malformed query raised: %v", err)
	}
}

// The copy of the tsvector expression is held to 0012's index the way the
// original is: by the plan, over the same seed. Transcripts are seeded too,
// with the needle, so "does not mention transcripts" is a claim about a
// statement that had every chance to read them.
func TestSearchNotesUsesTheIndexAtScale(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "notes-scale@example.com")

	const seed = 5000
	seedNotes(t, s, ctx, page, author, seed)
	seedTranscripts(t, s, ctx, seed)

	if _, err := s.Pool().Exec(ctx,
		`ANALYZE tier2.notes, tier2.note_revisions, tier2.transcripts`); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	plan := explainStatement(t, s, ctx, searchNotesSQL, "chiffchaff", 50)
	if !strings.Contains(plan, "note_revisions_fts") {
		t.Errorf("the planner is not using note_revisions_fts at %d rows:\n%s", seed, plan)
	}
	if strings.Contains(plan, "Seq Scan on note_revisions") {
		t.Errorf("a sequential scan survived at %d rows:\n%s", seed, plan)
	}
	if strings.Contains(plan, "transcripts") {
		t.Errorf("the notes statement's plan reaches transcripts:\n%s", plan)
	}

	hits, err := s.SearchNotes(ctx, "chiffchaff", 50)
	if err != nil {
		t.Fatalf("SearchNotes: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("hits = %d, want the 1 note needle (the transcript needle is not this statement's)", len(hits))
	}
}

// explainStatement is explain for a statement passed in. explain above
// hard-codes searchSQL and is left exactly as it was.
func explainStatement(t *testing.T, s *Store, ctx context.Context, stmt string, args ...any) string {
	t.Helper()
	rows, err := s.Pool().Query(ctx, "EXPLAIN "+stmt, args...)
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
