package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// --- fakes -----------------------------------------------------------------

// fakeIngest is a stand-in for the store's ingest, implementing just enough of
// CHRN-18's rule to test the watcher against it: one memo per (author, hash),
// and a repeat sighting of a known (memo, source, ref) collapses without
// writing an arrival.
type fakeIngest struct {
	mu        sync.Mutex
	memos     map[string]uuid.UUID // author:hash -> memo id
	arrivals  map[string]int       // memo id -> arrival count
	sightings map[string]bool      // memo:source:ref
	calls     []store.Arrival
	err       error
}

func newFakeIngest() *fakeIngest {
	return &fakeIngest{
		memos:     map[string]uuid.UUID{},
		arrivals:  map[string]int{},
		sightings: map[string]bool{},
	}
}

func (f *fakeIngest) IngestMemo(_ context.Context, in store.Arrival) (store.IngestResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.IngestResult{}, f.err
	}
	f.calls = append(f.calls, in)

	key := in.AuthorID.String() + ":" + in.ContentHash
	id, known := f.memos[key]
	if !known {
		id = uuid.New()
		f.memos[key] = id
	}
	sighting := id.String() + ":" + in.Source + ":" + in.SourceRef
	collapsed := known
	if !f.sightings[sighting] {
		f.sightings[sighting] = true
		f.arrivals[id.String()]++
	} else {
		collapsed = true
	}
	return store.IngestResult{
		Memo:       store.Memo{ID: id, AuthorID: in.AuthorID, ContentHash: in.ContentHash, ByteSize: in.ByteSize},
		Deliveries: f.arrivals[id.String()],
		Collapsed:  collapsed,
	}, nil
}

func (f *fakeIngest) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.memos)
}

type fakeLedger struct {
	mu       sync.Mutex
	entries  store.SeenIndex
	failMark bool
}

func newFakeLedger() *fakeLedger { return &fakeLedger{entries: store.SeenIndex{}} }

func (l *fakeLedger) LoadSeen(context.Context) (store.SeenIndex, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := store.SeenIndex{}
	for k, v := range l.entries {
		out[k] = v
	}
	return out, nil
}

func (l *fakeLedger) MarkSeen(_ context.Context, f store.SeenFile) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failMark {
		return errors.New("ledger unavailable")
	}
	l.entries[f.Path] = f
	return nil
}

type fakeAccounts struct{ ids []uuid.UUID }

func (a fakeAccounts) ListMembers(context.Context) ([]store.Member, error) {
	out := make([]store.Member, 0, len(a.ids))
	for _, id := range a.ids {
		out = append(out, store.Member{User: store.User{ID: id}})
	}
	return out, nil
}

// --- harness ---------------------------------------------------------------

