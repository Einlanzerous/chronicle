package transcribe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-27. The pump, against a programmable ASR service.
//
// The happy path is also tested against the REAL service in
// integration_test.go, which is what actually proves the generated client and
// the implementation agree. What is here is the branches that are awkward to
// produce for real: an expired result, a failed job, an unreachable service.

// --- a programmable ASR ----------------------------------------------------

type fakeASR struct {
	mu sync.Mutex

	// submitStatus, when non-zero, is returned for every submit instead of a
	// job. Used to hold the pump at the point where the key is persisted and
	// the request has not landed.
	submitStatus int

	// jobStatus is what a poll reports. resultStatus overrides the result
	// fetch's status code.
	jobStatus    asrclient.JobStatus
	resultStatus int
	result       asrclient.Result

	// keys records every Idempotency-Key seen, in order. The whole point of
	// persist-before-send is that a retry of one attempt reuses one key, so
	// this is the assertion, not an aid to one.
	keys []string
	jobs map[string]uuid.UUID // key -> job id, so a replay answers the same job
}

func newFakeASR() *fakeASR {
	return &fakeASR{
		jobStatus: asrclient.JobStatusSucceeded,
		jobs:      map[string]uuid.UUID{},
		result: asrclient.Result{
			Status: asrclient.ResultStatusSucceeded, Partial: false,
			Text:     "a thought, spoken",
			Segments: []asrclient.Segment{{StartMs: 0, EndMs: 1800, Text: "a thought, spoken"}},
			Model:    "whisper.cpp/small.en", Backend: "vulkan",
		},
	}
}

