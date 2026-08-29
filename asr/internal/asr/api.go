package asr

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/asr/internal/wire"
)

// The HTTP surface. asr/openapi.yaml is the contract and this implements it;
// the types on the wire are the GENERATED ones in asr/internal/wire, so a spec
// change the handlers have not caught up with is a compile error rather than a
// field that quietly stops being sent. Chronicle's client generates its own
// copy from the same file (internal/asrclient); the service does not import it,
// because nothing under asr/ imports anything outside it (CHRN-82 §2).

// clientIDKey carries the authenticated client through the request context.
type clientIDKey struct{}

// Deps is what the router needs to serve.
type Deps struct {
	Store       *Store
	Transcriber Transcriber
	Logger      *slog.Logger
	Version     string
	Commit      string

	// Tokens maps a bearer token to the client it names. CLIENT_ID IS DERIVED
	// FROM THE TOKEN and is never read from a body, header or query parameter:
	// CHRN-26 queues per client for fairness, so a client-asserted identity
	// would let either service submit as the other and jump its queue, and the
	// symptom would be one service's backlog starving a memo somebody is
	// waiting on.
	Tokens map[string]string

	DefaultModel  string
	MaxAudioBytes int64

	// Device reports the resident worker's state, and readiness now depends on
	// it: a service whose GPU has gone but whose database is fine used to
	// report ready and accept work forever.
	//
	// nil when this process runs no worker (ASR_WORKER off). Such a process
	// serves submit and status correctly — both are database-only — so
	// answering unready would take a healthy API process out of rotation for a
	// device it was never going to touch.
	Device func() ResidentState
}

type api struct {
	store         *Store
	transcriber   Transcriber
	logger        *slog.Logger
	tokens        map[string]string
	defaultModel  string
	maxAudioBytes int64
	device        func() ResidentState
}

// NewRouter builds the HTTP handler.
//
// The two probes answer different questions and must not be collapsed:
// /healthz is liveness and stays dependency-free, /readyz pings the database
// and reports the queue and the device. They are the only two routes reachable
// without a credential; there is no unauthenticated read surface.
//
// A DEAD whisper-server IS NOT A REASON TO RESTART asrd — asrd is the thing
// that restarts whisper-server — which is why the device shows up in the second
// probe and not the first.
func NewRouter(d Deps) http.Handler {
	a := &api{
		store:         d.Store,
		transcriber:   d.Transcriber,
		logger:        d.Logger,
		tokens:        d.Tokens,
		defaultModel:  d.DefaultModel,
		maxAudioBytes: d.MaxAudioBytes,
		device:        d.Device,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		body := wire.Health{Status: "ok"}
		if d.Version != "" {
			body.Version = &d.Version
		}
		if d.Commit != "" {
			body.Commit = &d.Commit
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := a.store.Ping(ctx); err != nil {
			a.logger.WarnContext(ctx, "readiness probe failed", "check", "database", "error", err)
			check := "database"
			writeJSON(w, http.StatusServiceUnavailable,
				wire.Readiness{Status: wire.Unready, Check: &check})
			return
		}
		body := wire.Readiness{Status: wire.Ready}
		if depth, err := a.store.QueueDepth(ctx); err == nil {
			body.QueueDepth = &depth
		}

		if a.device != nil {
			st := a.device()
			if st.Model != "" {
				body.ResidentModel = &st.Model
			}
			if st.InferenceElapsed > 0 {
				ms := st.InferenceElapsed.Milliseconds()
				body.InferenceRunningMs = &ms
			}
			// A STANDBY IS UNREADY, AND SAYS WHICH. It did not get the device
			// lock, so it holds no model and cannot transcribe — "ready" here
			// means the latter. Naming the check is what keeps a second asrd
			// that came up during a redeploy from reading as a broken one.
			check := ""
			switch {
			case st.Standby:
				check = "standby"
			case !st.Up || st.Model == "":
				check = "whisper_server"
			}
			if check != "" {
				a.logger.WarnContext(ctx, "readiness probe failed", "check", check)
				body.Status = wire.Unready
				body.Check = &check
				writeJSON(w, http.StatusServiceUnavailable, body)
				return
			}
		}
		writeJSON(w, http.StatusOK, body)
	})

	mux.HandleFunc("GET /v1/models", a.requireClient(a.handleModels))
	mux.HandleFunc("POST /v1/jobs", a.requireClient(a.handleSubmit))
	mux.HandleFunc("GET /v1/jobs/{id}", a.requireClient(a.handleGetJob))
	mux.HandleFunc("GET /v1/jobs/{id}/result", a.requireClient(a.handleGetResult))
	mux.HandleFunc("POST /v1/jobs/{id}/cancel", a.requireClient(a.handleCancel))

	return requestLogger(d.Logger, mux)
}

