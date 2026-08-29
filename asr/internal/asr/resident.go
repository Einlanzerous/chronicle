package asr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Einlanzerous/chronicle/asr/internal/wire"
)

// Resident is the worker CHRN-26 is about: one `whisper-server` process holding
// the model across jobs, supervised by asrd, spoken to over loopback, and the
// single point of admission to the GPU within this process.
//
// WHY A CHILD PROCESS AND NOT cgo. The image already builds libwhisper, so cgo
// was available and is rejected on two counts. It requires CGO_ENABLED=1, which
// ends the single-static-binary property every service in this estate has. And
// it puts a C decoder's lifetime inside the process serving HTTP: a segfault on
// a malformed file would stop answering /readyz and take the queue's only
// reader down with it. A crash in a child is a restart; a crash in the server
// is an outage — and §8 establishes that decoder crashes are a case this design
// expects to survive, repeatedly.
//
// WHAT RESIDENCY IS WORTH. R3 isolated a fixed per-process initialisation tax
// by transcribing a 1-second clip: 388 ms on Vulkan. It is a constant, not a
// share, so it dominates exactly the memos there are most of — a 5-second voice
// note is 5.6x slower per-invocation, against 1.38x for a 60-second one. The
// "28% on a 60-second memo" figure is the number for the case that needed this
// least.
type Resident struct {
	// Bin is the supervised child; Addr is where it listens. LOOPBACK ONLY.
	// whisper-server has no authentication of any kind, and a second listener
	// on construct_net that transcribes anything sent to it would make
	// ASR_CLIENT_TOKENS decorative.
	Bin  string
	Addr string

	ModelDir  string
	FFmpegBin string

	// Model is what the child starts holding, and what it comes back on after
	// a failed switch.
	Model string

	Logger *slog.Logger

	// ExpectedRates is model -> realtime multiple for THIS device, read for
	// both the deadline and the contention warning.
	ExpectedRates  map[string]float64
	DeadlineFactor float64
	MinDeadline    time.Duration
	LoadDeadline   time.Duration

	// DecodeDeadline bounds ffmpeg. Fixed rather than derived, because the
	// duration it would be derived from is what the decode produces.
	DecodeDeadline time.Duration

	// StartTimeout bounds how long the supervisor waits for /health after
	// spawning, and how long a claimed job waits for a child that is not up.
	StartTimeout time.Duration

	// Gate reports whether this process may hold the device — the advisory
	// lock. A process that cannot get it is a STANDBY: no child, no model, no
	// VRAM. nil means "always", which is what the tests that are not about the
	// lock want.
	Gate func() bool

	initOnce sync.Once

	// gpu is THE GPU LEASE INSIDE THIS PROCESS: a semaphore of one. The
	// advisory lock says which process owns the device; this says which job is
	// on it. Deliberately not the same thing as the job lease, which is a
	// timestamp held for the whole of a job INCLUDING the decode — a decode
	// under this semaphore would serialise ffmpeg behind the GPU.
	gpu    chan struct{}
	client *http.Client

	mu                 sync.Mutex
	proc               *os.Process
	up                 bool
	resident           string
	lastGood           string
	unloadable         map[string]string
	inferenceStart     time.Time
	lastContentionWarn time.Time

	// stoppedBy is why WE stopped the child, read once by the supervisor when
	// it wakes. Empty means it went on its own, which is the only case that
	// should be priced as a fault.
	stoppedBy string
}

// ResidentState is what /readyz reports about the device. A service whose GPU
// has gone but whose database is fine used to report ready and accept work
// forever.
type ResidentState struct {
	// Standby means another process holds the device lock. It is UNREADY and
	// says so by name, so a second asrd that came up during a redeploy is not
	// mistaken for a broken one.
	Standby bool

	// Up is whether the resident process is answering.
	Up bool

	// Model is what it holds. Empty when nothing is loaded, which is what
	// "refuses readiness when the resident model is absent" reads.
	Model string

	// InferenceElapsed is how long the current inference has been running, or
	// zero when the device is idle. A number on a readiness probe is NOT a
	// bound — §7's deadline is the bound — but it is the number an operator
	// asks for first.
	InferenceElapsed time.Duration
}

