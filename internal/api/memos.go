package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-107: what is behind `RevisionMeta.memo_id`.
//
// A note's text has always named the memo it came from, and until this file
// there was nothing behind the id: no capture time, no duration, no retention
// state, no transcript, no audio. Board `1c` draws all of it —
// `FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE` and
// `▶ PLAY SOURCE AUDIO · 1:44 · PRUNES 2026-09-20` — and none of it was
// reachable over HTTP.
//
// The decision is the approved plan on CHRN-107 (revision 4, eight rulings).
// Read it before changing anything here. The three that shape this file:
//
//	ruling 2  THE SPLIT. Metadata about a recording — including its retention
//	          dates — goes to every member who can read the note it fed. The
//	          transcript's words and the audio's bytes go to the memo's author
//	          and to the owner, and to nobody else. It is the document's own
//	          distinction: "what a person decided to write down and what they
//	          happened to say into a phone are different facts."
//	ruling 3  A PRUNED RECORDING ANSWERS 410, never 404. Deleted by policy and
//	          never existed are different facts, and 404 collapses them into
//	          the one that reads like a bug.
//	ruling 6  RANGE ARITHMETIC IS THE STANDARD LIBRARY'S. http.ServeContent,
//	          inside a wrapper that keeps this document's error envelope --
//	          see errorEnvelopeWriter, which is the one part of this that is
//	          not obvious.
//
// ============================================================================
// THE ORDER IS AUTHORIZATION, THEN RETENTION, THEN THE DISK.
// ============================================================================
//
// GetMemoAudio compares the caller against the memo's author BEFORE it reads
// audio_pruned_at and before it opens the file, and the ordering is tested
// rather than assumed. The prune date IS published to every member on purpose
// (see GetNoteProvenance), so the reason is not that it is a secret. It is:
//
//   - THE ERROR LINE. A memo whose file is missing answers 500 and writes an
//     ERROR line. No member may raise an operator alert about another
//     account's recording, and a log line is not a response anyone can be
//     shown or redact.
//   - DEFENCE IN DEPTH FOR THE MEMOS BEHIND NO NOTE. A memo never triaged, or
//     discarded, sits behind nothing a member can read at all. For those the
//     comparison is the only thing between a guessed uuid and a recording,
//     and a check that only runs second is a check the next handler can
//     reorder.
//
// ============================================================================
// WHAT AN AGENT SESSION GETS.
// ============================================================================
//
// POST /memos/uploads is policyMember, which is "any account" -- so an agent
// CAN become a memo's author_id; it simply never has. mayReadMemo is a
// comparison and not a kind check, so the rule falls out rather than being
// declared: an agent session reads the transcript and the audio only of memos
// it uploaded itself, and today no agent has. Behind every note the Scribe
// routed from a person's memo it gets the provenance metadata and nothing
// else. Refusing agent uploads outright would be a policy change to a shipped
// route and is not this file's.

// Memos is the slice of the store this surface needs. An interface, on Wiki's
// and Corpus's pattern, so the handlers are testable without Postgres;
// *store.Store satisfies it.
//
// RetentionStatus is declared here rather than reused from Corpus on purpose.
// Corpus is wired only when CHRONICLE_AUDIO_DIR is set, because the storage
// report is a report about a disk -- and `retentionOf` (upload.go) reads that
// nil-able field and returns "" when it is absent. A provenance entry whose
// whole job is to state a retention status must not be able to answer with an
// empty one, silently, on a host with no audio directory.
type Memos interface {
	GetMemo(ctx context.Context, id uuid.UUID) (store.Memo, error)
	GetTranscript(ctx context.Context, memoID uuid.UUID) (store.Transcript, error)
	RetentionStatus(ctx context.Context, memoID uuid.UUID, window time.Duration) (string, *time.Time, error)
}

// memoTimeout bounds the three reads an entry costs. The wiki's own budget,
// for reads of the same size on the same pool.
const memoTimeout = 15 * time.Second

