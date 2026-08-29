package asr

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// No database, no hardware. These cover the two places a wrong number would be
// plausible enough to survive review.

// The duration comes from the decoded WAV's own chunk list, not from a fixed
// 44-byte offset. ffmpeg writes a LIST/INFO chunk under some builds, and a
// fixed offset would then report a duration a few milliseconds long — exactly
// the sort of small, believable number nobody checks.
func TestWavDurationReadsTheChunkList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.wav")
	writeTestWAV(t, path, 2500*time.Millisecond)

	got, err := wavDurationMs(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != 2500 {
		t.Fatalf("duration %d ms, want 2500", got)
	}
}

func TestWavDurationRejectsSomethingElse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.wav")
	if err := writeFile(path, "this is not a RIFF file at all, not even close"); err != nil {
		t.Fatal(err)
	}
	if _, err := wavDurationMs(path); err == nil {
		t.Fatal("a non-WAV file was accepted and given a duration")
	}
}

// verbose_json, and the two facts read out of it.
//
// ITS TIMESTAMPS ARE FLOAT SECONDS. whisper-cli's -oj wrote integer
// milliseconds and the placeholder read them straight; the resident server
// emits t0 * 0.01, so the same fixture in the new format is a thousand times
// smaller. Leaving the conversion out would produce a corpus of transcripts
// timed a thousand times short — every one of them plausible.
//
// The transcript is assembled from segment text rather than from the top-level
// field, and covered_ms is the END OF THE LAST SEGMENT — evidence, never a
// predicate. The fixture deliberately ends before the audio does, which is what
// an ordinary recording with trailing silence looks like.
func TestParseVerboseJSON(t *testing.T) {
	raw := []byte(`{"task":"transcribe","duration":3.0,"text":" ignored",
	  "segments":[
	    {"id":0,"text":" first",  "start":0.0, "end":1.5},
	    {"id":1,"text":" second", "start":1.5, "end":2.6}
	  ]}`)

	tr, err := parseVerboseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "first second" {
		t.Fatalf("text %q", tr.Text)
	}
	if len(tr.Segments) != 2 || tr.Segments[1].StartMs != 1500 || tr.Segments[1].EndMs != 2600 {
		t.Fatalf("segments %+v", tr.Segments)
	}
	if tr.CoveredMs != 2600 {
		t.Fatalf("covered_ms %d, want 2600", tr.CoveredMs)
	}
}

// A RECORDING WITH NO SPEECH PARSES TO AN EMPTY TRANSCRIPT, NOT A NIL ONE.
//
// The distinction is the whole of §5's second-order trap: a nil transcript is
// something a client is tempted to skip, and skipping it strands the audio of
// exactly the memos that should prune.
func TestSilenceParsesToAnEmptyButPresentTranscript(t *testing.T) {
	tr, err := parseVerboseJSON([]byte(`{"duration":40.0,"text":"","segments":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "" {
		t.Fatalf("text %q, want empty", tr.Text)
	}
	if tr.Segments == nil {
		t.Fatal("segments is nil; it must be an empty list, so that nothing downstream " +
			"meets a transcript it might treat as absent")
	}
	if len(tr.Segments) != 0 || tr.CoveredMs != 0 {
		t.Fatalf("segments %+v covered %d", tr.Segments, tr.CoveredMs)
	}
}

// A RESPONSE WITH NO `segments` KEY AT ALL is what response_format=json
// produces, and it is the trap this ticket had to catch: CHRN-25's contract
// makes an empty segment list valid, so such a transcript passes every check
// downstream and quietly carries no timing.
//
// Parsing keeps the text rather than erroring — the honest place to notice is
// the worker, which knows whether the audio was silent — and this pins the
// shape that would arrive.
func TestJSONFormatYieldsTextAndNoSegments(t *testing.T) {
	tr, err := parseVerboseJSON([]byte(`{"text":"hello there"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "hello there" {
		t.Fatalf("text %q", tr.Text)
	}
	if len(tr.Segments) != 0 {
		t.Fatalf("segments %+v; the point of this fixture is that there are none", tr.Segments)
	}
}

func TestUnreadableWhisperOutputIsAFailureWithACode(t *testing.T) {
	_, err := parseVerboseJSON([]byte(`not json`))
	fe, ok := err.(*FailureError)
	if !ok {
		t.Fatalf("got %T, want *FailureError — a client branches on the code, not the message", err)
	}
	if fe.Code != "unreadable_output" {
		t.Fatalf("code %q", fe.Code)
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}

// The backend announcement, which exists because the failure CHRN-24 is built
// around is one that reports success. A CPU fallback transcribes correctly and
// is roughly twenty times slower, and nothing in the transcript says so — so
// the warning has to come from here or from nowhere.
func TestBackendAnnouncementWarnsOnASoftwareRasteriser(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	announceGGMLBackend(log, `ggml_vulkan: Found 1 Vulkan devices:
ggml_vulkan: 0 = llvmpipe (LLVM 20.1.2, 256 bits) (llvmpipe) | uma: 0 | matrix cores: none
whisper_init_with_params_no_state: use gpu = 1`)

	out := buf.String()
	if !strings.Contains(out, "THE GPU IS NOT BEING USED") {
		t.Fatalf("a software rasteriser was not flagged:\n%s", out)
	}
}

// A real device with cooperative-matrix shaders is announced and not warned
// about — a warning that fires on the healthy case is a warning that gets
// filtered out before the day it matters.
func TestBackendAnnouncementIsQuietOnTheRealDevice(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	announceGGMLBackend(log, `ggml_vulkan: Found 1 Vulkan devices:
ggml_vulkan: 0 = AMD Radeon AI PRO R9700 (RADV GFX1201) | uma: 0 | fp16: 1 | matrix cores: KHR_coopmat`)

	out := buf.String()
	if !strings.Contains(out, "R9700") {
		t.Fatalf("the device was not recorded at all:\n%s", out)
	}
	if strings.Contains(out, "level=WARN") {
		t.Fatalf("the healthy case produced a warning:\n%s", out)
	}
}

// Cooperative-matrix shaders missing is its own case: the GPU IS being used,
// so the CPU warning would not fire, and the result is a build that is correct
// and well off the pace — precisely what the pinned LunarG SDK prevents.
func TestBackendAnnouncementWarnsWhenCoopmatIsAbsent(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	announceGGMLBackend(log, `ggml_vulkan: 0 = AMD Radeon AI PRO R9700 (RADV GFX1201) | matrix cores: none`)

	if !strings.Contains(buf.String(), "cooperative-matrix") {
		t.Fatalf("a coopmat-less build was not flagged:\n%s", buf.String())
	}
}

// A child that names no Vulkan device at all is the third failure shape, and
// the quietest: nothing warns, nothing is slow enough to notice on one job, and
// the corpus is transcribed on the CPU for a week.
func TestBackendAnnouncementWarnsWhenNoDeviceIsNamed(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	announceGGMLBackend(log, "whisper_init_with_params_no_state: use gpu = 1")

	if !strings.Contains(buf.String(), "may be running on the CPU") {
		t.Fatalf("a startup that named no device was not flagged:\n%s", buf.String())
	}
}