func (r *Resident) init() {
	r.initOnce.Do(func() {
		r.gpu = make(chan struct{}, 1)
		r.unloadable = make(map[string]string)
		r.client = &http.Client{
			// NO CLIENT TIMEOUT. The bound on an inference is §7's per-job
			// deadline, computed from the audio, and a blanket timeout here
			// would either kill a forty-minute memo or fail to bound a
			// five-second one.
			Transport: &http.Transport{
				DisableCompression: true,
				MaxIdleConns:       2,
			},
		}
	})
}

// Models lists what is on disk, unchanged from CHRN-25: a submit naming a model
// this deployment does not have is a 400 now rather than a job that fails after
// it has been queued.
func (r *Resident) Models() []string { return modelsIn(r.ModelDir) }

// State reports the device for /readyz.
func (r *Resident) State() ResidentState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st := ResidentState{Up: r.up, Model: r.resident}
	if r.Gate != nil && !r.Gate() {
		st.Standby = true
	}
	if !r.inferenceStart.IsZero() {
		st.InferenceElapsed = time.Since(r.inferenceStart)
	}
	return st
}

// Run supervises the child until ctx is cancelled.
//
// A RESIDENT PROCESS NOBODY SUPERVISES is a resident process that dies once and
// turns the service into a queue that fills forever — which looks exactly like
// a busy service.
func (r *Resident) Run(ctx context.Context) error {
	r.init()
	const (
		minBackoff  = 500 * time.Millisecond
		maxBackoff  = 30 * time.Second
		healthyRun  = time.Minute
		standbyPoll = 2 * time.Second
	)
	backoff := minBackoff

	r.Logger.Info("resident worker supervisor started",
		"bin", r.Bin, "addr", r.Addr, "model", r.Model)

	for {
		if ctx.Err() != nil {
			return nil
		}
		if r.Gate != nil && !r.Gate() {
			// Standby. Nothing is spawned, so no model is loaded and no VRAM
			// is held — which per-inference locking would not have given.
			if !sleep(ctx, standbyPoll) {
				return nil
			}
			continue
		}

		model := r.startModel()
		started := time.Now()
		stopped, err := r.runOnce(ctx, model)
		if ctx.Err() != nil {
			return nil
		}
		ran := time.Since(started).Round(time.Millisecond)

		if stopped != "" {
			// WE STOPPED IT, so this is not a fault and must not be priced as
			// one. Cancelling a running job kills the child by design (§8), and
			// against a growing backoff five cancellations inside a minute
			// would leave the sixth restart sleeping sixteen seconds with an
			// idle GPU and a full queue — a user action charged as a crash
			// loop, logged at warn as a fault. Each of these cases is paced by
			// the thing that caused it: a deadline takes at least its own
			// length, a cancellation takes a person, the device lock takes a
			// poll interval.
			backoff = minBackoff
			r.Logger.Info("the resident process was stopped and is being restarted",
				"why", stopped, "model", model, "ran_for", ran.String())
		} else {
			if time.Since(started) >= healthyRun {
				// It ran long enough to have been working. A later crash is a
				// new fault, not a continuing one, so it does not inherit the
				// backoff.
				backoff = minBackoff
			}
			r.Logger.Warn("the resident process exited; restarting",
				"model", model, "ran_for", ran.String(),
				"backoff", backoff.String(), "error", errText(err))
		}
		if !sleep(ctx, backoff) {
			return nil
		}
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// startModel is what the next spawn should hold: THE LAST KNOWN GOOD, never the
// model that just failed.
//
// /load frees the old model before initialising the new one and calls exit(1)
// if that fails (upstream's own TODO, at examples/server/server.cpp:1184-1194 of
// the pinned tree). Restarting with the model that just failed reproduces the
// failure, and the backoff above then produces a restart loop rather than a
// service.
func (r *Resident) startModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lastGood != "" {
		return r.lastGood
	}
	return r.Model
}

// runOnce spawns the child, waits for it to be ready, and blocks until it exits
// — or until the device lock is lost and the GPU is idle, at which point a
// standby has no business holding a model.
func (r *Resident) runOnce(ctx context.Context, model string) (string, error) {
	r.stopReason() // forget any reason left over from the last child
	path := modelPath(r.ModelDir, model)
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("model %q is not at %s: %w", model, path, err)
	}
	host, port, err := net.SplitHostPort(r.Addr)
	if err != nil {
		return "", fmt.Errorf("addr %q: %w", r.Addr, err)
	}

	// THE DECODE PARAMETERS ARE PINNED, and this is the third unmatched knob
	// in this epic after -nt and the load guard. whisper-server defaults to
	// --beam-size -1 (greedy) and --best-of 2, while every CHRN-12 and CHRN-24
	// reference number was measured at whisper-cli's 5 and 5. Left alone, this
	// service would decode GREEDILY — faster, so the throughput Done-when
	// would pass for a reason that is not residency, and a different transcript
	// from the one CHRN-12's model comparison describes.
	//
	// -nlp turns off language probabilities, which the source itself calls an
	// expensive operation and which nothing in Chronicle reads. CHRN-24's
	// numbers were taken with whisper-cli -oj, which does no such detection.
	args := []string{
		"-m", path,
		"--host", host,
		"--port", port,
		"-bs", "5",
		"-bo", "5",
		"-nlp",
	}
	cmd := exec.Command(r.Bin, args...)
	out := &childOutput{logger: r.Logger}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start %s: %w", r.Bin, err)
	}
	r.Logger.Info("resident process started", "pid", cmd.Process.Pid, "model", model, "args", args)

	var pipes sync.WaitGroup
	pipes.Add(2)
	go func() { defer pipes.Done(); out.scan(stdout) }()
	go func() { defer pipes.Done(); out.scan(stderr) }()

	r.mu.Lock()
	r.proc = cmd.Process
	r.mu.Unlock()

	waitCh := make(chan error, 1)
	go func() {
		pipes.Wait()
		waitCh <- cmd.Wait()
	}()

	defer func() {
		r.mu.Lock()
		r.up, r.resident, r.proc = false, "", nil
		r.mu.Unlock()
	}()

	if err := r.waitHealthy(ctx, waitCh); err != nil {
		// Killed with NO reason recorded, deliberately: a child that never
		// became healthy is a fault, and the caller should price it as one.
		r.killProcess("")
		<-waitCh
		return "", err
	}

	// Said at every start rather than once per process: a restart that lands
	// on a software rasteriser is worth hearing about again.
	announceGGMLBackend(r.Logger, out.startup())

	r.mu.Lock()
	r.up, r.resident, r.lastGood = true, model, model
	r.mu.Unlock()
	r.Logger.Info("resident process ready", "model", model, "addr", r.Addr)

	gate := time.NewTicker(time.Second)
	defer gate.Stop()
	for {
		select {
		case err := <-waitCh:
			// The reason is empty unless something in this process killed it,
			// which is what tells a crash from a cancellation.
			return r.stopReason(), err
		case <-ctx.Done():
			r.killProcess("asrd is shutting down")
			<-waitCh
			return r.stopReason(), nil
		case <-gate.C:
			if r.Gate != nil && !r.Gate() && r.idle() {
				// Demoted: another process holds the device now. The
				// in-flight inference is allowed to finish (its job lease is
				// time-based and survives), but an idle standby holding 3 GB
				// of VRAM is not a standby.
				r.Logger.Warn("lost the device lock; stopping the resident process")
				r.killProcess("the device lock was lost")
				<-waitCh
				return r.stopReason(), nil
			}
		}
	}
}

