package switchyard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	return NewTicket{ProjectKey: "CHRN", Type: "task", Title: "Do the thing",
		Description: "## Summary\nx", MemoID: memo}
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

// Derived, never random: a random key makes every retry a new ticket, which is
// exactly the failure this prevents. Keying on the memo also catches the
// likelier accident — the same memo in two different batches.
func TestTheIdempotencyKeyIsDerivedFromTheMemo(t *testing.T) {
	memo := uuid.New()
	key := IdempotencyKey(memo)

	// Derived from the memo, so it is the same key on every retry without
	// anything having to be stored between them.
	if !strings.Contains(key, memo.String()) {
		t.Errorf("key %q does not name the memo", key)
	}
	if key == IdempotencyKey(uuid.New()) {
		t.Fatal("two memos share a key")
	}

	tr := &tracker{t: t}
	c := tr.start(t)
	if _, err := c.CreateTicket(context.Background(), aTicket(memo)); err != nil {
		t.Fatal(err)
	}
	if len(tr.keys) != 1 || tr.keys[0] != IdempotencyKey(memo) {
		t.Fatalf("sent key %v, want %s", tr.keys, IdempotencyKey(memo))
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
		{"no project", NewTicket{Type: "task", Title: "t", MemoID: memo}, "no project key"},
		{"no title", NewTicket{ProjectKey: "CHRN", Type: "task", MemoID: memo}, "no title"},
		{"no memo", NewTicket{ProjectKey: "CHRN", Type: "task", Title: "t"}, "no memo id"},
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