func (f *fakeASR) serve(t *testing.T) ASR {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("POST /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		f.mu.Lock()
		f.keys = append(f.keys, key)
		status := f.submitStatus
		f.mu.Unlock()

		// Read the body so the parts are exercised rather than assumed.
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Errorf("submit was not multipart: %q", r.Header.Get("Content-Type"))
		}
		var sawSpec, sawAudio bool
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			switch part.FormName() {
			case "spec":
				var spec asrclient.JobSpec
				if err := json.NewDecoder(part).Decode(&spec); err != nil {
					t.Errorf("spec part: %v", err)
				}
				if len(spec.AudioSha256) != 64 {
					t.Errorf("spec carried no audio hash: %+v", spec)
				}
				sawSpec = true
			case "audio":
				b, _ := io.ReadAll(part)
				if len(b) == 0 {
					t.Error("the audio part was empty")
				}
				if ct := part.Header.Get("Content-Type"); ct == "" {
					t.Error("the audio part declared no Content-Type")
				}
				sawAudio = true
			}
		}
		if !sawSpec || !sawAudio {
			t.Errorf("submit had spec=%v audio=%v", sawSpec, sawAudio)
		}

		if status != 0 {
			// writeJSON rather than http.Error: http.Error sends text/plain,
			// and the generated client only fills JSON409 when the response
			// says it is JSON — so the branch under test would never be
			// reached and the test would pass against a default case.
			writeJSON(w, status, asrclient.Error{
				Code: "programmed", Message: "programmed failure",
			})
			return
		}

		f.mu.Lock()
		id, replay := f.jobs[key]
		if !replay {
			id = uuid.New()
			f.jobs[key] = id
		}
		f.mu.Unlock()

		code := http.StatusCreated
		if replay {
			code = http.StatusOK
		}
		writeJSON(w, code, asrclient.Job{
			Id: id, Status: asrclient.JobStatusQueued, Model: "small.en",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	})

	mux.HandleFunc("GET /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		status := f.jobStatus
		f.mu.Unlock()
		id, _ := uuid.Parse(r.PathValue("id"))
		writeJSON(w, http.StatusOK, asrclient.Job{
			Id: id, Status: status, Model: "small.en",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	})

	mux.HandleFunc("GET /v1/jobs/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		code, res := f.resultStatus, f.result
		f.mu.Unlock()
		if code != 0 && code != http.StatusOK {
			writeJSON(w, code, asrclient.Error{Code: "gone", Message: "the payload aged out"})
			return
		}
		res.JobId, _ = uuid.Parse(r.PathValue("id"))
		writeJSON(w, http.StatusOK, res)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := asrclient.NewClientWithResponses(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func (f *fakeASR) seenKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.keys...)
}

func (f *fakeASR) set(fn func(*fakeASR)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// --- harness ---------------------------------------------------------------

type harness struct {
	store *store.Store
	audio *audio.Store
	ctx   context.Context
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	audioStore, err := audio.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &harness{store: store.New(pool), audio: audioStore, ctx: ctx}
}

// memo ingests a recording AND puts its bytes on disk, because the pump reads
// both and a test that only did one would pass against a pump that only used
// the other.
func (h *harness) memo(t *testing.T, email, content string) store.Memo {
	t.Helper()
	user, err := h.store.CreateUser(h.ctx, email, "Author", store.KindPerson)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256Hex(content)
	res, err := h.store.IngestMemo(h.ctx, store.Arrival{
		AuthorID: user.ID, ContentHash: hash, ByteSize: int64(len(content)),
		Source: store.SourceUpload, SourceRef: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	path, err := h.audio.Path(audio.Ref{AuthorID: user.ID, ContentHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return res.Memo
}

// noBackoff turns off CHRN-28's retry delay. A test that has to reach the
// ceiling would otherwise take half an hour of wall clock, and what those tests
// are about is the bound rather than the wait — the wait has its own test.
func noBackoff(o *Options) { o.RetryBackoff = -1 }

func (h *harness) pump(t *testing.T, asr ASR, opts ...func(*Options)) *Service {
	t.Helper()
	o := Options{
		Store:  h.store,
		Audio:  h.audio,
		ASR:    asr,
		Logger: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
		Model:  "small.en",
	}
	for _, fn := range opts {
		fn(&o)
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (h *harness) state(t *testing.T, id uuid.UUID) store.Memo {
	t.Helper()
	m, err := h.store.GetMemo(h.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// --- the tests -------------------------------------------------------------

// The whole path: a captured memo becomes a transcript, with the model and
// backend that produced it stored beside it.
func TestMemoBecomesATranscript(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "e2e@example.test", "pretend opus")

	// One sweep is enough here, and that is deliberate rather than incidental:
	// collect runs after submit in the same sweep, so a short memo — three
	// seconds of GPU, the common case — does not wait out an interval it has
	// already finished.
	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State != store.StateTranscribed {
		t.Fatalf("state %q, want transcribed", m.State)
	}

	tr, err := h.store.GetTranscript(h.ctx, memo.ID)
	if err != nil {
		t.Fatalf("no transcript was stored: %v", err)
	}
	if tr.Text != "a thought, spoken" || tr.Partial {
		t.Fatalf("transcript = %+v", tr)
	}
	if tr.Model != "whisper.cpp/small.en" || tr.Backend != "vulkan" {
		t.Fatalf("model %q backend %q — both are stored so a corpus transcribed by two "+
			"models does not vary invisibly", tr.Model, tr.Backend)
	}
	if len(tr.Segments) != 1 {
		t.Fatalf("segments = %+v", tr.Segments)
	}

	durable, err := h.store.HasDurableTranscript(h.ctx, memo.ID)
	if err != nil || !durable {
		t.Fatalf("the memo did not become durable: durable=%v err=%v", durable, err)
	}
}

// A ROW IS WRITTEN FOR EVERY SUCCEEDED RESULT, EMPTY TEXT INCLUDED.
//
// CHRN-25 §5 binds this on CHRN-27 in as many words, and names the line that
// breaks it: `if text == "" { return }` inverts the ruling in the SAFE
// direction, so nothing complains, and the audio of every silent memo is
// stranded forever.
func TestASilentMemoStillGetsATranscript(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) {
		f.result.Text = ""
		f.result.Segments = []asrclient.Segment{}
	})
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "silence@example.test", "forty seconds of traffic")

	p.Tick(h.ctx)
	p.Tick(h.ctx)

	if got := h.state(t, memo.ID).State; got != store.StateTranscribed {
		t.Fatalf("state %q; a silent memo is transcribed, not stuck", got)
	}
	tr, err := h.store.GetTranscript(h.ctx, memo.ID)
	if err != nil {
		t.Fatalf("no transcript row was written for an empty result: %v", err)
	}
	if tr.Partial {
		t.Fatal("a completed run over silence was recorded as partial")
	}
	durable, err := h.store.HasDurableTranscript(h.ctx, memo.ID)
	if err != nil || !durable {
		t.Fatal("a silent memo did not become durable, so its audio will never prune")
	}
}

// A partial result is stored and does NOT satisfy the gate.
func TestAPartialResultIsStoredButNotDurable(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) { f.result.Partial = true })
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "partial@example.test", "half a memo")

	p.Tick(h.ctx)
	p.Tick(h.ctx)

	tr, err := h.store.GetTranscript(h.ctx, memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Partial {
		t.Fatal("`partial` was not carried across the boundary; it is a fact the SERVICE " +
			"recorded and is never recomputed here")
	}
	durable, err := h.store.HasDurableTranscript(h.ctx, memo.ID)
	if err != nil || durable {
		t.Fatal("a partial transcript satisfied the durable gate; CHRN-22 would prune its audio")
	}
}

// A failure leaves the memo in a state a human can see, with a reason.
func TestAFailureHoldsTheMemoWithAReason(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) {
		f.jobStatus = asrclient.JobStatusFailed
		f.result.Status = asrclient.ResultStatusFailed
		f.result.Partial = true
		f.result.Failure = &asrclient.Failure{Code: "decode_failed", Message: "no audio stream"}
	})
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "fail@example.test", "corrupt")

	p.Tick(h.ctx)
	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State != store.StateHeld {
		t.Fatalf("state %q, want held", m.State)
	}
	if m.StateReason == nil || !strings.Contains(*m.StateReason, "decode_failed") {
		t.Fatalf("state_reason = %v; a human has to be able to see WHAT failed", m.StateReason)
	}
	// And nothing was written to tier 2. A failed run is not a transcript.
	if _, err := h.store.GetTranscript(h.ctx, memo.ID); err == nil {
		t.Fatal("a failed transcription wrote a transcript")
	}

	// The retry path: held -> queued, which `chronicle retranscribe` drives.
	if _, err := h.store.AdvanceMemoState(h.ctx, memo.ID, store.StateHeld, store.StateQueued, ""); err != nil {
		t.Fatalf("a held memo could not be retried: %v", err)
	}
}

// THE KEY IS PERSISTED BEFORE THE REQUEST IS SENT, and a retry of one attempt
// reuses it.
//
// This is the assertion CHRN-25 §3 exists for. Without it the failure is:
// Chronicle submits, dies before recording the job id, comes back and retries
// — and the GPU transcribes the memo twice, leaving two results for one memo
// and no way to say which is the transcript.
func TestOneAttemptReusesOneKey(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) { f.submitStatus = http.StatusInternalServerError })
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "onekey@example.test", "spoken once")

	p.Tick(h.ctx)

	// The row exists and the submit did not land: exactly the state the
	// ordering exists to make recoverable.
	job, err := h.store.LatestMemoJob(h.ctx, memo.ID)
	if err != nil {
		t.Fatalf("no attempt was recorded before the submit: %v", err)
	}
	if job.JobID != nil {
		t.Fatal("a job id was recorded for a submit that failed")
	}
	first := job.IdempotencyKey

	// Let it through, and sweep twice more.
	asr.set(func(f *fakeASR) { f.submitStatus = 0 })
	p.Tick(h.ctx)
	p.Tick(h.ctx)

	keys := asr.seenKeys()
	if len(keys) < 2 {
		t.Fatalf("only %d submits were seen; the retry never happened", len(keys))
	}
	for i, k := range keys {
		if k != first {
			t.Fatalf("submit %d used key %q, but the attempt persisted %q — a second key "+
				"is a second job, and the GPU transcribes the memo twice", i, k, first)
		}
	}

	var attempts int
	if err := h.store.Pool().QueryRow(h.ctx,
		`SELECT count(*) FROM tier1.memo_jobs WHERE memo_id = $1`, memo.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("%d attempts for one memo; a failed submit must be resumed, not duplicated", attempts)
	}
	if got := h.state(t, memo.ID).State; got != store.StateTranscribed {
		t.Fatalf("state %q; the resumed attempt did not complete", got)
	}
}

