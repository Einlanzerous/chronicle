package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

const testProposer = "ollama/gemma4:31b@v1"

// ============================================================================
// tracker — a Switchyard that behaves like the real one, INCLUDING THE PART
// THAT MAKES THIS TICKET HARD.
// ============================================================================
//
// The idempotency middleware is: look the key up, run the handler, write the
// cache row. There is NO LOCK between the lookup and the handler, so two
// overlapping requests under one key both reach the handler and two tickets
// exist. This stub reproduces that exactly — the cache is consulted and written
// under a mutex, and the handler runs OUTSIDE it.
//
// A stub that serialised would make every concurrency test here pass against a
// Chronicle with no pending row at all, which is the one thing they exist to
// prove.
type tracker struct {
	mu     sync.Mutex
	n      int
	cache  map[string]switchyard.Ticket
	byMemo map[uuid.UUID][]switchyard.Ticket

	// sentKeys records the idempotency key of every request, replays included.
	// The sweep must re-send the STORED key rather than mint one, and that is
	// only checkable from here.
	sentKeys []string

	// creates counts HANDLER RUNS — tickets that came into existence. calls
	// counts requests, replays included. The gap between them is the whole
	// subject.
	creates  int
	calls    int
	searches int

	// handlerDelay widens the window the real service has between its cache
	// lookup and its insert, so an overlap in a test is a real overlap.
	handlerDelay time.Duration

	// createErr is returned instead of creating. Set to a *switchyard.Error to
	// exercise the refused/failed split.
	createErr error
	searchErr error

	// onCreate fires after a handler run, so a test can make something happen
	// mid-batch — a client hanging up, most usefully.
	onCreate func()

	// plant adds tickets nothing here created — a race from before the pending
	// row existed, or somebody who copied the metadata by hand.
	planted map[uuid.UUID][]string
}

func newTracker() *tracker {
	return &tracker{
		cache:   map[string]switchyard.Ticket{},
		byMemo:  map[uuid.UUID][]switchyard.Ticket{},
		planted: map[uuid.UUID][]string{},
	}
}

func (tr *tracker) TicketURL(key string) string {
	if key == "" {
		return ""
	}
	return "https://switchyard.test/tickets/" + key
}

func (tr *tracker) CreateTicket(ctx context.Context, in switchyard.NewTicket) (switchyard.Ticket, error) {
	tr.mu.Lock()
	tr.calls++
	tr.sentKeys = append(tr.sentKeys, in.IdempotencyKey)
	hit, replayed := tr.cache[in.IdempotencyKey]
	tr.mu.Unlock()
	if replayed {
		return hit, nil
	}

	// THE HANDLER, OUTSIDE THE LOCK. This is the gap.
	if tr.handlerDelay > 0 {
		time.Sleep(tr.handlerDelay)
	}
	if tr.createErr != nil {
		return switchyard.Ticket{}, tr.createErr
	}

	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.n++
	tr.creates++
	key := fmt.Sprintf("CHRN-%d", 900+tr.n)
	t := switchyard.Ticket{Key: key, ID: key, URL: tr.TicketURL(key)}
	tr.cache[in.IdempotencyKey] = t
	tr.byMemo[in.MemoID] = append(tr.byMemo[in.MemoID], t)
	if tr.onCreate != nil {
		tr.onCreate()
	}
	return t, nil
}

func (tr *tracker) TicketsByMemo(ctx context.Context, memoID uuid.UUID) ([]switchyard.Ticket, error) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.searches++
	if tr.searchErr != nil {
		return nil, tr.searchErr
	}
	out := append([]switchyard.Ticket(nil), tr.byMemo[memoID]...)
	for _, k := range tr.planted[memoID] {
		out = append(out, switchyard.Ticket{Key: k, ID: k, URL: tr.TicketURL(k)})
	}
	return out, nil
}

func (tr *tracker) createdCount() int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return tr.creates
}

func (tr *tracker) plant(memoID uuid.UUID, keys ...string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.planted[memoID] = append(tr.planted[memoID], keys...)
}

// httpError builds the *switchyard.Error a real non-2xx produces.
func httpError(status int, detail string) error {
	return &switchyard.Error{Status: status, Method: "POST", Path: "/v1/tickets", Detail: detail}
}

// ============================================================================
// stubCatalogue
// ============================================================================

type stubCatalogue struct {
	snap  *catalogue.Snapshot
	err   error
	calls int
}

func (c *stubCatalogue) Fetch(ctx context.Context) (*catalogue.Snapshot, error) {
	c.calls++
	return c.snap, c.err
}

func liveCatalogue(t *testing.T, keys ...string) *catalogue.Snapshot {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1\nprojects:\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "  - key: %s\n    name: Project %s\n    description: the %s project\n", k, k, k)
	}
	// No pages: E5's tree does not exist until CHRN-37, and an empty catalogue
	// is the correct state rather than a gap.
	s, err := catalogue.Parse([]byte(b.String()))
	if err != nil {
		t.Fatalf("catalogue: %v", err)
	}
	return s
}

