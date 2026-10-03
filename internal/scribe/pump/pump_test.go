package pump

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Criterion 2: a pass over N unrouted memos writes N rows with the status each
// answer implies; a second pass asks nobody anything; and a memo with a
// transcript and no row — the state a crash between the two leaves — is routed
// by the next pass.
func TestAPassRoutesEveryUnroutedMemoAndTheNextAsksNobody(t *testing.T) {
	h := newHarness(t)
	note, ticket, garbage := h.memo("note"), h.memo("ticket"), h.memo("garbage")
	h.ollama.set("ticket", answerNeedsInput)
	h.ollama.set("garbage", answerGarbage)
	p := h.pump("m", true)

	rep := p.Tick(h.ctx)
	rows := h.rows(p.Proposer(), note, ticket, garbage)
	wantStatus(t, rows, note, scribe.StatusValid)
	wantStatus(t, rows, ticket, scribe.StatusNeedsInput)
	wantStatus(t, rows, garbage, scribe.StatusInvalid)
	if rep.Failed != 0 || rep.Outage {
		t.Fatalf("report = %+v, want no failures", rep)
	}

	calls, fetches := len(h.ollama.callLog()), h.cat.count()
	p.Tick(h.ctx)
	if got := len(h.ollama.callLog()); got != calls {
		t.Fatalf("the second pass made %d Ollama calls, want none", got-calls)
	}
	if got := h.cat.count(); got != fetches {
		t.Fatalf("the second pass fetched the catalogue %d times, want none", got-fetches)
	}

	// The crash: a transcript and no row.
	if _, err := h.st.Pool().Exec(h.ctx, `DELETE FROM tier1.memo_proposals WHERE memo_id = $1`, note); err != nil {
		t.Fatal(err)
	}
	p.Tick(h.ctx)
	wantStatus(t, h.rows(p.Proposer(), note), note, scribe.StatusValid)
	if log := h.ollama.callLog(); log[len(log)-1] != "note" {
		t.Fatalf("calls = %s, want the healed memo last", fmtCalls(log))
	}
}

// Criterion 3 (⚖1): a new proposer re-routes every memo still `transcribed`,
// leaves the old rows exactly as they were, and never asks about a decided memo.
func TestANewProposerReRoutesOnlyUndecidedMemos(t *testing.T) {
	h := newHarness(t)
	open1, open2, triaged, discarded := h.memo("open1"), h.memo("open2"), h.memo("triaged"), h.memo("discarded")

	old := h.pump("m", false)
	old.Tick(h.ctx)
	if old.Proposer() != "ollama/m@v1" {
		t.Fatalf("old proposer = %q", old.Proposer())
	}
	before := h.rows(old.Proposer(), open1, open2, triaged, discarded)
	if len(before) != 4 {
		t.Fatalf("old proposer has %d rows, want 4", len(before))
	}
	h.advance(triaged, store.StateTranscribed, store.StateTriaged)
	h.advance(discarded, store.StateTranscribed, store.StateDiscarded)

	calls := len(h.ollama.callLog())
	next := h.pump("m2", false)
	next.Tick(h.ctx)

	asked := h.ollama.callLog()[calls:]
	if strings.Join(asked, ",") != "open2,open1" {
		t.Fatalf("the new proposer asked about %s, want open2,open1 (newest first, nothing decided)", fmtCalls(asked))
	}
	after := h.rows(next.Proposer(), open1, open2, triaged, discarded)
	if len(after) != 2 || after[open1].Status != scribe.StatusValid || after[open2].Status != scribe.StatusValid {
		t.Fatalf("rows under %s = %+v, want open1 and open2 only", next.Proposer(), after)
	}
	again := h.rows(old.Proposer(), open1, open2, triaged, discarded)
	for id, b := range before {
		a := again[id]
		if a.Generation != b.Generation || a.RawOutput != b.RawOutput || !a.UpdatedAt.Equal(b.UpdatedAt) || a.Status != b.Status {
			t.Fatalf("the old row for %s moved: %+v -> %+v", id, b, a)
		}
	}
}

