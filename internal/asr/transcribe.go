package asr

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Einlanzerous/chronicle/internal/asrclient"
)

// The decode, and the seam the worker talks to. The resident implementation is
// in resident.go; CHRN-25's per-invocation CLITranscriber was deleted here by
// CHRN-26 rather than extended, because "claim, shell out, write" is not the
// shape of a worker that holds a model and a device lease.

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

// TranscribeRequest is one job's worth of work.
//
// It is a struct rather than five positional arguments because of the last
// field, which the placeholder had no need of: with a resident worker the GPU
// is a queue, so the moment inference actually STARTS is later than the moment
// the job was claimed, and only the transcriber knows when it arrives.
type TranscribeRequest struct {
	Audio     []byte
	MediaType string
	Model     string
	Language  string

	// OnInference is called once the device lease is held and the model is
	// loaded, immediately before inference begins, with the decoded audio's
	// duration. The worker moves the job `leased` -> `running` there: that edge
	// is the queue for the device made visible, which is what CHRN-25 kept the
	// two states apart for.
	//
	// Returning an error abandons the job before any GPU time is spent — the
	// lease was lost, and whoever holds it now is entitled to finish it.
	OnInference func(audioDurationMs int64) error
}

// Transcriber turns audio into a transcript. An interface so the worker's
// lease behaviour can be tested without a GPU — and it is worth being explicit
// that this is not a fake seam invented for tests: it is what let CHRN-26
// replace the implementation without the worker changing shape.
type Transcriber interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (Transcript, error)
	Models() []string
}

// FailureError is a transcription failure with a code the client can branch on.
// Distinguished from an ordinary error because a `failed` job carries
// {code, message} on the wire and "the message we happened to produce" is not
// a code anything can switch on.
//
// A FailureError FAILS THE JOB. Anything that is a fault of the SERVICE rather
// than of the audio must be a ReleaseError instead.
type FailureError struct {
	Code    string
	Message string
}

func (e *FailureError) Error() string { return e.Code + ": " + e.Message }

// ReleaseError says the job was not finished and nothing about it was the
// job's fault: the resident process died under it, or wedged and was killed.
//
// It exists because the two are opposite outcomes and the placeholder had only
// one. A child that crashes mid-inference produces exactly the same Go error as
// a decode that failed — a connection reset — and treating that as a
// transcription failure would PERMANENTLY FAIL A MEMO THAT NOTHING WAS WRONG
// WITH, on one crash. The worker turns this into Store.Release.
type ReleaseError struct {
	// Reason is short and machine-ish: it is what CHRN-28 will set different
	// ceilings against. A deadline breach costs five times a crash.
	Reason string
	Detail string
}

func (e *ReleaseError) Error() string { return "released: " + e.Reason + ": " + e.Detail }

