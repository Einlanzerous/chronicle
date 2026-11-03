package mcp_test

// search_notes and get_note (CHRN-66), against the real API handler on the test
// database, plus a stub API for the answers the real router cannot produce.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Einlanzerous/chronicle/internal/mcp"
	"github.com/Einlanzerous/chronicle/internal/resolve"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// projectKeys is a live Switchyard key set that knows SWY and CHRN, so a note
// naming a ticket is recognised as naming one. Without it the API's renderer
// treats SWY-389 as prose and the descriptors would never show a ticket.
func projectKeys(t *testing.T) *resolve.Keys {
	t.Helper()
	keys := resolve.NewKeys(resolve.KeysOptions{
		Fetch: func(context.Context) ([]string, error) { return []string{"SWY", "CHRN"}, nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go keys.Run(ctx)
	for deadline := time.Now().Add(2 * time.Second); !keys.Ready(); {
		if time.Now().After(deadline) {
			t.Fatal("the key set never loaded")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return keys
}

// callTool calls a tool and decodes its structured answer into out. It fails
// the test if the tool answered an error; callToolErr is for the ones that must.
func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any, out any) {
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
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
}

// callToolErr calls a tool that must answer an error, and returns its text.
func callToolErr(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("%s: want an error, got %+v", name, res.StructuredContent)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

type noteWorld struct {
	*world
	bot  store.User
	page store.Page
}

func newNoteWorld(t *testing.T) *noteWorld {
	t.Helper()
	w := newWorld(t)
	bot, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	page, err := w.st.CreatePage(w.ctx, nil, "field-notes")
	if err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	return &noteWorld{world: w, bot: bot, page: page}
}

func (n *noteWorld) note(t *testing.T, title, body string) store.Note {
	t.Helper()
	note, _, err := n.st.CreateNote(n.ctx, store.NewNote{
		PageID: n.page.ID, AuthorID: n.owner.ID, ConfirmedBy: n.owner.ID, Title: title, Body: body,
	})
	if err != nil {
		t.Fatalf("CreateNote: %v", err)
	}
	return note
}

var sessionKinds = []struct {
	who       string
	agent     bool
	transport mcp.Transport
}{
	{"a hosted person", false, mcp.TransportHosted},
	{"a stdio agent", true, mcp.TransportStdio},
}

func (n *noteWorld) forEachSession(t *testing.T, fn func(t *testing.T, cs *sdk.ClientSession, rec *recorder)) {
	t.Helper()
	for _, k := range sessionKinds {
		t.Run(k.who, func(t *testing.T) {
			user := n.owner
			if k.agent {
				user = n.bot
			}
			cs, rec := n.session(t, user, k.transport)
			rec.reset()
			fn(t, cs, rec)
		})
	}
}

func TestSearchNotesFindsANoteByAWordInItsBody(t *testing.T) {
	n := newNoteWorld(t)
	seeded := n.note(t, "Heron", "The heron stood very still over the pond.")
	n.note(t, "Other", "Nothing relevant lives here.")

	n.forEachSession(t, func(t *testing.T, cs *sdk.ClientSession, rec *recorder) {
		var got mcp.SearchNotesResult
		callTool(t, cs, "search_notes", map[string]any{"query": "heron"}, &got)
		rec.requireGETOnly(t)
		if got.Availability != mcp.Available || len(got.Items) != 1 || got.Items[0].Ref != seeded.Ref() {
			t.Fatalf("search = %+v, want exactly %s", got, seeded.Ref())
		}
		if got.Items[0].Title != "Heron" || !strings.Contains(got.Items[0].Snippet, "<b>") {
			t.Errorf("hit = %+v", got.Items[0])
		}
		raw, _ := json.Marshal(got)
		if strings.Contains(strings.ToLower(string(raw)), "transcript") {
			t.Errorf("a notes search answer names a transcript: %s", raw)
		}
	})
}

func TestSearchNotesClampsAndDefaultsTheLimit(t *testing.T) {
	n := newNoteWorld(t)
	n.note(t, "Heron", "The heron stood still.")
	cs, rec := n.session(t, n.owner, mcp.TransportHosted)

	// Not positive: the default is sent, never the 0 or -3 the API would 400.
	for _, asked := range []int{0, -3} {
		rec.reset()
		var got mcp.SearchNotesResult
		callTool(t, cs, "search_notes", map[string]any{"query": "heron", "limit": asked}, &got)
		if got.Limit != mcp.DefaultSearchLimit {
			t.Errorf("limit %d: echoed %d, want the default %d", asked, got.Limit, mcp.DefaultSearchLimit)
		}
		if len(rec.requests) != 1 || !strings.Contains(rec.requests[0], "limit=10") {
			t.Errorf("limit %d: requests = %v, want limit=10 sent", asked, rec.requests)
		}
	}
	// Over the cap: clamped and echoed, not refused.
	rec.reset()
	var got mcp.SearchNotesResult
	callTool(t, cs, "search_notes", map[string]any{"query": "heron", "limit": 5000}, &got)
	if got.Limit != mcp.APISearchCap || !got.LimitClamped {
		t.Errorf("over the cap: limit %d clamped=%v, want %d true", got.Limit, got.LimitClamped, mcp.APISearchCap)
	}
	if len(rec.requests) != 1 || !strings.Contains(rec.requests[0], "limit=100") {
		t.Errorf("requests = %v, want limit=100 sent", rec.requests)
	}
}

func TestSearchNotesRefusesAQueryOfOnlyPunctuation(t *testing.T) {
	n := newNoteWorld(t)
	cs, _ := n.session(t, n.owner, mcp.TransportHosted)
	if msg := callToolErr(t, cs, "search_notes", map[string]any{"query": "?!"}); !strings.Contains(msg, "refused") {
		t.Errorf("error = %q, want Chronicle's refusal", msg)
	}
}

func TestGetNoteReturnsTheBodyAndItsReferencesUnresolved(t *testing.T) {
	n := newNoteWorld(t)
	body := "See SWY-389 and CHR-0311, plus amber1.abc.def.0 for the record."
	seeded := n.note(t, "Pointers", body)

	n.forEachSession(t, func(t *testing.T, cs *sdk.ClientSession, rec *recorder) {
		var got mcp.GetNoteResult
		callTool(t, cs, "get_note", map[string]any{"ref": seeded.Ref()}, &got)
		rec.requireGETOnly(t)
		if got.Availability != mcp.Available || got.Body != body || got.Title != "Pointers" || got.Truncated {
			t.Fatalf("note = %+v", got)
		}
		if got.Page != "field-notes" || got.Revision == nil || got.Revision.Seq != 1 || got.Revision.ID == "" {
			t.Errorf("page %q revision %+v", got.Page, got.Revision)
		}
		systems := map[string]string{}
		for _, r := range got.References {
			systems[r.Token] = r.System
		}
		want := map[string]string{"SWY-389": "switchyard", "CHR-0311": "chronicle", "amber1.abc.def.0": "amber"}
		for tok, sys := range want {
			if systems[tok] != sys {
				t.Errorf("reference %s = %q, want %q (all: %v)", tok, systems[tok], sys, systems)
			}
		}
		for _, p := range rec.paths() {
			if strings.Contains(p, "/references/resolve") {
				t.Errorf("get_note resolved a reference: %v", rec.paths())
			}
		}
		if got := rec.paths(); len(got) != 1 || got[0] != "GET /notes/"+seeded.Ref() {
			t.Errorf("requests = %v, want exactly GET /notes/%s", got, seeded.Ref())
		}
	})
}

func TestGetNoteTellsAWithdrawnNoteFromARefThatNamesNothing(t *testing.T) {
	n := newNoteWorld(t)
	gone := n.note(t, "Gone", "Soon withdrawn.")
	if err := n.st.SoftDeleteNote(n.ctx, gone.ID, n.owner.ID); err != nil {
		t.Fatalf("SoftDeleteNote: %v", err)
	}
	cs, _ := n.session(t, n.owner, mcp.TransportHosted)

	var tomb, none mcp.GetNoteResult
	callTool(t, cs, "get_note", map[string]any{"ref": gone.Ref()}, &tomb)
	callTool(t, cs, "get_note", map[string]any{"ref": "CHR-9999"}, &none)

	if tomb.Availability != mcp.Withdrawn || tomb.WithdrawnBy != n.owner.ID.String() || tomb.WithdrawnAt == nil || tomb.Body != "" {
		t.Errorf("withdrawn = %+v", tomb)
	}
	if none.Availability != mcp.NotFound || none.WithdrawnBy != "" {
		t.Errorf("not found = %+v", none)
	}
}

func TestGetNoteCutsAnOverlongBodyAndSaysSo(t *testing.T) {
	n := newNoteWorld(t)
	long := n.note(t, "Long", strings.Repeat("é", mcp.MaxNoteBodyChars+5000))
	cs, _ := n.session(t, n.owner, mcp.TransportHosted)

	var got mcp.GetNoteResult
	callTool(t, cs, "get_note", map[string]any{"ref": long.Ref()}, &got)
	if !got.Truncated || len([]rune(got.Body)) != mcp.MaxNoteBodyChars || got.BodyChars != mcp.MaxNoteBodyChars+5000 {
		t.Errorf("truncated=%v body=%d chars full=%d", got.Truncated, len([]rune(got.Body)), got.BodyChars)
	}
	if got.Revision == nil || got.Revision.ID == "" {
		t.Errorf("a cut body must carry its revision: %+v", got.Revision)
	}
}

// A deployment without the notes store answers 503, which the real router never
// does (serve always wires the wiki), so it is driven by a stub API.
func TestNotesToolsSayTheFeatureIsOffWhenTheAPIAnswers503(t *testing.T) {
	n := newNoteWorld(t)
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusServiceUnavailable)
		_, _ = rw.Write([]byte(`{"code":"wiki_unconfigured","message":"notes are not configured"}`))
	}))
	t.Cleanup(stub.Close)
	api, err := mcp.NewAPI(stub.URL, "token", &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewAPI: %v", err)
	}
	cs := connectSession(t, mcp.Session{
		Identity: mcp.Identity{Transport: mcp.TransportStdio, Kind: string(n.owner.Kind), UserID: n.owner.ID},
		API:      api,
	})

	var search mcp.SearchNotesResult
	callTool(t, cs, "search_notes", map[string]any{"query": "anything"}, &search)
	var note mcp.GetNoteResult
	callTool(t, cs, "get_note", map[string]any{"ref": "CHR-0001"}, &note)
	if search.Availability != mcp.Unconfigured || note.Availability != mcp.Unconfigured {
		t.Errorf("search %q, note %q: want unconfigured, not an error", search.Availability, note.Availability)
	}
	if !strings.Contains(search.Message, "not turned on") || !strings.Contains(note.Message, "not turned on") {
		t.Errorf("messages %q / %q", search.Message, note.Message)
	}
}

// 401 is the session's problem and not an answer about the note.
func TestNotesToolsFailOnA401(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusUnauthorized)
		_, _ = rw.Write([]byte(`{"code":"unauthorized","message":"no"}`))
	}))
	t.Cleanup(stub.Close)
	api, err := mcp.NewAPI(stub.URL, "token", &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("NewAPI: %v", err)
	}
	cs := connectSession(t, mcp.Session{Identity: mcp.Identity{Transport: mcp.TransportStdio, Kind: "agent"}, API: api})
	for name, args := range map[string]map[string]any{
		"search_notes": {"query": "x"}, "get_note": {"ref": "CHR-0001"},
	} {
		if msg := callToolErr(t, cs, name, args); !strings.Contains(strings.ToLower(msg), "refused") {
			t.Errorf("%s: error %q should say the credential was refused", name, msg)
		}
	}
}

func TestNotesToolsDescribeWhatTheyCost(t *testing.T) {
	for _, tool := range mcp.Tools() {
		if tool.Name != "search_notes" && tool.Name != "get_note" {
			continue
		}
		if tool.Mutates {
			t.Errorf("%s declares Mutates", tool.Name)
		}
		for _, want := range []string{"Costs", "Prefer", "Changes nothing"} {
			if tool.Name == "get_note" && want == "Prefer" {
				want = "Use it once"
			}
			if !strings.Contains(tool.Description, want) {
				t.Errorf("%s description lacks %q: %s", tool.Name, want, tool.Description)
			}
		}
	}
}

// connectSession serves sess over an in-memory transport and returns the client
// side, for a session whose API is a stub and not the world's.
func connectSession(t *testing.T, sess mcp.Session) *sdk.ClientSession {
	t.Helper()
	server := mcp.BuildServer(mcp.Build{Version: "test"}, sess, nil)
	ctx, cancel := context.WithCancel(context.Background())
	serverSide, clientSide := sdk.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx, serverSide) }()
	t.Cleanup(func() { cancel(); <-done })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}
