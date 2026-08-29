package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-20's three Done-when assertions, plus the failure modes the shape of the
// protocol is chosen for. Nothing here needs Postgres: the session table is a
// map and the ingest rules are reproduced from CHRN-18, which keeps these tests
// about the transfer rather than about SQL.

// ---------------------------------------------------------------- fakes

// fakeSessions is tier1.memo_uploads as a map, including the unique index on
// (author_id, idempotency_key) that makes a re-POST a resume.
type fakeSessions struct {
	mu     sync.Mutex
	byID   map[uuid.UUID]store.Upload
	byKey  map[string]uuid.UUID
	closed []uuid.UUID
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{byID: map[uuid.UUID]store.Upload{}, byKey: map[string]uuid.UUID{}}
}

func keyOf(author uuid.UUID, key string) string { return author.String() + ":" + key }

func (f *fakeSessions) OpenUpload(_ context.Context, in store.Upload) (store.Upload, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byKey[keyOf(in.AuthorID, in.IdempotencyKey)]; ok {
		got := f.byID[id]
		if got.ContentHash != in.ContentHash || got.ByteSize != in.ByteSize {
			return store.Upload{}, false, store.ErrUploadKeyReused
		}
		return got, false, nil
	}
	in.ID = uuid.New()
	in.CreatedAt = time.Now()
	in.LastActivityAt = time.Now()
	f.byID[in.ID] = in
	f.byKey[keyOf(in.AuthorID, in.IdempotencyKey)] = in.ID
	return in, true, nil
}

func (f *fakeSessions) GetUpload(_ context.Context, id uuid.UUID) (store.Upload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return store.Upload{}, store.ErrNotFound
	}
	return u, nil
}

func (f *fakeSessions) TouchUpload(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok {
		u.LastActivityAt = time.Now()
		f.byID[id] = u
	}
	return nil
}

func (f *fakeSessions) DeleteUpload(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok {
		delete(f.byKey, keyOf(u.AuthorID, u.IdempotencyKey))
		delete(f.byID, id)
		f.closed = append(f.closed, id)
	}
	return nil
}

func (f *fakeSessions) ClearUploadKey(_ context.Context, author uuid.UUID, key string) error {
	f.mu.Lock()
	id, ok := f.byKey[keyOf(author, key)]
	f.mu.Unlock()
	if !ok {
		return nil
	}
	return f.DeleteUpload(context.Background(), id)
}

func (f *fakeSessions) CountOpenUploads(_ context.Context, author uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int
	for _, u := range f.byID {
		if u.AuthorID == author {
			n++
		}
	}
	return n, nil
}

func (f *fakeSessions) StaleUploads(_ context.Context, before time.Time) ([]store.Upload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Upload
	for _, u := range f.byID {
		if u.LastActivityAt.Before(before) {
			out = append(out, u)
		}
	}
	return out, nil
}

func (f *fakeSessions) LiveUploadIDs(context.Context) (map[uuid.UUID]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]struct{}{}
	for id := range f.byID {
		out[id] = struct{}{}
	}
	return out, nil
}

// fakeIngest reproduces the parts of CHRN-18's rule this package depends on:
// identity is (author, hash), the key names an ATTEMPT, and a same-key retry
// writes no arrival row — which is exactly why Collapsed cannot be inferred
// from a delivery count.
type fakeIngest struct {
	mu       sync.Mutex
	memos    map[string]*store.Memo
	byKey    map[string]uuid.UUID
	arrivals map[uuid.UUID]int
	calls    int
	// described counts SetMemoAudioInfo per memo, so a test can show the probe
	// runs once rather than on every delivery.
	described map[uuid.UUID]int

	// pruned is CHRN-22's re-delivery gate, keyed author/hash.
	pruned map[string]bool
}

func newFakeIngest() *fakeIngest {
	return &fakeIngest{
		memos:     map[string]*store.Memo{},
		byKey:     map[string]uuid.UUID{},
		arrivals:  map[uuid.UUID]int{},
		described: map[uuid.UUID]int{},
		pruned:    map[string]bool{},
	}
}

func rank(r string) int {
	switch r {
	case store.RetentionDiscardNow:
		return 0
	case store.RetentionDays30:
		return 1
	case store.RetentionForever:
		return 2
	}
	return -1
}

func (f *fakeIngest) find(id uuid.UUID) *store.Memo {
	for _, m := range f.memos {
		if m.ID == id {
			return m
		}
	}
	return nil
}

