package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/upload"
)

// CHRN-20 over the wire. The protocol's own behaviour is tested in
// internal/upload; what is asserted here is the HTTP contract — which status
// answers which refusal, that Upload-Offset is always present, and that a
// session belonging to somebody else is invisible rather than forbidden.

// ---------------------------------------------------------------- fakes

// uploadSessions is tier1.memo_uploads as a map. The embedded interface makes
// any method these tests do not exercise panic by name rather than return a
// zero value that quietly satisfies an assertion.
type uploadSessions struct {
	upload.Sessions
	mu    sync.Mutex
	byID  map[uuid.UUID]store.Upload
	byKey map[string]uuid.UUID
}

func newUploadSessions() *uploadSessions {
	return &uploadSessions{byID: map[uuid.UUID]store.Upload{}, byKey: map[string]uuid.UUID{}}
}

func sessionKey(author uuid.UUID, key string) string { return author.String() + ":" + key }

func (f *uploadSessions) OpenUpload(_ context.Context, in store.Upload) (store.Upload, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, ok := f.byKey[sessionKey(in.AuthorID, in.IdempotencyKey)]; ok {
		got := f.byID[id]
		if got.ContentHash != in.ContentHash || got.ByteSize != in.ByteSize {
			return store.Upload{}, false, store.ErrUploadKeyReused
		}
		return got, false, nil
	}
	in.ID = uuid.New()
	in.CreatedAt, in.LastActivityAt = time.Now(), time.Now()
	f.byID[in.ID] = in
	f.byKey[sessionKey(in.AuthorID, in.IdempotencyKey)] = in.ID
	return in, true, nil
}

func (f *uploadSessions) GetUpload(_ context.Context, id uuid.UUID) (store.Upload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return store.Upload{}, store.ErrNotFound
	}
	return u, nil
}

func (f *uploadSessions) TouchUpload(context.Context, uuid.UUID) error { return nil }

func (f *uploadSessions) DeleteUpload(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.byID[id]; ok {
		delete(f.byKey, sessionKey(u.AuthorID, u.IdempotencyKey))
		delete(f.byID, id)
	}
	return nil
}

func (f *uploadSessions) ClearUploadKey(_ context.Context, author uuid.UUID, key string) error {
	f.mu.Lock()
	id, ok := f.byKey[sessionKey(author, key)]
	f.mu.Unlock()
	if !ok {
		return nil
	}
	return f.DeleteUpload(context.Background(), id)
}

