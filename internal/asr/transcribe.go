package asr

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
)

// Transcript is what one run produced. It deliberately does NOT carry a
// `partial` flag: partial is a fact about whether the RUN completed, which the
// worker knows and the transcriber does not.
type Transcript struct {
	Text            string
	Segments        []asrclient.Segment
	AudioDurationMs int64

	// CoveredMs is the end of the last segment. EVIDENCE, NOT A PREDICATE — see
	// the openapi description and §5 of the decision. It is short of the
	// duration on any recording that ends in silence, which is most of them.
	CoveredMs int64
}

// Transcriber turns audio into a transcript. An interface so the worker's
// lease behaviour can be tested without a GPU — and it is worth being explicit
// that this is not a fake seam invented for tests: CHRN-26 replaces the
// implementation below with a resident process, and the worker should not have
// to change when it does.
type Transcriber interface {
	Transcribe(ctx context.Context, audio []byte, mediaType, model, language string) (Transcript, error)
	Models() []string
}

// CLITranscriber shells out to ffmpeg and whisper-cli, both of which the
// CHRN-24 image puts on PATH.
//
// Shelling out to siblings in the same image, NOT `docker run` from inside a
// container: that would need the daemon socket mounted, which hands this
// service the ability to start anything on the host. The image is the unit of
// deployment; asrd runs inside it.
//
// This is the placeholder CHRN-26 deletes. It is deliberately the shape CHRN-12
// measured as the SLOW one — 43.2x rather than 59.6x, because the model is
// loaded per invocation — and it is single-flight by construction: one worker
// process, one job at a time. The epic's exit criterion is that the R9700 is
// never running two inferences at once, and a placeholder is not exempt from it.
type CLITranscriber struct {
	WhisperBin string
	FFmpegBin  string
	ModelDir   string

	// Logger receives the backend announcement below. Optional; nil is silent.
	Logger *slog.Logger

	announce sync.Once
}

// Models lists the ggml-*.bin files actually on disk. Reported by GET
// /v1/models so a client discovers rather than hardcodes, and so a submit
// naming a model this deployment does not have is a 400 now rather than a job
// that fails after it has been queued.
func (t *CLITranscriber) Models() []string {
	entries, err := os.ReadDir(t.ModelDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "ggml-") || !strings.HasSuffix(name, ".bin") {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.TrimPrefix(name, "ggml-"), ".bin"))
	}
	sort.Strings(out)
	return out
}

func (t *CLITranscriber) modelPath(model string) string {
	return filepath.Join(t.ModelDir, "ggml-"+model+".bin")
}