func (f *fakeIngest) IngestMemo(_ context.Context, in store.Arrival) (store.IngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++

	if in.IdempotencyKey != "" {
		if id, ok := f.byKey[keyOf(in.AuthorID, in.IdempotencyKey)]; ok {
			m := f.find(id)
			if m.ContentHash != in.ContentHash {
				return store.IngestResult{}, store.ErrKeyReused
			}
			// A retry: no arrival row, no state change. The count stays put.
			return store.IngestResult{Memo: *m, Deliveries: f.arrivals[id], Collapsed: true}, nil
		}
	}

	ident := keyOf(in.AuthorID, in.ContentHash)
	m, existed := f.memos[ident]
	if !existed {
		m = &store.Memo{
			ID:          uuid.New(),
			AuthorID:    in.AuthorID,
			ContentHash: in.ContentHash,
			ByteSize:    in.ByteSize,
			State:       store.StateCaptured,
			Retention:   store.RetentionDays30,
			CapturedAt:  time.Now(),
		}
		if in.Retention != "" {
			m.Retention = in.Retention
		}
		if in.OriginalFilename != "" {
			name := in.OriginalFilename
			m.OriginalFilename = &name
		}
		f.memos[ident] = m
	} else if in.Retention != "" && rank(in.Retention) > rank(m.Retention) {
		m.Retention = in.Retention // the ratchet: raise only
	}

	f.arrivals[m.ID]++
	if in.IdempotencyKey != "" {
		f.byKey[keyOf(in.AuthorID, in.IdempotencyKey)] = m.ID
	}
	return store.IngestResult{
		Memo:       *m,
		Deliveries: f.arrivals[m.ID],
		Collapsed:  f.arrivals[m.ID] > 1,
	}, nil
}

// SetMemoAudioInfo records what a probe found (CHRN-21). Kept on the same fake
// as IngestMemo because it is the same interface: both ingest paths describe a
// memo through the store they wrote it to.
// AudioPrunedFor is CHRN-22's re-delivery gate. The fake answers from
// `pruned`, which a test sets to put a memo in the state the pruner leaves.
func (f *fakeIngest) AudioPrunedFor(_ context.Context, authorID uuid.UUID, hash string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pruned[authorID.String()+"/"+hash], nil
}

func (f *fakeIngest) SetMemoAudioInfo(_ context.Context, id uuid.UUID, in store.AudioInfo) (store.Memo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.described[id]++
	m := f.find(id)
	if m == nil {
		return store.Memo{}, store.ErrNotFound
	}
	d, c, r := in.DurationMS, in.Codec, in.SampleRateHz
	m.DurationMS, m.Codec = &d, &c
	if r > 0 {
		m.SampleRateHz = &r
	}
	return *m, nil
}

// ---------------------------------------------------------------- harness

type rig struct {
	svc      *Service
	sessions *fakeSessions
	ingest   *fakeIngest
	audio    *audio.Store
	root     string
	author   uuid.UUID
	now      time.Time
}

func newRig(t *testing.T) *rig {
	t.Helper()
	root := t.TempDir()
	as, err := audio.New(root)
	if err != nil {
		t.Fatalf("audio.New: %v", err)
	}
	r := &rig{
		sessions: newFakeSessions(),
		ingest:   newFakeIngest(),
		audio:    as,
		root:     root,
		author:   uuid.New(),
		now:      time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC),
	}
	r.svc = r.service(t)
	return r
}

// service builds a Service over the same disk and the same fakes. Restarting
// the process is exactly this: new Service, everything durable untouched.
func (r *rig) service(t *testing.T) *Service {
	t.Helper()
	s, err := New(Options{
		Audio:    r.audio,
		Sessions: r.sessions,
		Ingest:   r.ingest,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return r.now },
	})
	if err != nil {
		t.Fatalf("upload.New: %v", err)
	}
	return s
}

