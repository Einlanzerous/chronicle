package asr

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/asr/internal/wire"
)

// The HTTP surface, in process. The lease properties are tested against a real
// asrd in worker_test.go; these are about status codes, and a status code is
// part of the contract two codebases are generated from.

func testServer(t *testing.T) (*httptest.Server, *Store) {
	t.Helper()
	s := testStore(t)
	fake := newFakeRunner(t)
	srv := httptest.NewServer(NewRouter(Deps{
		Store:         s,
		Transcriber:   fake.transcriber(),
		Logger:        discardLogger(),
		Tokens:        map[string]string{testToken: "chronicle", testToken + "-two": "catenary"},
		DefaultModel:  "small.en",
		MaxAudioBytes: 1 << 20,
	}))
	t.Cleanup(srv.Close)
	return srv, s
}

// submitReq builds a multipart submission. The audio part carries its own
// Content-Type, which is the thing the 415 path turns on.
func submitReq(t *testing.T, url, token, key, mediaType, sha string, audio []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	spec, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="spec"`},
		"Content-Type":        {"application/json"},
	})
	_ = json.NewEncoder(spec).Encode(wire.JobSpec{AudioSha256: sha})

	part, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="audio"; filename="memo.ogg"`},
		"Content-Type":        {mediaType},
	})
	_, _ = part.Write(audio)
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, url+"/v1/jobs", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req
}

func do(t *testing.T, req *http.Request) (int, []byte) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func get(t *testing.T, url, token string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return do(t, req)
}

// AN UNSUPPORTED AUDIO MEDIA TYPE IS 415, AND THE ACCEPTED SET IS
// DISCOVERABLE. Done-when 7.
//
// Discoverability is half the requirement: a 415 a client cannot enumerate its
// way out of is one somebody debugs by guessing, and the guessing happens in
// the second codebase, later, by someone else.
func TestUnsupportedMediaTypeIsRefusedAndTheSetIsDiscoverable(t *testing.T) {
	srv, _ := testServer(t)

	code, body := do(t, submitReq(t, srv.URL, testToken, "key-mediamediamedia1",
		"application/octet-stream", hash64("m"), []byte("bytes")))
	if code != http.StatusUnsupportedMediaType {
		t.Fatalf("status %d, want 415: %s", code, body)
	}

	code, body = get(t, srv.URL+"/v1/models", testToken)
	if code != http.StatusOK {
		t.Fatalf("GET /v1/models: %d %s", code, body)
	}
	var caps wire.Capabilities
	if err := json.Unmarshal(body, &caps); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(caps.AcceptedMediaTypes, "audio/ogg") ||
		!slices.Contains(caps.AcceptedMediaTypes, "audio/webm") ||
		!slices.Contains(caps.AcceptedMediaTypes, "audio/mp4") {
		t.Fatalf("the advertised set does not cover both clients: %v", caps.AcceptedMediaTypes)
	}
	if !slices.Contains(caps.Models, "small.en") || caps.DefaultModel != "small.en" {
		t.Fatalf("models %v default %q", caps.Models, caps.DefaultModel)
	}
}

// A browser's `audio/webm;codecs=opus` is accepted. The first draft pinned the
// part to one media type; this is the case that would have found it, in
// Catenary, months later.
func TestMediaTypeParametersAreTolerated(t *testing.T) {
	srv, _ := testServer(t)
	code, body := do(t, submitReq(t, srv.URL, testToken, "key-webmwebmwebmwebm",
		"audio/webm;codecs=opus", hash64("w"), []byte("bytes")))
	if code != http.StatusCreated {
		t.Fatalf("status %d, want 201: %s", code, body)
	}
}

// 201 for a new job, 200 for a replay, 409 for a mismatch — the three answers
// §3 specifies, over HTTP rather than through the store.
func TestSubmitStatusCodes(t *testing.T) {
	srv, _ := testServer(t)
	sha := hash64("s")

	code, body := do(t, submitReq(t, srv.URL, testToken, "key-statusstatussta1", "audio/ogg", sha, []byte("a")))
	if code != http.StatusCreated {
		t.Fatalf("new job: %d %s", code, body)
	}
	var first wire.Job
	if err := json.Unmarshal(body, &first); err != nil {
		t.Fatal(err)
	}
	if first.RetryAfterMs == nil || *first.RetryAfterMs <= 0 {
		t.Fatal("a queued job carries no retry_after_ms; the server sets poll pressure, not the client")
	}

	code, body = do(t, submitReq(t, srv.URL, testToken, "key-statusstatussta1", "audio/ogg", sha, []byte("a")))
	if code != http.StatusOK {
		t.Fatalf("replay: %d %s, want 200", code, body)
	}
	var replay wire.Job
	if err := json.Unmarshal(body, &replay); err != nil {
		t.Fatal(err)
	}
	if replay.Id != first.Id {
		t.Fatal("a replay produced a second job")
	}

	code, body = do(t, submitReq(t, srv.URL, testToken, "key-statusstatussta1", "audio/ogg", hash64("OTHER"), []byte("b")))
	if code != http.StatusConflict {
		t.Fatalf("mismatch: %d %s, want 409", code, body)
	}
}