func (f *uploadSessions) CountOpenUploads(_ context.Context, author uuid.UUID) (int, error) {
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

// uploadIngest is enough of CHRN-18's rule for the wire tests: one memo per
// (author, hash), and a same-key retry that writes no arrival row.
type uploadIngest struct {
	mu       sync.Mutex
	memos    map[string]store.Memo
	byKey    map[string]uuid.UUID
	arrivals map[uuid.UUID]int
}

func newUploadIngest() *uploadIngest {
	return &uploadIngest{
		memos:    map[string]store.Memo{},
		byKey:    map[string]uuid.UUID{},
		arrivals: map[uuid.UUID]int{},
	}
}

func (f *uploadIngest) IngestMemo(_ context.Context, in store.Arrival) (store.IngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ident := sessionKey(in.AuthorID, in.ContentHash)
	if id, ok := f.byKey[sessionKey(in.AuthorID, in.IdempotencyKey)]; ok {
		for _, m := range f.memos {
			if m.ID == id {
				return store.IngestResult{Memo: m, Deliveries: f.arrivals[id], Collapsed: true}, nil
			}
		}
	}
	m, existed := f.memos[ident]
	if !existed {
		m = store.Memo{
			ID: uuid.New(), AuthorID: in.AuthorID, ContentHash: in.ContentHash,
			ByteSize: in.ByteSize, State: store.StateCaptured,
			Retention: store.RetentionDays30, CapturedAt: time.Now(),
		}
		if in.Retention != "" {
			m.Retention = in.Retention
		}
		f.memos[ident] = m
	}
	f.arrivals[m.ID]++
	f.byKey[sessionKey(in.AuthorID, in.IdempotencyKey)] = m.ID
	return store.IngestResult{Memo: m, Deliveries: f.arrivals[m.ID], Collapsed: f.arrivals[m.ID] > 1}, nil
}

// ---------------------------------------------------------------- harness

type uploadRig struct {
	handler  http.Handler
	accounts *fakeAccounts
	sessions *uploadSessions
	ingest   *uploadIngest
	me       store.User
	token    string
}

func newUploadRig(t *testing.T) *uploadRig {
	t.Helper()
	as, err := audio.New(t.TempDir())
	if err != nil {
		t.Fatalf("audio.New: %v", err)
	}
	sessions, ingest := newUploadSessions(), newUploadIngest()
	svc, err := newUploadService(as, sessions, ingest)
	if err != nil {
		t.Fatalf("upload.New: %v", err)
	}

	accounts := newFakeAccounts()
	me := person("me@example.com", true)
	accounts.sessions["chr_session_me"] = me

	return &uploadRig{
		handler: NewRouter(Deps{
			DB: fakePinger{}, Accounts: accounts, Logger: discardLogger(),
			Version: "test", SecureCookies: true, Audio: as, Uploads: svc,
		}),
		accounts: accounts,
		sessions: sessions,
		ingest:   ingest,
		me:       me,
		token:    "chr_session_me",
	}
}

// newUploadService builds the service the router is given, the way
// cmd/chronicle does.
func newUploadService(as *audio.Store, s upload.Sessions, i upload.Ingestor) (*upload.Service, error) {
	return upload.New(upload.Options{Audio: as, Sessions: s, Ingest: i, Logger: discardLogger()})
}

func (r *uploadRig) do(req *http.Request) *httptest.ResponseRecorder {
	req.Header.Set("Authorization", "Bearer "+r.token)
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec
}

func (r *uploadRig) openUpload(t *testing.T, key string, content []byte) uploadResponse {
	t.Helper()
	body := fmt.Sprintf(`{"idempotency_key":%q,"content_hash":%q,"byte_size":%d,"original_filename":"memo.opus"}`,
		key, digestOf(content), len(content))
	rec := r.do(jsonReq(http.MethodPost, "/memos/uploads", body))
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("open: status %d, body %s", rec.Code, rec.Body.String())
	}
	return decodeUpload(t, rec)
}

func (r *uploadRig) appendChunk(id string, offset int, chunk []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/memos/uploads/"+id, bytes.NewReader(chunk))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set(UploadOffsetHeader, strconv.Itoa(offset))
	req.ContentLength = int64(len(chunk))
	return r.do(req)
}

func decodeUpload(t *testing.T, rec *httptest.ResponseRecorder) uploadResponse {
	t.Helper()
	var got uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	// Every upload response carries the offset in a header as well, so a client
	// never has to parse a body to know where it is.
	if h := rec.Header().Get(UploadOffsetHeader); h != strconv.FormatInt(got.Offset, 10) {
		t.Fatalf("%s header is %q but the body says %d", UploadOffsetHeader, h, got.Offset)
	}
	return got
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func audioBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('a' + i%26)
	}
	return b
}

const testKey = "idem-key-for-tests-01"

// ---------------------------------------------------------------- the happy path

func TestAnUploadCanBeSentInPiecesAndBecomesOneMemo(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(3000)

	open := r.openUpload(t, testKey, content)
	if open.Status != "incomplete" || open.Offset != 0 {
		t.Fatalf("open: %+v", open)
	}
	if open.UploadID == "" {
		t.Fatal("open returned no upload_id")
	}
	if open.ExpiresAt == nil {
		t.Fatal("open returned no expires_at; a client cannot tell how long it has")
	}

	rec := r.appendChunk(open.UploadID, 0, content[:1200])
	if rec.Code != http.StatusOK {
		t.Fatalf("first chunk: %d %s", rec.Code, rec.Body.String())
	}
	mid := decodeUpload(t, rec)
	if mid.Status != "incomplete" || mid.Offset != 1200 {
		t.Fatalf("after the first chunk: %+v", mid)
	}

	rec = r.appendChunk(open.UploadID, 1200, content[1200:])
	if rec.Code != http.StatusOK {
		t.Fatalf("final chunk: %d %s", rec.Code, rec.Body.String())
	}
	done := decodeUpload(t, rec)
	if done.Status != "complete" {
		t.Fatalf("the last chunk did not complete the upload: %+v", done)
	}
	if done.Memo == nil {
		t.Fatal("a completed upload returned no memo")
	}
	if done.Memo.ContentHash != digestOf(content) {
		t.Fatalf("memo hash %s, want %s", done.Memo.ContentHash, digestOf(content))
	}
	if done.Duplicate {
		t.Fatal("a first delivery was reported as a duplicate")
	}
	// The memo shape carries no path: CHRN-23 derives one from the row and a
	// client has no business building its own.
	if bytes.Contains(rec.Body.Bytes(), []byte("/tmp")) {
		t.Fatalf("the response leaks a filesystem path: %s", rec.Body.String())
	}
}