// refFor names a recording the way the layout does.
func refFor(author uuid.UUID, hash string) audio.Ref {
	return audio.Ref{AuthorID: author, ContentHash: hash}
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// memoBytes is a stand-in for a recording. Long enough to be sent in pieces.
func memoBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

func (r *rig) open(t *testing.T, key string, content []byte, retention string) Result {
	t.Helper()
	res, err := r.svc.Open(context.Background(), OpenRequest{
		AuthorID:         r.author,
		IdempotencyKey:   key,
		ContentHash:      hashOf(content),
		ByteSize:         int64(len(content)),
		Retention:        retention,
		OriginalFilename: "memo.opus",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return res
}

func (r *rig) session(t *testing.T, id uuid.UUID) store.Upload {
	t.Helper()
	u, err := r.sessions.GetUpload(context.Background(), id)
	if err != nil {
		t.Fatalf("session %s: %v", id, err)
	}
	return u
}

// storedBytes reads back what ended up in the audio store for this memo.
func (r *rig) storedBytes(t *testing.T, hash string) []byte {
	t.Helper()
	p, err := r.audio.Path(audio.Ref{AuthorID: r.author, ContentHash: hash})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read stored recording: %v", err)
	}
	return b
}

const keyA = "idem-key-aaaaaaaaaaaa"
const keyB = "idem-key-bbbbbbbbbbbb"

// ---------------------------------------------------------------- Done-when 1

// "An interrupted upload resumes rather than restarts."
//
// The interruption is total: the transfer dies mid-chunk AND the process
// restarts, so nothing in memory survives. What the client gets back is the
// offset it actually reached, and the remainder is all it sends.
func TestInterruptedUploadResumesRatherThanRestarts(t *testing.T) {
	r := newRig(t)
	content := memoBytes(4096)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	if res.Session == nil || !res.Created {
		t.Fatalf("Open: want a fresh session, got %+v", res)
	}
	if res.Session.Offset != 0 {
		t.Fatalf("a new session starts at offset %d, want 0", res.Session.Offset)
	}
	id := res.Session.ID

	// First chunk lands.
	res, err := r.svc.Append(ctx, r.session(t, id), 0, bytes.NewReader(content[:1500]))
	if err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if res.Session.Offset != 1500 {
		t.Fatalf("after 1500 bytes the offset is %d", res.Session.Offset)
	}

	// Second chunk dies partway: 400 of the 1000 bytes it meant to send.
	_, err = r.svc.Append(ctx, r.session(t, id), 1500, iotest(content[1500:2500], 400))
	if err == nil {
		t.Fatal("a cut transfer should report an error")
	}

	// The process restarts. Nothing in memory survives; the file does.
	svc := r.service(t)

	got, err := svc.Status(ctx, r.session(t, id))
	if err != nil {
		t.Fatalf("Status after restart: %v", err)
	}
	if got.Session == nil {
		t.Fatalf("Status: want a session, got %+v", got)
	}
	// The bytes that landed before the cut are kept. Discarding them is the
	// behaviour this ticket exists to avoid.
	if got.Session.Offset != 1900 {
		t.Fatalf("resumed at offset %d, want 1900 — the 400 bytes that landed were lost", got.Session.Offset)
	}

	// The client sends the remainder and only the remainder.
	final, err := svc.Append(ctx, r.session(t, id), 1900, bytes.NewReader(content[1900:]))
	if err != nil {
		t.Fatalf("final chunk: %v", err)
	}
	if final.Committed == nil {
		t.Fatalf("the last byte should have committed the memo, got %+v", final)
	}
	if final.Committed.Memo.ByteSize != int64(len(content)) {
		t.Fatalf("memo byte_size %d, want %d", final.Committed.Memo.ByteSize, len(content))
	}
	if got := r.storedBytes(t, hashOf(content)); !bytes.Equal(got, content) {
		t.Fatalf("the stored recording is %d bytes and does not match the original (%d)", len(got), len(content))
	}
	// The session is spent and its staging file gone.
	if _, err := r.sessions.GetUpload(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the session should be gone after commit, got %v", err)
	}
	if n := stagingCount(t, r.audio); n != 0 {
		t.Fatalf("%d staging files left behind", n)
	}
}

// ---------------------------------------------------------------- Done-when 2

// "The same memo uploaded twice is one row."
//
// Twice under DIFFERENT keys, which is the case a delivery count can see. The
// second upload transfers nothing at all: the declared hash is enough to answer
// it at the first request.
func TestTheSameMemoUploadedTwiceIsOneRow(t *testing.T) {
	r := newRig(t)
	content := memoBytes(2048)
	ctx := context.Background()

	first := r.open(t, keyA, content, "")
	done, err := r.svc.Append(ctx, r.session(t, first.Session.ID), 0, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	if done.Committed == nil || done.Committed.Collapsed {
		t.Fatalf("the first upload should be a new memo, got %+v", done.Committed)
	}
	memoID := done.Committed.Memo.ID

	// Second delivery, different key, same bytes. Nothing is transferred.
	second := r.open(t, keyB, content, "")
	if second.Committed == nil {
		t.Fatalf("a second delivery of held bytes should commit at once, got %+v", second)
	}
	if second.Session != nil {
		t.Fatal("no session should be opened for bytes the author already holds")
	}
	if second.Committed.Memo.ID != memoID {
		t.Fatalf("two memos: %s and %s", memoID, second.Committed.Memo.ID)
	}
	if !second.Committed.Collapsed {
		t.Fatal("the second delivery should report as a duplicate")
	}
	if second.Committed.Deliveries != 2 {
		t.Fatalf("arrival count %d, want 2 — two deliveries of one memo", second.Committed.Deliveries)
	}
	if n := len(r.ingest.memos); n != 1 {
		t.Fatalf("%d memos, want 1", n)
	}
}

// A same-KEY retry is the case a delivery count cannot see, and it is the one
// CHRN-18 §10 and the round-1 finding on PR #7 are about: no arrival row is
// written, so Deliveries stays at 1 and only Collapsed reports the truth.
func TestSameKeyRetryCollapsesWithoutRaisingTheArrivalCount(t *testing.T) {
	r := newRig(t)
	content := memoBytes(1024)
	ctx := context.Background()

	first := r.open(t, keyA, content, "")
	done, err := r.svc.Append(ctx, r.session(t, first.Session.ID), 0, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if done.Committed.Deliveries != 1 {
		t.Fatalf("first delivery count %d, want 1", done.Committed.Deliveries)
	}

	// The phone never saw the response and retries the whole thing.
	for i := range 8 {
		retry := r.open(t, keyA, content, "")
		if retry.Committed == nil {
			t.Fatalf("retry %d: want a memo, got %+v", i, retry)
		}
		if !retry.Committed.Collapsed {
			t.Fatalf("retry %d is not reported as a duplicate; the log line would be missing", i)
		}
		if retry.Committed.Deliveries != 1 {
			t.Fatalf("retry %d raised the arrival count to %d — Deliveries > 1 is not the signal",
				i, retry.Committed.Deliveries)
		}
	}
	if n := len(r.ingest.memos); n != 1 {
		t.Fatalf("%d memos after eight retries, want 1", n)
	}
}

// ---------------------------------------------------------------- the shortcut's safety gate

// The already-held shortcut is gated on the FILE, not on the memo row. Without
// that gate a client could declare a hash it does not have the bytes for and
// mint a memo whose audio is missing — CHRN-23's one state that means something
// irreplaceable is gone.
func TestAlreadyHeldRequiresTheRecordingToBeOnDisk(t *testing.T) {
	r := newRig(t)
	content := memoBytes(512)
	ctx := context.Background()

	// A memo row exists for these bytes...
	if _, err := r.ingest.IngestMemo(ctx, store.Arrival{
		AuthorID:    r.author,
		ContentHash: hashOf(content),
		ByteSize:    int64(len(content)),
		Source:      store.SourceUpload,
		SourceRef:   "planted",
	}); err != nil {
		t.Fatalf("plant memo: %v", err)
	}
	// ...but nothing was ever written to disk for it.

	res := r.open(t, keyA, content, "")
	if res.Committed != nil {
		t.Fatal("the shortcut fired for a memo whose audio is not on disk; " +
			"the memo would keep pointing at a file that does not exist")
	}
	if res.Session == nil {
		t.Fatalf("want a session so the bytes are actually sent, got %+v", res)
	}
}

// The other half of the gate: a file of the WRONG size does not count as held.
func TestAlreadyHeldRejectsAFileOfTheWrongSize(t *testing.T) {
	r := newRig(t)
	content := memoBytes(512)

	p, err := r.audio.Path(audio.Ref{AuthorID: r.author, ContentHash: hashOf(content)})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, content[:100], 0o644); err != nil {
		t.Fatalf("plant truncated file: %v", err)
	}

	res := r.open(t, keyA, content, "")
	if res.Session == nil {
		t.Fatalf("a truncated file should not count as held, got %+v", res)
	}
}

// ---------------------------------------------------------------- offsets

// The 409 carries the server's own offset, which is what makes it a resume
// instruction rather than only a refusal.
func TestOffsetConflictCarriesTheServersOffset(t *testing.T) {
	r := newRig(t)
	content := memoBytes(1000)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	id := res.Session.ID
	if _, err := r.svc.Append(ctx, r.session(t, id), 0, bytes.NewReader(content[:300])); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	_, err := r.svc.Append(ctx, r.session(t, id), 0, bytes.NewReader(content[:300]))
	var conflict *OffsetConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("want an OffsetConflict, got %v", err)
	}
	if conflict.Offset != 300 {
		t.Fatalf("the conflict reports offset %d, want 300", conflict.Offset)
	}

	// Nothing was written by the rejected call.
	st, err := r.svc.Status(ctx, r.session(t, id))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Session.Offset != 300 {
		t.Fatalf("a rejected chunk moved the offset to %d", st.Session.Offset)
	}
}

// Concurrent chunks at the same offset: one wins, the other is told where the
// upload actually is. Without the per-session lock both would pass the offset
// check and interleave their writes.
func TestConcurrentAppendsAtOneOffsetProduceOneWinner(t *testing.T) {
	r := newRig(t)
	content := memoBytes(2000)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	u := r.session(t, res.Session.ID)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = r.svc.Append(ctx, u, 0, bytes.NewReader(content[:500]))
		}()
	}
	wg.Wait()

	var ok, conflicts int
	for _, err := range errs {
		var c *OffsetConflict
		switch {
		case err == nil:
			ok++
		case errors.As(err, &c):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("%d succeeded and %d conflicted, want exactly one of each", ok, conflicts)
	}
	st, err := r.svc.Status(ctx, r.session(t, res.Session.ID))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Session.Offset != 500 {
		t.Fatalf("offset %d after two racing 500-byte chunks, want 500", st.Session.Offset)
	}
}

// ---------------------------------------------------------------- refusals

// Bytes that do not match the declaration never become a memo, and the session
// is destroyed rather than left for the client to resume onto.
func TestHashMismatchIsRefusedAndDestroysTheSession(t *testing.T) {
	r := newRig(t)
	content := memoBytes(600)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	id := res.Session.ID

	// Same length, different bytes: only the hash can catch this.
	wrong := memoBytes(600)
	wrong[0] ^= 0xff

	_, err := r.svc.Append(ctx, r.session(t, id), 0, bytes.NewReader(wrong))
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("want ErrHashMismatch, got %v", err)
	}
	if len(r.ingest.memos) != 0 {
		t.Fatal("a memo was written from bytes that failed their hash check")
	}
	if _, err := r.sessions.GetUpload(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the session survived a hash mismatch: %v", err)
	}
	if n := stagingCount(t, r.audio); n != 0 {
		t.Fatalf("%d staging files left after a hash mismatch", n)
	}
	p, _ := r.audio.Path(audio.Ref{AuthorID: r.author, ContentHash: hashOf(content)})
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("unverified bytes were renamed into the audio store")
	}
}