// memosUnavailable answers a router assembled with no memo store behind it --
// wikiUnavailable's pattern and its reason: setup() always supplies one, so a
// nil here must be a refusal rather than a dereference, and a test router
// assembled without one is precisely where that happens.
//
// It is also what lets "memos" join contract_test.go's groupGuard: a group
// guard with no code to answer is a group guard that cannot be declared.
func (a *api) memosUnavailable(w http.ResponseWriter) bool {
	if a.memos != nil {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, codeMemosUnconfigured,
		"this router was assembled with no memo store behind it")
	return true
}

// provenanceUnavailable is the notes group's refusal, for a notes operation.
//
// getNoteProvenance reads both stores -- the note and its revisions through
// Wiki, each memo through Memos -- and answers the ONE 503 its tag implies,
// `wiki_unconfigured`, rather than two codes for one missing thing. setup()
// sets deps.Memos beside deps.Wiki, unconditionally, from the same
// *store.Store: a router holding one and not the other is a test's
// construction and not a deployment's, and from a client's side what is
// missing either way is the tier-2 store behind a note sub-resource.
func (a *api) provenanceUnavailable(w http.ResponseWriter) bool {
	if a.wiki != nil && a.memos != nil {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, codeWikiUnconfigured,
		"this router was assembled with no notes store behind it")
	return true
}

// mayReadMemo is ruling 2, and it is the whole of the access decision: the
// memo's author, or the owner.
//
// A comparison rather than a query -- store.GetMemo returns AuthorID -- and a
// comparison rather than a kind check, which is what makes the agent rule a
// consequence instead of a special case (see this file's header). IsAdmin()
// and not IsOwner: it is requireOwner's own predicate, and it is why an agent
// is never "the owner" here.
func mayReadMemo(caller store.User, m store.Memo) bool {
	return caller.ID == m.AuthorID || caller.IsAdmin()
}

// ── the memos behind a note ─────────────────────────────────────────────────

// GetNoteProvenance answers one entry per revision that came from a memo,
// oldest revision first.
//
// A SIBLING RATHER THAN A FIELD ON THE NOTE, for a stronger version of
// listNoteBacklinks's reason. A note's ETag is its current revision id, and
// `retention_status`, `prunes_at` and `audio_pruned_at` all move while no
// revision is appended -- the pruner sweeps at 03:00 and nothing about the
// note has changed. Inline, an unchanged note would answer 304 carrying a
// PRUNES date for audio that went hours ago, and a play control over nothing.
//
// A NOTE SOMEBODY TYPED ANSWERS AN EMPTY LIST, not a 404: the note exists and
// nothing produced it, which is a true answer rather than a missing one.
//
// The metadata here -- including both retention dates -- goes to every member
// who can read the note, deliberately. It is what lets a client render
// `PRUNES 2026-09-20`, or *transcript kept, audio pruned 2026-09-20*, without
// a request that fails; `audio_readable` and `transcript.readable` are what
// say whether asking for the words or the bytes is worth it.
func (a *api) GetNoteProvenance(w http.ResponseWriter, r *http.Request, ref string) {
	if a.provenanceUnavailable(w) {
		return
	}
	number, ok := noteRef(w, ref)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), memoTimeout)
	defer cancel()

	// The sibling rule every other note sub-resource follows: a withdrawn note
	// answers the tombstone, and liveNote is where that is decided once.
	n, ok := a.liveNote(w, r, ctx, number)
	if !ok {
		return
	}
	revs, err := a.wiki.NoteRevisions(ctx, n.ID)
	if err != nil {
		a.serverError(w, r, "note provenance: revisions", err)
		return
	}

	caller := userFrom(r.Context())
	items := make([]wire.MemoProvenance, 0, len(revs))
	for _, rv := range revs {
		if rv.MemoID == nil {
			continue
		}
		entry, err := a.provenanceEntry(ctx, rv, *rv.MemoID, caller)
		if err != nil {
			// Including store.ErrNotFound: note_revisions.memo_id references
			// tier2.memos, so a revision naming a memo that is not there is a
			// broken invariant rather than a 404 to hand a client.
			a.serverError(w, r, "note provenance: memo", err)
			return
		}
		items = append(items, entry)
	}
	writeJSON(w, http.StatusOK, wire.ProvenanceList{Items: items})
}

