package pump

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// ============================================================================
// fakeOllama — one /api/generate whose answer is chosen per memo.
// ============================================================================
//
// Every transcript a test writes carries `MEMO:<name>`, and the fake finds it in
// the prompt to decide how to answer. That is the only way to give each memo
// its own behaviour through a router that sends nothing but the prompt.

type behaviour string

const (
	answerValid      behaviour = "valid"       // a TICKET into a catalogued project
	answerNeedsInput behaviour = "needs_input" // a TICKET into a project the catalogue lacks
	answerGarbage    behaviour = "garbage"     // fails stage 1 on every attempt
	answerCtxFull    behaviour = "ctx_full"    // prompt_eval_count at num_ctx
	answerLength     behaviour = "length"      // done_reason: length
	answerEmpty      behaviour = "empty"       // 200 with nothing in it
	answer500        behaviour = "500"
	answerHang       behaviour = "hang" // until the client gives up
	answerHold       behaviour = "hold" // until released or the client gives up
)

var memoName = regexp.MustCompile(`MEMO:([A-Za-z0-9_-]+)`)

type fakeOllama struct {
	t    *testing.T
	addr string

	mu       sync.Mutex
	srv      *httptest.Server
	byName   map[string]behaviour
	fallback behaviour
	calls    []string // memo names, in the order generate was asked

	// hold: started fires when a held request arrives, cancelled when its
	// client went away, and release lets it answer.
	started   chan string
	cancelled chan time.Time
	release   chan struct{}
}

func newFakeOllama(t *testing.T) *fakeOllama {
	t.Helper()
	f := &fakeOllama{
		t: t, byName: map[string]behaviour{}, fallback: answerValid,
		started: make(chan string, 16), cancelled: make(chan time.Time, 16), release: make(chan struct{}),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	f.addr = l.Addr().String()
	f.serve(l)
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.srv != nil {
			f.srv.CloseClientConnections()
			f.srv.Close()
		}
	})
	return f
}

func (f *fakeOllama) URL() string { return "http://" + f.addr }

func (f *fakeOllama) serve(l net.Listener) {
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.handle))
	_ = srv.Listener.Close()
	srv.Listener = l
	srv.Start()
	f.mu.Lock()
	f.srv = srv
	f.mu.Unlock()
}

// down closes the port, so the next dial is refused.
func (f *fakeOllama) down() {
	f.mu.Lock()
	srv := f.srv
	f.srv = nil
	f.mu.Unlock()
	srv.CloseClientConnections()
	srv.Close()
}

// up listens on the same port again.
func (f *fakeOllama) up() {
	l, err := net.Listen("tcp", f.addr)
	if err != nil {
		f.t.Fatalf("re-listen on %s: %v", f.addr, err)
	}
	f.serve(l)
}

func (f *fakeOllama) set(name string, b behaviour) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byName[name] = b
}

func (f *fakeOllama) setAll(b behaviour) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byName = map[string]behaviour{}
	f.fallback = b
}

func (f *fakeOllama) callLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeOllama) handle(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/tags" {
		_, _ = w.Write([]byte(`{"models":[{"name":"m","digest":"sha256:feed"},{"name":"m2","digest":"sha256:beef"}]}`))
		return
	}
	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := ""
	if m := memoName.FindStringSubmatch(req.Prompt); m != nil {
		name = m[1]
	}
	f.mu.Lock()
	f.calls = append(f.calls, name)
	b, ok := f.byName[name]
	if !ok {
		b = f.fallback
	}
	f.mu.Unlock()

	reply := func(v map[string]any) { _ = json.NewEncoder(w).Encode(v) }
	ok200 := func(response string) map[string]any {
		return map[string]any{"response": response, "done_reason": "stop", "prompt_eval_count": 100, "eval_count": 50}
	}
	switch b {
	case answerValid:
		reply(ok200(proposalJSON("TICKET", "CHRN")))
	case answerNeedsInput:
		reply(ok200(proposalJSON("TICKET", "NOSUCH")))
	case answerGarbage:
		reply(ok200(`{"destination":"NONSENSE"}`))
	case answerCtxFull:
		reply(map[string]any{"response": proposalJSON("NOTE", ""), "done_reason": "stop", "prompt_eval_count": 16384, "eval_count": 10})
	case answerLength:
		reply(map[string]any{"response": `{"reason":"it goes on and`, "done_reason": "length", "prompt_eval_count": 100, "eval_count": 1536})
	case answerEmpty:
		reply(map[string]any{"response": "", "done_reason": "stop", "prompt_eval_count": 100, "eval_count": 0})
	case answer500:
		http.Error(w, "model runner crashed", http.StatusInternalServerError)
	case answerHang:
		<-r.Context().Done()
	case answerHold:
		f.started <- name
		select {
		case <-r.Context().Done():
			f.cancelled <- time.Now()
		case <-f.release:
			reply(ok200(proposalJSON("TICKET", "CHRN")))
		}
	}
}