// The key is required, and the error says what to do about it rather than
// merely that something was wrong.
func TestSubmitWithoutAnIdempotencyKeyIsRefused(t *testing.T) {
	srv, _ := testServer(t)
	code, body := do(t, submitReq(t, srv.URL, testToken, "", "audio/ogg", hash64("n"), []byte("a")))
	if code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", code, body)
	}
	if !bytes.Contains(body, []byte("Idempotency-Key")) {
		t.Fatalf("the error does not name the header: %s", body)
	}
}

// No credential reaches nothing, and the two probes stay open.
func TestAuthenticationBoundaries(t *testing.T) {
	srv, _ := testServer(t)

	for _, path := range []string{"/v1/models", "/v1/jobs/00000000-0000-0000-0000-000000000000"} {
		if code, _ := get(t, srv.URL+path, ""); code != http.StatusUnauthorized {
			t.Fatalf("GET %s without a token: %d, want 401", path, code)
		}
		if code, _ := get(t, srv.URL+path, "not-a-real-token-aaaaaaaaaaaaaaaaaaaa"); code != http.StatusUnauthorized {
			t.Fatalf("GET %s with a bad token: %d, want 401", path, code)
		}
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		if code, body := get(t, srv.URL+path, ""); code != http.StatusOK {
			t.Fatalf("GET %s: %d %s, want 200 without a credential", path, code, body)
		}
	}
}

// ANOTHER CLIENT'S JOB IS 404, NOT 403. A 403 confirms the id exists.
func TestAnotherClientsJobIs404OverHTTP(t *testing.T) {
	srv, _ := testServer(t)

	code, body := do(t, submitReq(t, srv.URL, testToken, "key-mineminemineminem", "audio/ogg", hash64("x"), []byte("a")))
	if code != http.StatusCreated {
		t.Fatalf("submit: %d %s", code, body)
	}
	var job wire.Job
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/v1/jobs/" + job.Id.String(), "/v1/jobs/" + job.Id.String() + "/result"} {
		if code, _ := get(t, srv.URL+path, testToken+"-two"); code != http.StatusNotFound {
			t.Fatalf("GET %s as the other client: %d, want 404", path, code)
		}
	}
}

// The result of an unfinished job is 409, not 404 and not an empty 200.
func TestResultBeforeTerminalIs409(t *testing.T) {
	srv, _ := testServer(t)

	code, body := do(t, submitReq(t, srv.URL, testToken, "key-pendingpendingpe1", "audio/ogg", hash64("p"), []byte("a")))
	if code != http.StatusCreated {
		t.Fatalf("submit: %d %s", code, body)
	}
	var job wire.Job
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatal(err)
	}
	if code, _ := get(t, srv.URL+"/v1/jobs/"+job.Id.String()+"/result", testToken); code != http.StatusConflict {
		t.Fatalf("result of a queued job: %d, want 409", code)
	}
}

