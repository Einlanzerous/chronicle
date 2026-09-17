package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-107's contract, against an in-memory corpus and a real directory of
// bytes.
//
// What is proved here is the HTTP contract: every declared status driven
// through apitest.Conform, the split in ruling 2 held from four principals'
// points of view, the pruned answer distinguishable from a nonexistent id, and
// the audio stream's range and cache behaviour asserted header by header. The
// store's own clause is the store's tests; memos_db_test.go is the same
// contract over the store E2 actually shipped.

// fakeMemos is the three reads a provenance entry costs.
type fakeMemos struct {
	mu          sync.Mutex
	memos       map[uuid.UUID]store.Memo
	transcripts map[uuid.UUID][]store.Transcript
	err         error
}

func newFakeMemos() *fakeMemos {
	return &fakeMemos{
		memos:       map[uuid.UUID]store.Memo{},
		transcripts: map[uuid.UUID][]store.Transcript{},
	}
}

func (f *fakeMemos) GetMemo(_ context.Context, id uuid.UUID) (store.Memo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Memo{}, f.err
	}
	m, ok := f.memos[id]
	if !ok {
		return store.Memo{}, store.ErrNotFound
	}
	return m, nil
}

// GetTranscript keeps the store's ordering: complete before partial, newest
// first within each. A memo can hold several rows and WHICH ONE is answered is
// the thing the entry's `partial` describes.
func (f *fakeMemos) GetTranscript(_ context.Context, memoID uuid.UUID) (store.Transcript, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Transcript{}, f.err
	}
	var best *store.Transcript
	for i, t := range f.transcripts[memoID] {
		switch {
		case best == nil,
			!t.Partial && best.Partial,
			t.Partial == best.Partial && t.TranscribedAt.After(best.TranscribedAt):
			best = &f.transcripts[memoID][i]
		}
	}
	if best == nil {
		return store.Transcript{}, store.ErrNotFound
	}
	return *best, nil
}

// RetentionStatus mirrors store.RetentionStatus's CASE — the same five values
// in the same order, and the same OVERLOADED `at`, which is set for
// `scheduled` and for `pruned` and is what ruling 7's narrowing has to be
// visible against. A fixture therefore cannot be seeded into a state the
// store could not produce.
func (f *fakeMemos) RetentionStatus(_ context.Context, memoID uuid.UUID, window time.Duration) (string, *time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", nil, f.err
	}
	m, ok := f.memos[memoID]
	if !ok {
		return "", nil, store.ErrNotFound
	}
	durable := false
	for _, t := range f.transcripts[memoID] {
		if t.Durable() {
			durable = true
		}
	}
	switch {
	case m.AudioPrunedAt != nil:
		return store.RetentionPruned, m.AudioPrunedAt, nil
	case m.Retention == store.RetentionForever:
		return store.RetentionStatusPinned, nil, nil
	case !durable:
		return store.RetentionAwaitingTranscript, nil, nil
	case m.Retention == store.RetentionDiscardNow:
		return store.RetentionDiscardPending, nil, nil
	default:
		at := m.CapturedAt.Add(window)
		return store.RetentionScheduled, &at, nil
	}
}

// ── the rig ─────────────────────────────────────────────────────────────────

type memoRig struct {
	h     http.Handler
	logs  *bytes.Buffer
	wiki  *fakeWiki
	memos *fakeMemos
	audio *audio.Store

	// FOUR PRINCIPALS, because ruling 2 is a claim about all four: the memo's
	// author, the owner, another member, and an agent session.
	author, owner, other, agent store.User
}

func newMemoRig(t *testing.T) *memoRig {
	t.Helper()
	rig := &memoRig{logs: &bytes.Buffer{}, wiki: newFakeWiki(), memos: newFakeMemos()}
	logger := jsonLogger(rig.logs)

	as, err := audio.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rig.audio = as

	f := newFakeAccounts()
	rig.author = f.signIn(person("author@example.com", false), "author-token")
	rig.owner = f.signIn(person("owner@example.com", true), "owner-token")
	rig.other = f.signIn(person("other@example.com", false), "other-token")
	rig.agent = f.signIn(store.User{
		ID: uuid.New(), Email: store.ScribeEmail, DisplayName: "Scribe", Kind: store.KindAgent,
	}, "agent-token")
	rig.wiki.agents[rig.agent.ID] = true

	rig.h = NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: logger, Version: "test", SecureCookies: true,
		Wiki: rig.wiki, Memos: rig.memos, Audio: as, LocalReferences: rig.wiki,
	})
	return rig
}

func (rig *memoRig) do(method, path, token string, headers map[string]string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	rig.h.ServeHTTP(rec, r)
	return rec
}

func (rig *memoRig) get(path, token string) *httptest.ResponseRecorder {
	return rig.do(http.MethodGet, path, token, nil)
}

type memoOpt func(*store.Memo)

func headerDuration(ms int32) memoOpt {
	return func(m *store.Memo) { m.DurationMS = &ms }
}
func filename(name string) memoOpt {
	return func(m *store.Memo) { m.OriginalFilename = &name }
}
func codec(c string) memoOpt {
	return func(m *store.Memo) { m.Codec = &c }
}

