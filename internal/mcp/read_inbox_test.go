package mcp_test

// read_inbox (CHRN-167), run through the real API handler on the test database
// and the GET-only harness: whose memos it returns, the one route it asks, the
// limit it sends, and what it says when triage is off.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Einlanzerous/chronicle/internal/mcp"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// transcribedMemo ingests a memo for author and takes it to `transcribed` with
// a durable transcript, which is what puts it in the triage inbox.
func (w *world) transcribedMemo(t *testing.T, author uuid.UUID, text string) store.Memo {
	t.Helper()
	sum := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(sum[:])
	res, err := w.st.IngestMemo(w.ctx, store.Arrival{
		AuthorID: author, ContentHash: hash, ByteSize: int64(len(text)),
		Source: store.SourceUpload, SourceRef: "test/" + hash[:8],
	})
	if err != nil {
		t.Fatalf("IngestMemo: %v", err)
	}
	if _, err := w.st.RecordTranscript(w.ctx, store.TranscriptInput{
		MemoID: res.Memo.ID, Text: text, Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		t.Fatalf("RecordTranscript: %v", err)
	}
	for _, step := range [][2]string{
		{store.StateCaptured, store.StateQueued},
		{store.StateQueued, store.StateTranscribing},
		{store.StateTranscribing, store.StateTranscribed},
	} {
		if _, err := w.st.AdvanceMemoState(w.ctx, res.Memo.ID, step[0], step[1], ""); err != nil {
			t.Fatalf("advance %s -> %s: %v", step[0], step[1], err)
		}
	}
	m, err := w.st.GetMemo(w.ctx, res.Memo.ID)
	if err != nil {
		t.Fatalf("GetMemo: %v", err)
	}
	return m
}

func callReadInbox(t *testing.T, cs *sdk.ClientSession, limit int) mcp.Inbox {
	t.Helper()
	args := map[string]any{}
	if limit != 0 {
		args["limit"] = limit
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "read_inbox", Arguments: args})
	if err != nil {
		t.Fatalf("read_inbox: %v", err)
	}
	if res.IsError {
		t.Fatalf("read_inbox answered an error: %+v", res.Content)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out mcp.Inbox
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode read_inbox: %v", err)
	}
	return out
}

func agentUser(t *testing.T, w *world) store.User {
	t.Helper()
	bot, err := w.st.CreateUser(w.ctx, "bot@example.com", "Bot", store.KindAgent)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return bot
}

func ids(in mcp.Inbox) map[string]bool {
	out := map[string]bool{}
	for _, m := range in.Memos {
		out[m.MemoID] = true
	}
	return out
}

// WHOSE INBOX, in both directions: a stdio agent account gets only what it
// captured itself, and a hosted owner session gets every author's.
func TestReadInboxIsTheInboxOfTheAccountTheSessionActsAs(t *testing.T) {
	w := newWorld(t)
	bot := agentUser(t, w)
	mine := w.transcribedMemo(t, bot.ID, "the agent recorded this one")
	theirs := w.transcribedMemo(t, w.owner.ID, "the owner recorded this one")

	t.Run("a stdio agent sees only its own", func(t *testing.T) {
		cs, rec := w.session(t, bot, mcp.TransportStdio)
		rec.reset()
		got := callReadInbox(t, cs, 0)
		if len(got.Memos) != 1 || got.Memos[0].MemoID != mine.ID.String() {
			t.Fatalf("agent inbox = %+v, want only memo %s", got.Memos, mine.ID)
		}
		if !strings.Contains(got.Memos[0].Excerpt, "the agent recorded") {
			t.Errorf("excerpt = %q, want the transcript", got.Memos[0].Excerpt)
		}
		if ids(got)[theirs.ID.String()] {
			t.Error("the agent was handed a memo the owner captured")
		}
		rec.requireGETOnly(t)
	})

	t.Run("a hosted owner sees every author's, oldest first", func(t *testing.T) {
		cs, _ := w.session(t, w.owner, mcp.TransportHosted)
		got := callReadInbox(t, cs, 0)
		seen := ids(got)
		if len(got.Memos) != 2 || !seen[mine.ID.String()] || !seen[theirs.ID.String()] {
			t.Fatalf("owner inbox = %+v, want both memos", got.Memos)
		}
		if got.Memos[0].CapturedAt.After(got.Memos[1].CapturedAt) {
			t.Errorf("not oldest first: %v then %v", got.Memos[0].CapturedAt, got.Memos[1].CapturedAt)
		}
	})
}

// An agent that captured nothing gets an empty list and a note that says it is
// not the owner's inbox: "nothing for me" must not read as "nothing waiting".
func TestReadInboxExplainsAnAgentsEmptyAnswer(t *testing.T) {
	w := newWorld(t)
	bot := agentUser(t, w)
	w.transcribedMemo(t, w.owner.ID, "waiting for the owner")

	cs, _ := w.session(t, bot, mcp.TransportStdio)
	got := callReadInbox(t, cs, 0)
	if len(got.Memos) != 0 {
		t.Fatalf("agent inbox = %+v, want empty", got.Memos)
	}
	if !strings.Contains(got.Note, "owner's inbox") {
		t.Errorf("note = %q, want it to say this is not the owner's inbox", got.Note)
	}

	owner, _ := w.session(t, w.owner, mcp.TransportStdio)
	if got := callReadInbox(t, owner, 0); got.Note != "" || len(got.Memos) != 1 {
		t.Errorf("owner inbox = %+v, want one memo and no note", got)
	}
}

