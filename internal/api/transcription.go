package api

import (
	"context"
	"net/http"
	"time"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Transcription is the slice of the store the transcription report needs.
type Transcription interface {
	TranscriptionStates(ctx context.Context) (map[string]int64, error)
	HeldMemos(ctx context.Context, limit int) ([]store.HeldMemo, error)
	PartialTranscripts(ctx context.Context, limit int) (int64, []store.PartialMemo, error)
}

// GetTranscriptionReport answers the half of CHRN-27's Done-when that a test
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
//
// The payload types are GENERATED from openapi.yaml into internal/api/wire --
// the report, its held sample and its partial sample. What each field is FOR is
// documented there, beside the shape three clients generate against, rather
// than here where only Go could read it.
func (a *api) GetTranscriptionReport(w http.ResponseWriter, r *http.Request) {
	if a.transcription == nil {
		// Same shape the storage report uses for an unconfigured audio store:
		// "not configured here" and "wrong URL" are different facts and a
		// client should be able to tell them apart.
		writeError(w, http.StatusServiceUnavailable, codeTranscriptionUnconfigured,
			"transcription reporting needs a database")
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
	partialCount, partial, err := a.transcription.PartialTranscripts(ctx, listSample)
	if err != nil {
		a.serverError(w, r, "partial transcripts", err)
		return
	}

	report := wire.TranscriptionReport{
		States:  states,
		Enabled: a.transcribing,
		Pending: states[store.StateCaptured] + states[store.StateQueued] + states[store.StateTranscribing],
		Held:    states[store.StateHeld],
		Partial: partialCount,
	}
	for _, p := range partial {
		report.PartialSample = append(report.PartialSample, wire.PartialTranscript{
			MemoId: p.MemoID, Model: p.Model, TranscribedAt: p.TranscribedAt,
		})
	}
	for _, h := range held {
		report.HeldSample = append(report.HeldSample, wire.HeldMemo{
			Id: h.ID, AuthorId: h.AuthorID, CapturedAt: h.CapturedAt, Reason: h.Reason,
			Retry: "chronicle retranscribe --memo " + h.ID.String(),
		})
	}
	writeJSON(w, http.StatusOK, report)
}