// A purged result is 410 Gone. The job row is still there, so 404 would be a
// lie, and a client that reads 404 as "transcription failed" marks a memo
// broken that merely aged out.
func TestPurgedResultIs410(t *testing.T) {
	srv, store := testServer(t)
	ctx := context.Background()

	job := runToSuccess(t, store, "key-goneegoneegoneego", "gone")
	if _, err := store.Pool().Exec(ctx,
		`UPDATE jobs SET result_purge_at = now() - interval '1 hour' WHERE id = $1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PurgeResults(ctx); err != nil {
		t.Fatal(err)
	}

	code, body := get(t, srv.URL+"/v1/jobs/"+job.ID.String()+"/result", testToken)
	if code != http.StatusGone {
		t.Fatalf("status %d, want 410: %s", code, body)
	}
	if code, _ := get(t, srv.URL+"/v1/jobs/"+job.ID.String(), testToken); code != http.StatusOK {
		t.Fatal("the job itself stopped being fetchable when its result was purged")
	}
}

// Cancel is idempotent and answers 200 even for a job that already finished.
func TestCancelIsIdempotent(t *testing.T) {
	srv, store := testServer(t)

	job := runToSuccess(t, store, "key-cancelidempotent", "ci")
	for i := 0; i < 3; i++ {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/jobs/"+job.ID.String()+"/cancel", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+testToken)
		code, body := do(t, req)
		if code != http.StatusOK {
			t.Fatalf("cancel %d: status %d, want 200: %s", i, code, body)
		}
		var got wire.Job
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != wire.JobStatusSucceeded {
			t.Fatalf("cancel changed a succeeded job to %q", got.Status)
		}
	}
}

// A model this deployment does not hold is refused at submit rather than
// queued and failed later.
func TestUnknownModelIsRefusedAtSubmit(t *testing.T) {
	srv, _ := testServer(t)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	spec, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="spec"`},
		"Content-Type":        {"application/json"},
	})
	model := "large-v3"
	_ = json.NewEncoder(spec).Encode(wire.JobSpec{AudioSha256: hash64("u"), Model: &model})
	part, _ := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="audio"`},
		"Content-Type":        {"audio/ogg"},
	})
	_, _ = part.Write([]byte("a"))
	_ = mw.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/jobs", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Idempotency-Key", "key-unknownmodelaaa1")

	code, resp := do(t, req)
	if code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", code, resp)
	}
}

// --- readiness reports the device (CHRN-26 §9) -----------------------------

// readyzWith builds a router whose device state is whatever the test says, and
// answers GET /readyz. The probe is open, so there is no token here.
func readyzWith(t *testing.T, device func() ResidentState) (int, wire.Readiness) {
	t.Helper()
	srv := httptest.NewServer(NewRouter(Deps{
		Store:        testStore(t),
		Transcriber:  newFakeRunner(t).transcriber(),
		Logger:       discardLogger(),
		Tokens:       map[string]string{testToken: "chronicle"},
		DefaultModel: "small.en",
		Device:       device,
	}))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var body wire.Readiness
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, body
}

// /readyz reports the resident model. A service whose GPU has gone but whose
// database is fine used to answer ready and accept work forever.
//
// This also settles a debt CHRN-25's review recorded against this ticket: its
// Surface table promised /readyz would report "the GPU lease and queue depth",
// and the shipped handler reported queue depth only — correctly, because there
// was no GPU lease to report until now.
func TestReadinessReportsTheResidentModel(t *testing.T) {
	code, body := readyzWith(t, func() ResidentState {
		return ResidentState{Up: true, Model: "small.en", InferenceElapsed: 1500 * time.Millisecond}
	})
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if body.ResidentModel == nil || *body.ResidentModel != "small.en" {
		t.Fatalf("resident_model %v", body.ResidentModel)
	}
	if body.InferenceRunningMs == nil || *body.InferenceRunningMs != 1500 {
		t.Fatalf("inference_running_ms %v", body.InferenceRunningMs)
	}
}

// AN ABSENT MODEL IS UNREADY. Jobs are still ACCEPTED — the queue is the right
// place for work a service cannot do yet, and rejecting submissions would push
// the retry into two clients — but this process should be out of rotation.
func TestReadinessRefusesWhenTheResidentModelIsAbsent(t *testing.T) {
	code, body := readyzWith(t, func() ResidentState { return ResidentState{Up: false} })
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", code)
	}
	if body.Check == nil || *body.Check != "whisper_server" {
		t.Fatalf("check %v, want whisper_server", body.Check)
	}
}

// A STANDBY IS UNREADY AND NAMES ITSELF. It serves submit and status correctly
// — both are database-only — and cannot transcribe, and "ready" here means the
// latter. Naming the check is what keeps a second asrd that came up during a
// redeploy from reading as a broken one.
func TestReadinessNamesAStandby(t *testing.T) {
	code, body := readyzWith(t, func() ResidentState { return ResidentState{Standby: true} })
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", code)
	}
	if body.Check == nil || *body.Check != "standby" {
		t.Fatalf("check %v, want standby", body.Check)
	}
}

// A process with NO WORKER answers for its database and nothing else. It serves
// the API correctly, so taking it out of rotation for a device it was never
// going to touch would be wrong.
func TestReadinessIgnoresTheDeviceWhenThereIsNoWorker(t *testing.T) {
	code, body := readyzWith(t, nil)
	if code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if body.ResidentModel != nil {
		t.Fatalf("a workerless process reported a resident model: %v", body.ResidentModel)
	}
}
