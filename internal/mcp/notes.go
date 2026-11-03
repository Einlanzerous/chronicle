package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Einlanzerous/chronicle/internal/apiclient"
)

// The two note reads (CHRN-66): find a note by what it says, and read one whose
// ref is known. Both are over the generated client, both are member-readable,
// and neither reaches a transcript: a note is what a person wrote and confirmed,
// and search never answers with what was said and not written down.

// SearchNotesInput is search_notes's arguments.
type SearchNotesInput struct {
	Query string `json:"query" jsonschema:"words to find in a note's title or body; a query made only of punctuation is refused"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many notes to return; default 10, at most 100; a value over the cap is clamped and echoed, one that is not positive means the default"`
}

// NoteHit is one search result.
type NoteHit struct {
	Ref       string    `json:"ref" jsonschema:"the note's permanent ref, e.g. CHR-0311; pass it to get_note"`
	Title     string    `json:"title,omitempty" jsonschema:"the current title; absent when the note has none"`
	Snippet   string    `json:"snippet" jsonschema:"up to two fragments around the match, joined by an ellipsis; each match is wrapped in <b> and everything else is HTML-escaped"`
	Rank      float32   `json:"rank" jsonschema:"relevance; higher is better"`
	CreatedAt time.Time `json:"created_at" jsonschema:"when the note was first written"`
}

// SearchNotesResult is search_notes's answer.
type SearchNotesResult struct {
	Availability Availability `json:"availability" jsonschema:"ok, or unconfigured when notes are not turned on in this deployment"`
	Message      string       `json:"message,omitempty" jsonschema:"set when availability is not ok"`
	Query        string       `json:"query" jsonschema:"the query as Chronicle read it"`
	Limit        int          `json:"limit" jsonschema:"the limit this call was made with, after clamping"`
	LimitClamped bool         `json:"limit_clamped,omitempty" jsonschema:"true when the limit asked for was not the limit used"`
	Items        []NoteHit    `json:"items" jsonschema:"best match first; not a page of anything, there is no cursor"`
}

var searchNotes = define("search_notes", fmt.Sprintf(
	"Find notes by what they say. Returns the best matches first, each with its ref, title and a short snippet around the match. "+
		"Searches written notes only: never a transcript, and never what was said aloud and not written down, so a thing that was only ever spoken will not turn up here. "+
		"Costs one read; a result is a few hundred characters per note, %d notes by default, %d at most. "+
		"There is no next page: if the note is not in the first results, narrow the words. "+
		"Prefer get_note once you hold a ref. Changes nothing.", DefaultSearchLimit, APISearchCap),
	false,
	func(ctx context.Context, sess Session, in SearchNotesInput) (SearchNotesResult, error) {
		limit, clamped := ClampLimit(in.Limit, DefaultSearchLimit, APISearchCap)
		resp, err := sess.API.SearchNotesWithResponse(ctx, &apiclient.SearchNotesParams{Q: in.Query, Limit: &limit})
		if err != nil {
			return SearchNotesResult{}, fmt.Errorf("reach the Chronicle API: %w", err)
		}
		out := SearchNotesResult{Query: in.Query, Limit: limit, LimitClamped: clamped, Items: []NoteHit{}}
		if resp.StatusCode() == http.StatusBadRequest {
			return out, refused("search_notes", resp.JSON400)
		}
		av, err := Classify("GET /notes/search", resp.StatusCode())
		if err != nil {
			return out, err
		}
		switch av {
		case Available:
		case Unconfigured:
			out.Availability, out.Message = av, FeatureOff("Notes")
			return out, nil
		default:
			return out, fmt.Errorf("GET /notes/search answered %d", resp.StatusCode())
		}
		if resp.JSON200 == nil {
			return out, fmt.Errorf("GET /notes/search answered %d with no body", resp.StatusCode())
		}
		out.Availability, out.Query, out.Limit = Available, resp.JSON200.Query, resp.JSON200.Limit
		for _, h := range resp.JSON200.Items {
			hit := NoteHit{Ref: h.Ref, Snippet: h.Snippet, Rank: h.Rank, CreatedAt: h.CreatedAt}
			if h.Title != nil {
				hit.Title = *h.Title
			}
			out.Items = append(out.Items, hit)
		}
		return out, nil
	})

