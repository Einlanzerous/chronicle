package api

import (
	"errors"
	"net/http"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/estatewiki"
)

// Tier 1 over HTTP (CHRN-100): the generated estate wiki, from a read-only
// mount, and the marking that makes every generated payload unmistakable.
//
// ============================================================================
// THE CONTENT IS NOT IN A DATABASE, AND THIS FILE DIALS NEITHER POOL.
// ============================================================================
//
// SERV-101's wiki is a markdown corpus construct-server generates on every
// deploy. SERV-189 publishes it to the deploy root and bind-mounts it here
// read-only; internal/estatewiki reads the files. So "tier 1 through the
// tier-1 pool", which the ticket was written to say, is not what this is —
// the decision recorded on CHRN-100 (2026-09-14) is that the kernel's `:ro`
// is the enforcement for this half of tier 1, and the pool and its
// credential are the enforcement for the other half, the derived rows.
//
// ============================================================================
// EVERY TIER-1 PAYLOAD CARRIES `generated`, INCLUDING THE ONE THAT PREDATES
// THIS FILE.
// ============================================================================
//
// The Scribe's proposals were the pool serving tier 1 over HTTP before this
// ticket, unmarked, inside the triage batch. toProposal now stamps them with
// the same component and `source: chronicle`, because the marking is a fact
// about which store a thing lives in, not about which route returned it.

// The notice is the client epic's line, carried here so CHRN-58 renders what
// the ticket specified rather than what a client remembered. One per source,
// because "Regenerated from SERV" would be false of a proposal.
const (
	noticeServ      = "Regenerated from SERV. Separate store — nothing here can overwrite tier 2."
	noticeChronicle = "Derived by Chronicle from its own corpus. Separate store — nothing here can overwrite tier 2."
)

// tier1Unavailable answers a router assembled with no corpus behind it —
// wikiUnavailable's shape. serve refuses to boot without one, so no
// deployment reaches this.
func (a *api) tier1Unavailable(w http.ResponseWriter) bool {
	if a.estate != nil {
		return false
	}
	writeError(w, http.StatusServiceUnavailable, codeTier1Unconfigured,
		"this router was assembled with no tier-1 corpus behind it")
	return true
}

// generatedByServ is the marking for a page of the corpus, carrying the
// generator's stamp when the corpus has one. ok false leaves ref and
// generated_at absent: the document says they are never invented.
func generatedByServ(b estatewiki.Build, ok bool) wire.Generated {
	out := wire.Generated{
		Tier:        wire.GeneratedTierOne,
		Source:      wire.GeneratedSourceServ,
		Regenerable: wire.GeneratedRegenerableTrue,
		Notice:      noticeServ,
	}
	if ok {
		if b.Ref != "" {
			out.Ref = &b.Ref
		}
		if !b.GeneratedAt.IsZero() {
			out.GeneratedAt = &b.GeneratedAt
		}
	}
	return out
}

// generatedByChronicle is the marking for a row Chronicle derived itself.
func generatedByChronicle() wire.Generated {
	return wire.Generated{
		Tier:        wire.GeneratedTierOne,
		Source:      wire.GeneratedSourceChronicle,
		Regenerable: wire.GeneratedRegenerableTrue,
		Notice:      noticeChronicle,
	}
}

// stamp reads the corpus's build stamp for a payload. A stamp that is present
// and unreadable is a server error rather than an absent field: a corpus
// whose build.json is corrupt is a deploy that went wrong, and a payload that
// quietly dropped the ref would hide it.
func (a *api) stamp(w http.ResponseWriter, r *http.Request, what string) (wire.Generated, bool) {
	b, ok, err := a.estate.Build()
	if err != nil {
		a.serverError(w, r, what+": build stamp", err)
		return wire.Generated{}, false
	}
	return generatedByServ(b, ok), true
}

func (a *api) ListTier1Pages(w http.ResponseWriter, r *http.Request) {
	if a.tier1Unavailable(w) {
		return
	}
	pages, err := a.estate.List()
	if err != nil {
		a.serverError(w, r, "list tier-1 pages", err)
		return
	}
	generated, ok := a.stamp(w, r, "list tier-1 pages")
	if !ok {
		return
	}
	items := make([]wire.Tier1PageSummary, 0, len(pages))
	for _, p := range pages {
		items = append(items, wire.Tier1PageSummary{Path: p.Path, Title: p.Title})
	}
	writeJSON(w, http.StatusOK, wire.Tier1PageList{Items: items, Generated: generated})
}

func (a *api) GetTier1Page(w http.ResponseWriter, r *http.Request, params wire.GetTier1PageParams) {
	if a.tier1Unavailable(w) {
		return
	}
	// Validated here as well as in the corpus so the refusal is a 400 naming
	// the parameter, and so the check happens before anything touches disk.
	if !estatewiki.ValidPath(params.Path) {
		writeError(w, http.StatusBadRequest, codeInvalidParameter,
			"path must be relative, slash-separated, without an empty or dot-prefixed segment, and without .md")
		return
	}
	page, err := a.estate.Read(params.Path)
	switch {
	case errors.Is(err, estatewiki.ErrNotFound):
		writeError(w, http.StatusNotFound, codeNotFound, "no such page in the generated corpus")
		return
	case errors.Is(err, estatewiki.ErrInvalidPath):
		writeError(w, http.StatusBadRequest, codeInvalidParameter, "path is not one the corpus could hold")
		return
	case err != nil:
		a.serverError(w, r, "get tier-1 page", err)
		return
	}
	html, err := a.renderer.Render([]byte(page.Body))
	if err != nil {
		a.serverError(w, r, "get tier-1 page: render", err)
		return
	}
	generated, ok := a.stamp(w, r, "get tier-1 page")
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, wire.Tier1Page{
		Path:      page.Path,
		Title:     page.Title,
		Body:      page.Body,
		Html:      string(html),
		Generated: generated,
	})
}
