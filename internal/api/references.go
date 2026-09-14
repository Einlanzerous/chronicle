package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// POST /references/resolve (CHRN-104): the second phase of the two-phase
// render CHRN-97 ruling 3 picked.
//
// ============================================================================
// THE SECOND PHASE OF THE RENDER, AND THE ONE CALL THAT LEAVES THE PROCESS.
// ============================================================================
//
// A note read (CHRN-98) is a pure tier-2 read: it scans its own text, hands
// back the reference descriptors the scan produced, and dials nothing -- which
// is what lets its ETag mean what it says and keeps a slow upstream out of a
// note's latency. A client that wants cards batches those descriptors here and
// gets one CHRN-51 Resolution each, keyed on token.
//
// ============================================================================
// THE SERVER RE-DERIVES EVERY DESCRIPTOR FROM ITS TOKEN (ruling 8).
// ============================================================================
//
// The request carries the full descriptor -- symmetrical with what the note
// payload emitted -- and this handler believes NONE of it except the token. It
// parses the token under CHRN-48's grammar and refuses a descriptor that
// disagrees. The field that matters is `system`: it is what picks the
// transport, and a client that could choose it could point Chronicle's
// credential at a namespace the token does not belong to. Nothing a client
// sends decides which upstream gets dialled.
//
// ============================================================================
// CHRONICLE'S OWN NAMESPACE NEVER LEAVES THE PROCESS.
// ============================================================================
//
// CHR-#### and DSC-#### have no Transport and no classifier: internal/resolve
// says a caller "either resolves them itself or registers a Transport for
// them", and this is the caller resolving them itself, against the number
// columns, with no network and no staleness. Chronicle is the upstream for its
// own namespace, and it is the only namespace it mints a vocabulary member in
// -- `deleted` for a soft-deleted note, and a discussion's own open/resolved.
//
// ============================================================================
// THIS HANDLER PARSES TOKENS. IT DOES NOT SCAN, AND IT NEVER FEEDS THE MISS FEED.
// ============================================================================
//
// Keys.NoteMisses has exactly one caller, and it is whoever SCANS -- the note
// handler. Ruling 8's first option would have made this endpoint a second one
// by re-scanning tokens as one-token documents, and one dead key in one note
// would then log twice per page render, once from the read and once from the
// resolve; the count resolve.logCounts exists to provide would stop counting
// anything. markdown.ParseReference exists so that this file has no way to
// produce a miss at all: it decides shape and never membership, and this
// struct holds no key set to report one to.

// References is the slice of the resolver this surface needs. An interface so
// the handler can be tested against a resolver built on fake upstreams and an
// injected clock, and so a router with none can be built at all.
type References interface {
	Resolve(ctx context.Context, refs []markdown.Reference) []resolve.Resolution
}

// LocalReferences is the slice of the store Chronicle's own namespace resolves
// against: two lookups by number and one by revision. Nothing here filters on
// deleted_at, because a deleted note is an answer rather than an absence.
type LocalReferences interface {
	NoteByNumber(ctx context.Context, number int64) (store.Note, error)
	CurrentRevision(ctx context.Context, noteID uuid.UUID) (store.NoteRevision, error)
	DiscussionByNumber(ctx context.Context, number int64) (store.Discussion, error)
}

// maxResolveBody bounds the request: fifty descriptors, each a token and four
// derived fields, with room to spare.
const maxResolveBody = 64 << 10

// Chronicle's own outcome vocabulary, minted in the one namespace it owns and
// in no other. Switchyard's and Amber's members are relayed verbatim.
const (
	localOutcomeDeleted  = "deleted"
	localOutcomeOpen     = "open"
	localOutcomeResolved = "resolved"
)

// referencesUnavailable answers a router assembled with no resolver behind it.
//
// `serve` always supplies one, so this is not a deployment state -- it is the
// shape guarded() already uses for a nil Accounts, and for the same reason: a
// nil here must be a refusal and not a dereference inside a render.
func (a *api) referencesUnavailable(w http.ResponseWriter) bool {
	if a.references != nil && a.localRefs != nil {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, codeReferencesUnconfigured,
		"this router was assembled with no reference resolver behind it")
	return true
}

