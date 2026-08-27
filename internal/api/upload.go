package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/upload"
)

// CHRN-20 — the direct upload endpoint, resumable, for the Android queue.
//
// The protocol is four calls and one header. `internal/upload` holds the
// argument for its shape; this file is the wire.
//
//	POST   /memos/uploads        declare an upload, or learn it is already held
//	PATCH  /memos/uploads/{id}   append at Upload-Offset
//	GET    /memos/uploads/{id}   how far did it get
//	DELETE /memos/uploads/{id}   give up on it
//
// Every response carries `Upload-Offset`, including the 409 that reports a
// disagreement — which is what makes recovery a header read rather than a
// client-side guess.

// UploadOffsetHeader is how far the server holds. It is spelled the way tus
// spells it because that is the obvious name; see the package comment on
// internal/upload for why this is deliberately not an implementation of tus.
const UploadOffsetHeader = "Upload-Offset"

// uploadBodyType is what a chunk must be sent as.
//
// It is a check with a second job. setSessionCookie's SameSite=Lax is justified
// on the grounds that "every mutating route requires application/json, which a
// cross-site form post cannot produce" — and this route requires something
// else, so that sentence has to keep holding for a different reason. It does,
// twice over: an HTML form can only send urlencoded, multipart or text/plain,
// and it cannot issue a PATCH at all. A cross-origin fetch that could set both
// is a preflighted request, and this service sends no CORS headers.
const uploadBodyType = "application/octet-stream"

// maxFilenameLen bounds the display filename carried through to the memo. It
// reaches a TEXT column, which has no length of its own.
const maxFilenameLen = 255

// hexDigest is 0003's CHECK on content_hash, restated where the value arrives
// from a caller rather than from a row. Same reasoning internal/audio gives for
// carrying its own copy: "the column has a CHECK on it" is not a defence when
// the value is about to name a file.
var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

type uploadOpenRequest struct {
	IdempotencyKey   string `json:"idempotency_key"`
	ContentHash      string `json:"content_hash"`
	ByteSize         int64  `json:"byte_size"`
	Retention        string `json:"retention"`
	OriginalFilename string `json:"original_filename"`
}

// uploadResponse is the one shape all four calls answer with, discriminated by
// Status. One shape rather than two because a client polling a session and a
// client finishing one are the same client, and it should not have to switch
// parsers on a status code.
type uploadResponse struct {
	// Status is "incomplete" — more bytes are expected — or "complete", in
	// which case Memo is set and UploadID is not.
	Status   string `json:"status"`
	UploadID string `json:"upload_id,omitempty"`
	ByteSize int64  `json:"byte_size"`

	// No omitempty: an offset of zero is the answer for a session that has
	// received nothing, and dropping the field there would leave a client
	// unable to tell "nothing yet" from "the server did not say". Same
	// reasoning as the volume figures in the storage report.
	Offset int64 `json:"offset"`

	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	Memo *memoJSON `json:"memo,omitempty"`
	// Duplicate is IngestResult.Collapsed: these bytes were already known.
	// Never inferred from a delivery count — CHRN-18 §10.
	Duplicate bool `json:"duplicate"`
}

// memoJSON is the wire shape of a memo. Deliberately not a path: CHRN-23
// derives one from the row, and publishing it would invite a client to build
// its own.
type memoJSON struct {
	ID          uuid.UUID `json:"id"`
	State       string    `json:"state"`
	Retention   string    `json:"retention"`
	ContentHash string    `json:"content_hash"`
	ByteSize    int64     `json:"byte_size"`
	CapturedAt  time.Time `json:"captured_at"`
	AudioPruned bool      `json:"audio_pruned"`

	// Filled by CHRN-21; null until it has run.
	DurationMS   *int32  `json:"duration_ms"`
	Codec        *string `json:"codec"`
	SampleRateHz *int32  `json:"sample_rate_hz"`

	OriginalFilename *string `json:"original_filename,omitempty"`
}

func toMemoJSON(m store.Memo) memoJSON {
	return memoJSON{
		ID:               m.ID,
		State:            m.State,
		Retention:        m.Retention,
		ContentHash:      m.ContentHash,
		ByteSize:         m.ByteSize,
		CapturedAt:       m.CapturedAt,
		AudioPruned:      m.AudioPruned(),
		DurationMS:       m.DurationMS,
		Codec:            m.Codec,
		SampleRateHz:     m.SampleRateHz,
		OriginalFilename: m.OriginalFilename,
	}
}

