package transcribe_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/asr"
	"github.com/Einlanzerous/chronicle/internal/asrclient"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/transcribe"
)

// Chronicle's client against the REAL ASR service, over the real wire format.
//
// This is what a generated client is supposed to buy and what nothing else in
// either package tests: transcribe_test.go asserts the pump's branches against
// a hand-written fake, and internal/asr asserts the service against its own
// expectations, and BOTH could be self-consistently wrong about the contract
// between them. Here the service is internal/asr.NewRouter, the client is the
// generated one, and the only thing standing between them is
// deploy/asr/openapi.yaml.
//
// It needs both databases, because the two services have two — which is
// itself the arrangement under test.

// stubTranscriber stands in for the GPU. It is a Go type rather than a fake
// binary because what is being tested here is the CONTRACT, not the runner;
// internal/asr's own tests exercise ffmpeg and a real child process through the
// real Resident.
type stubTranscriber struct{ text string }

func (s stubTranscriber) Models() []string { return []string{"small.en"} }

func (s stubTranscriber) Transcribe(ctx context.Context, req asr.TranscribeRequest) (asr.Transcript, error) {
	const durationMs = 60000
	// The worker moves the job leased -> running here, which is the edge
	// CHRN-26 made visible: `leased` while it decodes and waits for the
	// device, `running` only once inference has actually started.
	if req.OnInference != nil {
		if err := req.OnInference(durationMs); err != nil {
			return asr.Transcript{}, err
		}
	}
	segs := []asrclient.Segment{}
	covered := int64(0)
	if s.text != "" {
		segs = append(segs, asrclient.Segment{StartMs: 0, EndMs: 1800, Text: s.text})
		covered = 1800
	}
	return asr.Transcript{
		Text: s.text, Segments: segs,
		AudioDurationMs: durationMs, CoveredMs: covered,
	}, nil
}