// A client that sends more than it declared has the chunk discarded and the
// offset left exactly where the request found it. Distinct from a cut transfer,
// which keeps what landed — see Append.
func TestOversendDiscardsTheChunkAndLeavesTheOffset(t *testing.T) {
	r := newRig(t)
	content := memoBytes(1000)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	id := res.Session.ID
	if _, err := r.svc.Append(ctx, r.session(t, id), 0, bytes.NewReader(content[:400])); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	// 700 bytes offered where 600 remain.
	_, err := r.svc.Append(ctx, r.session(t, id), 400, bytes.NewReader(memoBytes(700)))
	if !errors.Is(err, ErrOversend) {
		t.Fatalf("want ErrOversend, got %v", err)
	}
	st, err := r.svc.Status(ctx, r.session(t, id))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Session.Offset != 400 {
		t.Fatalf("offset is %d after a discarded chunk, want 400", st.Session.Offset)
	}
	// And the upload still finishes normally from there.
	done, err := r.svc.Append(ctx, r.session(t, id), 400, bytes.NewReader(content[400:]))
	if err != nil {
		t.Fatalf("resume after an oversend: %v", err)
	}
	if done.Committed == nil {
		t.Fatal("the upload should complete after a discarded chunk")
	}
}

func TestDeclaredSizeAndOpenSessionsAreBounded(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	svc, err := New(Options{
		Audio: r.audio, Sessions: r.sessions, Ingest: r.ingest,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxBytes: 1000, MaxOpen: 2,
	})
	if err != nil {
		t.Fatalf("upload.New: %v", err)
	}

	_, err = svc.Open(ctx, OpenRequest{
		AuthorID: r.author, IdempotencyKey: keyA,
		ContentHash: hashOf(memoBytes(10)), ByteSize: 1001,
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}

	for i := range 2 {
		content := memoBytes(100 + i)
		if _, err := svc.Open(ctx, OpenRequest{
			AuthorID: r.author, IdempotencyKey: fmt.Sprintf("idem-key-%016d", i),
			ContentHash: hashOf(content), ByteSize: int64(len(content)),
		}); err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
	}
	content := memoBytes(300)
	_, err = svc.Open(ctx, OpenRequest{
		AuthorID: r.author, IdempotencyKey: "idem-key-one-too-many",
		ContentHash: hashOf(content), ByteSize: int64(len(content)),
	})
	if !errors.Is(err, ErrTooManyOpen) {
		t.Fatalf("want ErrTooManyOpen, got %v", err)
	}
}

// ---------------------------------------------------------------- recovery

// A crash in the window between the rename and the memo row leaves the
// recording on disk and the session still open. The client must not be told to
// send it all again.
func TestFinaliseRecoversFromACrashBetweenRenameAndMemo(t *testing.T) {
	r := newRig(t)
	content := memoBytes(800)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	id := res.Session.ID

	// Reproduce the crash state by hand: bytes in place, no staging file, the
	// session row still there.
	p, err := r.audio.Path(audio.Ref{AuthorID: r.author, ContentHash: hashOf(content)})
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("plant recording: %v", err)
	}

	got, err := r.svc.Status(ctx, r.session(t, id))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Committed == nil {
		t.Fatalf("want the memo to be finished from the bytes already on disk, got %+v", got)
	}
	if _, err := r.sessions.GetUpload(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the session should be spent, got %v", err)
	}
}

