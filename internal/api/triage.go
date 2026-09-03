package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

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
