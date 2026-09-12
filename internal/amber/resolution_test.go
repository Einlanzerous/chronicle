package amber

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
)

// ---------------------------------------------------------------------------
// CHRN-50's three Done-when clauses, end to end through the real resolver:
//
//	1. an Amber reference resolves to live state
//	2. an unreachable Amber renders as unresolved rather than as nothing
//	3. no Amber content is stored in Chronicle's tables
//
// Driven through resolve.Resolver rather than against Fetch alone, because the
// claim is about what a reader is shown and not about what one call returned.
// ---------------------------------------------------------------------------

// resolver wires this transport the way a render path will.
func resolver(t *testing.T, c *Client) *resolve.Resolver {
	t.Helper()
	r, err := resolve.New(resolve.Options{
		Transports: map[string]resolve.Transport{markdown.SystemAmber: c},
	})
	if err != nil {
		t.Fatalf("resolve.New: %v", err)
	}
	return r
}

// 1 · LIVE STATE.
func TestACitationResolvesToLiveState(t *testing.T) {
	c, _ := serve(t, http.StatusOK, heldBody())
	res := resolveOne(t, c, citation)

	if res.State != resolve.StateResolved {
		t.Fatalf("state = %q, want resolved (explain: %q)", res.State, res.Explain)
	}
	if res.Upstream == nil {
		t.Fatal("no upstream on a resolved citation: the archive answered and its answer is the card")
	}
	// AMBER'S OWN VOCABULARY MEMBER, relayed rather than translated. `held` is
	// the state the ticket imagined as `SEALED`, and Chronicle does not mint a
	// name in somebody else's namespace.
	if res.Upstream.Outcome != "held" {
		t.Errorf("outcome = %q, want held", res.Upstream.Outcome)
	}
	// Amber's own sentence, not a Chronicle paraphrase that would drift from
	// /v1/cite/format.
	if !strings.Contains(res.Explain, "in the archive") {
		t.Errorf("explain = %q, want Amber's own sentence relayed", res.Explain)
	}
	// The card can show its age, which is what keeps the cache from being a
	// copy that lies.
	if res.FetchedAt.IsZero() {
		t.Error("FetchedAt is zero on an answer that was actually fetched")
	}
	if res.LastResolvedAt == nil {
		t.Error("LastResolvedAt unset after a successful resolve")
	}
	// No outbound arrow: Amber serves no HTML, so there is nowhere to go.
	if res.Upstream.URL != "" {
		t.Errorf("URL = %q, want none", res.Upstream.URL)
	}
	// The reference is echoed back exactly as written.
	if res.Ref.Token != citation {
		t.Errorf("token = %q, want it echoed verbatim", res.Ref.Token)
	}
}

// Every member of the vocabulary lands as a card carrying its own name and its
// own recoverability. "Renders as nothing" is the failure mode this forbids:
// five of these six arrive on a non-2xx, which is precisely where a careless
// client drops the body and shows an empty card or no card at all.
func TestEveryOutcomeAmberPublishesBecomesACardRatherThanNothing(t *testing.T) {
	for _, tc := range []struct {
		outcome     string
		status      int
		recoverable bool
		want        resolve.State
	}{
		{"held", http.StatusOK, false, resolve.StateResolved},
		{"not_captured", http.StatusNotFound, false, resolve.StateBroken},
		{"block_out_of_range", http.StatusNotFound, false, resolve.StateBroken},
		{"prompt_only", http.StatusGone, false, resolve.StateBroken},
		{"not_in_capture", http.StatusGone, true, resolve.StateBroken},
		{"ambiguous", http.StatusConflict, true, resolve.StateBroken},

		// A seventh member Amber adds after this build ships. It reads as
		// broken and carries its own name and sentence verbatim, because
		// guessing "resolved" is the confident direction to be wrong in and a
		// compile error here would make Amber unable to extend its own
		// vocabulary.
		{"quarantined", http.StatusGone, true, resolve.StateBroken},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			body := `{"ref":"` + citation + `","outcome":"` + tc.outcome +
				`","explain":"Amber's own sentence about ` + tc.outcome +
				`","recoverable":` + boolText(tc.recoverable) + `}`
			c, _ := serve(t, tc.status, body)
			res := resolveOne(t, c, citation)

			if res.State != tc.want {
				t.Fatalf("state = %q, want %q", res.State, tc.want)
			}
			if res.Upstream == nil {
				t.Fatal("no upstream: the archive ANSWERED, and an answer about the referent is not nothing")
			}
			if res.Upstream.Outcome != tc.outcome {
				t.Errorf("outcome = %q, want %q relayed verbatim", res.Upstream.Outcome, tc.outcome)
			}
			// `recoverable` is the distinction this epic would otherwise have
			// had to invent: "aged out for good" against "try again later".
			if res.Upstream.Recoverable != tc.recoverable {
				t.Errorf("recoverable = %v, want %v", res.Upstream.Recoverable, tc.recoverable)
			}
			if !strings.Contains(res.Explain, tc.outcome) {
				t.Errorf("explain = %q, want Amber's own sentence", res.Explain)
			}
		})
	}
}

