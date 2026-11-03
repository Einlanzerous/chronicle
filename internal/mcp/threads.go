package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/Einlanzerous/chronicle/internal/apiclient"
)

// The thread reads (CHRN-66 row 2): get a thread by its ref, and -- ruling 5's
// second option -- find one that nobody handed over, by learning a page path
// from list_pages and listing the threads filed under it.
//
// All three are GETs through the generated client. get_thread does not mark a
// thread read: that is POST /discussions/{ref}/read, which no tool here calls,
// so reading a thread as the owner leaves their unread count where it was.

// ThreadTurn is one turn of a thread as an agent reads it: raw markdown, in the
// order the server assigned, with who wrote it and what kind of account they
// were when they wrote it.
type ThreadTurn struct {
	Seq        int       `json:"seq" jsonschema:"the turn's place in the thread; 1 is the opening turn"`
	Author     string    `json:"author,omitempty" jsonschema:"the author's display name, when they are a participant"`
	AuthorKind string    `json:"author_kind" jsonschema:"person or agent: the author's kind when the turn was written"`
	Body       string    `json:"body" jsonschema:"the turn, as markdown; references in it are pointers, not resolved"`
	CreatedAt  time.Time `json:"created_at" jsonschema:"when the turn arrived at the server"`
}

// ThreadResult is get_thread's answer.
type ThreadResult struct {
	Status  Availability `json:"status" jsonschema:"ok, not_found, or unconfigured"`
	Message string       `json:"message,omitempty" jsonschema:"what a status other than ok means"`

	Ref      string `json:"ref,omitempty"`
	Title    string `json:"title,omitempty"`
	Page     string `json:"page,omitempty" jsonschema:"the page the thread is filed against; absent for an unfiled thread"`
	Resolved bool   `json:"resolved,omitempty" jsonschema:"true once a person has concluded the thread"`
	// ResolvedNote is the note a resolved thread produced, when it produced one.
	ResolvedNote string `json:"resolved_note,omitempty" jsonschema:"the note the thread produced, when it resolved into one"`

	Turns []ThreadTurn `json:"turns,omitempty" jsonschema:"the newest turns, oldest first"`
	// TurnsOmitted is how many of the OLDEST turns were left out to keep the
	// result small. The newest are always kept; the opening turn is not.
	TurnsOmitted int `json:"turns_omitted,omitempty" jsonschema:"how many of the oldest turns were left out; 0 or absent when the whole thread is here"`
}

// ThreadRef is get_thread's input.
type ThreadRef struct {
	Ref string `json:"ref" jsonschema:"the thread's reference, such as DSC-0007"`
}

var getThread = define("get_thread",
	"Read one discussion thread by its reference (DSC-0007): its title, the page it is filed on, "+
		"whether it was resolved, and its turns in order, each with its author and the kind of account "+
		"(person or agent) that wrote it. Reads only: it does not mark the thread read for anyone. "+
		"Costs one call and returns at most the newest 100 turns, saying how many older ones it left out. "+
		"Use it once you have a reference, from a note's text or from list_threads; "+
		"to find a thread you were not handed, use list_pages and then list_threads.",
	false,
	func(ctx context.Context, sess Session, in ThreadRef) (ThreadResult, error) {
		resp, err := sess.API.GetDiscussionWithResponse(ctx, in.Ref)
		if err != nil {
			return ThreadResult{}, fmt.Errorf("reach the Chronicle API: %w", err)
		}
		avail, err := Classify("GET /discussions/{ref}", resp.StatusCode())
		if err != nil {
			return ThreadResult{}, err
		}
		switch avail {
		case NotFound:
			return ThreadResult{Status: NotFound, Message: "no thread has the reference " + in.Ref}, nil
		case Unconfigured:
			return ThreadResult{Status: Unconfigured, Message: FeatureOff("Discussion threads")}, nil
		case Withdrawn:
			// The thread API does not withdraw; a 410 here is an API this
			// build does not know, and saying so beats guessing.
			return ThreadResult{}, fmt.Errorf("GET /discussions/{ref} answered 410, which this tool does not know")
		}
		if resp.JSON200 == nil {
			return ThreadResult{}, fmt.Errorf("GET /discussions/{ref} answered %d with no thread", resp.StatusCode())
		}
		return threadResult(resp.JSON200), nil
	})

// threadResult shapes an API thread for a model: raw markdown and no rendered
// HTML, the newest MaxThreadTurns turns, and the count of those left out.
func threadResult(th *apiclient.Thread) ThreadResult {
	d := th.Discussion
	out := ThreadResult{Status: Available, Ref: d.Ref, Title: d.Title, Resolved: d.Resolved != nil}
	if d.Page != nil {
		out.Page = *d.Page
	}
	if d.Resolved != nil && d.Resolved.Note != nil {
		out.ResolvedNote = *d.Resolved.Note
	}
	names := make(map[string]string, len(th.Participants))
	for _, p := range th.Participants {
		names[p.UserId.String()] = p.DisplayName
	}
	turns := th.Turns
	if len(turns) > MaxThreadTurns {
		out.TurnsOmitted = len(turns) - MaxThreadTurns
		turns = turns[out.TurnsOmitted:]
	}
	out.Turns = make([]ThreadTurn, 0, len(turns))
	for _, t := range turns {
		out.Turns = append(out.Turns, ThreadTurn{
			Seq: t.Seq, Author: names[t.AuthorId.String()], AuthorKind: string(t.AuthorKind),
			Body: t.Body, CreatedAt: t.CreatedAt,
		})
	}
	return out
}