// memo seeds one recording and puts its bytes where the layout says they go.
// Every memo in the live corpus is m4a with a NULL codec, so that is the
// default here.
func (rig *memoRig) memo(t *testing.T, author store.User, body string, opts ...memoOpt) store.Memo {
	t.Helper()
	sum := sha256.Sum256([]byte(body))
	m := store.Memo{
		ID:               uuid.New(),
		AuthorID:         author.ID,
		ContentHash:      hex.EncodeToString(sum[:]),
		ByteSize:         int64(len(body)),
		CapturedAt:       time.Date(2026, 8, 21, 12, 55, 0, 0, time.UTC),
		State:            store.StateTranscribed,
		Retention:        store.RetentionDays30,
		OriginalFilename: ptrTo("voice memo.m4a"),
	}
	for _, o := range opts {
		o(&m)
	}
	rig.memos.mu.Lock()
	rig.memos.memos[m.ID] = m
	rig.memos.mu.Unlock()
	rig.writeAudio(t, m, body)
	return m
}

func (rig *memoRig) writeAudio(t *testing.T, m store.Memo, body string) {
	t.Helper()
	path, err := rig.audio.Path(audio.Ref{AuthorID: m.AuthorID, ContentHash: m.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(strings.TrimSuffix(path, "/"+m.ContentHash), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// removeAudio is CHRN-23's `missing`: the row expects its recording and the
// file is not there. Distinct from a prune, which is `audio_pruned_at` set.
func (rig *memoRig) removeAudio(t *testing.T, m store.Memo) {
	t.Helper()
	path, err := rig.audio.Path(audio.Ref{AuthorID: m.AuthorID, ContentHash: m.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// prune is what the sweep does: the file goes and audio_pruned_at is stamped.
func (rig *memoRig) prune(t *testing.T, m store.Memo) store.Memo {
	t.Helper()
	rig.removeAudio(t, m)
	at := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	rig.memos.mu.Lock()
	defer rig.memos.mu.Unlock()
	m.AudioPrunedAt = &at
	rig.memos.memos[m.ID] = m
	return m
}

func (rig *memoRig) transcribe(m store.Memo, text string, partial bool, model string, at time.Time) store.Transcript {
	t := store.Transcript{
		ID: uuid.New(), MemoID: m.ID, Text: text, Segments: []store.Segment{{StartMS: 0, EndMS: 1440, Text: text}},
		Partial: partial, Model: model, Backend: "vulkan", TranscribedAt: at,
	}
	rig.memos.mu.Lock()
	defer rig.memos.mu.Unlock()
	rig.memos.transcripts[m.ID] = append(rig.memos.transcripts[m.ID], t)
	return t
}

// page creates `estate` once, whatever order the fixtures run in.
func (rig *memoRig) page(t *testing.T) store.Page {
	t.Helper()
	if p, ok := rig.wiki.pages["estate"]; ok {
		return p
	}
	p, err := rig.wiki.CreatePage(context.Background(), nil, "estate")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// note files one note whose revisions came from these memos, in order — the
// shape 0011 chose the column for: "a vague memo in March, then a concrete one
// after a trade show in June. Those are TWO MEMOS feeding TWO REVISIONS of one
// note." With no memos it is a note somebody typed.
func (rig *memoRig) note(t *testing.T, author store.User, memos ...*store.Memo) store.Note {
	t.Helper()
	page := rig.page(t)

	var first *uuid.UUID
	if len(memos) > 0 {
		first = &memos[0].ID
	}
	n, _, err := rig.wiki.CreateNote(context.Background(), store.NewNote{
		PageID: page.ID, AuthorID: author.ID, ConfirmedBy: author.ID,
		Title: "Retention gates on a transcript", Body: "The pruner gates on a durable transcript.",
		MemoID: first, Verb: ptrTo("create"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(memos) < 2 {
		return n
	}
	for _, m := range memos[1:] {
		rev, err := rig.wiki.AppendRevision(context.Background(), n.ID, store.NewRevision{
			AuthorID: author.ID, ConfirmedBy: author.ID,
			Title: "Retention gates on a transcript", Body: "And never on the calendar.",
		})
		if err != nil {
			t.Fatal(err)
		}
		rig.wiki.stampMemo(n.ID, rev.ID, m.ID)
	}
	return n
}

// stampMemo puts a memo id on a revision that already exists.
//
// fakeWiki.AppendRevision ignores NewRevision.MemoID, because no shipped HTTP
// route appends a revision from a memo — triage's accept path writes that row
// in the store — and `which text came from which memo` is exactly what this
// list reads. So a fixture has to be able to say it.
func (f *fakeWiki) stampMemo(noteID, revID, memoID uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, rv := range f.revs[noteID] {
		if rv.ID == revID {
			f.revs[noteID][i].MemoID = &memoID
		}
	}
}

func ptrTo[T any](v T) *T { return &v }

func provenanceOf(t *testing.T, rig *memoRig, ref, token string) wire.ProvenanceList {
	t.Helper()
	rec := rig.get("/notes/"+ref+"/provenance", token)
	mustStatus(t, rec, http.StatusOK, "getNoteProvenance")
	return decodeInto[wire.ProvenanceList](t, rec)
}

// ── the list ────────────────────────────────────────────────────────────────

// One entry per revision that came from a memo, oldest first — and an empty
// list, not a 404, for a note somebody typed.
func TestProvenanceListsEveryMemoBearingRevisionOldestFirst(t *testing.T) {
	rig := newMemoRig(t)
	march := rig.memo(t, rig.author, "a vague thought in March")
	june := rig.memo(t, rig.author, "a concrete one after the trade show")
	rig.transcribe(march, "a vague thought", false, "whisper.cpp/small.en", march.CapturedAt.Add(time.Minute))
	rig.transcribe(june, "a concrete one", false, "whisper.cpp/small.en", june.CapturedAt.Add(time.Minute))
	n := rig.note(t, rig.author, &march, &june)

	list := provenanceOf(t, rig, n.Ref(), "author-token")
	if len(list.Items) != 2 {
		t.Fatalf("items = %d, want 2 (0011: two memos feeding two revisions of one note)", len(list.Items))
	}
	if list.Items[0].MemoId != march.ID || list.Items[1].MemoId != june.ID {
		t.Errorf("order = %v, %v; oldest revision first is listNoteRevisions's order",
			list.Items[0].MemoId, list.Items[1].MemoId)
	}
	if list.Items[0].RevisionSeq != 1 || list.Items[1].RevisionSeq != 2 {
		t.Errorf("revision_seq = %d, %d; the two lists zip by it",
			list.Items[0].RevisionSeq, list.Items[1].RevisionSeq)
	}
	first := list.Items[0]
	if !first.CapturedAt.Equal(march.CapturedAt) {
		t.Errorf("captured_at = %s, want %s", first.CapturedAt, march.CapturedAt)
	}
	if first.RetentionStatus != wire.Scheduled {
		t.Errorf("retention_status = %q, want scheduled for a durable transcript inside the window", first.RetentionStatus)
	}
	if first.PrunesAt == nil || !first.PrunesAt.Equal(march.CapturedAt.Add(audio.ProjectionWindow)) {
		t.Errorf("prunes_at = %v; a scheduled memo names the date the sweep will use", first.PrunesAt)
	}
	if first.AudioPrunedAt != nil {
		t.Errorf("audio_pruned_at = %v on a memo whose audio is still here", first.AudioPrunedAt)
	}
	if !first.Transcript.Present || first.Transcript.Model == nil || *first.Transcript.Model != "whisper.cpp/small.en" {
		t.Errorf("transcript = %+v; the model is runner-qualified as the store holds it", first.Transcript)
	}
	if first.Transcript.Partial == nil || *first.Transcript.Partial {
		t.Errorf("partial = %v, want false for a complete transcript", first.Transcript.Partial)
	}

	// A note somebody typed: the note exists and nothing produced it.
	typed := rig.note(t, rig.author)
	list = provenanceOf(t, rig, typed.Ref(), "author-token")
	if len(list.Items) != 0 {
		t.Errorf("items = %d for a typed note, want an empty list and not a 404", len(list.Items))
	}
	if rec := rig.get("/notes/"+typed.Ref()+"/provenance", "author-token"); !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("body = %s; an empty list serialises as [] rather than null", rec.Body.String())
	}
}

// The note sub-resource rule: a withdrawn note answers the tombstone, and a
// malformed ref is noteRef's flat 400.
func TestProvenanceFollowsTheNoteSubResourceRules(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "said once")
	n := rig.note(t, rig.author, &m)

	rec := rig.get("/notes/not-a-note-ref/provenance", "author-token")
	mustStatus(t, rec, http.StatusBadRequest, "getNoteProvenance")
	if code := decodeInto[wire.Error](t, rec).Code; code != codeInvalidParameter {
		t.Errorf("code = %q, want %q", code, codeInvalidParameter)
	}

	mustStatus(t, rig.get("/notes/CHR-9999/provenance", "author-token"), http.StatusNotFound, "getNoteProvenance")

	rig.wiki.softDelete(n.Number, rig.author.ID)
	rec = rig.get("/notes/"+n.Ref()+"/provenance", "author-token")
	mustStatus(t, rec, http.StatusGone, "getNoteProvenance")
	tomb := decodeInto[wire.NoteTombstone](t, rec)
	if tomb.Ref != n.Ref() {
		t.Errorf("tombstone = %+v", tomb)
	}

	// A memo is its own tier-2 fact and did not stop having been said.
	mustStatus(t, rig.get("/transcripts/"+m.ID.String(), "author-token"), http.StatusNotFound, "getMemoTranscript")
	mustStatus(t, rig.get("/audio/"+m.ID.String(), "author-token"), http.StatusOK, "getMemoAudio")
}

// ── the transcript ──────────────────────────────────────────────────────────

// The row is GetTranscript's, and a readable memo with none says `wait`
// rather than `wrong id`.
func TestTheTranscriptOperationAnswersTheNewestCompleteRow(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "a thought, spoken")

	rec := rig.get("/transcripts/"+m.ID.String(), "author-token")
	mustStatus(t, rec, http.StatusNotFound, "getMemoTranscript")
	body := decodeInto[wire.Error](t, rec)
	if body.Code != codeNoTranscript {
		t.Fatalf("code = %q, want %q: the remedy is to wait, not to correct the id", body.Code, codeNoTranscript)
	}
	if body.Code == codeNotFound {
		t.Error("a memo with no transcript is not the same answer as a memo that does not exist")
	}

	// TWO ROWS: an incomplete run, then a complete one from a later pass.
	rig.transcribe(m, "a thought, spo", true, "whisper.cpp/small.en", m.CapturedAt.Add(time.Minute))
	rig.transcribe(m, "a thought, spoken", false, "whisper.cpp/small.en", m.CapturedAt.Add(2*time.Minute))

	rec = rig.get("/transcripts/"+m.ID.String(), "author-token")
	mustStatus(t, rec, http.StatusOK, "getMemoTranscript")
	got := decodeInto[wire.MemoTranscript](t, rec)
	if got.Text != "a thought, spoken" || got.Partial {
		t.Errorf("transcript = %+v; complete before partial is GetTranscript's order", got)
	}
	if got.MemoId != m.ID || got.Model != "whisper.cpp/small.en" || got.Backend != "vulkan" {
		t.Errorf("transcript = %+v", got)
	}
	if len(got.Segments) != 1 || got.Segments[0].EndMs != 1440 {
		t.Errorf("segments = %+v; the whole row, not a bounded projection", got.Segments)
	}
	// And the entry describes the same row.
	n := rig.note(t, rig.author, &m)
	entry := provenanceOf(t, rig, n.Ref(), "author-token").Items[0]
	if entry.Transcript.Partial == nil || *entry.Transcript.Partial {
		t.Errorf("entry partial = %v; the entry describes GetTranscript's row, which is the complete one",
			entry.Transcript.Partial)
	}
}

// A memo whose ONLY transcript is incomplete: the entry says so, which is the
// only way a non-author learns a `present` transcript is a fallback.
func TestAnIncompleteTranscriptIsFlaggedOnTheEntry(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "half a thought")
	rig.transcribe(m, "half a", true, "whisper.cpp/small.en", m.CapturedAt.Add(time.Minute))
	n := rig.note(t, rig.author, &m)

	entry := provenanceOf(t, rig, n.Ref(), "other-token").Items[0]
	if !entry.Transcript.Present {
		t.Fatal("present = false; a partial row is still a row")
	}
	if entry.Transcript.Partial == nil || !*entry.Transcript.Partial {
		t.Errorf("partial = %v, want true", entry.Transcript.Partial)
	}
	if entry.Transcript.Readable {
		t.Error("readable = true for another member; the words are the author's and the owner's")
	}
	// A partial transcript does not satisfy the floor, so the audio is held.
	if entry.RetentionStatus != wire.AwaitingTranscript {
		t.Errorf("retention_status = %q, want awaiting_transcript", entry.RetentionStatus)
	}
}

// ── the stream ──────────────────────────────────────────────────────────────

// Range, 206, 416, 304 and the two headers that are chosen rather than
// defaulted — asserted by name, because without Cache-Control the response
// earns heuristic freshness and a player keeps serving bytes the pruner has
// deleted.
func TestTheAudioStreamRangesAndRevalidates(t *testing.T) {
	rig := newMemoRig(t)
	const body = "OggS-not-really-but-long-enough-to-slice"
	m := rig.memo(t, rig.author, body)
	path := "/audio/" + m.ID.String()

	rec := rig.get(path, "author-token")
	mustStatus(t, rec, http.StatusOK, "getMemoAudio")
	if rec.Body.String() != body {
		t.Errorf("body = %q, want the stored bytes", rec.Body.String())
	}
	if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-cache" {
		t.Errorf("Cache-Control = %q: without it RFC 9111 §4.2.2's heuristic freshness lets a player "+
			"serve a pruned recording for days", got)
	}
	etag := rec.Header().Get("ETag")
	if etag != `"`+m.ID.String()+`"` {
		t.Errorf("ETag = %q, want the memo id quoted", etag)
	}
	if got := rec.Header().Get("Last-Modified"); got != m.CapturedAt.UTC().Format(http.TimeFormat) {
		t.Errorf("Last-Modified = %q, want captured_at (%s) and never the file's mtime",
			got, m.CapturedAt.Format(http.TimeFormat))
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mp4" {
		t.Errorf("Content-Type = %q, want audio/mp4 for an m4a with a NULL codec — "+
			"every memo in the live corpus", got)
	}

	// A range.
	rec = rig.do(http.MethodGet, path, "author-token", map[string]string{"Range": "bytes=0-3"})
	mustStatus(t, rec, http.StatusPartialContent, "getMemoAudio")
	if rec.Body.String() != body[:4] {
		t.Errorf("ranged body = %q, want %q", rec.Body.String(), body[:4])
	}
	if got, want := rec.Header().Get("Content-Range"), "bytes 0-3/"+strconv.Itoa(len(body)); got != want {
		t.Errorf("Content-Range = %q, want %q", got, want)
	}

	// A range starting past the end: the one unsatisfiable case ServeContent
	// sets Content-Range for, in this API's envelope and not text/plain.
	rec = rig.do(http.MethodGet, path, "author-token", map[string]string{"Range": "bytes=9999-"})
	mustStatus(t, rec, http.StatusRequestedRangeNotSatisfiable, "getMemoAudio")
	if got, want := rec.Header().Get("Content-Range"), "bytes */"+strconv.Itoa(len(body)); got != want {
		t.Errorf("Content-Range = %q, want %q", got, want)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q on the 416; http.Error's text/plain would be a second error shape", got)
	}
	if code := decodeInto[wire.Error](t, rec).Code; code != codeRangeNotSatisfiable {
		t.Errorf("code = %q, want %q", code, codeRangeNotSatisfiable)
	}

	// MULTI-RANGE IS NOT SERVED. RFC 9110 lets a server ignore Range, and
	// multipart/byteranges is a shape this document does not declare.
	rec = rig.do(http.MethodGet, path, "author-token", map[string]string{"Range": "bytes=0-3,6-9"})
	mustStatus(t, rec, http.StatusOK, "getMemoAudio")
	if rec.Body.String() != body {
		t.Errorf("multi-range body = %q, want the whole recording", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); strings.Contains(ct, "multipart") {
		t.Errorf("Content-Type = %q; multipart/byteranges is never served and never declared", ct)
	}

	// The conditional read the Cache-Control above pays for.
	rec = rig.do(http.MethodGet, path, "author-token", map[string]string{"If-None-Match": etag})
	mustStatus(t, rec, http.StatusNotModified, "getMemoAudio")
	if rec.Body.Len() != 0 {
		t.Errorf("304 carried %d bytes", rec.Body.Len())
	}

	// And the precondition ServeContent answers whether or not a document
	// mentions it.
	rec = rig.do(http.MethodGet, path, "author-token", map[string]string{"If-Match": `"not-this-recording"`})
	mustStatus(t, rec, http.StatusPreconditionFailed, "getMemoAudio")
	if code := decodeInto[wire.Error](t, rec).Code; code != codePreconditionFailed {
		t.Errorf("code = %q, want %q", code, codePreconditionFailed)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q on the 412: checkPreconditions writes a BARE WriteHeader, "+
			"so a wrapper hooking Write alone would leave the audio type in the headers", got)
	}
}

// THE INVARIANT ON THE WIRE: pruned is not missing, and neither is 404.
func TestAPrunedRecordingIsGoneAndSaysSoInTheEnvelope(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "said once, kept forever in words")
	rig.transcribe(m, "said once", false, "whisper.cpp/small.en", m.CapturedAt.Add(time.Minute))
	n := rig.note(t, rig.author, &m)
	m = rig.prune(t, m)

	rec := rig.get("/audio/"+m.ID.String(), "author-token")
	mustStatus(t, rec, http.StatusGone, "getMemoAudio")
	if code := decodeInto[wire.Error](t, rec).Code; code != codeAudioPruned {
		t.Errorf("code = %q, want %q", code, codeAudioPruned)
	}

	// NO FIELDS BEYOND code AND message. A details bag is what apierr.go
	// refuses, and rev 2 of the plan declared one here before Retention
	// argued it away.
	var loose map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &loose); err != nil {
		t.Fatal(err)
	}
	if len(loose) != 2 {
		t.Errorf("410 body = %v; the shared envelope is two fields and no third", loose)
	}

	// And it is NOT the answer a nonexistent memo gets: deleted by policy and
	// never existed are different facts.
	nonexistent := rig.get("/audio/"+uuid.NewString(), "author-token")
	mustStatus(t, nonexistent, http.StatusNotFound, "getMemoAudio")
	if nonexistent.Code == rec.Code {
		t.Error("a pruned recording answers the same status as an id that names nothing")
	}

	// WHAT A CLIENT ACTUALLY RENDERS FROM. A media element exposes neither the
	// status nor the body of a failed load, so *transcript kept, audio pruned
	// 2026-09-20* comes off the entry.
	entry := provenanceOf(t, rig, n.Ref(), "author-token").Items[0]
	if entry.RetentionStatus != wire.Pruned {
		t.Errorf("retention_status = %q, want pruned", entry.RetentionStatus)
	}
	if entry.AudioPrunedAt == nil || !entry.AudioPrunedAt.Equal(*m.AudioPrunedAt) {
		t.Errorf("audio_pruned_at = %v, want %v", entry.AudioPrunedAt, m.AudioPrunedAt)
	}
	if !entry.Transcript.Present {
		t.Error("transcript.present = false on a pruned memo: the transcript is what survives")
	}
	// RULING 7: the field named for a future sweep does not carry a past date.
	if entry.PrunesAt != nil {
		t.Errorf("prunes_at = %v on a pruned memo; that date is audio_pruned_at's, in the past tense",
			entry.PrunesAt)
	}
}

// The state the invariant cares most about: the audio is here and MUST NOT be
// promised a date.
func TestAMemoAwaitingATranscriptStreamsAndPromisesNoDate(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "not transcribed yet")
	n := rig.note(t, rig.author, &m)

	mustStatus(t, rig.get("/audio/"+m.ID.String(), "author-token"), http.StatusOK, "getMemoAudio")

	entry := provenanceOf(t, rig, n.Ref(), "author-token").Items[0]
	if entry.RetentionStatus != wire.AwaitingTranscript {
		t.Fatalf("retention_status = %q, want awaiting_transcript", entry.RetentionStatus)
	}
	// ASSERTED EXPLICITLY. A client that renders `captured_at + 30 days` in
	// its place produces `PRUNES 2026-09-20` for a memo nothing will prune,
	// which is the label CHRN-22 §3 forbids — and it can only do that if this
	// is non-null or absent.
	if entry.PrunesAt != nil {
		t.Errorf("prunes_at = %v; awaiting_transcript prunes WHEN TRANSCRIBED and has no date",
			entry.PrunesAt)
	}
	if entry.AudioPrunedAt != nil {
		t.Errorf("audio_pruned_at = %v on audio that is still here", entry.AudioPrunedAt)
	}
}

// CHRN-23's `missing`: 500, and a log line whether or not anybody reads the
// response.
func TestAMissingRecordingIsAnErrorAndIsLogged(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "the file will go astray")
	rig.removeAudio(t, m)

	rec := rig.get("/audio/"+m.ID.String(), "author-token")
	mustStatus(t, rec, http.StatusInternalServerError, "getMemoAudio")
	if code := decodeInto[wire.Error](t, rec).Code; code != codeAudioMissing {
		t.Errorf("code = %q, want %q: not 404 and not 410, both of which say the absence is expected",
			code, codeAudioMissing)
	}
	// The HANDLER's line, named by its message rather than by the memo id:
	// requestLogger writes a second ERROR line for any 500, and that one is
	// the access log's rather than this claim's.
	if n := errorLines(rig.logs, "audio missing from disk"); n != 1 {
		t.Errorf("%d audio-missing ERROR lines, want 1: storage.go logs the same condition "+
			"unconditionally, and this is the second caller", n)
	}
	if !strings.Contains(rig.logs.String(), m.ID.String()) {
		t.Error("the log line does not name the memo, so nobody can act on it")
	}
}

// ── ruling 2, from four points of view ──────────────────────────────────────

// Another author's memo is indistinguishable from one that does not exist —
// and the ordering that makes that true is authorization, then retention, then
// the disk.
func TestAnotherAuthorsMemoIsIndistinguishableFromNothing(t *testing.T) {
	rig := newMemoRig(t)
	ordinary := rig.memo(t, rig.author, "ordinary")
	prunedMemo := rig.memo(t, rig.author, "pruned")
	missing := rig.memo(t, rig.author, "missing")
	for _, m := range []store.Memo{ordinary, prunedMemo, missing} {
		rig.transcribe(m, "words", false, "whisper.cpp/small.en", m.CapturedAt.Add(time.Minute))
	}
	prunedMemo = rig.prune(t, prunedMemo)
	rig.removeAudio(t, missing)
	n := rig.note(t, rig.author, &ordinary, &prunedMemo, &missing)

	nothing := uuid.NewString()
	for _, caller := range []struct{ who, token string }{
		{"another member", "other-token"},
		{"an agent session", "agent-token"},
	} {
		for _, op := range []struct{ id, path string }{
			{"getMemoTranscript", "/transcripts/"},
			{"getMemoAudio", "/audio/"},
		} {
			baseline := rig.get(op.path+nothing, caller.token)
			mustStatus(t, baseline, http.StatusNotFound, op.id)
			for name, m := range map[string]store.Memo{
				"ordinary": ordinary, "pruned": prunedMemo, "file missing": missing,
			} {
				before := errorLines(rig.logs, m.ID.String())
				rec := rig.get(op.path+m.ID.String(), caller.token)
				mustStatus(t, rec, http.StatusNotFound, op.id)
				if rec.Body.String() != baseline.Body.String() {
					t.Errorf("%s asking %s for a %s memo: body = %s, want byte-identical to a "+
						"nonexistent id (%s)", caller.who, op.id, name, rec.Body.String(), baseline.Body.String())
				}
				if strings.Contains(rec.Body.String(), "pruned") {
					t.Errorf("%s: the refusal discloses the prune", op.id)
				}
				// AUTHORIZATION RUNS BEFORE THE DISK. No member may raise an
				// operator alert about another account's recording, and a log
				// line is not a response anyone can be shown or redact.
				if after := errorLines(rig.logs, m.ID.String()); after != before {
					t.Errorf("%s asking %s for a %s memo wrote an ERROR line naming it",
						caller.who, op.id, name)
				}
			}
		}
	}

	// AND THE LIST STILL ANSWERS, for all three, because metadata about a
	// recording is what ruling 2 gives every member on purpose.
	for _, token := range []string{"other-token", "agent-token"} {
		list := provenanceOf(t, rig, n.Ref(), token)
		if len(list.Items) != 3 {
			t.Fatalf("items = %d for %s, want 3", len(list.Items), token)
		}
		for _, entry := range list.Items {
			if entry.AudioReadable || entry.Transcript.Readable {
				t.Errorf("%s: readable flags = %v/%v on another author's memo",
					token, entry.AudioReadable, entry.Transcript.Readable)
			}
		}
		pruned := list.Items[1]
		if pruned.RetentionStatus != wire.Pruned || pruned.AudioPrunedAt == nil {
			t.Errorf("%s: the pruned entry = %+v; the retention dates go to every member "+
				"deliberately — it is what renders *transcript kept, audio pruned <date>*", token, pruned)
		}
	}
}

// The flags are asserted on the entry rather than inferred from a failed
// request, because sparing a client that request is what they are for.
func TestTheReadabilityFlagsFollowTheCaller(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "whose words these are")
	rig.transcribe(m, "whose words", false, "whisper.cpp/small.en", m.CapturedAt.Add(time.Minute))
	n := rig.note(t, rig.author, &m)

	for _, c := range []struct {
		who, token string
		want       bool
	}{
		{"the memo's author", "author-token", true},
		{"the owner", "owner-token", true},
		{"another member", "other-token", false},
		// KindAgent is its own account and is never the owner. An agent reads
		// the words and the bytes only of memos it uploaded itself, and today
		// no agent has.
		{"an agent session", "agent-token", false},
	} {
		entry := provenanceOf(t, rig, n.Ref(), c.token).Items[0]
		if entry.AudioReadable != c.want || entry.Transcript.Readable != c.want {
			t.Errorf("%s: audio_readable=%v transcript.readable=%v, want both %v",
				c.who, entry.AudioReadable, entry.Transcript.Readable, c.want)
		}
		wantStatus := http.StatusNotFound
		if c.want {
			wantStatus = http.StatusOK
		}
		mustStatus(t, rig.get("/transcripts/"+m.ID.String(), c.token), wantStatus, "getMemoTranscript")
		mustStatus(t, rig.get("/audio/"+m.ID.String(), c.token), wantStatus, "getMemoAudio")
	}
}

// ── ruling 4 ────────────────────────────────────────────────────────────────

// All three cases, and the middle one is the shape of every memo in the live
// corpus.
func TestTheDurationNamesWhichColumnAnsweredIt(t *testing.T) {
	rig := newMemoRig(t)

	// m4a, NULL header duration, transcribed: all seventeen live memos.
	fromTranscript := rig.memo(t, rig.author, "m4a from a phone")
	tr := rig.transcribe(fromTranscript, "words", false, "whisper.cpp/small.en", fromTranscript.CapturedAt.Add(time.Minute))
	tr.AudioDurationMS = ptrTo(int64(104000))
	rig.memos.mu.Lock()
	rig.memos.transcripts[fromTranscript.ID] = []store.Transcript{tr}
	rig.memos.mu.Unlock()

	// Ogg Opus with a granule-derived duration in its header.
	fromHeader := rig.memo(t, rig.author, "ogg opus", codec("opus"), filename("memo.ogg"), headerDuration(104500))

	// Neither: the pre-transcription window.
	neither := rig.memo(t, rig.author, "just arrived")

	n := rig.note(t, rig.author, &fromTranscript, &fromHeader, &neither)
	items := provenanceOf(t, rig, n.Ref(), "author-token").Items

	if items[0].DurationMs == nil || *items[0].DurationMs != 104000 ||
		items[0].DurationSource == nil || *items[0].DurationSource != wire.MemoProvenanceDurationSourceTranscript {
		t.Errorf("m4a entry = %v/%v; reading memos.duration_ms alone renders a dash for the whole corpus",
			items[0].DurationMs, items[0].DurationSource)
	}
	if items[1].DurationMs == nil || *items[1].DurationMs != 104500 ||
		items[1].DurationSource == nil || *items[1].DurationSource != wire.MemoProvenanceDurationSourceMemoHeader {
		t.Errorf("ogg entry = %v/%v", items[1].DurationMs, items[1].DurationSource)
	}
	if items[2].DurationMs != nil || items[2].DurationSource != nil {
		t.Errorf("entry with neither = %v/%v; null and no source at all, rather than a zero that renders 0:00",
			items[2].DurationMs, items[2].DurationSource)
	}
}

// ── the refusals a nil store answers ────────────────────────────────────────

func TestTheMemoOperationsRefuseWhenTheirStoresAreAbsent(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("member@example.com", false), "member-token")

	// No memo store at all: the group's guard, on wikiUnavailable's pattern.
	bare := NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test", SecureCookies: true,
	})
	for _, c := range []struct {
		op, path, code string
	}{
		{"getMemoTranscript", "/transcripts/" + someUUID, codeMemosUnconfigured},
		{"getMemoAudio", "/audio/" + someUUID, codeMemosUnconfigured},
		{"getNoteProvenance", "/notes/CHR-0311/provenance", codeWikiUnconfigured},
	} {
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		r.Header.Set("Authorization", "Bearer member-token")
		bare.ServeHTTP(rec, r)
		mustStatus(t, rec, http.StatusServiceUnavailable, c.op)
		if code := decodeInto[wire.Error](t, rec).Code; code != c.code {
			t.Errorf("%s: code = %q, want %q", c.op, code, c.code)
		}
	}

	// A MEMO STORE AND NO AUDIO DIRECTORY. The database reads answer; only the
	// stream cannot, which is what wiring Memos beside Wiki rather than beside
	// Corpus buys.
	noDisk := NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test", SecureCookies: true,
		Wiki: newFakeWiki(), Memos: newFakeMemos(),
	})
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/audio/"+someUUID, nil)
	r.Header.Set("Authorization", "Bearer member-token")
	noDisk.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusServiceUnavailable, "getMemoAudio")
	if code := decodeInto[wire.Error](t, rec).Code; code != codeAudioUnconfigured {
		t.Errorf("code = %q, want %q", code, codeAudioUnconfigured)
	}
	// The transcript read is a database read and does NOT answer 503 here.
	rec = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/transcripts/"+someUUID, nil)
	r.Header.Set("Authorization", "Bearer member-token")
	noDisk.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusNotFound, "getMemoTranscript")
}