// modelsIn lists the ggml-*.bin files actually on disk. Reported by GET
// /v1/models so a client discovers rather than hardcodes, and so a submit
// naming a model this deployment does not have is a 400 now rather than a job
// that fails after it has been queued.
func modelsIn(dir string) []string {
	entries, err := os.ReadDir(dir)
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

func modelPath(dir, model string) string {
	return filepath.Join(dir, "ggml-"+model+".bin")
}

// decodeToWAV decodes submitted audio to 16 kHz mono s16 WAV in dir, and
// reports the duration it read from the result's own header.
//
// THE INVOCATION IS BYTE-FOR-BYTE THE BENCHMARK HARNESS'S, and that is not
// tidiness: every reference number in deploy/asr/README.md counts this decode,
// so a flag that moved here would move a published figure with nothing in the
// output to say so.
//
// The decode is in this service because whisper.cpp does not read Opus and
// Chronicle ships no decoder at all — the epic moved it to E3 on 2026-08-27 for
// exactly that reason. It happens OUTSIDE the GPU lease: collapsing the two
// would serialise ffmpeg behind the device, which is a decode that could have
// happened while the previous job was still on it.
//
// Everything derived lands in a caller-owned temp directory that goes away with
// the run. A derived artefact must never be written beside authored bytes, and
// here there are no authored bytes on disk at all: the submitted audio arrived
// over HTTP and lives in the database.
func decodeToWAV(ctx context.Context, ffmpegBin, dir string, audio []byte, mediaType string) (string, int64, error) {
	src := filepath.Join(dir, "in"+extensionFor(mediaType))
	if err := os.WriteFile(src, audio, 0o600); err != nil {
		// A RELEASE, NOT A FAILURE. Staging the bytes is this service's own
		// filesystem work — a full disk, a read-only mount — and none of it is
		// anything the audio did. Failing here would drain the whole queue
		// into `failed` in the time it takes to claim it, and `failed` is
		// terminal: the memos would not come back when the disk did.
		return "", 0, &ReleaseError{Reason: "worker_io", Detail: err.Error()}
	}

	wav := filepath.Join(dir, "in.wav")
	decode := exec.CommandContext(ctx, ffmpegBin,
		"-y", "-hide_banner", "-loglevel", "error",
		"-i", src, "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
	var decodeErr bytes.Buffer
	decode.Stderr = &decodeErr
	if err := decode.Run(); err != nil {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		return "", 0, &FailureError{
			Code:    "decode_failed",
			Message: firstLine(decodeErr.String(), err.Error()),
		}
	}

	durationMs, err := wavDurationMs(wav)
	if err != nil {
		// ffmpeg exited 0 and what it wrote is unreadable, or unreadable BY
		// US. That is this service's own output on this service's own disk, so
		// it releases for the same reason the staging write above does. The
		// audio's own faults are the branch above this one, where ffmpeg said
		// so itself.
		return "", 0, &ReleaseError{Reason: "worker_io", Detail: err.Error()}
	}
	return wav, durationMs, nil
}

// wavDurationMs reads the decoded WAV's own header rather than asking ffprobe.
//
// It walks the chunk list instead of assuming a 44-byte header: ffmpeg writes a
// LIST/INFO chunk under some builds, and a fixed offset would then report a
// duration a few milliseconds long — which is exactly the sort of small,
// plausible number nobody checks.
//
// verbose_json reports a duration too, and this is still the one used: it is
// measured from the bytes the reference numbers count, on the near side of a
// service that might not have answered.
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

// announceGGMLBackend puts ggml's device selection into THIS service's logs.
//
// It is not decoration. The failure CHRN-24 exists to prevent is a backend that
// runs, transcribes correctly, and is well off the pace — a CPU fallback, or a
// build without KHR_cooperative_matrix — and the defining property of that
// failure is that nothing about the output reveals it.
//
// CHRN-25 read these lines off whisper-cli's stderr on the first job. They now
// come off the SUPERVISED CHILD'S STARTUP (§8), which is both earlier and the
// only place they are printed at all once the model is resident: the banner is
// a property of the process, and the process now outlives the job.
func announceGGMLBackend(logger *slog.Logger, stderr string) {
	if logger == nil {
		return
	}
	var device string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ggml_vulkan:") {
			continue
		}
		logger.Info("ggml backend", "line", line)
		if strings.Contains(line, " = ") {
			device = line
		}
	}
	switch {
	case device == "":
		logger.Warn("whisper-server named no Vulkan device; this may be running on the CPU",
			"remedy", "check the render node is passed through (deploy/asr/compose.asr.yml)")
	case strings.Contains(device, "Device type is CPU") || strings.Contains(device, "(llvmpipe)"):
		logger.Warn("THE GPU IS NOT BEING USED: ggml selected a software rasteriser. "+
			"Transcription will be correct and roughly twenty times slower",
			"device", device,
			"remedy", "pass --device /dev/dri/renderD129 and the render group")
	case strings.Contains(device, "matrix cores: none"):
		logger.Warn("the GPU is in use but WITHOUT cooperative-matrix shaders, "+
			"which is the path that makes RDNA4 fast. This is the failure mode "+
			"CHRN-24 pins the LunarG SDK to prevent",
			"device", device)
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
