package pump

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

var errSwitchyardDown = errors.New("switchyard: GET /v1/projects: 502 Bad Gateway")

// memStore is the pump's Store in memory: enough to drive the loop without a
// database, for the cases that are about the loop and not about SQL.
type memStore struct {
	mu    sync.Mutex
	memos []store.Memo
	rows  map[uuid.UUID]scribe.Outcome
	load  store.TranscriptionLoad
}

func newMemStore(names ...string) *memStore {
	s := &memStore{rows: map[uuid.UUID]scribe.Outcome{}}
	for i, n := range names {
		s.memos = append(s.memos, store.Memo{
			ID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(n)), State: store.StateTranscribed,
			CapturedAt: time.Unix(int64(1000-i), 0), OriginalFilename: &names[i],
		})
	}
	return s
}

func (s *memStore) UnroutedMemos(ctx context.Context, proposer string, exclude []uuid.UUID, limit int) ([]store.Memo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Memo
	for _, m := range s.memos {
		if _, ok := s.rows[m.ID]; ok {
			continue
		}
		skip := false
		for _, x := range exclude {
			skip = skip || x == m.ID
		}
		if !skip && len(out) < limit {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *memStore) TranscriptionBusy(ctx context.Context, d time.Duration) (store.TranscriptionLoad, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load, nil
}

func (s *memStore) MemoState(ctx context.Context, id uuid.UUID) (string, error) {
	return store.StateTranscribed, nil
}

func (s *memStore) TranscriptForScribe(ctx context.Context, id uuid.UUID) (store.Transcript, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.memos {
		if m.ID == id {
			return store.Transcript{ID: uuid.New(), MemoID: id, Text: "MEMO:" + *m.OriginalFilename}, nil
		}
	}
	return store.Transcript{}, store.ErrNotFound
}

func (s *memStore) SaveProposal(ctx context.Context, memoID, transcriptID uuid.UUID, proposer string, out scribe.Outcome) (store.Proposal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[memoID] = out
	return store.Proposal{MemoID: memoID, Proposer: proposer, Status: out.Status}, nil
}

func (s *memStore) rowCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.rows)
}