// The credential wrapper's refusal is a response like any other, and the
// document describes it. There is no unauthenticated read surface here: a
// recording and its transcript are tier 2.
func TestTheMemoOperationsRefuseAnAnonymousCaller(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "not for a stranger")
	n := rig.note(t, rig.author, &m)

	for _, c := range []struct{ op, path string }{
		{"getNoteProvenance", "/notes/" + n.Ref() + "/provenance"},
		{"getMemoTranscript", "/transcripts/" + m.ID.String()},
		{"getMemoAudio", "/audio/" + m.ID.String()},
	} {
		rec := httptest.NewRecorder()
		rig.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		mustStatus(t, rec, http.StatusUnauthorized, c.op)
		if code := decodeInto[wire.Error](t, rec).Code; code != codeUnauthorized {
			t.Errorf("%s: code = %q, want %q", c.op, code, codeUnauthorized)
		}
	}
}

// A store that fails answers the documented 500 — declared on all three
// because requireUser alone can produce one, and DRIVEN here so Conform
// judges the real response rather than a schema nobody has produced.
func TestAFailingMemoStoreAnswersTheDocumented500(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "the store will fail under this")
	n := rig.note(t, rig.author, &m)

	rig.memos.mu.Lock()
	rig.memos.err = errors.New("connection reset by peer")
	rig.memos.mu.Unlock()

	for _, c := range []struct{ op, path string }{
		{"getNoteProvenance", "/notes/" + n.Ref() + "/provenance"},
		{"getMemoTranscript", "/transcripts/" + m.ID.String()},
		{"getMemoAudio", "/audio/" + m.ID.String()},
	} {
		rec := rig.get(c.path, "author-token")
		mustStatus(t, rec, http.StatusInternalServerError, c.op)
		body := decodeInto[wire.Error](t, rec)
		if body.Code != codeInternal {
			t.Errorf("%s: code = %q, want %q", c.op, body.Code, codeInternal)
		}
		// The detail is in the log and deliberately not in a body that may
		// cross the WAN.
		if strings.Contains(body.Message, "connection reset") {
			t.Errorf("%s: message %q relays the store's own words", c.op, body.Message)
		}
	}
}

