package mcp_test

// The thread reads (CHRN-66 row 2), through a real API and database. Every
// call goes through the GET-only recorder: that these tools change nothing is
// a thing the harness saw, not a thing their authors declared.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Einlanzerous/chronicle/internal/mcp"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// callThreadTool calls a tool and decodes its structured answer into Out.
func callThreadTool[Out any](t *testing.T, cs *sdk.ClientSession, name string, args any) Out {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("%s answered an error: %+v", name, res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return out
}

// seedThreads files three threads under estate/conventions, one unfiled, and
// returns the three refs and the unfiled one.
func seedThreads(t *testing.T, w *world) (filed []string, unfiled string) {
	t.Helper()
	estate, err := w.st.CreatePage(w.ctx, nil, "estate")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	conv, err := w.st.CreatePage(w.ctx, &estate.ID, "conventions")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	for i := 1; i <= 3; i++ {
		d, _, err := w.st.OpenDiscussion(w.ctx, store.NewDiscussion{
			Title: fmt.Sprintf("naming %d", i), PageID: &conv.ID, AuthorID: w.owner.ID, Body: "opening",
		})
		if err != nil {
			t.Fatalf("OpenDiscussion: %v", err)
		}
		filed = append(filed, store.FormatDiscussionRef(d.Number))
	}
	d, _, err := w.st.OpenDiscussion(w.ctx, store.NewDiscussion{Title: "no page", AuthorID: w.owner.ID, Body: "opening"})
	if err != nil {
		t.Fatalf("OpenDiscussion: %v", err)
	}
	return filed, store.FormatDiscussionRef(d.Number)
}

// unreadFor reads a thread over HTTP as user and returns the unread count the
// API reports for them, which is what get_thread must not change.
func unreadFor(t *testing.T, w *world, user store.User, ref string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, w.apiURL+"/discussions/"+ref, nil)
	req.Header.Set("Authorization", "Bearer "+w.sessionFor(t, user, "unread-probe"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", ref, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var th struct {
		Unread *int `json:"unread"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&th); err != nil || th.Unread == nil {
		t.Fatalf("decode unread for %s: %v (unread %v)", ref, err, th.Unread)
	}
	return *th.Unread
}

func TestGetThreadReturnsTurnsInOrderAndDoesNotMarkRead(t *testing.T) {
	w := newWorld(t)
	bot, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	filed, _ := seedThreads(t, w)
	ref := filed[0]
	d, err := w.st.DiscussionByRef(w.ctx, ref)
	if err != nil {
		t.Fatalf("DiscussionByRef: %v", err)
	}
	if err := w.st.AddParticipant(w.ctx, d.ID, bot.ID, w.owner.ID); err != nil {
		t.Fatalf("AddParticipant: %v", err)
	}
	if _, err := w.st.AppendTurn(w.ctx, store.NewTurn{DiscussionID: d.ID, AuthorID: bot.ID, Body: "a durable transcript"}); err != nil {
		t.Fatalf("AppendTurn: %v", err)
	}
	if got := unreadFor(t, w, w.owner, ref); got != 1 {
		t.Fatalf("setup: owner unread = %d, want 1", got)
	}

	for _, tc := range []struct {
		name      string
		user      store.User
		transport mcp.Transport
	}{
		{"a hosted person", w.owner, mcp.TransportHosted},
		{"a stdio agent", bot, mcp.TransportStdio},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, rec := w.session(t, tc.user, tc.transport)
			rec.reset()
			got := callThreadTool[mcp.ThreadResult](t, cs, "get_thread", map[string]any{"ref": ref})
			rec.requireGETOnly(t)
			if p := rec.paths(); len(p) != 1 || p[0] != "GET /discussions/"+ref {
				t.Errorf("recorded %v, want exactly GET /discussions/%s", p, ref)
			}
			if got.Status != mcp.Available || got.Ref != ref || got.Page != "estate/conventions" {
				t.Fatalf("result = %+v", got)
			}
			if len(got.Turns) != 2 || got.Turns[0].Seq != 1 || got.Turns[1].Seq != 2 {
				t.Fatalf("turns = %+v, want seq 1 then 2", got.Turns)
			}
			if got.Turns[0].AuthorKind != "person" || got.Turns[1].AuthorKind != "agent" || got.Turns[1].Author != "Bot" {
				t.Errorf("authors = %+v", got.Turns)
			}
			if got.Turns[0].Body != "opening" {
				t.Errorf("body = %q, want the raw markdown", got.Turns[0].Body)
			}
			if got.TurnsOmitted != 0 {
				t.Errorf("turns_omitted = %d on a two-turn thread", got.TurnsOmitted)
			}
		})
	}
	// Both reads happened; the owner has still not read the agent's turn.
	if got := unreadFor(t, w, w.owner, ref); got != 1 {
		t.Errorf("owner unread after get_thread = %d, want 1: reading through the tool marked the thread read", got)
	}
}

func TestGetThreadKeepsTheNewestTurnsAndSaysWhatItLeftOut(t *testing.T) {
	w := newWorld(t)
	filed, _ := seedThreads(t, w)
	d, err := w.st.DiscussionByRef(w.ctx, filed[0])
	if err != nil {
		t.Fatalf("DiscussionByRef: %v", err)
	}
	extra := 3
	for i := 0; i < mcp.MaxThreadTurns+extra-1; i++ { // turn 1 is the opening
		if _, err := w.st.AppendTurn(w.ctx, store.NewTurn{DiscussionID: d.ID, AuthorID: w.owner.ID, Body: fmt.Sprintf("turn %d", i+2)}); err != nil {
			t.Fatalf("AppendTurn %d: %v", i, err)
		}
	}
	cs, _ := w.session(t, w.owner, mcp.TransportStdio)
	got := callThreadTool[mcp.ThreadResult](t, cs, "get_thread", map[string]any{"ref": filed[0]})
	if len(got.Turns) != mcp.MaxThreadTurns || got.TurnsOmitted != extra {
		t.Fatalf("turns = %d omitted = %d, want %d and %d", len(got.Turns), got.TurnsOmitted, mcp.MaxThreadTurns, extra)
	}
	if got.Turns[0].Seq != extra+1 || got.Turns[len(got.Turns)-1].Seq != mcp.MaxThreadTurns+extra {
		t.Errorf("kept seq %d..%d, want the newest %d", got.Turns[0].Seq, got.Turns[len(got.Turns)-1].Seq, mcp.MaxThreadTurns)
	}
}

func TestGetThreadAnswersAnUnknownRefDistinctly(t *testing.T) {
	w := newWorld(t)
	cs, _ := w.session(t, w.owner, mcp.TransportStdio)
	got := callThreadTool[mcp.ThreadResult](t, cs, "get_thread", map[string]any{"ref": "DSC-9999"})
	if got.Status != mcp.NotFound || got.Message == "" || len(got.Turns) != 0 {
		t.Errorf("result = %+v, want not_found with a message and no turns", got)
	}
}

func TestListPagesAndListThreads(t *testing.T) {
	w := newWorld(t)
	bot, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	filed, unfiled := seedThreads(t, w)
	cs, rec := w.session(t, bot, mcp.TransportStdio)

	rec.reset()
	pages := callThreadTool[mcp.PagesResult](t, cs, "list_pages", struct{}{})
	rec.requireGETOnly(t)
	if pages.Status != mcp.Available || !slices.Contains(pages.Paths, "estate") || !slices.Contains(pages.Paths, "estate/conventions") {
		t.Fatalf("pages = %+v", pages)
	}

	// Two at a time, then the cursor for the third.
	first := callThreadTool[mcp.ThreadsResult](t, cs, "list_threads", map[string]any{"page": "estate/conventions", "limit": 2})
	if first.Status != mcp.Available || len(first.Threads) != 2 || first.NextCursor == "" || first.Limit != 2 || first.LimitClamped {
		t.Fatalf("first page = %+v", first)
	}
	second := callThreadTool[mcp.ThreadsResult](t, cs, "list_threads",
		map[string]any{"page": "estate/conventions", "limit": 2, "cursor": first.NextCursor})
	if len(second.Threads) != 1 || second.NextCursor != "" {
		t.Fatalf("second page = %+v, want one thread and no cursor", second)
	}
	var seen []string
	for _, th := range append(first.Threads, second.Threads...) {
		seen = append(seen, th.Ref)
	}
	slices.Sort(seen)
	if !slices.Equal(seen, filed) {
		t.Errorf("listed %v, want %v", seen, filed)
	}
	if slices.Contains(seen, unfiled) {
		t.Errorf("the unfiled thread %s was listed", unfiled)
	}

	// An unknown page is a result of its own, not an error and not an empty list.
	none := callThreadTool[mcp.ThreadsResult](t, cs, "list_threads", map[string]any{"page": "no/such/page"})
	if none.Status != mcp.NotFound || none.Message == "" {
		t.Errorf("unknown page = %+v, want not_found", none)
	}
}

// Each API family carries its own limit check, so list_threads is driven
// alone: a limit that is not positive becomes the default and is not sent to
// be refused with a 400, and one over the cap is cut to it and echoed.
func TestListThreadsClampsItsLimit(t *testing.T) {
	w := newWorld(t)
	seedThreads(t, w)
	cs, rec := w.session(t, w.owner, mcp.TransportStdio)
	for _, tc := range []struct {
		asked, used int
		clamped     bool
	}{
		{0, mcp.DefaultListLimit, false},
		{-5, mcp.DefaultListLimit, true},
		{100000, mcp.APIListCap, true},
	} {
		rec.reset()
		got := callThreadTool[mcp.ThreadsResult](t, cs, "list_threads", map[string]any{"page": "estate/conventions", "limit": tc.asked})
		if got.Status != mcp.Available || got.Limit != tc.used || got.LimitClamped != tc.clamped {
			t.Errorf("limit %d: status %q used %d clamped %v, want ok %d %v", tc.asked, got.Status, got.Limit, got.LimitClamped, tc.used, tc.clamped)
		}
	}
}

// serve always wires the wiki and the threads, so the real router cannot
// answer their 503s; a stub API can, and the tools must say the feature is
// off rather than fail the call.
func TestThreadToolsSayWhenTheFeatureIsOff(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusServiceUnavailable)
		_, _ = rw.Write([]byte(`{"code":"threads_unconfigured","message":"off"}`))
	}))
	t.Cleanup(stub.Close)
	api, err := mcp.NewAPI(stub.URL, "token", &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewAPI: %v", err)
	}
	sess := mcp.Session{Identity: mcp.Identity{Transport: mcp.TransportStdio, Kind: mcp.KindPerson, UserID: uuid.New(), Name: "p"}, API: api}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	serverSide, clientSide := sdk.NewInMemoryTransports()
	go func() { _ = mcp.BuildServer(mcp.Build{Version: "test"}, sess, nil).Run(ctx, serverSide) }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	if got := callThreadTool[mcp.ThreadResult](t, cs, "get_thread", map[string]any{"ref": "DSC-0001"}); got.Status != mcp.Unconfigured || got.Message == "" {
		t.Errorf("get_thread = %+v", got)
	}
	if got := callThreadTool[mcp.PagesResult](t, cs, "list_pages", struct{}{}); got.Status != mcp.Unconfigured || got.Message == "" {
		t.Errorf("list_pages = %+v", got)
	}
	if got := callThreadTool[mcp.ThreadsResult](t, cs, "list_threads", map[string]any{"page": "x"}); got.Status != mcp.Unconfigured || got.Message == "" {
		t.Errorf("list_threads = %+v", got)
	}
}