// requireClient resolves the bearer token to a client id, or answers 401.
//
// The comparison is constant-time against every configured token rather than a
// map lookup. There are two clients, so the cost is nothing, and a map lookup
// on a secret is the kind of thing that is fine until the day the token set is
// large and somebody times it.
func (a *api) requireClient(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(presented) == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "a client bearer token is required")
			return
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(presented)))

		var clientID string
		for token, name := range a.tokens {
			want := sha256.Sum256([]byte(token))
			if subtle.ConstantTimeCompare(sum[:], want[:]) == 1 {
				clientID = name
			}
		}
		if clientID == "" {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "unknown client token")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), clientIDKey{}, clientID)))
	}
}

func clientOf(r *http.Request) string {
	id, _ := r.Context().Value(clientIDKey{}).(string)
	return id
}

func (a *api) handleModels(w http.ResponseWriter, r *http.Request) {
	models := a.transcriber.Models()
	if models == nil {
		models = []string{}
	}
	writeJSON(w, http.StatusOK, wire.Capabilities{
		DefaultModel:       a.defaultModel,
		Models:             models,
		AcceptedMediaTypes: AcceptedMediaTypes,
	})
}

// handleSubmit takes a multipart submission and records a job.
func (a *api) handleSubmit(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(key) < 16 || len(key) > 200 {
		writeError(w, http.StatusBadRequest, "idempotency_key_required",
			"Idempotency-Key must be 16-200 characters. Mint it per transcription attempt "+
				"and persist it BEFORE sending, or a retry becomes a second job")
		return
	}

	// Bounded before anything is read. The limit is on the whole body rather
	// than on the audio part alone, which is the only bound that can be
	// enforced before the bytes have arrived.
	r.Body = http.MaxBytesReader(w, r.Body, a.maxAudioBytes+(1<<20))

	spec, audio, mediaType, err := readSubmission(r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			writeError(w, http.StatusRequestEntityTooLarge, "audio_too_large",
				fmt.Sprintf("the submission exceeds %d bytes", a.maxAudioBytes))
		case errors.Is(err, errUnsupportedMedia):
			writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
				fmt.Sprintf("%q is not accepted; GET /v1/models lists the set", mediaType))
		default:
			writeError(w, http.StatusBadRequest, "malformed_submission", err.Error())
		}
		return
	}

	model := a.defaultModel
	if spec.Model != nil && strings.TrimSpace(*spec.Model) != "" {
		model = strings.TrimSpace(*spec.Model)
	}
	// Refused now rather than queued and failed later: a job that cannot
	// possibly succeed should not consume a queue slot and a retry budget.
	if available := a.transcriber.Models(); len(available) > 0 && !slices.Contains(available, model) {
		writeError(w, http.StatusBadRequest, "unknown_model",
			fmt.Sprintf("this deployment does not have %q; GET /v1/models lists what it has", model))
		return
	}

	language := ""
	if spec.Language != nil {
		language = strings.TrimSpace(*spec.Language)
	}

	job, created, err := a.store.Submit(r.Context(), SubmitInput{
		ClientID:       clientOf(r),
		IdempotencyKey: key,
		AudioSHA256:    strings.ToLower(spec.AudioSha256),
		AudioMediaType: mediaType,
		Audio:          audio,
		Model:          model,
		Language:       language,
	})
	switch {
	case errors.Is(err, ErrKeyMismatch):
		writeError(w, http.StatusConflict, "idempotency_key_mismatch",
			"this Idempotency-Key was used for a different spec or different audio. "+
				"Mint a fresh key and retry; the result is a second job, which is correct")
		return
	case err != nil:
		a.serverError(w, r, "submit job", err)
		return
	}

	status := http.StatusOK // a replay: same job, current status, not an error
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, a.wireJob(job))
}