// GetNoteInput is get_note's argument.
type GetNoteInput struct {
	Ref string `json:"ref" jsonschema:"the note's ref, e.g. CHR-0311, from search_notes or from another note's text"`
}

// Reference is one pointer a note's text makes. It is a pointer and says so:
// nothing upstream was asked, so it carries no title and no state.
type Reference struct {
	Token  string  `json:"token" jsonschema:"the reference exactly as written in the note"`
	System string  `json:"system" jsonschema:"which upstream owns it: switchyard, amber or chronicle"`
	Key    *string `json:"key,omitempty" jsonschema:"the project key, e.g. CHRN; absent for amber"`
	Number *int64  `json:"number,omitempty" jsonschema:"the number after the hyphen; absent for amber"`
	Target *string `json:"target,omitempty" jsonschema:"what a chronicle reference points at, a note or a discussion"`
}

// NoteRevision is which revision of the note the body is, and where it came from.
type NoteRevision struct {
	ID        string    `json:"id" jsonschema:"the revision's id"`
	Seq       int       `json:"seq" jsonschema:"1 for the first revision; only ever grows"`
	AuthorID  string    `json:"author_id" jsonschema:"whose words these are; may be an agent"`
	CreatedAt time.Time `json:"created_at" jsonschema:"when this revision was written"`
}

// NoteSource is a discussion that concluded into the note.
type NoteSource struct {
	Ref        string    `json:"ref" jsonschema:"the discussion's ref, e.g. DSC-0007; pass it to get_thread"`
	Title      string    `json:"title" jsonschema:"the discussion's title"`
	ResolvedAt time.Time `json:"resolved_at" jsonschema:"when it concluded into this note"`
}

// GetNoteResult is get_note's answer. Availability says which of the four facts
// it is: a note, a ref that names nothing, a note that was withdrawn, or a
// deployment without notes. Only the first carries a body.
type GetNoteResult struct {
	Availability Availability `json:"availability" jsonschema:"ok; not_found when the ref names nothing; withdrawn when the note was removed; unconfigured when notes are off in this deployment"`
	Message      string       `json:"message,omitempty" jsonschema:"set when availability is not ok"`
	Ref          string       `json:"ref" jsonschema:"the ref that was asked for"`

	Title        string        `json:"title,omitempty"`
	Page         string        `json:"page,omitempty" jsonschema:"the path of the page the note is filed on, as of this read"`
	Body         string        `json:"body,omitempty" jsonschema:"the current revision's markdown source"`
	Truncated    bool          `json:"truncated,omitempty" jsonschema:"true when body was cut at the tool's limit; body_chars is the full length"`
	BodyChars    int           `json:"body_chars,omitempty" jsonschema:"the body's full length in characters, before any cut"`
	Revision     *NoteRevision `json:"revision,omitempty"`
	ETag         string        `json:"etag,omitempty" jsonschema:"the API's entity tag for this read; informational, this tool never reads conditionally"`
	References   []Reference   `json:"references,omitempty" jsonschema:"pointers the text makes, in order of appearance; a token named three times appears three times; none is resolved"`
	ResolvedFrom []NoteSource  `json:"resolved_from,omitempty" jsonschema:"discussions that concluded into this note"`
	CreatedAt    *time.Time    `json:"created_at,omitempty"`
	UpdatedAt    *time.Time    `json:"updated_at,omitempty"`

	WithdrawnBy string     `json:"withdrawn_by,omitempty" jsonschema:"when withdrawn: the account id that removed it"`
	WithdrawnAt *time.Time `json:"withdrawn_at,omitempty" jsonschema:"when withdrawn: when it was removed"`
}