// "Result expired" is not "transcription failed". A 410 sends the memo back to
// the queue for a fresh attempt rather than holding it as though something was
// wrong with the recording.
func TestAnExpiredResultRequeuesRatherThanHolding(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) { f.resultStatus = http.StatusGone })
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "expired@example.test", "said a while ago")

	p.Tick(h.ctx)
	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State == store.StateHeld {
		t.Fatal("an expired result held the memo. CHRN-25 §9: result expired is NOT " +
			"transcription failed, and holding it makes an aged-out payload look like a broken memo")
	}
	if m.State != store.StateQueued && m.State != store.StateTranscribing {
		t.Fatalf("state %q; want the memo back in the queue for a fresh attempt", m.State)
	}

	// And the fresh attempt uses a FRESH key, because it is a different
	// attempt — the opposite of the retry case above, and the distinction §3
	// turns on.
	asr.set(func(f *fakeASR) { f.resultStatus = 0 })
	p.Tick(h.ctx)
	p.Tick(h.ctx)

	keys := asr.seenKeys()
	if len(keys) < 2 || keys[0] == keys[len(keys)-1] {
		t.Fatalf("a deliberate re-transcription reused the first key: %v", keys)
	}
	if got := h.state(t, memo.ID).State; got != store.StateTranscribed {
		t.Fatalf("state %q after re-submitting", got)
	}
}