func (a *api) handleGetJob(w http.ResponseWriter, r *http.Request) {
	id, ok := a.jobID(w, r)
	if !ok {
		return
	}
	job, err := a.store.Get(r.Context(), clientOf(r), id)
	if a.handleLookupError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, a.wireJob(job))
}

func (a *api) handleGetResult(w http.ResponseWriter, r *http.Request) {
	id, ok := a.jobID(w, r)
	if !ok {
		return
	}
	res, err := a.store.Result(r.Context(), clientOf(r), id)
	switch {
	case errors.Is(err, ErrNotTerminal):
		writeError(w, http.StatusConflict, "not_finished",
			"the job has not reached a terminal state; poll GET /v1/jobs/{id}")
		return
	case errors.Is(err, ErrResultPurged):
		// 410 and not 404, deliberately. The job row is still here; what
		// expired is the payload. "Result expired" is not "transcription
		// failed" — a client that gets this re-submits with a fresh key.
		writeError(w, http.StatusGone, "result_purged",
			"this job's result payload has aged out. The job is still recorded; "+
				"re-submit the audio with a fresh Idempotency-Key to transcribe it again")
		return
	case a.handleLookupError(w, r, err):
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (a *api) handleCancel(w http.ResponseWriter, r *http.Request) {
	id, ok := a.jobID(w, r)
	if !ok {
		return
	}
	job, err := a.store.Cancel(r.Context(), clientOf(r), id)
	if a.handleLookupError(w, r, err) {
		return
	}
	// 200 in every case, including an already-terminal job. A client that
	// crashed after cancelling and retries must not receive an error for
	// having succeeded.
	writeJSON(w, http.StatusOK, a.wireJob(job))
}

// wireJob renders a job for a client, including the derived `cancelling`
// status and the server-set poll pressure.
func (a *api) wireJob(j Job) wire.Job {
	out := wire.Job{
		Id:        j.ID,
		Status:    j.WireStatus(),
		Model:     j.Model,
		Attempts:  j.Attempts,
		CreatedAt: j.CreatedAt,
		UpdatedAt: j.UpdatedAt,
	}
	if ms := retryAfterMs(j); ms > 0 {
		out.RetryAfterMs = &ms
	}
	return out
}

// retryAfterMs is the SERVER's opinion about poll pressure, which is the whole
// reason it is on the response rather than being a constant in each client. At
// 59.6x resident a three-minute memo is about three seconds of GPU, so the
// honest default is short — but a fixed client-side interval is the thing that
// becomes wrong the first time the queue is long, in two codebases at once.
//
// Absent on a terminal job: there is nothing left to wait for.
func retryAfterMs(j Job) int {
	switch j.Status {
	case StatusQueued:
		return 2000
	case StatusLeased, StatusRunning:
		return 1000
	default:
		return 0
	}
}

func (a *api) jobID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		// A malformed id is not this client's job either, and answering 404
		// keeps one shape for "no such job of yours" rather than teaching a
		// prober the difference between a bad id and someone else's.
		writeError(w, http.StatusNotFound, "not_found", "no such job")
		return uuid.UUID{}, false
	}
	return id, true
}