// provenanceEntry is the three reads one entry costs: the memo, its retention
// status and its best transcript. No stat -- `audio_readable` is a permission
// and says nothing about the disk, which is exactly what buys that.
func (a *api) provenanceEntry(ctx context.Context, rv store.NoteRevision, memoID uuid.UUID, caller store.User) (wire.MemoProvenance, error) {
	m, err := a.memos.GetMemo(ctx, memoID)
	if err != nil {
		return wire.MemoProvenance{}, err
	}
	status, at, err := a.memos.RetentionStatus(ctx, memoID, audio.ProjectionWindow)
	if err != nil {
		return wire.MemoProvenance{}, err
	}
	t, err := a.memos.GetTranscript(ctx, memoID)
	present := true
	if errors.Is(err, store.ErrNotFound) {
		present = false
	} else if err != nil {
		return wire.MemoProvenance{}, err
	}

	readable := mayReadMemo(caller, m)
	entry := wire.MemoProvenance{
		RevisionSeq:     rv.Seq,
		RevisionId:      rv.ID,
		MemoId:          m.ID,
		CapturedAt:      m.CapturedAt,
		AudioReadable:   readable,
		RetentionStatus: wire.MemoProvenanceRetentionStatus(status),
		// RULING 7, on this payload and on Memo: prunes_at names a FUTURE
		// sweep or nothing. store.RetentionStatus overloads one `at` across
		// `scheduled` and `pruned`, and passing it through unexamined is how a
		// field called prunes_at came to answer a date in the past.
		PrunesAt:      prunesAtFor(status, at),
		AudioPrunedAt: m.AudioPrunedAt,
		Transcript: wire.ProvenanceTranscript{
			Present: present,
			// The words are the author's and the owner's; whether there ARE
			// words is metadata. Two fields because they are two facts.
			Readable: readable,
		},
	}
	entry.DurationMs, entry.DurationSource = durationOf(m, t, present)
	if present {
		// model, transcribed_at and partial describe GetTranscript's row and
		// are metadata, not words: a non-author holding readable:false has no
		// other way to learn that a present transcript is an incomplete
		// fallback. The entry carries no durability claim of its own --
		// retention_status is where that is computed, from a different row.
		model, transcribedAt, partial := t.Model, t.TranscribedAt, t.Partial
		entry.Transcript.Model = &model
		entry.Transcript.TranscribedAt = &transcribedAt
		entry.Transcript.Partial = &partial
	}
	return entry, nil
}

// prunesAtFor narrows store.RetentionStatus's overloaded `at` to the one case
// the wire field is named for. `pruned` carries audio_pruned_at, which belongs
// in its own field and in the past tense.
func prunesAtFor(status string, at *time.Time) *time.Time {
	if status != store.RetentionScheduled {
		return nil
	}
	return at
}

// durationOf is ruling 4: ONE duration, plus which column answered.
//
// Two columns hold one and they are measured differently -- memos.duration_ms
// is Ogg granule arithmetic and is populated for Ogg Opus only, while
// transcripts.audio_duration_ms is measured off the normalised 16 kHz mono WAV
// and is populated for everything that transcribes. Every memo in the live
// corpus is m4a with a NULL header duration, so reading the memo column alone
// would render a dash for the whole corpus with no error anywhere.
//
// It decides nothing for CHRN-85. Whichever way that ticket goes, this
// collapses to one value and the contract does not change.
func durationOf(m store.Memo, t store.Transcript, present bool) (*int64, *wire.MemoProvenanceDurationSource) {
	source := func(v wire.MemoProvenanceDurationSource) *wire.MemoProvenanceDurationSource { return &v }
	if m.DurationMS != nil {
		ms := int64(*m.DurationMS)
		return &ms, source(wire.MemoProvenanceDurationSourceMemoHeader)
	}
	if present && t.AudioDurationMS != nil {
		ms := *t.AudioDurationMS
		return &ms, source(wire.MemoProvenanceDurationSourceTranscript)
	}
	// Neither an Ogg header nor a transcript: the pre-transcription window.
	// Null, and no source at all, rather than a zero that renders as 0:00.
	return nil, nil
}