// CHRN-18's Done-when #7: the worker SKIPS ASR when a durable transcript
// already exists. Read from Chronicle, never from the ASR service — whose
// answer at thirty days is a 410.
func TestADurableTranscriptSkipsASR(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "already@example.test", "said before")

	if _, err := h.store.RecordTranscript(h.ctx, store.TranscriptInput{
		MemoID: memo.ID, Text: "said before", Partial: false,
		Model: "small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatal(err)
	}

	p.Tick(h.ctx)

	if keys := asr.seenKeys(); len(keys) != 0 {
		t.Fatalf("%d submissions for a memo that already had a transcript; the GPU ran for nothing", len(keys))
	}
	if got := h.state(t, memo.ID).State; got != store.StateTranscribed {
		t.Fatalf("state %q; an already-transcribed memo should settle rather than loop", got)
	}
}

// A memo whose audio is not on disk is HELD, not retried forever. No number of
// retries produces a file that is not there.
func TestMissingAudioHoldsRatherThanLoops(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "gone@example.test", "was here")

	path, err := h.audio.Path(audio.Ref{AuthorID: memo.AuthorID, ContentHash: memo.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State != store.StateHeld {
		t.Fatalf("state %q, want held", m.State)
	}
	if m.StateReason == nil || !strings.Contains(*m.StateReason, "audio_missing") {
		t.Fatalf("state_reason = %v", m.StateReason)
	}
	if keys := asr.seenKeys(); len(keys) != 0 {
		t.Fatal("a memo with no audio was submitted anyway")
	}
}

// The attempt ceiling bounds the requeue loop, so that a service which keeps
// losing jobs cannot put one memo through an unmetered series of GPU runs.
//
// CHRN-28 turned this from a bound into a policy: the memo is held with a
// reason that tells a person what happened and what clears it, rather than one
// naming a ticket that had not been written yet.
func TestTheAttemptCeilingBoundsTheLoop(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) { f.resultStatus = http.StatusGone })
	p := h.pump(t, asr.serve(t), noBackoff)
	memo := h.memo(t, "loop@example.test", "round and round")

	for i := 0; i < DefaultMaxAttempts*3; i++ {
		p.Tick(h.ctx)
	}

	m := h.state(t, memo.ID)
	if m.State != store.StateHeld {
		t.Fatalf("state %q after %d sweeps; the requeue loop is unbounded", m.State, DefaultMaxAttempts*3)
	}
	if m.StateReason == nil || !strings.Contains(*m.StateReason, "retranscribe") {
		t.Fatalf("state_reason = %v; a held memo's reason has to tell a person what clears it",
			m.StateReason)
	}

	var attempts int
	if err := h.store.Pool().QueryRow(h.ctx,
		`SELECT count(*) FROM tier1.memo_jobs WHERE memo_id = $1`, memo.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts > DefaultMaxAttempts {
		t.Fatalf("%d attempts; the ceiling is %d", attempts, DefaultMaxAttempts)
	}
}

