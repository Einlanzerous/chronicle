package asr

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A fake whisper-server, so every property CHRN-26 promises can be tested
// WITHOUT A GPU and in CI: residency, single-flight, the inference deadline, a
// child that crashes, a model switch that fails, and a cancellation that has to
// actually stop the work.
//
// It is THE TEST BINARY RE-EXECUTED, not a shell script and not an httptest
// server. It has to be a real child process because that is what the code under
// test supervises — spawning it, polling its /health, and killing it are the
// mechanism — and it has to speak HTTP because /inference, /load and /health
// are the conversation. A stub the resident dialled instead of spawned would
// test neither half.

const (
	fakeWhisperEnv    = "ASR_FAKE_WHISPER"
	fakeWhisperDirEnv = "ASR_FAKE_WHISPER_DIR"
)

// The modes, read from <dir>/mode on EVERY request so a test can change its
// mind mid-flight — "crash on this job, behave on the retry" is the shape of
// most of the interesting ones.
const (
	fakeOK         = "ok"         // answer at once
	fakeGate       = "gate"       // answer once <dir>/release exists
	fakeHang       = "hang"       // never answer: the wedged child §7 is about
	fakeCrash      = "crash"      // exit(1) on /inference
	fakeLoadFail   = "loadfail"   // exit(1) on /load, which is what upstream does
	fakeLoadAbsent = "loadabsent" // 400 on /load: the model is not there
	fakeSlow       = "slow"       // answer after <dir>/sleep_ms
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeWhisperEnv) != "" {
		fakeWhisperMain()
		return
	}
	os.Exit(m.Run())
}

