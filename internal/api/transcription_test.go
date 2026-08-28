package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// fakeTranscription stands in for the database half of the report, so the case
// that matters — a held memo carrying a reason — is testable without Postgres.
type fakeTranscription struct {
	states  map[string]int64
	held    []store.HeldMemo
	partial []store.PartialMemo
}

func (f *fakeTranscription) TranscriptionStates(context.Context) (map[string]int64, error) {
	return f.states, nil
}

func (f *fakeTranscription) HeldMemos(context.Context, int) ([]store.HeldMemo, error) {
	return f.held, nil
}

func (f *fakeTranscription) PartialTranscripts(context.Context, int) (int64, []store.PartialMemo, error) {
	return int64(len(f.partial)), f.partial, nil
}

func transcriptionRouter(t *testing.T, tr Transcription, enabled bool) http.Handler {
	t.Helper()
	f := newFakeAccounts()
	owner := person("owner@example.com", true)
	f.byEmail[owner.Email] = owner
	f.sessions["chr_owner"] = owner

	return NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(),
		Version: "test", SecureCookies: true,
		Transcription: tr, Transcribing: enabled,
	})
}

func getTranscription(t *testing.T, h http.Handler, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/transcription", nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A HELD MEMO IS VISIBLE, WITH ITS REASON AND WITH THE COMMAND THAT RETRIES IT.
//
// This is the half of CHRN-27's Done-when a unit test of the pump cannot
// reach: *"a transcription failure leaves the memo in a state a human can see
// and retry."* A `held` row in Postgres is a state; it is not one a human can
// see while the only way to read it is a psql session on the shared database.
func TestTranscriptionReportShowsHeldMemosAndHowToRetry(t *testing.T) {
	id := uuid.New()
	h := transcriptionRouter(t, &fakeTranscription{
		states: map[string]int64{
			store.StateCaptured: 3, store.StateQueued: 1,
			store.StateTranscribing: 2, store.StateTranscribed: 40,
			store.StateHeld: 1,
		},
		held: []store.HeldMemo{{
			ID: id, AuthorID: uuid.New(), CapturedAt: time.Now(),
			Reason: "decode_failed: no audio stream",
		}},
	}, true)

	rec := getTranscription(t, h, "chr_owner")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var got transcriptionReport
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Pending != 6 {
		t.Fatalf("pending = %d; captured + queued + transcribing is the question being asked", got.Pending)
	}
	if got.Held != 1 || len(got.HeldSample) != 1 {
		t.Fatalf("held = %d sample = %d", got.Held, len(got.HeldSample))
	}
	if got.HeldSample[0].Reason != "decode_failed: no audio stream" {
		t.Fatalf("reason = %q; a human has to be able to see WHAT failed", got.HeldSample[0].Reason)
	}
	if got.HeldSample[0].Retry != "chronicle retranscribe --memo "+id.String() {
		t.Fatalf("retry = %q; the report should carry the command, not assume it is known",
			got.HeldSample[0].Retry)
	}
	if !got.Enabled {
		t.Fatal("enabled = false with a pump configured")
	}
}

// A backlog and a Chronicle that was never pointed at an ASR service produce
// the same `pending` count and want completely different remedies. The report
// says which it is.
func TestTranscriptionReportSaysWhenTranscriptionIsOff(t *testing.T) {
	h := transcriptionRouter(t, &fakeTranscription{
		states: map[string]int64{store.StateCaptured: 812},
	}, false)

	var got transcriptionReport
	rec := getTranscription(t, h, "chr_owner")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Fatal("enabled = true with no pump")
	}
	if got.Pending != 812 {
		t.Fatalf("pending = %d", got.Pending)
	}
}

// Owner only, like the storage report. Neither is a read a device session
// should have.
func TestTranscriptionReportNeedsTheOwner(t *testing.T) {
	h := transcriptionRouter(t, &fakeTranscription{states: map[string]int64{}}, true)
	if rec := getTranscription(t, h, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d without a session, want 401", rec.Code)
	}
}

// A PARTIAL MEMO IS OTHERWISE INVISIBLE, and the report is where it stops
// being. It is `transcribed`, so nothing sweeps it; it is not `held`, so
// `chronicle retranscribe` will not release it; and its audio correctly does
// not prune. Each of those is right on its own, and together they make a
// partial memo read as a healthy one.
func TestTranscriptionReportSurfacesPartialTranscripts(t *testing.T) {
	id := uuid.New()
	h := transcriptionRouter(t, &fakeTranscription{
		states:  map[string]int64{store.StateTranscribed: 40},
		partial: []store.PartialMemo{{MemoID: id, Model: "small.en", TranscribedAt: time.Now()}},
	}, true)

	var got transcriptionReport
	rec := getTranscription(t, h, "chr_owner")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Partial != 1 || len(got.PartialSample) != 1 {
		t.Fatalf("partial = %d sample = %d; a memo with only a partial transcript has to be "+
			"findable, or CHRN-28 has nothing to act on", got.Partial, len(got.PartialSample))
	}
	if got.PartialSample[0].MemoID != id {
		t.Fatalf("memo id = %s, want %s", got.PartialSample[0].MemoID, id)
	}
	// And it is NOT counted as pending: it is not waiting on anything.
	if got.Pending != 0 {
		t.Fatalf("pending = %d; a partial memo is transcribed, not pending", got.Pending)
	}
}
