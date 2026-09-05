package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/triage"
)

// CHRN-33's HTTP surface: the primary screen's two endpoints, and the report
// that says whether anything is stuck behind them.
//
//	GET  /triage/batch?limit=N   the day's untriaged memos, proposals attached
//	POST /triage/accept          apply a set of decisions
//	GET  /admin/triage           the backlog, and link rows split four ways
//
// CHRN-34 adds the escape hatch, and it is THREE VERBS AND NOT A DESTINATION:
//
//	POST /triage/hold            defer a memo's decision
//	POST /triage/release         put it back on the screen
//	GET  /triage/deferred        what has been parked, with an age
//
// Deliberately not items inside /triage/accept. An accept reaches Switchyard
// and advances tier-2 state; a hold writes one tier-1 row and can be undone by
// a DELETE. Behind one handler they would share a body shape, a size cap and a
// failure mode, and the reversible one would inherit the dangerous one's.
//
// BOTH OF THE FIRST TWO ARE requireUser, NOT requireOwner. Every account
// records its own memos and triages its own memos; the owner sees the whole
// corpus. The scoping is applied INSIDE THE SERVICE, per item on the POST as
// well as on the GET, because hiding a memo from a list is not access control —
// a client that names an id directly never went through the list.

// Triage is the batch surface. An interface so the handlers can be tested
// without a database or a Switchyard.
type Triage interface {
	Batch(ctx context.Context, actor store.User, limit int) ([]triage.BatchItem, error)
	Apply(ctx context.Context, actor store.User, items []triage.Item) ([]triage.Result, error)
	Admin(ctx context.Context) (triage.AdminReport, error)

	// CHRN-34.
	Hold(ctx context.Context, actor store.User, memoID uuid.UUID, reason string) (triage.DeferredItem, error)
	Release(ctx context.Context, actor store.User, memoID uuid.UUID) error
	Deferred(ctx context.Context, actor store.User, limit int) ([]triage.DeferredItem, error)
}

// batchResponse is what the screen renders from.
type batchResponse struct {
	Items []triage.BatchItem `json:"items"`

	// Limit is echoed because THE POST IS CAPPED AT IT. A client composing a
	// batch needs to know the cap without a second document to consult, and the
	// server-side clamp means the number it asked for may not be the one it got.
	Limit int `json:"limit"`
}

// acceptRequest is a set of decisions.
//
// ONE FIELD, AND NO VERB. An item accepts the proposal as shown or carries an
// override; append-versus-supersede is CHRN-39's question and CHRN-32's
// contract cannot express it, so there is nothing here to carry it and nothing
// to default wrongly. Unknown fields are REJECTED rather than ignored, so a
// client that invents one is told.
type acceptRequest struct {
	Items []triage.Item `json:"items"`
}

type acceptResponse struct {
	// Results is one per item, IN REQUEST ORDER. There is no batch-wide status
	// and there must not be: the interesting case is item 7 of 12 failing, and
	// a single status could not say which one to re-show.
	Results []triage.Result `json:"results"`
}

// triageUnavailable answers the "not configured here" case, on the shape the
// storage and transcription reports already use: a client can tell it from a
// wrong URL, and an operator is told which variables turn it on.
func (a *api) triageUnavailable(w http.ResponseWriter) bool {
	if a.triage != nil {
		return false
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error": "triage is not configured",
		"detail": "routing needs CHRONICLE_SCRIBE_OLLAMA_URL and CHRONICLE_SCRIBE_MODEL, " +
			"and landing a TICKET needs CHRONICLE_SWITCHYARD_URL and CHRONICLE_SWITCHYARD_TOKEN",
	})
	return true
}

func (a *api) handleTriageBatch(w http.ResponseWriter, r *http.Request) {
	if a.triageUnavailable(w) {
		return
	}
	limit := triage.DefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		// CLAMPED, NOT REFUSED. A client asking for a hundred cards gets
		// twenty-five and is told so by the echoed limit; refusing would make
		// an over-eager client unable to triage anything at all.
		limit = min(n, triage.MaxLimit)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	items, err := a.triage.Batch(ctx, userFrom(r.Context()), limit)
	if err != nil {
		a.serverError(w, r, "triage batch", err)
		return
	}
	writeJSON(w, http.StatusOK, batchResponse{Items: items, Limit: limit})
}

