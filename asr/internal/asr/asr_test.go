package asr

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
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

// write drops an executable stand-in on disk. The decode is still a shell
// script: ffmpeg is a one-shot command and a script is the honest fake for one.
// The resident process is not, and its fake lives in fakewhisper_test.go.
func write(t *testing.T, path, body string) {
	t.Helper()
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