// ── the transcript ──────────────────────────────────────────────────────────

// GetMemoTranscript answers what a memo said, to the memo's author and to the
// owner.
//
// THE ROW IS GetTranscript'S: the newest complete transcript, or the newest
// partial when no complete one exists yet. A memo can hold several -- a
// retranscribe with a different model writes another -- and `partial` says
// which kind this is.
//
// A readable memo with no transcript row answers 404 with `no_transcript`
// rather than `not_found`, because the remedy is to wait rather than to
// correct the id. A memo belonging to somebody else answers the `not_found`
// that a memo which does not exist answers, byte for byte.
func (a *api) GetMemoTranscript(w http.ResponseWriter, r *http.Request, memoID wire.MemoId) {
	if a.memosUnavailable(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), memoTimeout)
	defer cancel()

	m, ok := a.readableMemo(w, r, ctx, memoID)
	if !ok {
		return
	}
	t, err := a.memos.GetTranscript(ctx, m.ID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNoTranscript,
			"this memo has not been transcribed yet")
		return
	}
	if err != nil {
		a.serverError(w, r, "get transcript", err)
		return
	}
	writeJSON(w, http.StatusOK, toMemoTranscript(t))
}

// toMemoTranscript is the whole row. Not a bounded projection: the live corpus
// averages under two minutes a memo, which is a few kilobytes of segments, so
// a bounded-or-not choice here would be a ruling about nothing.
func toMemoTranscript(t store.Transcript) wire.MemoTranscript {
	segments := make([]wire.TranscriptSegment, 0, len(t.Segments))
	for _, s := range t.Segments {
		segments = append(segments, wire.TranscriptSegment{
			StartMs: s.StartMS, EndMs: s.EndMS, Text: s.Text,
		})
	}
	return wire.MemoTranscript{
		MemoId:          t.MemoID,
		Text:            t.Text,
		Segments:        segments,
		Partial:         t.Partial,
		Model:           t.Model,
		Backend:         t.Backend,
		AudioDurationMs: t.AudioDurationMS,
		CoveredMs:       t.CoveredMS,
		TranscribedAt:   t.TranscribedAt,
	}
}

// readableMemo resolves a memo id to a memo whose words and bytes this caller
// may have, and is the ONE place that decision is made for both operations.
//
// A memo belonging to somebody else answers 404 and not 403, on findUpload's
// stated rule: "403 would confirm that the id names a real upload, which is a
// fact about another account's activity that no caller is owed." Same status,
// same code, same message as an id that names nothing -- which is the point,
// and which a test asserts byte for byte.
func (a *api) readableMemo(w http.ResponseWriter, r *http.Request, ctx context.Context, id uuid.UUID) (store.Memo, bool) {
	m, err := a.memos.GetMemo(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such memo")
		return store.Memo{}, false
	case err != nil:
		a.serverError(w, r, "get memo", err)
		return store.Memo{}, false
	}
	if !mayReadMemo(userFrom(r.Context()), m) {
		writeError(w, http.StatusNotFound, codeNotFound, "no such memo")
		return store.Memo{}, false
	}
	return m, true
}

// ── the recording ───────────────────────────────────────────────────────────