// A resume that presents the key rather than the id gets the same half-written
// session back. This is the path a reinstalled app takes.
func TestReopeningByKeyResumesTheSameSession(t *testing.T) {
	r := newRig(t)
	content := memoBytes(1000)
	ctx := context.Background()

	first := r.open(t, keyA, content, "")
	id := first.Session.ID
	if _, err := r.svc.Append(ctx, r.session(t, id), 0, bytes.NewReader(content[:250])); err != nil {
		t.Fatalf("first chunk: %v", err)
	}

	again := r.open(t, keyA, content, "")
	if again.Created {
		t.Fatal("re-presenting a key created a second session beside the first")
	}
	if again.Session.ID != id {
		t.Fatalf("got session %s, want %s", again.Session.ID, id)
	}
	if again.Session.Offset != 250 {
		t.Fatalf("the resumed session reports offset %d, want 250", again.Session.Offset)
	}
}

// A key presented with a different declaration is refused rather than resolved
// — otherwise a resume attaches to the wrong file and only the hash check, a
// whole transfer later, notices.
func TestKeyReusedForDifferentContentIsRefused(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	first := memoBytes(500)
	r.open(t, keyA, first, "")

	other := memoBytes(700)
	_, err := r.svc.Open(ctx, OpenRequest{
		AuthorID: r.author, IdempotencyKey: keyA,
		ContentHash: hashOf(other), ByteSize: int64(len(other)),
	})
	if !errors.Is(err, store.ErrUploadKeyReused) {
		t.Fatalf("want ErrUploadKeyReused, got %v", err)
	}
}

// ---------------------------------------------------------------- retention

// "Carries the capture-time retention choice as a field, so the decision made
// at the moment of recording survives the trip."
func TestRetentionSurvivesTheTrip(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	for _, level := range []string{store.RetentionDiscardNow, store.RetentionForever} {
		content := memoBytes(200 + len(level))
		key := "idem-key-" + level + "-000000"
		res := r.open(t, key, content, level)
		done, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("%s: upload: %v", level, err)
		}
		if done.Committed.Memo.Retention != level {
			t.Fatalf("%s: memo carries retention %q", level, done.Committed.Memo.Retention)
		}
	}
}

