package asr

import (
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Configuration is env-only and ASR_-prefixed, matching the house style
// Chronicle uses — no config files, and every variable named in the error that
// rejects it rather than failing later at connect time.

// DefaultPort. 4011 is the next free slot in the estate's 40xx block; 4009 is
// Chronicle and 4010 is taken.
const DefaultPort = 4011

// Defaults that the decision fixed rather than left to taste.
const (
	// DefaultModel is CHRN-12's default, for CHRN-12's reasoning.
	DefaultModel = "small.en"

	// DefaultResultTTL matches upload.DefaultTTL, the CHRN-20 sweep's window.
	// Nothing depends on the number; matching an existing one beats inventing
	// a second.
	DefaultResultTTL = 7 * 24 * time.Hour

	// DefaultLeaseTTL is deliberately short — shorter than the shortest
	// plausible inference is the wrong direction, so it is long enough that a
	// live worker renews comfortably and short enough that a dead one's job
	// comes back in under a minute. CHRN-26 tunes it against the real worker.
	DefaultLeaseTTL = 30 * time.Second

	// DefaultMaxAudioBytes. A 40-minute memo is about 7 MB at the bitrate the
	// benchmark clip uses; 256 MB is far above anything a voice note produces
	// and far below anything that threatens the process.
	DefaultMaxAudioBytes = 256 << 20

	// DefaultModelSwitchMaxWait is ruling 1, settled at 60 s. It is TWO
	// things and the second is the one a client cares about: the starvation
	// bound for a job naming a non-resident model, and — because a queue can
	// hold two models at once — the fairness bound CHRN-29 publishes to client
	// two. Under mixed models a memo waits this long plus one job, not the
	// ~1 s the single-model case gives.
	DefaultModelSwitchMaxWait = 60 * time.Second

	// DefaultInferenceDeadlineFactor multiplies the expected inference time to
	// get the wall clock after which a job is treated as WEDGED rather than
	// long. Five, floored at DefaultMinInferenceDeadline, per CHRN-26 §7.
	//
	// Wide on purpose: contention with Ollama (§3, not prevented here) must
	// never trip it, and the contention warning fires at 2x — under half the
	// kill threshold — so a deadline kill is never the first symptom.
	DefaultInferenceDeadlineFactor = 5.0

	// DefaultMinInferenceDeadline floors the above, so a five-second memo is
	// not killed by a cold cache.
	DefaultMinInferenceDeadline = 30 * time.Second

	// DefaultLoadDeadline bounds a model switch. The measured cost is 1.9 s
	// for the largest model, so this is wide by thirty times and finite — and
	// finite is the property that matters: a /load that never returns is §7's
	// hung child one step earlier, with every lease reporting healthy.
	DefaultLoadDeadline = 60 * time.Second

	// DefaultDecodeDeadline bounds ffmpeg, which is the third blocking call on
	// the job path and the one the decision did not give a wall clock. The
	// decode runs at roughly 390x realtime in this image, so a forty-minute
	// memo takes about six seconds; this is fifty times that. Not a
	// performance budget — the difference between a hang and forever.
	DefaultDecodeDeadline = 5 * time.Minute

	// UnknownModelRate is the realtime multiple assumed for a model this
	// worker has no measurement for: the SLOWEST CHRN-24 measured, so an
	// unknown model errs wide rather than killing a healthy job.
	UnknownModelRate = 18.3

	// DefaultMaxAttempts is CHRN-28's retry ceiling: how many times a job may
	// lose its claim before it is dead-lettered rather than requeued.
	//
	// Five, because the loop it bounds is cheap and the thing it protects
	// against is unbounded rather than frequent — CHRN-26 §8 establishes that a
	// file which crashes the decoder loops until something stops it, and this
	// is that something.
	DefaultMaxAttempts = 5

	// DefaultMaxAttemptsWedged is the lower ceiling for the two expensive
	// reasons: a job killed by a deadline spent five times its expected run
	// getting nowhere. Two attempts, because the third would cost the same
	// stalled queue as the first two for the same reason.
	DefaultMaxAttemptsWedged = 2

	// DefaultDeviceID names the GPU this process claims. It is what the
	// advisory lock hashes and what lands in leased_by — per DEVICE, never per
	// deployment (CHRN-26 §3 [rev 2]).
	DefaultDeviceID = "r9700"
)

// DefaultExpectedRates is CHRN-24's RESIDENT column, in-container, measured
// under beam search with -bs 5 -bo 5. These describe the R9700; a worker on
// another device has its own and sets ASR_EXPECTED_RATES, because a deadline
// computed from somebody else's GPU is either a false kill or no bound at all.
var DefaultExpectedRates = map[string]float64{
	"base.en":   76.7,
	"small.en":  57.9,
	"medium.en": 35.9,
	"large-v3":  18.3,
}

// AcceptedMediaTypes is the audio the service will take.
//
// It is a SET rather than the single `application/ogg` the first draft pinned,
// which was both the wrong media type for audio-only Ogg (RFC 5334 gives
// audio/ogg) and too narrow to survive the second client: browsers produce
// audio/webm;codecs=opus and iOS produces audio/mp4. ffmpeg is in the image and
// reads all of them, so the narrowness bought nothing.
var AcceptedMediaTypes = []string{
	"audio/ogg",
	"audio/webm",
	"audio/mpeg",
	"audio/mp4",
	"audio/wav",
}

// Config is the process-wide configuration.
type Config struct {
	DatabaseURL   string
	Addr          string
	LogLevel      slog.Level
	LogFormat     string
	ShutdownGrace time.Duration

	// ClientTokens maps a bearer token to the client it identifies. The map is
	// keyed by TOKEN because that is the lookup direction; client_id is the
	// value, and it is the only place a client_id ever comes from.
	//
	// Empty is a BOOT ERROR, never "open". A transcription service that
	// accepts anonymous work is one that any container on construct_net can
	// queue 4 GB of audio into.
	ClientTokens map[string]string

	DefaultModel  string
	ResultTTL     time.Duration
	LeaseTTL      time.Duration
	MaxAudioBytes int64

	// ModelDir holds the ggml-*.bin files. The layout is the CHRN-24 image's:
	// $WHISPER/models, mounted read-only.
	ModelDir string

	// WhisperServerBin is the RESIDENT child asrd supervises, and FFmpegBin
	// decodes into it. Both default to names the CHRN-24 image puts on PATH,
	// which is where asrd runs — shelling out to a sibling binary in the same
	// image, not `docker run` from inside a container, which would need the
	// daemon socket and hand this service the estate.
	//
	// ASR_WHISPER_BIN is gone with the per-invocation placeholder: whisper-cli
	// is no longer on any path asrd takes.
	WhisperServerBin string
	FFmpegBin        string

	// WhisperServerAddr is where the child listens. LOOPBACK, always. It has
	// no authentication of any kind, so a second listener on construct_net
	// that transcribes anything sent to it would make ASR_CLIENT_TOKENS
	// decorative.
	WhisperServerAddr string

	// DeviceID names the GPU this process claims: what the Postgres advisory
	// lock hashes, and what lands in leased_by. A second device is a second
	// value rather than a redesign — CHRN-26 §3 [rev 2], CHRN-80.
	DeviceID string

	// ModelSwitchMaxWait bounds how long a job for a non-resident model waits
	// before it forces a switch. Ruling 1; also the mixed-model fairness bound.
	ModelSwitchMaxWait time.Duration

	// InferenceDeadlineFactor and MinInferenceDeadline turn an audio duration
	// into the wall clock after which a job is wedged rather than long.
	InferenceDeadlineFactor float64
	MinInferenceDeadline    time.Duration

	// MaxAttempts and MaxAttemptsWedged are CHRN-28's ceilings — see
	// CeilingFor for which reasons get which.
	MaxAttempts       int
	MaxAttemptsWedged int

	// ExpectedRates is model -> realtime multiple FOR THIS WORKER'S DEVICE. It
	// is read twice for two findings that cost one measurement between them:
	// the §7 deadline, and ruling 2's contention warning at 2x.
	ExpectedRates map[string]float64

	// Backend is recorded on every result. It describes the image asrd is
	// running in, so it is configuration rather than something to detect: a
	// service that guesses wrong labels a corpus wrong and nothing notices.
	Backend string

	// Worker is false to run the HTTP surface with no worker at all — which is
	// what CHRN-26 will want while it develops the real one alongside.
	Worker bool
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	var c Config
	var err error

	c.DatabaseURL = firstNonEmpty(os.Getenv("ASR_DATABASE_URL"))
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("config: ASR_DATABASE_URL is required")
	}

	port := DefaultPort
	if v := strings.TrimSpace(os.Getenv("ASR_PORT")); v != "" {
		// 0 is allowed and means "ask the OS for a free one" — which the
		// lease tests need, because they run a real asrd alongside whatever
		// else is on the box. The chosen port is in the `listening` log line,
		// so an ephemeral one is observable rather than lost.
		port, err = strconv.Atoi(v)
		if err != nil || port < 0 || port > 65535 {
			return c, fmt.Errorf("config: ASR_PORT %q is not a valid port", v)
		}
	}
	c.Addr = fmt.Sprintf(":%d", port)

	c.LogLevel, err = parseLevel(firstNonEmpty(os.Getenv("ASR_LOG_LEVEL"), "info"))
	if err != nil {
		return c, err
	}
	c.LogFormat = strings.ToLower(firstNonEmpty(os.Getenv("ASR_LOG_FORMAT"), "json"))
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return c, fmt.Errorf("config: ASR_LOG_FORMAT %q is not json or text", c.LogFormat)
	}

	c.ShutdownGrace = 20 * time.Second
	if v := strings.TrimSpace(os.Getenv("ASR_SHUTDOWN_GRACE")); v != "" {
		c.ShutdownGrace, err = time.ParseDuration(v)
		if err != nil || c.ShutdownGrace <= 0 {
			return c, fmt.Errorf("config: ASR_SHUTDOWN_GRACE %q is not a positive duration", v)
		}
	}

	c.ClientTokens, err = parseClientTokens(os.Getenv("ASR_CLIENT_TOKENS"))
	if err != nil {
		return c, err
	}

	c.DefaultModel = firstNonEmpty(os.Getenv("ASR_DEFAULT_MODEL"), DefaultModel)

	c.ResultTTL = DefaultResultTTL
	if v := strings.TrimSpace(os.Getenv("ASR_RESULT_TTL")); v != "" {
		c.ResultTTL, err = time.ParseDuration(v)
		if err != nil || c.ResultTTL <= 0 {
			return c, fmt.Errorf("config: ASR_RESULT_TTL %q is not a positive duration", v)
		}
	}

	c.LeaseTTL = DefaultLeaseTTL
	if v := strings.TrimSpace(os.Getenv("ASR_LEASE_TTL")); v != "" {
		c.LeaseTTL, err = time.ParseDuration(v)
		if err != nil || c.LeaseTTL <= 0 {
			return c, fmt.Errorf("config: ASR_LEASE_TTL %q is not a positive duration", v)
		}
	}

	c.MaxAudioBytes = DefaultMaxAudioBytes
	if v := strings.TrimSpace(os.Getenv("ASR_MAX_AUDIO_BYTES")); v != "" {
		c.MaxAudioBytes, err = strconv.ParseInt(v, 10, 64)
		if err != nil || c.MaxAudioBytes <= 0 {
			return c, fmt.Errorf("config: ASR_MAX_AUDIO_BYTES %q is not a positive integer", v)
		}
	}

	// Absolute or nothing, for the reason CHRONICLE_AUDIO_DIR is: a relative
	// path resolves against the daemon's working directory, which nobody
	// deploying this thinks about.
	c.ModelDir = strings.TrimSpace(firstNonEmpty(os.Getenv("ASR_MODEL_DIR"), "/opt/whisper/models"))
	if !filepath.IsAbs(c.ModelDir) {
		return c, fmt.Errorf("config: ASR_MODEL_DIR %q must be an absolute path", c.ModelDir)
	}

	c.WhisperServerBin = firstNonEmpty(os.Getenv("ASR_WHISPER_SERVER_BIN"), "whisper-server")
	c.WhisperServerAddr = firstNonEmpty(os.Getenv("ASR_WHISPER_SERVER_ADDR"), "127.0.0.1:8081")
	if _, _, err := net.SplitHostPort(c.WhisperServerAddr); err != nil {
		return c, fmt.Errorf("config: ASR_WHISPER_SERVER_ADDR %q is not host:port", c.WhisperServerAddr)
	}
	c.FFmpegBin = firstNonEmpty(os.Getenv("ASR_FFMPEG_BIN"), "ffmpeg")

	c.DeviceID = firstNonEmpty(os.Getenv("ASR_DEVICE_ID"), DefaultDeviceID)

	c.ModelSwitchMaxWait = DefaultModelSwitchMaxWait
	if v := strings.TrimSpace(os.Getenv("ASR_MODEL_SWITCH_MAX_WAIT")); v != "" {
		c.ModelSwitchMaxWait, err = time.ParseDuration(v)
		if err != nil || c.ModelSwitchMaxWait <= 0 {
			return c, fmt.Errorf("config: ASR_MODEL_SWITCH_MAX_WAIT %q is not a positive duration", v)
		}
	}

	c.InferenceDeadlineFactor = DefaultInferenceDeadlineFactor
	if v := strings.TrimSpace(os.Getenv("ASR_INFERENCE_DEADLINE_FACTOR")); v != "" {
		c.InferenceDeadlineFactor, err = strconv.ParseFloat(v, 64)
		if err != nil || c.InferenceDeadlineFactor < 1 {
			// Below 1 is a deadline shorter than the job, which kills healthy
			// work on every run. Refused at boot rather than discovered as a
			// queue that never finishes anything.
			return c, fmt.Errorf("config: ASR_INFERENCE_DEADLINE_FACTOR %q is not a number >= 1", v)
		}
	}
	c.MinInferenceDeadline = DefaultMinInferenceDeadline

	c.ExpectedRates, err = parseExpectedRates(os.Getenv("ASR_EXPECTED_RATES"))
	if err != nil {
		return c, err
	}

	c.MaxAttempts, err = positiveInt("ASR_MAX_ATTEMPTS", DefaultMaxAttempts)
	if err != nil {
		return c, err
	}
	c.MaxAttemptsWedged, err = positiveInt("ASR_MAX_ATTEMPTS_WEDGED", DefaultMaxAttemptsWedged)
	if err != nil {
		return c, err
	}
	c.Backend = firstNonEmpty(os.Getenv("ASR_BACKEND"), "vulkan")

	c.Worker = true
	if v := strings.TrimSpace(os.Getenv("ASR_WORKER")); v != "" {
		c.Worker, err = strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("config: ASR_WORKER %q is not a boolean", v)
		}
	}
	return c, nil
}

