package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-99's surface over an in-memory E6, with the store's rules modelled just
// far enough to drive every declared status: an agent may not follow an
// agent, a resolved thread takes no turns, a marker moves forward and is
// clamped, reading is not joining, an agent has no marker, and a resolution
// is recorded once. The store's own tests prove those rules; the
// database-backed test in threads_db_test.go proves this surface holds over
// the store E6 actually shipped.

type fakeThread struct {
	d            store.Discussion
	turns        []store.DiscussionTurn
	participants []*store.DiscussionParticipant
}

type fakeThreads struct {
	mu      sync.Mutex
	wiki    *fakeWiki
	threads map[int64]*fakeThread
	users   map[uuid.UUID]store.User
	next    int64
	busy    bool
	err     error
}

func newFakeThreads(w *fakeWiki) *fakeThreads {
	return &fakeThreads{wiki: w, threads: map[int64]*fakeThread{}, users: map[uuid.UUID]store.User{}, next: 1}
}

func (f *fakeThreads) isAgent(id uuid.UUID) bool { return f.users[id].Kind == store.KindAgent }

func (f *fakeThreads) byID(id uuid.UUID) *fakeThread {
	for _, th := range f.threads {
		if th.d.ID == id {
			return th
		}
	}
	return nil
}

func (f *fakeThreads) participant(th *fakeThread, user uuid.UUID) *store.DiscussionParticipant {
	for _, p := range th.participants {
		if p.UserID == user {
			return p
		}
	}
	return nil
}

// advance is ruling 3: posting is reading, for a person.
func (f *fakeThreads) advance(th *fakeThread, t store.DiscussionTurn) {
	if t.ByAgent() {
		return
	}
	now := time.Now()
	p := f.participant(th, t.AuthorID)
	if p == nil {
		u := f.users[t.AuthorID]
		p = &store.DiscussionParticipant{DiscussionID: th.d.ID, UserID: t.AuthorID, Kind: u.Kind,
			DisplayName: u.DisplayName, AddedAt: now, AddedBy: t.AuthorID}
		th.participants = append(th.participants, p)
	}
	if p.LastReadSeq == nil || *p.LastReadSeq < t.Seq {
		seq := t.Seq
		p.LastReadSeq, p.LastReadAt = &seq, &now
	}
}

func (f *fakeThreads) OpenDiscussion(_ context.Context, in store.NewDiscussion) (store.Discussion, store.DiscussionTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isAgent(in.AuthorID) {
		return store.Discussion{}, store.DiscussionTurn{}, store.ErrAgentMayNotFollowAgent
	}
	if in.PageID != nil {
		if _, ok := f.wiki.pathOf(*in.PageID); !ok {
			return store.Discussion{}, store.DiscussionTurn{}, store.ErrNotFound
		}
	}
	now := time.Now()
	d := store.Discussion{ID: uuid.New(), Number: f.next, PageID: in.PageID, Title: in.Title, CreatedAt: now}
	f.next++
	t := store.DiscussionTurn{ID: uuid.New(), DiscussionID: d.ID, Seq: 1, AuthorID: in.AuthorID,
		AuthorKind: f.users[in.AuthorID].Kind, Body: in.Body, CreatedAt: now, ComposedAt: in.ComposedAt, MemoID: in.MemoID}
	th := &fakeThread{d: d, turns: []store.DiscussionTurn{t}}
	f.threads[d.Number] = th
	f.advance(th, t)
	return d, t, nil
}

func (f *fakeThreads) AppendTurn(_ context.Context, in store.NewTurn) (store.DiscussionTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	th := f.byID(in.DiscussionID)
	if th == nil {
		return store.DiscussionTurn{}, store.ErrNotFound
	}
	if f.busy {
		return store.DiscussionTurn{}, store.ErrLinkLocked
	}
	if th.d.Resolved() {
		return store.DiscussionTurn{}, store.ErrDiscussionResolved
	}
	last := th.turns[len(th.turns)-1]
	if f.isAgent(in.AuthorID) && last.ByAgent() {
		return store.DiscussionTurn{}, store.ErrAgentMayNotFollowAgent
	}
	t := store.DiscussionTurn{ID: uuid.New(), DiscussionID: th.d.ID, Seq: last.Seq + 1, AuthorID: in.AuthorID,
		AuthorKind: f.users[in.AuthorID].Kind, Body: in.Body, CreatedAt: time.Now(), ComposedAt: in.ComposedAt, MemoID: in.MemoID}
	th.turns = append(th.turns, t)
	f.advance(th, t)
	return t, nil
}