// Transcribe decodes to 16 kHz mono s16 WAV and runs whisper-cli over it.
//
// The decode is here because whisper.cpp does not read Opus, and Chronicle
// ships no decoder at all — the epic moved the decode to E3 on 2026-08-27 for
// exactly that reason. The ffmpeg invocation matches the benchmark harness's
// byte for byte, so the numbers in deploy/asr/README.md describe this path.
//
// Everything derived lands in a temp directory that goes away with the run. A
// derived artefact must never be written beside authored bytes, and here there
// are no authored bytes on disk at all: the submitted audio arrived over HTTP
// and lives in the database.
func (t *CLITranscriber) Transcribe(ctx context.Context, audio []byte, mediaType, model, language string) (Transcript, error) {
	dir, err := os.MkdirTemp("", "asr-job-*")
	if err != nil {
		return Transcript{}, fmt.Errorf("asr: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	src := filepath.Join(dir, "in"+extensionFor(mediaType))
	if err := os.WriteFile(src, audio, 0o600); err != nil {
		return Transcript{}, fmt.Errorf("asr: stage audio: %w", err)
	}

	wav := filepath.Join(dir, "in.wav")
	decode := exec.CommandContext(ctx, t.FFmpegBin,
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", src, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
	var decodeErr bytes.Buffer
	decode.Stderr = &decodeErr
	if err := decode.Run(); err != nil {
		return Transcript{}, &FailureError{
			Code:    "decode_failed",
			Message: firstLine(decodeErr.String(), err.Error()),
		}
	}

	durationMs, err := wavDurationMs(wav)
	if err != nil {
		return Transcript{}, err
	}

	prefix := filepath.Join(dir, "out")
	args := []string{"-m", t.modelPath(model), "-f", wav, "-oj", "-of", prefix, "-np"}
	if language != "" {
		args = append(args, "-l", language)
	}
	run := exec.CommandContext(ctx, t.WhisperBin, args...)
	var runErr bytes.Buffer
	run.Stderr = &runErr
	if err := run.Run(); err != nil {
		return Transcript{}, &FailureError{
			Code:    "inference_failed",
			Message: firstLine(runErr.String(), err.Error()),
		}
	}

	t.announceBackend(runErr.String())

	raw, err := os.ReadFile(prefix + ".json")
	if err != nil {
		return Transcript{}, &FailureError{
			Code:    "no_output",
			Message: fmt.Sprintf("whisper-cli exited cleanly but wrote no JSON: %v", err),
		}
	}

	tr, err := parseWhisperJSON(raw)
	if err != nil {
		return Transcript{}, err
	}
	tr.AudioDurationMs = durationMs
	return tr, nil
}

// announceBackend puts ggml's device selection into THIS service's logs, once.
//
// It is not decoration. The failure CHRN-24 exists to prevent is a backend that
// runs, transcribes correctly, and is well off the pace — a CPU fallback, or a
// build without KHR_cooperative_matrix — and the defining property of that
// failure is that nothing about the output reveals it. whisper-cli says which
// device it chose on stderr, which this captures anyway and would otherwise
// throw away on every successful run, leaving asrd unable to answer the first
// question anyone asks when transcription is slow.
//
// Once per process: it is the same answer every time, and one line per job
// would bury everything else.
func (t *CLITranscriber) announceBackend(stderr string) {
	if t.Logger == nil {
		return
	}
	t.announce.Do(func() {
		var device string
		for _, line := range strings.Split(stderr, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "ggml_vulkan:") {
				continue
			}
			t.Logger.Info("ggml backend", "line", line)
			if strings.Contains(line, " = ") {
				device = line
			}
		}
		switch {
		case device == "":
			t.Logger.Warn("whisper-cli named no Vulkan device; this may be running on the CPU",
				"remedy", "check the render node is passed through (deploy/asr/compose.asr.yml)")
		case strings.Contains(device, "Device type is CPU") || strings.Contains(device, "(llvmpipe)"):
			t.Logger.Warn("THE GPU IS NOT BEING USED: ggml selected a software rasteriser. "+
				"Transcription will be correct and roughly twenty times slower",
				"device", device,
				"remedy", "pass --device /dev/dri/renderD129 and the render group")
		case strings.Contains(device, "matrix cores: none"):
			t.Logger.Warn("the GPU is in use but WITHOUT cooperative-matrix shaders, "+
				"which is the path that makes RDNA4 fast. This is the failure mode "+
				"CHRN-24 pins the LunarG SDK to prevent",
				"device", device)
		}
	})
}

// FailureError is a transcription failure with a code the client can branch on.
// Distinguished from an ordinary error because a `failed` job carries
// {code, message} on the wire and "the message we happened to produce" is not
// a code anything can switch on.
type FailureError struct {
	Code    string
	Message string
}

func (e *FailureError) Error() string { return e.Code + ": " + e.Message }

// whisperOutput is the subset of whisper.cpp's JSON this reads. Written as its
// own type rather than map[string]any so that a change in that format is a
// decode error naming the field, not a nil dereference three lines later.
type whisperOutput struct {
	Transcription []struct {
		Offsets struct {
			From int64 `json:"from"`
			To   int64 `json:"to"`
		} `json:"offsets"`
		Text string `json:"text"`
	} `json:"transcription"`
}

func parseWhisperJSON(raw []byte) (Transcript, error) {
	var out whisperOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return Transcript{}, &FailureError{
			Code:    "unreadable_output",
			Message: fmt.Sprintf("whisper-cli JSON did not parse: %v", err),
		}
	}

	// Segments and Text are always non-nil, even for a recording with no
	// speech in it. EMPTY IS A VALID RESULT — a memo that is forty seconds of
	// silence has a true and complete answer, and the answer is "no speech".
	// The value of building it here is that no downstream code ever meets a
	// nil transcript it might be tempted to skip.
	tr := Transcript{Segments: []asrclient.Segment{}}
	var text strings.Builder
	for _, seg := range out.Transcription {
		tr.Segments = append(tr.Segments, asrclient.Segment{
			StartMs: seg.Offsets.From,
			EndMs:   seg.Offsets.To,
			Text:    strings.TrimSpace(seg.Text),
		})
		if seg.Offsets.To > tr.CoveredMs {
			tr.CoveredMs = seg.Offsets.To
		}
		text.WriteString(seg.Text)
	}
	tr.Text = strings.TrimSpace(text.String())
	return tr, nil
}

// wavDurationMs reads the decoded WAV's own header rather than asking ffprobe.
//
// It walks the chunk list instead of assuming a 44-byte header: ffmpeg writes a
// LIST/INFO chunk under some builds, and a fixed offset would then report a
// duration a few milliseconds long — which is exactly the sort of small,
// plausible number nobody checks.
func wavDurationMs(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("asr: open decoded wav: %w", err)
	}
	defer func() { _ = f.Close() }()

	var riff [12]byte
	if _, err := f.Read(riff[:]); err != nil {
		return 0, fmt.Errorf("asr: read wav header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, fmt.Errorf("asr: decoded file is not RIFF/WAVE")
	}

	var byteRate uint32
	var hdr [8]byte
	for {
		if _, err := f.Read(hdr[:]); err != nil {
			return 0, fmt.Errorf("asr: decoded wav has no data chunk")
		}
		id := string(hdr[0:4])
		size := binary.LittleEndian.Uint32(hdr[4:8])
		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := f.Read(buf); err != nil || len(buf) < 16 {
				return 0, fmt.Errorf("asr: decoded wav has a truncated fmt chunk")
			}
			byteRate = binary.LittleEndian.Uint32(buf[8:12])
		case "data":
			if byteRate == 0 {
				return 0, fmt.Errorf("asr: decoded wav had data before fmt")
			}
			return int64(size) * 1000 / int64(byteRate), nil
		default:
			// Chunks are word-aligned; an odd size carries a pad byte.
			skip := int64(size) + int64(size%2)
			if _, err := f.Seek(skip, 1); err != nil {
				return 0, fmt.Errorf("asr: read decoded wav: %w", err)
			}
		}
	}
}

// extensionFor gives ffmpeg a filename it can take a hint from. It is a hint
// only — ffmpeg probes the content — but a container it can name in an error
// beats one it cannot.
func extensionFor(mediaType string) string {
	switch mediaType {
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".webm"
	case "audio/mpeg":
		return ".mp3"
	case "audio/mp4":
		return ".m4a"
	case "audio/wav":
		return ".wav"
	default:
		return ".bin"
	}
}

func firstLine(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if i := strings.IndexByte(v, '\n'); i >= 0 {
			v = v[:i]
		}
		return v
	}
	return "no detail"
}