// waitHealthy polls /health until the child answers ok.
//
// /health, and not a sleep: it answers {"status":"ok"} when READY and an error
// while a model is loading, so "started" and "ready to take work" are
// distinguishable without guessing (server.cpp:1205-1213).
//
// ONLY 200 COUNTS, and that is not fussiness. The handler sets 503 for the
// loading state, but the server's set_error_handler (server.cpp:1231-1238)
// rewrites every non-500 error response — so what actually arrives is a
// **404** with the body "File Not Found (/health)". Verified against the pinned
// tree in a container. Code written to wait for a 503 to clear would wait
// forever; code that requires 200 does not care which of them it is.
func (r *Resident) waitHealthy(ctx context.Context, exited <-chan error) error {
	timeout := r.StartTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		select {
		case err := <-exited:
			return fmt.Errorf("the resident process exited before it was ready: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if r.healthy(ctx) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the resident process did not become healthy within %s", timeout)
		}
		if !sleep(ctx, 100*time.Millisecond) {
			return ctx.Err()
		}
	}
}

func (r *Resident) healthy(ctx context.Context) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+r.Addr+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// Transcribe is the whole of a job's work: decode, wait for the device, load
// the model if this job needs a different one, then infer.
//
// The order is the decision's, and the first step is outside the lease on
// purpose — see decodeToWAV.
func (r *Resident) Transcribe(ctx context.Context, req TranscribeRequest) (Transcript, error) {
	r.init()

	dir, err := os.MkdirTemp("", "asr-job-*")
	if err != nil {
		// This service's disk, not the job's fault. See the ReleaseError on
		// the staging write for why that distinction is load-bearing here.
		return Transcript{}, &ReleaseError{Reason: "worker_io", Detail: err.Error()}
	}
	defer func() { _ = os.RemoveAll(dir) }()

	wav, durationMs, err := r.decode(ctx, dir, req)
	if err != nil {
		return Transcript{}, err
	}

	select {
	case r.gpu <- struct{}{}:
	case <-ctx.Done():
		return Transcript{}, ctx.Err()
	}
	defer func() { <-r.gpu }()

	if err := r.waitUp(ctx); err != nil {
		return Transcript{}, err
	}
	if err := r.ensureModel(ctx, req.Model); err != nil {
		return Transcript{}, err
	}
	if req.OnInference != nil {
		if err := req.OnInference(durationMs); err != nil {
			return Transcript{}, err
		}
	}
	return r.inference(ctx, wav, durationMs, req)
}