// 2 · UNREACHABLE RENDERS AS UNRESOLVED, NOT AS NOTHING.
func TestAnUnreachableAmberRendersAsUnresolvedRatherThanAsNothing(t *testing.T) {
	// A server that is started and immediately stopped: the address is right
	// and nothing is listening on it, which is what a stopped container looks
	// like from here.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	addr := srv.URL
	srv.Close()

	c, err := New(addr, "a-token")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	second := "amber1.9d654a7c-1ef5-49c8-bbe5-076164725a9d.11111111-2222-3333-4444-555555555555.0"
	out := resolver(t, c).Resolve(context.Background(),
		[]markdown.Reference{ref(citation), ref(second)})

	if out[0].State != resolve.StateUnreachable {
		t.Fatalf("state = %q, want unreachable", out[0].State)
	}
	// UNRESOLVED, WITH A SENTENCE. An empty explain is the "renders as nothing"
	// this clause forbids: the reader is owed the difference between "the
	// archive says this is gone" and "nobody could ask".
	if out[0].Explain == "" {
		t.Error("an unreachable card carries no sentence")
	}
	// And it is a fact about Chronicle's ability to ask, so there is no
	// upstream answer to show.
	if out[0].Upstream != nil {
		t.Errorf("upstream = %+v on a call nobody answered", out[0].Upstream)
	}
	if out[0].FetchedAt.IsZero() {
		t.Error("FetchedAt is zero: an attempt WAS made, and when it was made is what a card shows")
	}

	// The second reference is not dialled — one dead archive is one dead
	// archive — but it still says something rather than nothing.
	if out[1].State != resolve.StateUnchecked {
		t.Errorf("second state = %q, want unchecked", out[1].State)
	}
	if out[1].Explain == "" {
		t.Error("the unchecked card carries no sentence either")
	}
	for i, res := range out {
		if res.State == resolve.StateResolved {
			t.Errorf("reference %d claims resolved against an archive that never answered", i)
		}
	}
}

// A refused credential is NOT a missing citation. Rendering it as broken would
// tell a reader their evidence is gone when the archive is holding it and
// Chronicle simply was not let in.
func TestARefusedCredentialIsNotAMissingCitation(t *testing.T) {
	c, _ := serve(t, http.StatusUnauthorized, `{"error":"missing or invalid bearer token"}`)
	res := resolveOne(t, c, citation)

	if res.State != resolve.StateUnreachable {
		t.Fatalf("state = %q, want unreachable: a 401 is Chronicle's problem, not the evidence's", res.State)
	}
	if !strings.Contains(res.Explain, "credential") {
		t.Errorf("explain = %q, want it to name the credential so nobody goes looking for a lost record", res.Explain)
	}
}

// 3 · NO AMBER CONTENT IS KEPT.
//
// The strongest form available at this layer: a held answer carries the
// archived text, and NONE of it may appear anywhere in what Chronicle hands
// on. Serialised and searched rather than field-by-field, so a field added
// later that quietly carried content would fail this too.
func TestNoArchiveContentSurvivesTheCall(t *testing.T) {
	c, _ := serve(t, http.StatusOK, heldBody())

	// Twice: the second read comes from the cache, which is the copy that
	// would hold content if anything did.
	r := resolver(t, c)
	for i, pass := range []string{"first", "cached"} {
		out := r.Resolve(context.Background(), []markdown.Reference{ref(citation)})
		blob, err := json.Marshal(out[0])
		if err != nil {
			t.Fatalf("%s pass: marshal: %v", pass, err)
		}
		if strings.Contains(string(blob), archiveText) {
			t.Fatalf("%s pass (%d): archive content reached the resolution: %s", pass, i, blob)
		}
		if strings.Contains(string(blob), "project_slug") {
			t.Fatalf("%s pass: session evidence reached the resolution: %s", pass, blob)
		}
	}
}

// A cached card still carries the instant it was true. The pair matters for
// Amber specifically: a citation's state changes when a retention sweep takes
// the source, and a card that showed `held` with no age would be a copy that
// lies about evidence somebody is about to rely on.
func TestACachedCardKeepsTheInstantItWasTrue(t *testing.T) {
	c, rec := serve(t, http.StatusOK, heldBody())
	r := resolver(t, c)

	first := r.Resolve(context.Background(), []markdown.Reference{ref(citation)})[0]
	second := r.Resolve(context.Background(), []markdown.Reference{ref(citation)})[0]

	if rec.calls != 1 {
		t.Fatalf("dialled %d times; the second render should be a cache hit", rec.calls)
	}
	if !second.FetchedAt.Equal(first.FetchedAt) {
		t.Errorf("cached FetchedAt = %v, want the original %v: the age is the answer's, not the render's",
			second.FetchedAt, first.FetchedAt)
	}
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
