package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-107's Done-when against a REAL store, because four of its clauses are
// properties of the database rather than of this package:
//
//   - `retention_status` is store.RetentionStatus's CASE — the same clause the
//     pruner sweeps with — and the whole point of reading it rather than
//     recomputing one is that the two cannot drift. A fake that mirrors the
//     CASE proves the shape; only this proves the clause.
//   - WHICH transcript row is answered is `ORDER BY partial ASC,
//     transcribed_at DESC` over rows a real RecordTranscript wrote.
//   - `tier2.note_revisions.memo_id` is UNIQUE where not null, so "one memo
//     produces at most one revision" is the schema's claim and not a
//     convention this test could hold up on its own.
//   - and the durability floor behind `awaiting_transcript` is DurableClause,
//     split_part and all.
//
// It resets the shared chronicle_test like internal/store and wiki_db_test.go
// do, which is why verify.sh runs one test binary at a time.

type memoDBRig struct {
	st    *store.Store
	ctx   context.Context
	h     http.Handler
	audio *audio.Store

	owner, other, agent          store.User
	ownerTok, otherTok, agentTok string
	page                         store.Page
}

func realMemos(t *testing.T) *memoDBRig {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.New(pool)

	rig := &memoDBRig{st: st, ctx: ctx}
	if rig.owner, err = st.GetOwner(ctx); err != nil {
		t.Fatalf("GetOwner: %v", err)
	}
	if rig.other, err = st.CreateUser(ctx, "other@example.test", "Another member", store.KindPerson); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if rig.agent, err = st.EnsureAgent(ctx, store.ScribeEmail, store.ScribeDisplayName); err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	for _, pair := range []struct {
		u   store.User
		tok *string
	}{{rig.owner, &rig.ownerTok}, {rig.other, &rig.otherTok}, {rig.agent, &rig.agentTok}} {
		// MintToken does not check kind (CHRN-44 flagged it for CHRN-65),
		// which is what lets this test hold an agent session at all.
		tok, err := st.MintToken(ctx, pair.u.ID, store.TokenSession, "test", nil)
		if err != nil {
			t.Fatalf("MintToken: %v", err)
		}
		*pair.tok = tok
	}
	if rig.page, err = st.CreatePage(ctx, nil, "estate"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	as, err := audio.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rig.audio = as

	rig.h = NewRouter(Deps{
		DB: st, Accounts: st, Logger: discardLogger(), Version: "test", SecureCookies: true,
		Wiki: st, Memos: st, Audio: as, LocalReferences: st,
	})
	return rig
}

// memo ingests one recording through the real store and puts its bytes where
// the layout says they go.
func (rig *memoDBRig) memo(t *testing.T, author store.User, body, filename string) store.Memo {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	res, err := rig.st.IngestMemo(rig.ctx, store.Arrival{
		AuthorID:         author.ID,
		ContentHash:      hex.EncodeToString(sum[:]),
		ByteSize:         int64(len(body)),
		Source:           store.SourceUpload,
		SourceRef:        "test",
		OriginalFilename: filename,
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	path, err := rig.audio.Path(audio.Ref{AuthorID: author.ID, ContentHash: res.Memo.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return res.Memo
}

func (rig *memoDBRig) transcribe(t *testing.T, m store.Memo, text, model string, partial bool, durationMS int64) store.Transcript {
	t.Helper()
	tr, err := rig.st.RecordTranscript(rig.ctx, store.TranscriptInput{
		MemoID: m.ID, Text: text, Partial: partial, Model: model, Backend: "vulkan",
		Segments:        []store.Segment{{StartMS: 0, EndMS: durationMS, Text: text}},
		AudioDurationMS: &durationMS,
	})
	if err != nil {
		t.Fatalf("RecordTranscript(%s, partial=%v): %v", model, partial, err)
	}
	return tr
}

func (rig *memoDBRig) get(t *testing.T, path, token, op string, want int) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	rig.h.ServeHTTP(rec, r)
	mustStatus(t, rec, want, op)
	return rec
}

// From a note's ref alone, every memo that produced one of its revisions —
// with a retention status the pruner would agree with, over the real clause.
func TestProvenanceOverTheRealStore(t *testing.T) {
	rig := realMemos(t)

	march := rig.memo(t, rig.owner, "a vague thought in March", "march.m4a")
	june := rig.memo(t, rig.owner, "a concrete one in June", "june.m4a")
	rig.transcribe(t, march, "a vague thought in March", "whisper.cpp/small.en", false, 104000)
	rig.transcribe(t, june, "a concrete one in June", "whisper.cpp/small.en", false, 61000)

	n, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
		Title: "Retention gates on a transcript", Body: "From the March memo.",
		MemoID: &march.ID, Verb: ptrTo(store.VerbCreate),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	// A directly-authored revision in between: it produced no memo and must
	// not appear in the list.
	if _, err := rig.st.AppendRevision(rig.ctx, n.ID, store.NewRevision{
		AuthorID: rig.owner.ID, ConfirmedBy: rig.owner.ID,
		Title: "Retention gates on a transcript", Body: "Typed, by hand.",
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}
	if _, err := rig.st.AppendRevision(rig.ctx, n.ID, store.NewRevision{
		AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
		Title: "Retention gates on a transcript", Body: "And from the June memo.",
		MemoID: &june.ID, Verb: ptrTo(store.VerbAppend),
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}

	rec := rig.get(t, "/notes/"+n.Ref()+"/provenance", rig.ownerTok, "getNoteProvenance", http.StatusOK)
	list := decodeInto[wire.ProvenanceList](t, rec)
	if len(list.Items) != 2 {
		t.Fatalf("items = %d, want 2 (three revisions, two of them from memos)", len(list.Items))
	}
	if list.Items[0].MemoId != march.ID || list.Items[1].MemoId != june.ID {
		t.Errorf("order = %v; oldest revision first", []uuid.UUID{list.Items[0].MemoId, list.Items[1].MemoId})
	}
	if list.Items[0].RevisionSeq != 1 || list.Items[1].RevisionSeq != 3 {
		t.Errorf("revision_seq = %d, %d; the typed revision is seq 2 and is absent from this list",
			list.Items[0].RevisionSeq, list.Items[1].RevisionSeq)
	}

	// THE SAME CLAUSE THE SWEEP EVALUATES. A durable transcript inside the
	// window is `scheduled`, and the date is captured_at + the window.
	first := list.Items[0]
	if first.RetentionStatus != wire.Scheduled {
		t.Errorf("retention_status = %q, want scheduled", first.RetentionStatus)
	}
	if first.PrunesAt == nil || !first.PrunesAt.Equal(march.CapturedAt.Add(audio.ProjectionWindow)) {
		t.Errorf("prunes_at = %v, want %v", first.PrunesAt, march.CapturedAt.Add(audio.ProjectionWindow))
	}
	// m4a with a NULL header duration: the shape of every memo in the live
	// corpus, answered off the transcript.
	if first.DurationMs == nil || *first.DurationMs != 104000 ||
		first.DurationSource == nil || *first.DurationSource != wire.MemoProvenanceDurationSourceTranscript {
		t.Errorf("duration = %v from %v; reading memos.duration_ms alone renders a dash for the whole corpus",
			first.DurationMs, first.DurationSource)
	}
	if !first.Transcript.Present || first.Transcript.Partial == nil || *first.Transcript.Partial {
		t.Errorf("transcript = %+v", first.Transcript)
	}

	// A note somebody typed: an empty list, not a 404.
	typed, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.owner.ID, ConfirmedBy: rig.owner.ID,
		Title: "Typed straight in", Body: "No memo behind this one.",
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	rec = rig.get(t, "/notes/"+typed.Ref()+"/provenance", rig.ownerTok, "getNoteProvenance", http.StatusOK)
	if items := decodeInto[wire.ProvenanceList](t, rec).Items; len(items) != 0 {
		t.Errorf("items = %d for a typed note", len(items))
	}
}

// Which row is answered, over rows RecordTranscript actually wrote — and the
// durability floor behind `awaiting_transcript`, which is DurableClause.
func TestTheTranscriptRowTheRealStoreAnswers(t *testing.T) {
	rig := realMemos(t)

	// TWO ROWS ON ONE MEMO: an incomplete run first, then a complete one.
	// The store refuses the reverse (CH005), which is why the order is this.
	two := rig.memo(t, rig.owner, "a thought, spoken", "two-rows.m4a")
	rig.transcribe(t, two, "a thought, spo", "whisper.cpp/small.en", true, 104000)
	complete := rig.transcribe(t, two, "a thought, spoken", "whisper.cpp/small.en", false, 104000)

	rec := rig.get(t, "/transcripts/"+two.ID.String(), rig.ownerTok, "getMemoTranscript", http.StatusOK)
	got := decodeInto[wire.MemoTranscript](t, rec)
	if got.Text != complete.Text || got.Partial {
		t.Errorf("transcript = %+v; `ORDER BY partial ASC, transcribed_at DESC` answers the complete row", got)
	}

	// A memo whose ONLY row is incomplete: the entry flags it, and the real
	// durability floor holds the audio back.
	partialOnly := rig.memo(t, rig.owner, "half a thought", "partial.m4a")
	rig.transcribe(t, partialOnly, "half a", "whisper.cpp/small.en", true, 40000)

	// And a memo with a complete transcript from a model BELOW the floor: the
	// row is answered, and the audio is still held. Existing store behaviour,
	// and the reason the entry carries no durability claim of its own.
	belowFloor := rig.memo(t, rig.owner, "spoken to a small model", "floor.m4a")
	rig.transcribe(t, belowFloor, "spoken to a small model", "whisper.cpp/base.en", false, 30000)

	n, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
		Title: "Partial transcripts", Body: "From the partial memo.",
		MemoID: &partialOnly.ID, Verb: ptrTo(store.VerbCreate),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	if _, err := rig.st.AppendRevision(rig.ctx, n.ID, store.NewRevision{
		AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
		Title: "Partial transcripts", Body: "And from the below-the-floor one.",
		MemoID: &belowFloor.ID, Verb: ptrTo(store.VerbAppend),
	}); err != nil {
		t.Fatalf("AppendRevision: %v", err)
	}

	rec = rig.get(t, "/notes/"+n.Ref()+"/provenance", rig.ownerTok, "getNoteProvenance", http.StatusOK)
	items := decodeInto[wire.ProvenanceList](t, rec).Items
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].Transcript.Partial == nil || !*items[0].Transcript.Partial {
		t.Errorf("partial = %v on a memo whose only transcript is incomplete", items[0].Transcript.Partial)
	}
	if items[0].RetentionStatus != wire.AwaitingTranscript || items[0].PrunesAt != nil {
		t.Errorf("entry = %q/%v; a partial transcript does not satisfy DurableClause, and "+
			"`prunes when transcribed` has no date", items[0].RetentionStatus, items[0].PrunesAt)
	}
	if items[1].Transcript.Partial == nil || *items[1].Transcript.Partial {
		t.Errorf("partial = %v; the below-the-floor row is complete", items[1].Transcript.Partial)
	}
	if items[1].RetentionStatus != wire.AwaitingTranscript {
		t.Errorf("retention_status = %q; split_part(model, '/', 2) = 'base.en' is not in the "+
			"model allow-list, so the audio is held", items[1].RetentionStatus)
	}

	// A readable memo with no transcript at all says WAIT, not WRONG ID.
	none := rig.memo(t, rig.owner, "just arrived", "none.m4a")
	rec = rig.get(t, "/transcripts/"+none.ID.String(), rig.ownerTok, "getMemoTranscript", http.StatusNotFound)
	if code := decodeInto[wire.Error](t, rec).Code; code != codeNoTranscript {
		t.Errorf("code = %q, want %q", code, codeNoTranscript)
	}
}

// The stream over a real memo, before and after a real sweep.
func TestTheAudioStreamOverTheRealStore(t *testing.T) {
	rig := realMemos(t)
	const body = "not really an m4a, but these are the bytes"
	m := rig.memo(t, rig.owner, body, "voice memo.m4a")
	rig.transcribe(t, m, "words that outlive the audio", "whisper.cpp/small.en", false, 104000)

	n, _, err := rig.st.CreateNote(rig.ctx, store.NewNote{
		PageID: rig.page.ID, AuthorID: rig.agent.ID, ConfirmedBy: rig.owner.ID,
		Title: "The recording behind this note", Body: "Said once.",
		MemoID: &m.ID, Verb: ptrTo(store.VerbCreate),
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}

	rec := rig.get(t, "/audio/"+m.ID.String(), rig.ownerTok, "getMemoAudio", http.StatusOK)
	if rec.Body.String() != body {
		t.Errorf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "audio/mp4" {
		t.Errorf("Content-Type = %q, want audio/mp4 through the hoisted audio.MediaType", ct)
	}

	// ANOTHER MEMBER AND AN AGENT GET WHAT A NONEXISTENT ID GETS, while the
	// list still answers them with the metadata.
	nothing := uuid.NewString()
	for _, tok := range []string{rig.otherTok, rig.agentTok} {
		baseline := rig.get(t, "/audio/"+nothing, tok, "getMemoAudio", http.StatusNotFound)
		refused := rig.get(t, "/audio/"+m.ID.String(), tok, "getMemoAudio", http.StatusNotFound)
		if refused.Body.String() != baseline.Body.String() {
			t.Errorf("refusal = %s, want byte-identical to %s", refused.Body.String(), baseline.Body.String())
		}
		rig.get(t, "/transcripts/"+m.ID.String(), tok, "getMemoTranscript", http.StatusNotFound)

		entry := decodeInto[wire.ProvenanceList](t,
			rig.get(t, "/notes/"+n.Ref()+"/provenance", tok, "getNoteProvenance", http.StatusOK)).Items[0]
		if entry.AudioReadable || entry.Transcript.Readable {
			t.Errorf("readable flags = %v/%v for a caller who is neither the author nor the owner",
				entry.AudioReadable, entry.Transcript.Readable)
		}
		if !entry.Transcript.Present {
			t.Error("present = false; whether there ARE words is metadata")
		}
	}

	// THE SWEEP, for real: MarkAudioPruned over the whole predicate, then the
	// unlink. The window is a nanosecond because captured_at is immutable
	// (CH002) and cannot be backdated — internal/store's own tests do this.
	claimed, err := rig.st.MarkAudioPruned(rig.ctx, m.ID, time.Nanosecond)
	if err != nil || !claimed {
		t.Fatalf("MarkAudioPruned = %v, %v; the fixture's memo is not prunable", claimed, err)
	}
	path, err := rig.audio.Path(audio.Ref{AuthorID: m.AuthorID, ContentHash: m.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	rec = rig.get(t, "/audio/"+m.ID.String(), rig.ownerTok, "getMemoAudio", http.StatusGone)
	if code := decodeInto[wire.Error](t, rec).Code; code != codeAudioPruned {
		t.Errorf("code = %q, want %q", code, codeAudioPruned)
	}
	// The transcript is permanent and still answers.
	rig.get(t, "/transcripts/"+m.ID.String(), rig.ownerTok, "getMemoTranscript", http.StatusOK)

	// And what renders *transcript kept, audio pruned <date>* is the entry.
	entry := decodeInto[wire.ProvenanceList](t,
		rig.get(t, "/notes/"+n.Ref()+"/provenance", rig.ownerTok, "getNoteProvenance", http.StatusOK)).Items[0]
	if entry.RetentionStatus != wire.Pruned {
		t.Errorf("retention_status = %q, want pruned", entry.RetentionStatus)
	}
	if entry.AudioPrunedAt == nil {
		t.Error("audio_pruned_at is null on a pruned memo")
	}
	if entry.PrunesAt != nil {
		t.Errorf("prunes_at = %v; ruling 7 — that date is audio_pruned_at's", entry.PrunesAt)
	}
	if !entry.Transcript.Present {
		t.Error("present = false; the transcript is what survives a prune")
	}
}