// handleUploadOpen declares an upload.
//
// Three answers, and the third is the one that makes re-delivery cheap:
// 201 with a fresh session, 200 with the session this key already has, or 200
// with a memo because the author already holds those bytes and nothing needs
// sending.
func (a *api) handleUploadOpen(w http.ResponseWriter, r *http.Request) {
	if !a.uploadsReady(w) {
		return
	}
	var req uploadOpenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	// Validated here as well as in the store, and not for belt and braces: a
	// declaration is the one thing a client gets wrong on its first
	// integration, and "store: invalid input: ..." is an internal sentence to
	// answer it with. The store's checks stay where they are — they guard every
	// caller, not just this one.
	if l := len(req.IdempotencyKey); l < 16 || l > 200 {
		http.Error(w, "idempotency_key must be 16 to 200 characters", http.StatusBadRequest)
		return
	}
	if !hexDigest.MatchString(req.ContentHash) {
		http.Error(w, "content_hash must be the SHA-256 of the file as 64 lowercase hex characters",
			http.StatusBadRequest)
		return
	}
	if req.ByteSize <= 0 {
		http.Error(w, "byte_size must be a positive number of bytes", http.StatusBadRequest)
		return
	}
	switch req.Retention {
	case "", store.RetentionDiscardNow, store.RetentionDays30, store.RetentionForever:
	default:
		http.Error(w, `retention must be one of "discard_now", "days_30", "forever", or omitted`,
			http.StatusBadRequest)
		return
	}
	if !checkLen(w, "original_filename", req.OriginalFilename, maxFilenameLen) {
		return
	}

	res, err := a.uploads.Open(r.Context(), upload.OpenRequest{
		AuthorID:         userFrom(r.Context()).ID,
		IdempotencyKey:   req.IdempotencyKey,
		ContentHash:      req.ContentHash,
		ByteSize:         req.ByteSize,
		Retention:        req.Retention,
		OriginalFilename: req.OriginalFilename,
	})
	if err != nil {
		a.uploadError(w, r, "open upload", err)
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	a.writeUpload(w, status, res)
}

// handleUploadAppend takes the next chunk.
func (a *api) handleUploadAppend(w http.ResponseWriter, r *http.Request) {
	if !a.uploadsReady(w) {
		return
	}
	u, ok := a.findUpload(w, r)
	if !ok {
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != uploadBodyType {
		http.Error(w, "Content-Type must be "+uploadBodyType, http.StatusUnsupportedMediaType)
		return
	}
	// A chunk must declare its length. Go reports -1 for a chunked body, and
	// without a length there is no way to refuse an oversized chunk before
	// reading it — nor for the reader to end at a point the server knows, which
	// is what internal/upload's oversend check relies on not to wait forever on
	// a client that has stopped sending.
	if r.ContentLength < 0 {
		http.Error(w, "a chunk must carry a Content-Length; chunked bodies are not accepted",
			http.StatusLengthRequired)
		return
	}

	raw := r.Header.Get(UploadOffsetHeader)
	if raw == "" {
		http.Error(w, UploadOffsetHeader+" is required", http.StatusBadRequest)
		return
	}
	offset, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || offset < 0 {
		http.Error(w, UploadOffsetHeader+" must be a non-negative integer", http.StatusBadRequest)
		return
	}

	// Refused before a byte is read, where the header makes it free to see. The
	// service enforces the same bound over the reader itself — this is the
	// cheap case, not the guarantee, and a wrong offset still resolves as a 409
	// there because the offset is checked before anything is read either way.
	if remaining := u.ByteSize - offset; r.ContentLength > remaining {
		http.Error(w, "this chunk is longer than the upload has left to receive",
			http.StatusUnprocessableEntity)
		return
	}

	res, err := a.uploads.Append(r.Context(), u, offset, r.Body)
	if err != nil {
		a.uploadError(w, r, "append to upload", err)
		return
	}
	a.writeUpload(w, http.StatusOK, res)
}

// handleUploadStatus reports how far a session got. It is how a client that
// crashed mid-chunk finds out where to resume without guessing.
func (a *api) handleUploadStatus(w http.ResponseWriter, r *http.Request) {
	if !a.uploadsReady(w) {
		return
	}
	u, ok := a.findUpload(w, r)
	if !ok {
		return
	}
	res, err := a.uploads.Status(r.Context(), u)
	if err != nil {
		a.uploadError(w, r, "read upload status", err)
		return
	}
	a.writeUpload(w, http.StatusOK, res)
}

// handleUploadAbandon drops a session and its bytes.
func (a *api) handleUploadAbandon(w http.ResponseWriter, r *http.Request) {
	if !a.uploadsReady(w) {
		return
	}
	u, ok := a.findUpload(w, r)
	if !ok {
		return
	}
	if err := a.uploads.Abandon(r.Context(), u); err != nil {
		a.uploadError(w, r, "abandon upload", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// uploadsReady answers 503 naming the variable when there is nowhere to put a
// recording, rather than 404 — "not configured here" and "wrong URL" are
// different facts and a client should be able to tell them apart. Same shape
// the storage report uses.
func (a *api) uploadsReady(w http.ResponseWriter) bool {
	if a.uploads == nil {
		http.Error(w, "uploads are not configured: set CHRONICLE_AUDIO_DIR", http.StatusServiceUnavailable)
		return false
	}
	return true
}

// findUpload resolves {id} to this caller's session.
//
// A session belonging to somebody else is 404 and not 403. 403 would confirm
// that the id names a real upload, which is a fact about another account's
// activity that no caller is owed.
func (a *api) findUpload(w http.ResponseWriter, r *http.Request) (store.Upload, bool) {
	id, ok := pathUUID(w, r, "id", "upload")
	if !ok {
		return store.Upload{}, false
	}
	u, err := a.uploads.Find(r.Context(), id, userFrom(r.Context()).ID)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "no such upload", http.StatusNotFound)
		return store.Upload{}, false
	}
	if err != nil {
		a.serverError(w, r, "find upload", err)
		return store.Upload{}, false
	}
	return u, true
}

// writeUpload renders either half of a Result, with the offset in a header as
// well as the body so a client can act on it without parsing anything.
func (a *api) writeUpload(w http.ResponseWriter, status int, res upload.Result) {
	body := uploadResponse{}
	switch {
	case res.Committed != nil:
		m := toMemoJSON(res.Committed.Memo)
		body.Status = "complete"
		body.ByteSize = res.Committed.Memo.ByteSize
		body.Offset = res.Committed.Memo.ByteSize
		body.Memo = &m
		body.Duplicate = res.Committed.Collapsed
	case res.Session != nil:
		body.Status = "incomplete"
		body.UploadID = res.Session.ID.String()
		body.ByteSize = res.Session.ByteSize
		body.Offset = res.Session.Offset
		expires := res.Session.ExpiresAt
		body.ExpiresAt = &expires
	default:
		// Not reachable: Open, Append and Status each set exactly one. Answered
		// rather than left to render as an empty object, because a client
		// parsing `{"status":""}` would be debugging its own code.
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set(UploadOffsetHeader, strconv.FormatInt(body.Offset, 10))
	writeJSON(w, status, body)
}

// uploadError maps the service's refusals onto status codes.
//
// Everything here is a client-visible answer with a specific remedy, which is
// why they are enumerated rather than collapsed into 400: a phone deciding
// whether to resend, wait, or give up needs to tell "we disagree about the
// offset" from "those were the wrong bytes".
func (a *api) uploadError(w http.ResponseWriter, r *http.Request, what string, err error) {
	var conflict *upload.OffsetConflict
	var cut *upload.TransferCut
	switch {
	case errors.As(err, &conflict):
		// The whole point of the 409: it carries where to resume from.
		w.Header().Set(UploadOffsetHeader, strconv.FormatInt(conflict.Offset, 10))
		writeJSON(w, http.StatusConflict, uploadResponse{
			Status: "incomplete",
			Offset: conflict.Offset,
		})
	case errors.Is(err, store.ErrUploadKeyReused):
		http.Error(w, "that idempotency_key is already in use for different content; mint a new one",
			http.StatusConflict)
	case errors.Is(err, upload.ErrTooLarge):
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
	case errors.Is(err, upload.ErrTooManyOpen):
		http.Error(w, "too many uploads already open; finish or abandon one first",
			http.StatusTooManyRequests)
	case errors.Is(err, upload.ErrHashMismatch):
		http.Error(w, "the bytes received do not match content_hash; the upload has been discarded",
			http.StatusUnprocessableEntity)
	case errors.Is(err, upload.ErrOversend):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.As(err, &cut):
		// A dropped connection is the ordinary event this endpoint was built
		// for, and it must not answer like a fault. On the default branch it
		// reached a.serverError, which logs `request failed` at ERROR and
		// answers 500 — so every phone that loses signal mid-chunk emitted a
		// line indistinguishable from a real one, in the log CLAUDE.md wants
		// Dozzle and Datadog to read.
		//
		// 408 says what actually happened, and carries the new offset because
		// the bytes that landed were kept: this is a resume instruction.
		// requestLogger classifies a 4xx as a warning on its own, so correcting
		// the status is the whole of the fix — no special-cased log line.
		w.Header().Set(UploadOffsetHeader, strconv.FormatInt(cut.Offset, 10))
		writeJSON(w, http.StatusRequestTimeout, uploadResponse{
			Status: "incomplete",
			Offset: cut.Offset,
		})
	case errors.Is(err, store.ErrKeyReused):
		http.Error(w, "that idempotency_key already produced a different memo; mint a new one",
			http.StatusConflict)
	case errors.Is(err, store.ErrInvalidInput):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, upload.ErrStagingLost):
		// The declaration still stands and the session is left alone, so the
		// remedy is to send the bytes again from the beginning. Answered as a
		// conflict carrying offset 0, which is the shape a client already
		// handles.
		w.Header().Set(UploadOffsetHeader, "0")
		writeJSON(w, http.StatusConflict, uploadResponse{Status: "incomplete", Offset: 0})
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, "no such upload", http.StatusNotFound)
	default:
		a.serverError(w, r, what, err)
	}
}
