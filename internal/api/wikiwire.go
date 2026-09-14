package api

import (
	"html"
	"strings"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// E5's types, mapped onto the generated ones. Transcribed rather than aliased,
// on triagewire.go's argument, and checked both ways by
// TestEveryResolutionFieldReachesTheDocument's projection tables.

// toPage renders a page beside the path it was reached by. The store's Page
// carries no path — a path is a fact about ancestry, computed at read time —
// so the caller supplies the one it resolved.
func toPage(p store.Page, path string) wire.Page {
	return wire.Page{
		Id:        p.ID,
		Path:      path,
		Slug:      p.Slug,
		ParentId:  p.ParentID,
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func toNoteSummary(n store.Note, rev store.NoteRevision, path string) wire.NoteSummary {
	return wire.NoteSummary{
		Ref:       n.Ref(),
		Title:     rev.Title,
		Page:      path,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.UpdatedAt,
	}
}

func toRevisionMeta(rv store.NoteRevision) wire.RevisionMeta {
	out := wire.RevisionMeta{
		Id:           rv.ID,
		Seq:          rv.Seq,
		CreatedAt:    rv.CreatedAt,
		AuthorId:     rv.AuthorID,
		ConfirmedBy:  rv.ConfirmedBy,
		MemoId:       rv.MemoID,
		RestoredFrom: rv.RestoredFrom,
	}
	if rv.Verb != nil {
		v := wire.RevisionMetaVerb(*rv.Verb)
		out.Verb = &v
	}
	return out
}

func toRevision(rv store.NoteRevision) wire.Revision {
	out := wire.Revision{
		Id:           rv.ID,
		Seq:          rv.Seq,
		Title:        rv.Title,
		Body:         rv.Body,
		CreatedAt:    rv.CreatedAt,
		AuthorId:     rv.AuthorID,
		ConfirmedBy:  rv.ConfirmedBy,
		MemoId:       rv.MemoID,
		RestoredFrom: rv.RestoredFrom,
	}
	if rv.Verb != nil {
		v := wire.RevisionVerb(*rv.Verb)
		out.Verb = &v
	}
	return out
}

// toDiscussionSummaries renders the threads that concluded into a note. Every
// row here is resolved by construction — the query is on resolved_note_id —
// so resolved_at is required on the wire and dereferenced here.
func toDiscussionSummaries(in []store.Discussion) []wire.DiscussionSummary {
	out := make([]wire.DiscussionSummary, 0, len(in))
	for _, d := range in {
		s := wire.DiscussionSummary{Ref: d.Ref(), Title: d.Title}
		if d.ResolvedAt != nil {
			s.ResolvedAt = *d.ResolvedAt
		}
		out = append(out, s)
	}
	return out
}

// toSearchHits renders hits with the field set their kind says. A note hit
// carries ref and title; a transcript hit carries memo_id and model; nothing
// carries both, and the page id a note hit holds in the store is not on the
// wire — a client follows the ref.
func toSearchHits(in []store.SearchHit) []wire.SearchHit {
	out := make([]wire.SearchHit, 0, len(in))
	for _, h := range in {
		hit := wire.SearchHit{
			Kind:      wire.SearchHitKind(h.Kind),
			Snippet:   safeSnippet(h.Snippet),
			Rank:      h.Rank,
			CreatedAt: h.CreatedAt,
		}
		switch h.Kind {
		case store.HitNote:
			ref := h.Ref()
			hit.Ref = &ref
			hit.Title = optional(h.Title)
		case store.HitTranscript:
			hit.MemoId = h.MemoID
			hit.Model = optional(h.Model)
		}
		out = append(out, hit)
	}
	return out
}

// safeSnippet makes a ts_headline fragment safe to embed.
//
// ts_headline wraps each match in <b>…</b> and returns THE REST OF THE
// DOCUMENT VERBATIM -- Postgres escapes nothing and offers no option to. A
// note body is stored raw (CHRN-40), so a snippet of one carrying
// `<img onerror=…>` would reach a client whose document tells it the field is
// HTML, and Note.html's whole argument -- "the renderer is the only thing that
// writes HTML here" -- would be undone by the field beside it.
//
// So everything is escaped, and then exactly the two bare tags ts_headline
// writes are put back. Nothing with an attribute can survive: the only way to
// get `&lt;b&gt;` after escaping is a literal `<b>` in the text, and a bare
// <b> is a bold mark and never markup. The one imprecision is that a `<b>`
// somebody typed into a note reads as a highlight, which is a cosmetic wrong
// rather than an injection; the clean fix is sentinel StartSel/StopSel in the
// store's ts_headline call, which is internal/store's to make and is raised on
// the ticket.
func safeSnippet(s string) string {
	escaped := html.EscapeString(s)
	escaped = strings.ReplaceAll(escaped, "&lt;b&gt;", "<b>")
	return strings.ReplaceAll(escaped, "&lt;/b&gt;", "</b>")
}
