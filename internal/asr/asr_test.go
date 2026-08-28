package asr

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CHRN-25's Done-when assertions. The decision they check is in
// docs/decisions/chrn-25-job-contract.md.
//
// Database-backed tests SKIP without ASR_TEST_DATABASE_URL rather than passing
// vacuously, and verify.sh says so at the end of a run. The DSN must name a
// throwaway database: these tests truncate `jobs`.

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ASR_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("ASR_TEST_DATABASE_URL unset")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE jobs`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

func testStore(t *testing.T) *Store {
	t.Helper()
	return New(testPool(t), "test-backend", time.Hour)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// hash64 is a syntactically valid audio_sha256 that differs per input. The
// service does not verify the hash against the bytes — §3 is explicit that a
// client which lies about it gets a job transcribing something else, and that
// is a client bug this service should not pretend to defend against — so a
// test only needs the shape.
func hash64(seed string) string {
	out := make([]byte, 0, 64)
	for len(out) < 64 {
		for _, c := range fmt.Sprintf("%x", seed) {
			if len(out) == 64 {
				break
			}
			out = append(out, byte(c))
		}
		seed += "0"
	}
	return string(out)
}

func submitInput(client, key, seed string) SubmitInput {
	return SubmitInput{
		ClientID:       client,
		IdempotencyKey: key,
		AudioSHA256:    hash64(seed),
		AudioMediaType: "audio/ogg",
		Audio:          []byte("pretend this is opus " + seed),
		Model:          "small.en",
	}
}

// --- the fake runner -------------------------------------------------------
//
// A pair of shell scripts standing in for ffmpeg and whisper-cli, so that
// every lease property can be tested WITHOUT A GPU and in CI. What is being
// tested here is that a crashed process releases its work, not that whisper
// transcribes — CHRN-24's benchmarks are what cover the latter, on hardware.

type fakeRunner struct {
	Dir      string
	FFmpeg   string
	Whisper  string
	ModelDir string

	// releaseFile, once created, lets a waiting fake whisper-cli finish. A
	// fake that merely sleeps would leave the test choosing between a slow run
	// and a flaky one.
	releaseFile string
}

func newFakeRunner(t *testing.T) *fakeRunner {
	t.Helper()
	dir := t.TempDir()

	wav := filepath.Join(dir, "decoded.wav")
	writeTestWAV(t, wav, 3*time.Second)

	f := &fakeRunner{
		Dir:         dir,
		FFmpeg:      filepath.Join(dir, "fake-ffmpeg"),
		Whisper:     filepath.Join(dir, "fake-whisper"),
		ModelDir:    filepath.Join(dir, "models"),
		releaseFile: filepath.Join(dir, "release"),
	}

	if err := os.MkdirAll(f.ModelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.ModelDir, "ggml-small.en.bin"), []byte("not a model"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The last argument is ffmpeg's output path, which is also true of the
	// real invocation in transcribe.go.
	write(t, f.FFmpeg, `#!/bin/sh
out=""
for a in "$@"; do out="$a"; done
cp "`+wav+`" "$out"
`)

	// Waits for the release file, then writes the JSON whisper.cpp would.
	// ASR_FAKE_HANG=1 makes it wait indefinitely, which is what the kill -9
	// and cancel tests need: a job that is genuinely in flight.
	write(t, f.Whisper, `#!/bin/sh
prefix=""
next=0
for a in "$@"; do
  [ "$next" = 1 ] && { prefix="$a"; next=0; }
  [ "$a" = "-of" ] && next=1
done
parent=$PPID
i=0
while [ ! -f "`+f.releaseFile+`" ]; do
  i=$((i+1))
  [ "$i" -gt 240 ] && exit 1
  # Do not outlive asrd. The kill -9 test leaves this script with no parent,
  # and a stray fake that polls for another minute is a stray fake somebody
  # eventually finds in ps and has to explain.
  kill -0 "$parent" 2>/dev/null || exit 1
  sleep 0.25
done
cat > "$prefix.json" <<'JSON'
${ASR_FAKE_JSON}
JSON
`)
	return f
}

// release lets any waiting fake whisper-cli finish.
func (f *fakeRunner) release(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(f.releaseFile, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// transcriber is the CLITranscriber pointed at the fakes, so the code under
// test is the real one down to the exec call.
func (f *fakeRunner) transcriber() *CLITranscriber {
	return &CLITranscriber{WhisperBin: f.Whisper, FFmpegBin: f.FFmpeg, ModelDir: f.ModelDir}
}

// env is what asrd needs to run against these fakes.
func (f *fakeRunner) env(dsn, tokens string, leaseTTL time.Duration) []string {
	return []string{
		"ASR_DATABASE_URL=" + dsn,
		"ASR_CLIENT_TOKENS=" + tokens,
		"ASR_MODEL_DIR=" + f.ModelDir,
		"ASR_WHISPER_BIN=" + f.Whisper,
		"ASR_FFMPEG_BIN=" + f.FFmpeg,
		"ASR_LEASE_TTL=" + leaseTTL.String(),
		"ASR_LOG_FORMAT=text",
		"ASR_LOG_LEVEL=debug",
		"ASR_PORT=0",
		"PATH=" + os.Getenv("PATH"),
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	// The heredoc in the whisper fake reads ${ASR_FAKE_JSON} from the
	// environment, so the script is written verbatim.
	body = strings.Replace(body, "${ASR_FAKE_JSON}", `{"transcription":[
  {"offsets":{"from":0,"to":1500},"text":" hello"},
  {"offsets":{"from":1500,"to":2600},"text":" there"}
]}`, 1)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeTestWAV writes a 16 kHz mono s16 WAV of the given length, so that the
// duration the transcriber reads is a known number rather than whatever a
// fixture happened to be.
func writeTestWAV(t *testing.T, path string, d time.Duration) {
	t.Helper()
	const sampleRate = 16000
	const byteRate = sampleRate * 2
	dataLen := uint32(d.Seconds() * float64(byteRate))

	buf := make([]byte, 0, 44+int(dataLen))
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, 36+dataLen)
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1)          // PCM
	buf = binary.LittleEndian.AppendUint16(buf, 1)          // mono
	buf = binary.LittleEndian.AppendUint32(buf, sampleRate) // sample rate
	buf = binary.LittleEndian.AppendUint32(buf, byteRate)   // byte rate
	buf = binary.LittleEndian.AppendUint16(buf, 2)          // block align
	buf = binary.LittleEndian.AppendUint16(buf, 16)         // bits
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, dataLen)
	buf = append(buf, make([]byte, dataLen)...)

	if err := os.WriteFile(path, buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls until cond is true or the deadline passes. Polling rather than
// sleeping a fixed interval: these tests wait on another process reaching a
// state, and a fixed sleep is either slow or flaky.
func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}