func (f *fakeThreads) DiscussionByNumber(_ context.Context, number int64) (store.Discussion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return store.Discussion{}, f.err
	}
	th, ok := f.threads[number]
	if !ok {
		return store.Discussion{}, store.ErrNotFound
	}
	return th.d, nil
}

func (f *fakeThreads) DiscussionByID(_ context.Context, id uuid.UUID) (store.Discussion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if th := f.byID(id); th != nil {
		return th.d, nil
	}
	return store.Discussion{}, store.ErrNotFound
}

func (f *fakeThreads) DiscussionsOnPage(_ context.Context, pageID uuid.UUID) ([]store.Discussion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []store.Discussion
	for _, th := range f.threads {
		if th.d.PageID != nil && *th.d.PageID == pageID {
			out = append(out, th.d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (f *fakeThreads) Turns(_ context.Context, id uuid.UUID) ([]store.DiscussionTurn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	th := f.byID(id)
	if th == nil {
		return nil, store.ErrNotFound
	}
	return append([]store.DiscussionTurn(nil), th.turns...), nil
}

func (f *fakeThreads) Participants(_ context.Context, id uuid.UUID) ([]store.DiscussionParticipant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	th := f.byID(id)
	if th == nil {
		return nil, store.ErrNotFound
	}
	out := make([]store.DiscussionParticipant, 0, len(th.participants))
	for _, p := range th.participants {
		out = append(out, *p)
	}
	return out, nil
}

func (f *fakeThreads) AddParticipant(_ context.Context, id, user, by uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isAgent(by) || by == uuid.Nil {
		return store.ErrConfirmerRequired
	}
	th := f.byID(id)
	if th == nil {
		return store.ErrNotFound
	}
	u, known := f.users[user]
	if !known {
		return store.ErrNotFound
	}
	if p := f.participant(th, user); p != nil {
		p.RemovedAt, p.RemovedBy = nil, nil
		return nil
	}
	th.participants = append(th.participants, &store.DiscussionParticipant{DiscussionID: id, UserID: user,
		Kind: u.Kind, DisplayName: u.DisplayName, AddedAt: time.Now(), AddedBy: by})
	return nil
}

func (f *fakeThreads) RemoveParticipant(_ context.Context, id, user, by uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isAgent(by) || by == uuid.Nil {
		return store.ErrConfirmerRequired
	}
	th := f.byID(id)
	if th == nil {
		return nil
	}
	if p := f.participant(th, user); p != nil && p.RemovedAt == nil {
		now := time.Now()
		p.RemovedAt, p.RemovedBy = &now, &by
	}
	return nil
}

func (f *fakeThreads) MarkRead(_ context.Context, id, user uuid.UUID, through int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if through < 0 {
		return store.ErrInvalidInput
	}
	if f.isAgent(user) {
		return store.ErrAgentHasNoMarker
	}
	th := f.byID(id)
	if th == nil {
		return store.ErrNotFound
	}
	p := f.participant(th, user)
	if p == nil {
		return store.ErrNotAParticipant
	}
	head := th.turns[len(th.turns)-1].Seq
	want := min(through, head)
	if p.LastReadSeq == nil || want > *p.LastReadSeq {
		now := time.Now()
		p.LastReadSeq, p.LastReadAt = &want, &now
	}
	return nil
}

func (f *fakeThreads) UnreadCount(_ context.Context, id, user uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.isAgent(user) {
		return 0, nil
	}
	th := f.byID(id)
	if th == nil {
		return 0, store.ErrNotFound
	}
	p := f.participant(th, user)
	if p == nil {
		return 0, store.ErrNotAParticipant
	}
	read := 0
	if p.LastReadSeq != nil {
		read = *p.LastReadSeq
	}
	return max(0, th.turns[len(th.turns)-1].Seq-read), nil
}

func (f *fakeThreads) UnreadByDiscussion(_ context.Context, user uuid.UUID) (map[uuid.UUID]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[uuid.UUID]int{}
	if f.isAgent(user) {
		return out, nil
	}
	for _, th := range f.threads {
		p := f.participant(th, user)
		if p == nil || p.RemovedAt != nil {
			continue
		}
		read := 0
		if p.LastReadSeq != nil {
			read = *p.LastReadSeq
		}
		if n := th.turns[len(th.turns)-1].Seq - read; n > 0 {
			out[th.d.ID] = n
		}
	}
	return out, nil
}

func (f *fakeThreads) resolve(th *fakeThread, by uuid.UUID, note *uuid.UUID) error {
	if f.isAgent(by) || by == uuid.Nil {
		return store.ErrConfirmerRequired
	}
	switch {
	case th.d.ResolvedAt == nil:
		now := time.Now()
		th.d.ResolvedAt, th.d.ResolvedBy, th.d.ResolvedNoteID = &now, &by, note
	case th.d.ResolvedNoteID == nil && note != nil:
		th.d.ResolvedNoteID = note // completed; the original resolver stands
	case note != nil && th.d.ResolvedNoteID != nil && *note == *th.d.ResolvedNoteID:
	default:
		return store.ErrResolutionFixed
	}
	return nil
}

func (f *fakeThreads) ResolveIntoNewNote(ctx context.Context, id, by uuid.UUID, in store.Resolution) (store.Note, store.NoteRevision, error) {
	f.mu.Lock()
	th := f.byID(id)
	f.mu.Unlock()
	if th == nil {
		return store.Note{}, store.NoteRevision{}, store.ErrNotFound
	}
	if f.isAgent(by) {
		return store.Note{}, store.NoteRevision{}, store.ErrConfirmerRequired
	}
	n, rev, err := f.wiki.CreateNote(ctx, store.NewNote{PageID: in.PageID, AuthorID: by, ConfirmedBy: by, Title: in.Title, Body: in.Body})
	if err != nil {
		return store.Note{}, store.NoteRevision{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.resolve(th, by, &n.ID); err != nil {
		return store.Note{}, store.NoteRevision{}, err
	}
	f.wiki.mu.Lock()
	f.wiki.threads[n.ID] = append(f.wiki.threads[n.ID], th.d)
	f.wiki.mu.Unlock()
	return n, rev, nil
}

func (f *fakeThreads) ResolveIntoExistingNote(ctx context.Context, id, noteID, by uuid.UUID, title, body string) (store.NoteRevision, error) {
	f.mu.Lock()
	th := f.byID(id)
	f.mu.Unlock()
	if th == nil {
		return store.NoteRevision{}, store.ErrNotFound
	}
	rev, err := f.wiki.AppendRevision(ctx, noteID, store.NewRevision{AuthorID: by, ConfirmedBy: by, Title: title, Body: body})
	if err != nil {
		return store.NoteRevision{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.resolve(th, by, &noteID); err != nil {
		return store.NoteRevision{}, err
	}
	f.wiki.mu.Lock()
	f.wiki.threads[noteID] = append(f.wiki.threads[noteID], th.d)
	f.wiki.mu.Unlock()
	return rev, nil
}

func (f *fakeThreads) ResolveWithoutNote(_ context.Context, id, by uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	th := f.byID(id)
	if th == nil {
		return store.ErrNotFound
	}
	return f.resolve(th, by, nil)
}

type threadRig struct {
	*wikiRig
	threads *fakeThreads
	second  store.User
}

func newThreadRig(t *testing.T) *threadRig {
	t.Helper()
	w := newWikiRig(t, false)
	th := newFakeThreads(w.wiki)
	f := newFakeAccounts()
	member := f.signIn(w.member, "member-token")
	agent := f.signIn(w.agent, "agent-token")
	second := f.signIn(person("second@example.com", false), "second-token")
	for _, u := range []store.User{member, agent, second} {
		th.users[u.ID] = u
	}
	logger := jsonLogger(w.logs)
	w.h = NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: logger, Version: "test", SecureCookies: true,
		Wiki: w.wiki, Threads: th, LocalReferences: w.wiki,
	})
	return &threadRig{wikiRig: w, threads: th, second: second}
}

// ---------------------------------------------------------------------------

// E6's exit, over the fake: a person and an agent hold a threaded exchange
// over HTTP, unread is right for two sessions of the person, and resolving
// writes the link both ways.
func TestAThreadIsHeldOverHTTPWithAnAgent(t *testing.T) {
	rig := newThreadRig(t)
	me, agent, other := rig.as("member-token"), rig.as("agent-token"), rig.as("second-token")
	mustStatus(t, me(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")

	// An agent cannot open a thread; a person can, and has read their own turn.
	mustStatus(t, agent(http.MethodPost, "/discussions", `{"title":"t","body":"b"}`), http.StatusConflict, "openDiscussion")
	rec := me(http.MethodPost, "/discussions", `{"title":"How long do we keep audio","page":"estate","body":"@scribe what gates deletion? See CHR-0311."}`)
	mustStatus(t, rec, http.StatusCreated, "openDiscussion")
	th := decodeInto[wire.Thread](t, rec)
	if th.Discussion.Ref != "DSC-0001" || th.Discussion.Page == nil || *th.Discussion.Page != "estate" || th.Discussion.Resolved != nil {
		t.Fatalf("opened = %+v", th.Discussion)
	}
	if len(th.Turns) != 1 || th.Turns[0].Seq != 1 || th.Turns[0].AuthorKind != wire.TurnAuthorKindPerson {
		t.Fatalf("turns = %+v", th.Turns)
	}
	if len(th.Turns[0].References) != 1 || th.Turns[0].References[0].Token != "CHR-0311" || !strings.Contains(th.Turns[0].Html, "data-ref") {
		t.Errorf("turn 1 was not rendered: %+v", th.Turns[0])
	}
	if th.Unread == nil || *th.Unread != 0 {
		t.Errorf("unread after opening = %v, want 0: posting is reading", th.Unread)
	}

	// The agent replies, attributed as an agent; the person has exactly one
	// unread, in both of their sessions.
	rec = agent(http.MethodPost, "/discussions/DSC-0001/turns", `{"body":"Deletion is gated on a durable transcript."}`)
	mustStatus(t, rec, http.StatusCreated, "appendTurn")
	if turn := decodeInto[wire.Turn](t, rec); turn.Seq != 2 || turn.AuthorKind != wire.TurnAuthorKindAgent || turn.AuthorId != rig.agent.ID {
		t.Errorf("agent turn = %+v", turn)
	}
	for _, session := range []func(string, string, string) *httptest.ResponseRecorder{me, rig.as("member-token")} {
		rec = session(http.MethodGet, "/discussions/dsc-1", "")
		mustStatus(t, rec, http.StatusOK, "getDiscussion")
		if got := decodeInto[wire.Thread](t, rec); got.Unread == nil || *got.Unread != 1 {
			t.Errorf("unread after the agent's reply = %v, want exactly 1", got.Unread)
		}
	}
	// And the agent may not go again.
	rec = agent(http.MethodPost, "/discussions/DSC-0001/turns", `{"body":"and another thing"}`)
	mustStatus(t, rec, http.StatusConflict, "appendTurn")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeAgentAfterAgent {
		t.Errorf("code = %q", e.Code)
	}
	// The agent sees no unread, ever.
	rec = agent(http.MethodGet, "/discussions/DSC-0001", "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	if got := decodeInto[wire.Thread](t, rec); got.Unread != nil {
		t.Errorf("an agent was handed an unread count: %d", *got.Unread)
	}

	// The badge, then marking read in one session is reflected in the other.
	rec = me(http.MethodGet, "/discussions/unread", "")
	mustStatus(t, rec, http.StatusOK, "listUnread")
	if badge := decodeInto[wire.UnreadList](t, rec); len(badge.Items) != 1 || badge.Items[0].Ref != "DSC-0001" || badge.Items[0].Unread != 1 {
		t.Errorf("badge = %+v", badge)
	}
	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":999}`), http.StatusNoContent, "markRead")
	rec = rig.as("member-token")(http.MethodGet, "/discussions/DSC-0001", "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	if got := decodeInto[wire.Thread](t, rec); got.Unread == nil || *got.Unread != 0 {
		t.Errorf("unread after marking read = %v, want 0", got.Unread)
	}
	rec = me(http.MethodGet, "/discussions/unread", "")
	mustStatus(t, rec, http.StatusOK, "listUnread")
	if badge := decodeInto[wire.UnreadList](t, rec); len(badge.Items) != 0 {
		t.Errorf("badge after reading = %+v", badge.Items)
	}
	// Clamped: one more turn is exactly one unread, not 998 fewer.
	mustStatus(t, other(http.MethodPost, "/discussions/DSC-0001/turns", `{"body":"a second person"}`), http.StatusCreated, "appendTurn")
	rec = me(http.MethodGet, "/discussions/DSC-0001", "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	if got := decodeInto[wire.Thread](t, rec); got.Unread == nil || *got.Unread != 1 {
		t.Errorf("unread after a third turn = %v, want 1 (the marker was clamped to the thread)", got.Unread)
	}
	// A stale report is a no-op, not an error.
	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":1}`), http.StatusNoContent, "markRead")

	// Reading is not joining; an agent has no marker.
	rec = rig.do(http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":3}`, "second-token")
	mustStatus(t, rec, http.StatusNoContent, "markRead") // the second person posted, so they joined
	rec = agent(http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":3}`)
	mustStatus(t, rec, http.StatusForbidden, "markRead")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeAgentHasNoMarker {
		t.Errorf("code = %q", e.Code)
	}

	// The agent joins as a participant — by a person — and is listed as one.
	mustStatus(t, agent(http.MethodPost, "/discussions/DSC-0001/participants", `{"user_id":"`+rig.agent.ID.String()+`"}`), http.StatusForbidden, "addParticipant")
	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/participants", `{"user_id":"`+rig.agent.ID.String()+`"}`), http.StatusNoContent, "addParticipant")
	rec = me(http.MethodGet, "/discussions/DSC-0001", "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	got := decodeInto[wire.Thread](t, rec)
	var scribe *wire.Participant
	for i := range got.Participants {
		if got.Participants[i].UserId == rig.agent.ID {
			scribe = &got.Participants[i]
		}
	}
	if scribe == nil || scribe.Kind != wire.ParticipantKindAgent || scribe.LastReadSeq != nil {
		t.Errorf("the agent participant = %+v", scribe)
	}
	mustStatus(t, me(http.MethodDelete, "/discussions/DSC-0001/participants/"+rig.agent.ID.String(), ""), http.StatusNoContent, "removeParticipant")
	mustStatus(t, me(http.MethodDelete, "/discussions/DSC-0001/participants/"+rig.agent.ID.String(), ""), http.StatusNoContent, "removeParticipant")

	// Resolve into a new note. The link is readable from both ends: the
	// thread names the note, the note names the thread.
	mustStatus(t, agent(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"nothing"}`), http.StatusForbidden, "resolveDiscussion")
	rec = me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"new_note","page":"estate","title":"Retention","body":"Gated on a durable transcript."}`)
	mustStatus(t, rec, http.StatusOK, "resolveDiscussion")
	res := decodeInto[wire.DiscussionResolution](t, rec)
	if res.Discussion.Resolved == nil || res.Discussion.Resolved.Note == nil || *res.Discussion.Resolved.Note != "CHR-0001" || res.Discussion.Resolved.By != rig.member.ID {
		t.Fatalf("resolved = %+v", res.Discussion.Resolved)
	}
	if res.Note == nil || res.Note.Ref != "CHR-0001" || len(res.Note.ResolvedFrom) != 1 || res.Note.ResolvedFrom[0].Ref != "DSC-0001" {
		t.Errorf("note end of the link = %+v", res.Note)
	}
	rec = me(http.MethodGet, "/notes/CHR-0001", "")
	mustStatus(t, rec, http.StatusOK, "getNote")
	if n := decodeInto[wire.Note](t, rec); len(n.ResolvedFrom) != 1 || n.ResolvedFrom[0].Ref != "DSC-0001" {
		t.Errorf("GET /notes/CHR-0001 resolved_from = %+v", n.ResolvedFrom)
	}
	rec = me(http.MethodGet, "/discussions/DSC-0001", "")
	mustStatus(t, rec, http.StatusOK, "getDiscussion")
	if got := decodeInto[wire.Thread](t, rec); got.Discussion.Resolved == nil || deref(got.Discussion.Resolved.Note) != "CHR-0001" || len(got.Turns) != 3 {
		t.Errorf("thread after resolve = %+v", got.Discussion)
	}

	// Still readable, takes no more turns, and the conclusion is not rewritten.
	rec = me(http.MethodPost, "/discussions/DSC-0001/turns", `{"body":"late"}`)
	mustStatus(t, rec, http.StatusConflict, "appendTurn")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeDiscussionResolved {
		t.Errorf("code = %q", e.Code)
	}
	rec = me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"nothing"}`)
	mustStatus(t, rec, http.StatusConflict, "resolveDiscussion")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeResolutionFixed {
		t.Errorf("code = %q", e.Code)
	}
	// Idempotent on the same note.
	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"existing_note","note_ref":"CHR-0001","body":"again"}`), http.StatusOK, "resolveDiscussion")

	// Listed on its page, resolved and all.
	rec = me(http.MethodGet, "/discussions?page=estate", "")
	mustStatus(t, rec, http.StatusOK, "listDiscussions")
	if l := decodeInto[wire.DiscussionList](t, rec); len(l.Items) != 1 || l.Items[0].Resolved == nil || deref(l.Items[0].Resolved.Note) != "CHR-0001" {
		t.Errorf("list = %+v", l)
	}
}

// Resolving without a note is a deliberate choice, and can be completed later
// into an existing note; the original resolver stands.
func TestAThreadCanEndWithNothingAndBeCompletedLater(t *testing.T) {
	rig := newThreadRig(t)
	me, other := rig.as("member-token"), rig.as("second-token")
	mustStatus(t, me(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	mustStatus(t, me(http.MethodPost, "/notes", `{"page":"estate","title":"Principles","body":"§6"}`), http.StatusCreated, "createNote")
	mustStatus(t, me(http.MethodPost, "/discussions", `{"title":"t","body":"b"}`), http.StatusCreated, "openDiscussion")

	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"bogus"}`), http.StatusBadRequest, "resolveDiscussion")
	rec := me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"nothing"}`)
	mustStatus(t, rec, http.StatusOK, "resolveDiscussion")
	res := decodeInto[wire.DiscussionResolution](t, rec)
	if res.Note != nil || res.Discussion.Resolved == nil || res.Discussion.Resolved.Note != nil {
		t.Fatalf("resolved into nothing = %+v", res)
	}
	// An unfiled thread carries no page.
	if res.Discussion.Page != nil {
		t.Errorf("an unfiled thread carries page %q", *res.Discussion.Page)
	}

	rec = other(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"existing_note","note_ref":"CHR-0001","body":"Resolved into PRINCIPLES §6."}`)
	mustStatus(t, rec, http.StatusOK, "resolveDiscussion")
	res = decodeInto[wire.DiscussionResolution](t, rec)
	if res.Note == nil || res.Note.Revision.Seq != 2 || res.Note.Title != "Principles" || deref(res.Discussion.Resolved.Note) != "CHR-0001" {
		t.Errorf("completed = %+v / %+v", res.Discussion.Resolved, res.Note)
	}
	if res.Discussion.Resolved.By != rig.member.ID {
		t.Errorf("the completion reattributed the resolver to %s", res.Discussion.Resolved.By)
	}
	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"existing_note","note_ref":"CHR-0099","body":"x"}`), http.StatusNotFound, "resolveDiscussion")
	mustStatus(t, me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"new_note","page":"estate","title":"t","body":"b"}`), http.StatusConflict, "resolveDiscussion")
}

func TestThreadRefusalsAreTheDocumentedOnes(t *testing.T) {
	rig := newThreadRig(t)
	me := rig.as("member-token")
	mustStatus(t, me(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	mustStatus(t, me(http.MethodPost, "/notes", `{"page":"estate","title":"t","body":"b"}`), http.StatusCreated, "createNote")
	mustStatus(t, me(http.MethodPost, "/discussions", `{"title":"t","body":"b"}`), http.StatusCreated, "openDiscussion")

	// Malformed refs and missing fields.
	for _, tc := range []struct{ method, path, body, op string }{
		{http.MethodGet, "/discussions/not-a-ref", "", "getDiscussion"},
		{http.MethodPost, "/discussions/DSC-0/turns", `{"body":"b"}`, "appendTurn"},
		{http.MethodPost, "/discussions/CHR-1/read", `{"through_seq":1}`, "markRead"},
		{http.MethodPost, "/discussions/x/resolve", `{"into":"nothing"}`, "resolveDiscussion"},
		{http.MethodPost, "/discussions/x/participants", `{"user_id":"` + someUUID + `"}`, "addParticipant"},
		{http.MethodDelete, "/discussions/x/participants/" + someUUID, "", "removeParticipant"},
		{http.MethodPost, "/discussions", `{"title":"","body":"b"}`, "openDiscussion"},
		{http.MethodPost, "/discussions/DSC-0001/turns", `{"body":" "}`, "appendTurn"},
		{http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":-1}`, "markRead"},
		{http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"new_note","title":"t"}`, "resolveDiscussion"},
		{http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"existing_note","note_ref":"nope","body":"b"}`, "resolveDiscussion"},
		{http.MethodPost, "/discussions/DSC-0001/participants", `{}`, "addParticipant"},
		{http.MethodGet, "/discussions?page=Bad%20Path", "", "listDiscussions"},
		{http.MethodGet, "/discussions?page=estate&cursor=x", "", "listDiscussions"},
	} {
		mustStatus(t, me(tc.method, tc.path, tc.body), http.StatusBadRequest, tc.op)
	}
	// Not found.
	for _, tc := range []struct{ method, path, body, op string }{
		{http.MethodGet, "/discussions/DSC-0099", "", "getDiscussion"},
		{http.MethodPost, "/discussions/DSC-0099/turns", `{"body":"b"}`, "appendTurn"},
		{http.MethodPost, "/discussions/DSC-0099/read", `{"through_seq":1}`, "markRead"},
		{http.MethodPost, "/discussions/DSC-0099/resolve", `{"into":"nothing"}`, "resolveDiscussion"},
		{http.MethodPost, "/discussions/DSC-0099/participants", `{"user_id":"` + someUUID + `"}`, "addParticipant"},
		{http.MethodDelete, "/discussions/DSC-0099/participants/" + someUUID, "", "removeParticipant"},
		{http.MethodPost, "/discussions/DSC-0001/participants", `{"user_id":"` + someUUID + `"}`, "addParticipant"},
		{http.MethodPost, "/discussions", `{"title":"t","page":"nowhere","body":"b"}`, "openDiscussion"},
		{http.MethodGet, "/discussions?page=nowhere", "", "listDiscussions"},
		{http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"new_note","page":"nowhere","title":"t","body":"b"}`, "resolveDiscussion"},
	} {
		mustStatus(t, me(tc.method, tc.path, tc.body), http.StatusNotFound, tc.op)
	}
	// A busy thread, and a deleted note.
	rig.threads.busy = true
	rec := me(http.MethodPost, "/discussions/DSC-0001/turns", `{"body":"b"}`)
	mustStatus(t, rec, http.StatusConflict, "appendTurn")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeThreadBusy {
		t.Errorf("code = %q", e.Code)
	}
	rig.threads.busy = false
	rig.wiki.softDelete(1, rig.member.ID)
	rec = me(http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"existing_note","note_ref":"CHR-0001","body":"b"}`)
	mustStatus(t, rec, http.StatusConflict, "resolveDiscussion")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeNoteDeleted {
		t.Errorf("code = %q", e.Code)
	}
	// Not a participant.
	rec = rig.do(http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":1}`, "second-token")
	mustStatus(t, rec, http.StatusForbidden, "markRead")
	if e := decodeInto[wire.Error](t, rec); e.Code != codeNotAParticipant {
		t.Errorf("code = %q", e.Code)
	}
	// The store fails.
	rig.threads.err = errors.New("connection reset by peer")
	rec = me(http.MethodGet, "/discussions/DSC-0001", "")
	mustStatus(t, rec, http.StatusInternalServerError, "getDiscussion")
	if strings.Contains(rec.Body.String(), "connection reset") {
		t.Error("the cause reached the body")
	}
}

func TestDiscussionsOnAPageFollowARedirectAndPageByCursor(t *testing.T) {
	rig := newThreadRig(t)
	me := rig.as("member-token")
	mustStatus(t, me(http.MethodPost, "/pages", `{"path":"estate"}`), http.StatusCreated, "createPage")
	for _, title := range []string{"one", "two", "three"} {
		mustStatus(t, me(http.MethodPost, "/discussions", `{"title":"`+title+`","page":"estate","body":"b"}`), http.StatusCreated, "openDiscussion")
	}
	rig.wiki.redirects["old"] = "estate"
	rec := me(http.MethodGet, "/discussions?page=old&limit=2", "")
	mustStatus(t, rec, http.StatusOK, "listDiscussions")
	l := decodeInto[wire.DiscussionList](t, rec)
	if l.MovedFrom == nil || l.Page.Path != "estate" || len(l.Items) != 2 || l.Items[1].Ref != "DSC-0002" || l.NextCursor == nil {
		t.Fatalf("page 1 = %+v", l)
	}
	rec = me(http.MethodGet, "/discussions?page=estate&limit=2&cursor="+*l.NextCursor, "")
	mustStatus(t, rec, http.StatusOK, "listDiscussions")
	if l = decodeInto[wire.DiscussionList](t, rec); len(l.Items) != 1 || l.Items[0].Title != "three" || l.NextCursor != nil {
		t.Errorf("page 2 = %+v", l)
	}
}

func TestThreadsWithoutAStoreAnswerTheDocumented503(t *testing.T) {
	f := newFakeAccounts()
	f.signIn(person("member@example.com", false), "member-token")
	h := testRouter(f)
	for _, tc := range []struct{ method, path, body, op string }{
		{http.MethodGet, "/discussions?page=estate", "", "listDiscussions"},
		{http.MethodPost, "/discussions", `{"title":"t","body":"b"}`, "openDiscussion"},
		{http.MethodGet, "/discussions/unread", "", "listUnread"},
		{http.MethodGet, "/discussions/DSC-0001", "", "getDiscussion"},
		{http.MethodPost, "/discussions/DSC-0001/turns", `{"body":"b"}`, "appendTurn"},
		{http.MethodPost, "/discussions/DSC-0001/read", `{"through_seq":1}`, "markRead"},
		{http.MethodPost, "/discussions/DSC-0001/resolve", `{"into":"nothing"}`, "resolveDiscussion"},
		{http.MethodPost, "/discussions/DSC-0001/participants", `{"user_id":"` + someUUID + `"}`, "addParticipant"},
		{http.MethodDelete, "/discussions/DSC-0001/participants/" + someUUID, "", "removeParticipant"},
	} {
		rec := httptest.NewRecorder()
		var r *http.Request
		if tc.body != "" {
			r = jsonReq(tc.method, tc.path, tc.body)
		} else {
			r = httptest.NewRequest(tc.method, tc.path, nil)
		}
		r.Header.Set("Authorization", "Bearer member-token")
		h.ServeHTTP(rec, r)
		mustStatus(t, rec, http.StatusServiceUnavailable, tc.op)
	}
}

func TestThreadWritesDriveTheSharedRefusals(t *testing.T) {
	rig := newThreadRig(t)
	me := rig.as("member-token")
	mustStatus(t, me(http.MethodPost, "/discussions", `{"title":"t","body":"b"}`), http.StatusCreated, "openDiscussion")
	for _, tc := range []struct {
		path, op, valid string
		limit           int
	}{
		{"/discussions", "openDiscussion", `{"title":"t","body":"b"}`, maxNoteBody},
		{"/discussions/DSC-0001/turns", "appendTurn", `{"body":"b"}`, maxNoteBody},
		{"/discussions/DSC-0001/read", "markRead", `{"through_seq":1}`, maxPageBody},
		{"/discussions/DSC-0001/resolve", "resolveDiscussion", `{"into":"nothing"}`, maxNoteBody},
		{"/discussions/DSC-0001/participants", "addParticipant", `{"user_id":"` + someUUID + `"}`, maxPageBody},
	} {
		t.Run(tc.op, func(t *testing.T) {
			rec := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.valid))
			r.Header.Set("Content-Type", "text/plain")
			r.Header.Set("Authorization", "Bearer member-token")
			rig.h.ServeHTTP(rec, r)
			mustStatus(t, rec, http.StatusUnsupportedMediaType, tc.op)
			mustStatus(t, me(http.MethodPost, tc.path, `{"nope":`), http.StatusBadRequest, tc.op)
			mustStatus(t, me(http.MethodPost, tc.path, `{"unknown_field":1}`), http.StatusBadRequest, tc.op)
			huge := `{"body":"` + strings.Repeat("x", tc.limit) + `"}`
			mustStatus(t, me(http.MethodPost, tc.path, huge), http.StatusRequestEntityTooLarge, tc.op)
		})
	}
}
