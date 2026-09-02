package switchyard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// tracker is a Switchyard that honours Idempotency-Key the way the real one
// does: a replayed key returns the original response verbatim, and no second
// ticket exists.
type tracker struct {
	t        *testing.T
	created  map[string]string // idempotency key -> ticket key
	bodies   []map[string]any
	keys     []string
	next     int
	status   int
	noKeyOut bool
}

func (tr *tracker) start(t *testing.T) *Client {
	t.Helper()
	tr.created = map[string]string{}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tr.status != 0 {
			w.WriteHeader(tr.status)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tr.bodies = append(tr.bodies, body)

		idem := r.Header.Get("Idempotency-Key")
		tr.keys = append(tr.keys, idem)

		key, replayed := tr.created[idem]
		if !replayed {
			tr.next++
			key = "CHRN-" + string(rune('0'+tr.next))
			if idem != "" {
				tr.created[idem] = key
			}
		}
		out := map[string]any{"id": "11111111-1111-1111-1111-111111111111"}
		if !tr.noKeyOut {
			out["key"] = key
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(s.Close)
	c, err := New(s.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func aTicket(memo uuid.UUID) NewTicket {
	return aTicketWithKey(memo, "chronicle-decision-1")
}

func aTicketWithKey(memo uuid.UUID, key string) NewTicket {
	return NewTicket{ProjectKey: "CHRN", Type: "task", Title: "Do the thing",
		Description: "## Summary\nx", MemoID: memo, IdempotencyKey: key}
}

// The whole of this ticket: a memo that creates CHRN-1 and then gets replayed
// must return CHRN-1, not create CHRN-2.
func TestAReplayedCreateReturnsTheSameTicket(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	memo := uuid.New()

	first, err := c.CreateTicket(context.Background(), aTicket(memo))
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateTicket(context.Background(), aTicket(memo))
	if err != nil {
		t.Fatal(err)
	}
	if first.Key != second.Key {
		t.Fatalf("replay produced %s after %s — a second ticket for one memo", second.Key, first.Key)
	}
	if tr.next != 1 {
		t.Fatalf("the tracker minted %d tickets for one memo", tr.next)
	}
}

// The caller's key is sent verbatim. It belongs to the DECISION and is stored
// beside it, so a retry replays and a new decision does not.
func TestTheCallersIdempotencyKeyIsSentVerbatim(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	if _, err := c.CreateTicket(context.Background(),
		aTicketWithKey(uuid.New(), "chronicle-decision-abc")); err != nil {
		t.Fatal(err)
	}
	if len(tr.keys) != 1 || tr.keys[0] != "chronicle-decision-abc" {
		t.Fatalf("sent key %v, want the caller's", tr.keys)
	}
}

// A key derived from the memo would poison it: Switchyard caches every
// sub-500 JSON response — including a 4xx — under the key it was sent with,
// for 24 hours. A refusal would then replay for every corrected decision the
// operator made that day. A NEW decision must be able to reach Switchyard.
func TestANewDecisionOnTheSameMemoIsNotDeduplicated(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	memo := uuid.New()

	first, err := c.CreateTicket(context.Background(), aTicketWithKey(memo, "decision-1"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.CreateTicket(context.Background(), aTicketWithKey(memo, "decision-2"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Key == second.Key {
		t.Fatal("a second decision on the same memo replayed the first — the key is memo-derived somewhere")
	}
}

// Refused rather than defaulted: inventing a key here would make it fresh per
// call, which duplicates on every retry — the same failure with better manners.
func TestCreateRefusesAMissingIdempotencyKey(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	_, err := c.CreateTicket(context.Background(), aTicketWithKey(uuid.New(), ""))
	if err == nil || !strings.Contains(err.Error(), "no idempotency key") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if tr.next != 0 {
		t.Error("a keyless create reached the tracker")
	}
}

// Two decisions minted independently must not collide.
func TestNewIdempotencyKeyIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		k := NewIdempotencyKey()
		if seen[k] {
			t.Fatalf("minted key %q twice", k)
		}
		seen[k] = true
	}
}

// "The created ticket records where it came from (the memo, and Chronicle) so
// the trail back to the original voice note is not lost the moment the ticket
// is edited."
func TestTheTicketSaysWhichMemoMadeIt(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	memo := uuid.New()
	if _, err := c.CreateTicket(context.Background(), aTicket(memo)); err != nil {
		t.Fatal(err)
	}

	md, ok := tr.bodies[0]["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("no metadata on the created ticket: %v", tr.bodies[0])
	}
	if md["chronicle_memo_id"] != memo.String() {
		t.Errorf("metadata memo = %v, want %s", md["chronicle_memo_id"], memo)
	}
	if md["source"] != MetadataSource {
		t.Errorf("metadata source = %v, want %q", md["source"], MetadataSource)
	}
}

func TestTheKeyAndADeepLinkComeBack(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	got, err := c.CreateTicket(context.Background(), aTicket(uuid.New()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Key == "" {
		t.Fatal("no key")
	}
	if !strings.HasSuffix(got.URL, "/tickets/"+got.Key) {
		t.Errorf("url = %q, want a deep link to %s", got.URL, got.Key)
	}
}

// A guessed project_key is immutable after creation, so refusing here means a
// needs_input proposal cannot become a permanently misfiled ticket by way of a
// caller that forgot to check.
func TestCreateRefusesWhatCannotBeUndone(t *testing.T) {
	tr := &tracker{t: t}
	c := tr.start(t)
	memo := uuid.New()

	for _, tc := range []struct {
		name string
		in   NewTicket
		want string
	}{
		{"no project", NewTicket{Type: "task", Title: "t", MemoID: memo, IdempotencyKey: "k"}, "no project key"},
		{"no title", NewTicket{ProjectKey: "CHRN", Type: "task", MemoID: memo, IdempotencyKey: "k"}, "no title"},
		{"no memo", NewTicket{ProjectKey: "CHRN", Type: "task", Title: "t", IdempotencyKey: "k"}, "no memo id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.CreateTicket(context.Background(), tc.in); err == nil ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
	if tr.next != 0 {
		t.Errorf("a refused create still reached the tracker %d times", tr.next)
	}
}

// A ticket with no key is a ticket nothing can link to, which is worse than a
// failure because it looks like success.
func TestATicketWithNoKeyIsRefused(t *testing.T) {
	tr := &tracker{t: t, noKeyOut: true}
	c := tr.start(t)
	if _, err := c.CreateTicket(context.Background(), aTicket(uuid.New())); err == nil ||
		!strings.Contains(err.Error(), "no key came back") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

func TestACreateFailureCarriesItsStatus(t *testing.T) {
	tr := &tracker{t: t, status: http.StatusConflict}
	c := tr.start(t)
	_, err := c.CreateTicket(context.Background(), aTicket(uuid.New()))

	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want a *switchyard.Error", err)
	}
	if se.Status != http.StatusConflict || se.Retryable() {
		t.Errorf("status %d retryable %v, want 409 and not retryable", se.Status, se.Retryable())
	}
}

// The header replays a RESPONSE; it does not serialise a SIDE EFFECT.
//
// Switchyard's middleware looks the key up, runs the handler, then caches —
// with no lock in between — so two overlapping requests for one memo both
// create. This asserts the client does not paper over that, because a caller
// that believed it were safe would skip the serialisation it actually needs
// (CHRN-33's pending row with UNIQUE (memo_id)).
func TestTheClientDoesNotPretendTheHeaderIsALock(t *testing.T) {
	// A tracker that behaves the way the real middleware does under
	// concurrency: the cache is consulted, but nothing serialises the handler.
	var mu sync.Mutex
	minted := 0
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // both requests are inside the handler together
		mu.Lock()
		minted++
		key := "CHRN-" + string(rune('0'+minted))
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": key, "id": "x"})
	}))
	t.Cleanup(s.Close)
	c, err := New(s.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}

	memo := uuid.New()
	var wg sync.WaitGroup
	keys := make([]string, 2)
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tk, err := c.CreateTicket(context.Background(), aTicketWithKey(memo, "one-decision"))
			if err == nil {
				keys[i] = tk.Key
			}
		}()
	}
	close(release)
	wg.Wait()

	if keys[0] == keys[1] {
		t.Fatalf("both concurrent creates returned %s — this client does not serialise, "+
			"and a test asserting it does would hide the duplication CHRN-33 must prevent", keys[0])
	}
	if minted != 2 {
		t.Fatalf("minted %d tickets, want 2 — the point is that the header did NOT stop the second", minted)
	}
}