// GetMemoAudio streams the bytes, while they exist.
//
// Three honest answers, and the order they are decided in is this file's
// header: authorization, then retention, then the disk.
//
//	pruned   410, code audio_pruned. Gone on purpose, and the transcript
//	         remains. The date and the surviving transcript are on
//	         MemoProvenance, which is what a client renders from and the only
//	         thing an <audio src> element can read -- a media element exposes
//	         neither the status nor the body of a failed load.
//	present  200, and it streams. There is no refusal: the bytes are there.
//	missing  500, code audio_missing, AND an ERROR line whether or not anybody
//	         reads the response. CHRN-23's `missing`: not an expected absence,
//	         but the failure the reconciliation report exists to surface.
//
// THE BOUND PARAMETERS ARE DELIBERATELY UNUSED. `Range` and `If-None-Match`
// are declared on the operation because a client sends them and the document
// should say so, and the generated wrapper therefore binds them — but what
// acts on them is ServeContent, off r.Header, which is the whole point of
// handing the range arithmetic to the standard library. Reading the bound copy
// here and passing it back down would be two parsers for one header.
func (a *api) GetMemoAudio(w http.ResponseWriter, r *http.Request, memoID wire.MemoId, _ wire.GetMemoAudioParams) {
	if a.memosUnavailable(w) {
		return
	}
	if a.audio == nil {
		writeError(w, http.StatusServiceUnavailable, codeAudioUnconfigured,
			"audio is not configured on this deployment: set CHRONICLE_AUDIO_DIR")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), memoTimeout)
	defer cancel()

	// 1 · AUTHORIZATION, before anything else is read. Neither the refusal
	// below nor the ERROR line further down may say a word about another
	// account's recording.
	m, ok := a.readableMemo(w, r, ctx, memoID)
	if !ok {
		return
	}

	// 2 · RETENTION. Not 404: a client holding CHR-0311 must be able to tell
	// "the recording was deleted by policy" from "no such memo", and 404
	// collapses them into the answer that reads like a bug.
	if m.AudioPruned() {
		writeError(w, http.StatusGone, codeAudioPruned,
			"this recording was pruned; its transcript is permanent and remains")
		return
	}

	// 3 · THE DISK.
	path, err := a.audio.Path(audio.Ref{AuthorID: m.AuthorID, ContentHash: m.ContentHash})
	if err != nil {
		a.serverError(w, r, "audio path", err)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		// storage.go logs the same condition unconditionally and says why: a
		// memo that expects its recording and cannot find it is the failure
		// CLAUDE.md calls unrecoverable. This is the second caller.
		a.logger.ErrorContext(ctx, "audio missing from disk for a memo that expects it",
			"memo", m.ID, "root", a.audio.Root())
		writeError(w, http.StatusInternalServerError, codeAudioMissing,
			"the recording is not on disk; see GET /admin/storage")
		return
	}
	if err != nil {
		a.serverError(w, r, "open audio", err)
		return
	}
	defer func() { _ = f.Close() }()

	// ONE RULE, TWO CALLERS: the type a browser is told is the type the ASR
	// submission is told, because both ask audio.MediaType.
	w.Header().Set("Content-Type", audio.MediaType(m.Codec, m.OriginalFilename))

	// THE VALIDATOR IS THE MEMO ID. CH002 refuses any UPDATE that moves
	// author_id, content_hash, byte_size or captured_at -- the four together
	// -- so one id names one byte sequence for as long as the row exists.
	// Strong, with nothing derived in it, and the same idea as etagFor.
	w.Header().Set("ETag", `"`+m.ID.String()+`"`)

	// AND IT REVALIDATES. The default is not "no caching" but HEURISTIC
	// freshness: with a strong validator and a Last-Modified, RFC 9111 §4.2.2
	// invites a browser to treat a memo captured 25 days ago as fresh for two
	// and a half days -- inside which a player serves bytes the pruner has
	// deleted and never sees the 410.
	w.Header().Set("Cache-Control", "private, no-cache")

	// A MULTI-RANGE REQUEST IS NOT SERVED. ServeContent would answer
	// 206 multipart/byteranges, a shape this document does not declare and no
	// media element asks for; RFC 9110 lets a server ignore Range entirely, so
	// the header is dropped and the whole body answered. Cloned rather than
	// mutated in place: the request is the generated wrapper's.
	if strings.Contains(r.Header.Get("Range"), ",") {
		r = r.Clone(ctx)
		r.Header.Del("Range")
	}

	// modtime is captured_at and NOT the file's mtime, which moves when a
	// finished upload is renamed into place, when a restore rewrites it, and
	// when CHRN-68's drill runs. A validator that changes while the content
	// does not is a cache that misses for no reason.
	ww := &errorEnvelopeWriter{ResponseWriter: w}
	http.ServeContent(ww, r, "", m.CapturedAt, f)

	// A 500 from inside ServeContent (a Seek that failed) is logged here
	// rather than by the wrapper, so the writer stays a writer.
	if ww.status >= http.StatusInternalServerError {
		a.logger.ErrorContext(ctx, "serving a recording failed",
			"memo", m.ID, "http.status_code", ww.status)
	}
}