// The ratchet, from this side: a later delivery declaring the default must not
// undo a FOREVER pin.
func TestARedeliveryCannotLowerRetention(t *testing.T) {
	r := newRig(t)
	content := memoBytes(400)
	ctx := context.Background()

	res := r.open(t, keyA, content, store.RetentionForever)
	if _, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content)); err != nil {
		t.Fatalf("first upload: %v", err)
	}

	again := r.open(t, keyB, content, store.RetentionDays30)
	if again.Committed.Memo.Retention != store.RetentionForever {
		t.Fatalf("a re-delivery lowered retention to %q", again.Committed.Memo.Retention)
	}
}

// ---------------------------------------------------------------- helpers

func stagingCount(t *testing.T, as *audio.Store) int {
	t.Helper()
	entries, err := os.ReadDir(as.StagingRoot())
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read staging: %v", err)
	}
	return len(entries)
}

// iotest returns a reader that yields the first n bytes of b and then fails, so
// a cut transfer can be reproduced without a network.
func iotest(b []byte, n int) io.Reader {
	return io.MultiReader(bytes.NewReader(b[:n]), errReader{})
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("connection reset by peer")
}

// The already-held shortcut is also the crash-recovery path: bytes renamed into
// place, memo never written, session row still holding the key. Committing must
// release that key rather than leaving it for the sweep a week later — otherwise
// the client cannot reuse it and the row outlives everything it describes.
func TestRecoveringByKeyAfterACrashReleasesTheSession(t *testing.T) {
	r := newRig(t)
	content := memoBytes(700)
	ctx := context.Background()

	opened := r.open(t, keyA, content, "")
	id := opened.Session.ID

	// The crash state: recording on disk, no staging file, session row intact.
	p, err := r.audio.Path(refFor(r.author, hashOf(content)))
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		t.Fatalf("plant recording: %v", err)
	}

	again := r.open(t, keyA, content, "")
	if again.Committed == nil {
		t.Fatalf("want the memo finished from the bytes on disk, got %+v", again)
	}
	if _, err := r.sessions.GetUpload(ctx, id); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the spent session still holds its key: %v", err)
	}
	// And the key is genuinely free: a different recording can claim it.
	other := memoBytes(900)
	if _, err := r.svc.Open(ctx, OpenRequest{
		AuthorID: r.author, IdempotencyKey: keyA,
		ContentHash: hashOf(other), ByteSize: int64(len(other)),
	}); err != nil {
		t.Fatalf("the key was not released: %v", err)
	}
}

// PR #14 review, Important. finalise's no-staging-file branch used to commit on
// the strength of a comment about what offset() had checked. It is checked here
// instead, because a comment asserting a caller's work is not a check — and the
// failure it prevents is a memo whose audio is not on disk, which is CHRN-23's
// `missing`.
//
// Called directly: no request can reach this state on its own any more, which
// is the point. The invariant has to hold even when the caller was wrong.
func TestFinaliseRefusesToCommitWhenTheBytesAreGone(t *testing.T) {
	r := newRig(t)
	content := memoBytes(500)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	u := r.session(t, res.Session.ID)

	// Neither a staging file nor a recording: the state a racing finalise or a
	// concurrent Abandon leaves behind.
	if _, err := r.svc.finalise(ctx, u); !errors.Is(err, ErrStagingLost) {
		t.Fatalf("want ErrStagingLost, got %v", err)
	}
	if n := len(r.ingest.memos); n != 0 {
		t.Fatalf("%d memos written with no audio on disk", n)
	}
	// The declaration still stands, so the client can simply send them again.
	if _, err := r.sessions.GetUpload(ctx, u.ID); err != nil {
		t.Fatalf("the session was destroyed; the client would have to re-declare: %v", err)
	}
	done, err := r.svc.Append(ctx, r.session(t, u.ID), 0, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("resending after the bytes were lost: %v", err)
	}
	if done.Committed == nil {
		t.Fatal("the resend did not complete the upload")
	}
}