type harness struct {
	w      *Watcher
	inbox  string
	audio  *audio.Store
	ingest *fakeIngest
	ledger *fakeLedger
	author uuid.UUID
	now    time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	base := t.TempDir()
	inbox := filepath.Join(base, "inbox")
	root := filepath.Join(base, "audio")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	author := uuid.New()
	if err := os.MkdirAll(filepath.Join(inbox, author.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	as, err := audio.New(root)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		inbox: inbox, audio: as,
		ingest: newFakeIngest(), ledger: newFakeLedger(),
		author: author,
		// Far enough ahead that anything written by the test is settled.
		now: time.Now().Add(time.Hour),
	}
	w, err := New(Options{
		Root: inbox, Audio: as, Ingest: h.ingest, Ledger: h.ledger,
		Accounts: fakeAccounts{ids: []uuid.UUID{author}},
		Settle:   10 * time.Second,
		Now:      func() time.Time { return h.now },
	})
	if err != nil {
		t.Fatal(err)
	}
	h.w = w
	return h
}

func (h *harness) drop(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(h.inbox, h.author.String(), name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func (h *harness) scan(t *testing.T) Result {
	t.Helper()
	res, err := h.w.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return res
}

func hashOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func (h *harness) storedPath(t *testing.T, content string) string {
	t.Helper()
	p, err := h.audio.Path(audio.Ref{AuthorID: h.author, ContentHash: hashOf(content)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// --- the Done-whens --------------------------------------------------------

// Done when #1: a memo synced from a phone appears as a row.
func TestAFileInTheInboxBecomesAMemo(t *testing.T) {
	h := newHarness(t)
	h.drop(t, "2026-08-26T01-02-03.opus", "a recording")

	res := h.scan(t)
	if res.Ingested != 1 || res.Failed != 0 {
		t.Fatalf("scan = %+v, want 1 ingested", res)
	}
	if h.ingest.count() != 1 {
		t.Fatalf("memos = %d, want 1", h.ingest.count())
	}

	got := h.ingest.calls[0]
	if got.AuthorID != h.author {
		t.Errorf("author = %s, want the inbox subdirectory's account %s", got.AuthorID, h.author)
	}
	if got.ContentHash != hashOf("a recording") {
		t.Errorf("hash = %s, want the hash of the bytes that arrived", got.ContentHash)
	}
	if got.ByteSize != int64(len("a recording")) {
		t.Errorf("byte size = %d", got.ByteSize)
	}
	if got.Source != store.SourceCopyparty {
		t.Errorf("source = %q", got.Source)
	}
	// The handle is relative to the inbox root, so relocating the root does not
	// turn every known file into a new sighting.
	want := filepath.Join(h.author.String(), "2026-08-26T01-02-03.opus")
	if got.SourceRef != want {
		t.Errorf("source_ref = %q, want %q", got.SourceRef, want)
	}
	if got.Retention != "" {
		t.Errorf("retention = %q, want empty — a rescan default must not outrank a person's choice", got.Retention)
	}
	if got.IdempotencyKey != "" {
		t.Errorf("idempotency key = %q, want empty — the watcher has none to mint", got.IdempotencyKey)
	}

	// The bytes are COPIED into the audio store...
	stored, err := os.ReadFile(h.storedPath(t, "a recording"))
	if err != nil {
		t.Fatalf("the audio was not copied into the store: %v", err)
	}
	if string(stored) != "a recording" {
		t.Errorf("stored = %q", stored)
	}
	// ...and the inbox is left exactly as it was. The watcher observes and
	// never consumes: moving a file out from under a two-way sync either loops
	// or propagates the delete back to the phone.
	if _, err := os.Stat(filepath.Join(h.inbox, h.author.String(), "2026-08-26T01-02-03.opus")); err != nil {
		t.Errorf("the inbox file was consumed: %v", err)
	}
}

// Done when #2, and the reason this ticket is not trivial: a hash over a
// half-written file is a DIFFERENT hash, so a partial read becomes a second
// memo rather than a broken one.
func TestAFileStillBeingWrittenIsNeverIngested(t *testing.T) {
	t.Run("the settle window holds it back", func(t *testing.T) {
		h := newHarness(t)
		p := h.drop(t, "growing.opus", "first half")
		// Touched a moment ago: still inside the settle window.
		h.now = time.Now()
		if err := os.Chtimes(p, h.now, h.now); err != nil {
			t.Fatal(err)
		}

		res := h.scan(t)
		if res.Ingested != 0 || res.Unsettled != 1 {
			t.Fatalf("scan = %+v, want it held as unsettled", res)
		}
		if h.ingest.count() != 0 {
			t.Fatal("a file touched a moment ago was ingested")
		}

		// Once it settles, it lands — and as ONE memo, of the whole content.
		if err := os.WriteFile(p, []byte("first half and second half"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.now = time.Now().Add(time.Hour)
		res = h.scan(t)
		if res.Ingested != 1 {
			t.Fatalf("scan = %+v, want 1 ingested once settled", res)
		}
		if h.ingest.count() != 1 {
			t.Fatalf("memos = %d, want exactly 1 — a partial read would make 2", h.ingest.count())
		}
		if h.ingest.calls[0].ContentHash != hashOf("first half and second half") {
			t.Error("the memo was hashed over something other than the complete file")
		}
	})

	// The guard that is a guarantee rather than a hope: the file is re-stated
	// through the same handle after the copy, and a change means the bytes just
	// hashed were a snapshot of something still being written.
	t.Run("a file that changes DURING the copy is discarded", func(t *testing.T) {
		h := newHarness(t)
		p := h.drop(t, "racing.opus", "original content")

		// Stand in for the walk's stat reporting a size the file no longer has
		// — which is exactly what a write landing between the walk and the open
		// looks like from here.
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("content that grew after the stat"), 0o644); err != nil {
			t.Fatal(err)
		}

		out, err := h.w.ingestFile(context.Background(), p, h.author, info)
		if err != nil {
			t.Fatalf("ingestFile: %v", err)
		}
		if !out.changedWhileReading {
			t.Fatal("a file that changed under the reader was ingested anyway")
		}
		if h.ingest.count() != 0 {
			t.Fatal("a memo was recorded for a file that moved under the reader")
		}
		// Nothing was left in the audio store, and nothing was recorded, so the
		// next scan simply tries again.
		entries, _ := os.ReadDir(h.audio.Root())
		if len(entries) != 0 {
			t.Errorf("the audio store holds %d entries after a discarded read", len(entries))
		}
	})

	t.Run("names that say in progress are skipped", func(t *testing.T) {
		h := newHarness(t)
		for _, name := range []string{
			"upload.opus.PARTIAL", "upload.opus.part", "scratch.tmp",
			".hidden.opus", "~lock.opus", "chunk.filepart",
		} {
			h.drop(t, name, "content of "+name)
		}
		res := h.scan(t)
		if res.Ingested != 0 {
			t.Fatalf("scan = %+v, want nothing ingested", res)
		}
		if h.ingest.count() != 0 {
			t.Fatalf("%d memos from in-progress names", h.ingest.count())
		}
	})

	t.Run("an empty file is not a memo", func(t *testing.T) {
		h := newHarness(t)
		h.drop(t, "created-not-written.opus", "")
		res := h.scan(t)
		if res.Ingested != 0 || h.ingest.count() != 0 {
			t.Fatalf("an empty file was ingested: %+v", res)
		}
	})
}

// Done when #3: the watcher recovers everything it missed after being down.
// There is no recovery path — a file that arrived while the service was down is
// simply one the ledger has not seen, and its mtime is well outside the settle
// window. This asserts that one code path does the job.
func TestFilesThatArrivedWhileDownAreIngestedOnTheNextScan(t *testing.T) {
	h := newHarness(t)
	for _, n := range []string{"one.opus", "two.opus", "three.opus"} {
		h.drop(t, n, "recording "+n)
	}
	// An hour ago, i.e. while nothing was watching.
	past := time.Now().Add(-time.Hour)
	for _, n := range []string{"one.opus", "two.opus", "three.opus"} {
		p := filepath.Join(h.inbox, h.author.String(), n)
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}

	res := h.scan(t)
	if res.Ingested != 3 {
		t.Fatalf("scan = %+v, want all 3 recovered", res)
	}
	if h.ingest.count() != 3 {
		t.Fatalf("memos = %d, want 3", h.ingest.count())
	}
}

// The ledger's whole job: a rescan is not a re-delivery. Without it the watcher
// re-hashes the corpus every poll and writes an arrival row per sighting.
func TestARescanIsNotARedelivery(t *testing.T) {
	h := newHarness(t)
	h.drop(t, "kept.opus", "a recording")

	first := h.scan(t)
	if first.Ingested != 1 {
		t.Fatalf("first scan = %+v", first)
	}

	for i := 0; i < 5; i++ {
		res := h.scan(t)
		if res.Ingested != 0 || res.Skipped != 1 {
			t.Fatalf("rescan %d = %+v, want it skipped by the ledger", i, res)
		}
	}
	if n := len(h.ingest.calls); n != 1 {
		t.Errorf("the store was asked to ingest %d times across six scans, want 1", n)
	}
}

// And when the ledger is unavailable, correctness does not depend on it: the
// re-read collapses on the content hash instead. That is what makes the ledger
// honestly tier 1 — losing it costs time, not data.
func TestTheLedgerIsAPerformanceMechanismNotACorrectnessOne(t *testing.T) {
	h := newHarness(t)
	h.ledger.failMark = true
	h.drop(t, "kept.opus", "a recording")

	if res := h.scan(t); res.Ingested != 1 {
		t.Fatalf("first scan = %+v", res)
	}
	res := h.scan(t)
	if res.Skipped != 0 {
		t.Fatal("the ledger recorded something despite failing")
	}
	if res.Collapsed != 1 {
		t.Errorf("scan = %+v, want the re-read to collapse", res)
	}
	if h.ingest.count() != 1 {
		t.Fatalf("memos = %d, want 1 — a lost ledger must not duplicate a memo", h.ingest.count())
	}
}

// A file carries no identity, and tier2.memos.author_id is NOT NULL, so the
// directory supplies one. Which is exactly why an unrecognised directory must
// ingest nothing rather than inventing an account.
func TestOnlyDirectoriesThatNameAnAccountAreRead(t *testing.T) {
	h := newHarness(t)
	stranger := filepath.Join(h.inbox, uuid.New().String())
	if err := os.MkdirAll(stranger, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stranger, "planted.opus"), []byte("not yours"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.inbox, "not-a-uuid"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.inbox, "not-a-uuid", "planted.opus"), []byte("nor this"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file loose at the inbox root belongs to nobody either.
	if err := os.WriteFile(filepath.Join(h.inbox, "loose.opus"), []byte("loose"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.drop(t, "mine.opus", "a recording")

	res := h.scan(t)
	if res.Ingested != 1 {
		t.Fatalf("scan = %+v, want only the account's own file", res)
	}
	if h.ingest.calls[0].AuthorID != h.author {
		t.Errorf("author = %s, want %s", h.ingest.calls[0].AuthorID, h.author)
	}
}

// Nested directories are the ordinary shape of a phone's sync tree.
func TestFilesInSubdirectoriesOfAnAccountAreIngested(t *testing.T) {
	h := newHarness(t)
	h.drop(t, filepath.Join("2026", "08", "note.opus"), "nested recording")

	res := h.scan(t)
	if res.Ingested != 1 {
		t.Fatalf("scan = %+v, want the nested file", res)
	}
	want := filepath.Join(h.author.String(), "2026", "08", "note.opus")
	if got := h.ingest.calls[0].SourceRef; got != want {
		t.Errorf("source_ref = %q, want %q", got, want)
	}
}

// One recording delivered at two paths is one memo and two sightings — the
// content hash is the identity, not the path.
func TestTheSameBytesAtTwoPathsAreOneMemo(t *testing.T) {
	h := newHarness(t)
	h.drop(t, "note.opus", "identical bytes")
	h.drop(t, filepath.Join("backup", "note.opus"), "identical bytes")

	res := h.scan(t)
	if res.Ingested != 2 {
		t.Fatalf("scan = %+v, want both files read", res)
	}
	if h.ingest.count() != 1 {
		t.Fatalf("memos = %d, want 1", h.ingest.count())
	}
	if res.Collapsed != 1 {
		t.Errorf("collapsed = %d, want 1 — the second path is a second sighting", res.Collapsed)
	}
}

func TestScanOfAMissingInboxIsNotAnError(t *testing.T) {
	h := newHarness(t)
	if err := os.RemoveAll(h.inbox); err != nil {
		t.Fatal(err)
	}
	res, err := h.w.Scan(context.Background())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if res.Considered != 0 {
		t.Errorf("scan = %+v", res)
	}
}

func TestNewValidatesItsOptions(t *testing.T) {
	as, err := audio.New("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	base := Options{
		Root: "/inbox", Audio: as, Ingest: newFakeIngest(),
		Ledger: newFakeLedger(), Accounts: fakeAccounts{},
	}
	if _, err := New(base); err != nil {
		t.Fatalf("New: %v", err)
	}

	relative := base
	relative.Root = "inbox"
	if _, err := New(relative); err == nil {
		t.Error("New accepted a relative inbox root")
	}
	noAudio := base
	noAudio.Audio = nil
	if _, err := New(noAudio); err == nil {
		t.Error("New accepted a nil audio store")
	}
}