// ============================================================================
// harness
// ============================================================================

type harness struct {
	t       *testing.T
	ctx     context.Context
	store   *store.Store
	tier1   *store.Tier1Store
	svc     *Service
	tracker *tracker
	cat     *stubCatalogue
	owner   store.User
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)

	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := store.New(pool)
	owner, err := st.GetOwner(ctx)
	if err != nil {
		t.Fatalf("GetOwner: %v", err)
	}

	h := &harness{
		t: t, ctx: ctx, store: st,
		// The tier-1 surface over the same pool. These cases are about the
		// TYPE-level guarantee — there is no tier-2 write method on Tier1Store
		// to call — and about this package's own logic. The ROLE-level
		// guarantee is CHRN-52's, against the real chronicle_tier1 DSN.
		tier1:   store.NewTier1(pool),
		tracker: newTracker(),
		cat:     &stubCatalogue{},
		owner:   owner,
	}
	h.cat.snap = liveCatalogue(t, "CHRN", "SWY")

	svc, err := New(Options{
		Store: st, Tier1: h.tier1, Tickets: h.tracker, Catalogue: h.cat,
		Proposer: testProposer, PreacceptMin: 0.8,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Deterministic keys, so a test can assert what was sent. Still one per
	// DECISION and never derived from the memo, which is the property that
	// matters — see switchyard.NewTicket.IdempotencyKey.
	var minted int
	var mintMu sync.Mutex
	svc.newKey = func() string {
		mintMu.Lock()
		defer mintMu.Unlock()
		minted++
		return fmt.Sprintf("chronicle-decision-%d", minted)
	}
	h.svc = svc
	return h
}

// sweeper is a SECOND service over the same store and the same tracker, so a
// test can hold one service's T2 open and run the other's sweep against it —
// which is exactly the shape production has, where the background sweep is a
// different goroutine from the request handling a batch.
func (h *harness) sweeper() *Service {
	h.t.Helper()
	svc, err := New(Options{
		Store: h.store, Tier1: h.tier1, Tickets: h.tracker, Catalogue: h.cat,
		Proposer: testProposer, PreacceptMin: 0.8,
	})
	if err != nil {
		h.t.Fatalf("New: %v", err)
	}
	return svc
}

// crashAfterCreate leaves the world in the state a process killed between the
// outward call and the confirm leaves it in: a ticket exists and the pending
// row does not know.
func (h *harness) crashAfterCreate() {
	h.svc.afterCreate = func(uuid.UUID) error {
		return errCrash
	}
}

// crashBeforeCreate leaves a pending row with nothing behind it.
func (h *harness) crashBeforeCreate() {
	h.svc.beforeCreate = func(uuid.UUID) error {
		return errCrash
	}
}

var errCrash = errors.New("injected: the process died here")

// user creates a second account, so author scoping has something to scope.
func (h *harness) user(email string) store.User {
	h.t.Helper()
	u, err := h.store.CreateUser(h.ctx, email, "A Member", store.KindPerson)
	if err != nil {
		h.t.Fatalf("CreateUser: %v", err)
	}
	return u
}

// memo puts one recording into `transcribed` with a durable transcript, which
// is the state triage actually finds a memo in.
func (h *harness) memo(author uuid.UUID, text string) store.Memo {
	h.t.Helper()
	sum := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(sum[:])

	res, err := h.store.IngestMemo(h.ctx, store.Arrival{
		AuthorID: author, ContentHash: hash, ByteSize: int64(len(text)),
		Source: store.SourceUpload, SourceRef: "test/" + hash[:8],
	})
	if err != nil {
		h.t.Fatalf("IngestMemo: %v", err)
	}
	if _, err := h.store.RecordTranscript(h.ctx, store.TranscriptInput{
		MemoID: res.Memo.ID, Text: text, Model: "whisper.cpp/small.en", Backend: "vulkan",
	}); err != nil {
		h.t.Fatalf("RecordTranscript: %v", err)
	}
	for _, step := range [][2]string{
		{store.StateCaptured, store.StateQueued},
		{store.StateQueued, store.StateTranscribing},
		{store.StateTranscribing, store.StateTranscribed},
	} {
		if _, err := h.store.AdvanceMemoState(h.ctx, res.Memo.ID, step[0], step[1], ""); err != nil {
			h.t.Fatalf("advance %s -> %s: %v", step[0], step[1], err)
		}
	}
	m, err := h.store.GetMemo(h.ctx, res.Memo.ID)
	if err != nil {
		h.t.Fatalf("GetMemo: %v", err)
	}
	return m
}

// ownMemo is the common case: a memo belonging to the owner.
func (h *harness) ownMemo(text string) store.Memo { return h.memo(h.owner.ID, text) }

// partialMemo is `transcribed` with NO DURABLE TRANSCRIPT — its only run is
// incomplete. It reads as a healthy memo everywhere else, which is precisely
// why the triage screen has to leave it out: routing from half a sentence
// produces a proposal about half a sentence, and a card cannot show that.
func (h *harness) partialMemo(text string) store.Memo {
	h.t.Helper()
	sum := sha256.Sum256([]byte(text))
	hash := hex.EncodeToString(sum[:])
	res, err := h.store.IngestMemo(h.ctx, store.Arrival{
		AuthorID: h.owner.ID, ContentHash: hash, ByteSize: int64(len(text)),
		Source: store.SourceUpload, SourceRef: "test/" + hash[:8],
	})
	if err != nil {
		h.t.Fatalf("IngestMemo: %v", err)
	}
	if _, err := h.store.RecordTranscript(h.ctx, store.TranscriptInput{
		MemoID: res.Memo.ID, Text: text, Model: "whisper.cpp/small.en",
		Backend: "vulkan", Partial: true,
	}); err != nil {
		h.t.Fatalf("RecordTranscript: %v", err)
	}
	for _, step := range [][2]string{
		{store.StateCaptured, store.StateQueued},
		{store.StateQueued, store.StateTranscribing},
		{store.StateTranscribing, store.StateTranscribed},
	} {
		if _, err := h.store.AdvanceMemoState(h.ctx, res.Memo.ID, step[0], step[1], ""); err != nil {
			h.t.Fatalf("advance: %v", err)
		}
	}
	m, err := h.store.GetMemo(h.ctx, res.Memo.ID)
	if err != nil {
		h.t.Fatalf("GetMemo: %v", err)
	}
	return m
}

// propose writes a tier-1 proposal for a memo, the way a Scribe run would.
func (h *harness) propose(memoID uuid.UUID, p *scribe.Proposal) store.Proposal {
	h.t.Helper()
	tr, err := h.tier1.TranscriptForScribe(h.ctx, memoID)
	if err != nil {
		h.t.Fatalf("TranscriptForScribe: %v", err)
	}
	out := scribe.Outcome{Proposal: p, Raw: []byte(`{"destination":"TICKET"}`), Status: scribe.StatusValid}
	if p == nil {
		out = scribe.Outcome{Raw: []byte("not json"), Status: scribe.StatusInvalid,
			Err: fmt.Errorf("scribe: invalid proposal — destination: is required")}
	}
	got, err := h.tier1.SaveProposal(h.ctx, memoID, tr.ID, testProposer, out)
	if err != nil {
		h.t.Fatalf("SaveProposal: %v", err)
	}
	return got
}

// ticketProposal is the ordinary valid TICKET proposal.
func ticketProposal(project string) *scribe.Proposal {
	key := project
	return &scribe.Proposal{
		Destination: scribe.DestTicket, Confidence: 0.9,
		Reason: "names an owner and an outcome", Title: "Do the thing",
		ProjectKey: &key, TicketType: "task", Description: "## Summary\nthe thing",
	}
}

// accept builds an item that accepts a memo's proposal as shown.
func (h *harness) accept(memoID uuid.UUID) Item {
	h.t.Helper()
	p, err := h.tier1.GetProposal(h.ctx, memoID, testProposer)
	if err != nil {
		return Item{MemoID: memoID, Proposer: testProposer}
	}
	g := p.Generation
	return Item{MemoID: memoID, Proposer: testProposer, Generation: &g}
}

// override builds an item carrying a decision the operator authored.
func (h *harness) override(memoID uuid.UUID, o Override) Item {
	it := h.accept(memoID)
	it.Override = &o
	return it
}

func (h *harness) apply(actor store.User, items ...Item) []Result {
	h.t.Helper()
	out, err := h.svc.Apply(h.ctx, actor, items)
	if err != nil {
		h.t.Fatalf("Apply: %v", err)
	}
	return out
}

func (h *harness) link(memoID uuid.UUID) store.MemoLink {
	h.t.Helper()
	l, err := h.store.MemoLinkFor(h.ctx, memoID)
	if err != nil {
		h.t.Fatalf("MemoLinkFor: %v", err)
	}
	return l
}

func (h *harness) state(memoID uuid.UUID) string {
	h.t.Helper()
	m, err := h.store.GetMemo(h.ctx, memoID)
	if err != nil {
		h.t.Fatalf("GetMemo: %v", err)
	}
	return m.State
}

func (h *harness) hold(memoID uuid.UUID) {
	h.t.Helper()
	if _, err := h.store.AdvanceMemoState(h.ctx, memoID, store.StateTranscribed, store.StateHeld, "held by the operator"); err != nil {
		h.t.Fatalf("hold: %v", err)
	}
}

// wantStatus asserts one result's status, with the reason in the message —
// which is the field a person would read anyway.
func wantStatus(t *testing.T, got Result, want string) {
	t.Helper()
	if got.Status != want {
		t.Fatalf("status = %q (%s), want %q", got.Status, got.Reason, want)
	}
}