// decode turns the submitted audio into a WAV under a wall clock.
//
// THE DEADLINE IS §7'S ARGUMENT APPLIED TO THE THIRD BLOCKING CALL, and the
// decision applied it to only two — /inference, then /load. ffmpeg had neither,
// and an ffmpeg that does not exit reproduces exactly the state §7 was written
// to prevent, one step earlier and more quietly: the renewal goroutine ticks,
// the job lease never expires, the reaper never fires, and the single worker
// goroutine is blocked so nothing else is claimed either — while the child is
// up and holding its model, so /readyz still answers ready with a resident
// model. Only queue_depth climbing would say anything at all, and nothing would
// say why.
//
// The bound is fixed and deliberately enormous. The decode runs at roughly 390x
// realtime in this image (154 ms for the 60 s clip, asr/README.md), so a
// forty-minute memo — the longest recording this system is designed around —
// takes about six seconds. Five minutes is fifty times that. It is not a
// performance budget; it is the difference between a hang and forever.
func (r *Resident) decode(ctx context.Context, dir string, req TranscribeRequest) (string, int64, error) {
	deadline := r.DecodeDeadline
	if deadline <= 0 {
		deadline = DefaultDecodeDeadline
	}
	decodeCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	wav, durationMs, err := decodeToWAV(decodeCtx, r.FFmpegBin, dir, req.Audio, req.MediaType)
	if err != nil && ctx.Err() == nil && errors.Is(decodeCtx.Err(), context.DeadlineExceeded) {
		r.Logger.Error("DECODE DEADLINE BREACHED: ffmpeg is wedged, not slow. "+
			"Nothing downstream can see this — the resident process is healthy and idle",
			"model", req.Model, "bytes", len(req.Audio), "deadline", deadline.String())
		return "", 0, &ReleaseError{
			Reason: "decode_deadline",
			Detail: "ffmpeg did not finish within " + deadline.String(),
		}
	}
	return wav, durationMs, err
}

// waitUp gives a claimed job a short grace for a child that is restarting,
// then releases it rather than holding a lease against a process that is not
// coming back soon.
func (r *Resident) waitUp(ctx context.Context) error {
	timeout := r.StartTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		r.mu.Lock()
		up := r.up
		r.mu.Unlock()
		if up {
			return nil
		}
		if time.Now().After(deadline) {
			return &ReleaseError{
				Reason: "worker_down",
				Detail: "the resident process was not available within " + timeout.String(),
			}
		}
		if !sleep(ctx, 100*time.Millisecond) {
			return ctx.Err()
		}
	}
}

