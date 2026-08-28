package api

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// Transcription is the slice of the store the transcription report needs.
type Transcription interface {
	TranscriptionStates(ctx context.Context) (map[string]int64, error)
	HeldMemos(ctx context.Context, limit int) ([]store.HeldMemo, error)
}

// GET /admin/transcription answers the half of CHRN-27's Done-when that a test
// cannot: *"a transcription failure leaves the memo in a state a human can see
// and retry."*
//
// A memo in `held` with a reason is a state. It is not one a human can SEE
// while the only way to read it is a psql session on the shared Postgres, and
// a failure nobody can see is one nobody retries — so it accumulates, quietly,
// as a gap in a corpus that otherwise looks healthy.
//
// A read, and only a read. The retry is `chronicle retranscribe`, deliberately
// on the host rather than behind this endpoint: re-running transcription costs
// GPU time on a device three services share, and that is not a thing to expose
// as an unmetered HTTP verb before CHRN-26 has put a lease on it.
type transcriptionReport struct {
	// States counts every memo by state, not only the interesting ones. A
	// report that named only `held` could not distinguish "nothing is wrong"
	// from "nothing is happening", and a pump that has silently stopped looks
	// exactly like a corpus with no failures.
	States map[string]int64 `json:"states"`

	// Pending is what is waiting on transcription right now: captured, queued
	// and transcribing together. One number, because "how far behind is it"
	// is the question being asked.
	Pending int64 `json:"pending"`

	// Held is the count, HeldSample the readable part of it.
	Held       int64        `json:"held"`
	HeldSample []heldReport `json:"held_sample,omitempty"`

	// Enabled reports whether a pump is configured at all. Without it, an
	// operator reading `pending: 812` has no way to tell a backlog from a
	// service that was never pointed at an ASR endpoint.
	Enabled bool `json:"enabled"`
}

type heldReport struct {
	ID         uuid.UUID `json:"id"`
	AuthorID   uuid.UUID `json:"author_id"`
	CapturedAt time.Time `json:"captured_at"`
	Reason     string    `json:"reason"`
	Retry      string    `json:"retry"`
}

func (a *api) handleAdminTranscription(w http.ResponseWriter, r *http.Request) {
	if a.transcription == nil {
		// Same shape the storage report uses for an unconfigured audio store:
		// "not configured here" and "wrong URL" are different facts and a
		// client should be able to tell them apart.
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":  "no store",
			"detail": "transcription reporting needs a database",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	states, err := a.transcription.TranscriptionStates(ctx)
	if err != nil {
		a.serverError(w, r, "transcription states", err)
		return
	}
	held, err := a.transcription.HeldMemos(ctx, listSample)
	if err != nil {
		a.serverError(w, r, "held memos", err)
		return
	}

	report := transcriptionReport{
		States:  states,
		Enabled: a.transcribing,
		Pending: states[store.StateCaptured] + states[store.StateQueued] + states[store.StateTranscribing],
		Held:    states[store.StateHeld],
	}
	for _, h := range held {
		report.HeldSample = append(report.HeldSample, heldReport{
			ID: h.ID, AuthorID: h.AuthorID, CapturedAt: h.CapturedAt, Reason: h.Reason,
			Retry: "chronicle retranscribe --memo " + h.ID.String(),
		})
	}
	writeJSON(w, http.StatusOK, report)
}