func proposalJSON(dest, project string) string {
	p := map[string]any{
		"reason": "because it says so", "destination": dest, "confidence": 0.85,
		"title": "Do the thing", "nearest_page": nil, "project_key": project,
		"ticket_type": "task", "description": "## Summary\nx", "body": "x", "opening_post": "x",
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// ============================================================================
// stubCatalogue
// ============================================================================

type stubCatalogue struct {
	mu    sync.Mutex
	snap  *catalogue.Snapshot
	err   error
	calls int
}

func (c *stubCatalogue) Fetch(ctx context.Context) (*catalogue.Snapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.snap, c.err
}

func (c *stubCatalogue) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func testCatalogue(t *testing.T) *catalogue.Snapshot {
	t.Helper()
	s, err := catalogue.Parse([]byte("version: 1\nprojects:\n  - key: CHRN\n    name: Chronicle\n    description: voice notes\n"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// ============================================================================
// logs
// ============================================================================

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// ============================================================================
// harness — chronicle_test, with the pump's store connected AS chronicle_tier1
// ============================================================================
//
// THE ROLE IS THE PROOF. Fixtures are tier-2 writes and go through the owner's
// Store; everything the pump does goes through a Tier1Store on a pool that is
// chronicle_tier1, so a statement outside the grants fails these tests with
// `permission denied` rather than passing on the owner's privileges.

type harness struct {
	t      *testing.T
	ctx    context.Context
	st     *store.Store
	tier1  *store.Tier1Store
	owner  store.User
	ollama *fakeOllama
	cat    *stubCatalogue
	logs   *syncBuffer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	tier1DSN := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_TIER1_DATABASE_URL"))
	if dsn == "" || tier1DSN == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL and CHRONICLE_TEST_TIER1_DATABASE_URL are both needed")
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
	owner, err := st.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	t1pool, err := store.Connect(ctx, tier1DSN)
	if err != nil {
		t.Fatalf("connect as chronicle_tier1: %v", err)
	}
	t.Cleanup(t1pool.Close)
	tier1 := store.NewTier1(t1pool)
	// The positive control: without it every assertion would also pass on a
	// connection that was not the role it claims.
	if role, err := tier1.Role(ctx); err != nil || role != "chronicle_tier1" {
		t.Fatalf("tier-1 pool connects as %q (%v), want chronicle_tier1", role, err)
	}

	return &harness{
		t: t, ctx: ctx, st: st, tier1: tier1, owner: owner,
		ollama: newFakeOllama(t),
		cat:    &stubCatalogue{snap: testCatalogue(t)},
		logs:   &syncBuffer{},
	}
}

// pump builds a pump over the harness, with the waits shortened.
func (h *harness) pump(model string, gate bool) *Pump {
	h.t.Helper()
	p, err := New(Options{
		Store: h.tier1, Catalogue: h.cat,
		Logger:    slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		OllamaURL: h.ollama.URL(), Model: model, MaxAttempts: 3,
		TranscriptionGate: gate, Timeout: 2 * time.Second,
	})
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	p.poll = 20 * time.Millisecond
	p.interval = 20 * time.Millisecond
	p.outageBase, p.outageWait, p.outageMax = 10*time.Millisecond, 10*time.Millisecond, 40*time.Millisecond
	return p
}

// ingest makes a captured memo whose transcript, when it has one, names it.
func (h *harness) ingest(name string, withTranscript bool) uuid.UUID {
	h.t.Helper()
	sum := sha256.Sum256([]byte(name))
	hash := hex.EncodeToString(sum[:])
	res, err := h.st.IngestMemo(h.ctx, store.Arrival{
		AuthorID: h.owner.ID, ContentHash: hash, ByteSize: 1024,
		Source: store.SourceUpload, SourceRef: "test/" + hash[:8],
	})
	if err != nil {
		h.t.Fatalf("IngestMemo: %v", err)
	}
	if withTranscript {
		if _, err := h.st.RecordTranscript(h.ctx, store.TranscriptInput{
			MemoID: res.Memo.ID, Text: "MEMO:" + name + " a memo about " + name,
			Model: "whisper.cpp/small.en", Backend: "vulkan",
		}); err != nil {
			h.t.Fatalf("RecordTranscript: %v", err)
		}
	}
	return res.Memo.ID
}

func (h *harness) advance(id uuid.UUID, steps ...string) {
	h.t.Helper()
	for i := 1; i < len(steps); i++ {
		if _, err := h.st.AdvanceMemoState(h.ctx, id, steps[i-1], steps[i], ""); err != nil {
			h.t.Fatalf("advance %s -> %s: %v", steps[i-1], steps[i], err)
		}
	}
}

// memo is a `transcribed` memo with a durable transcript naming it.
func (h *harness) memo(name string) uuid.UUID {
	h.t.Helper()
	id := h.ingest(name, true)
	h.advance(id, store.StateCaptured, store.StateQueued, store.StateTranscribing, store.StateTranscribed)
	return id
}

// rows reads every proposal under one proposer, keyed by memo.
func (h *harness) rows(proposer string, ids ...uuid.UUID) map[uuid.UUID]store.Proposal {
	h.t.Helper()
	got, err := h.tier1.ProposalsForMemos(h.ctx, ids, proposer)
	if err != nil {
		h.t.Fatalf("ProposalsForMemos: %v", err)
	}
	return got
}

func (h *harness) invalidRows() int {
	h.t.Helper()
	var n int
	if err := h.st.Pool().QueryRow(h.ctx, `SELECT count(*) FROM tier1.memo_proposals WHERE status = 'invalid'`).Scan(&n); err != nil {
		h.t.Fatal(err)
	}
	return n
}

func wantStatus(t *testing.T, rows map[uuid.UUID]store.Proposal, id uuid.UUID, want scribe.Status) {
	t.Helper()
	p, ok := rows[id]
	if !ok {
		t.Fatalf("memo %s has no row, want %s", id, want)
	}
	if p.Status != want {
		t.Fatalf("memo %s is %s (%s), want %s", id, p.Status, p.Error, want)
	}
}

func fmtCalls(c []string) string { return fmt.Sprintf("%q", c) }