// The other half of the same finding: Status and Open take the session lock,
// which they did not, so two overlapping requests could both enter finalise.
// The one that mattered was a mismatched upload — the first removes the staging
// file, and the second used to fall through and commit.
func TestConcurrentFinalisesCannotWriteAMemoWithNoAudio(t *testing.T) {
	r := newRig(t)
	content := memoBytes(900)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	u := r.session(t, res.Session.ID)

	// A complete staging file whose bytes are NOT what was declared: same
	// length, different content, so only the hash can tell.
	wrong := memoBytes(900)
	wrong[0] ^= 0xff
	path, err := r.audio.StagingPath(u.ID)
	if err != nil {
		t.Fatalf("StagingPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, wrong, 0o644); err != nil {
		t.Fatalf("write staging: %v", err)
	}

	var wg sync.WaitGroup
	results := make([]Result, 4)
	errs := make([]error, 4)
	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = r.svc.Status(ctx, u)
		}()
	}
	wg.Wait()

	// The assertion that matters: whatever order they ran in, no memo exists,
	// because no bytes matching the declaration were ever on disk.
	if n := len(r.ingest.memos); n != 0 {
		t.Fatalf("%d memos written for an upload that never matched its hash", n)
	}
	for i, err := range errs {
		if err == nil && results[i].Committed != nil {
			t.Fatalf("call %d committed a memo with no audio behind it", i)
		}
		if err != nil && !errors.Is(err, ErrHashMismatch) && !errors.Is(err, ErrStagingLost) {
			t.Fatalf("call %d: unexpected error %v", i, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the mismatched staging file was left on disk")
	}
	final, err := r.audio.Path(refFor(r.author, hashOf(content)))
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if _, err := os.Stat(final); !os.IsNotExist(err) {
		t.Fatal("unverified bytes reached the audio store")
	}
}

// PR #14 review, nit 1. The open-session cap used to be checked before
// OpenUpload, so it refused a RESUME — the one request that must not be refused
// when an account is full of stalled sessions, because DELETE needs the id the
// client has just lost.
func TestTheOpenSessionCapDoesNotRefuseAResume(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	svc, err := New(Options{
		Audio: r.audio, Sessions: r.sessions, Ingest: r.ingest,
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		MaxOpen: 3,
	})
	if err != nil {
		t.Fatalf("upload.New: %v", err)
	}

	// Fill the account to the cap, keeping the first one's key.
	var firstKey string
	var firstID uuid.UUID
	for i := range 3 {
		content := memoBytes(500 + i)
		key := fmt.Sprintf("idem-key-%016d", i)
		res, err := svc.Open(ctx, OpenRequest{
			AuthorID: r.author, IdempotencyKey: key,
			ContentHash: hashOf(content), ByteSize: int64(len(content)),
		})
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if i == 0 {
			firstKey, firstID = key, res.Session.ID
		}
	}

	// A NEW session is refused, as it should be.
	fresh := memoBytes(4000)
	if _, err := svc.Open(ctx, OpenRequest{
		AuthorID: r.author, IdempotencyKey: "idem-key-one-too-many",
		ContentHash: hashOf(fresh), ByteSize: int64(len(fresh)),
	}); !errors.Is(err, ErrTooManyOpen) {
		t.Fatalf("a new session at the cap: want ErrTooManyOpen, got %v", err)
	}
	// And the refusal left nothing behind — the rollback has to be real, or the
	// cap ratchets down by one on every refused attempt.
	if n, _ := r.sessions.CountOpenUploads(ctx, r.author); n != 3 {
		t.Fatalf("%d sessions after a refused open, want 3", n)
	}

	// The resume is not.
	content := memoBytes(500)
	again, err := svc.Open(ctx, OpenRequest{
		AuthorID: r.author, IdempotencyKey: firstKey,
		ContentHash: hashOf(content), ByteSize: int64(len(content)),
	})
	if err != nil {
		t.Fatalf("resuming at the cap was refused: %v", err)
	}
	if again.Session == nil || again.Session.ID != firstID {
		t.Fatalf("the resume did not return the existing session: %+v", again)
	}
}

// A cut transfer is typed so the handler can classify it. The offset it carries
// is where the upload now stands, because the bytes that landed were kept.
func TestACutTransferReportsWhereItGotTo(t *testing.T) {
	r := newRig(t)
	content := memoBytes(2000)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	u := r.session(t, res.Session.ID)

	_, err := r.svc.Append(ctx, u, 0, iotest(content, 750))
	var cut *TransferCut
	if !errors.As(err, &cut) {
		t.Fatalf("want a *TransferCut, got %v", err)
	}
	if cut.Offset != 750 {
		t.Fatalf("the cut reports offset %d, want 750", cut.Offset)
	}
	st, err := r.svc.Status(ctx, r.session(t, u.ID))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Session.Offset != 750 {
		t.Fatalf("the session is at %d, so the error disagrees with the file", st.Session.Offset)
	}
}

// ---------------------------------------------------------------- CHRN-21

// opusStream builds a minimal but valid Ogg Opus file of the given length. Its
// byte layout is asserted against a real encoder in internal/audio; here it only
// has to be something Probe accepts, so the wiring can be tested end to end
// without a checked-in binary fixture.
func opusStream(seconds int) []byte {
	const preSkip = 312
	page := func(granule int64, seq uint32, payload []byte) []byte {
		var segs []byte
		for n := len(payload); ; n -= 255 {
			if n < 255 {
				segs = append(segs, byte(n))
				break
			}
			segs = append(segs, 255)
		}
		p := append([]byte{}, 'O', 'g', 'g', 'S', 0, 0)
		p = binary.LittleEndian.AppendUint64(p, uint64(granule))
		p = binary.LittleEndian.AppendUint32(p, 1)
		p = binary.LittleEndian.AppendUint32(p, seq)
		p = binary.LittleEndian.AppendUint32(p, 0)
		p = append(p, byte(len(segs)))
		p = append(p, segs...)
		return append(p, payload...)
	}
	head := append([]byte("OpusHead"), 1, 1)
	head = binary.LittleEndian.AppendUint16(head, preSkip)
	head = binary.LittleEndian.AppendUint32(head, 48000)
	head = append(head, 0, 0, 0)

	b := page(0, 0, head)
	b = append(b, page(0, 1, []byte("OpusTags\x00\x00\x00\x00\x00\x00\x00\x00"))...)
	return append(b, page(int64(seconds)*48000+preSkip, 2, make([]byte, 64))...)
}

// The upload path describes what it stored: duration, codec, sample rate, read
// from the headers of the file it has just written.
func TestAnUploadedRecordingIsDescribed(t *testing.T) {
	r := newRig(t)
	content := opusStream(3)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	done, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	m := r.ingest.find(done.Committed.Memo.ID)
	if m.DurationMS == nil {
		t.Fatal("the memo was never described")
	}
	if *m.DurationMS != 3000 {
		t.Fatalf("duration %d ms, want 3000", *m.DurationMS)
	}
	if m.Codec == nil || *m.Codec != audio.CodecOpus {
		t.Fatalf("codec %v, want %q", m.Codec, audio.CodecOpus)
	}
}

// Described once per memo, not once per delivery. The guard is DurationMS being
// nil, which also means a memo whose probe FAILED gets another attempt — that
// is the whole retry story and it costs nothing.
func TestARecordingIsDescribedOncePerMemoNotPerDelivery(t *testing.T) {
	r := newRig(t)
	content := opusStream(2)
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	done, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	id := done.Committed.Memo.ID

	// Four more deliveries: three same-key retries and one under a fresh key.
	for range 3 {
		r.open(t, keyA, content, "")
	}
	r.open(t, keyB, content, "")

	if n := r.ingest.described[id]; n != 1 {
		t.Fatalf("the recording was described %d times across five deliveries, want 1", n)
	}
}

// "A corrupt or non-Opus file fails loudly and leaves the columns NULL, rather
// than producing silence OR rejecting the memo." The second half is the one
// worth a test: a recording Chronicle cannot parse is still somebody's memo.
func TestAnUnreadableRecordingStillBecomesAMemo(t *testing.T) {
	r := newRig(t)
	content := memoBytes(4096) // not Opus, not Ogg, not anything
	ctx := context.Background()

	res := r.open(t, keyA, content, "")
	done, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("an unreadable recording failed the upload: %v", err)
	}
	if done.Committed == nil {
		t.Fatal("no memo for a file whose headers could not be read")
	}
	m := r.ingest.find(done.Committed.Memo.ID)
	if m.DurationMS != nil || m.Codec != nil {
		t.Fatalf("columns were filled for an unparseable file: %v / %v", m.DurationMS, m.Codec)
	}
	if n := r.ingest.described[done.Committed.Memo.ID]; n != 0 {
		t.Fatalf("SetMemoAudioInfo was called %d times for a file that could not be probed", n)
	}
	// And the bytes are still there, byte-for-byte. Failing to describe a
	// recording must not cost the recording.
	if got := r.storedBytes(t, hashOf(content)); !bytes.Equal(got, content) {
		t.Fatal("the stored recording does not match what was uploaded")
	}
}