func (a *api) handleTriageAccept(w http.ResponseWriter, r *http.Request) {
	if a.triageUnavailable(w) {
		return
	}

	// The shared decoder, so this endpoint gets the same Content-Type check
	// that closes login CSRF and the same UNKNOWN-FIELD REFUSAL every other
	// JSON route has. The refusal matters more here than anywhere: a misspelled
	// `generation` that decoded to its zero value would turn the echo — the one
	// check standing between an operator and committing a proposal they never
	// saw — into a silent no-check, and the request would look like it worked.
	var req acceptRequest
	if !decodeJSONLimit(w, r, &req, maxAcceptBody) {
		return
	}
	if len(req.Items) == 0 {
		http.Error(w, "a batch needs at least one item", http.StatusBadRequest)
		return
	}

	// NO TIMEOUT ON THIS CONTEXT beyond the client's own. Each item carries its
	// own budget and runs detached from this request precisely so that a
	// deadline here cannot abandon a decision that is already durable — a
	// request-wide timeout would cancel the batch between items, which is safe,
	// and is exactly what the per-item detachment already does more precisely.
	results, err := a.triage.Apply(r.Context(), userFrom(r.Context()), req.Items)
	switch {
	case errors.Is(err, triage.ErrTooLarge), errors.Is(err, triage.ErrBadRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		a.serverError(w, r, "triage accept", err)
		return
	}

	// 200 WITH PER-ITEM RESULTS, ALWAYS — including a batch where every item
	// refused. The request was understood and answered; what failed is twelve
	// separate things, and a 4xx or 5xx here would tell a client to retry a set
	// it has already been given precise answers about.
	writeJSON(w, http.StatusOK, acceptResponse{Results: results})
}

// maxAcceptBody bounds the request. Twenty-five items each carrying an override
// with a ticket description, with room to spare.
const maxAcceptBody = 512 << 10

func (a *api) handleAdminTriage(w http.ResponseWriter, r *http.Request) {
	if a.triageUnavailable(w) {
		return
	}
	// Longer than the other reports allow, because one of its four states is
	// observed by taking row locks rather than by reading a column.
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	rep, err := a.triage.Admin(ctx)
	if err != nil {
		a.serverError(w, r, "triage report", err)
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// holdRequest defers one memo. ONE MEMO AND NOT A SET, which is the difference
// between this and /triage/accept and is not an oversight.
//
// A batch exists because accepting twelve memos is twelve outward calls that
// have to survive item seven failing. Holding is a local write with no partial
// outcome to report, so a batch would buy nothing and would cost the thing that
// makes this endpoint safe: a body a person can read before sending it.
type holdRequest struct {
	MemoID uuid.UUID `json:"memo_id"`

	// Optional. Most deferrals are "not now"; the ones that are not are the
	// ones still legible in three weeks.
	Reason string `json:"reason,omitempty"`
}

type releaseRequest struct {
	MemoID uuid.UUID `json:"memo_id"`
}

// maxHoldBody bounds the request. A memo id and a sentence.
const maxHoldBody = 8 << 10

func (a *api) handleTriageHold(w http.ResponseWriter, r *http.Request) {
	if a.triageUnavailable(w) {
		return
	}
	var req holdRequest
	if !decodeJSONLimit(w, r, &req, maxHoldBody) {
		return
	}
	if req.MemoID == uuid.Nil {
		http.Error(w, "memo_id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	item, err := a.triage.Hold(ctx, userFrom(r.Context()), req.MemoID, req.Reason)
	switch {
	case errors.Is(err, triage.ErrNoSuchMemo):
		// 404 AND NOT 403, for a memo that exists and belongs to someone else.
		// The service already collapses the two; answering 403 here would undo
		// that by letting a caller tell them apart from the status code.
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, store.ErrNotHoldable):
		// 409. The memo is real and the caller may see it; it is simply past
		// the point where deferring means anything — already triaged, or
		// discarded. A retry will not change that, and the message says which
		// state it is in.
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		a.serverError(w, r, "triage hold", err)
		return
	}

	// 200 AND NOT 201, on the second tap as well as the first. Holding is
	// idempotent by construction (store.HoldForTriage keeps the original
	// held_at), so there is no created-versus-existing distinction for a status
	// code to carry — and a client retrying a hold it is unsure landed should
	// not have to tell 201 and 200 apart to know it worked.
	writeJSON(w, http.StatusOK, item)
}

func (a *api) handleTriageRelease(w http.ResponseWriter, r *http.Request) {
	if a.triageUnavailable(w) {
		return
	}
	var req releaseRequest
	if !decodeJSONLimit(w, r, &req, maxHoldBody) {
		return
	}
	if req.MemoID == uuid.Nil {
		http.Error(w, "memo_id is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	err := a.triage.Release(ctx, userFrom(r.Context()), req.MemoID)
	switch {
	case errors.Is(err, triage.ErrNoSuchMemo):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case errors.Is(err, store.ErrNotHeld):
		// 204, NOT 404. Releasing a memo that is not on hold has produced
		// exactly the state the caller asked for, and it is the ordinary
		// outcome of a second tap or a retry. Answering 404 would make a client
		// treat a successful release as a failure — and the memo is already
		// back on the screen, which is the only thing they wanted.
		w.WriteHeader(http.StatusNoContent)
		return
	case err != nil:
		a.serverError(w, r, "triage release", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deferredResponse mirrors batchResponse, echoed limit included, because the
// two listings are read by the same screen and an asymmetry between them is a
// client-side special case for no reason.
type deferredResponse struct {
	Items []triage.DeferredItem `json:"items"`
	Limit int                   `json:"limit"`
}

func (a *api) handleTriageDeferred(w http.ResponseWriter, r *http.Request) {
	if a.triageUnavailable(w) {
		return
	}
	limit := triage.DefaultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit must be a positive integer", http.StatusBadRequest)
			return
		}
		limit = min(n, triage.MaxLimit)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	items, err := a.triage.Deferred(ctx, userFrom(r.Context()), limit)
	if err != nil {
		a.serverError(w, r, "triage deferred", err)
		return
	}
	writeJSON(w, http.StatusOK, deferredResponse{Items: items, Limit: limit})
}