// ensureModel makes the resident model the one this job asked for.
//
// DRAIN, THEN SWITCH is the claim's job (§5); by the time a job with another
// model reaches here the queue has already decided the switch is due. What is
// decided HERE is what a failed switch costs, and the answer comes from reading
// /load rather than assuming it: it frees the old model first and exit(1)s if
// the new one will not initialise, so a failed switch is not an error response,
// it is the resident process terminating.
func (r *Resident) ensureModel(ctx context.Context, model string) error {
	if model == "" {
		model = r.Model
	}
	r.mu.Lock()
	current, why := r.resident, r.unloadable[model]
	r.mu.Unlock()

	if current == model {
		return nil
	}
	if why != "" {
		// A model that will not load is a DEPLOYMENT FAULT, not a job to
		// retry: jobs naming it fail with a code rather than being queued
		// forever behind a switch that cannot happen.
		return &FailureError{Code: "model_unloadable", Message: model + ": " + why}
	}

	// ABSENT IS NOT UNLOADABLE, and §4 keeps them apart for a reason: a model
	// that is merely missing becomes fine again the moment somebody mounts it,
	// so this is checked afresh every time rather than remembered. Marking it
	// would mean a fixed mount stayed broken until the process was restarted.
	path := modelPath(r.ModelDir, model)
	if _, err := os.Stat(path); err != nil {
		return &FailureError{Code: "model_not_installed", Message: model + " is not in " + r.ModelDir}
	}

	loadDeadline := r.LoadDeadline
	if loadDeadline <= 0 {
		loadDeadline = DefaultLoadDeadline
	}
	loadCtx, cancel := context.WithTimeout(ctx, loadDeadline)
	defer cancel()

	r.Logger.Info("switching the resident model", "from", current, "to", model)
	started := time.Now()
	status, err := r.load(loadCtx, path)

	switch {
	case err == nil && status == http.StatusOK:
		r.mu.Lock()
		r.resident, r.lastGood = model, model
		r.mu.Unlock()
		r.Logger.Info("resident model switched", "model", model,
			"took", time.Since(started).Round(time.Millisecond).String())
		return nil

	case ctx.Err() != nil:
		return ctx.Err()

	case errors.Is(loadCtx.Err(), context.DeadlineExceeded):
		// A load that never returns is §7's hung child one step earlier: every
		// lease healthy, nothing moving. From outside, a load that hangs and a
		// load that exits are the same fault, so they get the same answer.
		r.markUnloadable(model, "the load did not return within "+loadDeadline.String())
		r.killProcess("a model load wedged")
		return &FailureError{Code: "model_load_timeout", Message: model + ": the resident process wedged loading it"}

	case status == http.StatusBadRequest:
		// The file went away between the stat above and the call. Upstream
		// stores SERVER_STATE_LOADING_MODEL before this check and never resets
		// it on the 400 path (server.cpp:1163-1183), so THE CHILD IS PERMANENTLY
		// UNHEALTHY FROM HERE ON while still serving /inference perfectly well —
		// a readiness that lies. Confirmed against the pinned tree rather than
		// read: after a 400 from /load, /health answers 404 forever and
		// /inference answers correctly. The honest response is a restart on
		// last-known-good.
		//
		// Still ABSENT rather than unloadable, for the reason above: this is
		// the same answer as the stat, reached through a race.
		r.Logger.Warn("the resident process could not find a model this service can see; restarting it",
			"model", model, "path", path)
		r.killProcess("the resident process could not find a model")
		return &FailureError{Code: "model_not_installed", Message: model + " is not on the resident process's filesystem"}

	default:
		// A transport error IS the documented failure: whisper_free then
		// exit(1). The supervisor brings the child back on last-known-good and
		// this model is refused rather than retried, which is what stops a
		// failed switch becoming a restart loop.
		r.markUnloadable(model, "it failed to initialise")
		return &FailureError{Code: "model_unloadable", Message: model + ": the resident process could not initialise it"}
	}
}

// load posts /load and reports the status it got. `model` is a PATH: the
// endpoint takes a filesystem path, and the child reads its own filesystem.
func (r *Resident) load(ctx context.Context, path string) (int, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", path); err != nil {
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+r.Addr+"/load", &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := r.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("load: %s", resp.Status)
	}
	return resp.StatusCode, nil
}