// Criterion 4 (⚖3): an outage writes nothing, ends the pass after two failures
// in a row, and the rows land once Ollama is back. No row is ever `invalid`.
func TestAnOutageWritesNothingAndRecovers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		break_ func(h *harness, p *Pump)
		mend   func(h *harness)
	}{
		{"refused", func(h *harness, p *Pump) { h.ollama.down() }, func(h *harness) { h.ollama.up() }},
		{"500", func(h *harness, p *Pump) { h.ollama.setAll(answer500) }, func(h *harness) { h.ollama.setAll(answerValid) }},
		{"timeout", func(h *harness, p *Pump) { p.timeout = 150 * time.Millisecond; h.ollama.setAll(answerHang) },
			func(h *harness) { h.ollama.setAll(answerValid) }},
		{"empty 200", func(h *harness, p *Pump) { h.ollama.setAll(answerEmpty) }, func(h *harness) { h.ollama.setAll(answerValid) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			var ids []uuid.UUID
			for _, n := range []string{"a", "b", "c", "d", "e"} {
				ids = append(ids, h.memo(n))
			}
			p := h.pump("m", false)
			clock := time.Now()
			p.now = func() time.Time { return clock }
			tc.break_(h, p)

			calls := len(h.ollama.callLog())
			rep := p.Tick(h.ctx)
			if !rep.Outage || rep.Failed != 2 {
				t.Fatalf("report = %+v, want an outage after two failures", rep)
			}
			if tc.name != "refused" {
				if got := len(h.ollama.callLog()) - calls; got != 2 {
					t.Fatalf("the pass made %d calls, want it to end after 2", got)
				}
			}
			if n := len(h.rows(p.Proposer(), ids...)); n != 0 {
				t.Fatalf("an outage wrote %d rows", n)
			}
			if !strings.Contains(h.logs.String(), "upstream=ollama") {
				t.Fatalf("no outage warning naming Ollama:\n%s", h.logs.String())
			}

			tc.mend(h)
			p.Tick(h.ctx)
			if n := len(h.rows(p.Proposer(), ids...)); n != 3 {
				t.Fatalf("after recovery %d rows, want the 3 not in backoff", n)
			}
			clock = clock.Add(MemoBackoff + time.Second)
			p.Tick(h.ctx)
			rows := h.rows(p.Proposer(), ids...)
			for _, id := range ids {
				wantStatus(t, rows, id, scribe.StatusValid)
			}
			if n := h.invalidRows(); n != 0 {
				t.Fatalf("%d invalid rows; an outage must never be written down as one", n)
			}
			if !strings.Contains(h.logs.String(), "routing again") {
				t.Errorf("no recovery line:\n%s", h.logs.String())
			}
		})
	}
}

// Criterion 5 (⚖3): the two truncations and a stage-1 failure are final for
// this proposer — written `invalid`, never retried, and they stop nobody else.
func TestRepeatableFailuresAreInvalidAndDoNotStopThePass(t *testing.T) {
	h := newHarness(t)
	good1 := h.memo("good1")
	full := h.memo("full")
	cut := h.memo("cut")
	garbage := h.memo("garbage")
	good2 := h.memo("good2")
	h.ollama.set("full", answerCtxFull)
	h.ollama.set("cut", answerLength)
	h.ollama.set("garbage", answerGarbage)
	p := h.pump("m", false)

	rep := p.Tick(h.ctx)
	if rep.Failed != 0 || rep.Outage {
		t.Fatalf("report = %+v; a truncation is not an outage", rep)
	}
	rows := h.rows(p.Proposer(), good1, full, cut, garbage, good2)
	wantStatus(t, rows, good1, scribe.StatusValid)
	wantStatus(t, rows, good2, scribe.StatusValid)
	wantStatus(t, rows, full, scribe.StatusInvalid)
	wantStatus(t, rows, cut, scribe.StatusInvalid)
	wantStatus(t, rows, garbage, scribe.StatusInvalid)
	if !strings.Contains(rows[full].Error, "context window") {
		t.Errorf("num_ctx row error = %q", rows[full].Error)
	}
	if !strings.Contains(rows[cut].Error, "num_predict") {
		t.Errorf("num_predict row error = %q", rows[cut].Error)
	}
	if rows[garbage].LastAttemptRaw == "" {
		t.Errorf("stage-1 row has no last_attempt_raw")
	}

	calls := len(h.ollama.callLog())
	p.Tick(h.ctx)
	if got := len(h.ollama.callLog()); got != calls {
		t.Fatalf("the next pass asked %d more times; an invalid row is final under this proposer", got-calls)
	}
}