// errorEnvelopeWriter keeps this document's error shape over ServeContent.
//
// ============================================================================
// WHY IT HOOKS WriteHeader AND NOT Write.
// ============================================================================
//
// ServeContent produces two statuses at or above 400 and they arrive in
// opposite orders:
//
//	412  checkPreconditions writes a BARE w.WriteHeader(412) -- no body at
//	     all -- so a wrapper waiting for a Write to swallow never gets one,
//	     and the audio Content-Type set before the call is still in the
//	     headers.
//	416  serveError has already stripped Cache-Control, ETag and
//	     Last-Modified, and http.Error has set text/plain and nosniff and
//	     deleted Content-Length, all BEFORE WriteHeader.
//
// One hook at WriteHeader -- rewrite the type, write the envelope, swallow the
// body that may or may not follow -- is what covers both with one mechanism.
// apierr.go has this trap written down already for oapi-codegen's binder:
// "IT IS SUPPLIED BECAUSE THE DEFAULT UNDOES THE ERROR-SHAPE DECISION."
// ServeContent is the same default in a new place.
//
// Content-Range is NOT touched. ServeContent sets it on the no-overlap 416 --
// a range starting past the end of the file -- and not on a malformed one, and
// preserving what it did rather than asserting what it should have done is
// what keeps this a wrapper instead of a second implementation.
type errorEnvelopeWriter struct {
	http.ResponseWriter

	// status is what was written, 0 until something is. The handler reads it
	// to decide whether to log.
	status int

	// rewrote is set when the body was replaced with the envelope, so the
	// text/plain sentence http.Error tries to write afterwards is swallowed.
	rewrote bool
}

func (w *errorEnvelopeWriter) WriteHeader(status int) {
	if w.status != 0 {
		// Go's own "superfluous WriteHeader" case. Nothing to do twice.
		return
	}
	w.status = status
	if status < 400 {
		w.ResponseWriter.WriteHeader(status)
		return
	}

	code, message := audioRefusal(status)
	body, err := json.Marshal(wire.Error{Code: code, Message: message})
	if err != nil {
		// Unreachable: wire.Error is two strings.
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.rewrote = true
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	// http.Error's nosniff belongs to a text/plain body that is no longer
	// being sent; every other refusal in this API answers without it, and a
	// refusal that differs from the rest by one header is one a client has to
	// special-case.
	h.Del("X-Content-Type-Options")
	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(body)
}

func (w *errorEnvelopeWriter) Write(b []byte) (int, error) {
	if w.rewrote {
		// Reported as written so ServeContent's own bookkeeping is satisfied.
		return len(b), nil
	}
	return w.ResponseWriter.Write(b)
}

// audioRefusal names the two statuses ServeContent can answer that a client
// acts on differently, and answers `internal` for anything else it may grow.
func audioRefusal(status int) (code, message string) {
	switch status {
	case http.StatusRequestedRangeNotSatisfiable:
		return codeRangeNotSatisfiable, "the Range header names bytes this recording does not have"
	case http.StatusPreconditionFailed:
		return codePreconditionFailed, "a precondition on this request does not hold for this recording"
	default:
		return codeInternal, "internal error"
	}
}
