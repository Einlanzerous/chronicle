package amber

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
)

// ---------------------------------------------------------------------------
// THE SAME TESTS, AGAINST THE REAL ARCHIVE.
//
// Everything above this file is driven by fixtures taken from Amber's source.
// That proves the transport handles what Amber DOCUMENTS; it cannot prove the
// document is what Amber DOES. This one runs the whole path — client, resolver,
// classifier — against the live service, and it is how "an Amber reference
// resolves to live state" was actually checked rather than argued.
//
// IT SKIPS UNLESS BOTH VARIABLES ARE SET, so CI never depends on the estate
// being up and no test of Chronicle's reaches the network by default. Run it
// the way the credential is held, which is never as a literal:
//
//	signet exec --secret construct-server/AMBER_API_TOKEN -- bash -c \
//	  'CHRONICLE_AMBER_URL=http://127.0.0.1:4008 \
//	   CHRONICLE_AMBER_TOKEN="$AMBER_API_TOKEN" \
//	   go test ./internal/amber -run Live -v'
//
// The single quotes matter: the token is expanded by the CHILD shell, so it
// never lands in the parent's history or in a process listing.
//
// 127.0.0.1:4008 is the host port Uptime Kuma uses; inside construct_net the
// same service is http://amber:4008, which is what a deployed Chronicle sets.
// ---------------------------------------------------------------------------

func liveClient(t *testing.T) *Client {
	t.Helper()
	base, token := os.Getenv("CHRONICLE_AMBER_URL"), os.Getenv("CHRONICLE_AMBER_TOKEN")
	if base == "" || token == "" {
		t.Skip("CHRONICLE_AMBER_URL / CHRONICLE_AMBER_TOKEN unset — not reaching the live archive")
	}
	c, err := New(base, token)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// liveCitation asks Amber for one recent event and takes the citation it
// publishes for it. Every event carries the finished string in its `cite`
// field, so nothing here constructs a reference by hand — which is AMBR-11's
// own instruction: "building one by hand should never be necessary".
func liveCitation(t *testing.T, c *Client) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.BaseURL()+"/v1/events?limit=1", nil)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+os.Getenv("CHRONICLE_AMBER_TOKEN"))
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("GET /v1/events: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/events: status %d", resp.StatusCode)
	}
	var page struct {
		Events []struct {
			Cite string `json:"cite"`
		} `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatalf("decode events: %v", err)
	}
	if len(page.Events) == 0 || page.Events[0].Cite == "" {
		t.Skip("the live archive published no citable event — nothing to resolve")
	}
	return page.Events[0].Cite
}

func TestLiveArchiveResolvesACitationToItsUpstreamState(t *testing.T) {
	c := liveClient(t)
	token := liveCitation(t, c)

	// THE PARSER AND THE MINTER HAVE TO AGREE, or the two halves of this
	// feature never meet: internal/markdown decides which tokens in prose are
	// citations, and this one came out of Amber itself. References uses the
	// pure path with no key set, which is all an amber1 token needs — its
	// namespace is certain from the bytes.
	refs := markdown.References([]byte("see " + token + " for this"))
	if len(refs) != 1 || refs[0].System != markdown.SystemAmber || refs[0].Token != token {
		t.Fatalf("the parser did not recognise a citation Amber itself minted: %q -> %+v", token, refs)
	}

	out := resolver(t, c).Resolve(context.Background(), refs)
	res := out[0]
	t.Logf("live: state=%s outcome=%q recoverable=%v fetched_at=%s",
		res.State, upstreamOutcome(res), upstreamRecoverable(res), res.FetchedAt.Format(time.RFC3339))

	if res.State != resolve.StateResolved {
		t.Fatalf("state = %q (explain: %q), want resolved for a citation Amber just published",
			res.State, res.Explain)
	}
	if res.Upstream == nil || res.Upstream.Outcome != "held" {
		t.Fatalf("upstream = %+v, want outcome held", res.Upstream)
	}

	// And no archive content came with it. The live body carries the cited
	// block's text; none of it may be in what Chronicle hands on.
	blob, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{"project_slug", "vocabulary_version", "objects_read"} {
		if strings.Contains(string(blob), field) {
			t.Errorf("the resolution carries %s from the archive answer", field)
		}
	}
}

// A well-formed citation the archive does not hold. It must come back BROKEN
// with Amber's own outcome — the archive answered, and "no" is an answer — and
// never as unreachable, which would report a healthy archive as an outage.
func TestLiveArchiveAnswersAboutACitationItDoesNotHold(t *testing.T) {
	c := liveClient(t)
	absent := "amber1.00000000-0000-4000-8000-000000000000.11111111-1111-4111-8111-111111111111.0"

	res := resolveOne(t, c, absent)
	t.Logf("live: state=%s outcome=%q explain=%q", res.State, upstreamOutcome(res), res.Explain)

	if res.State != resolve.StateBroken {
		t.Fatalf("state = %q, want broken: the archive answered that it holds nothing for this", res.State)
	}
	if res.Upstream == nil || res.Upstream.Outcome == "" {
		t.Fatal("no outcome relayed: the card would say a thing is missing without saying which way")
	}
	if res.Explain == "" {
		t.Error("no sentence from Amber on a broken citation")
	}
}

func upstreamOutcome(r resolve.Resolution) string {
	if r.Upstream == nil {
		return ""
	}
	return r.Upstream.Outcome
}

func upstreamRecoverable(r resolve.Resolution) bool {
	return r.Upstream != nil && r.Upstream.Recoverable
}