// ── the two structural claims ───────────────────────────────────────────────

// TestAnUndeclaredRoutePanicsAtConstruction's sibling on the other side: the
// three new patterns register BESIDE the four shipped upload routes without
// panicking.
//
// `GET /memos/{id}/transcript` and `GET /memos/{id}/audio` would not: Go's
// ServeMux refuses two patterns that overlap where neither is more specific,
// and both match /memos/uploads/transcript. That is a runtime panic inside
// HandleFunc which the first router test in this package would hit at
// construction, and every other router test would then fail with — so it is
// asserted here rather than left to be diagnosed there.
func TestTheNewMemoRoutesRegisterBesideTheUploadRoutes(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("registering the route set panicked: %v", r)
		}
	}()

	routed := newPolicyRouter(http.NewServeMux(), &api{accounts: newFakeAccounts()})
	wire.HandlerWithOptions(&api{}, wire.StdHTTPServerOptions{
		BaseRouter:       routed,
		ErrorHandlerFunc: bindError(discardLogger()),
	})

	for _, pattern := range []string{
		"GET /transcripts/{memo_id}",
		"GET /audio/{memo_id}",
		"GET /notes/{ref}/provenance",
		"POST /memos/uploads",
		"GET /memos/uploads/{id}",
		"PATCH /memos/uploads/{id}",
		"DELETE /memos/uploads/{id}",
	} {
		if _, ok := routed.seen[pattern]; !ok {
			t.Errorf("%s was not registered", pattern)
		}
	}
}