// parseClientTokens reads `name:token` pairs, comma or whitespace separated.
//
// Empty is an error rather than a permissive default. There is no ASR_AUTH
// flag for the same reason Chronicle has no CHRONICLE_AUTH: auth is
// unconditional, so there is nothing to leave off by accident.
//
// The error names the variable and NEVER any value it held — a token in a
// startup error is a token in every log aggregator the estate has.
func parseClientTokens(raw string) (map[string]string, error) {
	out := make(map[string]string)
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		name, token, ok := strings.Cut(field, ":")
		name, token = strings.TrimSpace(name), strings.TrimSpace(token)
		if !ok || name == "" || token == "" {
			return nil, fmt.Errorf("config: ASR_CLIENT_TOKENS holds an entry that is not name:token")
		}
		if len(token) < 32 {
			// A short token is a token somebody typed. Refused at boot rather
			// than accepted and left to be discovered by whoever guesses it.
			return nil, fmt.Errorf("config: ASR_CLIENT_TOKENS: the token for %q is shorter than 32 characters", name)
		}
		if _, dup := out[token]; dup {
			return nil, fmt.Errorf("config: ASR_CLIENT_TOKENS: two clients share one token")
		}
		out[token] = name
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("config: ASR_CLIENT_TOKENS is required (name:token pairs, out of Signet). " +
			"There is no anonymous mode: an unauthenticated transcription service is one any " +
			"container on the network can queue work into")
	}
	return out, nil
}