// read_inbox makes exactly one request, to /triage/batch: wiring it to
// /triage/deferred, or adding a second call, fails here.
func TestReadInboxAsksTriageBatchAndNothingElse(t *testing.T) {
	w := newWorld(t)
	w.transcribedMemo(t, w.owner.ID, "anything")
	cs, rec := w.session(t, w.owner, mcp.TransportStdio)
	rec.reset()
	callReadInbox(t, cs, 0)
	rec.requireGETOnly(t)
	if got := rec.paths(); len(got) != 1 || got[0] != "GET /triage/batch" {
		t.Errorf("recorded %v, want exactly GET /triage/batch", got)
	}
}

// The limit is the tool's: over the cap it is lowered and echoed, and a limit
// of 0 or less is replaced by the default and never sent for the API to answer
// with a 400.
func TestReadInboxClampsItsOwnLimit(t *testing.T) {
	w := newWorld(t)
	for _, text := range []string{"one", "two", "three"} {
		w.transcribedMemo(t, w.owner.ID, text)
	}
	cs, rec := w.session(t, w.owner, mcp.TransportStdio)

	for _, tc := range []struct {
		name        string
		asked       int
		wantLimit   int
		wantClamped bool
		wantMemos   int
		wantQuery   string
	}{
		{"within the cap", 2, 2, false, 2, "limit=2"},
		{"no limit", 0, mcp.DefaultInboxLimit, false, 3, "limit=10"},
		{"over the cap", 99, mcp.APITriageCap, true, 3, "limit=25"},
		{"negative", -3, mcp.DefaultInboxLimit, true, 3, "limit=10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec.reset()
			got := callReadInbox(t, cs, tc.asked)
			if got.Limit != tc.wantLimit || got.LimitClamped != tc.wantClamped || len(got.Memos) != tc.wantMemos {
				t.Errorf("limit=%d clamped=%v memos=%d, want %d %v %d",
					got.Limit, got.LimitClamped, len(got.Memos), tc.wantLimit, tc.wantClamped, tc.wantMemos)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.requests) != 1 || !strings.Contains(rec.requests[0], tc.wantQuery) {
				t.Errorf("requests = %v, want one carrying %s", rec.requests, tc.wantQuery)
			}
		})
	}
}

// With the Scribe unconfigured the API answers 503 triage_unconfigured, and the
// tool says triage is off in this deployment: a result, not a transport error.
func TestReadInboxSaysWhenTriageIsOff(t *testing.T) {
	w := newWorldWithoutTriage(t)
	cs, _ := w.session(t, w.owner, mcp.TransportStdio)
	got := callReadInbox(t, cs, 0)
	if !got.Unconfigured || !strings.Contains(got.Note, "not turned on") || len(got.Memos) != 0 {
		t.Errorf("inbox = %+v, want unconfigured with a note and no memos", got)
	}
}

// The description is the product, and for this tool it must say whose inbox it
// is. Whether it also says what it returns, costs and when to prefer another
// tool is the review half; this pins the sentence an agent cannot do without.
func TestReadInboxDescriptionSaysWhoseInboxItIs(t *testing.T) {
	w := newWorld(t)
	cs, _ := w.session(t, w.owner, mcp.TransportStdio)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var desc string
	for _, tool := range res.Tools {
		if tool.Name == "read_inbox" {
			desc = tool.Description
		}
	}
	for _, want := range []string{"WHOSE INBOX", "the account this session acts as", "agent", "owner", "NOT that the owner's inbox is empty", "25"} {
		if !strings.Contains(desc, want) {
			t.Errorf("read_inbox description does not contain %q:\n%s", want, desc)
		}
	}
}

// One inbox tool: a second route to the same memos (/triage/deferred, an
// unread-threads list) is a different feature with its own plan.
func TestThereIsOneInboxTool(t *testing.T) {
	n := 0
	for _, tool := range mcp.Tools() {
		name := strings.ToLower(tool.Name)
		if strings.Contains(name, "inbox") || strings.Contains(name, "deferred") || strings.Contains(name, "triage") || strings.Contains(name, "unread") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d inbox-shaped tools in Tools(), want exactly read_inbox", n)
	}
}

// A 2xx that is not the JSON batch the contract promises -- an HTML page from a
// proxy, an empty 204, a body that is not JSON -- must come back as a tool
// error. The SDK does not recover a handler's panic, so an unguarded
// resp.JSON200 would take every hosted session down with it. Needs no database:
// the API is a stub.
func TestReadInboxAnswersAnErrorNotAPanicToAnUnexpectedSuccess(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		contentType string
		body        string
	}{
		{"an HTML page from a proxy", http.StatusOK, "text/html", "<html><body>captive portal</body></html>"},
		{"an empty 204", http.StatusNoContent, "", ""},
		{"a 200 that is not JSON", http.StatusOK, "text/plain", "ok"},
		{"JSON that is not the batch", http.StatusOK, "application/json", `"nope"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					rw.Header().Set("Content-Type", tc.contentType)
				}
				rw.WriteHeader(tc.status)
				_, _ = io.WriteString(rw, tc.body)
			}))
			t.Cleanup(stub.Close)

			api, err := mcp.NewAPI(stub.URL, "token", nil)
			if err != nil {
				t.Fatalf("NewAPI: %v", err)
			}
			sess := mcp.Session{
				Identity: mcp.Identity{Transport: mcp.TransportStdio, Kind: mcp.KindPerson},
				API:      api,
			}
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

			res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "read_inbox"})
			if err == nil && !res.IsError {
				t.Fatalf("read_inbox answered %+v to a response that is not a batch, want an error", res.StructuredContent)
			}
		})
	}
}