// fakeWhisperMain is the child. It never returns.
func fakeWhisperMain() {
	dir := os.Getenv(fakeWhisperDirEnv)
	port, model := "", ""
	for i, a := range os.Args {
		if i+1 >= len(os.Args) {
			break
		}
		switch a {
		case "--port":
			port = os.Args[i+1]
		case "-m":
			model = os.Args[i+1]
		}
	}
	if port == "" {
		fmt.Fprintln(os.Stderr, "fake whisper-server: no --port")
		os.Exit(2)
	}

	// The startup banner the real one prints, so announceGGMLBackend has
	// something to read and the CPU-fallback warning is exercised rather than
	// assumed.
	fmt.Fprintln(os.Stderr, "ggml_vulkan: Found 1 Vulkan devices:")
	fmt.Fprintln(os.Stderr, "ggml_vulkan: 0 = Radeon AI PRO R9700 (RADV GFX1201) | matrix cores: KHR_coopmat")

	appendEvent(dir, "start "+filepath.Base(model))

	// Do not outlive the process that spawned us. asrd is SIGKILLed in two of
	// these tests, and a stray fake that serves on a port for another hour is a
	// stray fake somebody eventually finds in ps and has to explain.
	parent := os.Getppid()
	go func() {
		for {
			time.Sleep(200 * time.Millisecond)
			if os.Getppid() != parent {
				os.Exit(0)
			}
		}
	}()

	var inflight atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("POST /load", func(w http.ResponseWriter, r *http.Request) {
		want := r.FormValue("model")
		switch fakeMode(dir) {
		case fakeLoadFail:
			// What the real one does when the new model will not initialise:
			// it has already freed the old one, so there is nothing to fall
			// back to and it exits. (server.cpp:1184-1194, upstream's own TODO.)
			appendEvent(dir, "load-exit "+filepath.Base(want))
			os.Exit(1)
		case fakeLoadAbsent:
			appendEvent(dir, "load-400 "+filepath.Base(want))
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"model not found!"}`))
			return
		}
		appendEvent(dir, "load "+filepath.Base(want))
		_, _ = w.Write([]byte("Load was successful!"))
	})

	mux.HandleFunc("POST /inference", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		format := r.FormValue("response_format")

		// SINGLE-FLIGHT IS THE CALLER'S JOB, NOT THIS FAKE'S. The real server
		// holds whisper_mutex for the whole of /inference, so it would hide an
		// asrd that admitted two at once. This one deliberately does not
		// serialise, and says so when it sees an overlap.
		n := inflight.Add(1)
		defer inflight.Add(-1)
		if n > 1 {
			appendEvent(dir, "OVERLAP")
		}
		appendEvent(dir, "infer "+format)

		switch mode := fakeMode(dir); mode {
		case fakeCrash:
			appendEvent(dir, "infer-exit")
			os.Exit(1)
		case fakeHang:
			// The case no lease can see: a child that stops answering without
			// exiting. Nothing here ever writes a response.
			select {}
		case fakeGate:
			release := filepath.Join(dir, "release")
			for {
				if _, err := os.Stat(release); err == nil {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
		case fakeSlow:
			time.Sleep(fakeSleep(dir))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeTranscript))
		appendEvent(dir, "answered")
	})

	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fake whisper-server:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// fakeTranscript is verbose_json as the pinned server emits it: FLOAT SECONDS,
// which is the conversion the placeholder did not need and which, left out,
// gives a corpus timed a thousand times short.
const fakeTranscript = `{
  "task": "transcribe",
  "language": "english",
  "duration": 3.0,
  "text": " hello there",
  "segments": [
    {"id": 0, "text": " hello", "start": 0.0,  "end": 1.5},
    {"id": 1, "text": " there", "start": 1.5,  "end": 2.6}
  ]
}`

func fakeMode(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "mode"))
	if err != nil {
		return os.Getenv(fakeWhisperEnv)
	}
	return strings.TrimSpace(string(b))
}

func fakeSleep(dir string) time.Duration {
	b, err := os.ReadFile(filepath.Join(dir, "sleep_ms"))
	if err != nil {
		return 100 * time.Millisecond
	}
	var ms int
	_, _ = fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &ms)
	return time.Duration(ms) * time.Millisecond
}

// appendEvent is the child's only channel back to the test. O_APPEND on a
// single line is atomic enough for two goroutines in one process, which is all
// that ever writes here.
var eventMu sync.Mutex

func appendEvent(dir, what string) {
	if dir == "" {
		return
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	f, err := os.OpenFile(filepath.Join(dir, "events"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintln(f, what)
}

// --- the harness the tests use ---------------------------------------------

// fakeRunner is a decode and a resident process that are both under the test's
// control: a shell script standing in for ffmpeg, and the fake above standing
// in for whisper-server.
type fakeRunner struct {
	Dir      string
	FFmpeg   string
	ModelDir string
	Addr     string
	Bin      string
}

func newFakeRunner(t *testing.T) *fakeRunner {
	t.Helper()
	dir := t.TempDir()

	wav := filepath.Join(dir, "decoded.wav")
	writeTestWAV(t, wav, 3*time.Second)

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary, which is the fake whisper-server: %v", err)
	}

	f := &fakeRunner{
		Dir:      dir,
		FFmpeg:   filepath.Join(dir, "fake-ffmpeg"),
		ModelDir: filepath.Join(dir, "models"),
		Addr:     freeAddr(t),
		Bin:      self,
	}

	if err := os.MkdirAll(f.ModelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"small.en", "medium.en"} {
		if err := os.WriteFile(filepath.Join(f.ModelDir, "ggml-"+m+".bin"), []byte("not a model"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.setMode(t, fakeGate)

	// The last argument is ffmpeg's output path, which is also true of the
	// real invocation in transcribe.go.
	write(t, f.FFmpeg, `#!/bin/sh
out=""
for a in "$@"; do out="$a"; done
cp "`+wav+`" "$out"
`)
	return f
}

// setMode changes what the fake resident does next. Read per request, so it
// takes effect on the next one rather than the next process.
func (f *fakeRunner) setMode(t *testing.T, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.Dir, "mode"), []byte(mode), 0o644); err != nil {
		t.Fatal(err)
	}
}

// release lets a waiting fake inference finish.
func (f *fakeRunner) release(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.Dir, "release"), []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// events is everything the child has reported so far, in order.
func (f *fakeRunner) events(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.Dir, "events"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func (f *fakeRunner) countEvents(t *testing.T, prefix string) int {
	t.Helper()
	n := 0
	for _, e := range f.events(t) {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

// resident is the real Resident pointed at the fakes, so the code under test is
// the production one down to the exec call and the HTTP request.
func (f *fakeRunner) resident(t *testing.T, logger *slog.Logger) *Resident {
	t.Helper()
	t.Setenv(fakeWhisperEnv, fakeGate)
	t.Setenv(fakeWhisperDirEnv, f.Dir)
	return &Resident{
		Bin:            f.Bin,
		Addr:           f.Addr,
		ModelDir:       f.ModelDir,
		FFmpegBin:      f.FFmpeg,
		Model:          "small.en",
		Logger:         logger,
		ExpectedRates:  map[string]float64{"small.en": 57.9, "medium.en": 35.9},
		DeadlineFactor: DefaultInferenceDeadlineFactor,
		MinDeadline:    2 * time.Second,
		LoadDeadline:   2 * time.Second,
		StartTimeout:   20 * time.Second,
	}
}

// transcriber is what the API-surface tests want: something that can list
// models. No process is spawned.
func (f *fakeRunner) transcriber() *Resident {
	return &Resident{ModelDir: f.ModelDir, Logger: discardLogger()}
}

// env is what asrd needs to run against these fakes.
func (f *fakeRunner) env(dsn, tokens string, leaseTTL time.Duration) []string {
	return []string{
		"ASR_DATABASE_URL=" + dsn,
		"ASR_CLIENT_TOKENS=" + tokens,
		"ASR_MODEL_DIR=" + f.ModelDir,
		"ASR_WHISPER_SERVER_BIN=" + f.Bin,
		"ASR_WHISPER_SERVER_ADDR=" + f.Addr,
		"ASR_FFMPEG_BIN=" + f.FFmpeg,
		"ASR_LEASE_TTL=" + leaseTTL.String(),
		"ASR_LOG_FORMAT=text",
		"ASR_LOG_LEVEL=debug",
		"ASR_PORT=0",
		fakeWhisperEnv + "=" + fakeGate,
		fakeWhisperDirEnv + "=" + f.Dir,
		"PATH=" + os.Getenv("PATH"),
	}
}

// startResident runs the supervisor for the duration of the test.
func startResident(t *testing.T, r *Resident) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("the resident supervisor did not stop")
		}
	})
	waitFor(t, "the resident process to be ready", 30*time.Second, func() bool {
		return r.State().Up
	})
}

// freeAddr picks a loopback port nothing is using. The child binds it a moment
// later, which is a race in principle and has no other answer: the port has to
// be known before the process that binds it is started.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}