// Criterion 7: failing memos at the head of the order do not starve the one
// behind them; and only a real failure buys a backoff.
func TestFailingMemosAtTheHeadDoNotStarveTheRest(t *testing.T) {
	h := newHarness(t)
	oldest := h.memo("oldest")
	f1, f2, f3 := h.memo("f1"), h.memo("f2"), h.memo("f3")
	for _, n := range []string{"f1", "f2", "f3"} {
		h.ollama.set(n, answer500)
	}
	p := h.pump("m", false)

	p.Tick(h.ctx) // f3, f2 fail: an outage, and both set aside
	p.Tick(h.ctx) // f1 fails, oldest succeeds
	rows := h.rows(p.Proposer(), oldest, f1, f2, f3)
	wantStatus(t, rows, oldest, scribe.StatusValid)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want only the oldest", rows)
	}
	for _, id := range []uuid.UUID{f1, f2, f3} {
		if _, ok := p.backoff[id]; !ok {
			t.Errorf("failing memo %s is not in backoff", id)
		}
	}
}

func TestOnlyATimeoutBuysABackoffAndNotAPreemptionOrShutdown(t *testing.T) {
	h := newHarness(t)

	// Timeout: a failure.
	slow := h.memo("slow")
	h.ollama.set("slow", answerHang)
	p := h.pump("m", true)
	p.timeout = 150 * time.Millisecond
	if rep := p.Tick(h.ctx); rep.Failed != 1 {
		t.Fatalf("report = %+v, want one failure", rep)
	}
	if _, ok := p.backoff[slow]; !ok {
		t.Fatal("a request that hit the client timeout bought no backoff")
	}

	// Preemption: not a failure.
	held := h.memo("held")
	h.ollama.set("held", answerHold)
	p = h.pump("m", true)
	p.backoff[slow] = time.Now().Add(time.Hour)
	done := make(chan Report)
	go func() { done <- p.Tick(h.ctx) }()
	<-h.ollama.started
	busy := h.ingest("arrives", false)
	rep := <-done
	if rep.Preempted != 1 || rep.Failed != 0 {
		t.Fatalf("report = %+v, want one preemption and no failure", rep)
	}
	if _, ok := p.backoff[held]; ok {
		t.Fatal("a preempted request bought a backoff")
	}
	<-h.ollama.cancelled
	h.advance(busy, store.StateCaptured, store.StateQueued)

	// Shutdown: not a failure, and Run returns without a row.
	ctx, cancel := context.WithCancel(h.ctx)
	ran := make(chan error)
	go func() { ran <- p.Run(ctx) }()
	<-h.ollama.started
	cancel()
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled mid-generation")
	}
	if _, ok := p.backoff[held]; ok {
		t.Fatal("a request cancelled by shutdown bought a backoff")
	}
	if n := len(h.rows(p.Proposer(), held)); n != 0 {
		t.Fatalf("shutdown wrote %d rows for the memo in flight", n)
	}
}

// Criterion 8: a memo recorded while a pass works through ten older ones is the
// next memo routed once transcription is idle again.
func TestAMemoRecordedMidPassIsRoutedNext(t *testing.T) {
	h := newHarness(t)
	for i := 0; i < PassLimit; i++ {
		h.memo("old" + string(rune('a'+i)))
	}
	p := h.pump("m", true)
	var arrived uuid.UUID
	n := 0
	p.onRouteCalled = func(uuid.UUID) {
		n++
		if n == 3 {
			// Somebody records a memo. It is `captured` on its way in, which
			// is what makes transcription busy.
			arrived = h.ingest("arrived", true)
		}
	}
	rep := p.Tick(h.ctx)
	if !rep.Yielded || rep.Full {
		t.Fatalf("report = %+v, want the pass to end on finding transcription busy", rep)
	}
	p.onRouteCalled = nil
	before := len(h.ollama.callLog())
	if before >= PassLimit {
		t.Fatalf("the pass asked about %d memos; it should have ended at the arrival", before)
	}

	// Transcribed now, and transcription idle again.
	h.advance(arrived, store.StateCaptured, store.StateQueued, store.StateTranscribing, store.StateTranscribed)
	p.Tick(h.ctx)
	if log := h.ollama.callLog(); len(log) <= before || log[before] != "arrived" {
		t.Fatalf("calls = %s; the first call after the yield should be the new memo", fmtCalls(log))
	}
}