// Re-delivery over the wire: a second upload of the same bytes under a new key
// answers immediately, with no session to send anything to.
func TestReDeliveryAnswersWithoutASession(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(800)

	open := r.openUpload(t, testKey, content)
	if rec := r.appendChunk(open.UploadID, 0, content); rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	body := fmt.Sprintf(`{"idempotency_key":%q,"content_hash":%q,"byte_size":%d}`,
		"idem-key-second-attempt", digestOf(content), len(content))
	rec := r.do(jsonReq(http.MethodPost, "/memos/uploads", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("re-delivery: status %d, want 200 (201 would mean a session was opened)", rec.Code)
	}
	got := decodeUpload(t, rec)
	if got.Status != "complete" {
		t.Fatalf("re-delivery: %+v", got)
	}
	if !got.Duplicate {
		t.Fatal("a re-delivery is not flagged as a duplicate")
	}
	if got.UploadID != "" {
		t.Fatalf("a session was opened for bytes already held: %s", got.UploadID)
	}
	if n := len(r.ingest.memos); n != 1 {
		t.Fatalf("%d memos, want 1", n)
	}
}

// ---------------------------------------------------------------- refusals

// The 409 carries where to resume, in the header and in the body.
func TestOffsetConflictAnswers409CarryingTheServersOffset(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(2000)

	open := r.openUpload(t, testKey, content)
	if rec := r.appendChunk(open.UploadID, 0, content[:700]); rec.Code != http.StatusOK {
		t.Fatalf("first chunk: %d", rec.Code)
	}

	rec := r.appendChunk(open.UploadID, 0, content[:700])
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
	if got := rec.Header().Get(UploadOffsetHeader); got != "700" {
		t.Fatalf("%s = %q on the conflict, want \"700\" — without it the client cannot resume",
			UploadOffsetHeader, got)
	}
	if got := decodeUpload(t, rec); got.Offset != 700 {
		t.Fatalf("conflict body reports offset %d, want 700", got.Offset)
	}
}

// A chunk longer than the upload has left is refused from the header, before
// the body is read.
func TestAnOversizedChunkIsRefusedFromContentLength(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(1000)

	open := r.openUpload(t, testKey, content)
	rec := r.appendChunk(open.UploadID, 0, audioBytes(1400))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
}

func TestAppendRequiresOctetStreamAndALength(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(500)
	open := r.openUpload(t, testKey, content)

	t.Run("wrong content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/memos/uploads/"+open.UploadID, bytes.NewReader(content))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(UploadOffsetHeader, "0")
		if rec := r.do(req); rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("status %d, want 415", rec.Code)
		}
	})

	// A chunked body has no length, so an oversized chunk could not be refused
	// before it was read.
	t.Run("chunked body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/memos/uploads/"+open.UploadID, bytes.NewReader(content))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set(UploadOffsetHeader, "0")
		req.ContentLength = -1
		if rec := r.do(req); rec.Code != http.StatusLengthRequired {
			t.Fatalf("status %d, want 411", rec.Code)
		}
	})

	t.Run("missing offset header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/memos/uploads/"+open.UploadID, bytes.NewReader(content))
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = int64(len(content))
		if rec := r.do(req); rec.Code != http.StatusBadRequest {
			t.Fatalf("status %d, want 400", rec.Code)
		}
	})
}

