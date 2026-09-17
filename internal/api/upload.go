package api

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/audio"
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

// wire.UploadState is the one shape all four calls answer with, discriminated
// by Status. One shape rather than two because a client polling a session and a
// client finishing one are the same client, and it should not have to switch
// parsers on a status code — which is also why the 409 and the 408 carry it
// rather than an error envelope: both are resume instructions.
//
// The shapes live in openapi.yaml now. Two properties that were comments here
// are documented there instead, where three clients can read them: `offset`
// carries no omitempty, because zero is the answer for a session that has
// received nothing and dropping it would leave a client unable to tell "nothing
// yet" from "the server did not say"; and `duplicate` is IngestResult.Collapsed
// — these bytes were already known — never inferred from a delivery count
// (CHRN-18 §10).

// toMemo renders a memo for the wire. Deliberately not a path: CHRN-23 derives
// one from the row, and publishing it would invite a client to build its own.
//
// `retention_status` is a STATUS RATHER THAN A DATE, because for a memo with no
// durable transcript there is no date the pruner will use — and a
// `PRUNES 2026-09-20` label that passes with nothing happening is the label
// lying, which CHRN-25 §5 already refused in the other direction. It is the
// same clause the sweep evaluates, which is what makes the date a UI shows the
// date the job uses.
//
// ============================================================================
// prunes_at NAMES A FUTURE SWEEP OR NOTHING (CHRN-107 ruling 7).
// ============================================================================
//
// store.RetentionStatus overloads one `at` across two cases -- its docstring
// says so: "set only for `scheduled` and `pruned`" -- and `pruned` carries
// audio_pruned_at, a date in the PAST. This function passed it through
// unexamined, so `getUpload` answered a field the document describes as "When,
// on that clause" with a timestamp behind us, on exactly the memo CHRN-22
// §3 [rev] says a person is most likely to be looking at when they wonder what
// happened to their recording.
//
// So the past tense gets its own field. audio_pruned_at comes off the memo row
// rather than out of the overloaded `at`, which also means it is right whatever
// the status says.
func toMemo(m store.Memo, retentionStatus string, prunesAt *time.Time) wire.Memo {
	if retentionStatus != store.RetentionScheduled {
		prunesAt = nil
	}
	return wire.Memo{
		RetentionStatus:  retentionStatus,
		PrunesAt:         prunesAt,
		AudioPrunedAt:    m.AudioPrunedAt,
		Id:               m.ID,
		State:            m.State,
		Retention:        wire.MemoRetention(m.Retention),
		ContentHash:      m.ContentHash,
		ByteSize:         m.ByteSize,
		CapturedAt:       m.CapturedAt,
		AudioPruned:      m.AudioPruned(),
		DurationMs:       m.DurationMS,
		Codec:            m.Codec,
		SampleRateHz:     m.SampleRateHz,
		OriginalFilename: m.OriginalFilename,
	}
}

// OpenUpload declares an upload.
//
// Three answers, and the third is the one that makes re-delivery cheap:
// 201 with a fresh session, 200 with the session this key already has, or 200
// with a memo because the author already holds those bytes and nothing needs
// sending.
func (a *api) OpenUpload(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusBadRequest, codeInvalidBody, "idempotency_key must be 16 to 200 characters")
		return
	}
	if !hexDigest.MatchString(req.ContentHash) {
		writeError(w, http.StatusBadRequest, codeInvalidBody,
			"content_hash must be the SHA-256 of the file as 64 lowercase hex characters")
		return
	}
	if req.ByteSize <= 0 {
		writeError(w, http.StatusBadRequest, codeInvalidBody, "byte_size must be a positive number of bytes")
		return
	}
	switch req.Retention {
	case "", store.RetentionDiscardNow, store.RetentionDays30, store.RetentionForever:
	default:
		writeError(w, http.StatusBadRequest, codeInvalidBody,
			`retention must be one of "discard_now", "days_30", "forever", or omitted`)
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
	a.writeUpload(w, r, status, res)
}

// AppendChunk takes the next chunk.
func (a *api) AppendChunk(w http.ResponseWriter, r *http.Request, id wire.UploadId, params wire.AppendChunkParams) {
	if !a.uploadsReady(w) {
		return
	}
	u, ok := a.findUpload(w, r, id)
	if !ok {
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != uploadBodyType {
		writeError(w, http.StatusUnsupportedMediaType, codeUnsupportedMedia, "Content-Type must be "+uploadBodyType)
		return
	}
	// A chunk must declare its length. Go reports -1 for a chunked body, and
	// without a length there is no way to refuse an oversized chunk before
	// reading it — nor for the reader to end at a point the server knows, which
	// is what internal/upload's oversend check relies on not to wait forever on
	// a client that has stopped sending.
	if r.ContentLength < 0 {
		writeError(w, http.StatusLengthRequired, codeLengthRequired,
			"a chunk must carry a Content-Length; chunked bodies are not accepted")
		return
	}

	// Bound by the generator from the declared header parameter: absent or
	// unparseable answers 400 through bindError before this runs. What it does
	// NOT enforce is the schema's `minimum: 0` — std-http-server binds types,
	// not constraints — so the negative case is still checked here rather than
	// assumed away by a line in the document.
	offset := params.UploadOffset
	if offset < 0 {
		writeError(w, http.StatusBadRequest, codeInvalidParameter,
			UploadOffsetHeader+" must be a non-negative integer")
		return
	}

	// Refused before a byte is read, where the header makes it free to see. The
	// service enforces the same bound over the reader itself — this is the
	// cheap case, not the guarantee, and a wrong offset still resolves as a 409
	// there because the offset is checked before anything is read either way.
	if remaining := u.ByteSize - offset; r.ContentLength > remaining {
		writeError(w, http.StatusUnprocessableEntity, codeOversend,
			"this chunk is longer than the upload has left to receive")
		return
	}

	res, err := a.uploads.Append(r.Context(), u, offset, r.Body)
	if err != nil {
		a.uploadError(w, r, "append to upload", err)
		return
	}
	a.writeUpload(w, r, http.StatusOK, res)
}

// GetUpload reports how far a session got. It is how a client that
// crashed mid-chunk finds out where to resume without guessing.
func (a *api) GetUpload(w http.ResponseWriter, r *http.Request, id wire.UploadId) {
	if !a.uploadsReady(w) {
		return
	}
	u, ok := a.findUpload(w, r, id)
	if !ok {
		return
	}
	res, err := a.uploads.Status(r.Context(), u)
	if err != nil {
		a.uploadError(w, r, "read upload status", err)
		return
	}
	a.writeUpload(w, r, http.StatusOK, res)
}

// AbandonUpload drops a session and its bytes.
func (a *api) AbandonUpload(w http.ResponseWriter, r *http.Request, id wire.UploadId) {
	if !a.uploadsReady(w) {
		return
	}
	u, ok := a.findUpload(w, r, id)
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
		writeError(w, http.StatusServiceUnavailable, codeUploadsUnconfigured, "uploads are not configured: set CHRONICLE_AUDIO_DIR")
		return false
	}
	return true
}