func (a *api) ResolveReferences(w http.ResponseWriter, r *http.Request) {
	if a.referencesUnavailable(w) {
		return
	}
	var req wire.ResolveRequest
	if !decodeJSONLimit(w, r, &req, maxResolveBody) {
		return
	}
	// The declared minItems and maxItems are not enforced by the generated
	// binder -- std-http-server binds types, not constraints -- so both are
	// checked here rather than assumed away by a line in the document.
	if len(req.References) == 0 {
		writeError(w, http.StatusBadRequest, codeInvalidBody, "a batch needs at least one reference")
		return
	}
	if len(req.References) > resolve.DefaultCap {
		writeError(w, http.StatusBadRequest, codeInvalidBody,
			fmt.Sprintf("a batch carries at most %d references", resolve.DefaultCap))
		return
	}

	// EVERY DESCRIPTOR IS RE-DERIVED BEFORE ANYTHING IS DIALLED. A refusal here
	// costs no upstream call, and the position rather than the token is what
	// the message names: the token is authored content and the message stays
	// flat.
	refs := make([]markdown.Reference, len(req.References))
	for i, d := range req.References {
		ref, ok := markdown.ParseReference(d.Token)
		if !ok {
			writeError(w, http.StatusBadRequest, codeInvalidReference,
				fmt.Sprintf("references[%d] is not a reference under Chronicle's grammar", i))
			return
		}
		if !descriptorAgrees(d, ref) {
			writeError(w, http.StatusBadRequest, codeReferenceMismatch,
				fmt.Sprintf("references[%d] disagrees with what its token parses to", i))
			return
		}
		refs[i] = ref
	}

	// The resolver's own budget is five seconds and the local reads are a
	// handful of primary-key lookups; this is the ceiling over both.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// SPLIT BY NAMESPACE. Chronicle's own resolve here; everything else goes to
	// the resolver in ONE batch, so the cap, the budget and the per-system
	// breaker see the whole page and not one reference at a time.
	out := make([]wire.Resolution, len(refs))
	var remote []markdown.Reference
	var remoteAt []int
	for i, ref := range refs {
		if ref.System != markdown.SystemChronicle {
			remote = append(remote, ref)
			remoteAt = append(remoteAt, i)
			continue
		}
		res, err := a.resolveLocal(ctx, ref)
		if err != nil {
			a.serverError(w, r, "resolve local reference", err)
			return
		}
		out[i] = toResolution(res)
	}
	if len(remote) > 0 {
		for j, res := range a.references.Resolve(ctx, remote) {
			out[remoteAt[j]] = toResolution(res)
		}
	}

	// How many references a render asked for, beside the per-system outcome
	// counts logCounts already writes: CHRN-51's premise is "a page with
	// thirty references does not make thirty calls", and this is the number
	// that says whether thirty-reference pages exist at all. Counts only --
	// no token reaches a log line.
	a.logger.DebugContext(ctx, "resolved a reference batch",
		"references", len(refs), "local", len(refs)-len(remote), "upstream", len(remote))

	writeJSON(w, http.StatusOK, wire.ResolveResponse{Resolutions: out})
}

// resolveLocal answers one CHR- or DSC- reference against the store.
//
// The shape is classifySwitchyard's, deliberately: a number nothing holds is
// BROKEN with the key as written and no URL, a fact about the referent rather
// than an outage. A store error is not an answer about the reference and is
// returned rather than classified -- the request as a whole cannot be served,
// and the 500 says so.
func (a *api) resolveLocal(ctx context.Context, ref markdown.Reference) (resolve.Resolution, error) {
	res := resolve.Resolution{Ref: ref, State: resolve.StateResolved, FetchedAt: time.Now()}

	switch ref.Target {
	case markdown.TargetNote:
		n, err := a.localRefs.NoteByNumber(ctx, ref.Number)
		switch {
		case errors.Is(err, store.ErrNotFound):
			res.State = resolve.StateBroken
			res.Upstream = &resolve.Upstream{Key: ref.Token}
			res.Explain = "Chronicle has no note with this number"
			return res, nil
		case err != nil:
			return res, err
		}
		if n.Deleted() {
			// BROKEN, WITH THE ONE MEMBER CHRONICLE MINTS, and nothing else:
			// no title, no URL. The note existed and was withdrawn, which is
			// the distinction CHRN-39's journal keeps and ruling 4's tombstone
			// carries; its text is not part of that answer.
			res.State = resolve.StateBroken
			res.Upstream = &resolve.Upstream{Key: n.Ref(), Outcome: localOutcomeDeleted}
			res.Explain = "this note was deleted"
			return res, nil
		}
		rev, err := a.localRefs.CurrentRevision(ctx, n.ID)
		if err != nil {
			return res, err
		}
		// THE KEY AS ANSWERED: a hand-written CHR-311 answers CHR-0311, which
		// is the same doctrine classifySwitchyard applies to an alias.
		res.Upstream = &resolve.Upstream{Key: n.Ref(), Title: rev.Title}

	case markdown.TargetDiscussion:
		d, err := a.localRefs.DiscussionByNumber(ctx, ref.Number)
		switch {
		case errors.Is(err, store.ErrNotFound):
			res.State = resolve.StateBroken
			res.Upstream = &resolve.Upstream{Key: ref.Token}
			res.Explain = "Chronicle has no discussion with this number"
			return res, nil
		case err != nil:
			return res, err
		}
		outcome := localOutcomeOpen
		if d.Resolved() {
			outcome = localOutcomeResolved
		}
		res.Upstream = &resolve.Upstream{Key: d.Ref(), Outcome: outcome, Title: d.Title}

	default:
		// Unreachable while the grammar mints only the two targets, and an
		// error rather than a guess for the reason policy.go's default case
		// gives: there is no safe answer to invent.
		return res, fmt.Errorf("api: chronicle reference %q has target %q", ref.Token, ref.Target)
	}
	return res, nil
}