// No payload in this contract carries the storage path or the client's own
// filename. Asserted over the GENERATED types rather than by reading the
// document, because the document is what generates them.
//
// content_hash scoped by its author IS the path — audio.RelPath is
// <author_id>/<hash[:2]>/<hash> and nothing else — so handing it to every
// member on a shared note publishes the layout for somebody else's recording.
// original_filename is authored text arriving from a client, which
// internal/upload already declines to log for the same reason.
func TestNoProvenancePayloadCarriesTheStoragePath(t *testing.T) {
	forbidden := map[string]bool{"content_hash": true, "byte_size": true, "original_filename": true}
	for _, payload := range []any{
		wire.MemoProvenance{}, wire.ProvenanceTranscript{}, wire.ProvenanceList{},
		wire.MemoTranscript{}, wire.TranscriptSegment{},
	} {
		typ := reflect.TypeOf(payload)
		for i := range typ.NumField() {
			name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if forbidden[name] {
				t.Errorf("%s carries %q", typ.Name(), name)
			}
		}
	}
}

// ── ruling 7, on the shipped payload ────────────────────────────────────────

// `getUpload` answered a field named prunes_at with a date in the PAST on a
// pruned memo, because store.RetentionStatus overloads one `at` and toMemo
// passed it through. A unit test rather than a fixture: no upload session
// reaches a pruned memo, so there is no request that would drive this.
func TestUploadStatePrunesAtNamesAFutureSweepOnly(t *testing.T) {
	pruneTime := time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC)
	captured := time.Date(2026, 8, 21, 12, 55, 0, 0, time.UTC)
	sweep := captured.Add(audio.ProjectionWindow)

	pruned := toMemo(store.Memo{
		ID: uuid.New(), CapturedAt: captured, AudioPrunedAt: &pruneTime,
		State: store.StateTranscribed, Retention: store.RetentionDays30,
	}, store.RetentionPruned, &pruneTime)
	if pruned.PrunesAt != nil {
		t.Errorf("prunes_at = %v on a pruned memo; the document describes it as \"when, on that clause\"",
			pruned.PrunesAt)
	}
	if pruned.AudioPrunedAt == nil || !pruned.AudioPrunedAt.Equal(pruneTime) {
		t.Errorf("audio_pruned_at = %v, want %v — the past tense has its own field now",
			pruned.AudioPrunedAt, pruneTime)
	}
	if !pruned.AudioPruned {
		t.Error("audio_pruned = false; the boolean says whether, audio_pruned_at says when")
	}

	scheduled := toMemo(store.Memo{
		ID: uuid.New(), CapturedAt: captured, State: store.StateTranscribed, Retention: store.RetentionDays30,
	}, store.RetentionScheduled, &sweep)
	if scheduled.PrunesAt == nil || !scheduled.PrunesAt.Equal(sweep) {
		t.Errorf("prunes_at = %v on a scheduled memo, want %v", scheduled.PrunesAt, sweep)
	}
	if scheduled.AudioPrunedAt != nil {
		t.Errorf("audio_pruned_at = %v on audio that is still here", scheduled.AudioPrunedAt)
	}

	// The three statuses that carry no date at all.
	for _, status := range []string{
		store.RetentionAwaitingTranscript, store.RetentionStatusPinned, store.RetentionDiscardPending,
	} {
		m := toMemo(store.Memo{ID: uuid.New(), CapturedAt: captured, State: store.StateCaptured,
			Retention: store.RetentionDays30}, status, nil)
		if m.PrunesAt != nil {
			t.Errorf("%s: prunes_at = %v, want null", status, m.PrunesAt)
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// errorLines counts ERROR log lines mentioning `needle`. The claim in two
// places is about whether a memo is NAMED in the log, not merely whether
// something failed.
func errorLines(logs *bytes.Buffer, needle string) int {
	n := 0
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"level":"ERROR"`) && strings.Contains(line, needle) {
			n++
		}
	}
	return n
}