// inference runs the job on the device under a deadline derived from the audio.
//
// THE DEADLINE IS THE ONLY THING THAT CAN SEE A HUNG CHILD. Every other
// mechanism here is built for a worker that DIES. A whisper-server that hangs —
// a GPU stall, a wedged driver — does not die: asrd is alive, its renewal
// goroutine ticks exactly as designed, the job lease never expires, the reaper
// never fires, and the semaphore and the advisory lock are both held. Every
// lease reports healthy and nothing moves, forever. A wall clock is the only
// thing that tells a long job from a stuck one, because from outside they are
// identical.
func (r *Resident) inference(ctx context.Context, wav string, durationMs int64, req TranscribeRequest) (Transcript, error) {
	expected := r.expected(req.Model, durationMs)
	deadline := time.Duration(r.deadlineFactor() * float64(expected))
	if min := r.minDeadline(); deadline < min {
		// Floored so a five-second memo is not tripped by a cold cache.
		deadline = min
	}

	inferCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	r.mu.Lock()
	r.inferenceStart = time.Now()
	r.mu.Unlock()
	started := time.Now()

	tr, err := r.infer(inferCtx, wav, req.Language)
	elapsed := time.Since(started)

	r.mu.Lock()
	r.inferenceStart = time.Time{}
	r.mu.Unlock()

	switch {
	case err == nil:
		r.checkContention(req.Model, elapsed, expected)
		tr.AudioDurationMs = durationMs
		if len(tr.Segments) == 0 && strings.TrimSpace(tr.Text) != "" {
			// The response_format trap, caught rather than shipped. CHRN-25's
			// contract makes an empty segment list VALID — a silent memo
			// legitimately has one — so a corpus with no timing at all would
			// pass every check downstream and nothing would ever complain.
			r.Logger.Error("the resident process returned text with NO SEGMENTS; "+
				"the transcript has no timing and nothing downstream will notice",
				"chars", len(tr.Text), "remedy", "response_format must be verbose_json")
		}
		return tr, nil

	case ctx.Err() != nil:
		// The JOB was cancelled, or the process is shutting down. Dropping the
		// HTTP request does NOT stop the inference: /inference holds
		// whisper_mutex for the whole synchronous call, so it would run to
		// completion holding the device with every job behind it blocked on
		// the mutex. Killing costs ~388 ms of startup plus a model load, and
		// it is the only thing that actually stops the work.
		r.Logger.Info("stopping the resident process to abandon an inference",
			"elapsed", elapsed.Round(time.Millisecond).String(), "cause", ctx.Err())
		r.killProcess("a job was cancelled mid-inference")
		return Transcript{}, ctx.Err()

	case errors.Is(inferCtx.Err(), context.DeadlineExceeded):
		r.Logger.Error("INFERENCE DEADLINE BREACHED: the resident process is wedged, not slow. "+
			"Killing it, because the process holding everything is the healthy one",
			"model", req.Model, "audio_ms", durationMs,
			"expected", expected.Round(time.Millisecond).String(),
			"deadline", deadline.Round(time.Millisecond).String())
		r.killProcess("an inference deadline was breached")
		return Transcript{}, &ReleaseError{
			Reason: "inference_deadline",
			Detail: fmt.Sprintf("no answer within %s for %d ms of audio", deadline, durationMs),
		}

	default:
		var fe *FailureError
		if errors.As(err, &fe) {
			return Transcript{}, fe
		}
		var re *ReleaseError
		if errors.As(err, &re) {
			// Already classified — reading our own decoded file failed, say —
			// and its reason is more precise than the one below.
			return Transcript{}, re
		}
		// A transport error to a child on loopback that this process
		// supervises is the child dying, and never the audio's fault. Failing
		// the job here would permanently fail a memo on one crash.
		return Transcript{}, &ReleaseError{Reason: "worker_crash", Detail: err.Error()}
	}
}