// positiveInt reads an optional count. Zero and negative are refused rather
// than treated as "no ceiling": a ceiling of nothing is the unbounded loop this
// setting exists to close, and it should not be reachable by typing 0.
func positiveInt(name string, def int) (int, error) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("config: %s %q is not a positive integer", name, v)
	}
	return n, nil
}

// parseExpectedRates reads `model=realtime_x` pairs, comma or whitespace
// separated, and falls back to CHRN-24's resident column when unset.
//
// A model this worker has no rate for is NOT an error: it uses
// UnknownModelRate, the slowest CHRN-24 measured, so the §7 deadline errs wide
// rather than killing a healthy job on a model somebody added to the mount and
// not to the environment.
func parseExpectedRates(raw string) (map[string]float64, error) {
	if strings.TrimSpace(raw) == "" {
		return maps.Clone(DefaultExpectedRates), nil
	}
	out := make(map[string]float64)
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		model, rate, ok := strings.Cut(field, "=")
		model, rate = strings.TrimSpace(model), strings.TrimSpace(rate)
		if !ok || model == "" {
			return nil, fmt.Errorf("config: ASR_EXPECTED_RATES holds an entry that is not model=realtime_x")
		}
		x, err := strconv.ParseFloat(rate, 64)
		if err != nil || x <= 0 {
			return nil, fmt.Errorf("config: ASR_EXPECTED_RATES: the rate for %q is not a positive number", model)
		}
		out[model] = x
	}
	return out, nil
}

// Logger builds the process logger this config describes.
func (c Config) Logger(w *os.File) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: ASR_LOG_LEVEL %q is not debug/info/warn/error", s)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