func TestChronicleTranscribesThroughTheRealService(t *testing.T) {
	chronicleDSN := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	asrDSN := strings.TrimSpace(os.Getenv("ASR_TEST_DATABASE_URL"))
	if chronicleDSN == "" || asrDSN == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL and ASR_TEST_DATABASE_URL are both needed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	quiet := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	// --- the ASR service, for real -----------------------------------------
	asrPool, err := asr.Connect(ctx, asrDSN)
	if err != nil {
		t.Fatalf("asr connect: %v", err)
	}
	defer asrPool.Close()
	if err := asr.Migrate(ctx, asrPool); err != nil {
		t.Fatalf("asr migrate: %v", err)
	}
	if _, err := asrPool.Exec(ctx, `TRUNCATE jobs`); err != nil {
		t.Fatalf("asr truncate: %v", err)
	}
	asrStore := asr.New(asrPool, "vulkan", time.Hour)

	const token = "integration-token-aaaaaaaaaaaaaaaaaa"
	stub := stubTranscriber{text: "the whole thought, spoken once"}
	srv := httptest.NewServer(asr.NewRouter(asr.Deps{
		Store:         asrStore,
		Transcriber:   stub,
		Logger:        quiet,
		Tokens:        map[string]string{token: "chronicle"},
		DefaultModel:  "small.en",
		MaxAudioBytes: 1 << 20,
	}))
	defer srv.Close()

	worker := &asr.Worker{
		Store: asrStore, Transcriber: stub, Logger: quiet,
		ID: "integration", LeaseTTL: 30 * time.Second, Idle: 50 * time.Millisecond,
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() { _ = worker.Run(workerCtx) }()

	// --- Chronicle ---------------------------------------------------------
	chrPool, err := store.Connect(ctx, chronicleDSN)
	if err != nil {
		t.Fatalf("chronicle connect: %v", err)
	}
	defer chrPool.Close()
	if err := store.MigrateDown(ctx, chrPool, 0); err != nil {
		t.Fatalf("chronicle reset: %v", err)
	}
	if err := store.Migrate(ctx, chrPool); err != nil {
		t.Fatalf("chronicle migrate: %v", err)
	}
	st := store.New(chrPool)

	audioStore, err := audio.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	user, err := st.CreateUser(ctx, "integration@example.test", "Author", store.KindPerson)
	if err != nil {
		t.Fatal(err)
	}
	const body = "pretend this is a real opus recording"
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	res, err := st.IngestMemo(ctx, store.Arrival{
		AuthorID: user.ID, ContentHash: hash, ByteSize: int64(len(body)),
		Source: store.SourceUpload, SourceRef: "integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	path, err := audioStore.Path(audio.Ref{AuthorID: user.ID, ContentHash: hash})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	client, err := asrclient.NewClientWithResponses(srv.URL,
		asrclient.WithRequestEditorFn(transcribe.BearerAuth(token)))
	if err != nil {
		t.Fatal(err)
	}
	pump, err := transcribe.New(transcribe.Options{
		Store: st, Audio: audioStore, ASR: client, Logger: quiet, Model: "small.en",
	})
	if err != nil {
		t.Fatal(err)
	}

	// --- and let them talk -------------------------------------------------
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pump.Tick(ctx)
		m, err := st.GetMemo(ctx, res.Memo.ID)
		if err != nil {
			t.Fatal(err)
		}
		if m.State == store.StateTranscribed {
			break
		}
		if m.State == store.StateHeld {
			reason := ""
			if m.StateReason != nil {
				reason = *m.StateReason
			}
			t.Fatalf("the memo was held: %s", reason)
		}
		time.Sleep(100 * time.Millisecond)
	}

	m, err := st.GetMemo(ctx, res.Memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != store.StateTranscribed {
		t.Fatalf("memo state %q after 30s; the two services did not complete a transcription", m.State)
	}

	tr, err := st.GetTranscript(ctx, res.Memo.ID)
	if err != nil {
		t.Fatalf("no transcript: %v", err)
	}
	if tr.Text != stub.text {
		t.Fatalf("text %q, want %q", tr.Text, stub.text)
	}
	if tr.Partial {
		t.Fatal("a completed run arrived marked partial")
	}
	if tr.Model != "whisper.cpp/small.en" || tr.Backend != "vulkan" {
		t.Fatalf("model %q backend %q — the service's own account of what produced this "+
			"did not survive the trip", tr.Model, tr.Backend)
	}
	if len(tr.Segments) != 1 || tr.Segments[0].EndMS != 1800 {
		t.Fatalf("segments did not survive the wire format: %+v", tr.Segments)
	}
	if tr.AudioDurationMS == nil || *tr.AudioDurationMS != 60000 {
		t.Fatalf("audio_duration_ms = %v", tr.AudioDurationMS)
	}

	// CHRN-22's MODEL FLOOR, PROVEN THROUGH THE REAL PATH, and this assertion
	// is why the test carries it here rather than in a store unit test.
	//
	// The floor reads `whisper.cpp/small.en`, and the `whisper.cpp/` half is a
	// string built by the ASR WORKER (internal/asr, another binary's code) and
	// carried across the HTTP contract. A literal fixture in a store test
	// cannot prove the two agree: if asrd ever renamed the runner, the pruner
	// would silently stop firing and every hand-written `whisper.cpp/small.en`
	// would keep passing. This transcript was produced by the real worker, sent
	// over the real contract and written by the real pump, so the gate is being
	// asked about a string nothing in this test typed.
	durable, err := st.HasDurableTranscript(ctx, res.Memo.ID)
	if err != nil || !durable {
		t.Fatal("a transcript that arrived through the real service did not satisfy the " +
			"durable gate. Either the model floor and the worker's model string disagree — " +
			"in which case the pruner has silently stopped — or the transcript is not durable")
	}
	if !strings.HasPrefix(tr.Model, "whisper.cpp/") {
		t.Fatalf("the worker wrote model %q; the floor's runner half reads the part before "+
			"the slash, and this is the string it has to match", tr.Model)
	}

	// The ASR side released the audio it was given: nothing in that service
	// may become the only copy of anything, and a terminal job holding bytes
	// is the first step towards it.
	var held []byte
	if err := asrPool.QueryRow(ctx,
		`SELECT audio FROM jobs WHERE audio IS NOT NULL`).Scan(&held); err == nil {
		t.Fatal("the ASR service is still holding submitted audio for a finished job")
	}
}

// A silent recording makes the round trip and comes back as a durable, empty
// transcript — through the real wire format, where an optional `text` field
// would have arrived as a nil pointer and been skipped.
func TestSilenceSurvivesTheWireFormat(t *testing.T) {
	chronicleDSN := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	asrDSN := strings.TrimSpace(os.Getenv("ASR_TEST_DATABASE_URL"))
	if chronicleDSN == "" || asrDSN == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL and ASR_TEST_DATABASE_URL are both needed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	quiet := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	asrPool, err := asr.Connect(ctx, asrDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer asrPool.Close()
	if err := asr.Migrate(ctx, asrPool); err != nil {
		t.Fatal(err)
	}
	if _, err := asrPool.Exec(ctx, `TRUNCATE jobs`); err != nil {
		t.Fatal(err)
	}
	asrStore := asr.New(asrPool, "vulkan", time.Hour)

	const token = "integration-token-bbbbbbbbbbbbbbbbbb"
	stub := stubTranscriber{text: ""} // forty seconds of traffic noise
	srv := httptest.NewServer(asr.NewRouter(asr.Deps{
		Store: asrStore, Transcriber: stub, Logger: quiet,
		Tokens: map[string]string{token: "chronicle"}, DefaultModel: "small.en",
		MaxAudioBytes: 1 << 20,
	}))
	defer srv.Close()

	worker := &asr.Worker{
		Store: asrStore, Transcriber: stub, Logger: quiet,
		ID: "integration", LeaseTTL: 30 * time.Second, Idle: 50 * time.Millisecond,
	}
	workerCtx, stopWorker := context.WithCancel(ctx)
	defer stopWorker()
	go func() { _ = worker.Run(workerCtx) }()

	chrPool, err := store.Connect(ctx, chronicleDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer chrPool.Close()
	if err := store.MigrateDown(ctx, chrPool, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, chrPool); err != nil {
		t.Fatal(err)
	}
	st := store.New(chrPool)

	audioStore, err := audio.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	user, err := st.CreateUser(ctx, "silence@example.test", "Author", store.KindPerson)
	if err != nil {
		t.Fatal(err)
	}
	const body = "forty seconds of traffic"
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	res, err := st.IngestMemo(ctx, store.Arrival{
		AuthorID: user.ID, ContentHash: hash, ByteSize: int64(len(body)),
		Source: store.SourceUpload, SourceRef: "integration",
	})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := audioStore.Path(audio.Ref{AuthorID: user.ID, ContentHash: hash})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	client, err := asrclient.NewClientWithResponses(srv.URL,
		asrclient.WithRequestEditorFn(transcribe.BearerAuth(token)))
	if err != nil {
		t.Fatal(err)
	}
	pump, err := transcribe.New(transcribe.Options{
		Store: st, Audio: audioStore, ASR: client, Logger: quiet, Model: "small.en",
	})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		pump.Tick(ctx)
		m, _ := st.GetMemo(ctx, res.Memo.ID)
		if m.State == store.StateTranscribed || m.State == store.StateHeld {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	m, err := st.GetMemo(ctx, res.Memo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != store.StateTranscribed {
		t.Fatalf("state %q; a silent memo has a true and complete answer and must not stick", m.State)
	}
	tr, err := st.GetTranscript(ctx, res.Memo.ID)
	if err != nil {
		t.Fatalf("no transcript row for a silent memo: %v — this is the exact failure "+
			"CHRN-25 §5 names, and it strands the audio of every such memo forever", err)
	}
	if tr.Text != "" || tr.Segments == nil || tr.Partial {
		t.Fatalf("transcript = %+v", tr)
	}
	durable, err := st.HasDurableTranscript(ctx, res.Memo.ID)
	if err != nil || !durable {
		t.Fatal("a silent memo did not become durable")
	}
}