// infer posts one job to /inference and reads verbose_json back.
func (r *Resident) infer(ctx context.Context, wav, language string) (Transcript, error) {
	fields := map[string]string{
		// REQUIRED. The default is `json`, which returns text and no segments
		// at all — see the check in inference().
		"response_format": "verbose_json",
	}
	if language != "" {
		fields["language"] = language
	}

	f, err := os.Open(wav)
	if err != nil {
		return Transcript{}, &ReleaseError{Reason: "worker_io", Detail: err.Error()}
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return Transcript{}, &ReleaseError{Reason: "worker_io", Detail: err.Error()}
	}

	head, tail, contentType, err := multipartAround("file", "audio.wav", fields)
	if err != nil {
		return Transcript{}, err
	}
	body := io.MultiReader(bytes.NewReader(head), f, bytes.NewReader(tail))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+r.Addr+"/inference", body)
	if err != nil {
		return Transcript{}, err
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.ContentLength = int64(len(head)) + info.Size() + int64(len(tail))

	resp, err := r.client.Do(httpReq)
	if err != nil {
		return Transcript{}, err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return Transcript{}, &FailureError{
			Code:    "inference_failed",
			Message: firstLine(string(detail), resp.Status),
		}
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Transcript{}, err
	}
	return parseVerboseJSON(raw)
}

// verboseJSON is the subset of whisper-server's verbose_json this reads.
// Written as its own type rather than map[string]any so that a change in the
// format is a decode error naming the field, not a nil dereference three lines
// later.
type verboseJSON struct {
	Text     string  `json:"text"`
	Duration float64 `json:"duration"`
	Segments []struct {
		Text  string   `json:"text"`
		Start *float64 `json:"start"`
		End   *float64 `json:"end"`
	} `json:"segments"`
}

// parseVerboseJSON turns the response into a Transcript.
//
// ITS TIMESTAMPS ARE FLOAT SECONDS — the server emits t0 * 0.01
// (server.cpp:1089-1090) — where whisper-cli's -oj wrote integer milliseconds.
// The conversion is mechanical and leaving it out would be a corpus of
// transcripts timed a thousand times short.
func parseVerboseJSON(raw []byte) (Transcript, error) {
	var out verboseJSON
	if err := json.Unmarshal(raw, &out); err != nil {
		return Transcript{}, &FailureError{
			Code:    "unreadable_output",
			Message: fmt.Sprintf("the resident process's JSON did not parse: %v", err),
		}
	}

	// Segments and Text are always non-nil, even for a recording with no
	// speech in it. EMPTY IS A VALID RESULT — a memo that is forty seconds of
	// silence has a true and complete answer, and the answer is "no speech".
	tr := Transcript{Segments: []wire.Segment{}}
	var text strings.Builder
	for _, seg := range out.Segments {
		s := wire.Segment{Text: strings.TrimSpace(seg.Text)}
		if seg.Start != nil {
			s.StartMs = int64(math.Round(*seg.Start * 1000))
		}
		if seg.End != nil {
			s.EndMs = int64(math.Round(*seg.End * 1000))
		}
		tr.Segments = append(tr.Segments, s)
		if s.EndMs > tr.CoveredMs {
			tr.CoveredMs = s.EndMs
		}
		text.WriteString(seg.Text)
	}
	tr.Text = strings.TrimSpace(text.String())
	if tr.Text == "" {
		// verbose_json carries the whole text as well as the segments; prefer
		// the segments, and fall back rather than lose a transcript to a
		// difference nobody has met yet.
		tr.Text = strings.TrimSpace(out.Text)
	}
	return tr, nil
}

// expected is how long this job SHOULD take on this device.
//
// The rate is the WORKER'S, not the table's: CHRN-24's numbers describe the
// R9700, and a deadline computed from somebody else's GPU is either a false
// kill or no bound at all. A model this worker has no rate for uses the slowest
// CHRN-24 measured, so an unknown model errs wide.
func (r *Resident) expected(model string, durationMs int64) time.Duration {
	rate := r.ExpectedRates[model]
	if rate <= 0 {
		rate = UnknownModelRate
	}
	return time.Duration(float64(durationMs) / rate * float64(time.Millisecond))
}

func (r *Resident) deadlineFactor() float64 {
	if r.DeadlineFactor < 1 {
		return DefaultInferenceDeadlineFactor
	}
	return r.DeadlineFactor
}

func (r *Resident) minDeadline() time.Duration {
	if r.MinDeadline <= 0 {
		return DefaultMinInferenceDeadline
	}
	return r.MinDeadline
}