// Criterion 9: a memo decided while its generation runs gets no row.
func TestAMemoDecidedMidGenerationGetsNoRow(t *testing.T) {
	h := newHarness(t)
	id := h.memo("decided")
	p := h.pump("m", false)
	p.onRouteCalled = func(uuid.UUID) {
		h.advance(id, store.StateTranscribed, store.StateDiscarded)
	}
	rep := p.Tick(h.ctx)
	if rep.Dropped != 1 {
		t.Fatalf("report = %+v, want the result dropped", rep)
	}
	if n := len(h.rows(p.Proposer(), id)); n != 0 {
		t.Fatalf("a decided memo got %d rows", n)
	}
}

// Criterion 10 lives in unit_test.go; this is its integration twin, for the
// Switchyard half of "an outage writes nothing".
func TestACatalogueFailureAsksOllamaNothing(t *testing.T) {
	h := newHarness(t)
	id := h.memo("waiting")
	h.cat.err = errSwitchyardDown
	p := h.pump("m", false)
	rep := p.Tick(h.ctx)
	if !rep.Outage || len(h.ollama.callLog()) != 0 || len(h.rows(p.Proposer(), id)) != 0 {
		t.Fatalf("report = %+v, calls %d; want an outage with no Ollama call and no row", rep, len(h.ollama.callLog()))
	}
}

// Criterion 11, the pump half: busy keeps the pump off Ollama, idle lets it
// route, and with no transcription pump a `captured` memo stops nothing.
func TestTheTranscriptionGate(t *testing.T) {
	h := newHarness(t)
	ready := h.memo("ready")
	captured := h.ingest("captured", false)

	p := h.pump("m", true)
	if rep := p.Tick(h.ctx); !rep.Yielded {
		t.Fatalf("report = %+v, want a yield while a memo is captured", rep)
	}
	if n := len(h.ollama.callLog()); n != 0 {
		t.Fatalf("made %d Ollama calls while transcription was busy", n)
	}

	off := h.pump("m", false)
	off.Tick(h.ctx)
	wantStatus(t, h.rows(off.Proposer(), ready), ready, scribe.StatusValid)

	if _, err := h.st.Pool().Exec(h.ctx, `DELETE FROM tier1.memo_proposals`); err != nil {
		t.Fatal(err)
	}
	h.advance(captured, store.StateCaptured, store.StateQueued)
	p.Tick(h.ctx)
	wantStatus(t, h.rows(p.Proposer(), ready), ready, scribe.StatusValid)
}

// Criterion 12 (⚖2): transcription work arriving mid-generation cancels the
// request within two poll intervals, writes nothing, and the memo is routed
// once transcription is idle.
func TestTranscriptionWorkPreemptsAGeneration(t *testing.T) {
	h := newHarness(t)
	id := h.memo("long")
	h.ollama.set("long", answerHold)
	p := h.pump("m", true)
	p.poll = 100 * time.Millisecond

	done := make(chan Report)
	go func() { done <- p.Tick(h.ctx) }()
	<-h.ollama.started
	arrival := time.Now()
	busy := h.ingest("arrives", false)

	select {
	case at := <-h.ollama.cancelled:
		if d := at.Sub(arrival); d > 2*p.poll {
			t.Fatalf("the generation was cancelled %s after transcription work arrived, want within %s", d, 2*p.poll)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the generation was never cancelled")
	}
	if rep := <-done; rep.Preempted != 1 || !rep.Yielded {
		t.Fatalf("report = %+v, want a preemption", rep)
	}
	if n := len(h.rows(p.Proposer(), id)); n != 0 {
		t.Fatalf("a preempted generation wrote %d rows", n)
	}

	h.advance(busy, store.StateCaptured, store.StateQueued)
	h.ollama.set("long", answerValid)
	p.Tick(h.ctx)
	wantStatus(t, h.rows(p.Proposer(), id), id, scribe.StatusValid)
}