var getNote = define("get_note", fmt.Sprintf(
	"Read one note by its ref: its title, the page it is filed on, its current body as markdown, which revision that is, and the references its text makes. "+
		"References come back as pointers, as written, with the system that owns them; none is looked up, so a ticket key here carries no title or status. "+
		"A ref that names nothing, a note that was withdrawn (with who and when) and a deployment without notes each come back as their own availability and not as an error. "+
		"Costs one read; a body over %d characters is cut, flagged truncated, and its full length is given. "+
		"Use it once you hold a ref from search_notes or another note; use search_notes to find one. Changes nothing, and does not mark anything read.", MaxNoteBodyChars),
	false,
	func(ctx context.Context, sess Session, in GetNoteInput) (GetNoteResult, error) {
		ref := strings.TrimSpace(in.Ref)
		out := GetNoteResult{Ref: ref}
		if ref == "" {
			return out, fmt.Errorf("get_note needs a ref, e.g. CHR-0311")
		}
		// No If-None-Match: a 304 means nothing to a model.
		resp, err := sess.API.GetNoteWithResponse(ctx, ref, &apiclient.GetNoteParams{})
		if err != nil {
			return out, fmt.Errorf("reach the Chronicle API: %w", err)
		}
		if resp.StatusCode() == http.StatusBadRequest {
			return out, refused("get_note", resp.JSON400)
		}
		av, err := Classify("GET /notes/{ref}", resp.StatusCode())
		if err != nil {
			return out, err
		}
		out.Availability = av
		switch av {
		case NotFound:
			out.Message = "no note has this ref"
			return out, nil
		case Withdrawn:
			out.Message = "this note existed and was withdrawn"
			if t := resp.JSON410; t != nil {
				at := t.DeletedAt
				out.WithdrawnBy, out.WithdrawnAt = t.DeletedBy.String(), &at
			}
			return out, nil
		case Unconfigured:
			out.Message = FeatureOff("Notes")
			return out, nil
		}
		n := resp.JSON200
		if n == nil {
			return out, fmt.Errorf("GET /notes/{ref} answered %d with no body", resp.StatusCode())
		}
		body, cut := Truncate(n.Body, MaxNoteBodyChars)
		out.Ref, out.Title, out.Page, out.Body, out.Truncated = n.Ref, n.Title, n.Page, body, cut
		if cut {
			out.BodyChars = len([]rune(n.Body))
		}
		out.Revision = &NoteRevision{
			ID: n.Revision.Id.String(), Seq: n.Revision.Seq,
			AuthorID: n.Revision.AuthorId.String(), CreatedAt: n.Revision.CreatedAt,
		}
		if resp.Headers200 != nil {
			out.ETag = resp.Headers200.ETag
		}
		for _, r := range n.References {
			pointer := Reference{Token: r.Token, System: string(r.System), Key: r.Key, Number: r.Number}
			if r.Target != nil {
				tg := string(*r.Target)
				pointer.Target = &tg
			}
			out.References = append(out.References, pointer)
		}
		for _, d := range n.ResolvedFrom {
			out.ResolvedFrom = append(out.ResolvedFrom, NoteSource{Ref: d.Ref, Title: d.Title, ResolvedAt: d.ResolvedAt})
		}
		created, updated := n.CreatedAt, n.UpdatedAt
		out.CreatedAt, out.UpdatedAt = &created, &updated
		return out, nil
	})

// refused is the error a tool returns for a 400: Chronicle understood the call
// and refused its arguments, and its sentence is the useful part.
func refused(tool string, e *apiclient.Error) error {
	if e != nil && e.Message != "" {
		return fmt.Errorf("%s: Chronicle refused the call: %s", tool, e.Message)
	}
	return fmt.Errorf("%s: Chronicle refused the call", tool)
}