// checkContention is ruling 2, and §7's deadline already pays for the
// measurement: the per-job wall clock is taken either way.
//
// It fires at 2x expected — under half the 5x kill threshold — so a deadline
// kill under contention is never the first symptom. §3 gives up ARBITRATING the
// device against Ollama; giving up visibility as well would mean the first
// symptom is a timeout in some other service.
//
// Rate-limited to one line a minute: a backfill of eight hundred jobs under
// sustained contention should say so, not say so eight hundred times.
func (r *Resident) checkContention(model string, elapsed, expected time.Duration) {
	if expected <= 0 || elapsed < 2*expected {
		return
	}
	r.mu.Lock()
	quiet := time.Since(r.lastContentionWarn) < time.Minute
	if !quiet {
		r.lastContentionWarn = time.Now()
	}
	r.mu.Unlock()
	if quiet {
		return
	}
	r.Logger.Warn("inference is running well behind this device's expected rate; "+
		"the likely cause is contention on the GPU. asrd admits one transcription at a "+
		"time but does not arbitrate the device against Ollama (CHRN-26 §3)",
		"model", model,
		"elapsed", elapsed.Round(time.Millisecond).String(),
		"expected", expected.Round(time.Millisecond).String(),
		"ratio", math.Round(float64(elapsed)/float64(expected)*10)/10)
}

func (r *Resident) markUnloadable(model, why string) {
	r.mu.Lock()
	r.unloadable[model] = why
	r.mu.Unlock()
	r.Logger.Error("MODEL MARKED UNLOADABLE; jobs naming it will fail rather than queue",
		"model", model, "why", why)
}

// killProcess is what stops work that a dropped request does not. SIGKILL: the
// cases that reach here are a wedged child and a cancellation, and asking a
// hung process to exit politely is asking the thing that is not responding to
// respond.
func (r *Resident) killProcess(why string) {
	r.mu.Lock()
	proc := r.proc
	r.up = false
	r.stoppedBy = why
	r.mu.Unlock()
	if proc == nil {
		return
	}
	_ = proc.Kill()
}

// stopReason reports why the child stopped and forgets it. Empty means it
// exited on its own.
func (r *Resident) stopReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	why := r.stoppedBy
	r.stoppedBy = ""
	return why
}

func (r *Resident) idle() bool {
	select {
	case r.gpu <- struct{}{}:
		<-r.gpu
		return true
	default:
		return false
	}
}

// childOutput carries the child's stdout and stderr into this service's log,
// and keeps the first few KB so the ggml banner can be read after startup.
type childOutput struct {
	logger *slog.Logger

	mu         sync.Mutex
	startupBuf strings.Builder
}

func (c *childOutput) scan(rd io.Reader) {
	s := bufio.NewScanner(rd)
	s.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for s.Scan() {
		line := s.Text()
		c.mu.Lock()
		if c.startupBuf.Len() < 8<<10 {
			c.startupBuf.WriteString(line)
			c.startupBuf.WriteByte('\n')
		}
		c.mu.Unlock()
		c.logger.Debug("whisper-server", "line", line)
	}
}

func (c *childOutput) startup() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startupBuf.String()
}

// multipartAround builds the bytes that go BEFORE and AFTER a file's contents
// in a multipart body, so the file can be streamed from disk rather than read
// into memory.
//
// A forty-minute memo decodes to about 76 MB of 16 kHz mono s16, and buffering
// that plus a copy of it inside a multipart writer is 150 MB of RSS for a
// service whose whole job is to hand bytes to another process. Content-Length
// is known — head + file + tail — so this is a plain request, not a chunked
// one, which is what the child's HTTP library is happiest reading.
func multipartAround(field, filename string, fields map[string]string) (head, tail []byte, contentType string, err error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			return nil, nil, "", err
		}
	}
	if _, err := w.CreateFormFile(field, filename); err != nil {
		return nil, nil, "", err
	}
	// What Close() would write after a part: the CRLF that ends it, then the
	// closing boundary. Written here because Close would append it to the same
	// buffer the head lives in, and the file's bytes go between the two.
	tail = []byte("\r\n--" + w.Boundary() + "--\r\n")
	return buf.Bytes(), tail, w.FormDataContentType(), nil
}

// sleep waits d, or returns false if ctx ended first.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func errText(err error) string {
	if err == nil {
		return "clean exit"
	}
	return err.Error()
}