// The audio part's media type comes from what the recording IS, not from what
// it is called — a filename is display-only in this system.
func TestMediaTypeFor(t *testing.T) {
	opus := "opus"
	webm := "memo.webm"
	m4a := "voice.M4A"
	odd := "recording.xyz"

	cases := []struct {
		name string
		memo store.Memo
		want string
	}{
		{"codec wins over filename", store.Memo{Codec: &opus, OriginalFilename: &webm}, "audio/ogg"},
		{"filename when there is no codec", store.Memo{OriginalFilename: &webm}, "audio/webm"},
		{"extensions are case-insensitive", store.Memo{OriginalFilename: &m4a}, "audio/mp4"},
		{"an unknown extension falls back", store.Memo{OriginalFilename: &odd}, "audio/ogg"},
		{"nothing known falls back", store.Memo{}, "audio/ogg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mediaTypeFor(c.memo); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// A 409 REQUEUES FOR A FRESH KEY, it does not hold.
//
// A held memo is never returned by MemosAwaitingTranscription, so the first
// version of this path settled the attempt and left a comment promising that
// "the next sweep mints a fresh key" — a recovery no code path provided. CHRN-25
// §3 prescribes the opposite: on a mismatch the client mints a fresh key and
// retries.
func TestAnIdempotencyMismatchRequeuesForAFreshKey(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(func(f *fakeASR) { f.submitStatus = http.StatusConflict })
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "mismatch@example.test", "said once")

	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State == store.StateHeld {
		t.Fatal("a 409 held the memo. Nothing sweeps a held memo, so the fresh key the " +
			"contract prescribes would never be minted and only the CLI could recover it")
	}
	if m.State != store.StateQueued {
		t.Fatalf("state %q, want queued", m.State)
	}
	first := asr.seenKeys()
	if len(first) != 1 {
		t.Fatalf("%d submits, want 1", len(first))
	}

	// The next sweep really does mint a fresh key, and the memo completes.
	asr.set(func(f *fakeASR) { f.submitStatus = 0 })
	p.Tick(h.ctx)
	p.Tick(h.ctx)

	keys := asr.seenKeys()
	if keys[len(keys)-1] == first[0] {
		t.Fatalf("the retry reused the rejected key %q; a mismatch means that key already "+
			"names something else", first[0])
	}
	if got := h.state(t, memo.ID).State; got != store.StateTranscribed {
		t.Fatalf("state %q; the memo did not recover", got)
	}
}

// --- CHRN-28: retry, then somewhere a human notices ------------------------

// failing points the fake at a failed job carrying one code.
func (f *fakeASR) failing(code string) func(*fakeASR) {
	return func(f *fakeASR) {
		f.jobStatus = asrclient.JobStatusFailed
		f.result = asrclient.Result{
			Status:  asrclient.ResultStatusFailed,
			Partial: true,
			Model:   "whisper.cpp/small.en", Backend: "vulkan",
			Segments: []asrclient.Segment{},
			Failure:  &asrclient.Failure{Code: code, Message: "the fake failed on purpose"},
		}
	}
}

// A FAILING MEMO IS RETRIED BEFORE IT IS HELD. Half of the ticket's Done-when,
// and the half CHRN-27 did not ship: every failure held the memo on its first
// attempt, so one dropped job, one crashed child or one 500 from a restarting
// service each cost a person's attention.
func TestAFailedAttemptIsRetriedBeforeItIsHeld(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(asr.failing("inference_failed"))
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "retry@example.test", "worth another go")

	// One sweep to submit, one to poll and fail.
	p.Tick(h.ctx)
	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State == store.StateHeld {
		t.Fatal("a transient failure held the memo on its first attempt; it should be retried")
	}
	if m.State != store.StateQueued {
		t.Fatalf("state %q; a failed attempt returns the memo to the queue", m.State)
	}
}

// A PERMANENT FAILURE IS HELD AT ONCE. Spending the ceiling to arrive at the
// same answer wastes GPU and delays the human who has to fix the mount.
func TestAPermanentFailureIsHeldWithoutSpendingTheCeiling(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(asr.failing("decode_failed"))
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "broken@example.test", "unreadable bytes")

	p.Tick(h.ctx)
	p.Tick(h.ctx)

	m := h.state(t, memo.ID)
	if m.State != store.StateHeld {
		t.Fatalf("state %q; a decode failure will fail identically next time", m.State)
	}
	if m.StateReason == nil || !strings.Contains(*m.StateReason, "decode_failed") {
		t.Fatalf("state_reason = %v; the code is what tells a person what to fix", m.StateReason)
	}

	var attempts int
	if err := h.store.Pool().QueryRow(h.ctx,
		`SELECT count(*) FROM tier1.memo_jobs WHERE memo_id = $1`, memo.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("%d attempts for a permanent failure; want 1", attempts)
	}
}