// PagesResult is list_pages' answer.
type PagesResult struct {
	Status  Availability `json:"status" jsonschema:"ok or unconfigured"`
	Message string       `json:"message,omitempty"`
	Paths   []string     `json:"paths,omitempty" jsonschema:"every page's path, sorted; the tree is in the strings"`
}

var listPages = define("list_pages",
	"List every page path in Chronicle's notes tree, sorted, such as estate/conventions/naming. "+
		"Reads only, one call, and the whole tree comes back at once, so it grows with the corpus. "+
		"Use it to learn a page path you do not have; the path is what list_threads asks for. "+
		"It lists pages, not the notes or threads on them.",
	false,
	func(ctx context.Context, sess Session, _ struct{}) (PagesResult, error) {
		resp, err := sess.API.ListPagesWithResponse(ctx)
		if err != nil {
			return PagesResult{}, fmt.Errorf("reach the Chronicle API: %w", err)
		}
		avail, err := Classify("GET /pages", resp.StatusCode())
		if err != nil {
			return PagesResult{}, err
		}
		if avail == Unconfigured {
			return PagesResult{Status: Unconfigured, Message: FeatureOff("The notes wiki")}, nil
		}
		if resp.JSON200 == nil {
			return PagesResult{}, fmt.Errorf("GET /pages answered %d with no page tree", resp.StatusCode())
		}
		return PagesResult{Status: Available, Paths: resp.JSON200.Paths}, nil
	})

// ThreadsQuery is list_threads' input.
type ThreadsQuery struct {
	Page   string `json:"page" jsonschema:"a page path, such as estate/conventions/naming; list_pages finds them"`
	Limit  int    `json:"limit,omitempty" jsonschema:"how many threads to return; the default is 10, over 200 is cut to 200, and 0 or less means the default"`
	Cursor string `json:"cursor,omitempty" jsonschema:"the next_cursor of the previous answer; omit for the first page"`
}

// ThreadSummary is one line of list_threads.
type ThreadSummary struct {
	Ref       string    `json:"ref"`
	Title     string    `json:"title"`
	Resolved  bool      `json:"resolved,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ThreadsResult is list_threads' answer.
type ThreadsResult struct {
	Status  Availability `json:"status" jsonschema:"ok, not_found (no such page), or unconfigured"`
	Message string       `json:"message,omitempty"`

	Page      string          `json:"page,omitempty" jsonschema:"the page asked about"`
	MovedFrom string          `json:"moved_from,omitempty" jsonschema:"set when the path asked for was an old one that now redirects"`
	Threads   []ThreadSummary `json:"threads,omitempty"`
	// NextCursor is absent at the end; pass it back as cursor for the rest.
	NextCursor string `json:"next_cursor,omitempty" jsonschema:"pass back as cursor to read the next page; absent at the end"`
	// Limit is the limit the call was made with, and LimitClamped says it
	// differs from the one asked for.
	Limit        int  `json:"limit" jsonschema:"the limit this call was made with"`
	LimitClamped bool `json:"limit_clamped,omitempty" jsonschema:"true when that differs from the limit asked for"`
}

var listThreads = define("list_threads",
	"List the discussion threads filed against one page, ten at a time by default. "+
		"Reads only: one call per page of results; pass next_cursor back to continue. "+
		"A thread that was never filed against a page is not listed here and cannot be found this way; "+
		"get_thread reaches it by reference. Use list_pages first if you do not have the page path.",
	false,
	func(ctx context.Context, sess Session, in ThreadsQuery) (ThreadsResult, error) {
		limit, clamped := ClampLimit(in.Limit, DefaultListLimit, APIListCap)
		params := &apiclient.ListDiscussionsParams{Page: in.Page, Limit: &limit}
		if in.Cursor != "" {
			params.Cursor = &in.Cursor
		}
		resp, err := sess.API.ListDiscussionsWithResponse(ctx, params)
		if err != nil {
			return ThreadsResult{}, fmt.Errorf("reach the Chronicle API: %w", err)
		}
		avail, err := Classify("GET /discussions", resp.StatusCode())
		if err != nil {
			return ThreadsResult{}, err
		}
		out := ThreadsResult{Status: avail, Limit: limit, LimitClamped: clamped}
		switch avail {
		case NotFound:
			out.Message = "no page has the path " + in.Page + "; list_pages lists the paths that exist"
			return out, nil
		case Unconfigured:
			out.Message = FeatureOff("Discussion threads")
			return out, nil
		case Withdrawn:
			return ThreadsResult{}, fmt.Errorf("GET /discussions answered 410, which this tool does not know")
		}
		if resp.JSON200 == nil {
			return ThreadsResult{}, fmt.Errorf("GET /discussions answered %d with no list", resp.StatusCode())
		}
		list := resp.JSON200
		out.Page = list.Page.Path
		if list.MovedFrom != nil {
			out.MovedFrom = *list.MovedFrom
		}
		if list.NextCursor != nil {
			out.NextCursor = *list.NextCursor
		}
		out.Threads = make([]ThreadSummary, 0, len(list.Items))
		for _, d := range list.Items {
			out.Threads = append(out.Threads, ThreadSummary{
				Ref: d.Ref, Title: d.Title, Resolved: d.Resolved != nil, CreatedAt: d.CreatedAt,
			})
		}
		return out, nil
	})