func TestOpenValidatesTheDeclaration(t *testing.T) {
	r := newUploadRig(t)
	good := digestOf(audioBytes(10))

	for name, body := range map[string]string{
		"short key":     fmt.Sprintf(`{"idempotency_key":"tooshort","content_hash":%q,"byte_size":10}`, good),
		"bad hash":      `{"idempotency_key":"idem-key-for-tests-01","content_hash":"nope","byte_size":10}`,
		"uppercase hex": fmt.Sprintf(`{"idempotency_key":"idem-key-for-tests-01","content_hash":%q,"byte_size":10}`, "A"+good[1:]),
		"zero size":     fmt.Sprintf(`{"idempotency_key":"idem-key-for-tests-01","content_hash":%q,"byte_size":0}`, good),
		"bad retention": fmt.Sprintf(`{"idempotency_key":"idem-key-for-tests-01","content_hash":%q,"byte_size":10,"retention":"someday"}`, good),
	} {
		t.Run(name, func(t *testing.T) {
			rec := r.do(jsonReq(http.MethodPost, "/memos/uploads", body))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400 — body was %s", rec.Code, rec.Body.String())
			}
		})
	}

	// And a declaration larger than the service accepts is 413, not 400: the
	// request is well formed, it is just too big to agree to.
	huge := fmt.Sprintf(`{"idempotency_key":"idem-key-for-tests-01","content_hash":%q,"byte_size":%d}`,
		good, upload.DefaultMaxBytes+1)
	if rec := r.do(jsonReq(http.MethodPost, "/memos/uploads", huge)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status %d, want 413", rec.Code)
	}
}

// Somebody else's upload is invisible, not forbidden. A 403 would confirm that
// the id names a real session, which is a fact about another account.
func TestAnotherAccountsUploadIsNotFound(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(400)
	open := r.openUpload(t, testKey, content)

	// A second signed-in account, with a valid session of its own.
	other := person("other@example.com", false)
	r.accounts.sessions["chr_session_other"] = other
	r.token = "chr_session_other"

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		rec := r.do(httptest.NewRequest(method, "/memos/uploads/"+open.UploadID, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, want 404 (403 would confirm the session exists)", method, rec.Code)
		}
	}
}

// Unset CHRONICLE_AUDIO_DIR is 503 naming the variable, not 404. Same shape the
// storage report uses: "not configured here" and "wrong URL" are different
// facts and a client should be able to tell them apart.
func TestUploadsReport503WhenNoAudioStoreIsConfigured(t *testing.T) {
	accounts := newFakeAccounts()
	me := person("me@example.com", true)
	accounts.sessions["chr_session_me"] = me
	h := NewRouter(Deps{
		DB: fakePinger{}, Accounts: accounts, Logger: discardLogger(),
		Version: "test", SecureCookies: true,
	})

	req := jsonReq(http.MethodPost, "/memos/uploads", `{}`)
	req.Header.Set("Authorization", "Bearer chr_session_me")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("CHRONICLE_AUDIO_DIR")) {
		t.Fatalf("the 503 does not name the variable to set: %s", rec.Body.String())
	}
}

// Abandoning frees the session and its bytes, and the id stops resolving.
func TestAbandonReleasesTheSession(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(600)
	open := r.openUpload(t, testKey, content)
	if rec := r.appendChunk(open.UploadID, 0, content[:200]); rec.Code != http.StatusOK {
		t.Fatalf("chunk: %d", rec.Code)
	}

	rec := r.do(httptest.NewRequest(http.MethodDelete, "/memos/uploads/"+open.UploadID, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("abandon: status %d, want 204", rec.Code)
	}
	rec = r.do(httptest.NewRequest(http.MethodGet, "/memos/uploads/"+open.UploadID, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after abandon: status %d, want 404", rec.Code)
	}
}

// GET reports the offset, which is how a client that crashed mid-chunk finds
// out where to resume without guessing.
func TestStatusReportsWhereToResume(t *testing.T) {
	r := newUploadRig(t)
	content := audioBytes(1500)
	open := r.openUpload(t, testKey, content)
	if rec := r.appendChunk(open.UploadID, 0, content[:640]); rec.Code != http.StatusOK {
		t.Fatalf("chunk: %d", rec.Code)
	}

	rec := r.do(httptest.NewRequest(http.MethodGet, "/memos/uploads/"+open.UploadID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	got := decodeUpload(t, rec)
	if got.Offset != 640 || got.ByteSize != 1500 {
		t.Fatalf("status reports offset %d of %d, want 640 of 1500", got.Offset, got.ByteSize)
	}
}