// asrd's own ceiling is permanent HERE too. Starting a fresh attempt would run
// the same file into the same wall with a new counter, which is how two
// bounded loops make one unbounded one.
func TestAnExhaustedASRJobIsNotRetriedAgain(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(asr.failing("retries_exhausted"))
	p := h.pump(t, asr.serve(t))
	memo := h.memo(t, "exhausted@example.test", "the service gave up")

	p.Tick(h.ctx)
	p.Tick(h.ctx)

	if m := h.state(t, memo.ID); m.State != store.StateHeld {
		t.Fatalf("state %q; the ASR service already exhausted its retries", m.State)
	}
}

// NO FAILURE PATH PRODUCES A DURABLE TRANSCRIPT — the ticket's last Done-when,
// and the one that matters most: a durable transcript is the only thing that
// lets CHRN-22 delete the audio, so this is the assertion that failure can
// never cost a recording.
func TestNoFailurePathLeavesADurableTranscript(t *testing.T) {
	for _, code := range []string{
		"decode_failed", "inference_failed", "retries_exhausted",
		"model_unloadable", "internal_error",
	} {
		t.Run(code, func(t *testing.T) {
			h := newHarness(t)
			asr := newFakeASR()
			asr.set(asr.failing(code))
			p := h.pump(t, asr.serve(t), noBackoff)
			memo := h.memo(t, code+"@example.test", "a memo that fails")

			for i := 0; i < DefaultMaxAttempts*2+2; i++ {
				p.Tick(h.ctx)
			}

			durable, err := h.store.HasDurableTranscript(h.ctx, memo.ID)
			if err != nil {
				t.Fatal(err)
			}
			if durable {
				t.Fatalf("%s produced a durable transcript; the pruner would delete the audio", code)
			}
			if m := h.state(t, memo.ID); m.State != store.StateHeld {
				t.Fatalf("state %q after the ceiling; a failing memo must end somewhere visible", m.State)
			}
		})
	}
}

// THE RETRIES ARE BACKED OFF, NOT MERELY BOUNDED. The ticket asks for "bounded
// retries with backoff", and a ceiling spent at the sweep interval spends
// itself in about a hundred seconds — so a whisper process restarting takes
// every memo in flight to `held` for a fault that clears on its own. That is
// the cost this ticket exists to remove, moved from attempt one to attempt
// five rather than removed.
func TestAFailedAttemptWaitsBeforeItIsRetried(t *testing.T) {
	h := newHarness(t)
	asr := newFakeASR()
	asr.set(asr.failing("inference_failed"))
	p := h.pump(t, asr.serve(t)) // the real backoff, one minute
	memo := h.memo(t, "backoff@example.test", "not so fast")

	p.Tick(h.ctx) // submit
	p.Tick(h.ctx) // poll, fail, requeue

	if m := h.state(t, memo.ID); m.State != store.StateQueued {
		t.Fatalf("state %q; setup expects the memo back in the queue", m.State)
	}

	// Several more sweeps, all inside the first minute.
	for i := 0; i < 5; i++ {
		p.Tick(h.ctx)
	}

	var attempts int
	if err := h.store.Pool().QueryRow(h.ctx,
		`SELECT count(*) FROM tier1.memo_jobs WHERE memo_id = $1`, memo.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("%d attempts within the backoff window; the ceiling would be spent in "+
			"seconds and a transient fault would still cost a person", attempts)
	}
	if m := h.state(t, memo.ID); m.State != store.StateQueued {
		t.Fatalf("state %q; a memo waiting out its backoff stays queued rather than held", m.State)
	}
}

// The delay grows, and it is capped. An unbounded doubling would put the last
// attempt beyond any interval an operator would wait for.
func TestTheRetryDelayDoublesAndIsCapped(t *testing.T) {
	s := &Service{retryBackoff: RetryBackoff}
	if got := s.retryDelay(0); got != 0 {
		t.Fatalf("a memo with no attempts waited %s; the first attempt is never delayed", got)
	}
	if got := s.retryDelay(1); got != RetryBackoff {
		t.Fatalf("first retry waits %s, want %s", got, RetryBackoff)
	}
	if got := s.retryDelay(2); got != 2*RetryBackoff {
		t.Fatalf("second retry waits %s, want %s", got, 2*RetryBackoff)
	}
	if got := s.retryDelay(40); got != MaxRetryBackoff {
		t.Fatalf("a far-out attempt waited %s; the doubling must cap rather than overflow", got)
	}
}