// findUpload resolves {id} to this caller's session.
//
// A session belonging to somebody else is 404 and not 403. 403 would confirm
// that the id names a real upload, which is a fact about another account's
// activity that no caller is owed.
func (a *api) findUpload(w http.ResponseWriter, r *http.Request, id uuid.UUID) (store.Upload, bool) {
	u, err := a.uploads.Find(r.Context(), id, userFrom(r.Context()).ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "no such upload")
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
// retentionOf asks what will happen to a memo's audio. Best effort: a memo
// that has just been captured always answers `awaiting_transcript`, and a
// missing answer is a field a client can see is absent rather than a failed
// upload.
func (a *api) retentionOf(r *http.Request, memoID uuid.UUID) (string, *time.Time) {
	if a.corpus == nil {
		return "", nil
	}
	status, at, err := a.corpus.RetentionStatus(r.Context(), memoID, audio.ProjectionWindow)
	if err != nil {
		a.logger.WarnContext(r.Context(), "could not read a memo's retention status",
			"memo", memoID, "error", err)
		return "", nil
	}
	return status, at
}

func (a *api) writeUpload(w http.ResponseWriter, r *http.Request, status int, res upload.Result) {
	body := wire.UploadState{}
	switch {
	case res.Committed != nil:
		status, prunesAt := a.retentionOf(r, res.Committed.Memo.ID)
		m := toMemo(res.Committed.Memo, status, prunesAt)
		body.Status = wire.Complete
		body.ByteSize = res.Committed.Memo.ByteSize
		body.Offset = res.Committed.Memo.ByteSize
		body.Memo = &m
		body.Duplicate = res.Committed.Collapsed
	case res.Session != nil:
		body.Status = wire.Incomplete
		uploadID := res.Session.ID.String()
		body.UploadId = &uploadID
		body.ByteSize = res.Session.ByteSize
		body.Offset = res.Session.Offset
		expires := res.Session.ExpiresAt
		body.ExpiresAt = &expires
	default:
		// Not reachable: Open, Append and Status each set exactly one. Answered
		// rather than left to render as an empty object, because a client
		// parsing `{"status":""}` would be debugging its own code.
		writeError(w, http.StatusInternalServerError, codeInternal, "internal error")
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
		writeJSON(w, http.StatusConflict, wire.UploadState{
			Status: wire.Incomplete,
			Offset: conflict.Offset,
		})
	case errors.Is(err, store.ErrUploadKeyReused):
		writeError(w, http.StatusConflict, codeKeyReused,
			"that idempotency_key is already in use for different content; mint a new one")
	case errors.Is(err, upload.ErrTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, codeBodyTooLarge, err.Error())
	case errors.Is(err, upload.ErrTooManyOpen):
		writeError(w, http.StatusTooManyRequests, codeTooManyOpen,
			"too many uploads already open; finish or abandon one first")
	case errors.Is(err, upload.ErrHashMismatch):
		writeError(w, http.StatusUnprocessableEntity, codeHashMismatch,
			"the bytes received do not match content_hash; the upload has been discarded")
	case errors.Is(err, upload.ErrOversend):
		writeError(w, http.StatusUnprocessableEntity, codeOversend, err.Error())
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
		writeJSON(w, http.StatusRequestTimeout, wire.UploadState{
			Status: wire.Incomplete,
			Offset: cut.Offset,
		})
	case errors.Is(err, store.ErrKeyReused):
		writeError(w, http.StatusConflict, codeKeyReused,
			"that idempotency_key already produced a different memo; mint a new one")
	case errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, codeInvalidBody, err.Error())
	case errors.Is(err, upload.ErrStagingLost):
		// The declaration still stands and the session is left alone, so the
		// remedy is to send the bytes again from the beginning. Answered as a
		// conflict carrying offset 0, which is the shape a client already
		// handles.
		w.Header().Set(UploadOffsetHeader, "0")
		writeJSON(w, http.StatusConflict, wire.UploadState{Status: wire.Incomplete, Offset: 0})
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such upload")
	default:
		a.serverError(w, r, what, err)
	}
}