// --- CHRN-22 Ruling 2: audio is delivered once -----------------------------

// A RE-UPLOAD OF A PRUNED MEMO TRANSFERS NOTHING. Resurrecting cannot work:
// captured_at is immutable, so a memo past its window would be re-pruned by the
// next sweep and the upload would have bought nothing.
func TestReUploadingAPrunedMemoTransfersNothing(t *testing.T) {
	r := newRig(t)
	author := uuid.New()
	body := []byte("pretend this is opus")
	hash := hashOf(body)

	// The memo exists and its audio has been pruned: no file on disk, and the
	// row says so.
	r.ingest.pruned[author.String()+"/"+hash] = true

	res, err := r.svc.Open(context.Background(), OpenRequest{
		AuthorID:       author,
		IdempotencyKey: "prune-redelivery-0001",
		ContentHash:    hash,
		ByteSize:       int64(len(body)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Committed == nil {
		t.Fatal("a re-delivery of a pruned memo opened a session; the client would send " +
			"bytes this system has deliberately stopped keeping")
	}
	if res.Session != nil {
		t.Fatal("a session was opened alongside the committed answer")
	}
}

// AND THE HEALING PATH STILL HEALS. A memo whose audio is merely MISSING —
// CHRN-23's one irrecoverable state, and the shape a crash between the rename
// and the memo row leaves — must still be repaired by the client sending the
// bytes. Ruling 2 splits this branch; it does not replace it.
func TestAMemoWhoseAudioIsMissingIsStillHealed(t *testing.T) {
	r := newRig(t)
	author := uuid.New()
	body := []byte("pretend this is opus")
	hash := hashOf(body)

	// Not pruned — just not on disk.
	res, err := r.svc.Open(context.Background(), OpenRequest{
		AuthorID:       author,
		IdempotencyKey: "missing-heal-000001",
		ContentHash:    hash,
		ByteSize:       int64(len(body)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Session == nil {
		t.Fatal("no session for a memo whose audio is missing; the self-healing path " +
			"CHRN-20 built has been closed by CHRN-22's split")
	}
}