// handleLookupError maps a store error onto a response, returning true when it
// has written one. ErrNotFound is 404 EVEN FOR ANOTHER CLIENT'S JOB — CHRN-71's
// precedent, for its reason: a 403 confirms the id exists.
func (a *api) handleLookupError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "no such job")
	default:
		a.serverError(w, r, "job lookup", err)
	}
	return true
}

var errUnsupportedMedia = errors.New("asr: unsupported audio media type")

// readSubmission streams the multipart body, which is what keeps a 40-minute
// memo from being held twice — once as the raw part and once decoded.
//
// The parts may arrive in either order. The spec documents `spec` first, and a
// contract that only works when a client happens to order its fields the way
// the example did is a contract with an undocumented rule in it.
func readSubmission(r *http.Request) (spec wire.JobSpec, audio []byte, mediaType string, err error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return spec, nil, "", fmt.Errorf("expected multipart/form-data with a spec part and an audio part: %w", err)
	}

	var haveSpec, haveAudio bool
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return spec, nil, mediaType, err
		}

		switch part.FormName() {
		case "spec":
			if err := json.NewDecoder(part).Decode(&spec); err != nil {
				return spec, nil, mediaType, fmt.Errorf("the spec part is not valid JSON: %w", err)
			}
			haveSpec = true
		case "audio":
			mediaType, _, err = mime.ParseMediaType(part.Header.Get("Content-Type"))
			if err != nil {
				// Parsed rather than compared verbatim: browsers send
				// `audio/webm;codecs=opus`, and a string equality check
				// against the bare type would reject the second client's
				// every upload.
				return spec, nil, part.Header.Get("Content-Type"),
					fmt.Errorf("the audio part has no readable Content-Type: %w", err)
			}
			if !slices.Contains(AcceptedMediaTypes, mediaType) {
				return spec, nil, mediaType, errUnsupportedMedia
			}
			audio, err = io.ReadAll(part)
			if err != nil {
				return spec, nil, mediaType, err
			}
			haveAudio = true
		default:
			// Ignored rather than refused: an unknown part is how a newer
			// client talks to an older deployment, and refusing one makes
			// every additive change to the contract breaking.
			_, _ = io.Copy(io.Discard, part)
		}
	}

	switch {
	case !haveSpec:
		return spec, nil, mediaType, errors.New("no `spec` part")
	case !haveAudio:
		return spec, nil, mediaType, errors.New("no `audio` part")
	case len(audio) == 0:
		return spec, nil, mediaType, errors.New("the audio part is empty")
	case !isSHA256Hex(spec.AudioSha256):
		return spec, nil, mediaType, errors.New("spec.audio_sha256 must be 64 lowercase hex characters")
	}
	return spec, audio, mediaType, nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		if !isDigit && !isLowerHex {
			return false
		}
	}
	return true
}

func (a *api) serverError(w http.ResponseWriter, r *http.Request, what string, err error) {
	a.logger.ErrorContext(r.Context(), "request failed", "op", what, "error", err)
	writeError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, kind, message string) {
	writeJSON(w, code, wire.Error{Code: kind, Message: message})
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLogger emits one structured line per request, in the shape Dozzle and
// Datadog read — Datadog's standard attributes, with `duration` in nanoseconds
// because that is what that field is documented to be.
//
// Note what is absent: no Authorization header, no request body, no query
// string. A client token in a log line is a client token in every aggregator
// the estate has.
func requestLogger(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		switch {
		case r.URL.Path == "/healthz" || r.URL.Path == "/readyz":
			level = slog.LevelDebug
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		logger.LogAttrs(r.Context(), level, "http request",
			slog.String("http.method", r.Method),
			slog.String("http.url", r.URL.Path),
			slog.Int("http.status_code", rec.status),
			slog.Int64("duration", time.Since(start).Nanoseconds()),
		)
	})
}