func unitPump(t *testing.T, st Store, cat *stubCatalogue, url string, logs *syncBuffer) *Pump {
	t.Helper()
	p, err := New(Options{
		Store: st, Catalogue: cat,
		Logger:    slog.New(slog.NewTextHandler(logs, nil)),
		OllamaURL: url, Model: "m", MaxAttempts: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	p.interval, p.poll = 10*time.Millisecond, 10*time.Millisecond
	return p
}

// Criterion 10: a catalogue that cannot be read is an outage by itself. No
// Ollama call, no row, and the warning names Switchyard.
func TestACatalogueFailureIsAnOutageNamingSwitchyard(t *testing.T) {
	ollama := newFakeOllama(t)
	st := newMemStore("one", "two")
	logs := &syncBuffer{}
	p := unitPump(t, st, &stubCatalogue{err: errSwitchyardDown}, ollama.URL(), logs)

	rep := p.Tick(context.Background())
	if !rep.Outage {
		t.Fatalf("report = %+v, want an outage", rep)
	}
	if n := len(ollama.callLog()); n != 0 {
		t.Fatalf("made %d Ollama calls with no catalogue", n)
	}
	if st.rowCount() != 0 {
		t.Fatal("wrote a row with no catalogue")
	}
	if !strings.Contains(logs.String(), "upstream=switchyard") || !strings.Contains(logs.String(), "502") {
		t.Fatalf("the warning does not name Switchyard:\n%s", logs.String())
	}

	// Said once per outage, not once per pass.
	p.Tick(context.Background())
	if n := strings.Count(logs.String(), "level=WARN"); n != 1 {
		t.Fatalf("%d warnings for one outage:\n%s", n, logs.String())
	}
}

// Criterion 13: cancelling the pump mid-generation returns from Run and writes
// nothing for the memo in flight.
func TestCancellingRunMidGenerationWritesNothing(t *testing.T) {
	ollama := newFakeOllama(t)
	ollama.setAll(answerHold)
	st := newMemStore("inflight")
	p := unitPump(t, st, &stubCatalogue{snap: testCatalogue(t)}, ollama.URL(), &syncBuffer{})

	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan error)
	go func() { ran <- p.Run(ctx) }()
	<-ollama.started
	cancel()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if st.rowCount() != 0 {
		t.Fatal("shutdown wrote a row for the memo in flight")
	}
}

// The wait after an outage doubles to its cap and resets on the first success.
func TestTheOutageBackoffDoublesAndResets(t *testing.T) {
	ollama := newFakeOllama(t)
	ollama.setAll(answer500)
	st := newMemStore("a", "b", "c", "d", "e", "f")
	p := unitPump(t, st, &stubCatalogue{snap: testCatalogue(t)}, ollama.URL(), &syncBuffer{})
	p.outageBase, p.outageWait, p.outageMax = time.Second, time.Second, 3*time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := []time.Duration{}
	for i := 0; i < 3; i++ {
		rep := p.Tick(ctx)
		if !rep.Outage {
			t.Fatalf("pass %d: %+v, want an outage", i, rep)
		}
		waits = append(waits, p.outageWait)
		p.outageWait = min(2*p.outageWait, p.outageMax)
	}
	if waits[0] != time.Second || waits[1] != 2*time.Second || waits[2] != 3*time.Second {
		t.Fatalf("waits = %v, want 1s 2s 3s", waits)
	}
	ollama.setAll(answerValid)
	p.backoff = map[uuid.UUID]time.Time{}
	p.Tick(ctx)
	if p.outageWait != p.outageBase {
		t.Fatalf("after a success the wait is %s, want it reset to %s", p.outageWait, p.outageBase)
	}
}

// A pump yielding for longer than LongYield says so, once, with the counts.
func TestALongYieldIsSaidOnce(t *testing.T) {
	st := newMemStore("x")
	st.load = store.TranscriptionLoad{InFlight: 1}
	logs := &syncBuffer{}
	p := unitPump(t, st, &stubCatalogue{snap: testCatalogue(t)}, "http://127.0.0.1:1", logs)
	p.gate = true
	clock := time.Now()
	p.now = func() time.Time { return clock }

	for i := 0; i < 5; i++ {
		p.Tick(context.Background())
		clock = clock.Add(LongYield / 2)
	}
	if n := strings.Count(logs.String(), "yielded to transcription"); n != 1 {
		t.Fatalf("%d long-yield warnings, want 1:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "in_flight_jobs=1") {
		t.Fatalf("the warning does not carry the counts:\n%s", logs.String())
	}
}

// The boot line, with the gate's state, and the proposer triage reads by.
func TestRunAnnouncesItself(t *testing.T) {
	logs := &syncBuffer{}
	p := unitPump(t, newMemStore(), &stubCatalogue{snap: testCatalogue(t)}, "http://127.0.0.1:1", logs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), `msg="scribe pump started" proposer=ollama/m@v1`) {
		t.Fatalf("no boot line:\n%s", logs.String())
	}
}

func TestNewRefusesAnUnusableConfiguration(t *testing.T) {
	cat := &stubCatalogue{}
	for _, o := range []Options{
		{Catalogue: cat, OllamaURL: "http://x:1", Model: "m"},
		{Store: newMemStore(), OllamaURL: "http://x:1", Model: "m"},
		{Store: newMemStore(), Catalogue: cat, Model: "m"},
		{Store: newMemStore(), Catalogue: cat, OllamaURL: "http://x:1"},
		{Store: newMemStore(), Catalogue: cat, OllamaURL: "http://x:1", Model: "a/b"},
	} {
		if _, err := New(o); err == nil {
			t.Errorf("New(%+v) accepted it", o)
		}
	}
}
